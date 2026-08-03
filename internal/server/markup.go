package server

// preferredMarkupKind returns the client's most preferred supported markup
// kind. Plain text is the protocol-compatible default.
func preferredMarkupKind(formats []MarkupKind) MarkupKind {
	for _, format := range formats {
		switch format {
		case Markdown, PlainText:
			return format
		}
	}
	return PlainText
}

// markupContent constructs markup content from equivalent Markdown and plain
// text representations.
func markupContent(kind MarkupKind, markdownValue, plainTextValue string) MarkupContent {
	if kind == Markdown {
		return MarkupContent{Kind: Markdown, Value: markdownValue}
	}
	return MarkupContent{Kind: PlainText, Value: plainTextValue}
}

// completionDocumentation converts markup content to the completion
// documentation representation compatible with older plain-text clients.
func completionDocumentation(content MarkupContent) *Or_CompletionItem_documentation {
	if content.Kind == PlainText {
		return &Or_CompletionItem_documentation{Value: content.Value}
	}
	return &Or_CompletionItem_documentation{Value: content}
}

// resourceMarkupContent constructs markup content for an XGo resource.
func resourceMarkupContent(uri XGoResourceURI, kind MarkupKind) MarkupContent {
	return markupContent(kind, uri.HTML(), string(uri))
}
