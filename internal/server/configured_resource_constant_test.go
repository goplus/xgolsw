package server

import (
	"go/constant"
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerConfiguredResourceAliasConstants(t *testing.T) {
	const configuration = `{"dataFile":"resources.json","types":[{"pkgPath":"example.com/assets","typeName":"Name","contextURI":"demo://assets"},{"pkgPath":"main","typeName":"Other","contextURI":"demo://other"}]}`
	for _, tt := range []struct{ name, source, want string }{
		{"Qualified", "import assets \"example.com/assets\"\necho assets.Default\n", "import assets \"example.com/assets\"\necho assets.Name(\"Next\")\n"},
		{"RenamedImport", "import a \"example.com/assets\"\necho a.Default\n", "import a \"example.com/assets\"\necho a.Name(\"Next\")\n"},
		{"DotImport", "import . \"example.com/assets\"\necho Default\n", "import . \"example.com/assets\"\necho Name(\"Next\")\n"},
		{"LocalConstant", "import assets \"example.com/assets\"\nconst name assets.Name = \"Intro\"\necho name\n", "import assets \"example.com/assets\"\nconst name assets.Name = \"Next\"\necho name\n"},
		{"OtherContext", "import assets \"example.com/assets\"\necho assets.Default\nother assets.Default\n", "import assets \"example.com/assets\"\necho assets.Name(\"Next\")\nother assets.Default\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := tt.source
			name := "Intro"
			newServer := func() *Server {
				t.Helper()
				s := newConfiguredResourceTestServer(t, map[string][]byte{
					"main.xgo":       []byte(source),
					"types.xgo":      []byte("type Other = string\nfunc other(value Other) {}\n"),
					"resources.json": []byte(`{"demo://assets":["` + name + `"],"demo://other":["Intro"]}`),
				}, configuration)
				proj := s.getProj()
				fallback := proj.Importer
				pkg := gotypes.NewPackage("example.com/assets", "assets")
				typ := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "Name", nil), gotypes.Typ[gotypes.String])
				pkg.Scope().Insert(typ.Obj())
				pkg.Scope().Insert(gotypes.NewConst(token.NoPos, pkg, "Default", typ, constant.MakeString("Intro")))
				pkg.MarkComplete()
				proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
					if path == pkg.Path() {
						return pkg, nil
					}
					return fallback.Import(path)
				})
				return s
			}
			for _, next := range []string{"Next", "Final", ""} {
				s := newServer()
				proj := s.requestProject()
				_, err := proj.TypeInfo()
				require.NoError(t, err)
				links, err := documentLinksForResources(proj, "main.xgo")
				require.NoError(t, err)
				want := []string{"demo://assets/" + name}
				if tt.name == "LocalConstant" {
					want = append(want, want[0])
				}
				if tt.name == "OtherContext" {
					want = append(want, "demo://other/Intro")
				}
				assert.ElementsMatch(t, want, documentLinkTargets(t, links))
				if next == "" {
					break
				}
				edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: XGoResourceURI("demo://assets/" + name)}, NewName: next}})
				require.NoError(t, err)
				source = applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
				assert.Equal(t, strings.ReplaceAll(tt.want, "Next", next), source)
				name = next
			}
		})
	}
}

func TestServerConfiguredResourceConstantDependencies(t *testing.T) {
	const configuration = `{"dataFile":"resources.json","types":[{"pkgPath":"main","typeName":"Asset","contextURI":"demo://assets"},{"pkgPath":"main","typeName":"Other","contextURI":"demo://other"}]}`
	const declarations = "type Asset string\ntype Other string\nfunc use(value Asset) {}\nfunc other(value Other) {}\n"
	for _, tt := range []struct{ name, source, want string }{
		{"Suffix", "const derived = base + \"Tail\"\nuse derived\n", "const derived = \"IntroTail\"\nuse derived\n"},
		{"Prefix", "const derived = \"Tail\" + base\nuse derived\n", "const derived = \"TailIntro\"\nuse derived\n"},
		{"MultipleUses", "const derived = base + base\nuse derived\n", "const derived = \"IntroIntro\"\nuse derived\n"},
		{"Converted", "const derived = Other(base)\nother derived\n", "const derived = Other(\"Intro\")\nother derived\n"},
		{"Comparison", "const derived = base == \"Intro\"\necho derived\n", "const derived = (0 == 0)\necho derived\n"},
		{"ShadowedBoolean", "const true = 0\nconst derived = base == \"Intro\"\necho derived\n", "const true = 0\nconst derived = (0 == 0)\necho derived\n"},
		{"EscapedValue", "const derived = base + \"$$\"\necho derived\n", "const derived = \"Intro\\x24\"\necho derived\n"},
		{"RepeatedInitializer", "const (\nderived = base + \"Tail\"\nrepeated\n)\nuse derived\nuse repeated\n", "const (\nderived = \"IntroTail\"\nrepeated\n)\nuse derived\nuse repeated\n"},
		{"DependencyChain", "const derived = base + \"Tail\"\nconst next = derived + \"End\"\necho next\n", "const derived = \"IntroTail\"\nconst next = derived + \"End\"\necho next\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := "const base Asset = \"Intro\"\n" + tt.source
			name := "Intro"
			newServer := func() *Server {
				t.Helper()
				return newConfiguredResourceTestServer(t, map[string][]byte{
					"main.xgo": []byte(source), "types.xgo": []byte(declarations),
					"resources.json": []byte(`{"demo://assets":["` + name + `","IntroTail","TailIntro","IntroIntro"],"demo://other":["Intro"]}`),
				}, configuration)
			}
			original, err := newServer().requestProject().TypeInfo()
			require.NoError(t, err)
			for _, next := range []string{"Next", "Final", ""} {
				s := newServer()
				info, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
				for _, symbol := range []string{"derived", "repeated", "next"} {
					obj, ok := original.Pkg.Scope().Lookup(symbol).(*gotypes.Const)
					if !ok {
						continue
					}
					updated := requireValueAs[*gotypes.Const](t, info.Pkg.Scope().Lookup(symbol))
					assert.Equal(t, obj.Val().ExactString(), updated.Val().ExactString())
					assert.Equal(t, obj.Type().String(), updated.Type().String())
				}
				links, err := documentLinksForResources(s.requestProject(), "main.xgo")
				require.NoError(t, err)
				targets := documentLinkTargets(t, links)
				assert.Contains(t, targets, "demo://assets/"+name)
				if name != "Intro" {
					assert.NotContains(t, targets, "demo://assets/Intro")
				}
				if next == "" {
					break
				}
				edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: XGoResourceURI("demo://assets/" + name)}, NewName: next}})
				require.NoError(t, err)
				source = applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
				assert.Equal(t, "const base Asset = \""+next+"\"\n"+tt.want, source)
				name = next
			}
		})
	}
}
