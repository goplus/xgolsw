package server

import (
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomodfile "golang.org/x/mod/modfile"
)

func classfileTestModule() modload.Module {
	config := modload.Module{Opt: &modfile.File{}}
	for _, name := range []string{"first", "second"} {
		config.Opt.Projects = append(config.Opt.Projects, &modfile.Project{
			Ext: "." + name, Class: "App", PkgPaths: []string{"example.com/" + name},
			Works: []*modfile.Class{{Ext: "." + name + "work", Class: "Item"}},
		})
	}
	return config
}

func newClassfileTestServer(t testing.TB, files map[string][]byte) *Server {
	t.Helper()

	return newClassfileTestServerWithModule(t, files, classfileTestModule())
}

func newClassfileTestServerWithModule(t testing.TB, files map[string][]byte, config modload.Module) *Server {
	t.Helper()

	s := newTestServer(t, files)
	proj := s.getProj()
	baseImporter, baseLookup := proj.Importer, s.lookupPkgDoc
	packages := make(map[string]*gotypes.Package)
	docs := make(map[string]*pkgdoc.PkgDoc)
	importer := testImporterFunc(func(pkgPath string) (*gotypes.Package, error) {
		if pkg := packages[pkgPath]; pkg != nil {
			return pkg, nil
		}
		return baseImporter.Import(pkgPath)
	})
	add := func(pkgPath, source string) {
		t.Helper()
		file, err := goparser.ParseFile(proj.Fset, pkgPath+"/framework.go", source, goparser.ParseComments)
		require.NoError(t, err)
		pkg, err := (&gotypes.Config{Importer: importer}).Check(pkgPath, proj.Fset, []*goast.File{file}, nil)
		require.NoError(t, err)
		packages[pkgPath] = pkg
		docs[pkgPath] = pkgdoc.NewGo(pkgPath, &goast.Package{Name: pkg.Name(), Files: map[string]*goast.File{"framework.go": file}})
	}
	for _, name := range []string{"first", "second"} {
		pkgPath := "example.com/" + name
		add(pkgPath+"/internal/base", fmt.Sprintf(`package base
const XGoPackage = true
type Decoy struct {
    // Count belongs to another type.
    Count int
}
type State struct {
    // Count belongs to %s.
    Count int
}
type StateCopy State
// Read belongs to %s.
func (*State) Read(n int) int { return n }
// Score belongs to %s.
func (*State) Score() int { return 1 }
type Reader interface {
    // Read belongs to the reader in %s.
    Read(n int) int
}
type Box[T any] struct {
    // Value belongs to the box in %s.
    Value T
}
`, name, name, name, name, name))
		add(pkgPath, fmt.Sprintf(`package framework
import "example.com/%s/internal/base"
const XGoPackage = true
type App struct { *base.State }
type AppAlias = App
type Wrapper[T any] struct { *App }
type Options base.State
type OptionsAlias = Options
type PublicReader base.Reader
type ReaderAlias = PublicReader
type CombinedReader interface { PublicReader }
type IntBox base.Box[int]
func (*App) initApp() {}
type Item struct { *base.State }
func (*Item) Main() {}
func XGot_App_Main(app interface { initApp() }, items ...interface { Main() }) {}
`, name))
	}
	add("example.com/facade", `package facade
import (
    first "example.com/first"
    second "example.com/second"
)
type App = first.App
type Item = second.Item
`)
	proj.SetModule(newTestModule(t, config))
	proj.Importer = importer
	s.lookupPkgDoc = func(pkgPath string) (*pkgdoc.PkgDoc, error) {
		if doc := docs[pkgPath]; doc != nil {
			return doc, nil
		}
		return baseLookup(pkgPath)
	}
	return s
}

