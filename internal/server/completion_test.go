package server

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletion(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`

MySprite.
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

		emptyLineItems, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, emptyLineItems)
		assert.NotEmpty(t, emptyLineItems)
		assert.True(t, containsCompletionItemLabel(emptyLineItems, "println"))
		assert.True(t, containsCompletionSpxDefinitionID(emptyLineItems, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MySprite"),
		}))

		assert.Contains(t, emptyLineItems, SpxDefinition{
			ID: SpxDefinitionIdentifier{
				Package: ToPtr(SpxPkgPath),
				Name:    ToPtr("Game.getWidget"),
			},
			Overview: "func getWidget(T Type, name WidgetName) *T",
			Detail:   "GetWidget returns the widget instance (in given type) with given name. It panics if not found.\n",

			CompletionItemLabel:            "getWidget",
			CompletionItemKind:             FunctionCompletion,
			CompletionItemInsertText:       "getWidget",
			CompletionItemInsertTextFormat: PlainTextTextFormat,
		}.CompletionItem())

		mySpriteDotItems, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 9},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mySpriteDotItems)
		assert.NotEmpty(t, mySpriteDotItems)
		assert.False(t, containsCompletionItemLabel(mySpriteDotItems, "println"))
		assert.True(t, containsCompletionSpxDefinitionID(mySpriteDotItems, SpxDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.turn"),
			OverloadID: ToPtr("0"),
		}))
		assert.True(t, containsCompletionSpxDefinitionID(mySpriteDotItems, SpxDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.turn"),
			OverloadID: ToPtr("0"),
		}))
		assert.True(t, containsCompletionSpxDefinitionID(mySpriteDotItems, SpxDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.turn"),
			OverloadID: ToPtr("1"),
		}))
		assert.True(t, containsCompletionSpxDefinitionID(mySpriteDotItems, SpxDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.clone"),
			OverloadID: ToPtr("0"),
		}))
		assert.True(t, containsCompletionSpxDefinitionID(mySpriteDotItems, SpxDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.clone"),
			OverloadID: ToPtr("1"),
		}))
	})

	t.Run("InSpxEventHandler", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {

}
run "assets", {Title: "My Game"}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.False(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr(SpxPkgPath),
			Name:    ToPtr("Sprite.onStart"),
		}))
		assert.False(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr(SpxPkgPath),
			Name:    ToPtr("Sprite.onClick"),
		}))
	})

	t.Run("InStringLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
run "a
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 6},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("InComment", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
// Run My G
run "assets", {Title: "My Game"}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 11},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("InStringLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
run "a
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 6},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("InImportStringLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "f
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 9},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "fmt"))
	})

	t.Run("InImportGroupStringLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import (
	"f
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 3},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "fmt"))
	})

	t.Run("PackageMember", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "fmt"
fmt.
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 4},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "println"))
	})

	t.Run("GeneralOrUnknown", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`

onStart => {

}
run "assets", {Title: "My Game"}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items1, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items1)
		assert.NotEmpty(t, items1)
		assert.True(t, containsCompletionItemLabel(items1, "len"))

		items2, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 12},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items2)
		assert.Empty(t, items2)

		items3, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items3)
		assert.NotEmpty(t, items3)
		assert.True(t, containsCompletionItemLabel(items3, "len"))
	})

	t.Run("VarDecl", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func test() {}
onStart => {
	var x i
}
run "assets", {Title: "My Game"}
`),
			"MySprite.spx": []byte(`
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 8},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "int"))
		assert.True(t, containsCompletionItemLabel(items, "MySprite"))
		assert.True(t, containsCompletionItemLabel(items, "Sprite"))
		assert.False(t, containsCompletionItemLabel(items, "len"))
		assert.False(t, containsCompletionItemLabel(items, "test"))
		assert.False(t, containsCompletionItemLabel(items, "play"))
	})

	t.Run("VarDeclAndAssign", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var x SpriteName = "m"
}
run "assets", {Title: "My Game"}
`),
			"MySprite.spx": []byte(`
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 22},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "MySprite"))
	})

	t.Run("SpxSoundResourceStringLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
play "r"
run "assets", {Title: "My Game"}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sounds/recording/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 7},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "recording"))
	})

	t.Run("FuncOverloads", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
play r
run "assets", {Title: "My Game"}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sounds/recording/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 6},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, `"recording"`))
	})

	t.Run("WithImplicitSpxSpriteResource", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
run "assets", {Title: "My Game"}
`),
			"MySprite.spx": []byte(`
onClick => {
	setCostume "c"
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{"costumes":[{"name":"costume"}]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 2, Character: 14},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "costume"))
	})

	t.Run("WithExplicitSpxSpriteResource", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
MySprite.setCostume "c"
run "assets", {Title: "My Game"}
`),
			"MySprite.spx":                       []byte(``),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{"costumes":[{"name":"costume"}]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 22},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "costume"))
	})

	t.Run("WithCrossSpxSpriteResource", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
run "assets", {Title: "My Game"}
`),
			"Sprite1.spx": []byte(`
onClick => {
	Sprite2.setCostume "c"
}
`),
			"Sprite2.spx":                       []byte(``),
			"assets/index.json":                 []byte(`{}`),
			"assets/sprites/Sprite1/index.json": []byte(`{"costumes":[{"name":"Sprite1Costume"}]}`),
			"assets/sprites/Sprite2/index.json": []byte(`{"costumes":[{"name":"Sprite2Costume"}]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///Sprite1.spx"},
				Position:     Position{Line: 2, Character: 22},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "Sprite2Costume"))
	})

	t.Run("AtLineStartWithAnIdentifier", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onClick => {
	pr
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 3},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "println"))
	})

	t.Run("AtLineStartWithAMemberAccessExpression", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
MySprite.setCo`), // Cursor at EOF.
			"MySprite.spx": []byte(`
onClick => {
	MySprite.setCo
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items1, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 14},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items1)
		assert.NotEmpty(t, items1)
		assert.True(t, containsCompletionItemLabel(items1, "setCostume"))

		items2, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 2, Character: 15},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items2)
		assert.NotEmpty(t, items2)
		assert.True(t, containsCompletionItemLabel(items2, "setCostume"))
	})

	t.Run("WithXGoBuiltins", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onClick => {
	var n in
}
`),
			"MySprite.spx": []byte(`
onClick => {
	ec
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items1, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 9},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items1)
		assert.NotEmpty(t, items1)
		assert.True(t, containsCompletionItemLabel(items1, "int128"))

		items2, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 2, Character: 3},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items2)
		assert.NotEmpty(t, items2)
		assert.True(t, containsCompletionItemLabel(items2, "echo"))
	})

	t.Run("MainPackageInterfaceMethod", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Runner interface {
	Run()
}

type MyRunner struct {}
func (r *MyRunner) Run() {}

onStart => {}
	var r Runner = new(MyRunner)
	r.
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 10, Character: 3},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("Runner.Run"),
		}))
		assert.False(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MyRunner.Run"),
		}))
	})

	t.Run("NonMainPackageInterfaceMethod", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "fmt"

type MyStringer struct {}
func (s *MyStringer) String() string {}

onStart => {}
	var s fmt.Stringer = new(MyStringer)
	s.
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 3},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("fmt"),
			Name:    ToPtr("Stringer.string"),
		}))
		assert.False(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MyStringer.String"),
		}))
	})

	t.Run("MainPackageStructLiteralField", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Point struct {
	X int
	Y int
}

onStart => {
	p := Point{}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 7, Character: 12},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			itemData, ok := item.Data.(*CompletionItemData)
			if ok && itemData.Definition.String() == "xgo:main?Point.X" {
				assert.Equal(t, "X: ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
				return true
			}
			return false
		}))
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			itemData, ok := item.Data.(*CompletionItemData)
			if ok && itemData.Definition.String() == "xgo:main?Point.Y" {
				assert.Equal(t, "Y: ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
				return true
			}
			return false
		}))
	})

	t.Run("NonMainPackageStructLiteralField", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "image/color"

onStart => {
	c := color.RGBA{}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 17},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			itemData, ok := item.Data.(*CompletionItemData)
			if ok && itemData.Definition.String() == "xgo:image/color?RGBA.R" {
				assert.Equal(t, "R: ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
				return true
			}
			return false
		}))
	})

	t.Run("UnresolvedFuncCall", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStar => {
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 6},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "onStart"))
	})

	t.Run("WithinIdentifier", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var abc bool
	if ab {
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 6},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "abc"))
	})

	t.Run("ErrorInterfaceMethodCall", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type myError struct{}

func (myError) Error() string {
	return "myError"
}

onStart => {
	var err error = myError{}
	echo err.
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 10},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "error"))
	})

	t.Run("StartWithInvalidChar", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
“”var (
	maps []int
)
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 10},
			},
		})
		require.NoError(t, err)
		require.Nil(t, items)
		assert.Empty(t, items)
	})

	t.Run("MathPackage", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	n := ab
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 8},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "abs"))
	})

	t.Run("StructLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MyStruct struct {
	Foobar int
}

onStart => {
	ms := My
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 6, Character: 9},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "MyStruct"))
	})

	t.Run("StructLitFieldName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MyStruct struct {
	Foobar int
}

onStart => {
	ms := MyStruct{
		Fo
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 7, Character: 4},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "Foobar"))
	})

	t.Run("TypeAssertion", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MyStruct struct {
	Foobar int
}

onStart => {
	var i any = MyStruct{}
	_, ok := i.(My)
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 7, Character: 15},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "MyStruct"))
	})

	t.Run("NoCompletionAfterNumberLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var x = 123.
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 13}, // After "123."
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("NoCompletionAfterNumberLiteralInShortVarDecl", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	x := 123.
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 10}, // After "123."
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("XGoStyleMapLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func printMap(m map[string]int) {
	echo m
}

onStart => {
	var foo int
	printMap {
		"foo": f
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 10}, // After "f"
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "foo"))
	})

	t.Run("XGoStyleMapLitWithMultipleValues", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func printMap(m map[string]string) {
	echo m
}

onStart => {
	var bar, baz string
	printMap {
		"first": bar,
		"second": b
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 13}, // After "b" in second value
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "bar"))
		assert.True(t, containsCompletionItemLabel(items, "baz"))
	})

	t.Run("XGoStyleNestedMapLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func processData(data map[string]map[string]int) {
	echo data
}

onStart => {
	var count int
	processData {
		"nested": {
			"value": c
		}
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 13}, // After "c" in nested map
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "count"))
	})

	t.Run("XGoMapLiteralWithoutType", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func printData(data any) {
	echo data
}

onStart => {
	var myVar string
	printData {
		"name": m
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 11}, // After "m" in map value
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "myVar"))
	})

	t.Run("RegularStructLitNotAffected", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Config struct {
	Name  string
	Value int
}

func setup(cfg Config) {
	echo cfg
}

onStart => {
	var myName string
	setup Config{
		Name: m
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 13, Character: 9}, // After "m" in struct field value
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "myName"))
	})

	t.Run("TypedMapLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var value int
	var data map[string]int
	data = map[string]int{
		"key": value
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 14}, // After "value" in map value
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
	})

	t.Run("TypedMapLiteralAsArgument", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func processMap(m map[string]int) {
	echo m
}

onStart => {
	var num int
	processMap map[string]int{
		"count": n
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 12}, // After "n" in map value
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "num"))
	})

	t.Run("StructLitFieldValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MyStruct struct {
	Field1 string
	Field2 int
}

onStart => {
	var s MyStruct
	s = MyStruct{
		F
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 3}, // After "F" in struct literal
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		if len(items) > 0 {
			hasField1 := containsCompletionItemLabel(items, "Field1")
			hasField2 := containsCompletionItemLabel(items, "Field2")
			assert.True(t, hasField1 || hasField2, "Should suggest at least one struct field")
		}
	})

	t.Run("SimpleReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getName() string {
	var str string = "myName"
	return s
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 9}, // After "s" in return
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "str"))
		assert.True(t, containsCompletionItemLabel(items, "string"))
	})

	t.Run("SimpleAssign", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var str string = "myName"
	str = s
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 8}, // After "s" in assignment
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "str"))
		assert.True(t, containsCompletionItemLabel(items, "string"))
	})

	t.Run("SimpleCallArg", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var str string = "myName"
	println(s)
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 10}, // After "s" in call argument
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "str"))
		assert.True(t, containsCompletionItemLabel(items, "string"))
	})

	t.Run("TypedMapLitInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func countFunc() int { return 42 }
func countFuncNoReturnValue() {}
func countFuncMultiReturnValues() (int, int) { return 0, 1 }

func getData() map[string]int {
	var count int = 42
	return map[string]int{
		"total": c
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 12}, // After "c" in map value
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "count"))
		assert.True(t, containsCompletionItemLabel(items, "countFunc"))
		assert.False(t, containsCompletionItemLabel(items, "countFuncNoReturnValue"))
		assert.False(t, containsCompletionItemLabel(items, "countFuncMultiReturnValues"))
	})

	t.Run("XGoStyleMapLitInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func appNameFunc() string { return "myApp" }
func appNameFuncNoReturnValue() {}
func appNameFuncMultiReturnValues() (string, string) { return "app1", "app2" }

func getConfig() map[string]string {
	var appName = "myApp"
	return {
		"name": a
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 11}, // After "a" in map value
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "appName"))
		assert.True(t, containsCompletionItemLabel(items, "appNameFunc"))
		assert.False(t, containsCompletionItemLabel(items, "appNameFuncNoReturnValue"))
		assert.False(t, containsCompletionItemLabel(items, "appNameFuncMultiReturnValues"))
	})

	t.Run("XGoStyleNestedMapLitInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getNestedData() map[string]map[string]int {
	var total int = 100
	return {
		"stats": {
			"count": t
		}
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 13}, // After "t" in nested map value
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "total"))
	})

	t.Run("MapLitInMultiReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getResult() (map[string]int, error) {
	var result int = 42
	return {
		"value": r
	}, nil
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 12}, // After "r" in map value
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "result"))
	})

	t.Run("TypedStructLitInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Person struct {
	Name string
	Age  int
}

func getPerson() Person {
	var myName = "Alice"
	var myAge = 25
	return Person{
		Name: m
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 10, Character: 9}, // After "m" in struct field value
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "myName"))
		assert.True(t, containsCompletionItemLabel(items, "myAge"))
	})

	t.Run("PointerStructLitInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Config struct {
	Host string
	Port int
}

func getConfig() *Config {
	var defaultHost = "localhost"
	var defaultPort = 8080
	return &Config{
		Host: d
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 10, Character: 9}, // After "d" in struct field value
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "defaultHost"))
		assert.True(t, containsCompletionItemLabel(items, "defaultPort"))
	})

	t.Run("FuncLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var myCallback = func(x int) int {
		var result = x * 2
		return r
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 10}, // After "r" in return statement
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "result"))
	})

	t.Run("FuncLiteralAsArgument", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func process(fn func(int) int) {
	echo fn(10)
}

onStart => {
	var multiplier = 3
	process func(x int) int {
		var product = x * multiplier
		return p
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 10}, // After "p" in return statement
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "product"))
		assert.False(t, containsCompletionItemLabel(items, "process"))
	})

	t.Run("SliceLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var first = 10
	var second = 20
	var nums = []int{
		f
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 3}, // After "f" in slice literal
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "first"))
	})

	t.Run("SliceLiteralInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getNumbers() []int {
	var num1 = 100
	var num2 = 200
	return []int{
		n
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 3}, // After "n" in slice literal
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "num1"))
		assert.True(t, containsCompletionItemLabel(items, "num2"))
	})

	t.Run("XGoStyleSliceLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func printSlice(s []string) {
	echo s
}

onStart => {
	var item1 = "hello"
	var item2 = "world"
	printSlice [
		i
	]
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 3}, // After "i" in slice literal
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "item1"))
		assert.True(t, containsCompletionItemLabel(items, "item2"))
	})

	t.Run("XGoStyleSliceLiteralInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getItems() []string {
	var item1 = "hello"
	var item2 = "world"
	return [
		i
	]
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 3}, // After "i" in slice literal
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "item1"))
		assert.True(t, containsCompletionItemLabel(items, "item2"))
	})

	t.Run("NestedSliceLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func processMatrix(m [][]int) {
	echo m
}

onStart => {
	var value = 42
	processMatrix [][]int{
		[]int{v},
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 9}, // After "v" in nested slice
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "value"))
	})

	t.Run("ArrayLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var element1 = "a"
	var element2 = "b"
	var arr = [3]string{
		e
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 3}, // After "e" in array literal
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "element1"))
		assert.True(t, containsCompletionItemLabel(items, "element2"))
	})

	t.Run("FuncLiteralInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getHandler() func(int) int {
	var factor = 5
	return func(x int) int {
		var result = x * factor
		return r
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 10}, // After "r" in inner return
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "result"))
	})

	t.Run("VarDeclWithValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var str string = "test"
	var x string = s
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 17}, // After "s" in var declaration with value
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "str"))
		assert.True(t, containsCompletionItemLabel(items, "string"))
	})

	t.Run("ConstDeclWithValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var str string = "test"
	const x = s
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 12}, // After "s" in const declaration
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)

		assert.True(t, containsCompletionItemLabel(items, "str"))
		assert.True(t, containsCompletionItemLabel(items, "string"))
	})

	t.Run("ShortVarDecl", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var str string = "test"
	x := s
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 7}, // After "s" in short var decl
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "str"))
		assert.True(t, containsCompletionItemLabel(items, "string"))
	})

	t.Run("MultipleReceiverAssignment", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getTwoValues() (string, int) { return "hello", 42 }
func getSingleValue() string { return "world" }
func getThreeValues() (string, int, bool) { return "test", 123, true }

onStart => {
	var x string
	var y int
	x, y = g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 9}, // After "g" in assignment
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "getTwoValues"), "Should suggest function returning (string, int)")
		assert.True(t, containsCompletionItemLabel(items, "getSingleValue"), "Should suggest single value functions for flexible use")
		assert.False(t, containsCompletionItemLabel(items, "getThreeValues"), "Should not suggest function returning three values")
	})

	t.Run("MultipleReceiverShortVarDecl", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getTwoInts() (int, int) { return 1, 2 }
func getTwoStrings() (string, string) { return "a", "b" }
func getSingleInt() int { return 42 }

onStart => {
	x, y := g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 6, Character: 10}, // After "g" in short var decl
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "getTwoInts"), "Should suggest functions returning two values (int, int)")
		assert.True(t, containsCompletionItemLabel(items, "getTwoStrings"), "Should suggest functions returning two values (string, string)")
		assert.True(t, containsCompletionItemLabel(items, "getSingleInt"), "Should suggest single value functions for flexible use")
	})

	t.Run("MultipleExpressionAssignment", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getInt() int { return 1 }
func getString() string { return "hello" }
func getTwoInts() (int, int) { return 1, 2 }

onStart => {
	var x int
	var y int
	x, y = getInt(), g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 19}, // After "g" in second expression
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "getInt"), "Should suggest function returning int for second position")
		assert.False(t, containsCompletionItemLabel(items, "getString"), "Should not suggest function returning string")
		assert.False(t, containsCompletionItemLabel(items, "getTwoInts"), "Should not suggest function returning multiple values")
	})

	t.Run("MultipleReceiverWithError", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "errors"

func getTwoValuesWithError() (string, error) { return "hello", nil }
func getSingleValueWithError() error { return nil }
func getThreeValues() (string, int, error) { return "test", 123, nil }

onStart => {
	var s string
	var err error
	s, err = g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 10, Character: 11}, // After "g" in assignment
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "getTwoValuesWithError"), "Should suggest function returning (string, error)")
		assert.False(t, containsCompletionItemLabel(items, "getSingleValueWithError"), "Should not suggest single value function with incompatible type")
		assert.False(t, containsCompletionItemLabel(items, "getThreeValues"), "Should not suggest function returning three values")
	})

	t.Run("NestedMultipleReceiverAssignment", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getTwoBools() (bool, bool) { return true, false }
func getSingleBool() bool { return true }

onStart => {
	if x, y := g; x && y {
		// Inside if statement with short var decl
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 13}, // After "g" in if statement init
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "getTwoBools"), "Should suggest function returning two bools")
		assert.True(t, containsCompletionItemLabel(items, "getSingleBool"), "Should suggest single value functions for flexible use")
	})

	t.Run("TypeConversion", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type UserID int
type OrderID int

func getUserID() UserID { return 123 }
func getOrderID() OrderID { return 456 }
func getInt() int { return 789 }

onStart => {
	var id int = g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 15}, // After "g"
			},
		})
		require.NoError(t, err)
		require.NotNil(t, items)
		assert.True(t, containsCompletionItemLabel(items, "getInt"), "Should show exact type match")
		assert.True(t, containsCompletionItemLabel(items, "getUserID"), "Should show convertible type UserID")
		assert.True(t, containsCompletionItemLabel(items, "getOrderID"), "Should show convertible type OrderID")
	})

	t.Run("TypeConversionExclusion", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getPort() int { return 8080 }
func getHost() string { return "localhost" }

onStart => {
	var port int = g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 17}, // After "g" in int assignment
			},
		})
		require.NoError(t, err)
		assert.True(t, containsCompletionItemLabel(items, "getPort"), "Should show int function")
		assert.False(t, containsCompletionItemLabel(items, "getHost"), "Should not suggest string→int conversion")
	})

	t.Run("SelfReferenceInValueExpression", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var counter int = 10
	counter = counter + c
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 22}, // After "c"
			},
		})
		require.NoError(t, err)
		assert.True(t, containsCompletionItemLabel(items, "counter"), "Should show counter in value expression")
	})

	t.Run("CombinedSingleReturns", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getX() int { return 1 }
func getY() int { return 2 }
func getPair() (int, int) { return 1, 2 }

onStart => {
	var x, y int
	x, y = g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 7, Character: 9}, // After "g"
			},
		})
		require.NoError(t, err)
		assert.True(t, containsCompletionItemLabel(items, "getPair"), "Should show function with matching return count")
		assert.True(t, containsCompletionItemLabel(items, "getX"), "Should show single return for flexible use")
		assert.True(t, containsCompletionItemLabel(items, "getY"), "Should show single return for flexible use")
	})
}

func containsCompletionItemLabel(items []CompletionItem, label string) bool {
	return slices.ContainsFunc(items, func(item CompletionItem) bool {
		return item.Label == label
	})
}

func containsCompletionSpxDefinitionID(items []CompletionItem, id SpxDefinitionIdentifier) bool {
	return slices.ContainsFunc(items, func(item CompletionItem) bool {
		itemData, ok := item.Data.(*CompletionItemData)
		if !ok {
			return false
		}
		return itemData.Definition.String() == id.String()
	})
}
