package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentReferences(t *testing.T) {
	for _, tt := range []struct {
		name               string
		filename           string
		includeDeclaration bool
	}{
		{name: "XGo", filename: "main.xgo", includeDeclaration: true},
		{name: "Gop", filename: "main.gop", includeDeclaration: true},
		{name: "NormalClass", filename: "Record.gox", includeDeclaration: true},
		{name: "ExcludeDeclaration", filename: "main.xgo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				tt.filename: []byte("var value int\nfunc use() { println(value) }\n"),
			})
			uri := s.toDocumentURI(tt.filename)
			refs, err := s.textDocumentReferences(&ReferenceParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: uri},
					Position:     Position{Line: 1, Character: 21},
				},
				Context: ReferenceContext{IncludeDeclaration: tt.includeDeclaration},
			})
			require.NoError(t, err)
			want := []Location{{
				URI: uri,
				Range: Range{
					Start: Position{Line: 1, Character: 21},
					End:   Position{Line: 1, Character: 26},
				},
			}}
			if tt.includeDeclaration {
				want = append(want, Location{
					URI: uri,
					Range: Range{
						Start: Position{Line: 0, Character: 4},
						End:   Position{Line: 0, Character: 9},
					},
				})
			}
			assert.ElementsMatch(t, want, refs)
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
			refs, err := s.textDocumentReferences(&ReferenceParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: tt.uri},
				},
			})
			if tt.wantErr {
				assert.ErrorContains(t, err, "workspace root URI")
			} else {
				assert.NoError(t, err)
			}
			assert.Nil(t, refs)
		})
	}

	t.Run("FileUpdatesWithTypeErrors", func(t *testing.T) {
		files := map[string][]byte{
			"main.xgo": []byte("var value int\nfunc use() { println(value) }\n"),
		}
		s := newTestServer(t, files)
		params := &ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 0, Character: 4},
			},
			Context: ReferenceContext{IncludeDeclaration: true},
		}
		refs, err := s.textDocumentReferences(params)
		require.NoError(t, err)
		require.Len(t, refs, 2)

		s.ModifyFiles([]FileChange{{
			Path:    "main.xgo",
			Content: append(files["main.xgo"], []byte("func again() { println(value); missing() }\n")...),
			Version: 1,
		}})
		refs, err = s.textDocumentReferences(params)
		require.NoError(t, err)
		assert.ElementsMatch(t, []Location{
			{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}},
			{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 1, Character: 21}, End: Position{Line: 1, Character: 26}}},
			{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 2, Character: 23}, End: Position{Line: 2, Character: 28}}},
		}, refs)
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

		workerRefs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 1, Character: 0},
			},
			Context: ReferenceContext{
				IncludeDeclaration: true,
			},
		})
		require.NoError(t, err)
		require.Len(t, workerRefs, 3)
		assert.Contains(t, workerRefs, Location{
			URI: "file:///main_fixture.gox",
			Range: Range{
				Start: Position{Line: 1, Character: 0},
				End:   Position{Line: 1, Character: 6},
			},
		})
		assert.Contains(t, workerRefs, Location{
			URI: "file:///main_fixture.gox",
			Range: Range{
				Start: Position{Line: 2, Character: 0},
				End:   Position{Line: 2, Character: 6},
			},
		})
		assert.Contains(t, workerRefs, Location{
			URI: "file:///Worker_fixture.gox",
			Range: Range{
				Start: Position{Line: 2, Character: 1},
				End:   Position{Line: 2, Character: 7},
			},
		})

		applyRefs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 1, Character: 7},
			},
			Context: ReferenceContext{
				IncludeDeclaration: true,
			},
		})
		require.NoError(t, err)
		require.Len(t, applyRefs, 3)
		assert.Contains(t, applyRefs, Location{
			URI: "file:///main_fixture.gox",
			Range: Range{
				Start: Position{Line: 1, Character: 7},
				End:   Position{Line: 1, Character: 12},
			},
		})
		assert.Contains(t, applyRefs, Location{
			URI: "file:///main_fixture.gox",
			Range: Range{
				Start: Position{Line: 2, Character: 7},
				End:   Position{Line: 2, Character: 12},
			},
		})
		assert.Contains(t, applyRefs, Location{
			URI: "file:///Worker_fixture.gox",
			Range: Range{
				Start: Position{Line: 2, Character: 8},
				End:   Position{Line: 2, Character: 13},
			},
		})
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("InvalidPosition", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`var x int`),
		}
		s := newTestServer(t, m)

		refs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 99, Character: 99},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, refs)
	})

	t.Run("MethodsAcrossFiles", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			position Position
		}{
			{name: "ConcreteMethod", position: Position{Line: 1, Character: 11}},
			{name: "EmbeddedMethod", position: Position{Line: 4, Character: 12}},
			{name: "NestedEmbeddedMethod", position: Position{Line: 5, Character: 10}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"types.xgo": []byte(`type Runner interface { Run() }
type Extended interface { Runner }
type Unrelated interface { Stop() }
type Worker struct{}
func (Worker) Run() {}
type Wrapper struct { Worker }
type Outer struct { Wrapper }
type Different struct{}
func (Different) Run(int) {}
`),
					"main.xgo": []byte(`func use(worker Worker, runner Runner, extended Extended, wrapper Wrapper, outer Outer, different Different) {
    worker.run()
    runner.run()
    extended.run()
    wrapper.run()
    outer.run()
    different.run 1
}
`),
				})
				refs, err := s.textDocumentReferences(&ReferenceParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
						Position:     tt.position,
					},
					Context: ReferenceContext{IncludeDeclaration: true},
				})
				require.NoError(t, err)
				assert.ElementsMatch(t, []Location{
					{URI: "file:///types.xgo", Range: Range{Start: Position{Line: 4, Character: 14}, End: Position{Line: 4, Character: 17}}},
					{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 1, Character: 11}, End: Position{Line: 1, Character: 14}}},
					{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 2, Character: 11}, End: Position{Line: 2, Character: 14}}},
					{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 3, Character: 13}, End: Position{Line: 3, Character: 16}}},
					{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 4, Character: 12}, End: Position{Line: 4, Character: 15}}},
					{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 5, Character: 10}, End: Position{Line: 5, Character: 13}}},
				}, refs)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
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

		refs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 2, Character: 4},
			},
			Context: ReferenceContext{
				IncludeDeclaration: true,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, refs)
		require.Len(t, refs, 4)
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 2, Character: 1},
				End:   Position{Line: 2, Character: 6},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 8, Character: 11},
				End:   Position{Line: 8, Character: 16},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 9, Character: 11},
				End:   Position{Line: 9, Character: 16},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///other.xgo",
			Range: Range{
				Start: Position{Line: 0, Character: 25},
				End:   Position{Line: 0, Character: 30},
			},
		})
	})

	t.Run("OverloadKwargField", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Worker struct{}

type CountOptions struct {
    Count int
}

type NameOptions struct {
    Name string
}

var worker Worker

func (w *Worker) handleCount(opts CountOptions?) {}
func (w *Worker) handleName(opts NameOptions?) {}

func (Worker).handle = (
    (Worker).handleCount
    (Worker).handleName
)

func main() {
    worker.handle count = 1
    worker.handle count = 2
}
`),
		}
		s := newTestServer(t, m)

		refs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 5},
			},
			Context: ReferenceContext{
				IncludeDeclaration: true,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, refs)
		require.Len(t, refs, 3)
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 4, Character: 4},
				End:   Position{Line: 4, Character: 9},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 22, Character: 18},
				End:   Position{Line: 22, Character: 23},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 23, Character: 18},
				End:   Position{Line: 23, Character: 23},
			},
		})
	})

	t.Run("KwargInterfaceMethod", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Client struct{}

type Params interface {
	MaxTokens(n int64) Params
}

var client Client

func (c Client) Params() Params { return nil }

func (c Client) complete(prompt string, params Params?) {}

func main() {
	client.complete "hi", maxTokens = 1
	client.complete "bye", maxTokens = 2
}
`),
		}
		s := newTestServer(t, m)

		refs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 14, Character: 25},
			},
			Context: ReferenceContext{
				IncludeDeclaration: true,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, refs)
		require.Len(t, refs, 3)
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 4, Character: 1},
				End:   Position{Line: 4, Character: 10},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 14, Character: 23},
				End:   Position{Line: 14, Character: 32},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 15, Character: 24},
				End:   Position{Line: 15, Character: 33},
			},
		})
	})
}
