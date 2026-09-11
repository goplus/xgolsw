package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentDocumentHighlight(t *testing.T) {
	for _, tt := range []struct {
		name     string
		filename string
	}{
		{name: "XGo", filename: "main.xgo"},
		{name: "Gop", filename: "main.gop"},
		{name: "NormalClass", filename: "Record.gox"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				tt.filename: []byte("var value int\nfunc use() { println(value) }\n"),
			})
			highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
					Position:     Position{Line: 1, Character: 21},
				},
			})
			require.NoError(t, err)
			require.NotNil(t, highlights)
			assert.ElementsMatch(t, []DocumentHighlight{
				{
					Range: Range{
						Start: Position{Line: 0, Character: 4},
						End:   Position{Line: 0, Character: 9},
					},
					Kind: Write,
				},
				{
					Range: Range{
						Start: Position{Line: 1, Character: 21},
						End:   Position{Line: 1, Character: 26},
					},
					Kind: Read,
				},
			}, *highlights)
		})
	}

	for _, tt := range []struct {
		name    string
		uri     DocumentURI
		wantErr bool
	}{
		{name: "MissingFile", uri: "file:///missing.xgo"},
		{name: "NonSourceFile", uri: "file:///notes.txt"},
		{name: "InvalidURI", uri: "https://example.com/main.xgo", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"notes.txt": []byte("var value int")})
			highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: tt.uri},
				},
			})
			if tt.wantErr {
				assert.ErrorContains(t, err, "workspace root URI")
			} else {
				assert.NoError(t, err)
			}
			assert.Nil(t, highlights)
		})
	}

	for _, tt := range []struct {
		name     string
		source   string
		position Position
		want     []DocumentHighlight
	}{
		{
			name: "Assignments",
			source: `value := 1
value = value
value += 2
value++
println value
`,
			position: Position{Line: 0, Character: 0},
			want: []DocumentHighlight{
				{Kind: Write, Range: Range{Start: Position{Line: 0, Character: 0}, End: Position{Line: 0, Character: 5}}},
				{Kind: Write, Range: Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 1, Character: 5}}},
				{Kind: Read, Range: Range{Start: Position{Line: 1, Character: 8}, End: Position{Line: 1, Character: 13}}},
				{Kind: Write, Range: Range{Start: Position{Line: 2, Character: 0}, End: Position{Line: 2, Character: 5}}},
				{Kind: Write, Range: Range{Start: Position{Line: 3, Character: 0}, End: Position{Line: 3, Character: 5}}},
				{Kind: Read, Range: Range{Start: Position{Line: 4, Character: 8}, End: Position{Line: 4, Character: 13}}},
			},
		},
		{
			name: "RangeVariable",
			source: `for value := range [1, 2] {
    println value
}
`,
			position: Position{Line: 1, Character: 12},
			want: []DocumentHighlight{
				{Kind: Write, Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}},
				{Kind: Read, Range: Range{Start: Position{Line: 1, Character: 12}, End: Position{Line: 1, Character: 17}}},
			},
		},
		{
			name: "RangeSource",
			source: `values := [1, 2]
for _, value := range values {
    println value
}
`,
			position: Position{Line: 0, Character: 0},
			want: []DocumentHighlight{
				{Kind: Write, Range: Range{Start: Position{Line: 0, Character: 0}, End: Position{Line: 0, Character: 6}}},
				{Kind: Read, Range: Range{Start: Position{Line: 1, Character: 22}, End: Position{Line: 1, Character: 28}}},
			},
		},
		{
			name: "FunctionDeclaration",
			source: `func run() {}
run()
`,
			position: Position{Line: 0, Character: 5},
			want: []DocumentHighlight{
				{Kind: Write, Range: Range{Start: Position{Line: 0, Character: 5}, End: Position{Line: 0, Character: 8}}},
				{Kind: Read, Range: Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 1, Character: 3}}},
			},
		},
		{
			name:     "InvalidPosition",
			source:   "var value int\n",
			position: Position{Line: 99, Character: 99},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
			highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     tt.position,
				},
			})
			require.NoError(t, err)
			if tt.want == nil {
				assert.Nil(t, highlights)
			} else {
				require.NotNil(t, highlights)
				assert.ElementsMatch(t, tt.want, *highlights)
			}
			_, err = s.workspaceRootFS.TypeInfo()
			assert.NoError(t, err)
		})
	}

	t.Run("FileUpdatesWithTypeErrors", func(t *testing.T) {
		files := map[string][]byte{
			"main.xgo": []byte("var value int\nfunc use() { println(value) }\n"),
		}
		s := newTestServer(t, files)
		params := &DocumentHighlightParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 0, Character: 4},
			},
		}
		highlights, err := s.textDocumentDocumentHighlight(params)
		require.NoError(t, err)
		require.NotNil(t, highlights)
		require.Len(t, *highlights, 2)

		s.ModifyFiles([]FileChange{{
			Path:    "main.xgo",
			Content: append(files["main.xgo"], []byte("func again() { println(value); missing() }\n")...),
			Version: 1,
		}})
		highlights, err = s.textDocumentDocumentHighlight(params)
		require.NoError(t, err)
		require.NotNil(t, highlights)
		assert.ElementsMatch(t, []DocumentHighlight{
			{Kind: Write, Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}},
			{Kind: Read, Range: Range{Start: Position{Line: 1, Character: 21}, End: Position{Line: 1, Character: 26}}},
			{Kind: Read, Range: Range{Start: Position{Line: 2, Character: 23}, End: Position{Line: 2, Character: 28}}},
		}, *highlights)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.ErrorContains(t, err, "undefined: missing")
	})

	t.Run("FrameworkClasses", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`
Worker.apply Low
Worker.apply High
`),
			"Worker_fixture.gox": []byte(`
onValue amount => {
	Worker.apply High
}
`),
		}
		s := newFrameworkTestServer(t, m)

		workerHighlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 1, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, workerHighlights)
		assert.Len(t, *workerHighlights, 2)
		assert.Contains(t, *workerHighlights, DocumentHighlight{
			Range: Range{
				Start: Position{Line: 1, Character: 0},
				End:   Position{Line: 1, Character: 6},
			},
			Kind: Read,
		})
		assert.Contains(t, *workerHighlights, DocumentHighlight{
			Range: Range{
				Start: Position{Line: 2, Character: 0},
				End:   Position{Line: 2, Character: 6},
			},
			Kind: Read,
		})

		highHighlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 2, Character: 13},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, highHighlights)
		assert.Len(t, *highHighlights, 1)
		assert.Contains(t, *highHighlights, DocumentHighlight{
			Range: Range{
				Start: Position{Line: 2, Character: 13},
				End:   Position{Line: 2, Character: 17},
			},
			Kind: Read,
		})
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("KwargField", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

func main() {
	configure count = 1
	configure count = 2
}
`),
			"other.xgo": []byte("func other() { configure count = 3 }\n"),
		}
		s := newTestServer(t, m)

		highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 8, Character: 12},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, highlights)
		assert.Len(t, *highlights, 3)
		assert.Contains(t, *highlights, DocumentHighlight{
			Range: Range{
				Start: Position{Line: 2, Character: 1},
				End:   Position{Line: 2, Character: 6},
			},
			Kind: Write,
		})
		assert.Contains(t, *highlights, DocumentHighlight{
			Range: Range{
				Start: Position{Line: 8, Character: 11},
				End:   Position{Line: 8, Character: 16},
			},
			Kind: Read,
		})
		assert.Contains(t, *highlights, DocumentHighlight{
			Range: Range{
				Start: Position{Line: 9, Character: 11},
				End:   Position{Line: 9, Character: 16},
			},
			Kind: Read,
		})
	})

	t.Run("FuncDecoratorArgument", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`func retry(times int, fn func()) {
	fn()
}

@retry(times)
func run(times int) {
	println(times)
}
`),
		}
		s := newTestServer(t, m)

		highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 8},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, highlights)
		assert.Len(t, *highlights, 3)
		assert.Contains(t, *highlights, DocumentHighlight{
			Range: Range{
				Start: Position{Line: 4, Character: 7},
				End:   Position{Line: 4, Character: 12},
			},
			Kind: Read,
		})
		assert.Contains(t, *highlights, DocumentHighlight{
			Range: Range{
				Start: Position{Line: 5, Character: 9},
				End:   Position{Line: 5, Character: 14},
			},
			Kind: Write,
		})
		assert.Contains(t, *highlights, DocumentHighlight{
			Range: Range{
				Start: Position{Line: 6, Character: 9},
				End:   Position{Line: 6, Character: 14},
			},
			Kind: Read,
		})
	})
}
