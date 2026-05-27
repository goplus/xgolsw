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
		assert.Equal(t, uint32(0), help.ActiveParameter)
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

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 13},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label: "configure(opts main.Options)",
			Parameters: []ParameterInformation{
				{
					Label: "opts main.Options",
				},
			},
		}, help.Signatures[0])
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
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 22, Character: 18},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label: "handle(opts main.CountOptions)",
			Parameters: []ParameterInformation{
				{
					Label: "opts main.CountOptions",
				},
			},
		}, help.Signatures[0])
	})

	t.Run("OverloadIncompleteKwargName", func(t *testing.T) {
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
	worker.handle cou = 1
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 22, Character: 16},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 2)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, "handle(opts main.CountOptions)", help.Signatures[0].Label)
		assert.Equal(t, "handle(opts main.NameOptions)", help.Signatures[1].Label)
	})

	t.Run("VariadicKwargActiveParameter", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func process(opts map[string]string?, args ...int) {}

onStart => {
    process 1, name = "x"
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		positionalHelp, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 12},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, positionalHelp)
		require.Len(t, positionalHelp.Signatures, 1)
		assert.Equal(t, uint32(1), positionalHelp.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label: "process(opts map[string]string, args ...int)",
			Parameters: []ParameterInformation{
				{
					Label: "opts map[string]string",
				},
				{
					Label: "args ...int",
				},
			},
		}, positionalHelp.Signatures[0])

		kwargHelp, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 23},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, kwargHelp)
		require.Len(t, kwargHelp.Signatures, 1)
		assert.Equal(t, uint32(0), kwargHelp.ActiveParameter)
		assert.Equal(t, positionalHelp.Signatures[0], kwargHelp.Signatures[0])
	})
}
