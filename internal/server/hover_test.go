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

func TestServerTextDocumentHover(t *testing.T) {
	t.Run("FieldOwners", func(t *testing.T) {
		const record = "type Record struct {\n// Count stores the value.\nCount int\n}\n"
		for _, tt := range []struct {
			name      string
			filename  string
			source    string
			extra     map[string][]byte
			field     string
			id        string
			doc       string
			target    string
			newServer testServerFactory
		}{
			{
				name: "DirectField", filename: "main.xgo", newServer: newTestServer,
				source: record + "type Adapter struct { Record }\nvar item Record\nitem.|Count = 1\n",
				field:  "Count", id: "xgo:main?Record.Count", doc: "Count stores the value.", target: "Record",
			},
			{
				name: "DefinedSibling", filename: "main.xgo", newServer: newTestServer,
				source: record + "type Adapter Record\nvar item Record\nitem.|Count = 1\n",
				field:  "Count", id: "xgo:main?Record.Count", doc: "Count stores the value.", target: "Record",
			},
			{
				name: "DefinedType", filename: "main.xgo", newServer: newTestServer,
				source: record + "type Adapter Record\nvar item Adapter\nitem.|Count = 1\n",
				field:  "Count", id: "xgo:main?Adapter.Count", target: "Adapter",
			},
			{
				name: "CallReceiver", filename: "main.xgo", newServer: newTestServer,
				source: record + "type Adapter Record\nfunc item() *Adapter { return new(Adapter) }\nitem().|Count = 1\n",
				field:  "Count", id: "xgo:main?Adapter.Count", target: "Adapter",
			},
			{
				name: "IndexReceiver", filename: "main.xgo", newServer: newTestServer,
				source: record + "type Adapter Record\nitems := [Adapter{}]\nitems[0].|Count = 1\n",
				field:  "Count", id: "xgo:main?Adapter.Count", target: "Adapter",
			},
			{
				name: "LocalDefinedSibling", filename: "main.xgo", newServer: newTestServer,
				source: "func run() {\n" + record + "type Adapter Record\nvar item Record\nitem.|Count = 1\n}\n",
				field:  "Count", id: "xgo:main?Record.Count",
			},
			{
				name: "PromotedField", filename: "main.xgo", newServer: newTestServer,
				source: record + "type Adapter struct { *Record }\nvar item Adapter\nitem.|Count = 1\n",
				field:  "Count", id: "xgo:main?Record.Count", doc: "Count stores the value.", target: "Adapter",
			},
			{
				name: "PointerAlias", filename: "main.xgo", newServer: newTestServer,
				source: record + "type Adapter struct { Record }\ntype Alias = *Record\nvar item Alias\nitem.|Count = 1\n",
				field:  "Count", id: "xgo:main?Record.Count", doc: "Count stores the value.", target: "Adapter",
			},
			{
				name: "ShadowedField", filename: "main.xgo", newServer: newTestServer,
				source: record + "type Adapter struct {\nRecord\n// Count stores the replacement.\nCount int\n}\nvar item Adapter\nitem.|Count = 1\n",
				field:  "Count", id: "xgo:main?Adapter.Count", doc: "Count stores the replacement.", target: "Adapter",
			},
			{
				name: "LocalPointerAlias", filename: "main.xgo", newServer: newTestServer,
				source: "func run() {\n" + record + "type Alias = *Record\nvar item Alias\nitem.|Count = 1\n}\n",
				field:  "Count", id: "xgo:main?Record.Count",
			},
			{
				name: "LocalType", filename: "main.xgo", newServer: newTestServer,
				source: "func run() {\n" + record + "type Adapter struct { Record }\nvar item Adapter\nitem.|Count = 1\n}\n",
				field:  "Count", id: "xgo:main?Record.Count",
			},
			{
				name: "ProjectField", filename: "main_fixture.gox", newServer: newFrameworkTestServer,
				source: "// Count stores the value.\nvar Count int\n|Count = 1\n",
				extra:  map[string][]byte{"helpers.xgo": []byte("type Adapter struct { *App }\n")},
				field:  "Count", id: "xgo:main?App.Count", doc: "Count stores the value.", target: "App",
			},
			{
				name: "WorkField", filename: "Worker_fixture.gox", newServer: newFrameworkTestServer,
				source: "// Count stores the value.\nvar Count int\n|Count = 1\n",
				extra:  map[string][]byte{"main_fixture.gox": nil, "helpers.xgo": []byte("type Adapter struct { *Worker }\n")},
				field:  "Count", id: "xgo:main?Worker.Count", doc: "Count stores the value.", target: "Worker",
			},
			{
				name: "ProjectDefinedSibling", filename: "main_fixture.gox", newServer: newFrameworkTestServer,
				source: "// Count stores the value.\nvar Count int\n|Count = 1\n",
				extra:  map[string][]byte{"helpers.xgo": []byte("type Adapter App\n")},
				field:  "Count", id: "xgo:main?App.Count", doc: "Count stores the value.", target: "App",
			},
			{
				name: "WorkDefinedSibling", filename: "Worker_fixture.gox", newServer: newFrameworkTestServer,
				source: "// Count stores the value.\nvar Count int\n|Count = 1\n",
				extra:  map[string][]byte{"main_fixture.gox": nil, "helpers.xgo": []byte("type Adapter Worker\n")},
				field:  "Count", id: "xgo:main?Worker.Count", doc: "Count stores the value.", target: "Worker",
			},
			{
				name: "ImportedImplicitField", filename: "Worker_fixture.gox", newServer: newFrameworkTestServer,
				source: "|Value = 1\n",
				extra:  map[string][]byte{"main_fixture.gox": nil},
				field:  "Value", id: "xgo:example.com/framework?Item.Value", doc: "Value stores the work item's value.",
			},
			{
				name: "ImportedDefinedType", filename: "main.xgo", newServer: newFrameworkTestServer,
				source: "import f \"example.com/framework\"\nvar item f.Copy\nitem.|Value = 1\n",
				field:  "Value", id: "xgo:example.com/framework?Copy.Value",
			},
			{
				name: "ImportedGenericField", filename: "main.xgo", newServer: newFrameworkTestServer,
				source: "import f \"example.com/framework\"\nvar item f.Box[int]\nitem.|Value = 1\n",
				field:  "Value", id: "xgo:example.com/framework?Box.Value", doc: "Value holds the boxed value.",
			},
			{
				name: "ImportedField", filename: "main.xgo", newServer: newFrameworkTestServer,
				source: "import f \"example.com/framework\"\nvar item f.Item\nitem.|Value = 1\n",
				field:  "Value", id: "xgo:example.com/framework?Item.Value", doc: "Value stores the work item's value.",
			},
			{
				name: "ImportedPromotedField", filename: "main.xgo", newServer: newFrameworkTestServer,
				source: "import f \"example.com/framework\"\nvar item f.Group\nitem.|Value = 1\n",
				field:  "Value", id: "xgo:example.com/framework?Item.Value", doc: "Value stores the work item's value.",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				before, after, ok := strings.Cut(tt.source, "|")
				require.True(t, ok)
				files := map[string][]byte{tt.filename: []byte(before + after)}
				for filename, content := range tt.extra {
					files[filename] = content
				}
				s := tt.newServer(t, files)
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				position := Position{Line: uint32(strings.Count(before, "\n")), Character: uint32(UTF16Len(before[strings.LastIndex(before, "\n")+1:])) + 1}
				hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}, Position: position,
				}})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, `def-id="`+tt.id+`"`)
				if tt.doc != "" {
					assert.Contains(t, hover.Contents.Value, tt.doc)
				}
				links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}})
				require.NoError(t, err)
				assert.Contains(t, links, DocumentLink{
					Range: hover.Range, Target: ToPtr(URI(tt.id)),
				})
				item := completionItemByLabel(completionItemsAt(t, s, tt.filename, position), tt.field)
				require.NotNil(t, item)
				data := requireValueAs[*XGoCompletionItemData](t, item.Data)
				require.NotNil(t, data.Definition)
				assert.Equal(t, tt.id, data.Definition.String())
				if tt.doc != "" {
					require.NotNil(t, item.Documentation)
					assert.Contains(t, requireValueAs[MarkupContent](t, item.Documentation.Value).Value, tt.doc)
				}
				if tt.target != "" {
					properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: tt.target})
					require.NoError(t, err)
					var found bool
					for _, property := range properties {
						if property.Name == tt.field {
							found = true
							assert.Equal(t, tt.id, property.Definition.String())
							assert.Contains(t, property.Doc, tt.doc)
						}
					}
					assert.True(t, found)
				}
			})
		}
	})

	t.Run("KwargFieldOwners", func(t *testing.T) {
		const declarations = "type Record struct {\n// Count stores the value.\nCount int\n}\ntype Adapter Record\n"
		const overloads = declarations + "func configureRecord(prefix string, opts Record?) {}\nfunc configureAdapter(prefix int, opts Adapter?) {}\nfunc configure = (\nconfigureRecord\nconfigureAdapter\n)\n"
		for _, tt := range []struct {
			name         string
			declarations string
			call         string
			label        string
			id           string
			doc          string
			newServer    testServerFactory
		}{
			{
				name:         "DefinedSibling",
				declarations: declarations + "func configure(opts Record?) {}\n", call: "configure ",
				label: "count", id: "xgo:main?Record.Count", doc: "Count stores the value.",
				newServer: newTestServer,
			},
			{
				name:         "DefinedType",
				declarations: declarations + "func configure(opts Adapter?) {}\n", call: "configure ",
				label: "count", id: "xgo:main?Adapter.Count",
				newServer: newTestServer,
			},
			{
				name:         "Alias",
				declarations: declarations + "type Options = Record\nfunc configure(opts Options?) {}\n", call: "configure ",
				label: "count", id: "xgo:main?Record.Count", doc: "Count stores the value.",
				newServer: newTestServer,
			},
			{
				name:         "OverloadRecord",
				declarations: overloads, call: "configure \"text\", ",
				label: "count", id: "xgo:main?Record.Count", doc: "Count stores the value.",
				newServer: newTestServer,
			},
			{
				name:         "OverloadAdapter",
				declarations: overloads, call: "configure 1, ",
				label: "count", id: "xgo:main?Adapter.Count",
				newServer: newTestServer,
			},
			{
				name:         "ImportedField",
				declarations: "import f \"example.com/framework\"\nfunc configure(opts f.Item?) {}\n", call: "configure ",
				label: "value", id: "xgo:example.com/framework?Item.Value", doc: "Value stores the work item's value.",
				newServer: newFrameworkTestServer,
			},
			{
				name:         "ImportedDefinedType",
				declarations: "import f \"example.com/framework\"\nfunc configure(opts f.Copy?) {}\n", call: "configure ",
				label: "value", id: "xgo:example.com/framework?Copy.Value",
				newServer: newFrameworkTestServer,
			},
			{
				name:         "ImportedGenericField",
				declarations: "import f \"example.com/framework\"\nfunc configure(opts f.Box[int]?) {}\n", call: "configure ",
				label: "value", id: "xgo:example.com/framework?Box.Value", doc: "Value holds the boxed value.",
				newServer: newFrameworkTestServer,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := tt.newServer(t, map[string][]byte{"main.xgo": []byte(tt.declarations + tt.call + tt.label + " = 1\n")})
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				position := Position{Line: uint32(strings.Count(tt.declarations, "\n")), Character: uint32(UTF16Len(tt.call) + 1)}
				hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
				}})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, `def-id="`+tt.id+`"`)
				if tt.doc != "" {
					assert.Contains(t, hover.Contents.Value, tt.doc)
				}
				links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}})
				require.NoError(t, err)
				var kwargLinks []DocumentLink
				for _, link := range links {
					if link.Range.Start == hover.Range.Start {
						kwargLinks = append(kwargLinks, link)
					}
				}
				assert.Equal(t, []DocumentLink{{Range: hover.Range, Target: ToPtr(URI(tt.id))}}, kwargLinks)

				s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(tt.declarations + tt.call + "option = 1\n"), Version: 1}})
				item := completionItemByLabel(completionItemsAt(t, s, "main.xgo", position), tt.label)
				require.NotNil(t, item)
				data := requireValueAs[*XGoCompletionItemData](t, item.Data)
				require.NotNil(t, data.Definition)
				assert.Equal(t, tt.id, data.Definition.String())
			})
		}
	})

	t.Run("SourceKinds", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			filename  string
			owner     string
			newServer testServerFactory
		}{
			{name: "XGo", filename: "main.xgo", newServer: newTestServer},
			{name: "LegacyXGo", filename: "main.gop", newServer: newTestServer},
			{name: "StandaloneClass", filename: "Record.gox", owner: "Record.", newServer: newTestServer},
			{name: "ProjectClass", filename: "main_fixture.gox", owner: "App.", newServer: newFrameworkTestServer},
			{name: "WorkClass", filename: "Worker_fixture.gox", owner: "Worker.", newServer: newFrameworkTestServer},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					tt.filename: []byte("// Count documentation.\nvar Count int\nCount = 1\n"),
				}
				if tt.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := tt.newServer(t, files)
				_, err := s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)
				for _, position := range []Position{{Line: 1, Character: 4}, {Line: 2}} {
					hover, err := s.textDocumentHover(&HoverParams{
						TextDocumentPositionParams: TextDocumentPositionParams{
							TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
							Position:     position,
						},
					})
					require.NoError(t, err)
					require.NotNil(t, hover)
					assert.Contains(t, hover.Contents.Value, `def-id="xgo:main?`+tt.owner+`Count"`)
					assert.Contains(t, hover.Contents.Value, "Count documentation.")
					assert.Equal(t, Range{
						Start: position,
						End:   Position{Line: position.Line, Character: position.Character + 5},
					}, hover.Range)
				}
			})
		}
	})

	t.Run("Documentation", func(t *testing.T) {
		for _, markup := range []struct {
			name string
			kind MarkupKind
		}{
			{name: "Markdown", kind: Markdown}, {name: "PlainText", kind: PlainText},
		} {
			for _, tt := range []struct {
				name     string
				source   string
				position Position
				want     string
			}{
				{name: "Import", source: "import \"example.com/framework\"\n", position: Position{Character: 8},
					want: "Package framework provides a minimal classfile framework for language tests."},
				{name: "Package", source: "import \"example.com/framework\"\nvar item framework.Item\n", position: Position{Line: 1, Character: 10},
					want: "Package framework provides a minimal classfile framework for language tests."},
				{name: "Type", source: "import \"example.com/framework\"\nvar item framework.Item\n", position: Position{Line: 1, Character: 19},
					want: "Item is the work base class."},
				{name: "Method", source: "import \"example.com/framework\"\nvar item framework.Item\nitem.apply 1\n", position: Position{Line: 2, Character: 5},
					want: "Apply accepts a value on a work instance."},
				{name: "Function", source: "import \"example.com/framework\"\nframework.runWhen true, => {}\n", position: Position{Line: 1, Character: 10},
					want: "RunWhen accepts a deferred condition and a callback."},
				{name: "Constant", source: "import \"example.com/framework\"\necho framework.Low\n", position: Position{Line: 1, Character: 15},
					want: "Low and High are values used by framework methods."},
			} {
				t.Run(tt.name+markup.name, func(t *testing.T) {
					s := newFrameworkTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
					_, err := s.initialize(&InitializeParams{XInitializeParams: protocol.XInitializeParams{
						Capabilities: protocol.ClientCapabilities{TextDocument: protocol.TextDocumentClientCapabilities{
							Hover: &protocol.HoverClientCapabilities{ContentFormat: []protocol.MarkupKind{markup.kind}},
						}},
					}})
					require.NoError(t, err)
					s.finishInitialize(nil)
					hover, err := s.textDocumentHover(&HoverParams{
						TextDocumentPositionParams: TextDocumentPositionParams{
							TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: tt.position,
						},
					})
					require.NoError(t, err)
					require.NotNil(t, hover)
					assert.Equal(t, markup.kind, hover.Contents.Kind)
					assert.Contains(t, hover.Contents.Value, tt.want)
					if tt.name == "Package" && markup.kind == Markdown {
						assert.Contains(t, hover.Contents.Value, `def-id="xgo:example.com/framework"`)
					}
				})
			}
		}
	})

	t.Run("MissingDocumentation", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			source    string
			position  Position
			want      string
			newServer testServerFactory
		}{
			{name: "Import", source: "import \"example.com/framework\"\n", position: Position{Character: 8}, newServer: newFrameworkTestServer},
			{name: "Method", source: "import \"example.com/framework\"\nvar item framework.Item\nitem.apply 1\n", position: Position{Line: 2, Character: 5},
				want: `overview="func apply(value int)"`, newServer: newFrameworkTestServer},
			{name: "BuiltinAlias", source: "var number int128\n", position: Position{Character: 12},
				want: `overview="type Int128"`, newServer: newTestServer},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := tt.newServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
				s.lookupPkgDoc = func(string) (*pkgdoc.PkgDoc, error) { return nil, fs.ErrNotExist }
				hover, err := s.textDocumentHover(&HoverParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: tt.position,
					},
				})
				require.NoError(t, err)
				if tt.want == "" {
					assert.Nil(t, hover)
				} else {
					require.NotNil(t, hover)
					assert.Contains(t, hover.Contents.Value, tt.want)
				}
			})
		}
	})

	t.Run("BuiltinDocumentation", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("var count int8\n")})
		s.lookupPkgDoc = func(pkgPath string) (*pkgdoc.PkgDoc, error) {
			require.Equal(t, "builtin", pkgPath)
			return &pkgdoc.PkgDoc{Types: map[string]*pkgdoc.TypeDoc{
				"int8": {Doc: "A signed 8-bit integer from the test documentation."},
			}}, nil
		}
		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: Position{Character: 10},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, "A signed 8-bit integer from the test documentation.")
		assert.Contains(t, hover.Contents.Value, `def-id="xgo:builtin?int8" overview="type int8"`)
	})

	t.Run("DocumentationIsolation", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			source   string
			position Position
			setDoc   func(*pkgdoc.PkgDoc, string)
		}{
			{name: "Method", source: "measure 1\n", position: Position{Character: 1},
				setDoc: func(doc *pkgdoc.PkgDoc, text string) { doc.Types["App"].Methods["Measure__0"] = text }},
			{name: "Type", source: "import \"example.com/framework\"\nvar item framework.Item\n", position: Position{Line: 1, Character: 19},
				setDoc: func(doc *pkgdoc.PkgDoc, text string) { doc.Types["Item"].Doc = text }},
			{name: "Field", source: "import \"example.com/framework\"\nvar item framework.Item\nitem.Value = 1\n", position: Position{Line: 2, Character: 5},
				setDoc: func(doc *pkgdoc.PkgDoc, text string) { doc.Types["Item"].Fields["Value"] = text }},
			{name: "Constant", source: "import \"example.com/framework\"\necho framework.Low\n", position: Position{Line: 1, Character: 15},
				setDoc: func(doc *pkgdoc.PkgDoc, text string) { doc.Consts["Low"] = text }},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(tt.source)})
				params := &HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}, Position: tt.position,
				}}
				for _, text := range []string{"First documentation.", "Second documentation."} {
					doc := testframework.NewPkgDoc(t)
					tt.setDoc(doc, text)
					s.lookupPkgDoc = func(pkgPath string) (*pkgdoc.PkgDoc, error) {
						require.Equal(t, testframework.PkgPath, pkgPath)
						return doc, nil
					}
					hover, err := s.textDocumentHover(params)
					require.NoError(t, err)
					require.NotNil(t, hover)
					assert.Contains(t, hover.Contents.Value, text)
				}
			})
		}
	})

	t.Run("DocumentUpdates", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("// Before update.\nvar value int\n")})
		params := &HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: Position{Line: 1, Character: 4},
		}}
		before, err := s.textDocumentHover(params)
		require.NoError(t, err)
		require.NotNil(t, before)
		assert.Contains(t, before.Contents.Value, "Before update.")
		s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte("// After update.\nvar value string\n"), Version: 1}})
		after, err := s.textDocumentHover(params)
		require.NoError(t, err)
		require.NotNil(t, after)
		assert.Contains(t, after.Contents.Value, "After update.")
		assert.Contains(t, after.Contents.Value, "var value string")
		assert.NotContains(t, after.Contents.Value, "Before update.")
	})

	t.Run("UTF16Range", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("var count int\r\necho \"\U0001f600\", count\r\n"),
		})
		hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: Position{Line: 1, Character: 12},
		}})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Equal(t, Range{Start: Position{Line: 1, Character: 11}, End: Position{Line: 1, Character: 16}}, hover.Range)
		assert.Contains(t, hover.Contents.Value, "var count int")
	})

	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`
import (
	"fmt"
	"image"
)

var (
	// count is a variable.
	count int

	imagePoint image.Point
)

// MaxCount is a constant.
const MaxCount = 100

// Add is a function.
func Add(x, y int) int {
	return x + y
}

// Point is a type.
type Point struct {
	// X is a field.
	X int

	// Y is a field.
	Y int
}

fmt.Println(int8(1))
`),
			"Worker_fixture.gox": []byte("imagePoint.X = 100\n"),
		}

		s := newFrameworkTestServer(t, m)

		varHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 8, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, varHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:main?App.count\" overview=\"var count int\">\ncount is a variable.\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 8, Character: 1},
				End:   Position{Line: 8, Character: 6},
			},
		}, varHover)

		constHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 14, Character: 6},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, constHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:main?MaxCount\" overview=\"const MaxCount = 100\">\nMaxCount is a constant.\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 14, Character: 6},
				End:   Position{Line: 14, Character: 14},
			},
		}, constHover)

		funcHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 17, Character: 5},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, funcHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:main?App.Add\" overview=\"func Add(x int, y int) int\">\nAdd is a function.\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 17, Character: 5},
				End:   Position{Line: 17, Character: 8},
			},
		}, funcHover)

		typeHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 22, Character: 5},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, typeHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:main?Point\" overview=\"type Point\">\nPoint is a type.\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 22, Character: 5},
				End:   Position{Line: 22, Character: 10},
			},
		}, typeHover)

		typeFieldHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 24, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, typeFieldHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:main?Point.X\" overview=\"field X int\">\nX is a field.\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 24, Character: 1},
				End:   Position{Line: 24, Character: 2},
			},
		}, typeFieldHover)

		pkgHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 30, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pkgHover)
		assert.Equal(t, Range{
			Start: Position{Line: 30, Character: 0},
			End:   Position{Line: 30, Character: 3},
		}, pkgHover.Range)

		pkgFuncHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 30, Character: 4},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pkgFuncHover)
		assert.Equal(t, Range{
			Start: Position{Line: 30, Character: 4},
			End:   Position{Line: 30, Character: 11},
		}, pkgFuncHover.Range)

		builtinFuncHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 30, Character: 12},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, builtinFuncHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:builtin?int8\" overview=\"type int8\">\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 30, Character: 12},
				End:   Position{Line: 30, Character: 16},
			},
		}, builtinFuncHover)

		imagePointFieldHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///Worker_fixture.gox"},
				Position:     Position{Line: 0, Character: 11},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, imagePointFieldHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:image?Point.X\" overview=\"field X int\">\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 0, Character: 11},
				End:   Position{Line: 0, Character: 12},
			},
		}, imagePointFieldHover)

	})

	t.Run("Autoclosure", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`
onStart => {
	runWhen true, => {}
}
`),
		}
		s := newFrameworkTestServer(t, m)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 2, Character: 2},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, `overview="func runWhen(condition bool, callback func())"`)
	})

	t.Run("UnavailableDocument", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			uri       DocumentURI
			wantError bool
		}{
			{name: "Empty", uri: "file:///empty.xgo"},
			{name: "Missing", uri: "file:///missing.xgo"},
			{name: "Unsupported", uri: "file:///notes.txt"},
			{name: "InvalidURI", uri: "https://example.com/main.xgo", wantError: true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"empty.xgo": nil,
					"notes.txt": []byte("var count int\n"),
				})
				hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: tt.uri},
				}})
				if tt.wantError {
					assert.ErrorContains(t, err, "failed to get file path")
				} else {
					require.NoError(t, err)
				}
				assert.Nil(t, hover)
			})
		}
	})

	t.Run("InvalidPosition", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`var x int`),
		}
		s := newTestServer(t, m)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 99, Character: 99},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, hover)
	})

	t.Run("ImportsAtASTFilePosition", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
import (
	"example.com/framework"
	"image"
)

framework.RunWhen true, => {}
`),
		}
		s := newFrameworkTestServer(t, m)

		importHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, importHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "Package framework provides a minimal classfile framework for language tests.",
			},
			Range: Range{
				Start: Position{Line: 2, Character: 1},
				End:   Position{Line: 2, Character: 24},
			},
		}, importHover)
	})

	t.Run("Append", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
var nums []int
nums = append(nums, 1)
`),
		}
		s := newTestServer(t, m)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 2, Character: 7},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, `def-id="xgo:builtin?append"`)
		assert.Equal(t, Range{
			Start: Position{Line: 2, Character: 7},
			End:   Position{Line: 2, Character: 13},
		}, hover.Range)
	})

	t.Run("WithXGoBuiltins", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
var num int128
echo num
`),
		}
		s := newTestServer(t, m)

		hover1, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 1, Character: 8},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover1)
		assert.Contains(t, hover1.Contents.Value, `def-id="xgo:builtin?int128"`)
		assert.Equal(t, Range{
			Start: Position{Line: 1, Character: 8},
			End:   Position{Line: 1, Character: 14},
		}, hover1.Range)

		hover2, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 2, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover2)
		assert.Contains(t, hover2.Contents.Value, `def-id="xgo:fmt?println"`)
		assert.Equal(t, Range{
			Start: Position{Line: 2, Character: 0},
			End:   Position{Line: 2, Character: 4},
		}, hover2.Range)
	})

	t.Run("WithNonENCharacters", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte("\nfunc run() {\n\tvar \u4e2d\u6587 []int\n\t\u4e2d\u6587 = append(\u4e2d\u6587, 1)\n\tprintln \"\u975e\u82f1\u6587\", \u4e2d\u6587\n}\n"),
		}
		s := newTestServer(t, m)

		hover1, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 3, Character: 14},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover1)
		assert.Contains(t, hover1.Contents.Value, `def-id="xgo:main?%E4%B8%AD%E6%96%87"`)
		assert.Equal(t, Range{
			Start: Position{Line: 3, Character: 13},
			End:   Position{Line: 3, Character: 15},
		}, hover1.Range)

		hover2, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 3, Character: 18},
			},
		})
		require.NoError(t, err)
		require.Nil(t, hover2)

		hover3, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 17},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover3)
		assert.Contains(t, hover3.Contents.Value, `def-id="xgo:main?%E4%B8%AD%E6%96%87"`)
		assert.Equal(t, Range{
			Start: Position{Line: 4, Character: 16},
			End:   Position{Line: 4, Character: 18},
		}, hover3.Range)
	})

	t.Run("VariadicFunctionCall", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
func run() {
	echo 1
}
`),
		}
		s := newTestServer(t, m)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, `def-id="xgo:fmt?println"`)
		assert.Contains(t, hover.Contents.Value, `overview="func println(a ...any) (n int, err error)"`)
		assert.Equal(t, Range{
			Start: Position{Line: 2, Character: 1},
			End:   Position{Line: 2, Character: 5},
		}, hover.Range)
	})

	t.Run("XGotMethodCall", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`
onStart => {
	create int, "value"
}
`),
		}
		s := newFrameworkTestServer(t, m)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, `def-id="xgo:example.com/framework?App.create"`)
		assert.Contains(t, hover.Contents.Value, `overview="func create(T Type, name string) *T"`)
		assert.Contains(t, hover.Contents.Value, `Create provides a method with an explicit type argument.`)
		assert.Equal(t, Range{
			Start: Position{Line: 2, Character: 1},
			End:   Position{Line: 2, Character: 7},
		}, hover.Range)
	})

	t.Run("StartWithInvalidChar", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte("\n\u201c\u201dvar (\n\tmaps []int\n)\n"),
		}
		s := newTestServer(t, m)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		require.Nil(t, hover)
	})

	t.Run("BlankIdentShouldReturnNilHover", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`const _ = 1
`),
		}
		s := newTestServer(t, m)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 0, Character: 6},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, hover)
	})

	t.Run("SyntheticThisShouldReturnNilHover", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`onStart => {
    _ = this
}
`),
		}
		s := newFrameworkTestServer(t, m)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 1, Character: 8},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, hover)
	})

	t.Run("NonSyntheticThisShouldStillHover", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`// this is a variable.
var this int
this = 1
`),
		}
		s := newTestServer(t, m)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 1, Character: 4},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, `overview="var this int"`)
	})

	t.Run("ClassfileLineStart", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			source   string
			method   string
			owner    string
		}{
			{"Project", "main_fixture.gox", "onStart => {\n    println 1\n    println 2\n}\n", "onStart", "App"},
			{"Work", "Worker_fixture.gox", "onValue value => {\n    apply value\n    apply 2\n}\n", "onValue", "Item"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{"main_fixture.gox": nil}
				files[tt.filename] = []byte(tt.source)
				s := newFrameworkTestServer(t, files)
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				for ch := range uint32(len(tt.method)) {
					hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}, Position: Position{Character: ch},
					}})
					require.NoError(t, err)
					require.NotNil(t, hover)
					assert.Contains(t, hover.Contents.Value, `def-id="xgo:example.com/framework?`+tt.owner+`.`+tt.method+`"`)
					assert.NotContains(t, hover.Contents.Value, "var this")
					assert.Equal(t, Range{End: Position{Character: uint32(len(tt.method))}}, hover.Range)
				}
				for _, line := range []uint32{1, 2} {
					for ch := range uint32(4) {
						hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
							TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}, Position: Position{Line: line, Character: ch},
						}})
						require.NoError(t, err)
						assert.Nil(t, hover)
					}
				}
			})
		}
	})

	t.Run("KwargField", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
	// Count is a kwarg field.
	Count int
}

