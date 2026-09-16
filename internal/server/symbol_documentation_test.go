package server

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerSymbolDocumentation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		label  string
		doc    string
		id     string
	}{
		{name: "Alias", source: "// Value is documented.\ntype Value = int\nvar value |Value\n", label: "Value", doc: "Value is documented."},
		{name: "Slice", source: "// Value is documented.\ntype Value []int\nvar value |Value\n", label: "Value", doc: "Value is documented."},
		{name: "Interface", source: "// Value is documented.\ntype Value interface { Read() int }\nvar value |Value\n", label: "Value", doc: "Value is documented."},
		{name: "InterfaceMethod", source: "type Reader interface {\n// Read retrieves a value.\nRead() int\n}\nvar value Reader\necho value.|read()\n", label: "Read", doc: "Read retrieves a value.", id: "xgo:main?Reader.Read"},
		{name: "DefinedInterfaceMethod", source: "type Reader interface {\n// Read retrieves a value.\nRead() int\n}\ntype Adapter Reader\nvar value Adapter\necho value.|read()\n", label: "Read", doc: "Read retrieves a value.", id: "xgo:main?Reader.Read"},
		{name: "EmbeddedInterfaceMethod", source: "type Reader interface {\n// Read retrieves a value.\nRead() int\n}\ntype Adapter interface { Reader }\nvar value Adapter\necho value.|read()\n", label: "Read", doc: "Read retrieves a value.", id: "xgo:main?Reader.Read"},
		{name: "EmbeddedPointer", source: "type Item struct{}\ntype Group struct {\n// Item is the active item.\n*Item\n}\nvar value Group\necho value.|Item\n", label: "Item", doc: "Item is the active item."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: position,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, tt.doc)
			if tt.id != "" {
				assert.Contains(t, hover.Contents.Value, `def-id="`+tt.id+`"`)
			}
			position.Character++
			item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", position), tt.label)
			require.NotNil(t, item)
			require.NotNil(t, item.Documentation)
			assert.Contains(t, requireValueAs[MarkupContent](t, item.Documentation.Value).Value, tt.doc)
			if tt.id != "" {
				data := requireValueAs[*CompletionItemData](t, item.Data)
				require.NotNil(t, data.Definition)
				assert.Equal(t, tt.id, data.Definition.String())
			}
		})
	}
}