func TestClassBaseTypes(t *testing.T) {
	s := newClassfileTestServer(t, map[string][]byte{
		"First.first": nil, "Worker.firstwork": nil,
		"Second.second": nil, "Helper.secondwork": nil,
	})
	proj := s.getProj()
	ctx := &definitionContext{proj: proj}
	for _, name := range []string{"first", "second"} {
		pkg, err := proj.Importer.Import("example.com/" + name)
		require.NoError(t, err)
		for _, className := range []string{"App", "Item"} {
			named := resolvedNamedType(pkg.Scope().Lookup(className).Type())
			assert.True(t, ctx.isClassBaseType(named))
			duplicate := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, gotypes.NewPackage(pkg.Path(), pkg.Name()), className, nil), gotypes.NewStruct(nil, nil), nil)
			assert.False(t, ctx.isClassBaseType(duplicate))
		}
		base, err := proj.Importer.Import("example.com/" + name + "/internal/base")
		require.NoError(t, err)
		assert.False(t, ctx.isClassBaseType(resolvedNamedType(base.Scope().Lookup("State").Type())))
	}
	assert.Len(t, ctx.classTypes, 4)

	for _, tt := range []struct {
		name      string
		configure func(*modfile.Project)
		want      []string
	}{
		{name: "Pointers", configure: func(class *modfile.Project) {
			class.Class = "*App"
			class.Works[0].Class = "*Item"
		}, want: []string{"App", "Item"}},
		{name: "Alias", configure: func(class *modfile.Project) { class.Class = "AppAlias" }, want: []string{"App", "Item"}},
		{name: "Flat", configure: func(class *modfile.Project) { class.Flat = true }, want: []string{"App"}},
		{name: "MissingClass", configure: func(class *modfile.Project) { class.Class = "Missing" }, want: []string{"Item"}},
		{name: "NoPackages", configure: func(class *modfile.Project) { class.PkgPaths = nil }},
		{name: "UnavailablePackage", configure: func(class *modfile.Project) { class.PkgPaths = []string{"example.com/missing"} }},
		{name: "FirstPackageOnly", configure: func(class *modfile.Project) {
			class.PkgPaths = []string{"example.com/first/internal/base", "example.com/first"}
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newClassfileTestServer(t, map[string][]byte{"main.xgo": []byte("import f \"example.com/first\"\nvar value f.App\n")})
			proj := s.getProj()
			config := classfileTestModule()
			tt.configure(config.Opt.Projects[0])
			proj.SetModule(newTestModule(t, config))
			var names []string
			for named := range classBaseTypes(proj) {
				assert.Equal(t, "example.com/first", named.Obj().Pkg().Path())
				names = append(names, named.Obj().Name())
			}
			assert.ElementsMatch(t, tt.want, names)
		})
	}

	t.Run("ImportedPackagesOnly", func(t *testing.T) {
		s := newClassfileTestServer(t, map[string][]byte{"main.xgo": []byte("import \"example.com/facade\"\nvar value facade.App\n")})
		proj := s.getProj()
		_, err := proj.TypeInfo()
		require.NoError(t, err)
		first, err := proj.Importer.Import("example.com/first")
		require.NoError(t, err)
		second, err := proj.Importer.Import("example.com/second")
		require.NoError(t, err)
		proj.Importer = testImporterFunc(func(pkgPath string) (*gotypes.Package, error) {
			assert.Fail(t, "unexpected import after type checking", pkgPath)
			return nil, fmt.Errorf("unexpected import: %s", pkgPath)
		})
		bases := classBaseTypes(proj)
		require.Len(t, bases, 4)
		for _, pkg := range []*gotypes.Package{first, second} {
			for _, name := range []string{"App", "Item"} {
				assert.Contains(t, bases, resolvedNamedType(pkg.Scope().Lookup(name).Type()))
			}
		}
	})

	t.Run("OverriddenRegistration", func(t *testing.T) {
		s := newClassfileTestServer(t, map[string][]byte{"main.xgo": []byte("import f \"example.com/first\"\nvar value f.App\n")})
		proj := s.getProj()
		config := classfileTestModule()
		config.Opt.Projects = append(config.Opt.Projects, &modfile.Project{
			Ext: ".first", Class: "Options", PkgPaths: []string{"example.com/first"},
			Works: []*modfile.Class{{Ext: ".firstwork", Class: "IntBox"}},
		})
		proj.SetModule(newTestModule(t, config))
		registration, ok := proj.Module().LookupClass(".first")
		require.True(t, ok)
		var names []string
		for named := range classBaseTypes(proj) {
			names = append(names, named.Obj().Name())
		}
		assert.ElementsMatch(t, []string{"Options", "IntBox"}, names)
		after, ok := proj.Module().LookupClass(".first")
		require.True(t, ok)
		assert.Same(t, registration, after)
	})

	t.Run("UnusedFramework", func(t *testing.T) {
		s := newClassfileTestServer(t, map[string][]byte{"main.xgo": []byte("echo 1\n"), "Record.gox": nil})
		proj := s.getProj()
		_, err := proj.TypeInfo()
		require.NoError(t, err)
		proj.Importer = testImporterFunc(func(pkgPath string) (*gotypes.Package, error) {
			assert.Fail(t, "unexpected framework import", pkgPath)
			return nil, fmt.Errorf("unexpected import: %s", pkgPath)
		})
		assert.Empty(t, classBaseTypes(proj))
	})
}

