package server

import (
	"testing"

	"github.com/goplus/xgolsw/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreferredMarkupKind(t *testing.T) {
	for _, tt := range []struct {
		name    string
		formats []MarkupKind
		want    MarkupKind
	}{
		{
			name: "DefaultsToPlainText",
			want: PlainText,
		},
		{
			name:    "UsesMarkdown",
			formats: []MarkupKind{Markdown},
			want:    Markdown,
		},
		{
			name:    "HonorsClientPreference",
			formats: []MarkupKind{PlainText, Markdown},
			want:    PlainText,
		},
		{
			name:    "SkipsUnknownKinds",
			formats: []MarkupKind{"custom", Markdown},
			want:    Markdown,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, preferredMarkupKind(tt.formats))
		})
	}
}

func TestResourceMarkupContent(t *testing.T) {
	uri := XGoResourceURI("spx://resources/sounds/MySound")
	assert.Equal(t, MarkupContent{
		Kind:  Markdown,
		Value: "<resource-preview resource=\"spx://resources/sounds/MySound\" />\n",
	}, resourceMarkupContent(uri, Markdown))
	assert.Equal(t, MarkupContent{
		Kind:  PlainText,
		Value: "spx://resources/sounds/MySound",
	}, resourceMarkupContent(uri, PlainText))
}

func TestSpxDefinitionMarkupContent(t *testing.T) {
	def := SpxDefinition{
		ID:       SpxDefinitionIdentifier{Name: ToPtr("count")},
		Overview: "var count int",
		Detail:   "count is a variable.\n",
	}
	assert.Equal(t, MarkupContent{
		Kind:  Markdown,
		Value: "<pre is=\"definition-item\" def-id=\"xgo:?count\" overview=\"var count int\">\ncount is a variable.\n</pre>\n",
	}, def.markupContent(Markdown))
	assert.Equal(t, MarkupContent{
		Kind:  PlainText,
		Value: "var count int\n\ncount is a variable.",
	}, def.markupContent(PlainText))
}

func TestCompletionDocumentation(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content MarkupContent
		want    *Or_CompletionItem_documentation
	}{
		{
			name:    "Markdown",
			content: MarkupContent{Kind: Markdown, Value: "**count**"},
			want: &Or_CompletionItem_documentation{
				Value: MarkupContent{Kind: Markdown, Value: "**count**"},
			},
		},
		{
			name:    "PlainText",
			content: MarkupContent{Kind: PlainText, Value: "count"},
			want:    &Or_CompletionItem_documentation{Value: "count"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, completionDocumentation(tt.content))
		})
	}
}

func TestServerTextDocumentHoverPlainTextEnum(t *testing.T) {
	files := map[string][]byte{
		"main.spx": []byte(`type Color const (
	// Red documentation.
	Red = iota
)
`),
	}
	server := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})
	_, err := server.initialize(&InitializeParams{
		XInitializeParams: protocol.XInitializeParams{
			Capabilities: protocol.ClientCapabilities{
				TextDocument: protocol.TextDocumentClientCapabilities{
					Hover: &protocol.HoverClientCapabilities{
						ContentFormat: []protocol.MarkupKind{protocol.PlainText},
					},
				},
			},
		},
	})
	require.NoError(t, err)
	server.finishInitialize(nil)

	hover, err := server.textDocumentHover(&HoverParams{
		TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
			Position:     Position{Line: 2, Character: 1},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, hover)
	assert.Equal(t, PlainText, hover.Contents.Kind)
	assert.Contains(t, hover.Contents.Value, "Red documentation.")
	assert.NotContains(t, hover.Contents.Value, "<pre")
}
