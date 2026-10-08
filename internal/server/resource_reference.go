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
	Static    bool
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
			if _, seen := contextual[value.Expr]; seen {
				return true
			}
			value.Name, value.Static = resourceStringValue(value.Expr, info)
			id, recognized := resolve(value)
			if recognized {
				for expr := range resourceExpressionParts(value.Expr, info) {
					contextual[expr] = struct{}{}
				}
			}
			if id == nil || !value.Static {
				return true
			}
			kind := XGoResourceRefKindStringExpression
			switch resourceStringOperand(value.Expr, info).(type) {
			case *ast.BasicLit:
				kind = XGoResourceRefKindStringLiteral
			case *ast.Ident, *ast.SelectorExpr:
				kind = XGoResourceRefKindConstantReference
			}
			return yield(resourceRef{ID: id, Kind: kind, Node: value.Expr})
		}
		var intrinsic []ast.Expr
		// Invalid calls may be absent from info.Types, so inspect the source AST.
		if astPkg != nil {
			for _, file := range astPkg.Files {
				stopped := false
				ast.Inspect(file, func(node ast.Node) bool {
					if stopped {
						return false
					}
					if expr, ok := node.(ast.Expr); ok {
						intrinsic = append(intrinsic, expr)
					}
					if selector, ok := node.(*ast.SelectorExpr); ok {
						contextual[selector.Sel] = struct{}{}
					}
					call := callExprFromNode(info, node)
					if call == nil || resourceStringOperand(call, info) != call {
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
			if !inspect(resourceValue{Expr: expr, Type: xgoutil.DerefType(typ)}) {
				return
			}
		}
		// Source order visits complete expressions before their components.
		// Map iteration could otherwise resolve a converted identifier first.
		for _, expr := range intrinsic {
			switch expr.(type) {
			case *ast.BasicLit, *ast.Ident, *ast.SelectorExpr:
			case *ast.CallExpr:
				if resourceStringOperand(expr, info) == expr {
					continue
				}
			default:
				continue
			}
			tv := info.Types[expr]
			typ := resourceExpressionType(expr, info)
			if !expr.Pos().IsValid() || tv.IsType() || typ == nil {
				continue
			}
			if !inspect(resourceValue{Expr: expr, Type: xgoutil.DerefType(typ), Intrinsic: true}) {
				return
			}
		}
	}
}

// resourceExpressionType retains declaration identity when contextual typing
// replaces an alias constant or conversion with its underlying string type.
func resourceExpressionType(expr ast.Expr, info *types.Info) gotypes.Type {
	reference := astutil.Unparen(expr)
	call, conversion := reference.(*ast.CallExpr)
	if conversion {
		if resourceStringOperand(call, info) == call {
			return info.Types[expr].Type
		}
		reference = astutil.Unparen(call.Fun)
	}
	var ident *ast.Ident
	switch reference := reference.(type) {
	case *ast.Ident:
		ident = reference
	case *ast.SelectorExpr:
		ident = reference.Sel
	}
	if ident != nil {
		switch obj := info.Uses[ident].(type) {
		case *gotypes.Const:
			return obj.Type()
		case *gotypes.TypeName:
			if conversion {
				return obj.Type()
			}
		}
	}
	return info.Types[expr].Type
}

// resourceStringOperand unwraps string-to-string conversions. Numeric
// conversions and function calls do not expose an editable resource operand.
func resourceStringOperand(expr ast.Expr, info *types.Info) ast.Expr {
	for {
		expr = astutil.Unparen(expr)
		call, ok := expr.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 {
			return expr
		}
		// XGo can omit IsType for imported aliases. Constant call values
		// still identify conversions, and runtime conversions retain Uses.
		conversion := info.Types[call.Fun].IsType() || info.Types[call].Value != nil
		switch fun := astutil.Unparen(call.Fun).(type) {
		case *ast.Ident:
			_, namedType := info.ObjectOf(fun).(*gotypes.TypeName)
			conversion = conversion || namedType
		case *ast.SelectorExpr:
			_, namedType := info.ObjectOf(fun.Sel).(*gotypes.TypeName)
			conversion = conversion || namedType
		}
		if !conversion {
			return expr
		}
		result, arg := info.TypeOf(call), info.TypeOf(call.Args[0])
		if result == nil || arg == nil {
			return expr
		}
		resultBasic, _ := result.Underlying().(*gotypes.Basic)
		argBasic, _ := arg.Underlying().(*gotypes.Basic)
		if resultBasic == nil || argBasic == nil || resultBasic.Info()&gotypes.IsString == 0 || argBasic.Info()&gotypes.IsString == 0 {
			return expr
		}
		expr = call.Args[0]
	}
}

// resourceExpressionParts yields expressions belonging to one resource value.
// The boolean distinguishes complete values from concatenation fragments.
func resourceExpressionParts(expr ast.Expr, info *types.Info) iter.Seq2[ast.Expr, bool] {
	return func(yield func(ast.Expr, bool) bool) {
		var visit func(ast.Expr, bool) bool
		visit = func(expr ast.Expr, complete bool) bool {
			expr = astutil.Unparen(expr)
			if !yield(expr, complete) {
				return false
			}
			if operand := resourceStringOperand(expr, info); operand != expr {
				return visit(expr.(*ast.CallExpr).Args[0], complete)
			}
			if binary, ok := expr.(*ast.BinaryExpr); ok && binary.Op == token.ADD {
				return visit(binary.X, false) && visit(binary.Y, false)
			}
			return true
		}
		visit(expr, true)
	}
}

// resourceStringValue evaluates static string source expressions without
// treating interpolation, numeric conversions, or runtime calls as names.
func resourceStringValue(expr ast.Expr, info *types.Info) (string, bool) {
	expr = resourceStringOperand(expr, info)
	switch expr := expr.(type) {
	case *ast.BasicLit:
		return xgoutil.StringLitOrConstValue(expr, info.Types[expr])
	case *ast.Ident:
		if obj, ok := info.Uses[expr].(*gotypes.Const); ok && obj.Val().Kind() == constant.String {
			return constant.StringVal(obj.Val()), true
		}
	case *ast.SelectorExpr:
		if value := info.Types[expr].Value; value != nil && value.Kind() == constant.String {
			return constant.StringVal(value), true
		}
	case *ast.BinaryExpr:
		// A named string can overload addition with a runtime method.
		// Require the compiler to confirm that the result is constant.
		value := info.Types[expr].Value
		if expr.Op != token.ADD || value == nil || value.Kind() != constant.String {
			return "", false
		}
		left, leftOK := resourceStringValue(expr.X, info)
		right, rightOK := resourceStringValue(expr.Y, info)
		if leftOK && rightOK {
			return left + right, true
		}
	}
	return "", false
}
