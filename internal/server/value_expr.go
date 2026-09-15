package server

import (
	gotypes "go/types"
	"iter"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/types"
)

// valueExprTypes yields the target types of values in declarations,
// assignments, and returns. Nested expressions retain their own type context.
func valueExprTypes(astPkg *ast.Package, typeInfo *types.Info) iter.Seq2[ast.Expr, gotypes.Type] {
	return func(yield func(ast.Expr, gotypes.Type) bool) {
		if astPkg == nil || typeInfo == nil {
			return
		}
		more := true
		emit := func(expr ast.Expr, typ gotypes.Type) {
			if !more || expr == nil || typ == nil {
				return
			}
			// A multiple-result expression has no single target type.
			if _, ok := typeInfo.TypeOf(expr).(*gotypes.Tuple); ok {
				return
			}
			more = yield(expr, typ)
		}
		var inspect func(ast.Node, *gotypes.Signature)
		inspect = func(root ast.Node, sig *gotypes.Signature) {
			ast.Inspect(root, func(node ast.Node) bool {
				if !more {
					return false
				}
				switch node := node.(type) {
				case *ast.FuncDecl:
					if node.Body != nil {
						var funcSig *gotypes.Signature
						if fun, _ := typeInfo.ObjectOf(node.Name).(*gotypes.Func); fun != nil {
							funcSig = fun.Signature()
						}
						inspect(node.Body, funcSig)
					}
					return false
				case *ast.LambdaExpr:
					if node.Body != nil {
						funcSig, _ := typeInfo.TypeOf(node).(*gotypes.Signature)
						inspect(node.Body, funcSig)
					}
					return false
				case *ast.FuncLit:
					if node.Body != nil {
						funcSig, _ := typeInfo.TypeOf(node).(*gotypes.Signature)
						inspect(node.Body, funcSig)
					}
					return false
				case *ast.ValueSpec:
					for i := range min(len(node.Values), len(node.Names)) {
						emit(node.Values[i], typeInfo.TypeOf(node.Names[i]))
					}
				case *ast.AssignStmt:
					if node.Tok != token.ASSIGN && node.Tok != token.DEFINE {
						return true
					}
					for i := range min(len(node.Rhs), len(node.Lhs)) {
						emit(node.Rhs[i], typeInfo.TypeOf(node.Lhs[i]))
					}
				case *ast.ReturnStmt:
					if sig == nil {
						return true
					}
					for i := range min(len(node.Results), sig.Results().Len()) {
						emit(node.Results[i], sig.Results().At(i).Type())
					}
				}
				return more
			})
		}
		for _, file := range astPkg.Files {
			inspect(file, nil)
			if !more {
				return
			}
		}
	}
}
