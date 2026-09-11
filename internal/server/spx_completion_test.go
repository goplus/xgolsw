//go:build !test_no_pkgdata

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletionSpx(t *testing.T) {
	t.Run("ImportFrameworkPackage", func(t *testing.T) {
		files := map[string][]byte{"main.spx": []byte("import \"github.com/goplus/spx\n")}
		s := newSpxTestServer(t, files)
		items := completionItemsAt(t, s, "main.spx", Position{Character: 28})
		item := completionItemByLabel(items, SpxPkgPath)
		require.NotNil(t, item)
		assert.Equal(t, ModuleCompletion, item.Kind)
		assert.Equal(t, SpxPkgPath, item.InsertText)
		data := requireValueAs[*CompletionItemData](t, item.Data)
		assert.Equal(t, ToPtr(SpxPkgPath), data.Definition.Package)
	})

	t.Run("ResourceNames", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			source   string
			position Position
			want     []string
			absent   []string
		}{
			{name: "Backdrop", filename: "main.spx", source: "var backdrop BackdropName = B\n", position: Position{Character: 28},
				want: []string{`"Studio"`}},
			{name: "Widget", filename: "main.spx", source: "var widget WidgetName = W\n", position: Position{Character: 24},
				want: []string{`"Score"`}},
			{name: "AnimationFromProject", filename: "main.spx", source: "var animation SpriteAnimationName = A\n", position: Position{Character: 37},
				want: []string{`"walk"`, `"jump"`}},
			{name: "AnimationFromSprite", filename: "Runner.spx", source: "var animation SpriteAnimationName = A\n", position: Position{Character: 37},
				want: []string{`"walk"`}, absent: []string{`"jump"`}},
			{name: "AnimationFromReceiver", filename: "Other.spx", source: "Runner.animate \"w\"\n", position: Position{Character: 17},
				want: []string{"walk"}, absent: []string{"jump"}},
			{name: "CostumeFromSprite", filename: "Runner.spx", source: "var costume SpriteCostumeName = C\n", position: Position{Character: 32},
				want: []string{`"runner"`}, absent: []string{`"other"`}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"main.spx":                         nil,
					"Runner.spx":                       nil,
					"Other.spx":                        nil,
					"assets/index.json":                []byte(`{"backdrops":[{"name":"Studio"}],"zorder":[{"name":"Score","type":"monitor"}]}`),
					"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"runner"}],"fAnimations":{"walk":{}}}`),
					"assets/sprites/Other/index.json":  []byte(`{"costumes":[{"name":"other"}],"fAnimations":{"jump":{},"walk":{}}}`),
				}
				files[tt.filename] = []byte("func test() {\n\t" + tt.source + "}\n")
				s := newSpxTestServer(t, files)
				items := completionItemsAt(t, s, tt.filename, Position{Line: 1, Character: tt.position.Character + 1})
				for _, label := range tt.want {
					item := completionItemByLabel(items, label)
					require.NotNil(t, item, label)
					assert.Equal(t, 1, countCompletionItemLabel(items, label))
					assert.Equal(t, TextCompletion, item.Kind)
					assert.Equal(t, label, item.InsertText)
					assert.Equal(t, ToPtr(PlainTextTextFormat), item.InsertTextFormat)
				}
				for _, label := range tt.absent {
					assert.NotContains(t, completionItemLabels(items), label)
				}
			})
		}
	})

	t.Run("PropertyNamesOutsideCalls", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			want     []string
			absent   []string
		}{
			{name: "Project", filename: "main.spx", want: []string{`"score"`}, absent: []string{`"hp"`, `"mana"`}},
			{name: "Sprite", filename: "Runner.spx", want: []string{`"hp"`, `"score"`}, absent: []string{`"mana"`}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"main.spx":   []byte("var score int\n"),
					"Runner.spx": []byte("var hp int\n"),
					"Other.spx":  []byte("var mana int\n"),
				}
				files[tt.filename] = append(files[tt.filename], []byte("func test() {\n\tvar property PropertyName = P\n}\n")...)
				s := newSpxTestServer(t, files)
				items := completionItemsAt(t, s, tt.filename, Position{Line: 2, Character: 30})
				for _, label := range tt.want {
					item := completionItemByLabel(items, label)
					require.NotNil(t, item)
					assert.Equal(t, PropertyCompletion, item.Kind)
					assert.Equal(t, label, item.InsertText)
				}
				for _, label := range tt.absent {
					assert.NotContains(t, completionItemLabels(items), label)
				}
			})
		}
	})

	t.Run("PropertyNamesForImportedReceiver", func(t *testing.T) {
		files := map[string][]byte{"main.spx": []byte(`import "github.com/goplus/spx/v3"
var score int
func test() {
	var sprite spx.SpriteImpl
	sprite.showVar P
}
`)}
		s := newSpxTestServer(t, files)
		items := completionItemsAt(t, s, "main.spx", Position{Line: 4, Character: 17})
		assert.NotContains(t, completionItemLabels(items), `"score"`)
		assert.NotContains(t, completionItemLabels(items), `"xpos"`)
	})

	t.Run("SpriteAutoBinding", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx":                         []byte("var Runner Sprite\nfunc test() {\n\tvar target Sprite = R\n}\n"),
			"assets/sprites/Runner/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, files)
		items := completionItemsAt(t, s, "main.spx", Position{Line: 2, Character: 22})
		item := completionItemByLabel(items, "Runner")
		require.NotNil(t, item)
		assert.Equal(t, "Runner", item.InsertText)
		data := requireValueAs[*CompletionItemData](t, item.Data)
		assert.Equal(t, "xgo:main?Game.Runner", data.Definition.String())
	})

	t.Run("StepToWithKwargNameCompletion", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			code      string
			character uint32
		}{
			{
				name: "BareIdent",
				code: `
onStart => {
	stepToWith "Red", s
}
`,
				character: 20,
			},
			{
				name: "KwargExpr",
				code: `
onStart => {
	stepToWith "Red", spe = 2
}
`,
				character: 21,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				m := map[string][]byte{
					"main.spx":                           []byte(``),
					"MySprite.spx":                       []byte(tt.code),
					"Red.spx":                            []byte(``),
					"assets/index.json":                  []byte(`{}`),
					"assets/sprites/MySprite/index.json": []byte(`{}`),
					"assets/sprites/Red/index.json":      []byte(`{}`),
				}
				s := newSpxTestServer(t, m)

				items := completionItemsAt(t, s, "MySprite.spx", Position{Line: 2, Character: tt.character})
				assert.True(t, containsKwargCompletionItem(items, "speed", SpxDefinitionIdentifier{
					Package: ToPtr(SpxPkgPath),
					Name:    ToPtr("MotionOptions.Speed"),
				}))
				assert.True(t, containsKwargCompletionItem(items, "animation", SpxDefinitionIdentifier{
					Package: ToPtr(SpxPkgPath),
					Name:    ToPtr("MotionOptions.Animation"),
				}))
			})
		}
	})

	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`

MySprite.
`),
			"MySprite.spx": []byte(`
onStart => {
	MySprite.turn Right
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		emptyLineItems := completionItemsAt(t, s, "main.spx", Position{Line: 1, Character: 0})
		assert.NotEmpty(t, emptyLineItems)
		assert.Contains(t, completionItemLabels(emptyLineItems), "println")
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

		mySpriteDotItems := completionItemsAt(t, s, "main.spx", Position{Line: 2, Character: 9})
		assert.NotEmpty(t, mySpriteDotItems)
		assert.NotContains(t, completionItemLabels(mySpriteDotItems), "println")
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
`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "main.spx", Position{Line: 2, Character: 1})
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

	t.Run("VarDeclAndAssign", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var x SpriteName = "m"
}
`),
			"MySprite.spx": []byte(`
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "main.spx", Position{Line: 2, Character: 22})
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "MySprite")
	})

	t.Run("VarDeclAndAssignWithAlias", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MySpriteName = SpriteName

onStart => {
	var x MySpriteName = "m"
}
`),
			"MySprite.spx":                       []byte(``),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "main.spx", Position{Line: 4, Character: 24})
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "MySprite")
	})

	t.Run("SpxSoundResourceStringLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
play "r"
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sounds/recording/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "main.spx", Position{Line: 1, Character: 7})
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "recording")
	})

	t.Run("FuncOverloads", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
play r
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sounds/recording/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "main.spx", Position{Line: 1, Character: 6})
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), `"recording"`)
	})

	t.Run("WithImplicitSpxSpriteResource", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
`),
			"MySprite.spx": []byte(`
onClick => {
	setCostume "c"
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{"costumes":[{"name":"costume"}]}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "MySprite.spx", Position{Line: 2, Character: 14})
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "costume")
	})

	t.Run("WithExplicitSpxSpriteResource", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
MySprite.setCostume "c"
`),
			"MySprite.spx":                       []byte(``),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{"costumes":[{"name":"costume"}]}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "main.spx", Position{Line: 1, Character: 22})
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "costume")
	})

	t.Run("WithCrossSpxSpriteResource", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
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
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "Sprite1.spx", Position{Line: 2, Character: 22})
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "Sprite2Costume")
	})

	t.Run("WithCrossSpxSpriteResourceInGoStmt", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(``),
			"Sprite1.spx": []byte(`
onClick => {
	go Sprite2.setCostume("c")
}
`),
			"Sprite2.spx":                       []byte(``),
			"assets/index.json":                 []byte(`{}`),
			"assets/sprites/Sprite1/index.json": []byte(`{"costumes":[{"name":"Sprite1Costume"}]}`),
			"assets/sprites/Sprite2/index.json": []byte(`{"costumes":[{"name":"Sprite2Costume"}]}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "Sprite1.spx", Position{Line: 2, Character: 25})
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "Sprite2Costume")
	})

	t.Run("WithCrossSpxSpriteResourceInDeferStmt", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(``),
			"Sprite1.spx": []byte(`
onClick => {
	defer Sprite2.setCostume("c")
}
`),
			"Sprite2.spx":                       []byte(``),
			"assets/index.json":                 []byte(`{}`),
			"assets/sprites/Sprite1/index.json": []byte(`{"costumes":[{"name":"Sprite1Costume"}]}`),
			"assets/sprites/Sprite2/index.json": []byte(`{"costumes":[{"name":"Sprite2Costume"}]}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "Sprite1.spx", Position{Line: 2, Character: 28})
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "Sprite2Costume")
	})

	t.Run("SpriteCostumeNameInImplicitCallUsesCurrentSprite", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(``),
			"Sprite1.spx": []byte(`
onStart => {
	setCostume C
}
`),
			"Sprite2.spx":                       []byte(``),
			"Sprite3.spx":                       []byte(``),
			"assets/index.json":                 []byte(`{}`),
			"assets/sprites/Sprite1/index.json": []byte(`{"costumes":[{"name":"Crab2"},{"name":"Crab3"}]}`),
			"assets/sprites/Sprite2/index.json": []byte(`{"costumes":[{"name":"Crab2"}]}`),
			"assets/sprites/Sprite3/index.json": []byte(`{"costumes":[{"name":"Crab2"}]}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "Sprite1.spx", Position{Line: 2, Character: 13})
		assert.Equal(t, 1, countCompletionItemLabel(items, `"Crab2"`))
		assert.Equal(t, 1, countCompletionItemLabel(items, `"Crab3"`))
	})

	t.Run("SpriteCostumeNameInDeclDeduplicatesCrossSpriteNames", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var costume SpriteCostumeName = C
}
`),
			"Sprite1.spx":                       []byte(``),
			"Sprite2.spx":                       []byte(``),
			"Sprite3.spx":                       []byte(``),
			"assets/index.json":                 []byte(`{}`),
			"assets/sprites/Sprite1/index.json": []byte(`{"costumes":[{"name":"Crab2"},{"name":"Crab3"}]}`),
			"assets/sprites/Sprite2/index.json": []byte(`{"costumes":[{"name":"Crab2"}]}`),
			"assets/sprites/Sprite3/index.json": []byte(`{"costumes":[{"name":"Crab2"}]}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "main.spx", Position{Line: 2, Character: 34})
		assert.Equal(t, 1, countCompletionItemLabel(items, `"Crab2"`))
		assert.Equal(t, 1, countCompletionItemLabel(items, `"Crab3"`))
	})

	t.Run("StepToOverloadsDeduplicateSpriteNameSuggestions", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(``),
			"Runner.spx": []byte(`
onStart => {
	stepTo C
}
`),
			"Crab2.spx":                        []byte(``),
			"Crab3.spx":                        []byte(``),
			"assets/index.json":                []byte(`{}`),
			"assets/sprites/Runner/index.json": []byte(`{"costumes":[]}`),
			"assets/sprites/Crab2/index.json":  []byte(`{"costumes":[]}`),
			"assets/sprites/Crab3/index.json":  []byte(`{"costumes":[]}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "Runner.spx", Position{Line: 2, Character: 10})
		assert.Equal(t, 1, countCompletionItemLabel(items, `"Crab2"`))
		assert.Equal(t, 1, countCompletionItemLabel(items, `"Crab3"`))
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
		s := newSpxTestServer(t, m)

		items1 := completionItemsAt(t, s, "main.spx", Position{Line: 1, Character: 14})
		assert.NotEmpty(t, items1)
		assert.Contains(t, completionItemLabels(items1), "setCostume")

		items2 := completionItemsAt(t, s, "MySprite.spx", Position{Line: 2, Character: 15})
		assert.NotEmpty(t, items2)
		assert.Contains(t, completionItemLabels(items2), "setCostume")
	})

	t.Run("MathPackage", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	n := ab
}
`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "main.spx", Position{Line: 2, Character: 8})
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "abs")
	})

	t.Run("SpriteInterfaceEmbedding", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MyInterface interface {
	Sprite
	methodOne()
}

onStart => {
	var iface MyInterface = MySprite
	iface.on
}
`),
			"MySprite.spx": []byte(`
func methodOne() {}
onStart => {}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "main.spx", Position{Line: 8, Character: 9}) // After "n"
		assert.Contains(t, completionItemLabels(items), "onClick")
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("github.com/goplus/spx/v3"),
			Name:    ToPtr("Sprite.onClick"),
		}))
		assert.Contains(t, completionItemLabels(items), "methodOne")
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MyInterface.methodOne"),
		}))
	})

	t.Run("PropertyNameCompletionInMainSpx", func(t *testing.T) {
		// showVar in main.spx makes getPropertyTarget return "Game"
		// → collectPropertyNames("Game") → property methods from embedded spx.Game appear
		m := map[string][]byte{
			"main.spx": []byte(`
var score int
onStart => {
	showVar(x)
}
`),
			"MySprite.spx":                       []byte(`onStart => {}`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "main.spx", Position{Line: 3, Character: 10}) // inside 'x' arg of showVar
		// score is declared in main.spx and becomes a Game field.
		assert.Contains(t, completionItemLabels(items), `"score"`)
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("Game.score"),
		}))
		// Property method from embedded spx.Game.
		assert.Contains(t, completionItemLabels(items), `"volume"`)
	})

	t.Run("PropertyNameCompletionInSpriteSpx", func(t *testing.T) {
		// showVar in MySprite.spx → getPropertyTarget returns "MySprite" (not "Game").
		// hp is a field of MySprite, so its appearance confirms the correct target is used.
		m := map[string][]byte{
			"main.spx": []byte(`
`),
			"MySprite.spx": []byte(`
var hp int

onStart => {
	showVar(x)
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				// Line 4: "\tshowVar(x)" — tab(0)+showVar(1-7)+(8)+x(9)
				Position: Position{Line: 4, Character: 10},
			},
		})
		require.NoError(t, err)
		items := requireValueAs[[]CompletionItem](t, itemsResult)
		require.NotNil(t, items)
		// hp is a direct field of MySprite — confirms target is "MySprite", not "Game".
		assert.Contains(t, completionItemLabels(items), `"hp"`)
	})

	t.Run("PropertyNameCompletionExplicitReceiver", func(t *testing.T) {
		// MySprite.showVar(x) in main.spx makes getPropertyTarget return "MySprite"
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	MySprite.showVar(x)
}
`),
			"MySprite.spx": []byte(`
var hp int
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				// Line 3: "\tMySprite.showVar(x)" — tab(0)+MySprite(1-8)+.(9)+showVar(10-16)+(17)+x(18)
				Position: Position{Line: 2, Character: 19},
			},
		})
		require.NoError(t, err)
		items := requireValueAs[[]CompletionItem](t, itemsResult)
		require.NotNil(t, items)
		assert.Contains(t, completionItemLabels(items), `"hp"`)
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MySprite.hp"),
		}))
	})

	t.Run("PropertyNameCompletionEmbeddedMethod", func(t *testing.T) {
		// SpriteImpl is embedded in MySprite; its property methods should appear.
		m := map[string][]byte{
			"main.spx": []byte(`
`),
			"MySprite.spx": []byte(`
var hp int

showVar(
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "MySprite.spx", Position{Line: 3, Character: 8})
		// Direct field of MySprite.
		assert.Contains(t, completionItemLabels(items), `"hp"`)
		// Property method from embedded spx.SpriteImpl (e.g. "xpos" → "Xpos").
		assert.Contains(t, completionItemLabels(items), `"xpos"`)
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("github.com/goplus/spx/v3"),
			Name:    ToPtr("Sprite.xpos"),
		}))
	})

	t.Run("PropertyNameCompletionInsideStringLit", func(t *testing.T) {
		// When cursor is inside a string literal, insert text should NOT be quoted.
		m := map[string][]byte{
			"main.spx": []byte(`
var score int

showVar("s
`),
			"assets/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		items := completionItemsAt(t, s, "main.spx", Position{Line: 3, Character: 10})
		// Inside string literal: label/insertText is unquoted.
		assert.Contains(t, completionItemLabels(items), "score")
		assert.NotContains(t, completionItemLabels(items), `"score"`)
	})

	t.Run("SpxSeconds", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`onStart => {
	wait 1
}
`),
			"assets/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 7},
			},
		})
		require.NoError(t, err)
		items := requireValueAs[CompletionList](t, itemsResult).Items
		assert.Truef(t, containsCompletionItemLabel(items, "s"), "%v", completionItemLabels(items))
		assert.Truef(t, containsCompletionItemLabel(items, "ms"), "%v", completionItemLabels(items))
		assert.Equal(t, "1s", completionItemByLabel(items, "s").FilterText)
		assert.Equal(t, "1ms", completionItemByLabel(items, "ms").FilterText)
		assert.NotContains(t, completionItemLabels(items), "m")
	})
}
