package server

import (
	gotypes "go/types"

	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// getPropertyTarget returns the project type whose properties are addressed by
// the enclosing call, or the current classfile when there is no enclosing call.
func (ctx *completionContext) getPropertyTarget() *gotypes.Named {
	var named *gotypes.Named
	if call := ctx.getEnclosingCallExpr(); call != nil {
		named = propertyTargetForCall(ctx.typeInfo, ctx.astFile, call)
	} else {
		named = classTypeForFile(ctx.proj, ctx.astFile)
	}
	if named == nil || !xgoutil.IsInMainPkg(named.Obj()) {
		return nil
	}
	return named
}

// collectPropertyNames collects property name completion items for the receiver.
func (ctx *completionContext) collectPropertyNames(named *gotypes.Named) {
	if named == nil {
		return
	}
	if _, ok := named.Underlying().(*gotypes.Struct); !ok {
		return
	}

	for m := range ctx.propertyMembers(named) {
		key := "property:" + m.Name
		if _, seen := ctx.itemSet.seenDefinitions[key]; seen {
			continue
		}
		if ctx.itemSet.notRecommended != nil && ctx.itemSet.notRecommended(m.Definition) {
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