func TestServerClassfileModuleUpdates(t *testing.T) {
	source, position := typeDisplayTestSource(t, "import f \"example.com/first\"\ntype Target = f.App\nvar value Target\necho value.|score()\n")
	s := newClassfileTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	proj := s.getProj()
	dir := t.TempDir()
	metadataPath := filepath.Join(dir, "gox.mod")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/first\n"), 0o644))
	require.NoError(t, os.WriteFile(metadataPath, []byte("project .first App example.com/first\nclass .firstwork Item\n"), 0o644))
	file, err := gomodfile.Parse("go.mod", []byte("module example.com/project\nrequire example.com/first v0.0.0\nreplace example.com/first => "+strconv.Quote(dir)+"\n"), nil)
	require.NoError(t, err)
	config := modload.Module{File: file, Opt: &modfile.File{ClassMods: []string{"example.com/first"}}}
	proj.SetModule(newTestModule(t, config))
	snapshot := proj.Snapshot()
	registration, ok := proj.Module().LookupClass(".first")
	require.True(t, ok)

	check := func(owner string, wantBaseNames []string) {
		t.Helper()

		var names []string
		for named := range classBaseTypes(proj) {
			names = append(names, named.Obj().Name())
		}
		assert.ElementsMatch(t, wantBaseNames, names)
		wantID := "xgo:" + owner + ".score"
		hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
		}})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, `def-id="`+wantID+`"`)
		assert.Contains(t, hover.Contents.Value, "Score belongs to first.")
		item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", position), "score")
		require.NotNil(t, item)
		assert.Equal(t, wantID, requireValueAs[*XGoCompletionItemData](t, item.Data).Definition.String())
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}})
		require.NoError(t, err)
		assert.Contains(t, links, DocumentLink{Range: hover.Range, Target: ToPtr(URI(wantID))})
		properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Target"})
		require.NoError(t, err)
		var found bool
		for _, property := range properties {
			if property.Name == "score" {
				found = true
				assert.Equal(t, wantID, property.Definition.String())
				assert.Equal(t, "Score belongs to first.", strings.TrimSpace(property.Doc))
			}
		}
		assert.True(t, found)
	}
	check("example.com/first?App", []string{"App", "Item"})

	// Disk changes do not change the loaded module or any source snapshot.
	require.NoError(t, os.WriteFile(metadataPath, []byte("project .first Options example.com/first\nclass .firstwork IntBox\n"), 0o644))
	replacement := newTestModule(t, config)
	check("example.com/first?App", []string{"App", "Item"})
	require.NoError(t, os.Remove(metadataPath))
	check("example.com/first?App", []string{"App", "Item"})
	after, ok := proj.Module().LookupClass(".first")
	require.True(t, ok)
	assert.Same(t, registration, after)
	_, err = formatSource(proj, "main.xgo", []byte(source))
	require.NoError(t, err)
	_, err = xgo.NewModule(config)
	require.Error(t, err)
	check("example.com/first?App", []string{"App", "Item"})

	// Installing the separately loaded replacement changes only this project.
	proj.SetModule(replacement)
	check("example.com/first/internal/base?State", []string{"Options", "IntBox"})
	var oldNames []string
	for named := range classBaseTypes(snapshot) {
		oldNames = append(oldNames, named.Obj().Name())
	}
	assert.ElementsMatch(t, []string{"App", "Item"}, oldNames)
}