func TestServerSignatureDocumentation(t *testing.T) {
	t.Run("Formats", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			capabilities *protocol.SignatureHelpClientCapabilities
			initialized  bool
			markdown     bool
		}{
			{name: "BeforeInitialize"},
			{name: "AbsentCapability", initialized: true},
			{name: "AbsentSignatureInformation", initialized: true, capabilities: &protocol.SignatureHelpClientCapabilities{}},
			{name: "EmptyFormats", initialized: true, capabilities: &protocol.SignatureHelpClientCapabilities{SignatureInformation: &protocol.ClientSignatureInformationOptions{}}},
			{name: "PlainText", initialized: true, capabilities: &protocol.SignatureHelpClientCapabilities{SignatureInformation: &protocol.ClientSignatureInformationOptions{DocumentationFormat: []MarkupKind{PlainText, Markdown}}}},
			{name: "Markdown", initialized: true, markdown: true, capabilities: &protocol.SignatureHelpClientCapabilities{SignatureInformation: &protocol.ClientSignatureInformationOptions{DocumentationFormat: []MarkupKind{Markdown, PlainText}}}},
			{name: "UnknownFormat", initialized: true, capabilities: &protocol.SignatureHelpClientCapabilities{SignatureInformation: &protocol.ClientSignatureInformationOptions{DocumentationFormat: []MarkupKind{"unknown"}}}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, position := typeDisplayTestSource(t, "// Add returns the next value.\nfunc add(value int) int { return value + 1 }\nadd(|1)\n")
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				if tt.initialized {
					_, err := s.initialize(&InitializeParams{XInitializeParams: protocol.XInitializeParams{
						Capabilities: ClientCapabilities{TextDocument: protocol.TextDocumentClientCapabilities{SignatureHelp: tt.capabilities}},
					}})
					require.NoError(t, err)
					s.finishInitialize(nil)
				}
				help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: position,
				}})
				require.NoError(t, err)
				require.NotNil(t, help)
				require.Len(t, help.Signatures, 1)
				require.NotNil(t, help.Signatures[0].Documentation)
				if tt.markdown {
					assert.Equal(t, MarkupContent{Kind: Markdown, Value: "Add returns the next value."}, help.Signatures[0].Documentation.Value)
				} else {
					assert.Equal(t, "Add returns the next value.", help.Signatures[0].Documentation.Value)
				}
			})
		}
	})

	t.Run("SourceUpdates", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			declarations string
			call         string
		}{
			{name: "Function", declarations: "// MESSAGE\nfunc use(value int) {}\n", call: "use(|1)\n"},
			{name: "Method", declarations: "type Item struct{}\n// MESSAGE\nfunc (item *Item) Use(value int) {}\n", call: "var item Item\nitem.use(|1)\n"},
			{name: "InterfaceMethod", declarations: "type Item interface {\n// MESSAGE\nUse(value int)\n}\n", call: "var item Item\nitem.use(|1)\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, position := typeDisplayTestSource(t, tt.call)
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source), "declarations.xgo": nil})
				for version, doc := range []string{"First documentation.", "Second documentation.", ""} {
					s.ModifyFiles([]FileChange{{Path: "declarations.xgo", Content: []byte(strings.ReplaceAll(tt.declarations, "MESSAGE", doc)), Version: version + 1}})
					_, err := s.getProj().TypeInfo()
					require.NoError(t, err)
					help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: position,
					}})
					require.NoError(t, err)
					require.NotNil(t, help)
					require.Len(t, help.Signatures, 1)
					if doc == "" {
						assert.Nil(t, help.Signatures[0].Documentation)
					} else {
						require.NotNil(t, help.Signatures[0].Documentation)
						assert.Equal(t, doc, help.Signatures[0].Documentation.Value)
					}
				}
			})
		}
	})

	t.Run("PackageUpdates", func(t *testing.T) {
		source, position := typeDisplayTestSource(t, "import f \"example.com/framework\"\nf.use(|nil)\n")
		s := newFrameworkTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		for _, text := range []string{"First documentation.", "Second documentation.", ""} {
			doc := testframework.NewPkgDoc(t)
			doc.Funcs["Use"] = text
			s.lookupPkgDoc = func(pkgPath string) (*pkgdoc.PkgDoc, error) {
				if pkgPath == testframework.PkgPath && text != "" {
					return doc, nil
				}
				return nil, fs.ErrNotExist
			}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: position,
			}})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			if text == "" {
				assert.Nil(t, help.Signatures[0].Documentation)
			} else {
				require.NotNil(t, help.Signatures[0].Documentation)
				assert.Equal(t, text, help.Signatures[0].Documentation.Value)
			}
		}
	})

	t.Run("FrameworkOverload", func(t *testing.T) {
		source, position := typeDisplayTestSource(t, "measure |missing\n")
		srv := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
		_, err := srv.getProj().TypeInfo()
		require.ErrorContains(t, err, "undefined: missing")
		help, err := srv.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: srv.toDocumentURI("main_fixture.gox")}, Position: position,
		}})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 2)
		for i, doc := range []string{"Measure__0 is the integer overload of Measure.", "Measure__1 is the string overload of Measure."} {
			require.NotNil(t, help.Signatures[i].Documentation)
			assert.Equal(t, doc, help.Signatures[i].Documentation.Value)
		}
	})

	t.Run("ScopedInterfaceMethods", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
			doc    string
		}{
			{
				name:   "ShadowedInterface",
				source: "type Reader interface {\n// Global read.\nRead(n int) int\n}\nfunc run() {\ntype Reader interface {\n// Local read.\nRead(n int) int\n}\nvar value Reader\necho value.|Read(1)\n}\n",
				doc:    "Local read.",
			},
			{
				name:   "AnonymousInterface",
				source: "// Read is an unrelated function.\nfunc Read(n int) int { return n }\nvar value interface {\n// Anonymous read.\nRead(n int) int\n}\necho value.|Read(1)\n",
				doc:    "Anonymous read.",
			},
			{
				name:   "UndocumentedAnonymousInterface",
				source: "// Read is an unrelated function.\nfunc Read(n int) int { return n }\nvar value interface { Read(n int) int }\necho value.|Read(1)\n",
			},
			{
				name:   "NestedInterface",
				source: "type Outer interface {\n// Global read.\nRead(n int) int\nInner() interface {\n// Nested read.\nRead(n int) int\n}\n}\nvar value Outer\necho value.Inner().|Read(1)\n",
				doc:    "Nested read.",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, position := typeDisplayTestSource(t, tt.source)
				srv := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				_, err := srv.getProj().TypeInfo()
				require.NoError(t, err)
				hover, err := srv.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: srv.toDocumentURI("main.xgo")}, Position: position,
				}})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, tt.doc)
				assert.NotContains(t, hover.Contents.Value, "Global read.")
				assert.NotContains(t, hover.Contents.Value, "unrelated function")
				item := completionItemByLabel(completionItemsAt(t, srv, "main.xgo", position), "Read")
				require.NotNil(t, item)
				require.NotNil(t, item.Documentation)
				doc := requireValueAs[MarkupContent](t, item.Documentation.Value).Value
				assert.Contains(t, doc, tt.doc)
				assert.NotContains(t, doc, "Global read.")
				assert.NotContains(t, doc, "unrelated function")

				position.Character += 5
				help, err := srv.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: srv.toDocumentURI("main.xgo")}, Position: position,
				}})
				require.NoError(t, err)
				require.NotNil(t, help)
				require.Len(t, help.Signatures, 1)
				if tt.doc == "" {
					assert.Nil(t, help.Signatures[0].Documentation)
				} else {
					require.NotNil(t, help.Signatures[0].Documentation)
					assert.Equal(t, tt.doc, help.Signatures[0].Documentation.Value)
				}
			})
		}
	})

	t.Run("Overloads", func(t *testing.T) {
		const declarations = `// Integer documentation.
func handleInt(value int) {}
// String documentation.
func handleString(value string) {}
func handle = (
	handleInt
	handleString
)
`
		for _, tt := range []struct {
			name string
			arg  string
			want []string
		}{
			{name: "Resolved", arg: "1", want: []string{"Integer documentation."}},
			{name: "Unresolved", arg: "missing", want: []string{"Integer documentation.", "String documentation."}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, position := typeDisplayTestSource(t, "handle |"+tt.arg+"\n")
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source), "functions.xgo": []byte(declarations)})
				help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: position,
				}})
				require.NoError(t, err)
				require.NotNil(t, help)
				require.Len(t, help.Signatures, len(tt.want))
				for i, doc := range tt.want {
					require.NotNil(t, help.Signatures[i].Documentation)
					assert.Equal(t, doc, help.Signatures[i].Documentation.Value)
				}
			})
		}
	})
}

