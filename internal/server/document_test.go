package server

import (
	"io/fs"
	"testing"

	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentDocumentLink(t *testing.T) {
	t.Run("SourceKinds", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			owner    string
		}{
			{name: "XGo", filename: "main.xgo"},
			{name: "LegacyXGo", filename: "main.gop"},
			{name: "StandaloneClass", filename: "Record.gox", owner: "Record."},
			{name: "ProjectClass", filename: "main_fixture.gox", owner: "App."},
			{name: "WorkClass", filename: "Worker_fixture.gox", owner: "Worker."},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{tt.filename: []byte("var Count int\nCount = 1\n")}
				if tt.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := newTestServer(t, files)
				_, err := s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)
				links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
				})
				require.NoError(t, err)
				assert.Equal(t, []DocumentLink{
					{Range: Range{Start: Position{Character: 10}, End: Position{Character: 13}}, Target: toURI("xgo:builtin?int")},
					{Range: Range{Start: Position{Character: 4}, End: Position{Character: 9}}, Target: toURI("xgo:main?" + tt.owner + "Count")},
					{Range: Range{Start: Position{Line: 1}, End: Position{Line: 1, Character: 5}}, Target: toURI("xgo:main?" + tt.owner + "Count")},
				}, links)
			})
		}
	})

	t.Run("CrossFileClassSymbols", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main_fixture.gox":   []byte("var Count int\n"),
			"Worker_fixture.gox": []byte("onValue value => {\n    Count = value\n    apply Count\n}\n"),
		})
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///Worker_fixture.gox"},
		})
		require.NoError(t, err)
		assert.Equal(t, []DocumentLink{
			{Range: Range{Start: Position{Line: 2, Character: 4}, End: Position{Line: 2, Character: 9}}, Target: toURI("xgo:example.com/framework?Item.apply")},
			{Range: Range{Start: Position{}, End: Position{Character: 7}}, Target: toURI("xgo:example.com/framework?Item.onValue")},
			{Range: Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 9}}, Target: toURI("xgo:main?App.Count")},
			{Range: Range{Start: Position{Line: 2, Character: 10}, End: Position{Line: 2, Character: 15}}, Target: toURI("xgo:main?App.Count")},
			{Range: Range{Start: Position{Character: 8}, End: Position{Character: 13}}, Target: toURI("xgo:main?value")},
			{Range: Range{Start: Position{Line: 1, Character: 12}, End: Position{Line: 1, Character: 17}}, Target: toURI("xgo:main?value")},
		}, links)
		links, err = s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		})
		require.NoError(t, err)
		assert.Equal(t, []DocumentLink{
			{Range: Range{Start: Position{Character: 10}, End: Position{Character: 13}}, Target: toURI("xgo:builtin?int")},
			{Range: Range{Start: Position{Character: 4}, End: Position{Character: 9}}, Target: toURI("xgo:main?App.Count")},
		}, links)
	})

	t.Run("ImportedSymbols", func(t *testing.T) {
		for _, name := range []string{"WithDocumentation", "MissingDocumentation"} {
			t.Run(name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte("import \"example.com/framework\"\nvar item framework.Item\nitem.apply 1\nvar number int128\n"),
				})
				if name == "MissingDocumentation" {
					s.lookupPkgDoc = func(string) (*pkgdoc.PkgDoc, error) { return nil, fs.ErrNotExist }
				}
				_, err := s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)
				links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				})
				require.NoError(t, err)
				assert.Equal(t, []DocumentLink{
					{Range: Range{Start: Position{Line: 3, Character: 11}, End: Position{Line: 3, Character: 17}}, Target: toURI("xgo:builtin?int128")},
					{Range: Range{Start: Position{Line: 1, Character: 9}, End: Position{Line: 1, Character: 18}}, Target: toURI("xgo:example.com/framework")},
					{Range: Range{Start: Position{Line: 1, Character: 19}, End: Position{Line: 1, Character: 23}}, Target: toURI("xgo:example.com/framework?Item")},
					{Range: Range{Start: Position{Line: 2, Character: 5}, End: Position{Line: 2, Character: 10}}, Target: toURI("xgo:example.com/framework?Item.apply")},
					{Range: Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 8}}, Target: toURI("xgo:main?item")},
					{Range: Range{Start: Position{Line: 2}, End: Position{Line: 2, Character: 4}}, Target: toURI("xgo:main?item")},
					{Range: Range{Start: Position{Line: 3, Character: 4}, End: Position{Line: 3, Character: 10}}, Target: toURI("xgo:main?number")},
				}, links)
			})
		}
	})

	t.Run("FrameworkCalls", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte("measure 1\nmeasure \"one\"\ncreate int, \"item\"\nrunWhen true, => {}\n"),
		})
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		})
		require.NoError(t, err)
		for _, tt := range []struct {
			line   uint32
			length uint32
			target string
		}{
			{line: 0, length: 7, target: "xgo:example.com/framework?App.measure#0"},
			{line: 1, length: 7, target: "xgo:example.com/framework?App.measure#1"},
			{line: 2, length: 6, target: "xgo:example.com/framework?App.create"},
			{line: 3, length: 7, target: "xgo:example.com/framework?runWhen"},
		} {
			assert.Contains(t, links, DocumentLink{
				Range:  Range{Start: Position{Line: tt.line}, End: Position{Line: tt.line, Character: tt.length}},
				Target: toURI(tt.target),
			})
		}
	})

	t.Run("CrossFileEnumMembers", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"types.xgo":        []byte("type Color const (\n    Red = iota\n)\n"),
			"main_fixture.gox": []byte("var color Color = Red\n"),
		})
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		})
		require.NoError(t, err)
		assert.Equal(t, []DocumentLink{
			{Range: Range{Start: Position{Character: 4}, End: Position{Character: 9}}, Target: toURI("xgo:main?App.color")},
			{Range: Range{Start: Position{Character: 10}, End: Position{Character: 15}}, Target: toURI("xgo:main?Color")},
			{Range: Range{Start: Position{Character: 18}, End: Position{Character: 21}}, Target: toURI("xgo:main?Red")},
		}, links)
	})

	t.Run("This", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			source   string
			want     []DocumentLink
		}{
			{
				name: "Synthetic", filename: "main_fixture.gox", source: "onStart => {\n    _ = this\n}\n",
				want: []DocumentLink{{
					Range: Range{Start: Position{}, End: Position{Character: 7}}, Target: toURI("xgo:example.com/framework?App.onStart"),
				}},
			},
			{
				name: "UserVariable", filename: "main.xgo", source: "var this = 1\nthis = 2\n",
				want: []DocumentLink{
					{Range: Range{Start: Position{Character: 4}, End: Position{Character: 8}}, Target: toURI("xgo:main?this")},
					{Range: Range{Start: Position{Line: 1}, End: Position{Line: 1, Character: 4}}, Target: toURI("xgo:main?this")},
				},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{tt.filename: []byte(tt.source)})
				links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
				})
				require.NoError(t, err)
				assert.Equal(t, tt.want, links)
			})
		}
	})

	t.Run("KwargDefinitions", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
    Count int
}

