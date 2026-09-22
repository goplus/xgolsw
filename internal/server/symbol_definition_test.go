package server

import (
	"fmt"
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSymbolDefinitionMemberPackage(t *testing.T) {
	for _, tt := range []struct{ name, path string }{
		{"Framework", "example.com/framework/internal/engine"},
		{"SpxSubpackage", "github.com/goplus/spx/v3/internal/engine"},
		{"SpxPackage", "github.com/goplus/spx/v3"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, typeName := range []string{"Game", "Sprite", "SpriteImpl"} {
				t.Run(typeName, func(t *testing.T) {
					// Raw symbol helpers must not load an SDK or rename its types.
					pkg := gotypes.NewPackage(tt.path, "engine")
					field := gotypes.NewField(token.NoPos, pkg, "Count", gotypes.Typ[gotypes.Int], false)
					named := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, typeName, nil), gotypes.NewStruct([]*gotypes.Var{field}, nil), nil)
					method := gotypes.NewFunc(token.NoPos, pkg, "Read", gotypes.NewSignatureType(
						gotypes.NewVar(token.NoPos, pkg, "", named), nil, nil, nil, nil, false,
					))
					doc := &pkgdoc.PkgDoc{Types: map[string]*pkgdoc.TypeDoc{
						"SpriteImpl": {Methods: map[string]string{"Read": "Implementation documentation."}},
						typeName:     {Methods: map[string]string{"Read": "Declaration documentation."}},
					}}
					display := typeDisplay{}
					assert.Equal(t, typeName, extractTypeName(named))
					fieldDef := display.definitionForVar(field, typeName, false, nil)
					assert.Equal(t, "xgo:"+tt.path+"?"+typeName+".Count", fieldDef.ID.String())
					methodDef := display.definitionForFunc(method, "", doc)
					assert.Equal(t, "xgo:"+tt.path+"?"+typeName+".read", methodDef.ID.String())
					assert.Equal(t, "Declaration documentation.", methodDef.Detail)
				})
			}
		})
	}
}

func TestServerSymbolMemberNames(t *testing.T) {
	for _, tt := range []struct {
		name      string
		filename  string
		newServer testServerFactory
	}{
		{name: "XGo", filename: "main.xgo", newServer: newTestServer},
		{name: "Classfile", filename: "main_fixture.gox", newServer: newFrameworkTestServer},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, typeName := range []string{"Sprite", "SpriteImpl"} {
				t.Run(typeName, func(t *testing.T) {
					for _, member := range []struct {
						name string
						use  string
						kind CompletionItemKind
					}{
						{name: "Value", use: "Value", kind: FieldCompletion},
						{name: "Read", use: "Read()", kind: FunctionCompletion},
					} {
						t.Run(member.name, func(t *testing.T) {
							source, position := typeDisplayTestSource(t, fmt.Sprintf(`type %[1]s struct {
    // Value documentation.
    Value int
}
var value %[1]s
// Read documentation.
func (%[1]s) Read() int { return 0 }
echo value.|%[2]s
`, typeName, member.use))
							s := tt.newServer(t, map[string][]byte{tt.filename: []byte(source)})
							_, err := s.getProj().TypeInfo()
							require.NoError(t, err)
							wantID := "xgo:main?" + typeName + "." + member.name
							wantDoc := member.name + " documentation."

							hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
								TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}, Position: position,
							}})
							require.NoError(t, err)
							require.NotNil(t, hover)
							assert.Contains(t, hover.Contents.Value, `def-id="`+wantID+`"`)
							assert.Contains(t, hover.Contents.Value, wantDoc)

							item := completionItemByLabel(completionItemsAt(t, s, tt.filename, position), member.name)
							require.NotNil(t, item)
							assert.Equal(t, member.kind, item.Kind)
							data := requireValueAs[*XGoCompletionItemData](t, item.Data)
							assert.Equal(t, wantID, data.Definition.String())
							doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
							assert.Contains(t, doc.Value, wantDoc)

							links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
								TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
							})
							require.NoError(t, err)
							assert.Contains(t, links, DocumentLink{Range: hover.Range, Target: ToPtr(URI(wantID))})
						})
					}
				})
			}
		})
	}
}

func TestServerBuiltinInterfaceMember(t *testing.T) {
	source, position := typeDisplayTestSource(t, "var value error\necho value.|error()\n")
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	_, err := s.getProj().TypeInfo()
	require.NoError(t, err)
	const wantID = "xgo:builtin?error.error"
	hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
	}})
	require.NoError(t, err)
	require.NotNil(t, hover)
	assert.Contains(t, hover.Contents.Value, `def-id="`+wantID+`"`)
	assert.Contains(t, hover.Contents.Value, "func error() string")
	item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", position), "error")
	require.NotNil(t, item)
	assert.Equal(t, FunctionCompletion, item.Kind)
	data := requireValueAs[*XGoCompletionItemData](t, item.Data)
	assert.Equal(t, wantID, data.Definition.String())
	doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
	assert.Contains(t, doc.Value, "func error() string")
	links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}})
	require.NoError(t, err)
	assert.Contains(t, links, DocumentLink{Range: hover.Range, Target: ToPtr(URI(wantID))})
}
