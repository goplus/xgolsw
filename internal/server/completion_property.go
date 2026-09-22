package server

import (
	gotypes "go/types"

	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// getPropertyTarget returns the project type whose properties are addressed by
// the enclosing call, or the current classfile when there is no enclosing call.
func (ctx *completionContext) getPropertyTarget() string {
	var named *gotypes.Named
	if call := ctx.getEnclosingCallExpr(); call != nil {
		named = propertyTargetForCall(ctx.proj, ctx.astFile, call)
	} else {
		named = classTypeForFile(ctx.proj, ctx.astFile)
	}
	if named == nil || !xgoutil.IsInMainPkg(named.Obj()) {
		return ""
	}
	return named.Obj().Name()
}

// collectPropertyNames collects property name completion items for the given target type.
func (ctx *completionContext) collectPropertyNames(target string) {
	typeName, ok := ctx.typeInfo.Pkg.Scope().Lookup(target).(*gotypes.TypeName)
	if !ok {
		return
	}
	typ := gotypes.Unalias(xgoutil.DerefType(gotypes.Unalias(typeName.Type())))
	namedType, ok := typ.(*gotypes.Named)
	if !ok {
		return
	}
	if _, ok := namedType.Underlying().(*gotypes.Struct); !ok {
		return
	}

	for m := range ctx.propertyMembers(namedType) {
		key := "property:" + m.Name
		if _, seen := ctx.itemSet.seenDefinitions[key]; seen {
			continue
		}
		item := m.Definition.completionItem(ctx.itemSet.documentationKind)
		item.Kind = PropertyCompletion
		if !ctx.setCompletionStringValue(&item, m.Name) {
			continue
		}
		ctx.itemSet.seenDefinitions[key] = struct{}{}
		// The candidate is a property name, independent of the property's value type.
		ctx.itemSet.add(item)
	}
}