type Params interface {
    MaxTokens(n int64) Params
}

type Client struct{}

var client Client

func (c Client) Params() Params { return nil }

func (c Client) complete(prompt string, params Params?) {}

func configure(opts Options?) {}

func run() {
    configure count = 1
    client.complete "hi", maxTokens = 1
}
`),
		}
		s := newTestServer(t, m)

		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		assert.Contains(t, links, DocumentLink{
			Range: Range{
				Start: Position{Line: 20, Character: 14},
				End:   Position{Line: 20, Character: 19},
			},
			Target: toURI("xgo:main?Options.Count"),
		})
		assert.Contains(t, links, DocumentLink{
			Range: Range{
				Start: Position{Line: 21, Character: 26},
				End:   Position{Line: 21, Character: 35},
			},
			Target: toURI("xgo:main?interface%7BMaxTokens%28n+int64%29+main.Params%7D.MaxTokens"),
		})
	})

	t.Run("DocumentUpdates", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("var Before int\nBefore = 1\n")})
		params := &DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}
		before, err := s.textDocumentDocumentLink(params)
		require.NoError(t, err)
		assert.Contains(t, before, DocumentLink{
			Range: Range{Start: Position{Line: 1}, End: Position{Line: 1, Character: 6}}, Target: toURI("xgo:main?Before"),
		})
		s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte("\nvar After string\nAfter = missing\n"), Version: 1}})
		after, err := s.textDocumentDocumentLink(params)
		require.NoError(t, err)
		typeInfo, err := s.workspaceRootFS.TypeInfo()
		require.Error(t, err)
		require.NotNil(t, typeInfo)
		assert.Equal(t, []DocumentLink{
			{Range: Range{Start: Position{Line: 1, Character: 10}, End: Position{Line: 1, Character: 16}}, Target: toURI("xgo:builtin?string")},
			{Range: Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 9}}, Target: toURI("xgo:main?After")},
			{Range: Range{Start: Position{Line: 2}, End: Position{Line: 2, Character: 5}}, Target: toURI("xgo:main?After")},
		}, after)
	})

	t.Run("ParseError", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("const (\n    Count = 1\n")})
		astFile, err := s.workspaceRootFS.ASTFile("main.xgo")
		require.Error(t, err)
		require.NotNil(t, astFile)
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		assert.Equal(t, []DocumentLink{{
			Range: Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 9}}, Target: toURI("xgo:main?Count"),
		}}, links)
	})

	t.Run("UTF16Ranges", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("var count int\r\necho \"\U0001f600\", count\r\n"),
		})
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		assert.Equal(t, []DocumentLink{
			{Range: Range{Start: Position{Character: 10}, End: Position{Character: 13}}, Target: toURI("xgo:builtin?int")},
			{Range: Range{Start: Position{Line: 1}, End: Position{Line: 1, Character: 4}}, Target: toURI("xgo:fmt?println")},
			{Range: Range{Start: Position{Character: 4}, End: Position{Character: 9}}, Target: toURI("xgo:main?count")},
			{Range: Range{Start: Position{Line: 1, Character: 11}, End: Position{Line: 1, Character: 16}}, Target: toURI("xgo:main?count")},
		}, links)
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
			{name: "InvalidASTPosition", uri: "file:///invalid.xgo"},
			{name: "InvalidURI", uri: "https://example.com/main.xgo", wantError: true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"empty.xgo":   nil,
					"notes.txt":   []byte("var count int\n"),
					"invalid.xgo": []byte("\u201c\u201dvar count int\n"),
				})
				links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
					TextDocument: TextDocumentIdentifier{URI: tt.uri},
				})
				if tt.wantError {
					assert.ErrorContains(t, err, "failed to get file path")
				} else {
					require.NoError(t, err)
				}
				assert.Empty(t, links)
			})
		}
	})

	t.Run("BlankIdentifier", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`type`),
		}
		s := newTestServer(t, m)

		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.Empty(t, links)
	})

	t.Run("BlankIdent", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
const _ = 1
`),
		}
		s := newTestServer(t, m)

		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		})
		require.NoError(t, err)
		require.Empty(t, links)
	})
}

