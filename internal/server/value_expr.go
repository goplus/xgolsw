package server

import (
	gotypes "go/types"
	"iter"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// valueExprTypes yields contextual types for complete values throughout a
// package. Arithmetic fragments are excluded from resource name matching.
func valueExprTypes(astPkg *ast.Package, typeInfo *types.Info) iter.Seq2[ast.Expr, gotypes.Type] {
	return func(yield func(ast.Expr, gotypes.Type) bool) {
		if astPkg == nil || typeInfo == nil {
			return
		}
		more := true
		for _, file := range astPkg.Files {
			var stack []ast.Node
			path := func() []ast.Node {
				result := slices.Clone(stack)
				slices.Reverse(result)
				return result
			}
			ast.Inspect(file, func(node ast.Node) bool {
				if node == nil {
					stack = stack[:len(stack)-1]
					return false
				}
				if !more {
					return false
				}
				stack = append(stack, node)
				// A compound assignment changes part of a value rather than
				// naming a resource of the destination type.
				if assign, ok := node.(*ast.AssignStmt); ok && assign.Tok != token.ASSIGN && assign.Tok != token.DEFINE {
					return true
				}
				// Comparisons name complete values. Arithmetic operands can be
				// fragments of a resource name, such as a concatenated suffix.
				if binary, ok := node.(*ast.BinaryExpr); ok && binary.Op != token.EQL && binary.Op != token.NEQ {
					return true
				}
				switch node.(type) {
				case *ast.CompositeLit, *ast.TupleLit, *ast.SliceLit, *ast.MatrixLit:
				default:
					if len(valueOperands(typeInfo, node)) == 0 {
						return true
					}
				}
				for expr, typ := range contextualValueTypes(typeInfo, path()) {
					if !xgoutil.IsValidType(typ) {
						continue
					}
					// Literals supply their own element types when visited. A
					// multiple-result expression has no single target type.
					switch astutil.Unparen(expr).(type) {
					case *ast.CompositeLit, *ast.TupleLit, *ast.SliceLit, *ast.MatrixLit:
						continue
					}
					if _, ok := typ.(*gotypes.Tuple); ok {
						continue
					}
					if more = yield(expr, typ); !more {
						break
					}
				}
				return true
			})
			if !more {
				return
			}
		}
	}
}
