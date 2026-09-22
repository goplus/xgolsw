package server

import (
	gotypes "go/types"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// expressionTypesCacheKind identifies source expression types for a project revision.
type expressionTypesCacheKind struct{}

// buildExpressionTypesCache supplements the compiler's expression types with
// implicit call results. Object identities and signatures remain unchanged.
// The compiler's original type map remains unchanged.
func buildExpressionTypesCache(proj *xgo.Project) (any, error) {
	proj.TypeInfo()
	proj = proj.Snapshot()
	info, err := proj.TypeInfo()
	if info == nil {
		return nil, err
	}
	astPkg, _ := proj.ASTPackage()
	view := *info
	view.ImplicitCallTypes = make(map[ast.Expr]gotypes.Type)
	resolver := expressionTypeResolver{proj: proj, info: &view, receivers: make(map[gotypes.Type]*autoPropertyResolver)}
	for _, file := range astPkg.Files {
		resolver.collect(file)
	}
	return &view, nil
}

// expressionTypeResolver records implicit call results while walking source
// ancestry. Receiver resolvers are reused only while building this revision.
type expressionTypeResolver struct {
	proj      *xgo.Project
	info      *types.Info
	receivers map[gotypes.Type]*autoPropertyResolver
}

// collect resolves children before their parents so chained getters read the
// receiver's value type without rescanning its syntax.
func (r *expressionTypeResolver) collect(file *ast.File) {
	var receiver gotypes.Type
	if class := classTypeForFile(r.proj, file); class != nil {
		receiver = gotypes.NewPointer(class)
	}
	var stack []ast.Node
	ast.Inspect(file, func(node ast.Node) bool {
		if node != nil {
			stack = append(stack, node)
			return true
		}
		node = stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		var typ gotypes.Type
		switch expr := node.(type) {
		case *ast.Ident:
			typ = r.autoPropertyType(expr, stack, receiver)
		case *ast.SelectorExpr:
			typ = r.info.ImplicitCallTypes[expr.Sel]
		case *ast.ParenExpr:
			typ = r.info.ImplicitCallTypes[expr.X]
		}
		if xgoutil.IsValidType(typ) {
			r.info.ImplicitCallTypes[node.(ast.Expr)] = typ
		}
		return true
	})
}

// autoPropertyType resolves a source property read without treating explicit
// calls, method expressions, or declarations as property values.
func (r *expressionTypeResolver) autoPropertyType(ident *ast.Ident, parents []ast.Node, receiver gotypes.Type) gotypes.Type {
	info := r.info
	obj := info.ObjectOf(ident)
	if !isAliasCallable(obj) || info.Defs[ident] != nil {
		return nil
	}
	for _, node := range slices.Backward(parents) {
		if call := callExprFromNode(info, node); call != nil && xgoutil.CallExprFunIdent(call) == ident {
			return nil
		}
		if selector, ok := node.(*ast.SelectorExpr); ok && selector.Sel == ident {
			if isType, _ := xgoutil.IsTypeExpr(info, selector.X); isType {
				return nil
			}
			receiver = info.TypeOf(selector.X)
		}
	}
	if xgoutil.IsValidType(receiver) {
		resolver := r.receivers[receiver]
		if resolver == nil {
			resolver = &autoPropertyResolver{proj: r.proj, receiver: receiver}
			r.receivers[receiver] = resolver
		}
		property := resolver.resolve(ident.Name)
		if property.function != nil && types.ObjectOrigin(property.function) == types.ObjectOrigin(obj) {
			return property.typ
		}
	}
	// Package aliases belong to the source declaration, which may name an
	// overload rather than its selected implementation. An implicit overload
	// call can select an optional or variadic implementation before a no-arg one.
	source := info.SourceObjectOf(ident)
	name := source.Name()
	sig := signatureType(obj.Type())
	if sig.Recv() == nil && name != ident.Name && functionAliasName(name) == ident.Name &&
		sig.TypeParams().Len() == 0 && (sig.Params().Len() == 0 || source != obj) {
		if sig.Results().Len() == 1 {
			return sig.Results().At(0).Type()
		}
		return sig.Results()
	}
	return nil
}

// expressionTypeInfo returns a read-only view with source expression value
// types. Explicit calls and callbacks retain their callable signatures.
func expressionTypeInfo(proj *xgo.Project) (*types.Info, error) {
	data, err := proj.Cache(expressionTypesCacheKind{})
	if err != nil {
		return nil, err
	}
	return data.(*types.Info), nil
}
