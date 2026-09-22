package server

import (
	"go/constant"
	gotypes "go/types"
	"iter"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// resourceValue describes a string value and the type supplied by its context.
// Intrinsic values have no recognized resource type from an assignment or call.
type resourceValue struct {
	Expr      ast.Expr
	Type      gotypes.Type
	Name      string
	Call      *ast.CallExpr
	Intrinsic bool
}

// resourceReferences resolves literal and constant references throughout project
// source. The resolver's boolean reports a recognized resource type even when
// its context cannot produce an ID, preventing unrelated intrinsic-type matches.
func resourceReferences(proj *xgo.Project, resolve func(resourceValue) (resourceID, bool)) iter.Seq[resourceRef] {
	return func(yield func(resourceRef) bool) {
		info, _ := expressionTypeInfo(proj)
		if info == nil {
			return
		}
		astPkg, _ := proj.ASTPackage()
		contextual := make(map[ast.Expr]struct{})
		inspect := func(value resourceValue) bool {
			value.Expr = astutil.Unparen(value.Expr)
			expr := value.Expr
			literal, converted := resourceStringLiteral(expr, info)
			if literal != nil {
				expr = literal
			}
			name, ok := xgoutil.StringLitOrConstValue(expr, info.Types[expr])
			if !ok {
				return true
			}
			value.Name = name
			id, recognized := resolve(value)
			if recognized {
				contextual[value.Expr] = struct{}{}
				for _, expr := range converted {
					contextual[expr] = struct{}{}
				}
			}
			if id == nil {
				return true
			}
			kind := XGoResourceRefKindStringLiteral
			if _, ok := value.Expr.(*ast.Ident); ok {
				kind = XGoResourceRefKindConstantReference
			}
			return yield(resourceRef{ID: id, Kind: kind, Node: value.Expr})
		}
		var conversions []*ast.CallExpr
		// Invalid calls may be absent from info.Types, so inspect the source AST.
		if astPkg != nil {
			for _, file := range astPkg.Files {
				stopped := false
				ast.Inspect(file, func(node ast.Node) bool {
					if stopped {
						return false
					}
					call := callExprFromNode(info, node)
					if call == nil {
						return true
					}
					if literal, _ := resourceStringLiteral(call, info); literal != nil {
						conversions = append(conversions, call)
						return true
					}
					for expr, typ := range callArgValueTypes(info, call) {
						if !inspect(resourceValue{Expr: expr, Type: typ, Call: call}) {
							stopped = true
							return false
						}
					}
					return true
				})
				if stopped {
					return
				}
			}
		}

		// Literal elements can also be call arguments. Keep the call's resource
		// scope, including recognized types whose resource is unavailable.
		for expr, typ := range valueExprTypes(astPkg, info) {
			if _, seen := contextual[astutil.Unparen(expr)]; seen {
				continue
			}
			if !inspect(resourceValue{Expr: expr, Type: xgoutil.DerefType(typ)}) {
				return
			}
		}
		for _, call := range conversions {
			if _, seen := contextual[call]; !seen && !inspect(resourceValue{Expr: call, Type: info.TypeOf(call), Intrinsic: true}) {
				return
			}
		}

		for expr, tv := range info.Types {
			if expr == nil || !expr.Pos().IsValid() || tv.IsType() || tv.Type == nil {
				continue
			}
			if _, ok := contextual[expr]; ok {
				continue
			}
			switch expr.(type) {
			case *ast.BasicLit, *ast.Ident:
				if !inspect(resourceValue{Expr: expr, Type: xgoutil.DerefType(tv.Type), Intrinsic: true}) {
					return
				}
			}
		}
	}
}

// resourceStringLiteral finds a literal through single-argument calls that the
// compiler evaluates to constant strings, including type conversions. The
// returned nodes retain the expressions whose intrinsic types the reference supersedes.
// Conversions of identifiers or numeric values are not literal references.
func resourceStringLiteral(expr ast.Expr, info *types.Info) (*ast.BasicLit, []ast.Expr) {
	var converted []ast.Expr
	for {
		expr = astutil.Unparen(expr)
		converted = append(converted, expr)
		switch node := expr.(type) {
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				return node, converted
			}
		case *ast.CallExpr:
			value := info.Types[node].Value
			if len(node.Args) == 1 && value != nil && value.Kind() == constant.String {
				expr = node.Args[0]
				continue
			}
		}
		return nil, nil
	}
}
