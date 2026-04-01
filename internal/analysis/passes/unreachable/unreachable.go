package unreachable

import (
	_ "embed"

	"github.com/goplus/xgo/ast"
	xgotoken "github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/passes/internal/analysisutil"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
)

//go:embed doc.go
var doc string

var Analyzer = &protocol.Analyzer{
	Name:             "unreachable",
	Doc:              analysisutil.MustExtractDoc(doc, "unreachable"),
	URL:              "https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/unreachable",
	Requires:         []*protocol.Analyzer{inspect.Analyzer},
	RunDespiteErrors: true,
	Run:              run,
}

func run(pass *protocol.Pass) (any, error) {
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)

	nodeFilter := []ast.Node{
		(*ast.FuncDecl)(nil),
		(*ast.FuncLit)(nil),
	}
	insp.Preorder(nodeFilter, func(n ast.Node) {
		var body *ast.BlockStmt
		switch n := n.(type) {
		case *ast.FuncDecl:
			body = n.Body
		case *ast.FuncLit:
			body = n.Body
		}
		if body == nil {
			return
		}
		d := &deadState{
			pass:     pass,
			hasBreak: make(map[ast.Stmt]bool),
			hasGoto:  make(map[string]bool),
			labels:   make(map[string]ast.Stmt),
		}
		d.findLabels(body)
		d.reachable = true
		d.findDead(body)
	})
	return nil, nil
}

type deadState struct {
	pass        *protocol.Pass
	hasBreak    map[ast.Stmt]bool
	hasGoto     map[string]bool
	labels      map[string]ast.Stmt
	breakTarget ast.Stmt

	reachable bool
}

func (d *deadState) findLabels(stmt ast.Stmt) {
	switch x := stmt.(type) {
	default:
		// leaf statement, no sub-statements

	case *ast.AssignStmt, *ast.BadStmt, *ast.DeclStmt, *ast.DeferStmt,
		*ast.EmptyStmt, *ast.ExprStmt, *ast.GoStmt, *ast.IncDecStmt,
		*ast.ReturnStmt, *ast.SendStmt:
		// no statements inside

	case *ast.BlockStmt:
		for _, s := range x.List {
			d.findLabels(s)
		}

	case *ast.BranchStmt:
		switch x.Tok {
		case xgotoken.GOTO:
			if x.Label != nil {
				d.hasGoto[x.Label.Name] = true
			}
		case xgotoken.BREAK:
			target := d.breakTarget
			if x.Label != nil {
				target = d.labels[x.Label.Name]
			}
			if target != nil {
				d.hasBreak[target] = true
			}
		}

	case *ast.IfStmt:
		d.findLabels(x.Body)
		if x.Else != nil {
			d.findLabels(x.Else)
		}

	case *ast.LabeledStmt:
		d.labels[x.Label.Name] = x.Stmt
		d.findLabels(x.Stmt)

	case *ast.ForStmt:
		outer := d.breakTarget
		d.breakTarget = x
		d.findLabels(x.Body)
		d.breakTarget = outer

	case *ast.RangeStmt:
		outer := d.breakTarget
		d.breakTarget = x
		d.findLabels(x.Body)
		d.breakTarget = outer

	case *ast.SelectStmt:
		outer := d.breakTarget
		d.breakTarget = x
		d.findLabels(x.Body)
		d.breakTarget = outer

	case *ast.SwitchStmt:
		outer := d.breakTarget
		d.breakTarget = x
		d.findLabels(x.Body)
		d.breakTarget = outer

	case *ast.TypeSwitchStmt:
		outer := d.breakTarget
		d.breakTarget = x
		d.findLabels(x.Body)
		d.breakTarget = outer

	case *ast.CommClause:
		for _, s := range x.Body {
			d.findLabels(s)
		}

	case *ast.CaseClause:
		for _, s := range x.Body {
			d.findLabels(s)
		}
	}
}

func (d *deadState) findDead(stmt ast.Stmt) {
	// If this is a goto target, assume reachable.
	if x, isLabel := stmt.(*ast.LabeledStmt); isLabel && d.hasGoto[x.Label.Name] {
		d.reachable = true
	}

	if !d.reachable {
		switch stmt.(type) {
		case *ast.EmptyStmt:
			// do not warn about unreachable empty statements
		default:
			d.pass.ReportRangef(stmt, "unreachable code")
			d.reachable = true // silence error about next statement
		}
	}

	switch x := stmt.(type) {
	default:
		// no control flow change

	case *ast.AssignStmt, *ast.BadStmt, *ast.DeclStmt, *ast.DeferStmt,
		*ast.EmptyStmt, *ast.GoStmt, *ast.IncDecStmt, *ast.SendStmt:
		// no control flow

	case *ast.BlockStmt:
		for _, s := range x.List {
			d.findDead(s)
		}

	case *ast.BranchStmt:
		switch x.Tok {
		case xgotoken.BREAK, xgotoken.GOTO, xgotoken.FALLTHROUGH, xgotoken.CONTINUE:
			d.reachable = false
		}

	case *ast.ExprStmt:
		// Call to panic?
		call, ok := x.X.(*ast.CallExpr)
		if ok {
			if name, ok := call.Fun.(*ast.Ident); ok && name.Name == "panic" && name.Obj == nil {
				d.reachable = false
			}
		}

	case *ast.ForStmt:
		d.findDead(x.Body)
		d.reachable = x.Cond != nil || d.hasBreak[x]

	case *ast.IfStmt:
		d.findDead(x.Body)
		if x.Else != nil {
			r := d.reachable
			d.reachable = true
			d.findDead(x.Else)
			d.reachable = d.reachable || r
		} else {
			d.reachable = true
		}

	case *ast.LabeledStmt:
		d.findDead(x.Stmt)

	case *ast.RangeStmt:
		d.findDead(x.Body)
		d.reachable = true

	case *ast.ReturnStmt:
		d.reachable = false

	case *ast.SelectStmt:
		anyReachable := false
		for _, comm := range x.Body.List {
			d.reachable = true
			for _, s := range comm.(*ast.CommClause).Body {
				d.findDead(s)
			}
			anyReachable = anyReachable || d.reachable
		}
		d.reachable = anyReachable || d.hasBreak[x]

	case *ast.SwitchStmt:
		anyReachable := false
		hasDefault := false
		for _, cas := range x.Body.List {
			cc := cas.(*ast.CaseClause)
			if cc.List == nil {
				hasDefault = true
			}
			d.reachable = true
			for _, s := range cc.Body {
				d.findDead(s)
			}
			anyReachable = anyReachable || d.reachable
		}
		d.reachable = anyReachable || d.hasBreak[x] || !hasDefault

	case *ast.TypeSwitchStmt:
		anyReachable := false
		hasDefault := false
		for _, cas := range x.Body.List {
			cc := cas.(*ast.CaseClause)
			if cc.List == nil {
				hasDefault = true
			}
			d.reachable = true
			for _, s := range cc.Body {
				d.findDead(s)
			}
			anyReachable = anyReachable || d.reachable
		}
		d.reachable = anyReachable || d.hasBreak[x] || !hasDefault
	}
}
