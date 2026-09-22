package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerStructMemberCompletion(t *testing.T) {
	const reader = "type Reader interface {\n// Read retrieves a value.\nRead(n int) int\n}\n"
	for _, tt := range []struct {
		name      string
		source    string
		label     string
		overview  string
		doc       string
		id        string
		framework bool
	}{
		{name: "UnnamedStruct", source: "var item struct {\n// Value stores the value.\nValue int\n}\necho item.|Value\n", label: "Value", overview: "field Value int", doc: "Value stores the value.", id: "xgo:main?Value"},
		{name: "UnnamedPointer", source: "var item *struct { Value int }\necho item.|Value\n", label: "Value", overview: "field Value int", id: "xgo:main?Value"},
		{name: "UnnamedAlias", source: "type Item = struct { Value int }\nvar item Item\necho item.|Value\n", label: "Value", overview: "field Value int", id: "xgo:main?Item.Value"},
		{name: "UnnamedPointerAlias", source: "type Item = *struct { Value int }\nvar item Item\necho item.|Value\n", label: "Value", overview: "field Value int", id: "xgo:main?Value"},
		{name: "UnnamedCallResult", source: "func item() struct { Value int } { return struct { Value int }{} }\necho item().|Value\n", label: "Value", overview: "field Value int", id: "xgo:main?Value"},
		{name: "NestedStruct", source: "type Item struct { Nested struct { Value int } }\nvar item Item\necho item.Nested.|Value\n", label: "Value", overview: "field Value int", id: "xgo:main?Value"},
		{name: "EmbeddedInterface", source: reader + "type Item struct { Reader }\nvar item Item\necho item.|Read(1)\n", label: "Read", overview: "func Read(n int) int", doc: "Read retrieves a value.", id: "xgo:main?Reader.Read"},
		{name: "AliasedStruct", source: reader + "type Item struct { Reader }\ntype Alias = Item\nvar item Alias\necho item.|Read(1)\n", label: "Read", overview: "func Read(n int) int", doc: "Read retrieves a value.", id: "xgo:main?Reader.Read"},
		{name: "NestedEmbeddedInterface", source: reader + "type Nested interface { Reader }\ntype Item struct { Nested }\nvar item Item\necho item.|Read(1)\n", label: "Read", overview: "func Read(n int) int", doc: "Read retrieves a value.", id: "xgo:main?Reader.Read"},
		{name: "UnnamedEmbeddedInterface", source: reader + "var item struct { Reader }\necho item.|Read(1)\n", label: "Read", overview: "func Read(n int) int", doc: "Read retrieves a value.", id: "xgo:main?Reader.Read"},
		{name: "ImportedInterface", source: "import f \"example.com/framework\"\ntype Item struct { f.Reader }\nvar item Item\necho item.|Read(1)\n", label: "read", overview: "func read(n int) int", doc: "Read retrieves a value.", id: "xgo:example.com/framework?Reader.read", framework: true},
		{name: "EmbeddedScalarMethod", source: "type Value int\n// Read retrieves a value.\nfunc (v Value) Read(n int) int { return n }\ntype Item struct { Value }\nvar item Item\necho item.|Read(1)\n", label: "Read", overview: "func Read(n int) int", doc: "Read retrieves a value.", id: "xgo:main?Value.Read"},
		{name: "DirectFieldShadowsInterfaceMethod", source: reader + "type Item struct { Reader; Read string }\nvar item Item\necho item.|Read\n", label: "Read", overview: "field Read string", id: "xgo:main?Item.Read"},
		{name: "DirectMethodShadowsInterfaceMethod", source: reader + "type Item struct { Reader }\n// Read returns text.\nfunc (item Item) Read(n int) string { return \"value\" }\nvar item Item\necho item.|Read(1)\n", label: "Read", overview: "func Read(n int) string", doc: "Read returns text.", id: "xgo:main?Item.Read"},
		{name: "XGoDepthFirstLookup", source: "type Deep struct { Value string }\ntype Left struct { Deep }\ntype Right struct { Value int }\ntype Item struct { Left; Right }\nvar item Item\necho item.|Value\n", label: "Value", overview: "field Value string", id: "xgo:main?Deep.Value"},
		{name: "XGoSiblingLookup", source: "type Left struct { Value string }\ntype Right struct { Value int }\ntype Item struct { Left; Right }\nvar item Item\necho item.|Value\n", label: "Value", overview: "field Value string", id: "xgo:main?Left.Value"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			newServer := newTestServer
			if tt.framework {
				newServer = newFrameworkTestServer
			}
			s := newServer(t, map[string][]byte{"main.xgo": []byte(source)})
			info, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			file, err := s.getProj().ASTFile("main.xgo")
			require.NoError(t, err)
			_, obj, _ := objectAtPosition(s.getProj(), info, file, ToPosition(s.getProj(), file, position))
			require.NotNil(t, obj)
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: position,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `def-id="`+tt.id+`"`)
			assert.Contains(t, hover.Contents.Value, tt.overview)
			position.Character++
			items := completionItemsAt(t, s, "main.xgo", position)
			var matches []CompletionItem
			for _, item := range items {
				if item.Label == tt.label {
					matches = append(matches, item)
				}
			}
			require.Len(t, matches, 1)
			item := matches[0]
			data := requireValueAs[*XGoCompletionItemData](t, item.Data)
			require.NotNil(t, data.Definition)
			assert.Equal(t, tt.id, data.Definition.String())
			require.NotNil(t, item.Documentation)
			doc := requireValueAs[MarkupContent](t, item.Documentation.Value).Value
			assert.Contains(t, doc, tt.overview)
			if tt.doc != "" {
				assert.Contains(t, doc, tt.doc)
				assert.Contains(t, hover.Contents.Value, tt.doc)
			}
			if strings.HasPrefix(tt.overview, "field ") {
				assert.Equal(t, FieldCompletion, item.Kind)
			} else {
				assert.Equal(t, FunctionCompletion, item.Kind)
				position.Character += uint32(UTF16Len(tt.label))
				help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: position,
				}})
				require.NoError(t, err)
				require.NotNil(t, help)
				require.Len(t, help.Signatures, 1)
				require.NotNil(t, help.Signatures[0].Documentation)
				assert.Equal(t, tt.doc, help.Signatures[0].Documentation.Value)
			}
		})
	}
}

