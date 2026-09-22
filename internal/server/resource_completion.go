package server

// collectResourceNames adds unique resource names with previews in the client's
// preferred format. The first resource with a given name supplies its preview.
func (ctx *completionContext) collectResourceNames(ids []resourceID) {
	seenResourceNames := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		name := id.Name()
		if _, ok := seenResourceNames[name]; ok {
			continue
		}
		seenResourceNames[name] = struct{}{}
		item := CompletionItem{
			Kind:          TextCompletion,
			Documentation: completionDocumentation(resourceMarkupContent(id.URI(), ctx.itemSet.documentationKind)),
		}
		if ctx.setCompletionStringValue(&item, name) {
			ctx.itemSet.add(item)
		}
	}
}
