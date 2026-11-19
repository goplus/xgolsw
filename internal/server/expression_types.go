package server

import (
	gotypes "go/types"
	"slices"

	"github.com/goplus/gogen"
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
	resolver := expressionTypeResolver{
		proj:              proj,
		info:              &view,
		receivers:         make(map[propertyReceiverKey]*autoPropertyResolver),
		packageProperties: autoPropertyResolver{proj: proj},
	}
	for _, file := range astPkg.Files {
		resolver.collect(file)
	}
	return &view, nil
}

// expressionTypeResolver records implicit call results while walking source
// ancestry. Receiver resolvers are reused only while building this revision.
type expressionTypeResolver struct {
	proj              *xgo.Project
	info              *types.Info
	receivers         map[propertyReceiverKey]*autoPropertyResolver
	packageProperties autoPropertyResolver
}

// propertyReceiverKey shares lookups for pointers constructed separately for
// the same element type while preserving named types and aliases.
type propertyReceiverKey struct {
	typ     gotypes.Type
	pointer bool
}

// collect resolves children before their parents so chained getters read the
// receiver's value type without rescanning its syntax.
func (r *expressionTypeResolver) collect(file *ast.File) {
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
			typ = r.autoPropertyType(expr, file, stack)
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
func (r *expressionTypeResolver) autoPropertyType(ident *ast.Ident, file *ast.File, parents []ast.Node) gotypes.Type {
	info := r.info
	obj := info.ObjectOf(ident)
	if !isAliasCallable(obj) || info.Defs[ident] != nil {
		return nil
	}
	var selector *ast.SelectorExpr
	for _, node := range slices.Backward(parents) {
		if call := callExprFromNode(info, node); call != nil && xgoutil.CallExprFunIdent(call) == ident {
			return nil
		}
		if sel, ok := node.(*ast.SelectorExpr); ok && sel.Sel == ident {
			if isType, _ := xgoutil.IsTypeExpr(info, sel.X); isType {
				return nil
			}
			selector = sel
		}
	}
	var receiver gotypes.Type
	if selector != nil {
		receiver = info.TypeOf(selector.X)
	} else {
		receiver = xgoutil.EnclosingClassReceiver(info, file, ident.Pos(), slices.Backward(parents))
	}
	source := info.SourceObjectOf(ident)
	if xgoutil.IsValidType(receiver) {
		key := propertyReceiverKey{typ: receiver}
		if ptr, ok := receiver.(*gotypes.Pointer); ok {
			key = propertyReceiverKey{typ: ptr.Elem(), pointer: true}
		}
		resolver := r.receivers[key]
		if resolver == nil {
			resolver = &autoPropertyResolver{proj: r.proj, receiver: receiver}
			r.receivers[key] = resolver
		}
		property := resolver.resolve(ident.Name)
		if property.implementation != nil && (types.ObjectOrigin(property.implementation) == types.ObjectOrigin(obj) ||
			types.ObjectOrigin(property.selectedMember) == types.ObjectOrigin(source)) {
			return property.typ
		}
	}
	// Package aliases belong to the source declaration, which may name an
	// overload rather than its selected implementation. An implicit overload
	// call can select an optional or variadic implementation before a no-arg one.
	name := source.Name()
	sig := signatureType(obj.Type())
	if sig.Recv() == nil && name != ident.Name && xgoutil.FunctionAliasName(name) == ident.Name &&
		sig.TypeParams().Len() == 0 && (sig.Params().Len() == 0 || source != obj) {
		if _, extended := gogen.CheckFuncEx(sig); extended {
			return r.packageProperties.resolvePackageObject(source).typ
		}
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
