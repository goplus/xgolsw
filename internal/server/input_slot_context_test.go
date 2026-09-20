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
			{name: "ForPhraseDeclaration", code: `for shared <- []string{label(11)} {
	println 22
}
`},
			{name: "ComprehensionDeclaration", code: `println [label(22) for shared <- []string{label(11)}]
`},
			{name: "FilteredComprehension", code: `println [label(22) for shared <- []string{label(11)}, len(shared) > 0]
`},
			{name: "MapComprehension", code: `println {shared: label(22) for shared <- []string{label(11)}}
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
				for _, literal := range []*ast.BasicLit{before, after} {
					assert.NotContains(t, collectPredefinedNames(ctx, literal, nil), "_xgo_ret")
				}
				beforeItems := completionItemsAt(t, s, "main.xgo", ctx.position(before.Pos()))
				for _, item := range beforeItems {
					if item.Label == "shared" {
						require.NotNil(t, item.Documentation)
						doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
						assert.Contains(t, doc.Value, "shared int")
					}
				}
				afterItems := completionItemsAt(t, s, "main.xgo", ctx.position(after.Pos()))
				for _, item := range afterItems {
					if item.Label == "shared" {
						require.NotNil(t, item.Documentation)
						doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
						assert.Contains(t, doc.Value, "shared string")
					}
				}
			})
		}
	})

	t.Run("TypeDeclarationShadowing", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("var value int\nfunc run() {\nprintln value, 11\ntype value string\nprintln 22\n}\n")})
		info, err := s.requestProject().TypeInfo()
		require.NoError(t, err)
		for ident, obj := range info.Uses {
			if ident.Name == "value" {
				assert.Same(t, info.Pkg.Scope().Lookup("value"), obj)
			}
		}
		ctx := inputSlotTestContext(t, s, "main.xgo")
		for _, value := range []string{"11", "22", "11", "22"} {
			names := collectPredefinedNames(ctx, inputSlotLiteral(t, ctx, value), gotypes.Typ[gotypes.Int])
			if value == "11" {
				assert.Contains(t, names, "value")
			} else {
				assert.NotContains(t, names, "value")
			}
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
		s := newFrameworkTestServer(t, map[string][]byte{
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
				mod := testframework.NewModule(t)
				class, ok := mod.LookupClass("_fixture.gox")
				require.True(t, ok)
				class.PkgPaths = append(class.PkgPaths, "os", "io")
				s := newFrameworkTestServerWithModule(t, files, mod)
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
		s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte("var Count int\nprintln 5\n")})
		importer := &completionTestImporter{Importer: s.getProj().Importer}
		s.getProj().Importer = importer
		ctx := inputSlotTestContext(t, s, "main_fixture.gox")
		_, err := ctx.proj.TypeInfo()
		require.NoError(t, err)
		importer.unavailablePath = testframework.PkgPath
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

func TestServerClassfileCandidateShadowing(t *testing.T) {
	for _, kind := range []struct {
		name     string
		filename string
	}{
		{name: "NormalClass", filename: "Record.gox"},
		{name: "ProjectClass", filename: "main_fixture.gox"},
		{name: "WorkClass", filename: "Worker_fixture.gox"},
	} {
		t.Run(kind.name, func(t *testing.T) {
			filename := kind.filename
			for _, tt := range []struct {
				name      string
				signature string
				body      string
				visible   bool
			}{
				{name: "Visible", signature: "()", body: "number(11)\n", visible: true},
				{name: "Parameter", signature: "(zebra string)", body: "println zebra\nnumber(11)\n"},
				{name: "NamedResult", signature: "() (zebra string)", body: "number(11)\nreturn \"\"\n"},
				{name: "Local", signature: "()", body: "zebra := \"\"\nprintln zebra\nnumber(11)\n"},
				{name: "Constant", signature: "()", body: "const zebra = \"\"\nnumber(11)\n"},
				{name: "Type", signature: "()", body: "type zebra string\nnumber(11)\n"},
				{name: "Initializer", signature: "()", body: "zebra := label(11)\nprintln zebra\nnumber(22)\n", visible: true},
			} {
				t.Run(tt.name, func(t *testing.T) {
					source := "func zebra() int { return 1 }\nfunc number(n int) {}\nfunc label(n int) string { return \"\" }\nfunc run" + tt.signature + " {\n" + tt.body + "}\n"
					files := map[string][]byte{filename: []byte(source)}
					if filename == "Worker_fixture.gox" {
						files["main_fixture.gox"] = nil
					}
					s := newFrameworkTestServer(t, files)
					_, err := s.requestProject().TypeInfo()
					require.NoError(t, err)
					ctx := inputSlotTestContext(t, s, filename)
					literal := inputSlotLiteral(t, ctx, "11")
					names := collectPredefinedNames(ctx, literal, gotypes.Typ[gotypes.Int])
					if tt.visible {
						assert.Contains(t, names, "zebra")
					} else {
						assert.NotContains(t, names, "zebra")
						items := completionItemsAt(t, s, filename, ctx.position(literal.Pos()))
						assert.NotContains(t, completionItemLabels(items), "zebra")
					}
					if tt.name == "Initializer" {
						literal = inputSlotLiteral(t, ctx, "22")
						assert.NotContains(t, collectPredefinedNames(ctx, literal, gotypes.Typ[gotypes.Int]), "zebra")
					}
				})
			}
		})
	}
}

func inputSlotCall(t *testing.T, ctx *inputSlotContext, name string) *ast.CallExpr {
	t.Helper()

	var found *ast.CallExpr
	ast.Inspect(ctx.astFile, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		var callee string
		switch fun := call.Fun.(type) {
		case *ast.Ident:
			callee = fun.Name
		case *ast.SelectorExpr:
			callee = fun.Sel.Name
		}
		if callee == name {
			found = call
			return false
		}
		return true
	})
	require.NotNil(t, found, name)
	return found
}

func TestServerComprehensionFilterScopes(t *testing.T) {
	for _, kind := range []struct {
		name      string
		filename  string
		newServer testServerFactory
	}{
		{name: "XGo", filename: "main.xgo", newServer: newTestServer},
		{name: "NormalClass", filename: "Record.gox", newServer: newTestServer},
		{name: "ProjectClass", filename: "main_fixture.gox", newServer: newFrameworkTestServer},
		{name: "WorkClass", filename: "Worker_fixture.gox", newServer: newFrameworkTestServer},
	} {
		t.Run(kind.name, func(t *testing.T) {
			for _, tt := range []struct {
				name string
				expr string
			}{
				{name: "List", expr: "[number(11) for value <- [number(44)], limit := number(22); limit > number(33)]"},
				{name: "Map", expr: "{number(11): value for value <- [number(44)], limit := number(22); limit > number(33)}"},
				{name: "Select", expr: "{number(11) for value <- [number(44)], limit := number(22); limit > number(33)}"},
				{name: "NestedClauses", expr: "[number(11) for inner <- [number(55)], other := limit + 1; other > 0 for value <- [number(44)], limit := number(22); limit > number(33)]"},
				{name: "NestedComprehension", expr: "[[number(11) for inner <- [number(55)]] for value <- [number(44)], limit := number(22); limit > number(33)]"},
				{name: "NestedFunction", expr: "[func() int { before := number(11); var limit string; println limit; return before }() for value <- [number(44)], limit := number(22); limit > number(33)]"},
				{name: "InitializerFunction", expr: "[number(11) for value <- [number(44)], limit := func() int { return number(22) }(); limit > number(33)]"},
				{name: "MultipleDeclarations", expr: "[number(11) for value <- [number(44)], limit, other := pair(number(22)); limit + other > number(33)]"},
			} {
				t.Run(tt.name, func(t *testing.T) {
					const prefix = "var limit string\nfunc number(n int) int { return n }\nfunc pair(n int) (int, int) { return n, n }\nfunc run() {\nprintln "
					source := prefix + tt.expr + "\n}\n"
					files := map[string][]byte{kind.filename: []byte(source)}
					if kind.filename == "Worker_fixture.gox" {
						files["main_fixture.gox"] = nil
					}
					s := kind.newServer(t, files)
					for version := range 2 {
						if version > 0 {
							s.ModifyFiles([]FileChange{{Path: kind.filename, Content: []byte("\n" + source), Version: version}})
						}
						_, err := s.requestProject().TypeInfo()
						require.NoError(t, err)
						ctx := inputSlotTestContext(t, s, kind.filename)
						// Revisit both sides of the initializer to exercise cached candidates.
						for _, value := range []string{"11", "22", "33", "44", "33", "22", "11"} {
							literal := inputSlotLiteral(t, ctx, value)
							names := collectPredefinedNames(ctx, literal, gotypes.Typ[gotypes.Int])
							items := completionItemsAt(t, s, kind.filename, ctx.position(literal.Pos()))
							if value == "11" || value == "33" {
								assert.Contains(t, names, "limit")
								item := completionItemByLabel(items, "limit")
								require.NotNil(t, item)
								require.NotNil(t, item.Documentation)
								doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
								assert.Contains(t, doc.Value, "var limit int")
							} else {
								assert.NotContains(t, names, "limit")
								assert.NotContains(t, completionItemLabels(items), "limit")
							}
							if tt.name == "NestedClauses" || tt.name == "MultipleDeclarations" {
								if value == "11" {
									assert.Contains(t, names, "other")
									assert.Contains(t, completionItemLabels(items), "other")
								} else if value == "22" || value == "44" {
									assert.NotContains(t, names, "other")
								}
							}
						}
					}
				})
			}
		})
	}
}
