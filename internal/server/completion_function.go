package server

import (
	gotypes "go/types"

	"github.com/goplus/gogen"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// appendPackagePropertyDefinitions offers implicit package calls alongside
// their declarations, including aliases of function-valued variables.
func (ctx *completionContext) appendPackagePropertyDefinitions(resolver *autoPropertyResolver, defs []symbolDefinition) []symbolDefinition {
	for _, def := range defs {
		obj := def.SourceObject
		if !isAliasCallable(obj) || xgoutil.IsXGoInternalName(obj.Name()) {
			continue
		}
		if fun, ok := obj.(*gotypes.Func); ok && (fun.Signature().Recv() != nil || fun != def.Function) {
			continue
		}
		name := functionAliasName(obj.Name())
		if name == obj.Name() || name == def.CompletionItemLabel {
			continue
		}
		if obj.Pkg() == ctx.typeInfo.Pkg && obj.Pkg().Scope().Lookup(name) != nil {
			continue
		}
		property := resolver.resolvePackageObject(obj)
		if !property.exists || !xgoutil.IsValidType(property.typ) {
			continue
		}
		def.CompletionItemLabel = name
		def.CompletionItemInsertText = name
		def.AutoPropertyType = property.typ
		defs = append(defs, def)
	}
	return defs
}

// indexedPackageDefinition describes an exported declaration that can accept
// bracketed type arguments. XGox aliases and overload groups require a call.
func (ctx *completionContext) indexedPackageDefinition(resolver *autoPropertyResolver, def symbolDefinition, args []gotypes.Type) (symbolDefinition, bool) {
	if obj, ok := def.SourceObject.(*gotypes.TypeName); ok && !ctx.itemSet.expectsFunctionValue {
		return ctx.indexedTypeDefinition(def, obj, args)
	}
	fun := def.Function
	if fun == nil || !fun.Exported() {
		return def, false
	}
	name := fun.Name()
	typ := instantiateFunction(fun.Signature(), args)
	if ctx.itemSet.expectsFunctionValue {
		typ = ctx.functionValueType(resolver, name, typ, args)
	}
	if typ == nil {
		return def, false
	}
	if ctx.itemSet.callResult {
		name = functionAliasName(name)
	}
	def.TypeHint = typ
	def.functionValueSpelling = name
	sig := signatureType(typ)
	def.Overview = ctx.functionOverview(name, sig, sig.Params(), 0)
	def.CompletionItemLabel = name
	def.CompletionItemInsertText = name
	return def, true
}

// methodExpressionCompletionDefinition uses the compiler to resolve each
// spelling. A field or static value can hide the declared name or its alias.
func (ctx *completionContext) methodExpressionCompletionDefinition(resolver *autoPropertyResolver, def symbolDefinition, typeArgs []gotypes.Type) (symbolDefinition, bool) {
	if def.Function == nil {
		return def, typeArgs == nil
	}
	source := def.SourceObject.(*gotypes.Func)
	ext, _ := gogen.CheckFuncEx(source.Signature())
	if alias, ok := ext.(*gogen.TyTypeAsParams); ok && isTemplateMethodWrapper(alias.Obj()) {
		// The short alias binds the selector as a template receiver.
		// A type selector can use the encoded method spelling instead.
		return def, false
	}
	value := ctx.itemSet.isFunctionValue(def.TypeHint)
	if ctx.itemSet.expectsFunctionValue && !ctx.itemSet.callResult {
		if value || def.Function.Signature().TypeParams().Len() != 0 {
			if callback, ok := ctx.methodExpressionValueDefinition(resolver, def, source, typeArgs); ok {
				return callback, true
			}
		}
		if value || typeArgs != nil {
			return def, false
		}
	}
	name := def.CompletionItemLabel
	if def.staticMemberName == "" && source != def.Function {
		_, name, _ = displayedFuncName(source)
	}
	alternate := source.Name()
	if alternate == name {
		alternate = functionAliasName(name)
	}
	spellings := []string{name, alternate}
	if name == alternate {
		spellings = spellings[:1]
	}
	for _, spelling := range spellings {
		selected := resolver.resolveTypeMethod(spelling)
		if types.ObjectOrigin(selected.object) != types.ObjectOrigin(source) {
			continue
		}
		typ := instantiateFunction(selected.typ, typeArgs)
		if typ == nil {
			continue
		}
		if spelling != def.CompletionItemLabel {
			def.nameFunctionOverview(spelling)
			def.CompletionItemLabel = spelling
			def.CompletionItemInsertText = spelling
		}
		if typeArgs != nil {
			def.TypeHint = typ
		}
		ctx.formatTemplateMethodExpression(&def, source, spelling, typ)
		return def, true
	}
	return def, false
}

// methodExpressionValueDefinition checks the actual selector signature when
// completing a function value, including generic methods inferred by context.
func (ctx *completionContext) methodExpressionValueDefinition(resolver *autoPropertyResolver, def symbolDefinition, source *gotypes.Func, typeArgs []gotypes.Type) (symbolDefinition, bool) {
	name := def.functionValueName()
	declared := name
	if isTemplateMethodWrapper(source) {
		declared = source.Name()
	}
	spellings := []string{declared}
	if alias := functionAliasName(declared); alias != declared {
		spellings = append(spellings, alias)
	}
	for _, spelling := range spellings {
		selected := resolver.resolveTypeMethod(spelling)
		obj := types.ObjectOrigin(selected.object)
		if obj != types.ObjectOrigin(source) && obj != types.ObjectOrigin(def.Function) {
			continue
		}
		typ := ctx.functionValueType(resolver, spelling, instantiateFunction(selected.typ, typeArgs), typeArgs)
		if typ == nil {
			continue
		}
		if spelling != name {
			def.functionValueSpelling = spelling
		}
		def.TypeHint = typ
		ctx.formatTemplateMethodExpression(&def, source, spelling, typ)
		return def, true
	}
	return def, false
}

// functionValueType returns the instantiated type when the callable matches an
// expected function value.
func (ctx *completionContext) functionValueType(resolver *autoPropertyResolver, spelling string, typ gotypes.Type, args []gotypes.Type) gotypes.Type {
	if sig := signatureType(typ); sig != nil && sig.TypeParams().Len() != 0 {
		typ = resolver.inferFunctionValue(spelling, sig, args, ctx.itemSet.expectedTypes)
	}
	if ctx.itemSet.isFunctionValue(typ) {
		return typ
	}
	return nil
}

// instantiateFunction applies a complete type argument list. A partial
// list keeps the signature generic until a call or callback can infer the rest.
func instantiateFunction(typ gotypes.Type, args []gotypes.Type) gotypes.Type {
	if len(args) == 0 {
		return typ
	}
	sig := signatureType(typ)
	if sig == nil || len(args) > sig.TypeParams().Len() {
		return nil
	}
	if len(args) < sig.TypeParams().Len() {
		return sig
	}
	instantiated, err := gotypes.Instantiate(nil, sig, args, true)
	if err != nil {
		return nil
	}
	return instantiated
}

// formatTemplateMethodExpression displays the template implementation's
// explicit receiver when the selector has an ordinary function signature.
func (ctx *completionContext) formatTemplateMethodExpression(def *symbolDefinition, source *gotypes.Func, name string, typ gotypes.Type) {
	if source == def.Function || !isTemplateMethodWrapper(source) {
		return
	}
	if sig := signatureType(typ); sig != nil {
		if _, extended := gogen.CheckFuncEx(sig); !extended {
			def.Overview = ctx.functionOverview(name, sig, sig.Params(), 0)
		}
	}
}

// functionCompletionDefinition keeps a callable's source name distinct from
// its implementation and filters implicit calls by the compiler-selected result.
func (ctx *completionContext) functionCompletionDefinition(resolver *autoPropertyResolver, def symbolDefinition, implicitCall bool) (symbolDefinition, bool) {
	if def.Function == nil || def.AutoPropertyType != nil || def.staticMemberName != "" {
		return def, true
	}
	if resolver.receiver != nil && def.Function.Signature().Params().Len() == 0 {
		if _, _, template := xgoutil.TemplateMethodName(def.Function, false); template {
			// A malformed imported template cannot bind an instance receiver.
			return def, false
		}
	}
	source := def.SourceObject.(*gotypes.Func)
	if ctx.itemSet.isFunctionValue(def.TypeHint) {
		// Callback insertion uses the implementation's exact name. An alias
		// does not make a hidden implementation accessible through that name.
		if source != def.Function {
			name := functionValueName(def.Function)
			var obj gotypes.Object
			if resolver.receiver != nil {
				obj = resolver.resolve(name).object
			} else if def.Function.Pkg() == source.Pkg() {
				obj = source.Pkg().Scope().Lookup(name)
				if obj != nil && obj.Pkg() != ctx.typeInfo.Pkg && !obj.Exported() {
					return def, false
				}
			}
			if obj == nil || types.ObjectOrigin(obj) != types.ObjectOrigin(def.Function) && types.ObjectOrigin(obj) != types.ObjectOrigin(source) {
				return def, false
			}
		}
		return def, true
	}
	if source != def.Function {
		_, name, _ := displayedFuncName(source)
		if implicitCall {
			name = functionAliasName(functionValueName(source))
		}
		if !xgoutil.IsInBuiltinPkg(source) {
			def.nameFunctionOverview(name)
		}
		def.CompletionItemLabel = name
		def.CompletionItemInsertText = name
		if resolver.receiver == nil && source.Pkg() == ctx.typeInfo.Pkg {
			if obj := source.Pkg().Scope().Lookup(name); obj != nil && obj != source {
				return def, false
			}
		}
	}
	if !implicitCall {
		return def, true
	}
	var property autoProperty
	if resolver.receiver != nil {
		property = resolver.resolve(def.CompletionItemLabel)
		if _, field := property.object.(*gotypes.Var); field {
			return def, false
		}
	} else {
		name := functionValueName(source)
		if def.CompletionItemLabel == name || def.CompletionItemLabel != functionAliasName(name) {
			return def, true
		}
		property = resolver.resolvePackageObject(source)
	}
	if !property.exists {
		return def, true
	}
	if property.typ == nil || property.function == nil || types.ObjectOrigin(property.function) != types.ObjectOrigin(def.Function) {
		return def, false
	}
	def.AutoPropertyType = property.typ
	return def, true
}
