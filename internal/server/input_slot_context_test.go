package server

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectPredefinedNames(t *testing.T) {
	t.Run("LexicalScope", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`var shared int
func main() {
	before := 1
	println 11
	later := 2
	println 22
	{
		var shared string
		println 33
	}
}
`)})
		ctx := inputSlotTestContext(t, s, "main.xgo")
		for _, tt := range []struct {
			name    string
			literal string
			want    []string
		}{
			{name: "BeforeDeclaration", literal: "11", want: []string{"before", "shared"}},
			{name: "AfterDeclaration", literal: "22", want: []string{"before", "later", "shared"}},
			{name: "ShadowedDifferentType", literal: "33", want: []string{"before", "later"}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				literal := inputSlotLiteral(t, ctx, tt.literal)
				assert.ElementsMatch(t, tt.want, collectPredefinedNames(ctx, literal, gotypes.Typ[gotypes.Int]))
			})
		}
	})

	t.Run("DeclarationVisibility", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			code string
		}{
			{name: "ShortDeclaration", code: `shared := label(11)
println 22
`},
			{name: "VariableDeclaration", code: `var shared = label(11)
println 22
`},
			{name: "ConstantDeclaration", code: `const shared = string(11)
println 22
`},
			{name: "ParentDeclarationAfterBlock", code: `{
	println 11
}
var shared string
println 22
`},
			{name: "RangeDeclaration", code: `for _, shared := range []string{label(11)} {
	println 22
}
`},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`var shared int
func label(n int) string { return "" }
func main() {
` + tt.code + "}\n")})
				_, err := s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)
				ctx := inputSlotTestContext(t, s, "main.xgo")
				before := inputSlotLiteral(t, ctx, "11")
				after := inputSlotLiteral(t, ctx, "22")
				assert.ElementsMatch(t, []string{"shared"}, collectPredefinedNames(ctx, before, gotypes.Typ[gotypes.Int]))
				assert.Empty(t, collectPredefinedNames(ctx, after, gotypes.Typ[gotypes.Int]))
				assert.ElementsMatch(t, []string{"shared"}, collectPredefinedNames(ctx, before, gotypes.Typ[gotypes.Int]))
			})
		}
	})

	t.Run("CrossFileGlobals", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo":   []byte("println 5\n"),
			"values.xgo": []byte("var exported int\nconst limit = 100\nvar message string\n"),
		})
		ctx := inputSlotTestContext(t, s, "main.xgo")
		assert.ElementsMatch(t, []string{"exported", "limit"}, collectPredefinedNames(ctx, inputSlotLiteral(t, ctx, "5"), gotypes.Typ[gotypes.Int]))
	})

	t.Run("ClassMembersAndCallback", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main_fixture.gox":   nil,
			"Worker_fixture.gox": []byte("var Count int\nonValue value => {\n\tprintln 5\n}\n"),
		})
		ctx := inputSlotTestContext(t, s, "Worker_fixture.gox")
		literal := inputSlotLiteral(t, ctx, "5")
		assert.ElementsMatch(t, []string{"Count", "Value", "value"}, collectPredefinedNames(ctx, literal, gotypes.Typ[gotypes.Int]))
		assert.ElementsMatch(t, []string{"label"}, collectPredefinedNames(ctx, literal, gotypes.Typ[gotypes.String]))
	})

	t.Run("ImplicitPackages", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			implicit bool
		}{
			{name: "ProjectClass", filename: "main_fixture.gox", implicit: true},
			{name: "WorkClass", filename: "Worker_fixture.gox", implicit: true},
			{name: "XGo", filename: "main.xgo"},
			{name: "StandaloneClass", filename: "Record.gox"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{"main_fixture.gox": nil}
				files[tt.filename] = []byte("println 5\n")
				s := newTestServer(t, files)
				class, ok := s.workspaceRootFS.Mod.LookupClass("_fixture.gox")
				require.True(t, ok)
				class.PkgPaths = append(class.PkgPaths, "os", "io")
				ctx := inputSlotTestContext(t, s, tt.filename)
				literal := inputSlotLiteral(t, ctx, "5")
				names := collectPredefinedNames(ctx, literal, nil)
				if tt.implicit {
					assert.Contains(t, names, "Args")
					assert.Contains(t, names, "EOF")
					assert.NotContains(t, names, "errInvalidWrite")
				} else {
					assert.NotContains(t, names, "Args")
					assert.NotContains(t, names, "EOF")
				}
				assert.NotContains(t, names, "Mouse")
				assert.NotContains(t, names, "this")
			})
		}
	})

	t.Run("UnavailableImplicitPackage", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main_fixture.gox": []byte("var Count int\nprintln 5\n")})
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		s.workspaceRootFS.Importer = completionTestImporter{Importer: s.workspaceRootFS.Importer, unavailablePath: testframework.PkgPath}
		ctx := inputSlotTestContext(t, s, "main_fixture.gox")
		assert.ElementsMatch(t, []string{"Count"}, collectPredefinedNames(ctx, inputSlotLiteral(t, ctx, "5"), gotypes.Typ[gotypes.Int]))
	})
}

func inputSlotLiteral(t *testing.T, ctx *inputSlotContext, value string) *ast.BasicLit {
	t.Helper()

	var literal *ast.BasicLit
	ast.Inspect(ctx.astFile, func(node ast.Node) bool {
		if lit, ok := node.(*ast.BasicLit); ok && lit.Value == value {
			literal = lit
		}
		return true
	})
	require.NotNil(t, literal)
	return literal
}