func TestServerRegisteredClassfileMembers(t *testing.T) {
	for _, name := range []string{"First", "Second"} {
		framework := strings.ToLower(name)
		for _, class := range []struct{ name, filename, owner, prefix string }{
			{"Project", name + "." + framework, "App", "echo |"},
			{"Work", name + "Worker." + framework + "work", "Item", "echo |"},
			{"ProjectInstance", "main.xgo", "App", "var value " + name + "\necho value.|"},
			{"WorkInstance", "main.xgo", "Item", "var value " + name + "Worker\necho value.|"},
		} {
			for _, member := range []struct{ name, expression, label string }{
				{"Field", "Count", "Count"}, {"Method", "read(1)", "read"},
			} {
				t.Run(name+class.name+member.name, func(t *testing.T) {
					source, position := typeDisplayTestSource(t, class.prefix+member.expression+"\n")
					files := map[string][]byte{
						"First.first": nil, "FirstWorker.firstwork": nil,
						"Second.second": nil, "SecondWorker.secondwork": nil,
					}
					files[class.filename] = []byte(source)
					s := newClassfileTestServer(t, files)
					_, err := s.getProj().TypeInfo()
					require.NoError(t, err)
					wantID := "xgo:example.com/" + framework + "?" + class.owner + "." + member.label
					wantDoc := strings.ToUpper(member.label[:1]) + member.label[1:] + " belongs to " + framework + "."
					hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(class.filename)}, Position: position,
					}})
					require.NoError(t, err)
					require.NotNil(t, hover)
					assert.Contains(t, hover.Contents.Value, `def-id="`+wantID+`"`)
					assert.Contains(t, hover.Contents.Value, wantDoc)
					item := completionItemByLabel(completionItemsAt(t, s, class.filename, position), member.label)
					require.NotNil(t, item)
					data := requireValueAs[*XGoCompletionItemData](t, item.Data)
					assert.Equal(t, wantID, data.Definition.String())
					doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
					assert.Contains(t, doc.Value, wantDoc)
					links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(class.filename)}})
					require.NoError(t, err)
					assert.Contains(t, links, DocumentLink{Range: hover.Range, Target: ToPtr(URI(wantID))})
				})
			}
		}
	}
}

func TestServerRegisteredClassfileDefinition(t *testing.T) {
	s := newClassfileTestServer(t, map[string][]byte{
		"First.first":      []byte("var Total int\n"),
		"Worker.firstwork": []byte("echo Total\n"),
		"ignored.spx":      []byte("invalid source {{{"),
	})
	_, err := s.getProj().TypeInfo()
	require.NoError(t, err)
	params := &DefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///Worker.firstwork"},
		Position:     Position{Character: 5},
	}}
	for version := range 2 {
		def, err := s.textDocumentDefinition(params)
		require.NoError(t, err)
		assert.Equal(t, Location{
			URI:   "file:///First.first",
			Range: Range{Start: Position{Line: uint32(version), Character: 4}, End: Position{Line: uint32(version), Character: 9}},
		}, requireValueAs[Location](t, def))
		if version == 0 {
			s.ModifyFiles([]FileChange{{Path: "First.first", Content: []byte("\nvar Total int\n"), Version: 1}})
		}
	}
}

