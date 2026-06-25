package server

import (
	"html"
)

// adaptHoverMarkupForClient selects a hover markup kind supported by the client.
func adaptHoverMarkupForClient(capabilities HoverClientCapabilities, hover *Hover) *Hover {
	if hover == nil || markupKindSupportedByClient(capabilities.ContentFormat, hover.Contents.Kind) {
		return hover
	}
	result := *hover
	result.Contents = MarkupContent{
		Kind:  PlainText,
		Value: markupValueAsPlainText(hover.Contents),
	}
	return &result
}

// adaptCompletionDocumentationForClient selects a documentation form supported by the client.
func adaptCompletionDocumentationForClient(capabilities CompletionClientCapabilities, documentation *Or_CompletionItem_documentation) *Or_CompletionItem_documentation {
	if documentation == nil {
		return nil
	}
	content, ok := documentation.Value.(MarkupContent)
	if !ok || markupKindSupportedByClient(capabilities.CompletionItem.DocumentationFormat, content.Kind) {
		return documentation
	}
	return &Or_CompletionItem_documentation{Value: markupValueAsPlainText(content)}
}

// markupKindSupportedByClient reports whether kind can be sent as-is.
func markupKindSupportedByClient(formats []MarkupKind, kind MarkupKind) bool {
	if kind == PlainText {
		return true
	}
	for _, format := range formats {
		if format == kind {
			return true
		}
		if format == PlainText {
			return false
		}
	}
	return false
}

// markupValueAsPlainText returns markup content for a plain-text response. It
// decodes HTML entities but does not parse Markdown or HTML markup.
func markupValueAsPlainText(content MarkupContent) string {
	return html.UnescapeString(content.Value)
}
