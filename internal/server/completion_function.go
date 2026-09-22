package server

import (
	gotypes "go/types"

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

// functionCompletionDefinition keeps a callable's source name distinct from
// its implementation and filters implicit calls by the compiler-selected result.
func (ctx *completionContext) functionCompletionDefinition(resolver *autoPropertyResolver, def symbolDefinition, implicitCall bool) (symbolDefinition, bool) {
	if def.Function == nil || def.AutoPropertyType != nil {
		return def, true
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
		_, name, _, _ := displayedFuncName(source)
		if implicitCall {
			name = functionAliasName(functionValueName(source))
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
