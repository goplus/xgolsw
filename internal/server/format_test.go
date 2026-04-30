package server

import (
	gotypes "go/types"
	"io/fs"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentFormatting(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
// An spx game.

var (
  MyAircraft MyAircraft
  Bullet Bullet
)
type Score int
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 8, Character: 0},
			},
			NewText: `// An spx game.

type Score int

var (
	MyAircraft MyAircraft
	Bullet     Bullet
)
`,
		})
	})

	t.Run("NonSpxFile", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`echo "Hello, XGo!"`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Nil(t, edits)
	})

	t.Run("FileNotFound", func(t *testing.T) {
		s := New(newProjectWithoutModTime(map[string][]byte{}), nil, fileMapGetter(map[string][]byte{}), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///notexist.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.ErrorIs(t, err, fs.ErrNotExist)
		require.Nil(t, edits)
	})

	t.Run("NoChangesNeeded", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(``),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Nil(t, edits)
	})

	t.Run("AcceptableFormatError", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`// An spx game.

var MyAircraft MyAircraft
!InvalidSyntax
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 4, Character: 0},
			},
			NewText: `// An spx game.

var (
	MyAircraft MyAircraft
)

!InvalidSyntax
`,
		})
	})

	t.Run("WithFormatSpx", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`// An spx game.

// The first var block.
var (
	// The aircraft.
	MyAircraft MyAircraft // The only aircraft.
) // Trailing comment for the first var block.

var Bullet Bullet // The first bullet.

// The second bullet.
var Bullet2 Bullet

var ( // Weirdly placed comment for the fourth var block.
	// The third bullet.
	Bullet3 Bullet
)

// The fifth var block.
var (
	Bullet4 Bullet // The fourth bullet.
) // Trailing comment for the fifth var block.

// The last var block.
var (
	// The fifth bullet.
	Bullet5 Bullet

	Bullet6 Bullet // The sixth bullet.
) // Trailing comment for the last var block.
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 30, Character: 0},
			},
			NewText: `// An spx game.

var (
	// The first var block.

	// The aircraft.
	MyAircraft MyAircraft // The only aircraft.

	// Trailing comment for the first var block.

	Bullet Bullet // The first bullet.

	// The second bullet.
	Bullet2 Bullet

	// Weirdly placed comment for the fourth var block.

	// The third bullet.
	Bullet3 Bullet

	// The fifth var block.

	Bullet4 Bullet // The fourth bullet.

	// Trailing comment for the fifth var block.

	// The last var block.

	// The fifth bullet.
	Bullet5 Bullet

	Bullet6 Bullet // The sixth bullet.

	// Trailing comment for the last var block.
)
`,
		})
	})

	t.Run("VarBlockWithoutDoc", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`// An spx game.

var (
	// The aircraft.
	MyAircraft MyAircraft // The only aircraft.
)

var Bullet Bullet
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 8, Character: 0},
			},
			NewText: `// An spx game.

var (
	// The aircraft.
	MyAircraft MyAircraft // The only aircraft.

	Bullet Bullet
)
`,
		})
	})

	t.Run("NoTypeSpriteVarDeclaration", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`// An spx game.

var (
	MySprite
)
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Nil(t, edits)
	})

	t.Run("WithImportStmt", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`// An spx game.
import "math"

onClick => {
	println math.floor(2.5)
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Nil(t, edits)
	})

	t.Run("WithUnusedLambdaParams", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`// An spx game.
onKey [KeyLeft, KeyRight], (key) => {
	println "key"
}

onKey [KeyLeft, KeyRight], (key) => {
	println key
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 8, Character: 0},
			},
			NewText: `// An spx game.
onKey [KeyLeft, KeyRight], () => {
	println "key"
}

onKey [KeyLeft, KeyRight], (key) => {
	println key
}
`,
		})
	})

	t.Run("WithUnusedLambdaParamsInKwarg", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`type Worker struct{}

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
		echo "hi"
	}
	worker.handle handler = (n) => {
		echo n
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 28, Character: 0},
			},
			NewText: `type Worker struct{}

type Options0 struct {
	Handler func()
}

type Options1 struct {
	Handler func(int)
}

func (w *Worker) handle0(opts Options0?) {}

func (w *Worker) handle1(opts Options1?) {}

func (Worker).handle = (
	(Worker).handle0
	(Worker).handle1
)

var (
	worker Worker
)

onStart => {
	worker.handle handler = () => {
		echo "hi"
	}
	worker.handle handler = (n) => {
		echo n
	}
}
`,
		})
	})

	t.Run("WithUnusedLambdaParamsForSprite", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": {},
			"MySprite.spx": []byte(`// An spx game.
onKey [KeyLeft, KeyRight], (key) => {
	println "key"
}
onTouchStart "MySprite", (s) => {
	println "touched", s
}
onTouchStart "MySprite", (s) => {}
onTouchStart (s, t) => { // type mismatch
}
onTouchStart 123, (s) => { // type mismatch
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 12, Character: 0},
			},
			NewText: `// An spx game.
onKey [KeyLeft, KeyRight], () => {
	println "key"
}
onTouchStart "MySprite", (s) => {
	println "touched", s
}
onTouchStart "MySprite", () => {
}
onTouchStart (s, t) => { // type mismatch
}
onTouchStart 123, (s) => { // type mismatch
}
`,
		})
	})

	t.Run("EmptyFile", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(``),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Nil(t, edits)
	})

	t.Run("WhitespaceOnlyFile", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(` `),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 0, Character: 1},
			},
			NewText: ``,
		})
	})

	t.Run("WithFloatingComments", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`import "fmt"

// floating comment1

// comment for var a
var a int

// floating comment2

// comment for func test
func test() {
	// comment inside func test
}

// floating comment3

// comment for const b
const b = "123"

// floating comment4


// floating comment5
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 23, Character: 0},
			},
			NewText: `import "fmt"

// floating comment1

// floating comment2

// floating comment3

// comment for const b
const b = "123"

var (
	// comment for var a
	a int
)

// comment for func test
func test() {
	// comment inside func test
}

// floating comment4

// floating comment5
`,
		})
	})

	t.Run("WithTrailingComments", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`import "fmt" // trailing comment for import "fmt"

const foo = "bar" // trailing comment for const foo

var a int // trailing comment for var a

func test() {} // trailing comment for func test
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 7, Character: 0},
			},
			NewText: `import "fmt" // trailing comment for import "fmt"

const foo = "bar" // trailing comment for const foo

var (
	a int // trailing comment for var a
)

func test() {} // trailing comment for func test
`,
		})
	})

	t.Run("WithMethods", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`// An spx game.

type Foo struct{}

var (
	flag bool
)

func (Foo) Bar() {}

func Bar() {}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 11, Character: 0},
			},
			NewText: `// An spx game.

type Foo struct{}

func (Foo) Bar() {}

var (
	flag bool
)

func Bar() {}
`,
		})
	})

	t.Run("VarBlocksWithAndWithoutInit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`// An spx game.

var (
	dir int
	snakeBodyParts []Sprite
)

var (
	moveStep int = 20
)

`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 11, Character: 0},
			},
			NewText: `// An spx game.

var (
	dir            int
	snakeBodyParts []Sprite
)

var (
	moveStep int = 20
)
`,
		})
	})

	t.Run("MultipleVarBlocksWithMultipleTypes", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`// An spx game.

var (
    score int
    highScore int
)

var (
    playerName string = "Player1"
    gameStarted bool = false
)

var (
    lives int
)

var (
    gameSpeed int = 5
)

`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 20, Character: 0},
			},
			NewText: `// An spx game.

var (
	score     int
	highScore int

	lives int
)

var (
	playerName  string = "Player1"
	gameStarted bool   = false

	gameSpeed int = 5
)
`,
		})
	})

	t.Run("MixedVarDeclarationsWithComments", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`// An spx game.

// Variables without initialization
var (
    // Score for the player
    playerScore int
    // Lives remaining
    livesRemaining int
)

// Variables with initialization
var (
    // Game speed setting
    speed int = 10
    // Player name
    name string = "DefaultPlayer"
)

`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 18, Character: 0},
			},
			NewText: `// An spx game.

// Variables without initialization
var (
	// Score for the player
	playerScore int
	// Lives remaining
	livesRemaining int
)

// Variables with initialization
var (
	// Game speed setting
	speed int = 10
	// Player name
	name string = "DefaultPlayer"
)
`,
		})
	})

	t.Run("SingleVarDeclarationsWithMixedInit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`// An spx game.

var x int
var y int = 10
var z string
var name string = "Player"

`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, edits, 1)
		assert.Contains(t, edits, TextEdit{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 7, Character: 0},
			},
			NewText: `// An spx game.

var (
	x int

	z string
)

var (
	y int = 10

	name string = "Player"
)
`,
		})
	})

	t.Run("WithShadowEntryComments", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`// An spx game.
var (
	count int
)

// onStart comment
onStart => {
	count++
	echo count
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		params := &DocumentFormattingParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		edits, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Empty(t, edits)
	})
}

