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
		{name: "GroupedVariable", source: "var (\n// Declaration documentation.\nvalue int\nother int\n)\necho |value\n", want: "Declaration documentation.\n"},
		{name: "GroupedConstant", source: "const (\n// Declaration documentation.\nvalue = 1\nother = 2\n)\necho |value\n", want: "Declaration documentation.\n"},
		{name: "GroupedType", source: "type (\n// Declaration documentation.\nItem int\nOther string\n)\nvar value |Item\n", want: "Declaration documentation.\n"},
		{name: "MultipleNames", source: "// Declaration documentation.\nvar first, second int\necho |second\n", want: "Declaration documentation.\n"},
		{name: "VariableTrailingComment", source: "var value int // Declaration documentation.\necho |value\n", want: "Declaration documentation.\n"},
		{name: "TypeTrailingComment", source: "type Item int // Declaration documentation.\nvar value |Item\n", want: "Declaration documentation.\n"},
		{name: "VariableLeadingComment", source: "// Declaration documentation.\nvar value int // Trailing comment.\necho |value\n", want: "Declaration documentation.\n"},
		{name: "TypeLeadingComment", source: "// Declaration documentation.\ntype Item int // Trailing comment.\nvar value |Item\n", want: "Declaration documentation.\n"},
		{name: "GroupDoesNotDocumentIndividualMembers", source: "// Group documentation.\nvar (\nvalue int\nother int\n)\necho |value\n"},
		{name: "ParameterDoesNotBorrowFunctionDocumentation", source: "// Function documentation.\nfunc run(value int) { echo |value }\n"},
		{name: "FieldTrailingComment", source: "type Item struct {\nValue int // Declaration documentation.\n}\nvar item Item\necho item.|Value\n", want: "Declaration documentation.\n"},
		{name: "SiblingLocalType", source: "func first() {\n// Unrelated documentation.\ntype Item int\nvar item Item\necho item\n}\nfunc second() {\n// Declaration documentation.\ntype Item string\nvar item |Item\necho item\n}\n", want: "Declaration documentation.\n"},
		{name: "NestedLocalType", source: "func run() {\n// Outer documentation.\ntype Item int\nif true {\n// Declaration documentation.\ntype Item string\nvar item |Item\necho item\n}\n}\n", want: "Declaration documentation.\n"},
		{name: "LocalAlias", source: "func run() {\n// Declaration documentation.\ntype Item = int\nvar item |Item\necho item\n}\n", want: "Declaration documentation.\n"},
		{name: "NestedFunctionLocal", source: "// Enclosing documentation.\nfunc run() {\ninvoke := func() { value := 1; echo |value }\ninvoke()\n}\n"},
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
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("// Project documentation.\nvar value int\n")})
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
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("// Old documentation.\nvar value int\n")})
		proj := s.getProj()
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		old := info.Pkg.Scope().Lookup("value")
		require.NotNil(t, old)
		s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte("// New documentation.\nvar value int\n"), Version: 1}})
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
