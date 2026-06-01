package server

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentSemanticTokensFull(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
MySprite.turn Left
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

		mainSpxTokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, mainSpxTokens)
		assert.Equal(t, []uint32{
			1, 0, 1, 13, 0, // {
			0, 0, 8, 6, 0, // MySprite
			0, 8, 1, 13, 0, // .
			0, 1, 4, 8, 0, // turn
			0, 5, 4, 5, 6, // Left
			0, 4, 1, 13, 0, // }
		}, mainSpxTokens.Data)

		mySpriteTokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, mySpriteTokens)
		assert.Equal(t, []uint32{
			1, 0, 1, 13, 0, //{
			0, 0, 7, 8, 0, // onStart
			0, 8, 2, 13, 0, // =>
			0, 3, 1, 13, 0, // {
			1, 1, 8, 6, 0, // MySprite
			0, 8, 1, 13, 0, // .
			0, 1, 4, 8, 0, // turn
			0, 5, 5, 5, 6, // Right
			1, 0, 1, 13, 0, // }
			0, 1, 1, 13, 0, // }
		}, mySpriteTokens.Data)
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
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)

		assert.Contains(t, decodeSemanticTokens(tokens.Data), decodedSemanticToken{
			line:      8,
			character: 11,
			length:    5,
			tokenType: PropertyType,
		})
	})

	t.Run("XGoUnit", func(t *testing.T) {
		s := newXGoUnitTestServer(`import "time"

func wait(d time.Duration) {}

onStart => {
	wait 1m
}
`)

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)

		assert.Contains(t, decodeSemanticTokens(tokens.Data), decodedSemanticToken{
			line:      5,
			character: 6,
			length:    1,
			tokenType: NumberType,
		})
	})
}

type decodedSemanticToken struct {
	line      uint32
	character uint32
	length    uint32
	tokenType SemanticTokenTypes
}

func decodeSemanticTokens(data []uint32) []decodedSemanticToken {
	if len(data)%5 != 0 {
		return nil
	}

	var (
		line      uint32
		character uint32
		tokens    []decodedSemanticToken
	)
	for tokenData := range slices.Chunk(data, 5) {
		line += tokenData[0]
		if tokenData[0] == 0 {
			character += tokenData[1]
		} else {
			character = tokenData[1]
		}

		tokenTypeIndex := tokenData[3]
		if int(tokenTypeIndex) >= len(semanticTokenTypesLegend) {
			continue
		}
		tokens = append(tokens, decodedSemanticToken{
			line:      line,
			character: character,
			length:    tokenData[2],
			tokenType: semanticTokenTypesLegend[tokenTypeIndex],
		})
	}
	return tokens
}
