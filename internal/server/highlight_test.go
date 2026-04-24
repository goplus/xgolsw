package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentDocumentHighlight(t *testing.T) {
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

		mySpriteHighlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mySpriteHighlights)
		assert.Len(t, *mySpriteHighlights, 2)
		assert.Contains(t, *mySpriteHighlights, DocumentHighlight{
			Range: Range{
				Start: Position{Line: 1, Character: 0},
				End:   Position{Line: 1, Character: 8},
			},
			Kind: Read,
		})
		assert.Contains(t, *mySpriteHighlights, DocumentHighlight{
			Range: Range{
				Start: Position{Line: 2, Character: 0},
				End:   Position{Line: 2, Character: 8},
			},
			Kind: Read,
		})

		leftHighlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 14},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, leftHighlights)
		assert.Len(t, *leftHighlights, 1)
		assert.Contains(t, *leftHighlights, DocumentHighlight{
			Range: Range{
				Start: Position{Line: 2, Character: 14},
				End:   Position{Line: 2, Character: 19},
			},
			Kind: Read,
		})
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

		highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
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
}
