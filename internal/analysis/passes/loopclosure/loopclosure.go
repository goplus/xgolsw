package loopclosure

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
	Name:     "loopclosure",
	Doc:      analysisutil.MustExtractDoc(doc, "loopclosure"),
	URL:      "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/loopclosure",
	Requires: []*protocol.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *protocol.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.RangeStmt)(nil),
		(*ast.ForStmt)(nil),
	}
	insp.Nodes(nodeFilter, func(n ast.Node, push bool) bool {
		if !push {
			return true
		}

		var vars []types.Object
		addVar := func(expr ast.Expr) {
			if id, _ := expr.(*ast.Ident); id != nil {
				if obj := pass.TypesInfo.ObjectOf(id); obj != nil {
					vars = append(vars, obj)
				}
			}
		}

		var body *ast.BlockStmt
		switch n := n.(type) {
		case *ast.RangeStmt:
			body = n.Body
			addVar(n.Key)
			addVar(n.Value)
		case *ast.ForStmt:
			body = n.Body
			switch post := n.Post.(type) {
			case *ast.AssignStmt:
				for _, lhs := range post.Lhs {
					addVar(lhs)
				}
			case *ast.IncDecStmt:
				addVar(post.X)
			}
		}
		if len(vars) == 0 {
			return true
		}

		forEachLastStmt(body.List, func(last ast.Stmt) {
			var stmts []ast.Stmt
			switch s := last.(type) {
			case *ast.GoStmt:
				stmts = litStmts(s.Call.Fun)
			case *ast.DeferStmt:
				stmts = litStmts(s.Call.Fun)
			}
			for _, stmt := range stmts {
				reportCaptured(pass, vars, stmt)
			}
		})
		return true
	})
	return nil, nil
}

// reportCaptured reports a diagnostic if checkStmt contains references to vars.
func reportCaptured(pass *protocol.Pass, vars []types.Object, checkStmt ast.Stmt) {
	ast.Inspect(checkStmt, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		obj := pass.TypesInfo.Uses[id]
		if obj == nil {
			return true
		}
		for _, v := range vars {
			if v == obj {
				pass.ReportRangef(id, "loop variable %s captured by func literal", id.Name)
			}
		}
		return true
	})
}

// forEachLastStmt calls onLast on each "last" statement in a list of statements,
// expanding compound statements recursively.
func forEachLastStmt(stmts []ast.Stmt, onLast func(last ast.Stmt)) {
	if len(stmts) == 0 {
		return
	}

	s := stmts[len(stmts)-1]
	switch s := s.(type) {
	case *ast.IfStmt:
	loop:
		for {
			forEachLastStmt(s.Body.List, onLast)
			switch e := s.Else.(type) {
			case *ast.BlockStmt:
				forEachLastStmt(e.List, onLast)
				break loop
			case *ast.IfStmt:
				s = e
			case nil:
				break loop
			}
		}
	case *ast.ForStmt:
		forEachLastStmt(s.Body.List, onLast)
	case *ast.RangeStmt:
		forEachLastStmt(s.Body.List, onLast)
	case *ast.SwitchStmt:
		for _, c := range s.Body.List {
			cc := c.(*ast.CaseClause)
			forEachLastStmt(cc.Body, onLast)
		}
	case *ast.TypeSwitchStmt:
		for _, c := range s.Body.List {
			cc := c.(*ast.CaseClause)
			forEachLastStmt(cc.Body, onLast)
		}
	case *ast.SelectStmt:
		for _, c := range s.Body.List {
			cc := c.(*ast.CommClause)
			forEachLastStmt(cc.Body, onLast)
		}
	default:
		onLast(s)
	}
}

// litStmts returns the body statements of a function literal expression.
func litStmts(fun ast.Expr) []ast.Stmt {
	lit, _ := fun.(*ast.FuncLit)
	if lit == nil {
		return nil
	}
	return lit.Body.List
}
