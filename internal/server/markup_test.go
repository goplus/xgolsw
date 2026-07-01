package server

import (
	"testing"

	"github.com/goplus/xgolsw/protocol"
	"github.com/stretchr/testify/assert"
)

func TestAdaptHoverMarkupForClient(t *testing.T) {
	for _, tt := range []struct {
		name         string
		capabilities HoverClientCapabilities
		hover        *Hover
		want         *Hover
	}{
		{
			name: "DowngradesUnsupportedMarkdown",
			hover: &Hover{
				Contents: MarkupContent{Kind: Markdown, Value: "&lt;pre&gt;count&lt;/pre&gt;"},
				Range:    Range{Start: Position{Line: 1, Character: 2}, End: Position{Line: 1, Character: 4}},
			},
			want: &Hover{
				Contents: MarkupContent{Kind: PlainText, Value: "<pre>count</pre>"},
				Range:    Range{Start: Position{Line: 1, Character: 2}, End: Position{Line: 1, Character: 4}},
			},
		},
		{
			name: "KeepsSupportedMarkdown",
			capabilities: HoverClientCapabilities{
				ContentFormat: []MarkupKind{Markdown},
			},
			hover: &Hover{
				Contents: MarkupContent{Kind: Markdown, Value: "**count**"},
			},
			want: &Hover{
				Contents: MarkupContent{Kind: Markdown, Value: "**count**"},
			},
		},
		{
			name: "UsesPreferredPlainText",
			capabilities: HoverClientCapabilities{
				ContentFormat: []MarkupKind{PlainText, Markdown},
			},
			hover: &Hover{
				Contents: MarkupContent{Kind: Markdown, Value: "**count**"},
			},
			want: &Hover{
				Contents: MarkupContent{Kind: PlainText, Value: "**count**"},
			},
		},
		{
			name: "KeepsNilHover",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, adaptHoverMarkupForClient(tt.capabilities, tt.hover))
		})
	}
}

func TestAdaptCompletionDocumentationForClient(t *testing.T) {
	for _, tt := range []struct {
		name          string
		capabilities  CompletionClientCapabilities
		documentation *Or_CompletionItem_documentation
		want          *Or_CompletionItem_documentation
	}{
		{
			name: "DowngradesUnsupportedMarkdown",
			documentation: &Or_CompletionItem_documentation{
				Value: MarkupContent{Kind: Markdown, Value: "&lt;pre&gt;count&lt;/pre&gt;"},
			},
			want: &Or_CompletionItem_documentation{Value: "<pre>count</pre>"},
		},
		{
			name: "KeepsSupportedMarkdown",
			capabilities: CompletionClientCapabilities{
				CompletionItem: protocol.ClientCompletionItemOptions{
					DocumentationFormat: []MarkupKind{Markdown},
				},
			},
			documentation: &Or_CompletionItem_documentation{
				Value: MarkupContent{Kind: Markdown, Value: "**count**"},
			},
			want: &Or_CompletionItem_documentation{
				Value: MarkupContent{Kind: Markdown, Value: "**count**"},
			},
		},
		{
			name: "UsesPreferredPlainText",
			capabilities: CompletionClientCapabilities{
				CompletionItem: protocol.ClientCompletionItemOptions{
					DocumentationFormat: []MarkupKind{PlainText, Markdown},
				},
			},
			documentation: &Or_CompletionItem_documentation{
				Value: MarkupContent{Kind: Markdown, Value: "**count**"},
			},
			want: &Or_CompletionItem_documentation{Value: "**count**"},
		},
		{
			name:          "KeepsStringDocumentation",
			documentation: &Or_CompletionItem_documentation{Value: "count"},
			want:          &Or_CompletionItem_documentation{Value: "count"},
		},
		{
			name: "KeepsNilDocumentation",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, adaptCompletionDocumentationForClient(tt.capabilities, tt.documentation))
		})
	}
}
