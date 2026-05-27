package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentReferences(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
MySprite.turn Left
MySprite.turn Right
`),
			"MySprite.spx": []byte(`
onStart => {
	MySprite.turn Right
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		mainSpxMySpriteRef, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 0},
			},
			Context: ReferenceContext{
				IncludeDeclaration: true,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mainSpxMySpriteRef)
		require.Len(t, mainSpxMySpriteRef, 3)
		assert.Contains(t, mainSpxMySpriteRef, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 1, Character: 0},
				End:   Position{Line: 1, Character: 8},
			},
		})
		assert.Contains(t, mainSpxMySpriteRef, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 2, Character: 0},
				End:   Position{Line: 2, Character: 8},
			},
		})
		assert.Contains(t, mainSpxMySpriteRef, Location{
			URI: "file:///MySprite.spx",
			Range: Range{
				Start: Position{Line: 2, Character: 1},
				End:   Position{Line: 2, Character: 9},
			},
		})

		mainSpxTurnRef, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 9},
			},
			Context: ReferenceContext{
				IncludeDeclaration: true,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mainSpxTurnRef)
		require.Len(t, mainSpxTurnRef, 3)
		assert.Contains(t, mainSpxTurnRef, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 1, Character: 9},
				End:   Position{Line: 1, Character: 13},
			},
		})
		assert.Contains(t, mainSpxTurnRef, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 2, Character: 9},
				End:   Position{Line: 2, Character: 13},
			},
		})
		assert.Contains(t, mainSpxTurnRef, Location{
			URI: "file:///MySprite.spx",
			Range: Range{
				Start: Position{Line: 2, Character: 10},
				End:   Position{Line: 2, Character: 14},
			},
		})
	})

	t.Run("InvalidPosition", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`var x int`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		refs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 99, Character: 99},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, refs)
	})

	t.Run("KwargField", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

onStart => {
	configure count = 1
	configure count = 2
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		refs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 4},
			},
			Context: ReferenceContext{
				IncludeDeclaration: true,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, refs)
		require.Len(t, refs, 3)
		assert.Contains(t, refs, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 2, Character: 1},
				End:   Position{Line: 2, Character: 6},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 8, Character: 11},
				End:   Position{Line: 8, Character: 16},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 9, Character: 11},
				End:   Position{Line: 9, Character: 16},
			},
		})
	})

	t.Run("OverloadKwargField", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
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

onStart => {
    worker.handle count = 1
    worker.handle count = 2
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		refs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
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
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 4, Character: 4},
				End:   Position{Line: 4, Character: 9},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 22, Character: 18},
				End:   Position{Line: 22, Character: 23},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 23, Character: 18},
				End:   Position{Line: 23, Character: 23},
			},
		})
	})

	t.Run("KwargInterfaceMethod", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Client struct{}

type Params interface {
	MaxTokens(n int64) Params
}

func (c Client) Params() Params { return nil }

func (c Client) complete(prompt string, params Params?) {}

var client Client

onStart => {
	client.complete "hi", maxTokens = 1
	client.complete "bye", maxTokens = 2
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		refs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
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
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 4, Character: 1},
				End:   Position{Line: 4, Character: 10},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 14, Character: 23},
				End:   Position{Line: 14, Character: 32},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 15, Character: 24},
				End:   Position{Line: 15, Character: 33},
			},
		})
	})
}
