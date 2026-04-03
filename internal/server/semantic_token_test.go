package server

import (
	"go/types"
	"strings"
	"testing"

	"github.com/goplus/gogen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentSemanticTokensFull(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
MySprite.turn Left
run "assets", {Title: "My Game"}
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
			1, 0, 3, 7, 0, // run
			0, 4, 8, 11, 0, // assets
			0, 10, 1, 13, 0, // {
			0, 1, 5, 6, 0, // Title
			0, 5, 1, 13, 0, // :
			0, 2, 9, 11, 0, // My Game
			0, 9, 1, 13, 0, // }
			0, 1, 1, 13, 0, // }
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

	t.Run("LocalVariableKind", func(t *testing.T) {
		const mySpriteSrc = `onStart => {
 z := 1
 echo z
}
`
		m := map[string][]byte{
			"main.spx":                           []byte(`run "assets", {Title: "My Game"}`),
			"MySprite.spx":                       []byte(mySpriteSrc),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, tokens)

		var zTokens []decodedSemanticToken
		for _, tok := range decodeSemanticTokens(mySpriteSrc, tokens.Data) {
			if tok.text == "z" {
				zTokens = append(zTokens, tok)
			}
		}
		require.Len(t, zTokens, 2)
		assert.Equal(t, getSemanticTokenTypeIndex(VariableType), zTokens[0].tokenType)
		assert.Equal(t, getSemanticTokenTypeIndex(VariableType), zTokens[1].tokenType)
	})
}

func TestSemanticTokenTypeForVarKind(t *testing.T) {
	for _, tt := range []struct {
		name string
		kind types.VarKind
		want SemanticTokenTypes
	}{
		{name: "ParamVar", kind: types.ParamVar, want: ParameterType},
		{name: "RecvVar", kind: types.RecvVar, want: ParameterType},
		{name: "ParamOptionalVar", kind: types.VarKind(gogen.ParamOptionalVar), want: ParameterType},
		{name: "PackageVar", kind: types.PackageVar, want: VariableType},
		{name: "LocalVar", kind: types.LocalVar, want: VariableType},
		{name: "ResultVar", kind: types.ResultVar, want: VariableType},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, semanticTokenTypeForVarKind(tt.kind))
		})
	}
}

type decodedSemanticToken struct {
	line      uint32
	char      uint32
	length    uint32
	tokenType uint32
	text      string
}

func decodeSemanticTokens(src string, data []uint32) []decodedSemanticToken {
	lines := strings.Split(src, "\n")
	var (
		line uint32
		char uint32
		out  []decodedSemanticToken
	)
	for i := 0; i+4 < len(data); i += 5 {
		line += data[i]
		if data[i] == 0 {
			char += data[i+1]
		} else {
			char = data[i+1]
		}
		length := data[i+2]
		text := ""
		if int(line) < len(lines) {
			row := lines[line]
			start := int(char)
			end := start + int(length)
			if start >= 0 && end <= len(row) {
				text = row[start:end]
			}
		}
		out = append(out, decodedSemanticToken{
			line:      line,
			char:      char,
			length:    length,
			tokenType: data[i+3],
			text:      text,
		})
	}
	return out
}
