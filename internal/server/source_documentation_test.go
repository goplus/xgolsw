package server

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefinitionContextSourceDocumentation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		want   string
	}{
		{name: "GroupedVariable", source: `var (
// Declaration documentation.
value int
other int
)
echo |value
`, want: "Declaration documentation.\n"},
		{name: "GroupedConstant", source: `const (
// Declaration documentation.
value = 1
other = 2
)
echo |value
`, want: "Declaration documentation.\n"},
		{name: "GroupedType", source: `type (
// Declaration documentation.
Item int
Other string
)
var value |Item
`, want: "Declaration documentation.\n"},
		{name: "MultipleNames", source: `// Declaration documentation.
var first, second int
echo |second
`, want: "Declaration documentation.\n"},
		{name: "VariableTrailingComment", source: `var value int // Declaration documentation.
echo |value
`, want: "Declaration documentation.\n"},
		{name: "TypeTrailingComment", source: `type Item int // Declaration documentation.
var value |Item
`, want: "Declaration documentation.\n"},
		{name: "VariableLeadingComment", source: `// Declaration documentation.
var value int // Trailing comment.
echo |value
`, want: "Declaration documentation.\n"},
		{name: "TypeLeadingComment", source: `// Declaration documentation.
type Item int // Trailing comment.
var value |Item
`, want: "Declaration documentation.\n"},
		{name: "GroupDoesNotDocumentIndividualMembers", source: `// Group documentation.
var (
value int
other int
)
echo |value
`},
		{name: "ParameterDoesNotBorrowFunctionDocumentation", source: `// Function documentation.
func run(value int) { echo |value }
`},
		{name: "FieldTrailingComment", source: `type Item struct {
Value int // Declaration documentation.
}
var item Item
echo item.|Value
`, want: "Declaration documentation.\n"},
		{name: "SiblingLocalType", source: `func first() {
// Unrelated documentation.
type Item int
var item Item
echo item
}
func second() {
// Declaration documentation.
type Item string
var item |Item
echo item
}
`, want: "Declaration documentation.\n"},
		{name: "NestedLocalType", source: `func run() {
// Outer documentation.
type Item int
if true {
// Declaration documentation.
type Item string
var item |Item
echo item
}
}
`, want: "Declaration documentation.\n"},
		{name: "LocalAlias", source: `func run() {
// Declaration documentation.
type Item = int
var item |Item
echo item
}
`, want: "Declaration documentation.\n"},
		{name: "NestedFunctionLocal", source: `// Enclosing documentation.
func run() {
invoke := func() { value := 1; echo |value }
invoke()
}
`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			proj := s.getProj()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			file, err := proj.ASTFile("main.xgo")
			require.NoError(t, err)
			_, obj, _ := objectAtPosition(proj, info, file, ToPosition(proj, file, position))
			require.NotNil(t, obj)
			ctx := &definitionContext{proj: proj}
			doc, ok := ctx.sourceDocumentation(obj)
			assert.True(t, ok)
			assert.Equal(t, tt.want, doc)
		})
	}

	t.Run("ForeignPackagePosition", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`// Project documentation.
var value int
`)})
		proj := s.getProj()
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		local := info.Pkg.Scope().Lookup("value")
		require.NotNil(t, local)
		foreign := gotypes.NewVar(local.Pos(), gotypes.NewPackage("main", "main"), "value", gotypes.Typ[gotypes.Int])
		ctx := &definitionContext{proj: proj}
		doc, ok := ctx.sourceDocumentation(foreign)
		assert.False(t, ok)
		assert.Empty(t, doc)
		def := ctx.definitionForVar(foreign, "", false, &pkgdoc.PkgDoc{Vars: map[string]string{"value": "Imported documentation."}})
		assert.Equal(t, "Imported documentation.", def.Detail)
	})

	t.Run("SourceVersion", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`// Old documentation.
var value int
`)})
		proj := s.getProj()
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		old := info.Pkg.Scope().Lookup("value")
		require.NotNil(t, old)
		s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(`// New documentation.
var value int
`), Version: 1}})
		ctx := &definitionContext{proj: s.getProj()}
		doc, ok := ctx.sourceDocumentation(old)
		assert.False(t, ok)
		assert.Empty(t, doc)
	})

	t.Run("GeneratedObject", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": nil})
		proj := s.getProj()
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		obj := gotypes.NewVar(token.NoPos, info.Pkg, "generated", gotypes.Typ[gotypes.Int])
		ctx := &definitionContext{proj: proj}
		doc, ok := ctx.sourceDocumentation(obj)
		assert.False(t, ok)
		assert.Empty(t, doc)
	})
}