func TestServerUnnamedStructLiteralCompletion(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		want   []string
	}{
		{name: "Direct", source: "item := struct {\n// Value stores the value.\nValue int\n}{|}\necho item\n", want: []string{"Value"}},
		{name: "Pointer", source: "item := &struct { Value int }{|}\necho item\n", want: []string{"Value"}},
		{name: "Alias", source: "type Item = struct { Value int }\nitem := Item{|}\necho item\n", want: []string{"Value"}},
		{name: "UsedFields", source: "item := struct { Value int; Other int }{Value: 1, |}\necho item\n", want: []string{"Other"}},
		{name: "EmbeddedFields", source: "type Base struct { Promoted int }\nitem := struct { Base; Value int }{|}\necho item\n", want: []string{"Base", "Value"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, "main.xgo", position)
			assert.ElementsMatch(t, tt.want, completionItemLabels(items))
			for _, item := range items {
				assert.Equal(t, FieldCompletion, item.Kind)
				assert.Equal(t, item.Label+": ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
			}
			if tt.name == "Direct" {
				item := completionItemByLabel(items, "Value")
				require.NotNil(t, item)
				require.NotNil(t, item.Documentation)
				assert.Contains(t, requireValueAs[MarkupContent](t, item.Documentation.Value).Value, "Value stores the value.")
			}
		})
	}

	t.Run("FieldValue", func(t *testing.T) {
		source, position := typeDisplayTestSource(t, "value := 1\nitem := struct { Value int }{Value: |value}\necho item\n")
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		_, err := s.getProj().TypeInfo()
		require.NoError(t, err)
		items := completionItemsAt(t, s, "main.xgo", position)
		item := completionItemByLabel(items, "value")
		require.NotNil(t, item)
		assert.Equal(t, VariableCompletion, item.Kind)
		assert.Equal(t, "value", item.InsertText)
	})
}
