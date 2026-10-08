package server

import (
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"io/fs"
	"strings"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newSpxSymbolTestServer supplies synthetic SDK symbols without embedded package data.
func newSpxSymbolTestServer(t testing.TB, files map[string][]byte, pkgPath string) *Server {
	t.Helper()

	s := newTestServer(t, files)
	proj := s.getProj()
	const source = `package sdk
const XGoPackage = true
type Game struct{}
type Sprite interface {
    // Read declaration.
    Read(n int) int
    // Score declaration.
    Score() int
}
type SpriteAlias = Sprite
type SpriteImpl struct {
    // Count declaration.
    Count int
}
type SpriteImplAlias = SpriteImpl
// Read implementation.
func (*SpriteImpl) Read(n int) int { return n }
// Score implementation.
func (*SpriteImpl) Score() int { return 1 }
func (*SpriteImpl) ImplementationOnly() {}
`
	file, err := goparser.ParseFile(proj.Fset, "sdk.go", source, goparser.ParseComments)
	require.NoError(t, err)
	pkg, err := (&gotypes.Config{}).Check(pkgPath, proj.Fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	doc := pkgdoc.NewGo(pkgPath, &goast.Package{Name: pkg.Name(), Files: map[string]*goast.File{"sdk.go": file}})
	baseImporter, baseLookup := proj.Importer, s.lookupPkgDoc
	proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
		if path == pkgPath {
			return pkg, nil
		}
		return baseImporter.Import(path)
	})
	s.lookupPkgDoc = func(path string) (*pkgdoc.PkgDoc, error) {
		if path == pkgPath {
			return doc, nil
		}
		return baseLookup(path)
	}
	return s
}

func setSpxSymbolTestModule(t testing.TB, s *Server, pkgPath string) {
	t.Helper()

	s.getProj().SetModule(newTestModule(t, modload.Module{Opt: &modfile.File{Projects: []*modfile.Project{{
		Ext: ".spx", Class: "Game", PkgPaths: []string{pkgPath},
		Works: []*modfile.Class{{Ext: ".spx", Class: "SpriteImpl"}},
	}}}}))
}

func TestSpxSymbolsIsSpxSymbol(t *testing.T) {
	for _, tt := range []struct {
		name        string
		registered  bool
		pkgPath     string
		distinct    bool
		unavailable bool
		want        bool
	}{
		{name: "Registered", registered: true, pkgPath: SpxPkgPath, want: true},
		{name: "Unregistered", pkgPath: SpxPkgPath},
		{name: "OtherFramework", registered: true, pkgPath: "example.com/framework"},
		{name: "DistinctPackage", registered: true, pkgPath: SpxPkgPath, distinct: true},
		{name: "UnavailablePackage", registered: true, pkgPath: SpxPkgPath, unavailable: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newSpxSymbolTestServer(t, nil, SpxPkgPath)
			if tt.registered {
				setSpxSymbolTestModule(t, s, tt.pkgPath)
			}
			proj := s.getProj()
			pkg, err := proj.Importer.Import(SpxPkgPath)
			require.NoError(t, err)
			obj := pkg.Scope().Lookup("SpriteImpl")
			require.NotNil(t, obj)
			if tt.distinct {
				other := newSpxSymbolTestServer(t, nil, SpxPkgPath)
				pkg, err := other.getProj().Importer.Import(SpxPkgPath)
				require.NoError(t, err)
				obj = pkg.Scope().Lookup("SpriteImpl")
			}
			if tt.unavailable {
				proj.Importer = testImporterFunc(func(string) (*gotypes.Package, error) { return nil, fs.ErrNotExist })
			} else if !tt.registered || tt.pkgPath != SpxPkgPath {
				proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
					assert.Fail(t, "unexpected import", path)
					return nil, fs.ErrNotExist
				})
			}
			ctx := newSpxSymbols(proj)
			assert.Equal(t, tt.want, ctx.isSpxSymbol(obj))
			assert.False(t, ctx.isSpxSymbol(gotypes.Universe.Lookup("int")))
		})
	}
}