func TestOverloadResolvedCallExprArgType(t *testing.T) {
	pkg := gotypes.NewPackage("main", "main")
	handlerType := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
	handlerField := gotypes.NewField(token.NoPos, pkg, "Handler", handlerType, false)
	optionsType := gotypes.NewNamed(
		gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
		gotypes.NewStruct([]*gotypes.Var{handlerField}, nil),
		nil,
	)
	overload := gotypes.NewFunc(token.NoPos, pkg, "handle", gotypes.NewSignatureType(
		nil,
		nil,
		nil,
		gotypes.NewTuple(
			gotypes.NewParam(token.NoPos, pkg, "name", gotypes.Typ[gotypes.String]),
			gotypes.NewParam(token.NoPos, pkg, "opts", optionsType),
		),
		nil,
		false,
	))
	kwarg := &ast.KwargExpr{
		Name:  &ast.Ident{Name: "handler"},
		Value: &ast.Ident{Name: "callback"},
	}

	t.Run("Keyword", func(t *testing.T) {
		callExpr := &ast.CallExpr{
			Args:   []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"first"`}},
			Kwargs: []*ast.KwargExpr{kwarg},
		}

		got := overloadResolvedCallExprArgType(nil, callExpr, overload, xgoutil.ResolvedCallExprArg{
			Kind:       xgoutil.ResolvedCallExprArgKeyword,
			Kwarg:      kwarg,
			ParamIndex: 0,
		})
		assert.True(t, gotypes.Identical(handlerType, got))
	})

	t.Run("PositionalAfterVariadicKwargParam", func(t *testing.T) {
		variadicOverload := gotypes.NewFunc(token.NoPos, pkg, "handle", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(
				gotypes.NewParam(token.NoPos, pkg, "opts", optionsType),
				gotypes.NewParam(token.NoPos, pkg, "values", gotypes.NewSlice(gotypes.Typ[gotypes.Int])),
			),
			nil,
			true,
		))
		callExpr := &ast.CallExpr{
			Args:   []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "1"}},
			Kwargs: []*ast.KwargExpr{kwarg},
		}

		got := overloadResolvedCallExprArgType(nil, callExpr, variadicOverload, xgoutil.ResolvedCallExprArg{
			Kind:       xgoutil.ResolvedCallExprArgPositional,
			ArgIndex:   0,
			ParamIndex: 0,
		})
		assert.True(t, gotypes.Identical(gotypes.Typ[gotypes.Int], got))
	})
}

func TestOverloadMatchesCallExpr(t *testing.T) {
	t.Run("LambdaArity", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		handlerType := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		overload := gotypes.NewFunc(token.NoPos, pkg, "handle", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(
				gotypes.NewParam(token.NoPos, pkg, "handler", handlerType),
				gotypes.NewParam(token.NoPos, pkg, "values", gotypes.NewSlice(gotypes.Typ[gotypes.Int])),
			),
			nil,
			true,
		))
		callExpr := &ast.CallExpr{
			Args: []ast.Expr{&ast.LambdaExpr2{}},
		}

		assert.True(t, overloadMatchesCallExpr(nil, callExpr, overload, -1))
	})

	t.Run("SkippedUnknownKwarg", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		countField := gotypes.NewField(token.NoPos, pkg, "Count", gotypes.Typ[gotypes.Int], false)
		optionsType := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
			gotypes.NewStruct([]*gotypes.Var{countField}, nil),
			nil,
		)
		overload := gotypes.NewFunc(token.NoPos, pkg, "handle", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "opts", optionsType)),
			nil,
			false,
		))
		callExpr := &ast.CallExpr{
			Kwargs: []*ast.KwargExpr{{
				Name:  &ast.Ident{Name: "unknown"},
				Value: &ast.Ident{Name: "value"},
			}},
		}

		assert.True(t, overloadMatchesCallExpr(nil, callExpr, overload, 0))
		assert.False(t, overloadMatchesCallExpr(nil, callExpr, overload, -1))
	})
}

func TestOverloadCallExprArgType(t *testing.T) {
	pkg := gotypes.NewPackage("main", "main")
	handlerType := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
	sig := gotypes.NewSignatureType(
		nil,
		nil,
		nil,
		gotypes.NewTuple(
			gotypes.NewParam(token.NoPos, pkg, "name", gotypes.Typ[gotypes.String]),
			gotypes.NewParam(token.NoPos, pkg, "handlers", gotypes.NewSlice(handlerType)),
		),
		nil,
		true,
	)

	assert.Equal(t, gotypes.Typ[gotypes.String], overloadCallExprArgType(sig, 0))
	assert.True(t, gotypes.Identical(handlerType, overloadCallExprArgType(sig, 1)))
	assert.True(t, gotypes.Identical(handlerType, overloadCallExprArgType(sig, 2)))
	assert.Nil(t, overloadCallExprArgType(sig, -1))
}