func TestServerImportedMemberDefinitions(t *testing.T) {
	for _, typ := range []struct {
		name, declaration, field, wantID, wantDoc string
	}{
		{"Defined", "type Record f.Options", "Count", "xgo:main?Record.Count", "Count belongs to first."},
		{"Imported", "type Record = f.Options", "Count", "xgo:example.com/first?Options.Count", "Count belongs to first."},
		{"ImportedAlias", "type Record = f.OptionsAlias", "Count", "xgo:example.com/first?Options.Count", "Count belongs to first."},
		{"Instantiated", "type Record = f.IntBox", "Value", "xgo:example.com/first?IntBox.Value", "Value belongs to the box in first."},
	} {
		for _, site := range []string{"Selector", "Literal", "Kwarg"} {
			t.Run(typ.name+site, func(t *testing.T) {
				label := typ.field
				source := "import f \"example.com/first\"\n" + typ.declaration + "\n"
				switch site {
				case "Selector":
					source += "var value Record\necho value.|" + typ.field + "\n"
				case "Literal":
					source += "var value = Record{|" + typ.field + ": 1}\n"
				case "Kwarg":
					label = xgoutil.ToLowerCamelCase(typ.field)
					source += "func configure(opts Record?) {}\nconfigure |" + label + " = 1\n"
				}
				source, position := typeDisplayTestSource(t, source)
				s := newClassfileTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
				}})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, `def-id="`+typ.wantID+`"`)
				assert.Contains(t, hover.Contents.Value, typ.wantDoc)
				assert.Contains(t, hover.Contents.Value, "field "+typ.field+" int")
				links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}})
				require.NoError(t, err)
				assert.Contains(t, links, DocumentLink{Range: hover.Range, Target: ToPtr(URI(typ.wantID))})

				switch site {
				case "Literal":
					s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(strings.ReplaceAll(source, typ.field+": 1", "")), Version: 1}})
				case "Kwarg":
					s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(strings.ReplaceAll(source, label+" =", label[:1]+" =")), Version: 1}})
					position.Character++
				}
				item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", position), label)
				require.NotNil(t, item)
				data := requireValueAs[*XGoCompletionItemData](t, item.Data)
				assert.Equal(t, typ.wantID, data.Definition.String())
				doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, doc.Value, typ.wantDoc)
				assert.Contains(t, doc.Value, "field "+typ.field+" int")
			})
		}
	}

	for _, tt := range []struct{ name, declaration, typ, owner string }{
		{"DefinedInterface", "", "f.PublicReader", "PublicReader"},
		{"InterfaceAlias", "", "f.ReaderAlias", "PublicReader"},
		{"ImportedEmbedding", "", "f.CombinedReader", "CombinedReader"},
		{"LocalEmbedding", "type Local interface { f.ReaderAlias }\n", "Local", "PublicReader"},
		{"StructEmbedding", "type Local struct { f.PublicReader }\n", "Local", "PublicReader"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, "import f \"example.com/first\"\n"+tt.declaration+"var value "+tt.typ+"\necho value.|read(1)\n")
			s := newClassfileTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			wantID := "xgo:example.com/first?" + tt.owner + ".read"
			const wantDoc = "Read belongs to the reader in first."
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `def-id="`+wantID+`"`)
			assert.Contains(t, hover.Contents.Value, wantDoc)
			links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}})
			require.NoError(t, err)
			assert.Contains(t, links, DocumentLink{Range: hover.Range, Target: ToPtr(URI(wantID))})
			item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", position), "read")
			require.NotNil(t, item)
			data := requireValueAs[*XGoCompletionItemData](t, item.Data)
			assert.Equal(t, wantID, data.Definition.String())
			doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
			assert.Contains(t, doc.Value, wantDoc)
		})
	}
}

