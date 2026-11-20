package server

import (
	"go/constant"
	gotoken "go/token"
	gotypes "go/types"
	"slices"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil/typeparams"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// indexedCompletionTypeArguments resolves written type arguments of the
// identifier or selector at the head of path. Value indices keep the
// ordinary completion context.
func (ctx *completionContext) indexedCompletionTypeArguments(path []ast.Node) []gotypes.Type {
	if len(path) < 2 {
		return nil
	}
	switch path[0].(type) {
	case *ast.Ident, *ast.SelectorExpr:
	default:
		return nil
	}
	for i, node := range path[1:] {
		if _, ok := node.(*ast.ParenExpr); ok {
			continue
		}
		indexed := typeparams.UnpackIndexExpr(node)
		if indexed == nil || astutil.Unparen(indexed.X) != path[0] {
			break
		}
		args := ctx.resolveTypeArguments(indexed.Indices, ctx.typeExpression)
		if args == nil {
			return nil
		}
		if !ctx.typeExpression && !ctx.itemSet.callResult {
			expected := expectedExprTypes(ctx.typeInfo, path[i+1:])
			if slices.ContainsFunc(expected, func(typ gotypes.Type) bool { return signatureType(typ) != nil }) {
				ctx.itemSet.setExpectedTypes(expected)
			}
		}
		return args
	}
	return nil
}

// resolveTypeArguments resolves a complete list of type arguments.
func (ctx *completionContext) resolveTypeArguments(indices []ast.Expr, typeSyntax bool) []gotypes.Type {
	args := make([]gotypes.Type, 0, len(indices))
	for _, index := range indices {
		arg := ctx.resolveTypeArgument(index, typeSyntax)
		if !xgoutil.IsValidType(arg) {
			return nil
		}
		args = append(args, arg)
	}
	return args
}

// resolveTypeArgument resolves a type argument even when an incomplete
// selector prevents the checker from reaching them.
func (ctx *completionContext) resolveTypeArgument(expr ast.Expr, typeSyntax bool) gotypes.Type {
	expr = astutil.Unparen(expr)
	if isType, known := xgoutil.IsTypeExpr(ctx.typeInfo, expr); known {
		if !isType {
			return nil
		}
		if typ := ctx.typeInfo.TypeOf(expr); xgoutil.IsValidType(typ) {
			return typ
		}
	}
	switch expr := expr.(type) {
	case *ast.Ident, *ast.SelectorExpr:
		if obj, ok := ctx.typeArgumentObject(expr, typeSyntax).(*gotypes.TypeName); ok {
			return obj.Type()
		}
	case *ast.IndexExpr, *ast.IndexListExpr:
		indexed := typeparams.UnpackIndexExpr(expr)
		if obj, ok := ctx.typeArgumentObject(indexed.X, typeSyntax).(*gotypes.TypeName); ok {
			if args := ctx.resolveTypeArguments(indexed.Indices, typeSyntax); args != nil {
				return instantiateIndexedType(obj.Type(), args, typeSyntax)
			}
		}
	case *ast.StarExpr:
		if elem := ctx.resolveTypeArgument(expr.X, typeSyntax); xgoutil.IsValidType(elem) {
			return gotypes.NewPointer(elem)
		}
	case *ast.ArrayType:
		if elem := ctx.resolveTypeArgument(expr.Elt, true); xgoutil.IsValidType(elem) {
			if expr.Len == nil {
				return gotypes.NewSlice(elem)
			}
			if length, ok := ctx.arrayTypeLength(expr.Len); ok {
				return gotypes.NewArray(elem, length)
			}
		}
	case *ast.MapType:
		key := ctx.resolveTypeArgument(expr.Key, true)
		value := ctx.resolveTypeArgument(expr.Value, true)
		if xgoutil.IsValidType(key) && xgoutil.IsValidType(value) {
			return gotypes.NewMap(key, value)
		}
	case *ast.ChanType:
		if elem := ctx.resolveTypeArgument(expr.Value, true); xgoutil.IsValidType(elem) {
			dir := gotypes.SendRecv
			switch expr.Dir {
			case ast.SEND:
				dir = gotypes.SendOnly
			case ast.RECV:
				dir = gotypes.RecvOnly
			}
			return gotypes.NewChan(dir, elem)
		}
	case *ast.FuncType:
		return ctx.resolveFunctionTypeArgument(expr)
	}
	return nil
}

// arrayTypeLength resolves a recorded constant, a literal, or a constant name.
func (ctx *completionContext) arrayTypeLength(expr ast.Expr) (int64, bool) {
	expr = astutil.Unparen(expr)
	value := ctx.typeInfo.Types[expr].Value
	if value == nil {
		switch expr := expr.(type) {
		case *ast.BasicLit:
			switch expr.Kind {
			case token.INT, token.FLOAT, token.CHAR:
				value = constant.MakeFromLiteral(expr.Value, gotoken.Token(expr.Kind), 0)
			}
		case *ast.Ident, *ast.SelectorExpr:
			if obj, ok := ctx.typeArgumentObject(expr, false).(*gotypes.Const); ok {
				value = obj.Val()
			}
		}
	}
	if value == nil {
		return 0, false
	}
	length, ok := constant.Int64Val(constant.ToInt(value))
	return length, ok && length >= 0
}

// resolveFunctionTypeArgument rebuilds the signature of a written function type.
func (ctx *completionContext) resolveFunctionTypeArgument(expr *ast.FuncType) gotypes.Type {
	params, ok := ctx.resolveFunctionTypeFields(expr.Params)
	if !ok {
		return nil
	}
	results, ok := ctx.resolveFunctionTypeFields(expr.Results)
	if !ok {
		return nil
	}
	var variadic bool
	if fields := expr.Params.List; len(fields) != 0 {
		_, variadic = fields[len(fields)-1].Type.(*ast.Ellipsis)
	}
	return gotypes.NewSignatureType(nil, nil, nil, params, results, variadic)
}

// resolveFunctionTypeFields expands each parameter or result name into a
// tuple entry, with a slice representing a variadic parameter.
func (ctx *completionContext) resolveFunctionTypeFields(fields *ast.FieldList) (*gotypes.Tuple, bool) {
	if fields == nil {
		return nil, true
	}
	var vars []*gotypes.Var
	for _, field := range fields.List {
		expr := field.Type
		ellipsis, variadic := expr.(*ast.Ellipsis)
		if variadic {
			expr = ellipsis.Elt
		}
		typ := ctx.resolveTypeArgument(expr, true)
		if !xgoutil.IsValidType(typ) {
			return nil, false
		}
		if variadic {
			typ = gotypes.NewSlice(typ)
		}
		if len(field.Names) == 0 {
			vars = append(vars, gotypes.NewParam(field.Pos(), ctx.typeInfo.Pkg, "", typ))
			continue
		}
		for _, name := range field.Names {
			vars = append(vars, gotypes.NewParam(name.Pos(), ctx.typeInfo.Pkg, name.Name, typ))
		}
	}
	return gotypes.NewTuple(vars...), true
}

// typeArgumentObject resolves the declaration rather than the recorded type of
// a name, which may already hold an instantiated type.
func (ctx *completionContext) typeArgumentObject(expr ast.Expr, typeSyntax bool) gotypes.Object {
	var obj gotypes.Object
	switch expr := astutil.Unparen(expr).(type) {
	case *ast.Ident:
		obj = ctx.typeInfo.ObjectOf(expr)
		if obj == nil {
			obj = ctx.unqualifiedTypeArgumentObject(expr.Name, typeSyntax)
		}
	case *ast.SelectorExpr:
		obj = ctx.typeInfo.ObjectOf(expr.Sel)
		if obj == nil && expr.Sel.IsExported() {
			if ident, ok := astutil.Unparen(expr.X).(*ast.Ident); ok {
				if pkg := ctx.selectorPackage(ident, typeSyntax); pkg != nil {
					obj = pkg.Imported().Scope().Lookup(expr.Sel.Name)
				}
			}
		}
	}
	return obj
}

// selectorPackage resolves an import even when the checker did not record its
// use. In value expressions, a visible local or class member can shadow it.
func (ctx *completionContext) selectorPackage(ident *ast.Ident, typeSyntax bool) *gotypes.PkgName {
	if obj := ctx.typeInfo.ObjectOf(ident); obj != nil {
		pkg, _ := obj.(*gotypes.PkgName)
		return pkg
	}
	pkg := importsForFile(ctx.proj, ctx.astFile).named[ident.Name]
	if pkg != nil && !typeSyntax {
		if visible := ctx.typeDisplay.sourceLookup(ident.Name); visible != nil && visible.Parent() != gotypes.Universe {
			return nil
		}
	}
	return pkg
}

// unqualifiedTypeArgumentObject follows XGo's type lookup inside type syntax
// and expression lookup in a bracket operand. A lowercase function alias can
// supply a value index by invoking an auto-property.
func (ctx *completionContext) unqualifiedTypeArgumentObject(name string, typeSyntax bool) gotypes.Object {
	var visible gotypes.Object
	exported := name
	if typeSyntax {
		_, obj := ctx.innermostScope.LookupParent(name, ctx.pos)
		if _, ok := obj.(*gotypes.TypeName); ok {
			return obj
		}
	} else {
		visible = ctx.typeDisplay.sourceLookup(name)
		if visible != nil && visible.Parent() != gotypes.Universe {
			return visible
		}
		exported = upperFirstASCII(name)
	}
	if !token.IsExported(exported) {
		return visible
	}
	imports := importsForFile(ctx.proj, ctx.astFile)
	if imports.ambiguous[name] {
		return nil
	}
	for _, pkg := range imports.members {
		if obj := pkg.Scope().Lookup(exported); obj != nil && (name == exported || gogen.HasAutoProperty(obj.Type())) {
			return obj
		}
	}
	return visible
}

// indexedTypeDefinition describes a type after applying its written arguments.
func (ctx *completionContext) indexedTypeDefinition(def symbolDefinition, obj *gotypes.TypeName, args []gotypes.Type) (symbolDefinition, bool) {
	instance := instantiateIndexedType(obj.Type(), args, ctx.typeExpression)
	if instance == nil {
		return def, false
	}
	if _, overloaded := gogen.CheckOverloadNamed(obj.Type()); overloaded {
		variant := instance.(*gotypes.Named).Obj()
		def = ctx.typeDisplay.definitionForType(variant, obj.Name(), ctx.pkgDocForObject(variant))
		def.SourceObject = obj
	}
	def.TypeHint = instance
	return def, true
}

// instantiateIndexedType applies a complete type argument list to a named
// type or alias. Only type syntax tries overloaded named types in index order.
func instantiateIndexedType(typ gotypes.Type, args []gotypes.Type, typeSyntax bool) gotypes.Type {
	if group, overloaded := gogen.CheckOverloadNamed(typ); overloaded {
		if !typeSyntax {
			return nil
		}
		for _, variant := range group.Types {
			if instance := instantiateIndexedType(variant, args, typeSyntax); instance != nil {
				return instance
			}
		}
		return nil
	}
	generic, ok := typ.(interface{ TypeParams() *gotypes.TypeParamList })
	if !ok || generic.TypeParams().Len() != len(args) {
		return nil
	}
	instance, err := gotypes.Instantiate(nil, typ, args, true)
	if err != nil {
		return nil
	}
	return instance
}
