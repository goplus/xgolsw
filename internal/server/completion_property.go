package server

import gotypes "go/types"

// collectPropertyNames collects property name completion items for the given target type.
func (ctx *completionContext) collectPropertyNames(target string) {
	typeName, ok := ctx.typeInfo.Pkg.Scope().Lookup(target).(*gotypes.TypeName)
	if !ok {
		return
	}
	namedType := resolvedNamedType(typeName.Type())
	if namedType == nil {
		return
	}

	mainPkgDoc, _ := ctx.proj.PkgDoc()
	for m := range propertyMembers(namedType, makePkgDocFor(mainPkgDoc, ctx.lookupPkgDoc)) {
		key := m.SpxDef.ID.String()
		if _, seen := ctx.itemSet.seenSpxDefs[key]; seen {
			continue
		}
		item := m.SpxDef.completionItem(ctx.itemSet.documentationKind)
		item.Kind = PropertyCompletion
		if !ctx.setCompletionStringValue(&item, m.Name) {
			continue
		}
		ctx.itemSet.seenSpxDefs[key] = struct{}{}
		// The candidate is a property name, independent of the property's value type.
		ctx.itemSet.add(item)
	}
}
