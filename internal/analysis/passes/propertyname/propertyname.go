package propertyname

import (
	_ "embed"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/passes/internal/analysisutil"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// doc contains the analyzer documentation.
//
//go:embed doc.go
var doc string

// Analyzer reports invalid property-name arguments.
var Analyzer = &protocol.Analyzer{
	Name:     "propertyname",
	Doc:      analysisutil.MustExtractDoc(doc, "propertyname"),
	URL:      "https://pkg.go.dev/github.com/goplus/xgolsw/internal/analysis/passes/propertyname",
	Requires: []*protocol.Analyzer{inspect.Analyzer},
	Run:      run,
}

// run reports property-name arguments that do not match the call target.
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

		validNames := pass.GetPropertyNamesForCall(call)
		if validNames == nil {
			return
		}
		validNamesSet := make(map[string]struct{}, len(validNames))
		for _, name := range validNames {
			validNamesSet[name] = struct{}{}
		}

		resolvedCallExprArgs := xgoutil.ResolvedCallExprArgs(pass.TypesInfo, call)
		if pass.ResolvedCallExprArgs != nil {
			resolvedCallExprArgs = pass.ResolvedCallExprArgs(call)
		}
		for resolvedArg := range resolvedCallExprArgs {
			if resolvedArg.ExpectedType == nil {
				continue
			}
			if !pass.IsPropertyNameType(resolvedArg.ExpectedType) {
				continue
			}

			// Only validate string literal / constant arguments.
			tv := pass.TypesInfo.Types[resolvedArg.Arg]
			propName, ok := xgoutil.StringLitOrConstValue(resolvedArg.Arg, tv)
			if !ok {
				continue
			}

			if _, ok := validNamesSet[propName]; !ok {
				pass.ReportRangef(resolvedArg.Arg, "unknown property %q", propName)
			}
		}
	})

	return nil, nil
}