func TestServerScopedSymbolDocumentation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		label  string
		want   string
	}{
		{name: "LocalStructField", source: "type Item struct {\n// Package doc.\nValue int\n}\nfunc run() {\ntype Item struct {\n// Local doc.\nValue int\n}\nvar item Item\necho item.|Value\n}\n", label: "Value", want: "Local doc."},
		{name: "LocalVariable", source: "// Package doc.\nvar value int\nfunc run() {\n// Local doc.\nvar value int\necho |value\n}\n", label: "value", want: "Local doc."},
		{name: "UndocumentedLocalVariable", source: "// Package doc.\nvar value int\nfunc run() {\nvar value int\necho |value\n}\n", label: "value"},
		{name: "ShortVariable", source: "// Package doc.\nvar value int\n// Enclosing function documentation.\nfunc run() {\nvalue := 1\necho |value\n}\n", label: "value"},
		{name: "RangeVariable", source: "// Package doc.\nvar value int\nfunc run() {\nfor value <- [1, 2] {\necho |value\n}\n}\n", label: "value"},
		{name: "NamedResult", source: "// Package doc.\nvar value int\n// Enclosing function documentation.\nfunc run() (value int) {\nreturn |value\n}\n", label: "value"},
		{name: "LocalConst", source: "// Package doc.\nconst Limit = 1\nfunc run() {\n// Local doc.\nconst Limit = 2\necho |Limit\n}\n", label: "Limit", want: "Local doc."},
		{name: "LocalType", source: "// Package doc.\ntype Item int\nfunc run() {\n// Local doc.\ntype Item string\nvar item |Item\necho item\n}\n", label: "Item", want: "Local doc."},
		{name: "LocalAlias", source: "// Package doc.\ntype Item = int\nfunc run() {\n// Local doc.\ntype Item = string\nvar item |Item\necho item\n}\n", label: "Item", want: "Local doc."},
		{name: "Parameter", source: "// Package doc.\nvar value int\nfunc run(value int) {\necho |value\n}\n", label: "value"},
		{name: "AnonymousStructField", source: "// Package doc.\nvar Value int\nvar item struct {\n// Local doc.\nValue int\n}\necho item.|Value\n", label: "Value", want: "Local doc."},
		{name: "NestedStructField", source: "type Item struct {\n// Package doc.\nValue int\nNested struct {\n// Local doc.\nValue int\n}\n}\nvar item Item\necho item.Nested.|Value\n", label: "Value", want: "Local doc."},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			srv := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := srv.getProj().TypeInfo()
			require.NoError(t, err)
			hover, err := srv.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: srv.toDocumentURI("main.xgo")}, Position: position,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			if tt.want != "" {
				assert.Contains(t, hover.Contents.Value, tt.want, "hover declaration documentation")
			}
			assert.NotContains(t, hover.Contents.Value, "Package doc.", "hover must not borrow documentation")
			assert.NotContains(t, hover.Contents.Value, "Enclosing function documentation.")
			position.Character++
			item := completionItemByLabel(completionItemsAt(t, srv, "main.xgo", position), tt.label)
			require.NotNil(t, item)
			require.NotNil(t, item.Documentation)
			doc := requireValueAs[MarkupContent](t, item.Documentation.Value).Value
			if tt.want != "" {
				assert.Contains(t, doc, tt.want, "completion declaration documentation")
			}
			assert.NotContains(t, doc, "Package doc.", "completion must not borrow documentation")
			assert.NotContains(t, doc, "Enclosing function documentation.")
		})
	}
}

