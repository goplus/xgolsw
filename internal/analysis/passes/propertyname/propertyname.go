package propertyname

import (
	_ "embed"
	"go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/passes/internal/analysisutil"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

//go:embed doc.go
var doc string

var Analyzer = &protocol.Analyzer{
	Name:     "propertyname",
	Doc:      analysisutil.MustExtractDoc(doc, "propertyname"),
	URL:      "https://pkg.go.dev/github.com/goplus/xgolsw/internal/analysis/passes/propertyname",
	Requires: []*protocol.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *protocol.Pass) (any, error) {
	if pass.IsPropertyNameType == nil || pass.GetPropertyNamesForCall == nil {
		return nil, nil
	}

	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}

	inspect.Preorder(nodeFilter, func(n ast.Node) {
		call := n.(*ast.CallExpr)

		xgoutil.WalkCallExprArgs(pass.TypesInfo, call,
			func(fun *types.Func, params *types.Tuple, paramIndex int, arg ast.Expr, argIndex int) bool {
				param := params.At(paramIndex)
				if !pass.IsPropertyNameType(param.Type()) {
					return true
				}

				// Only validate string literal / constant arguments.
				tv := pass.TypesInfo.Types[arg]
				propName, ok := xgoutil.StringLitOrConstValue(arg, tv)
				if !ok {
					return true
				}

				validNames := pass.GetPropertyNamesForCall(call)
				for _, name := range validNames {
					if name == propName {
						return true
					}
				}

				pass.ReportRangef(arg, "unknown property %q", propName)
				return true
			})
	})

	return nil, nil
}