func configure(opts Options?) {}

func run() {
	configure count = 1
}
`),
		}
		s := newTestServer(t, m)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 9, Character: 12},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:main?Options.Count\" overview=\"field Count int\">\nCount is a kwarg field.\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 9, Character: 11},
				End:   Position{Line: 9, Character: 16},
			},
		}, hover)
	})

	t.Run("MapKwargHasNoHover", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
func configure(opts map[string]int?) {}

func run() {
	configure count = 1
}
`),
		}
		s := newTestServer(t, m)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 12},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, hover)
	})

	t.Run("KwargInterfaceMethod", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Client struct{}

type Params interface {
	// MaxTokens limits the response length.
	MaxTokens(n int64) Params
}

var client Client

func (c Client) Params() Params { return nil }

func (c Client) complete(prompt string, params Params?) {}

func run() {
	client.complete "hi", maxTokens = 1
}
`),
		}
		s := newTestServer(t, m)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 15, Character: 25},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Equal(t, Range{
			Start: Position{Line: 15, Character: 23},
			End:   Position{Line: 15, Character: 32},
		}, hover.Range)
		assert.Contains(t, hover.Contents.Value, `def-id="xgo:main?Params.MaxTokens"`)
		assert.Contains(t, hover.Contents.Value, `overview="func MaxTokens(n int64) Params"`)
	})

	t.Run("DuplicateEnumMembers", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`type First const (
	// First member documentation.
	Unknown = iota
)

type Second const (
	// Second member documentation.
	Unknown = iota
)

var (
	first First = Unknown
	second Second = Unknown
)

echo Unknown
`),
		}
		s := newTestServer(t, m)
		hoverAt := func(line, character uint32) *Hover {
			hover, err := s.textDocumentHover(&HoverParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: line, Character: character},
				},
			})
			require.NoError(t, err)
			require.NotNil(t, hover)
			return hover
		}

		firstDeclaration := hoverAt(2, 2)
		assert.Contains(t, firstDeclaration.Contents.Value, "First member documentation.")
		assert.NotContains(t, firstDeclaration.Contents.Value, "Second member documentation.")

		firstReference := hoverAt(11, 16)
		assert.Contains(t, firstReference.Contents.Value, "First member documentation.")
		assert.NotContains(t, firstReference.Contents.Value, "Second member documentation.")

		secondReference := hoverAt(12, 18)
		assert.NotContains(t, secondReference.Contents.Value, "First member documentation.")
		assert.Contains(t, secondReference.Contents.Value, "Second member documentation.")

		ambiguousReference := hoverAt(15, 6)
		assert.Contains(t, ambiguousReference.Contents.Value, "First.Unknown:")
		assert.Contains(t, ambiguousReference.Contents.Value, "First member documentation.")
		assert.Contains(t, ambiguousReference.Contents.Value, "Second.Unknown:")
		assert.Contains(t, ambiguousReference.Contents.Value, "Second member documentation.")
	})

	t.Run("EnumMemberSharedWithRegularConstant", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`const (
	// Regular documentation.
	Shared = 1
)

type Color const (
	// Enum documentation.
	Shared = 1
)

func run() {
	println(Shared)
	var color Color = Shared
}
`),
		}
		s := newTestServer(t, m)

		for _, tt := range []struct {
			name        string
			position    Position
			wantDoc     string
			unwantedDoc string
		}{
			{
				name:        "RegularDeclaration",
				position:    Position{Line: 2, Character: 2},
				wantDoc:     "Regular documentation.",
				unwantedDoc: "Enum documentation.",
			},
			{
				name:        "RegularReference",
				position:    Position{Line: 11, Character: 10},
				wantDoc:     "Regular documentation.",
				unwantedDoc: "Enum documentation.",
			},
			{
				name:        "EnumDeclaration",
				position:    Position{Line: 7, Character: 2},
				wantDoc:     "Enum documentation.",
				unwantedDoc: "Regular documentation.",
			},
			{
				name:        "EnumReference",
				position:    Position{Line: 12, Character: 20},
				wantDoc:     "Enum documentation.",
				unwantedDoc: "Regular documentation.",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				hover, err := s.textDocumentHover(&HoverParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
						Position:     tt.position,
					},
				})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, tt.wantDoc)
				assert.NotContains(t, hover.Contents.Value, tt.unwantedDoc)
			})
		}
	})

	t.Run("ParenthesizedDuplicateEnumMembers", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`type First const (
	// First member documentation.
	Unknown = iota
)

type Second const (
	// Second member documentation.
	Unknown = iota
)

func use(Second) {}

func run() {
	var value Second = (Unknown)
	use((Unknown))
}
`),
		}
		s := newTestServer(t, m)

		for _, position := range []Position{
			{Line: 13, Character: 22},
			{Line: 14, Character: 7},
		} {
			hover, err := s.textDocumentHover(&HoverParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     position,
				},
			})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, "Second member documentation.")
			assert.NotContains(t, hover.Contents.Value, "First member documentation.")
		}
	})

	t.Run("TupleDuplicateEnumMembers", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`type First const (
	// First member documentation.
	Unknown = iota
)

type Second const (
	// Second member documentation.
	Unknown = iota
)

func use(First, Second) {}

func run() {
	use((Unknown, Unknown))
}
`),
		}
		s := newTestServer(t, m)

		for _, tt := range []struct {
			name        string
			position    Position
			wantDoc     string
			unwantedDoc string
		}{
			{
				name:        "FirstElement",
				position:    Position{Line: 13, Character: 8},
				wantDoc:     "First member documentation.",
				unwantedDoc: "Second member documentation.",
			},
			{
				name:        "SecondElement",
				position:    Position{Line: 13, Character: 17},
				wantDoc:     "Second member documentation.",
				unwantedDoc: "First member documentation.",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				hover, err := s.textDocumentHover(&HoverParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
						Position:     tt.position,
					},
				})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, tt.wantDoc)
				assert.NotContains(t, hover.Contents.Value, tt.unwantedDoc)
			})
		}
	})

	t.Run("ContextualDuplicateEnumMembers", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`type First const (
	// First member documentation.
	Unknown = iota
)

type Second const (
	// Second member documentation.
	Unknown = iota
)

type Pair struct {
	Left Second
	Right Second
}

func returnSecond() Second {
	return Unknown
}

func run(second Second) {
	second = Unknown
	switch second {
	case Unknown:
	}
	_ = Unknown == second
	_ = second == Unknown
	_ = []Second{Unknown}
	_ = [1]Second{Unknown}
	_ = Pair{Unknown, Unknown}
	_ = Pair{Left: Unknown}
	_ = map[Second]Second{Unknown: Unknown}
	var unary Second = +Unknown
	values := map[Second]int{Unknown: 1}
	_ = values[Unknown]
	ch := make(chan Second, 1)
	ch <- Unknown
	var binary Second = Unknown + 1
	_ = second << Unknown
	var slice []int = []int{1}[Unknown:]
	var collection []Second = [Unknown]
	literal := func() Second { return Unknown }
	_ = literal
}
`),
		}
		s := newTestServer(t, m)

		for _, tt := range []struct {
			name     string
			position Position
		}{
			{name: "ReturnFuncDecl", position: Position{Line: 16, Character: 9}},
			{name: "Assignment", position: Position{Line: 20, Character: 11}},
			{name: "SwitchCase", position: Position{Line: 22, Character: 7}},
			{name: "BinaryLeft", position: Position{Line: 24, Character: 6}},
			{name: "BinaryRight", position: Position{Line: 25, Character: 16}},
			{name: "SliceLiteral", position: Position{Line: 26, Character: 15}},
			{name: "ArrayLiteral", position: Position{Line: 27, Character: 16}},
			{name: "StructPositionalFirst", position: Position{Line: 28, Character: 11}},
			{name: "StructPositionalSecond", position: Position{Line: 28, Character: 19}},
			{name: "StructKeyed", position: Position{Line: 29, Character: 17}},
			{name: "MapKey", position: Position{Line: 30, Character: 24}},
			{name: "MapValue", position: Position{Line: 30, Character: 33}},
			{name: "UnaryExpression", position: Position{Line: 31, Character: 22}},
			{name: "MapIndex", position: Position{Line: 33, Character: 13}},
			{name: "Send", position: Position{Line: 35, Character: 8}},
			{name: "BinaryWithUntypedOperand", position: Position{Line: 36, Character: 22}},
			{name: "XGoSliceLiteral", position: Position{Line: 39, Character: 29}},
			{name: "ReturnFuncLit", position: Position{Line: 40, Character: 36}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				hover, err := s.textDocumentHover(&HoverParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
						Position:     tt.position,
					},
				})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, "Second member documentation.")
				assert.NotContains(t, hover.Contents.Value, "First member documentation.")
			})
		}

		for _, tt := range []struct {
			name     string
			position Position
		}{
			{name: "ShiftCount", position: Position{Line: 37, Character: 16}},
			{name: "SliceBound", position: Position{Line: 38, Character: 29}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				hover, err := s.textDocumentHover(&HoverParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
						Position:     tt.position,
					},
				})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, "First member documentation.")
				assert.Contains(t, hover.Contents.Value, "Second member documentation.")
			})
		}
	})

	t.Run("ContextualDuplicateEnumMemberInLambda", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`type First const (
	// First member documentation.
	Unknown = iota
)

type Second const (
	// Second member documentation.
	Unknown = iota
)

func use(func() Second) {}

func run() {
	use(=> Unknown)
}
`),
		}
		s := newTestServer(t, m)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 13, Character: 9},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, "Second member documentation.")
		assert.NotContains(t, hover.Contents.Value, "First member documentation.")
	})

	t.Run("LocalEnumMember", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`func run() {
	type Color const (
		// Local red documentation.
		Red = iota
	)
	var color Color = Red
}
`),
		}
		s := newTestServer(t, m)

		for _, position := range []Position{
			{Line: 3, Character: 2},
			{Line: 5, Character: 20},
		} {
			hover, err := s.textDocumentHover(&HoverParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     position,
				},
			})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, "Local red documentation.")
		}
	})

	for _, tt := range []struct {
		name         string
		source       string
		unit         string
		multiplier   string
		endCharacter uint32
	}{
		{
			name:         "XGoUnit",
			source:       "import \"time\"\n\nfunc wait(d time.Duration) {}\n\nfunc run() {\n\twait 1m\n}\n",
			unit:         "m",
			multiplier:   "60000000000",
			endCharacter: 8,
		},
		{
			name:         "XGoUnicodeUnit",
			source:       "import \"time\"\n\nfunc wait(d time.Duration) {}\n\nfunc run() {\n\twait 1\u00b5s\n}\n",
			unit:         "\u00b5s",
			multiplier:   "1000",
			endCharacter: 9,
		},
		{
			name:         "XGoUnitImportedAliasFallback",
			source:       "import \"example.com/unit\"\n\nfunc wait(d unit.Delay) {}\n\nfunc run() {\n\twait 1ms\n}\n",
			unit:         "ms",
			multiplier:   "1000000",
			endCharacter: 9,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
			proj := s.workspaceRootFS
			proj.Importer = xgoUnitTestImporter{fallback: proj.Importer}

			hover, err := s.textDocumentHover(&HoverParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 5, Character: 7},
				},
			})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Equal(t, Range{
				Start: Position{Line: 5, Character: 7},
				End:   Position{Line: 5, Character: tt.endCharacter},
			}, hover.Range)
			assert.Contains(t, hover.Contents.Value, "unit `"+tt.unit+"`")
			assert.Contains(t, hover.Contents.Value, "time.Duration")
			assert.Contains(t, hover.Contents.Value, "Multiplier: `"+tt.multiplier+"`")
		})
	}
}
