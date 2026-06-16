package appends

import (
	_ "embed"
	gotypes "go/types"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/passes/internal/analysisutil"
	"github.com/goplus/xgolsw/internal/analysis/passes/internal/typeutil"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

//go:embed doc.go
var doc string

var Analyzer = &protocol.Analyzer{
	Name:     "appends",
	Doc:      analysisutil.MustExtractDoc(doc, "appends"),
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/appends",
	Requires: []*protocol.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *protocol.Pass) (any, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}
	inspect.Preorder(nodeFilter, func(n ast.Node) {
		call := n.(*ast.CallExpr)
		if isBuiltinAppend(typeutil.Callee(pass.TypesInfo, call)) && len(call.Args) == 1 {
			pass.ReportRangef(call, "append with no values")
		}
	})

	return nil, nil
}

// isBuiltinAppend reports whether obj identifies the built-in append function,
// including XGo's overloaded representation of it.
func isBuiltinAppend(obj gotypes.Object) bool {
	if obj == nil || obj.Name() != "append" || !xgoutil.IsInBuiltinPkg(obj) {
		return false
	}
	switch obj := obj.(type) {
	case *gotypes.Builtin:
		// XGo type checking can expose append as the standard Go builtin for
		// invalid calls.
		return true
	case *gogen.TemplateFunc:
		return true
	case *gotypes.Func:
		sig, ok := obj.Type().(*gotypes.Signature)
		if !ok {
			return false
		}
		_, overloads := gogen.CheckSigFuncExObjects(sig)
		for _, overload := range overloads {
			if tfn, ok := overload.(*gogen.TemplateFunc); ok &&
				tfn.Name() == "append" && xgoutil.IsInBuiltinPkg(tfn) {
				return true
			}
		}
	}
	return false
}
