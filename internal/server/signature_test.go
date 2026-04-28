package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextDocumentSignatureHelp(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "fmt"
fmt.Println 
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

		fmtHelp, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 10},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, fmtHelp)
		require.Len(t, fmtHelp.Signatures, 1)
		assert.Equal(t, SignatureInformation{
			Label: "println(a ...any) (n int, err error)",
			Parameters: []ParameterInformation{
				{
					Label: "a ...any",
				},
			},
		}, fmtHelp.Signatures[0])

		turnHelp, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 10},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, turnHelp)
		require.Len(t, turnHelp.Signatures, 1)
		assert.Equal(t, SignatureInformation{
			Label: "turn(dir Direction)",
			Parameters: []ParameterInformation{
				{
					Label: "dir Direction",
				},
			},
		}, turnHelp.Signatures[0])
	})

	t.Run("SingleResult", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func answer() int { return 42 }

onStart => {
	answer
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 4},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, "answer() int", help.Signatures[0].Label)
		assert.Empty(t, help.Signatures[0].Parameters)
	})

	t.Run("SingleNamedResult", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func answer() (n int) { return 42 }

onStart => {
	answer
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 4},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, "answer() (n int)", help.Signatures[0].Label)
		assert.Empty(t, help.Signatures[0].Parameters)
	})

	t.Run("XGoxMethod", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	getWidget Monitor, "myWidget"
}
`),
			"assets/index.json": []byte(`{"zorder":[{"name":"myWidget"}]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, SignatureInformation{
			Label: "getWidget(T Type, name WidgetName) *T",
			Parameters: []ParameterInformation{
				{
					Label: "T Type",
				},
				{
					Label: "name WidgetName",
				},
			},
		}, help.Signatures[0])
	})
}