func TestServerSpxSymbolDefinitions(t *testing.T) {
	for _, tt := range []struct {
		name       string
		pkgPath    string
		typ        string
		registered bool
		owner      string
		doc        string
		iface      bool
	}{
		{"UnregisteredImplementation", SpxPkgPath, "SpriteImpl", false, "SpriteImpl", "Read implementation.", false},
		{"RegisteredImplementation", SpxPkgPath, "SpriteImpl", true, "Sprite", "Read implementation.", false},
		{"ImplementationAlias", SpxPkgPath, "SpriteImplAlias", true, "Sprite", "Read implementation.", false},
		{"UnregisteredInterface", SpxPkgPath, "Sprite", false, "Sprite", "Read declaration.", true},
		{"RegisteredInterface", SpxPkgPath, "Sprite", true, "Sprite", "Read implementation.", true},
		{"InterfaceAlias", SpxPkgPath, "SpriteAlias", true, "Sprite", "Read implementation.", true},
		{"OtherFramework", "example.com/framework", "SpriteImpl", true, "SpriteImpl", "Read implementation.", false},
		{"OtherInterface", "example.com/framework", "Sprite", true, "Sprite", "Read declaration.", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, fmt.Sprintf("import sdk %q\ntype Target = sdk.%s\nvar value Target\necho value.|read(1)\n", tt.pkgPath, tt.typ))
			s := newSpxSymbolTestServer(t, map[string][]byte{"main.xgo": []byte(source)}, tt.pkgPath)
			if tt.registered {
				setSpxSymbolTestModule(t, s, tt.pkgPath)
			}
			_, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			wantID := "xgo:" + tt.pkgPath + "?" + tt.owner + ".read"
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `def-id="`+wantID+`"`)
			assert.Contains(t, hover.Contents.Value, tt.doc)
			items := completionItemsAt(t, s, "main.xgo", position)
			item := completionItemByLabel(items, "read")
			require.NotNil(t, item)
			data := requireValueAs[*XGoCompletionItemData](t, item.Data)
			assert.Equal(t, wantID, data.Definition.String())
			doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
			assert.Contains(t, doc.Value, tt.doc)
			if tt.iface {
				assert.NotContains(t, completionItemLabels(items), "implementationOnly")
				assert.NotContains(t, completionItemLabels(items), "Count")
			} else {
				require.NotNil(t, completionItemByLabel(items, "implementationOnly"))
				fieldItem := completionItemByLabel(items, "Count")
				require.NotNil(t, fieldItem)
				fieldData := requireValueAs[*XGoCompletionItemData](t, fieldItem.Data)
				assert.Equal(t, "xgo:"+tt.pkgPath+"?"+tt.owner+".Count", fieldData.Definition.String())
				properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Target"})
				require.NoError(t, err)
				var found bool
				for _, property := range properties {
					if property.Name == "score" {
						found = true
						assert.Equal(t, "xgo:"+tt.pkgPath+"?"+tt.owner+".score", property.Definition.String())
						assert.Equal(t, "Score implementation.", strings.TrimSpace(property.Doc))
					}
				}
				assert.True(t, found)
			}
			links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}})
			require.NoError(t, err)
			assert.Contains(t, links, DocumentLink{Range: hover.Range, Target: ToPtr(URI(wantID))})
			position.Character += uint32(len("read("))
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
			}})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, "read(n int) int", help.Signatures[0].Label)
			require.NotNil(t, help.Signatures[0].Documentation)
			assert.Contains(t, fmt.Sprint(help.Signatures[0].Documentation.Value), tt.doc)
		})
	}
}

func TestDefinitionContextSpxFunctionDocumentation(t *testing.T) {
	for _, tt := range []struct {
		name           string
		implementation *pkgdoc.TypeDoc
		want           string
	}{
		{name: "Implementation", implementation: &pkgdoc.TypeDoc{Methods: map[string]string{"Read": "Implementation."}}, want: "Implementation."},
		{name: "EmptyComment", implementation: &pkgdoc.TypeDoc{Methods: map[string]string{"Read": ""}}},
		{name: "MissingMethod", implementation: &pkgdoc.TypeDoc{}, want: "Declaration."},
		{name: "MissingType", want: "Declaration."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newSpxSymbolTestServer(t, nil, SpxPkgPath)
			setSpxSymbolTestModule(t, s, SpxPkgPath)
			proj := s.getProj()
			pkg, err := proj.Importer.Import(SpxPkgPath)
			require.NoError(t, err)
			named := requireValueAs[*gotypes.Named](t, pkg.Scope().Lookup("Sprite").Type())
			iface := requireValueAs[*gotypes.Interface](t, named.Underlying())
			fun := iface.Method(0)
			require.Equal(t, "Read", fun.Name())
			doc := &pkgdoc.PkgDoc{Types: map[string]*pkgdoc.TypeDoc{
				"Sprite":     {Methods: map[string]string{"Read": "Declaration."}},
				"SpriteImpl": tt.implementation,
			}}
			ctx := &definitionContext{proj: proj, lookupPkgDoc: func(path string) (*pkgdoc.PkgDoc, error) {
				assert.Equal(t, SpxPkgPath, path)
				return doc, nil
			}}
			assert.Equal(t, tt.want, ctx.definitionForFunc(fun, "", doc).Detail)
			assert.Equal(t, tt.want, ctx.functionDocumentation(fun))
		})
	}
}
