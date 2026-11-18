package server

import (
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotRecommendedDeclarationsGenericEmbeddedFields(t *testing.T) {
	for _, tt := range []struct {
		name, embedded, field string
	}{
		{name: "SingleTypeArgument", embedded: "Embedded[int]", field: "Embedded"},
		{name: "PointerTypeArgument", embedded: "*Embedded[int]", field: "Embedded"},
		{name: "MultipleTypeArguments", embedded: "Pair[int, string]", field: "Pair"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := "type Holder struct {\n//xgols:notrecommended\n" + tt.embedded + "\nVisible int\n}\nvar visibleValue int\n"
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			proj := s.getProj()
			file, err := proj.ASTFile("main.xgo")
			require.NoError(t, err)
			declarations := newNotRecommendedDeclarations(proj, nil)
			// Generic embedded types can be parsed while their type information
			// is incomplete. Their arguments and neighboring names stay unmarked.
			var embedded token.Pos
			ast.Inspect(file, func(node ast.Node) bool {
				if ident, ok := node.(*ast.Ident); ok {
					if ident.Name == tt.field {
						embedded = ident.Pos()
					} else {
						assert.False(t, declarations.hasSourcePosition(file, ident.Pos()), ident.Name)
					}
				}
				return true
			})
			require.True(t, embedded.IsValid())
			assert.True(t, declarations.hasSourcePosition(file, embedded))
		})
	}
}

func TestServerNotRecommendedQualifiedEmbeddedField(t *testing.T) {
	const declarations = `import dep "example.com/dependency"
type Holder struct {
    //xgols:notrecommended
    dep.Record
}
var holder Holder
`
	for _, tt := range []struct {
		name, use string
		want      []string
		absent    []string
	}{
		{
			name: "Member", use: "func run() { println holder.|VisibleField }\n",
			want: []string{"VisibleField", "visibleMethod"}, absent: []string{"Record", "HiddenField", "hiddenMethod"},
		},
		{
			name: "Type", use: "func run() { var item dep.|Record; println item.VisibleField }\n",
			want: []string{"Record"}, absent: []string{"HiddenType"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, declarations+tt.use)
			s := newNotRecommendedImportServer(t, source)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			for _, name := range tt.want {
				assert.Contains(t, labels, name)
			}
			for _, name := range tt.absent {
				assert.NotContains(t, labels, name)
			}
		})
	}
}

func TestServerNotRecommendedClassEmbeddedField(t *testing.T) {
	for _, tt := range []struct {
		name, directive string
		wantEmbedded    bool
	}{
		{name: "Annotated", directive: "//xgols:notrecommended\n"},
		{name: "Unannotated", wantEmbedded: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `type Embedded struct { Value int }
var box Box
println box.|Value
`)
			s := newTestServer(t, map[string][]byte{
				"main.xgo": []byte(source),
				"Box.gox":  []byte("var (\n" + tt.directive + "*Embedded\nVisible int\n)\n"),
			})
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Contains(t, labels, "Visible")
			assert.Contains(t, labels, "Value")
			if tt.wantEmbedded {
				assert.Contains(t, labels, "Embedded")
			} else {
				assert.NotContains(t, labels, "Embedded")
			}
		})
	}
}

func TestServerNotRecommendedMalformedEmbeddedField(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `type Broken struct {
    //xgols:notrecommended
    []int
}
var visibleValue int
func visible() {}
func run() {
|
}
`)
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	info, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	require.NotNil(t, info)
	labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
	assert.Contains(t, labels, "Broken")
	assert.Contains(t, labels, "visibleValue")
	assert.Contains(t, labels, "visible")
}

func TestNotRecommendedDeclarationsIncompleteEmbeddedField(t *testing.T) {
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`type Embedded struct{}
type Record struct {
    //xgols:notrecommended
    Embedded
    Visible int
}
//xgols:notrecommended
var hiddenValue int
var visibleValue int
`)})
	proj := s.getProj()
	info, err := proj.TypeInfo()
	require.NoError(t, err)
	file, err := proj.ASTFile("main.xgo")
	require.NoError(t, err)
	// A retained partial AST may lack a usable embedded type expression.
	ast.Inspect(file, func(node ast.Node) bool {
		if field, ok := node.(*ast.Field); ok && len(field.Names) == 0 {
			field.Type = &ast.BadExpr{From: field.Type.Pos(), To: field.Type.End()}
		}
		return true
	})
	declarations := newNotRecommendedDeclarations(proj, nil)
	assert.True(t, declarations.hasObject(info.Pkg.Scope().Lookup("hiddenValue")))
	assert.False(t, declarations.hasObject(info.Pkg.Scope().Lookup("visibleValue")))
}