func TestServerRegisteredClassfileProperties(t *testing.T) {
	for _, name := range []string{"First", "Second"} {
		t.Run(name, func(t *testing.T) {
			framework := strings.ToLower(name)
			filename := name + "." + framework
			source, position := typeDisplayTestSource(t, "echo |score()\n")
			s := newClassfileTestServer(t, map[string][]byte{filename: []byte(source)})
			info, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			wantID := "xgo:example.com/" + framework + "?App.score"
			wantDoc := "Score belongs to " + framework + "."
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: position,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `def-id="`+wantID+`"`)
			assert.Contains(t, hover.Contents.Value, wantDoc)
			properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: name})
			require.NoError(t, err)
			require.Len(t, properties, 2)
			assert.Equal(t, "score", properties[1].Name)
			assert.Equal(t, wantID, properties[1].Definition.String())
			assert.Equal(t, wantDoc, strings.TrimSpace(properties[1].Doc))
			ctx := &completionContext{
				definitionContext: definitionContext{proj: s.getProj(), lookupPkgDoc: s.lookupPkgDoc},
				typeInfo:          info, itemSet: newCompletionItemSet(Markdown),
			}
			ctx.collectPropertyNames(name)
			require.Len(t, ctx.itemSet.items, 2)
			item := completionItemByLabel(ctx.itemSet.items, `"score"`)
			require.NotNil(t, item)
			assert.Equal(t, `"score"`, item.Label)
			assert.Equal(t, PropertyCompletion, item.Kind)
			data := requireValueAs[*XGoCompletionItemData](t, item.Data)
			assert.Equal(t, wantID, data.Definition.String())
			doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
			assert.Contains(t, doc.Value, wantDoc)
		})
	}
}

func TestServerRegisteredClassfileLiteralMembers(t *testing.T) {
	for _, tt := range []struct {
		name, source, owner string
	}{
		{"FieldKey", "var value = Options{|Count: 1}\n", "Options"},
		{"FieldValue", "var value = Options{Count: |Count}\n", "App"},
		{"ElidedLiteral", "var values = []Options{{|Count: 1}}\n", "Options"},
		{"MapKey", "var values = map[int]int{|Count: 1}\n", "App"},
		{"MapValue", "var values = map[int]int{1: |Count}\n", "App"},
		{"SliceElement", "var values = []int{|Count}\n", "App"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newClassfileTestServer(t, map[string][]byte{"First.first": []byte(source)})
			_, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			wantID := "xgo:example.com/first?" + tt.owner + ".Count"
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///First.first"}, Position: position,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `def-id="`+wantID+`"`)
			assert.Contains(t, hover.Contents.Value, "Count belongs to first.")
			links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///First.first"}})
			require.NoError(t, err)
			assert.Contains(t, links, DocumentLink{Range: hover.Range, Target: ToPtr(URI(wantID))})
		})
	}
}

func TestClassTypeForFile(t *testing.T) {
	for _, tt := range []struct {
		name, filename, prefix, source, want string
	}{
		{name: "Project", filename: "main.first", want: "App"},
		{name: "NamedProject", filename: "First.first", want: "First"},
		{name: "Work", filename: "Worker.firstwork", want: "Worker"},
		{name: "WorkPrefix", filename: "Worker.firstwork", prefix: "Actor", want: "ActorWorker"},
		{name: "ReservedName", filename: "type.firstwork", want: "_type"},
		{name: "NestedFile", filename: "nested/Worker.firstwork", want: "Worker"},
		{name: "LineDirective", filename: "Worker.firstwork", source: "//line other.firstwork:100:20\nvar Count int\n", want: "Worker"},
		{name: "StandaloneClass", filename: "Record.gox", want: "Record"},
		{name: "PlainXGo", filename: "main.xgo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{tt.filename: []byte(tt.source)}
			if strings.HasSuffix(tt.filename, ".firstwork") {
				files["First.first"] = nil
			}
			s := newClassfileTestServer(t, files)
			proj := s.getProj()
			config := classfileTestModule()
			config.Opt.Projects[0].Works[0].Prefix = tt.prefix
			proj.SetModule(newTestModule(t, config))
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			file, err := proj.ASTFile(tt.filename)
			require.NoError(t, err)
			named := classTypeForFile(proj, file)
			if tt.want == "" {
				assert.Nil(t, named)
				return
			}
			require.NotNil(t, named)
			assert.Same(t, info.Pkg.Scope().Lookup(tt.want), named.Obj())
			ctx := &completionContext{definitionContext: definitionContext{proj: proj}, astFile: file}
			assert.Equal(t, tt.want, ctx.getPropertyTarget())
		})
	}

	t.Run("MissingFile", func(t *testing.T) {
		s := newTestServer(t, nil)
		assert.Nil(t, classTypeForFile(s.getProj(), nil))
	})
}

