package stringintconv

import (
	_ "embed"
	"go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/passes/internal/analysisutil"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
)

//go:embed doc.go
var doc string

var Analyzer = &protocol.Analyzer{
	Name:     "stringintconv",
	Doc:      analysisutil.MustExtractDoc(doc, "stringintconv"),
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/stringintconv",
	Requires: []*protocol.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *protocol.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.CallExpr)(nil),
	}
	insp.Preorder(nodeFilter, func(n ast.Node) {
		call := n.(*ast.CallExpr)

		// A type conversion has exactly one argument.
		if len(call.Args) != 1 {
			return
		}

		// The function (i.e., the type being converted to) must be a type.
		funType := pass.TypesInfo.Types[call.Fun]
		if !funType.IsType() {
			return
		}

		// The target type must be string.
		if !types.Identical(funType.Type, types.Typ[types.String]) {
			return
		}

		// The argument type must be an integer type, but not byte or rune.
		arg := call.Args[0]
		argType := pass.TypesInfo.Types[arg].Type
		if argType == nil {
			return
		}

		// Unwrap named types to get the underlying basic type.
		basic, ok := argType.Underlying().(*types.Basic)
		if !ok {
			return
		}

		info := basic.Info()
		if info&types.IsInteger == 0 {
			return
		}

		// byte (uint8) and rune (int32) are intentional: skip them.
		kind := basic.Kind()
		if kind == types.Byte || kind == types.Rune {
			return
		}

		// Also skip if the named type is literally "byte" or "rune".
		if named, ok := argType.(*types.Named); ok {
			name := named.Obj().Name()
			if name == "byte" || name == "rune" {
				return
			}
		}

		pass.ReportRangef(call,
			"conversion from %s to string yields a string of one rune, not a string of digits (did you mean fmt.Sprint(x)?)",
			argType)
	})
	return nil, nil
}
