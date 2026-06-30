package inspector

import (
	"slices"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectorXGoExtensionNodes(t *testing.T) {
	newInspector := func() *Inspector {
		f := &ast.File{
			Name: &ast.Ident{Name: "main"},
			Decls: []ast.Decl{
				&ast.FuncDecl{
					Name: &ast.Ident{Name: "f"},
					Type: &ast.FuncType{},
					Body: &ast.BlockStmt{
						List: []ast.Stmt{
							&ast.ExprStmt{
								X: &ast.SliceLit{
									Elts: []ast.Expr{&ast.Ident{Name: "item"}},
								},
							},
							&ast.ExprStmt{
								X: &ast.TupleLit{
									Elts: []ast.Expr{&ast.Ident{Name: "tupleItem"}},
								},
							},
						},
					},
				},
			},
		}
		return New([]*ast.File{f})
	}

	t.Run("Preorder", func(t *testing.T) {
		inspect := newInspector()

		var sawSliceLit bool
		var sawTupleLit bool
		var sawItem bool
		inspect.Preorder(nil, func(n ast.Node) {
			switch n := n.(type) {
			case *ast.SliceLit:
				sawSliceLit = true
			case *ast.TupleLit:
				sawTupleLit = true
			case *ast.Ident:
				if n.Name == "item" {
					sawItem = true
				}
			}
		})
		assert.True(t, sawSliceLit)
		assert.True(t, sawTupleLit)
		assert.True(t, sawItem)

		var filtered []ast.Node
		inspect.Preorder([]ast.Node{(*ast.SliceLit)(nil)}, func(n ast.Node) {
			filtered = append(filtered, n)
		})
		require.Len(t, filtered, 1)
		assert.IsType(t, (*ast.SliceLit)(nil), filtered[0])

		filtered = nil
		inspect.Preorder([]ast.Node{
			(*ast.Ident)(nil),
			(*ast.SliceLit)(nil),
		}, func(n ast.Node) {
			filtered = append(filtered, n)
		})
		assert.True(t, slices.ContainsFunc(filtered, func(n ast.Node) bool {
			_, ok := n.(*ast.SliceLit)
			return ok
		}))
		assert.False(t, slices.ContainsFunc(filtered, func(n ast.Node) bool {
			_, ok := n.(*ast.TupleLit)
			return ok
		}))
		assert.True(t, slices.ContainsFunc(filtered, func(n ast.Node) bool {
			ident, ok := n.(*ast.Ident)
			return ok && ident.Name == "item"
		}))
	})

	t.Run("Nodes", func(t *testing.T) {
		inspect := newInspector()

		var pushCount int
		var popCount int
		inspect.Nodes(nil, func(n ast.Node, push bool) bool {
			switch n.(type) {
			case *ast.SliceLit, *ast.TupleLit:
				if push {
					pushCount++
				} else {
					popCount++
				}
			}
			return true
		})
		assert.Equal(t, 2, pushCount)
		assert.Equal(t, 2, popCount)

		pushCount = 0
		popCount = 0
		inspect.Nodes([]ast.Node{(*ast.SliceLit)(nil)}, func(n ast.Node, push bool) bool {
			assert.IsType(t, (*ast.SliceLit)(nil), n)
			if push {
				pushCount++
			} else {
				popCount++
			}
			return true
		})
		assert.Equal(t, 1, pushCount)
		assert.Equal(t, 1, popCount)
	})

	t.Run("WithStack", func(t *testing.T) {
		inspect := newInspector()

		var pushCount int
		var popCount int
		inspect.WithStack(nil, func(n ast.Node, push bool, stack []ast.Node) bool {
			switch n.(type) {
			case *ast.SliceLit, *ast.TupleLit:
				require.NotEmpty(t, stack)
				assert.Equal(t, n, stack[len(stack)-1])
				if push {
					pushCount++
				} else {
					popCount++
				}
			}
			return true
		})
		assert.Equal(t, 2, pushCount)
		assert.Equal(t, 2, popCount)

		pushCount = 0
		popCount = 0
		inspect.WithStack([]ast.Node{(*ast.TupleLit)(nil)}, func(n ast.Node, push bool, stack []ast.Node) bool {
			assert.IsType(t, (*ast.TupleLit)(nil), n)
			require.NotEmpty(t, stack)
			assert.Equal(t, n, stack[len(stack)-1])
			if push {
				pushCount++
			} else {
				popCount++
			}
			return true
		})
		assert.Equal(t, 1, pushCount)
		assert.Equal(t, 1, popCount)
	})
}

func TestPreorderParsedXGoExtensionNodes(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.xgo", `
@retry
func fn() int {
	return 1
}

echo [
	fn()
	row...
]
`, parser.ParseComments)
	require.NoError(t, err)

	inspect := New([]*ast.File{f})

	var calls []string
	inspect.Preorder([]ast.Node{(*ast.CallExpr)(nil)}, func(n ast.Node) {
		call, ok := n.(*ast.CallExpr)
		require.True(t, ok)
		if ident, ok := call.Fun.(*ast.Ident); ok {
			calls = append(calls, ident.Name)
		}
	})
	assert.Contains(t, calls, "fn")

	var matrixLitCount int
	var elemEllipsisCount int
	var funcDecoratorCount int
	inspect.Preorder([]ast.Node{
		(*ast.MatrixLit)(nil),
		(*ast.ElemEllipsis)(nil),
		(*ast.FuncDecorator)(nil),
	}, func(n ast.Node) {
		switch n.(type) {
		case *ast.MatrixLit:
			matrixLitCount++
		case *ast.ElemEllipsis:
			elemEllipsisCount++
		case *ast.FuncDecorator:
			funcDecoratorCount++
		}
	})
	assert.Equal(t, 1, matrixLitCount)
	assert.Equal(t, 1, elemEllipsisCount)
	assert.Equal(t, 1, funcDecoratorCount)
}
