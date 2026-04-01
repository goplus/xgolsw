package unusedresult

import (
	_ "embed"
	"go/types"

	"github.com/goplus/xgo/ast"
	xgotoken "github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/passes/internal/analysisutil"
	"github.com/goplus/xgolsw/internal/analysis/passes/internal/typeutil"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
)

//go:embed doc.go
var doc string

var Analyzer = &protocol.Analyzer{
	Name:     "unusedresult",
	Doc:      analysisutil.MustExtractDoc(doc, "unusedresult"),
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/unusedresult",
	Requires: []*protocol.Analyzer{inspect.Analyzer},
	Run:      run,
}

// mustUseFuncs is the set of package-level functions whose results must be used.
var mustUseFuncs = map[[2]string]bool{
	{"errors", "New"}:      true,
	{"fmt", "Errorf"}:      true,
	{"fmt", "Sprint"}:      true,
	{"fmt", "Sprintf"}:     true,
	{"fmt", "Sprintln"}:    true,
	{"fmt", "Append"}:      true,
	{"fmt", "Appendf"}:     true,
	{"fmt", "Appendln"}:    true,
	{"sort", "Reverse"}:    true,
	{"strings", "Replace"}: true,
	{"strings", "Title"}:   true,
	{"bytes", "Replace"}:   true,
}

// mustUseStringMethods is the set of method names of type func() string whose results must be used.
var mustUseStringMethods = map[string]bool{
	"Error":  true,
	"String": true,
}

// sigNoArgsStringResult is the signature func() string.
var sigNoArgsStringResult = types.NewSignatureType(nil, nil, nil, nil,
	types.NewTuple(types.NewParam(xgotoken.NoPos, nil, "", types.Typ[types.String])),
	false)

func run(pass *protocol.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.ExprStmt)(nil),
	}
	insp.Preorder(nodeFilter, func(n ast.Node) {
		call, ok := astutil.Unparen(n.(*ast.ExprStmt).X).(*ast.CallExpr)
		if !ok {
			return // not a call statement
		}

		fn, ok := typeutil.Callee(pass.TypesInfo, call).(*types.Func)
		if !ok {
			return // builtin or non-function
		}

		sig := fn.Type().(*types.Signature)
		if sig.Recv() != nil {
			// method call
			if types.Identical(sig, sigNoArgsStringResult) {
				if mustUseStringMethods[fn.Name()] {
					pass.ReportRangef(call, "result of (%s).%s call not used",
						sig.Recv().Type(), fn.Name())
				}
			}
		} else {
			// package-level function
			pkg := fn.Pkg()
			if pkg == nil {
				return
			}
			key := [2]string{pkg.Path(), fn.Name()}
			if mustUseFuncs[key] {
				pass.ReportRangef(call, "result of %s.%s call not used",
					pkg.Path(), fn.Name())
			}
		}
	})
	return nil, nil
}
