package inspector

import (
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreorderXGoExtensionNodes(t *testing.T) {
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

	inspect := New([]*ast.File{f})

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
	for _, n := range filtered {
		assert.IsType(t, (*ast.SliceLit)(nil), n)
	}
}

func TestPreorderMatrixLit(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.xgo", `
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
	inspect.Preorder([]ast.Node{
		(*ast.MatrixLit)(nil),
		(*ast.ElemEllipsis)(nil),
	}, func(n ast.Node) {
		switch n.(type) {
		case *ast.MatrixLit:
			matrixLitCount++
		case *ast.ElemEllipsis:
			elemEllipsisCount++
		}
	})
	assert.Equal(t, 1, matrixLitCount)
	assert.Equal(t, 1, elemEllipsisCount)
}
