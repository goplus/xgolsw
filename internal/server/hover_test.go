package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentHover(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import (
	"fmt"
	"image"
)

var (
	// count is a variable.
	count int

	imagePoint image.Point
)

// MaxCount is a constant.
const MaxCount = 100

// Add is a function.
func Add(x, y int) int {
	return x + y
}

// Point is a type.
type Point struct {
	// X is a field.
	X int

	// Y is a field.
	Y int
}

fmt.Println(int8(1))

play "MySound"
MySprite.turn Left
MySprite.setCostume "costume1"
Game.onClick => {}
onClick => {}
follow "MySprite"
run "assets", {Title: "My Game"}
`),
			"MySprite.spx": []byte(`
MySprite.onClick => {}
onClick => {}
onStart => {
	MySprite.turn Right
	clone
	imagePoint.X = 100
}
onTouchStart "MySprite", => {}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{"costumes":[{"name":"costume1"}]}`),
			"assets/sounds/MySound/index.json":   []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		varHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, varHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:main?Game.count\" overview=\"var count int\">\ncount is a variable.\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 8, Character: 1},
				End:   Position{Line: 8, Character: 6},
			},
		}, varHover)

		constHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 14, Character: 6},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, constHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:main?MaxCount\" overview=\"const MaxCount = 100\">\nMaxCount is a constant.\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 14, Character: 6},
				End:   Position{Line: 14, Character: 14},
			},
		}, constHover)

		funcHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 17, Character: 5},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, funcHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:main?Game.Add\" overview=\"func Add(x int, y int) int\">\nAdd is a function.\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 17, Character: 5},
				End:   Position{Line: 17, Character: 8},
			},
		}, funcHover)

		typeHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 22, Character: 5},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, typeHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:main?Point\" overview=\"type Point\">\nPoint is a type.\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 22, Character: 5},
				End:   Position{Line: 22, Character: 10},
			},
		}, typeHover)

		typeFieldHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 24, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, typeFieldHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:main?Point.X\" overview=\"field X int\">\nX is a field.\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 24, Character: 1},
				End:   Position{Line: 24, Character: 2},
			},
		}, typeFieldHover)

		pkgHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 30, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pkgHover)
		assert.Equal(t, Range{
			Start: Position{Line: 30, Character: 0},
			End:   Position{Line: 30, Character: 3},
		}, pkgHover.Range)

		pkgFuncHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 30, Character: 4},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, pkgFuncHover)
		assert.Equal(t, Range{
			Start: Position{Line: 30, Character: 4},
			End:   Position{Line: 30, Character: 11},
		}, pkgFuncHover.Range)

		builtinFuncHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 30, Character: 12},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, builtinFuncHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:builtin?int8\" overview=\"type int8\">\nint8 is the set of all signed 8-bit integers.\nRange: -128 through 127.\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 30, Character: 12},
				End:   Position{Line: 30, Character: 16},
			},
		}, builtinFuncHover)

		mySoundRefHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 32, Character: 5},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mySoundRefHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<resource-preview resource=\"spx://resources/sounds/MySound\" />\n",
			},
			Range: Range{
				Start: Position{Line: 32, Character: 5},
				End:   Position{Line: 32, Character: 14},
			},
		}, mySoundRefHover)

		mySpriteRefHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 33, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mySpriteRefHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<resource-preview resource=\"spx://resources/sprites/MySprite\" />\n",
			},
			Range: Range{
				Start: Position{Line: 33, Character: 0},
				End:   Position{Line: 33, Character: 8},
			},
		}, mySpriteRefHover)

		mySpriteCostumeRefHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 34, Character: 20},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mySpriteCostumeRefHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<resource-preview resource=\"spx://resources/sprites/MySprite/costumes/costume1\" />\n",
			},
			Range: Range{
				Start: Position{Line: 34, Character: 20},
				End:   Position{Line: 34, Character: 30},
			},
		}, mySpriteCostumeRefHover)

		mySpriteSetCostumeFuncHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 34, Character: 9},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mySpriteSetCostumeFuncHover)
		assert.Equal(t, Range{
			Start: Position{Line: 34, Character: 9},
			End:   Position{Line: 34, Character: 19},
		}, mySpriteSetCostumeFuncHover.Range)

		GameOnClickHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 35, Character: 5},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, GameOnClickHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:github.com/goplus/spx/v2?Game.onClick\" overview=\"func onClick(onClick func())\">\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 35, Character: 5},
				End:   Position{Line: 35, Character: 12},
			},
		}, GameOnClickHover)

		mainSpxOnClickHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 36, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mainSpxOnClickHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:github.com/goplus/spx/v2?Game.onClick\" overview=\"func onClick(onClick func())\">\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 36, Character: 0},
				End:   Position{Line: 36, Character: 7},
			},
		}, mainSpxOnClickHover)

		mainSpxFollowHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 37, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mainSpxFollowHover)
		assert.Contains(t, mainSpxFollowHover.Contents.Value, `def-id="xgo:github.com/goplus/spx/v2?Game.follow#1"`)
		assert.Equal(t, Range{
			Start: Position{Line: 37, Character: 0},
			End:   Position{Line: 37, Character: 6},
		}, mainSpxFollowHover.Range)

		mySpriteOnClickFuncHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 1, Character: 9},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mySpriteOnClickFuncHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:github.com/goplus/spx/v2?Sprite.onClick\" overview=\"func onClick(onClick func())\">\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 1, Character: 9},
				End:   Position{Line: 1, Character: 16},
			},
		}, mySpriteOnClickFuncHover)

		mySpriteSpxOnClickFuncHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 2, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mySpriteSpxOnClickFuncHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:github.com/goplus/spx/v2?Sprite.onClick\" overview=\"func onClick(onClick func())\">\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 2, Character: 0},
				End:   Position{Line: 2, Character: 7},
			},
		}, mySpriteSpxOnClickFuncHover)

		mySpriteCloneFuncHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 5, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mySpriteCloneFuncHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:github.com/goplus/spx/v2?Sprite.clone#0\" overview=\"func clone()\">\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 5, Character: 1},
				End:   Position{Line: 5, Character: 6},
			},
		}, mySpriteCloneFuncHover)

		imagePointFieldHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 6, Character: 12},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, imagePointFieldHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:image?Point.X\" overview=\"field X int\">\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 6, Character: 12},
				End:   Position{Line: 6, Character: 13},
			},
		}, imagePointFieldHover)

		onTouchStartFirstArgHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 8, Character: 14},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, onTouchStartFirstArgHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<resource-preview resource=\"spx://resources/sprites/MySprite\" />\n",
			},
			Range: Range{
				Start: Position{Line: 8, Character: 13},
				End:   Position{Line: 8, Character: 23},
			},
		}, onTouchStartFirstArgHover)
	})

	t.Run("InvalidPosition", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`var x int`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 99, Character: 99},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, hover)
	})

	t.Run("ImportsAtASTFilePosition", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import (
	"fmt"
	"image"
)

fmt.Println("Hello, World!")
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		importHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, importHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "Package fmt implements formatted I/O with functions analogous to C's printf and scanf.",
			},
			Range: Range{
				Start: Position{Line: 2, Character: 1},
				End:   Position{Line: 2, Character: 6},
			},
		}, importHover)
	})

	t.Run("Append", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var nums []int
nums = append(nums, 1)
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 7},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, `def-id="xgo:builtin?append"`)
		assert.Equal(t, Range{
			Start: Position{Line: 2, Character: 7},
			End:   Position{Line: 2, Character: 13},
		}, hover.Range)
	})

	t.Run("WithXGoBuiltins", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var num int128
echo num
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		hover1, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 8},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover1)
		assert.Contains(t, hover1.Contents.Value, `def-id="xgo:builtin?int128"`)
		assert.Equal(t, Range{
			Start: Position{Line: 1, Character: 8},
			End:   Position{Line: 1, Character: 14},
		}, hover1.Range)

		hover2, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover2)
		assert.Contains(t, hover2.Contents.Value, `def-id="xgo:fmt?println"`)
		assert.Equal(t, Range{
			Start: Position{Line: 2, Character: 0},
			End:   Position{Line: 2, Character: 4},
		}, hover2.Range)
	})

	t.Run("WithNonENCharacters", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var 中文 []int
	中文 = append(中文, 1)
	println "非英文", 中文
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		hover1, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 14},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover1)
		assert.Contains(t, hover1.Contents.Value, `def-id="xgo:main?%E4%B8%AD%E6%96%87"`)
		assert.Equal(t, Range{
			Start: Position{Line: 3, Character: 13},
			End:   Position{Line: 3, Character: 15},
		}, hover1.Range)

		hover2, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 18},
			},
		})
		require.NoError(t, err)
		require.Nil(t, hover2)

		hover3, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 17},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover3)
		assert.Contains(t, hover3.Contents.Value, `def-id="xgo:main?%E4%B8%AD%E6%96%87"`)
		assert.Equal(t, Range{
			Start: Position{Line: 4, Character: 16},
			End:   Position{Line: 4, Character: 18},
		}, hover3.Range)
	})

	t.Run("VariadicFunctionCall", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	echo 1
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, `def-id="xgo:fmt?println"`)
		assert.Contains(t, hover.Contents.Value, `overview="func println(a ...any) (n int, err error)"`)
		assert.Equal(t, Range{
			Start: Position{Line: 2, Character: 1},
			End:   Position{Line: 2, Character: 5},
		}, hover.Range)
	})

	t.Run("XGotMethodCall", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	getWidget Monitor, "myWidget"
}
`),
			"assets/index.json": []byte(`{"zorder":[{"name":"myWidget"}]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, `def-id="xgo:github.com/goplus/spx/v2?Game.getWidget"`)
		assert.Contains(t, hover.Contents.Value, `overview="func getWidget(T Type, name WidgetName) *T"`)
		assert.Contains(t, hover.Contents.Value, `GetWidget returns the widget instance (in given type) with given name. It panics if not found.`)
		assert.Equal(t, Range{
			Start: Position{Line: 2, Character: 1},
			End:   Position{Line: 2, Character: 10},
		}, hover.Range)
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

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		require.Nil(t, hover)
		assert.Empty(t, hover)
	})
}