func TestSortDocumentLinks(t *testing.T) {
	t.Run("NilTargetSorting", func(t *testing.T) {
		links := []DocumentLink{
			{Range: Range{Start: Position{Line: 10, Character: 5}}, Target: nil},
			{Range: Range{Start: Position{Line: 5, Character: 10}}, Target: toURI("xgo:main?target1")},
			{Range: Range{Start: Position{Line: 20, Character: 15}}, Target: nil},
			{Range: Range{Start: Position{Line: 15, Character: 20}}, Target: toURI("xgo:main?target2")},
		}

		sortDocumentLinks(links)

		// Links with nil targets should come first.
		require.Len(t, links, 4)
		assert.Nil(t, links[0].Target)
		assert.Nil(t, links[1].Target)
		assert.NotNil(t, links[2].Target)
		assert.NotNil(t, links[3].Target)

		// Nil targets should be sorted by line number.
		assert.Equal(t, uint32(10), links[0].Range.Start.Line)
		assert.Equal(t, uint32(20), links[1].Range.Start.Line)
	})

	t.Run("TargetURISorting", func(t *testing.T) {
		targetA := toURI("xgo:main?A")
		targetB := toURI("xgo:main?B")
		targetC := toURI("xgo:main?C")
		links := []DocumentLink{
			{Range: Range{Start: Position{Line: 10, Character: 5}}, Target: targetC},
			{Range: Range{Start: Position{Line: 5, Character: 10}}, Target: targetA},
			{Range: Range{Start: Position{Line: 15, Character: 15}}, Target: targetB},
		}

		sortDocumentLinks(links)

		// Links with targets should be sorted by target URI string.
		require.Len(t, links, 3)
		assert.Equal(t, targetA, links[0].Target)
		assert.Equal(t, targetB, links[1].Target)
		assert.Equal(t, targetC, links[2].Target)
	})

	t.Run("LineNumberSorting", func(t *testing.T) {
		target := toURI("xgo:main?same-target")
		links := []DocumentLink{
			{Range: Range{Start: Position{Line: 30, Character: 5}}, Target: target},
			{Range: Range{Start: Position{Line: 10, Character: 10}}, Target: target},
			{Range: Range{Start: Position{Line: 20, Character: 15}}, Target: target},
		}

		sortDocumentLinks(links)

		// Same target URI should be sorted by line number.
		require.Len(t, links, 3)
		assert.Equal(t, uint32(10), links[0].Range.Start.Line)
		assert.Equal(t, uint32(20), links[1].Range.Start.Line)
		assert.Equal(t, uint32(30), links[2].Range.Start.Line)
	})

	t.Run("CharacterPositionSorting", func(t *testing.T) {
		target := toURI("xgo:main?same-target")
		links := []DocumentLink{
			{Range: Range{Start: Position{Line: 10, Character: 25}}, Target: target},
			{Range: Range{Start: Position{Line: 10, Character: 5}}, Target: target},
			{Range: Range{Start: Position{Line: 10, Character: 15}}, Target: target},
		}

		sortDocumentLinks(links)

		// Same target URI and line should be sorted by character position.
		require.Len(t, links, 3)
		assert.Equal(t, uint32(5), links[0].Range.Start.Character)
		assert.Equal(t, uint32(15), links[1].Range.Start.Character)
		assert.Equal(t, uint32(25), links[2].Range.Start.Character)
	})

	t.Run("ComplexSorting", func(t *testing.T) {
		targetA := toURI("xgo:main?A")
		targetB := toURI("xgo:main?B")
		links := []DocumentLink{
			{Range: Range{Start: Position{Line: 5, Character: 10}}, Target: nil},
			{Range: Range{Start: Position{Line: 5, Character: 20}}, Target: targetB},
			{Range: Range{Start: Position{Line: 10, Character: 5}}, Target: targetA},
			{Range: Range{Start: Position{Line: 5, Character: 5}}, Target: targetA},
			{Range: Range{Start: Position{Line: 5, Character: 15}}, Target: targetA},
			{Range: Range{Start: Position{Line: 1, Character: 10}}, Target: nil},
		}

		sortDocumentLinks(links)

		// Nil targets should come first, sorted by line number.
		require.Len(t, links, 6)
		assert.Nil(t, links[0].Target)
		assert.Nil(t, links[1].Target)
		assert.Equal(t, uint32(1), links[0].Range.Start.Line)
		assert.Equal(t, uint32(5), links[1].Range.Start.Line)

		// Then links sorted by target URI.
		assert.Equal(t, targetA, links[2].Target)
		assert.Equal(t, targetA, links[3].Target)
		assert.Equal(t, targetA, links[4].Target)
		assert.Equal(t, targetB, links[5].Target)

		// Same target URIs should be sorted by line number.
		assert.Equal(t, uint32(5), links[2].Range.Start.Line)
		assert.Equal(t, uint32(5), links[3].Range.Start.Line)
		assert.Equal(t, uint32(10), links[4].Range.Start.Line)

		// Same target URI and line should be sorted by character position.
		assert.Equal(t, uint32(5), links[2].Range.Start.Character)
		assert.Equal(t, uint32(15), links[3].Range.Start.Character)
	})
}
