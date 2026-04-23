package bools

import (
	_ "embed"

	"github.com/goplus/xgo/ast"
	xgotoken "github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/passes/internal/analysisutil"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
)

//go:embed doc.go
var doc string

var Analyzer = &protocol.Analyzer{
	Name:     "bools",
	Doc:      analysisutil.MustExtractDoc(doc, "bools"),
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/bools",
	Requires: []*protocol.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *protocol.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.BinaryExpr)(nil),
	}
	seen := make(map[*ast.BinaryExpr]bool)
	insp.Preorder(nodeFilter, func(n ast.Node) {
		e := n.(*ast.BinaryExpr)
		if seen[e] {
			return
		}

		var op boolOp
		switch e.Op {
		case xgotoken.LOR:
			op = or
		case xgotoken.LAND:
			op = and
		default:
			return
		}

		comm := op.commutativeSets(pass, e, seen)
		for _, exprs := range comm {
			op.checkRedundant(pass, exprs)
			op.checkSuspect(pass, exprs)
		}
	})
	return nil, nil
}

type boolOp struct {
	name  string
	tok   xgotoken.Token // token corresponding to this operator
	badEq xgotoken.Token // equality operator that should not be used with this operator
}

var (
	or  = boolOp{"or", xgotoken.LOR, xgotoken.NEQ}
	and = boolOp{"and", xgotoken.LAND, xgotoken.EQL}
)

// commutativeSets returns all side-effect-free sets of expressions in e
// connected by op. It adds expanded BinaryExprs to seen.
func (op boolOp) commutativeSets(pass *protocol.Pass, e *ast.BinaryExpr, seen map[*ast.BinaryExpr]bool) [][]ast.Expr {
	exprs := op.split(e, seen)

	i := 0
	var sets [][]ast.Expr
	for j := 0; j <= len(exprs); j++ {
		if j == len(exprs) || !analysisutil.NoSideEffects(exprs[j]) {
			if i < j {
				sets = append(sets, exprs[i:j])
			}
			i = j + 1
		}
	}
	return sets
}

// checkRedundant checks for expressions of the form e && e or e || e.
func (op boolOp) checkRedundant(pass *protocol.Pass, exprs []ast.Expr) {
	seen := make(map[string]bool)
	for _, e := range exprs {
		efmt := analysisutil.PrintExpr(e)
		if seen[efmt] {
			pass.ReportRangef(e, "redundant %s: %s %s %s", op.name, efmt, op.tok, efmt)
		} else {
			seen[efmt] = true
		}
	}
}

// checkSuspect checks for expressions of the form:
//
//	x != c1 || x != c2  (always true when c1 != c2)
//	x == c1 && x == c2  (always false when c1 != c2)
func (op boolOp) checkSuspect(pass *protocol.Pass, exprs []ast.Expr) {
	seen := make(map[string]string)

	for _, e := range exprs {
		bin, ok := e.(*ast.BinaryExpr)
		if !ok || bin.Op != op.badEq {
			continue
		}

		// One operand should be constant.
		var x ast.Expr
		if pass.TypesInfo.Types[bin.Y].Value != nil {
			x = bin.X
		} else if pass.TypesInfo.Types[bin.X].Value != nil {
			x = bin.Y
		} else {
			continue
		}

		xfmt := analysisutil.PrintExpr(x)
		efmt := analysisutil.PrintExpr(e)
		if prev, found := seen[xfmt]; found {
			if efmt != prev {
				pass.ReportRangef(e, "suspect %s: %s %s %s", op.name, efmt, op.tok, prev)
			}
		} else {
			seen[xfmt] = efmt
		}
	}
}

// split returns a slice of all subexpressions in e connected by op.
// For example, given 'a || (b || c) || d' with the or op,
// split returns {d, c, b, a}.
func (op boolOp) split(e ast.Expr, seen map[*ast.BinaryExpr]bool) (exprs []ast.Expr) {
	for {
		e = astutil.Unparen(e)
		if b, ok := e.(*ast.BinaryExpr); ok && b.Op == op.tok {
			seen[b] = true
			exprs = append(exprs, op.split(b.Y, seen)...)
			e = b.X
		} else {
			exprs = append(exprs, e)
			break
		}
	}
	return
}
