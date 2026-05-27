package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentDefinition(t *testing.T) {
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

		mainSpxMySpriteDef, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 0},
			},
		})
		require.NoError(t, err)
		require.Nil(t, mainSpxMySpriteDef)

		mainSpxMySpriteTurnDef, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 9},
			},
		})
		require.NoError(t, err)
		require.Nil(t, mainSpxMySpriteTurnDef)

		mySpriteSpxMySpriteDef, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		require.Nil(t, mySpriteSpxMySpriteDef)
	})

	t.Run("BuiltinType", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var x int
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 6},
			},
		})
		require.NoError(t, err)
		require.Nil(t, def)
	})

	t.Run("ThisPtr", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 0},
			},
		})
		require.NoError(t, err)
		require.Nil(t, def)
	})

	t.Run("BlankIdent", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
const _ = 1
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 6},
			},
		})
		require.NoError(t, err)
		require.Nil(t, def)
	})

	t.Run("InvalidPosition", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var x int
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 99, Character: 99},
			},
		})
		require.NoError(t, err)
		require.Nil(t, def)
	})

	t.Run("ImportedPackage", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "fmt"
fmt.println "Hello, spx!"
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, def)
		loc := requireLocation(t, def)
		assert.Equal(t, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 1, Character: 7},
				End:   Position{Line: 1, Character: 7},
			},
		}, loc)
	})

	t.Run("ImportedPackageWithAlias", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import fmt2 "fmt"
fmt2.println "Hello, spx!"
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, def)
		loc := requireLocation(t, def)
		assert.Equal(t, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 1, Character: 7},
				End:   Position{Line: 1, Character: 11},
			},
		}, loc)
	})

	t.Run("InvalidTextDocument", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var x int
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "bucket:///main.spx"},
				Position:     Position{Line: 99, Character: 99},
			},
		})
		require.Contains(t, err.Error(), "failed to get file path from document URI")
		require.Nil(t, def)
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

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 14},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, def)
		loc := requireLocation(t, def)
		assert.Equal(t, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 2, Character: 4},
				End:   Position{Line: 2, Character: 9},
			},
		}, loc)
	})

	t.Run("MapKwargHasNoDefinition", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func configure(opts map[string]int?) {}

onStart => {
    configure count = 1
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 14},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, def)
	})

	t.Run("NestedKwargField", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type OuterOptions struct {
    Count int
}

type InnerOptions struct {
    Name string
}

func makeValue(opts InnerOptions?) int { return 0 }

func configure(value int, opts OuterOptions?) {}

onStart => {
    configure makeValue(name = "x"), count = 1
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		innerDef, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 14, Character: 25},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, innerDef)
		innerLoc := requireLocation(t, innerDef)
		assert.Equal(t, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 6, Character: 4},
				End:   Position{Line: 6, Character: 8},
			},
		}, innerLoc)

		outerDef, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 14, Character: 38},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, outerDef)
		outerLoc := requireLocation(t, outerDef)
		assert.Equal(t, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 2, Character: 4},
				End:   Position{Line: 2, Character: 9},
			},
		}, outerLoc)
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

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 22, Character: 19},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, def)
		loc := requireLocation(t, def)
		assert.Equal(t, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 4, Character: 4},
				End:   Position{Line: 4, Character: 9},
			},
		}, loc)
	})

	t.Run("OverloadKwargFieldDisambiguatesByValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Worker struct{}

type Options0 struct {
    Handler func()
}

type Options1 struct {
    Handler func(int)
}

var worker Worker

func (w *Worker) handle0(opts Options0?) {}
func (w *Worker) handle1(opts Options1?) {}

func (Worker).handle = (
    (Worker).handle0
    (Worker).handle1
)

onStart => {
    worker.handle handler = (n) => {
        echo n
    }
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 22, Character: 20},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, def)
		loc := requireLocation(t, def)
		assert.Equal(t, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 8, Character: 4},
				End:   Position{Line: 8, Character: 11},
			},
		}, loc)
	})

	t.Run("NonOptionalKwargField", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
    Count int
}

func configure(opts Options) {}

onStart => {
    configure count = 1
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 14},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, def)
		loc := requireLocation(t, def)
		assert.Equal(t, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 2, Character: 4},
				End:   Position{Line: 2, Character: 9},
			},
		}, loc)
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
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 14, Character: 25},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, def)
		loc := requireLocation(t, def)
		assert.Equal(t, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 4, Character: 1},
				End:   Position{Line: 4, Character: 10},
			},
		}, loc)
	})
}

func requireLocation(t *testing.T, v any) Location {
	t.Helper()

	loc, ok := v.(Location)
	require.True(t, ok)
	return loc
}

func TestServerTextDocumentTypeDefinition(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MyType struct {
	field int
}
var x MyType
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 6},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, def)
		loc := requireLocation(t, def)
		assert.Equal(t, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 1, Character: 5},
				End:   Position{Line: 1, Character: 5},
			},
		}, loc)
	})

	t.Run("AliasType", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MyType struct{}
type MyAlias = MyType
var x MyAlias
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 4},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, def)
		loc := requireLocation(t, def)
		assert.Equal(t, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 2, Character: 5},
				End:   Position{Line: 2, Character: 5},
			},
		}, loc)
	})

	t.Run("KwargField", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Handler func()

type Options struct {
    Handler Handler
}

func configure(opts Options?) {}

onStart => {
    configure handler = () => {}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 10, Character: 14},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, def)
		loc := requireLocation(t, def)
		assert.Equal(t, Location{
			URI: "file:///main.spx",
			Range: Range{
				Start: Position{Line: 1, Character: 5},
				End:   Position{Line: 1, Character: 5},
			},
		}, loc)
	})

	t.Run("SpriteType", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var (
	MySprite Sprite
)
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 10},
			},
		})
		require.NoError(t, err)
		require.Nil(t, def)
	})

	t.Run("BuiltinType", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var x int
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 6},
			},
		})
		require.NoError(t, err)
		require.Nil(t, def)
	})

	t.Run("InvalidPosition", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var x int
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		def, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 99, Character: 99},
			},
		})
		require.NoError(t, err)
		require.Nil(t, def)
	})
}
