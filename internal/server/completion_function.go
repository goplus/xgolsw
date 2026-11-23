package server

import (
	goast "go/ast"
	gotypes "go/types"
	"slices"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
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
		if fun, ok := obj.(*gotypes.Func); ok && (fun.Signature().Recv() != nil || fun != def.Implementation) {
			continue
		}
		name := xgoutil.FunctionAliasName(obj.Name())
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
	callback := ctx.itemSet.expectsFunctionValue && !ctx.itemSet.callResult
	if obj, ok := def.SourceObject.(*gotypes.TypeName); ok && !callback {
		return ctx.indexedTypeDefinition(def, obj, args)
	}
	fun := def.function()
	if fun == nil || !fun.Exported() {
		return def, false
	}
	name := fun.Name()
	typ := ctx.indexedCallableType(resolver, fun.Signature(), args)
	if callback {
		typ = ctx.functionValueType(resolver, name, typ, args)
	}
	if typ == nil {
		return def, false
	}
	if ctx.itemSet.callResult {
		name = xgoutil.FunctionAliasName(name)
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
	if def.Implementation == nil {
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
		if value || signatureType(def.Implementation.Type()).TypeParams().Len() != 0 {
			if callback, ok := ctx.methodExpressionValueDefinition(resolver, def, source, typeArgs); ok {
				return callback, true
			}
		}
		if value || typeArgs != nil {
			return def, false
		}
	}
	name := def.CompletionItemLabel
	if def.staticMemberName == "" && source != def.Implementation {
		_, name, _ = displayedFuncName(source)
	}
	alternate := source.Name()
	if alternate == name {
		alternate = xgoutil.MemberAliasName(name)
	}
	spellings := []string{name, alternate}
	if name == alternate {
		spellings = spellings[:1]
	}
	for _, spelling := range spellings {
		selected := resolver.resolveTypeMember(spelling)
		if types.ObjectOrigin(selected.object) != types.ObjectOrigin(source) {
			continue
		}
		typ := ctx.indexedCallableType(resolver, selected.typ, typeArgs)
		if typ == nil || !ctx.formatMethodExpression(&def, spelling, typ, resolver.receiver) {
			continue
		}
		def.CompletionItemLabel = spelling
		def.CompletionItemInsertText = spelling
		if typeArgs != nil {
			def.TypeHint = typ
		}
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
	if alias := xgoutil.MemberAliasName(declared); alias != declared {
		spellings = append(spellings, alias)
	}
	for _, spelling := range spellings {
		selected := resolver.resolveTypeMember(spelling)
		obj := types.ObjectOrigin(selected.object)
		if obj != types.ObjectOrigin(source) && obj != types.ObjectOrigin(def.Implementation) {
			continue
		}
		typ := ctx.functionValueType(resolver, spelling, instantiateFunction(selected.typ, typeArgs), typeArgs)
		if typ == nil || !ctx.formatMethodExpression(&def, spelling, typ, resolver.receiver) {
			continue
		}
		if spelling != name {
			def.functionValueSpelling = spelling
		}
		def.TypeHint = typ
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

// indexedCallableType uses the written value arguments to finish instantiating
// a call with a partial list of bracketed type arguments.
func (ctx *completionContext) indexedCallableType(resolver *autoPropertyResolver, typ gotypes.Type, typeArgs []gotypes.Type) gotypes.Type {
	typ = instantiateFunction(typ, typeArgs)
	call := ctx.calleeCall
	if len(typeArgs) == 0 || call == nil || !ctx.itemSet.callResult {
		return typ
	}
	sig := signatureType(typ)
	if sig == nil || sig.TypeParams().Len() == 0 {
		return typ
	}
	// Tuple lowering runs before the compiler can infer the remaining types.
	args, ellipsis := xgoutil.CallExprArgs(ctx.typeInfo, call, nil)
	operandCount := len(args)
	if len(call.Kwargs) != 0 {
		operandCount++
	}
	var flags gogen.InstrFlags
	if ellipsis {
		if !sig.Variadic() || operandCount != sig.Params().Len() {
			return typ
		}
		flags = gogen.InstrFlagEllipsis
	}
	placeholder := goast.NewIdent("_")
	operands := make([]*gogen.Element, 0, operandCount)
	for _, arg := range args {
		value := ctx.writtenValue(resolver, arg)
		if value.IsType() || !xgoutil.IsValidType(value.Type) || value.Type.Underlying() == nil {
			return typ
		}
		operands = append(operands, &gogen.Element{Val: placeholder, Type: value.Type, CVal: value.Value})
	}
	cb := resolver.codeBuilder()
	if len(call.Kwargs) != 0 {
		// A partially instantiated bracket call has no signature during
		// keyword lowering. XGo appends a map inferred from its values.
		mapType := xgoutil.KeywordMapType(cb, call.Kwargs, nil, func(expr ast.Expr) gotypes.Type {
			value := ctx.writtenValue(resolver, expr)
			if value.IsType() {
				return nil
			}
			return value.Type
		})
		if mapType == nil {
			return typ
		}
		operands = append(operands, &gogen.Element{Val: placeholder, Type: mapType})
	}
	fn := &gogen.Element{Val: placeholder, Type: sig}
	inferred, err := gogen.InferFunc(cb.Pkg(), fn, sig, typeArgs, operands, flags)
	if err != nil {
		return typ
	}
	return inferred
}

// formatMethodExpression displays the selected callable with its explicit
// receiver. A synthetic group supplies one signature per implementation.
func (ctx *completionContext) formatMethodExpression(def *symbolDefinition, name string, typ, receiver gotypes.Type) bool {
	sig := signatureType(typ)
	if sig == nil {
		return false
	}
	params := sig.Params()
	if _, extended := gogen.CheckFuncEx(sig); extended {
		sig = signatureType(def.Implementation.Type())
		params = sig.Params()
		if recv := sig.Recv(); recv != nil {
			params = xgoutil.MethodExpressionParams(params, receiver, recv.Type())
			if params == nil {
				return false
			}
		}
	}
	def.Overview = ctx.functionOverview(name, sig, params, 0)
	return true
}

// instanceCompletionDefinition keeps only callables available through the
// actual receiver and spelling, including implicit class receivers.
func (ctx *completionContext) instanceCompletionDefinition(resolver *autoPropertyResolver, def symbolDefinition, implicitCall bool) (symbolDefinition, bool) {
	def, ok := ctx.functionCompletionDefinition(resolver, def, implicitCall)
	if !ok || def.Implementation == nil {
		return def, ok
	}
	name := ctx.itemSet.definitionName(def)
	selection := resolver.resolve(name)
	if def.AutoPropertyType == nil && ctx.itemSet.isFunctionValue(def.TypeHint) && !ctx.itemSet.isFunctionValue(selection.selectedType) {
		return def, false
	}
	selected := types.ObjectOrigin(selection.selectedMember)
	return def, selected == types.ObjectOrigin(def.SourceObject) || selected == types.ObjectOrigin(def.Implementation)
}

// functionCompletionDefinition keeps a callable's source name distinct from
// its implementation and filters implicit calls by the compiler-selected result.
func (ctx *completionContext) functionCompletionDefinition(resolver *autoPropertyResolver, def symbolDefinition, implicitCall bool) (symbolDefinition, bool) {
	implementation := def.Implementation
	if implementation == nil || def.AutoPropertyType != nil || def.staticMemberName != "" {
		return def, true
	}
	fun := def.function()
	if resolver.receiver != nil && fun != nil && fun.Signature().Params().Len() == 0 {
		if _, _, template := xgoutil.TemplateMethodName(fun, false); template {
			// A malformed imported template cannot bind an instance receiver.
			return def, false
		}
	}
	source := def.SourceObject.(*gotypes.Func)
	aliasName := xgoutil.FunctionAliasName
	if resolver.receiver != nil {
		aliasName = xgoutil.MemberAliasName
	}
	if ctx.itemSet.isFunctionValue(def.TypeHint) {
		// Callback insertion uses the implementation's exact name. An alias
		// does not make a hidden implementation accessible through that name.
		if source != implementation {
			name := def.functionValueName()
			var obj gotypes.Object
			if resolver.receiver != nil {
				obj = resolver.resolve(name).object
			} else if implementation.Pkg() == source.Pkg() {
				obj = source.Pkg().Scope().Lookup(name)
				if obj != nil && obj.Pkg() != ctx.typeInfo.Pkg && !obj.Exported() {
					return def, false
				}
			}
			if obj == nil || types.ObjectOrigin(obj) != types.ObjectOrigin(implementation) && types.ObjectOrigin(obj) != types.ObjectOrigin(source) {
				return def, false
			}
		}
		return def, true
	}
	if source == implementation {
		if group := xgoutil.OverloadGroupForImplementation(source); group != nil &&
			(def.CompletionItemInsertText == group.Name() || def.CompletionItemInsertText == aliasName(group.Name())) {
			// Dropping the index selects the group, which can exclude this
			// implementation through an explicit list.
			if !slices.ContainsFunc(xgoutil.ExpandXGoOverloadableFunc(group), func(fun *gotypes.Func) bool {
				return fun.Origin() == source.Origin()
			}) {
				return def, false
			}
			def.SourceObject = group
		}
	}
	if source != implementation {
		_, name, _ := displayedFuncName(source)
		if implicitCall {
			name = aliasName(functionValueName(source))
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
		if def.CompletionItemLabel == name || def.CompletionItemLabel != aliasName(name) {
			return def, true
		}
		property = resolver.resolvePackageObject(source)
	}
	if !property.exists {
		return def, true
	}
	if property.typ == nil || types.ObjectOrigin(property.implementation) != types.ObjectOrigin(implementation) {
		return def, false
	}
	def.AutoPropertyType = property.typ
	return def, true
}