func TestServerSymbolDocumentationUpdates(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		label  string
	}{
		{name: "LocalVariable", source: "// Package documentation.\nvar value int\nfunc run() {\n// MESSAGE\nvar value int\necho |value\n}\n", label: "value"},
		{name: "LocalConst", source: "// Package documentation.\nconst Limit = 1\nfunc run() {\n// MESSAGE\nconst Limit = 2\necho |Limit\n}\n", label: "Limit"},
		{name: "LocalType", source: "// Package documentation.\ntype Item int\nfunc run() {\n// MESSAGE\ntype Item string\nvar item |Item\necho item\n}\n", label: "Item"},
		{name: "LocalField", source: "type Item struct {\n// Package documentation.\nValue int\n}\nfunc run() {\ntype Item struct {\n// MESSAGE\nValue int\n}\nvar item Item\necho item.|Value\n}\n", label: "Value"},
		{name: "DefinedTypeField", source: "type Item struct {\n// MESSAGE\nValue int\n}\ntype Copy Item\nvar item Copy\necho item.|Value\n", label: "Value"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": nil})
			for version, doc := range []string{"First documentation.", "Second documentation.", ""} {
				source, position := typeDisplayTestSource(t, strings.ReplaceAll(tt.source, "MESSAGE", doc))
				s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(source), Version: version + 1}})
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: position,
				}})
				require.NoError(t, err)
				require.NotNil(t, hover)
				position.Character++
				item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", position), tt.label)
				require.NotNil(t, item)
				require.NotNil(t, item.Documentation)
				for _, content := range []string{hover.Contents.Value, requireValueAs[MarkupContent](t, item.Documentation.Value).Value} {
					assert.NotContains(t, content, "Package documentation.")
					for _, message := range []string{"First documentation.", "Second documentation."} {
						if message == doc {
							assert.Contains(t, content, message)
						} else {
							assert.NotContains(t, content, message)
						}
					}
				}
			}
		})
	}
}
