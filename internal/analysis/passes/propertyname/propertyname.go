package propertyname

import (
	_ "embed"
	"slices"

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
	inspect.Preorder([]ast.Node{
		(*ast.CallExpr)(nil),
		(*ast.FuncDecorator)(nil),
	}, func(n ast.Node) {
		var call *ast.CallExpr
		switch n := n.(type) {
		case *ast.CallExpr:
			call = n
		case *ast.FuncDecorator:
			call = &n.CallExpr
		}

		isStringValue := func(expr ast.Expr) bool {
			_, ok := xgoutil.StringLitOrConstValue(expr, pass.TypesInfo.Types[expr])
			return ok
		}
		hasStringValueArg := slices.ContainsFunc(call.Args, isStringValue) ||
			slices.ContainsFunc(call.Kwargs, func(kwarg *ast.KwargExpr) bool {
				return isStringValue(kwarg.Value)
			})
		if !hasStringValueArg {
			return
		}

		resolvedCallExprArgs := xgoutil.ResolvedCallExprArgs(pass.TypesInfo, call)
		if pass.ResolvedCallExprArgs != nil {
			resolvedCallExprArgs = pass.ResolvedCallExprArgs(call)
		}
		type propertyArg struct {
			expr ast.Expr
			name string
		}
		var propertyArgs []propertyArg
		for resolvedArg := range resolvedCallExprArgs {
			if resolvedArg.ExpectedType == nil || !pass.IsPropertyNameType(resolvedArg.ExpectedType) {
				continue
			}

			// Only validate string literal / constant arguments.
			propName, ok := xgoutil.StringLitOrConstValue(resolvedArg.Arg, pass.TypesInfo.Types[resolvedArg.Arg])
			if !ok {
				continue
			}
			propertyArgs = append(propertyArgs, propertyArg{
				expr: resolvedArg.Arg,
				name: propName,
			})
		}
		if len(propertyArgs) == 0 {
			return
		}

		validNames := pass.GetPropertyNamesForCall(call)
		if validNames == nil {
			return
		}
		for _, arg := range propertyArgs {
			if _, ok := validNames[arg.name]; !ok {
				pass.ReportRangef(arg.expr, "unknown property %q", arg.name)
			}
		}
	})

	return nil, nil
}
