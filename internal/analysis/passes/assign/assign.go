package assign

import (
	_ "embed"
	"go/types"
	"strings"

	"github.com/goplus/xgo/ast"
	xgotoken "github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/passes/internal/analysisutil"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
)

//go:embed doc.go
var doc string

var Analyzer = &protocol.Analyzer{
	Name:     "assign",
	Doc:      analysisutil.MustExtractDoc(doc, "assign"),
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/assign",
	Requires: []*protocol.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *protocol.Pass) (any, error) {
	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.AssignStmt)(nil),
	}
	inspect.Preorder(nodeFilter, func(n ast.Node) {
		stmt := n.(*ast.AssignStmt)
		if stmt.Tok != xgotoken.ASSIGN {
			return // ignore :=
		}
		if len(stmt.Lhs) != len(stmt.Rhs) {
			return
		}

		var selfAssigned []string
		for i, lhs := range stmt.Lhs {
			rhs := stmt.Rhs[i]
			if !analysisutil.NoSideEffects(lhs) || !analysisutil.NoSideEffects(rhs) {
				continue
			}
			if isMapIndex(pass.TypesInfo, lhs) {
				continue
			}
			if sameExpr(astutil.Unparen(lhs), astutil.Unparen(rhs)) {
				selfAssigned = append(selfAssigned, analysisutil.PrintExpr(lhs))
			}
		}
		if len(selfAssigned) > 0 {
			pass.ReportRangef(stmt, "self-assignment of %s", strings.Join(selfAssigned, ", "))
		}
	})
	return nil, nil
}

// isMapIndex reports whether e is a map index expression (m[k]).
func isMapIndex(info *xgotypes.Info, e ast.Expr) bool {
	e = astutil.Unparen(e)
	idx, ok := e.(*ast.IndexExpr)
	if !ok {
		return false
	}
	tv, ok := info.Types[idx.X]
	if !ok || tv.Type == nil {
		return false
	}
	_, isMap := tv.Type.Underlying().(*types.Map)
	return isMap
}

// isMapType checks if the expression's type is a map.
func isMapType(info *xgotypes.Info, e ast.Expr) bool {
	tv, ok := info.Types[e]
	if !ok || tv.Type == nil {
		return false
	}
	// Use string check since we can't import go/types here without a cycle
	return strings.Contains(tv.Type.Underlying().String(), "map[")
}

// sameExpr reports whether two expressions are structurally identical.
func sameExpr(x, y ast.Expr) bool {
	x = astutil.Unparen(x)
	y = astutil.Unparen(y)
	switch x := x.(type) {
	case *ast.Ident:
		y, ok := y.(*ast.Ident)
		return ok && x.Name == y.Name
	case *ast.BasicLit:
		y, ok := y.(*ast.BasicLit)
		return ok && x.Kind == y.Kind && x.Value == y.Value
	case *ast.SelectorExpr:
		y, ok := y.(*ast.SelectorExpr)
		return ok && x.Sel.Name == y.Sel.Name && sameExpr(x.X, y.X)
	case *ast.IndexExpr:
		y, ok := y.(*ast.IndexExpr)
		return ok && sameExpr(x.X, y.X) && sameExpr(x.Index, y.Index)
	case *ast.StarExpr:
		y, ok := y.(*ast.StarExpr)
		return ok && sameExpr(x.X, y.X)
	case *ast.UnaryExpr:
		y, ok := y.(*ast.UnaryExpr)
		return ok && x.Op == y.Op && sameExpr(x.X, y.X)
	case *ast.BinaryExpr:
		y, ok := y.(*ast.BinaryExpr)
		return ok && x.Op == y.Op && sameExpr(x.X, y.X) && sameExpr(x.Y, y.Y)
	}
	return false
}