func TestServerImportedClassfileMembers(t *testing.T) {
	for _, tt := range []struct{ name, pkgPath, typ, owner, framework string }{
		{"Project", "example.com/first", "f.App", "App", "first"},
		{"Work", "example.com/second", "f.Item", "Item", "second"},
		{"Alias", "example.com/first", "f.AppAlias", "App", "first"},
		{"Pointer", "example.com/second", "*f.Item", "Item", "second"},
		{"GenericEmbedding", "example.com/first", "f.Wrapper[int]", "App", "first"},
		{"ReexportedProject", "example.com/facade", "f.App", "App", "first"},
		{"ReexportedWork", "example.com/facade", "f.Item", "Item", "second"},
	} {
		for _, member := range []struct{ name, expression, label string }{
			{"Field", "Count", "Count"}, {"Method", "score()", "score"},
		} {
			t.Run(tt.name+member.name, func(t *testing.T) {
				source, position := typeDisplayTestSource(t, "import f \""+tt.pkgPath+"\"\ntype Target = "+tt.typ+"\nvar value Target\necho value.|"+member.expression+"\n")
				files := map[string][]byte{"main.xgo": []byte(source)}
				s := newClassfileTestServer(t, files)
				wantID := "xgo:example.com/" + tt.framework + "?" + tt.owner + "." + member.label
				wantDoc := strings.ToUpper(member.label[:1]) + member.label[1:] + " belongs to " + tt.framework + "."
				for phase := range 3 {
					if phase == 1 {
						files["First.first"], files["Second.second"] = nil, nil
						s.ModifyFiles([]FileChange{{Path: "First.first"}, {Path: "Second.second"}})
					} else if phase == 2 {
						for _, filename := range []string{"First.first", "Second.second"} {
							delete(files, filename)
							require.NoError(t, s.getProj().DeleteFile(filename))
						}
					}
					_, err := s.getProj().TypeInfo()
					require.NoError(t, err)
					hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
					}})
					require.NoError(t, err)
					require.NotNil(t, hover)
					assert.Contains(t, hover.Contents.Value, `def-id="`+wantID+`"`)
					assert.Contains(t, hover.Contents.Value, wantDoc)
					item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", position), member.label)
					require.NotNil(t, item)
					data := requireValueAs[*XGoCompletionItemData](t, item.Data)
					assert.Equal(t, wantID, data.Definition.String())
					doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
					assert.Contains(t, doc.Value, wantDoc)
					links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}})
					require.NoError(t, err)
					assert.Contains(t, links, DocumentLink{Range: hover.Range, Target: ToPtr(URI(wantID))})
					if member.name == "Method" {
						properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Target"})
						require.NoError(t, err)
						var found bool
						for _, property := range properties {
							if property.Name == member.label {
								found = true
								assert.Equal(t, wantID, property.Definition.String())
								assert.Equal(t, wantDoc, strings.TrimSpace(property.Doc))
							}
						}
						assert.True(t, found, "missing property %s", member.label)
					}
				}
			})
		}
	}
}
