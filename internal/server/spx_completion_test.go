//go:build !test_no_pkgdata

package server

import (
	"go/constant"
	"maps"
	"strings"
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

	t.Run("CostumeReceiver", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			source   string
			want     []string
			absent   []string
		}{
			{"Implicit", "Runner.spx", "onClick => {\n\tsetCostume \"c|\"\n}\n", []string{"runner"}, []string{"other"}},
			{"Explicit", "main.spx", "Runner.setCostume \"c|\"\n", []string{"runner"}, []string{"other"}},
			{"CrossSprite", "Other.spx", "onClick => {\n\tRunner.setCostume \"c|\"\n}\n", []string{"runner"}, []string{"other"}},
			{"ExplicitLineDirective", "main.spx", "//line virtual.spx:100:20\r\nRunner.setCostume \"\U0001f600r|suffix\"\r\n", []string{"runner"}, []string{"other"}},
			{"ImplicitLineDirective", "Runner.spx", "//line virtual.spx:100:20\nsetCostume \"r|\"\n", []string{"runner"}, []string{"other"}},
			{"FuncDecorator", "Runner.spx", "func withCostume(costume SpriteCostumeName, fn func()) {}\n@withCostume(\"r|\")\nfunc run() {}\n", []string{"runner"}, []string{"other"}},
			{"ShadowedAutoBinding", "main.spx", "onStart => {\n\tRunner := Other\n\tRunner.setCostume \"r|\"\n}\n", []string{"runner", "other"}, nil},
			{"RawEndWithCR", "main.spx", "Runner.setCostume `re\r\rsource|`\n", []string{"runner"}, []string{"other"}},
			{"RawUnterminatedCR", "main.spx", "Runner.setCostume `re\r\rsource|", []string{"runner"}, []string{"other"}},
			{"Multiline", "Other.spx", "onClick => {\r\n\tRunner.setCostume `before\r\nre|source\r\nafter`\r\n}\r\n", []string{"runner"}, []string{"other"}},
			{"Go", "Other.spx", "onClick => {\n\tgo Runner.setCostume(\"c|\")\n}\n", []string{"runner"}, []string{"other"}},
			{"Defer", "Other.spx", "onClick => {\n\tdefer Runner.setCostume(\"c|\")\n}\n", []string{"runner"}, []string{"other"}},
			{"ImplicitOutsideString", "Runner.spx", "onStart => {\n\tsetCostume C|\n}\n", []string{`"runner"`}, []string{`"other"`}},
			{"ProjectDeclaration", "main.spx", "onStart => {\n\tvar costume SpriteCostumeName = C|\n}\n", []string{`"runner"`, `"other"`}, nil},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"main.spx": nil, "Runner.spx": nil, "Other.spx": nil,
					"assets/index.json":                []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"runner"}]}`),
					"assets/sprites/Other/index.json":  []byte(`{"costumes":[{"name":"other"}]}`),
				}
				before := tt.source[:strings.IndexByte(tt.source, '|')]
				files[tt.filename] = []byte(strings.Replace(tt.source, "|", "", 1))
				s := newSpxTestServer(t, files)
				position := Position{
					Line:      uint32(strings.Count(before, "\n")),
					Character: uint32(UTF16Len(before[strings.LastIndex(before, "\n")+1:])),
				}
				items := completionItemsAt(t, s, tt.filename, position)
				for _, label := range tt.want {
					assert.Equal(t, 1, countCompletionItemLabel(items, label))
					if !strings.HasPrefix(label, `"`) {
						item := completionItemByLabel(items, label)
						require.NotNil(t, item)
						require.NotNil(t, item.TextEdit)
						edit := requireValueAs[TextEdit](t, item.TextEdit.Value)
						updated := applyCompletionTestEdits(t, string(files[tt.filename]), position, append([]TextEdit{edit}, item.AdditionalTextEdits...))
						updatedFiles := maps.Clone(files)
						updatedFiles[tt.filename] = []byte(updated)
						proj := newSpxTestServer(t, updatedFiles).getProj()
						info, err := proj.TypeInfo()
						require.NoError(t, err, updated)
						file, err := proj.ASTFile(tt.filename)
						require.NoError(t, err)
						literal := inputSlotLiteral(t, newInputSlotContext(proj, file), edit.NewText)
						value := info.Types[literal].Value
						require.NotNil(t, value)
						assert.Equal(t, label, constant.StringVal(value))
					}
				}
				for _, label := range tt.absent {
					assert.NotContains(t, completionItemLabels(items), label)
				}
			})
		}
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

	t.Run("PropertyNamesInCalls", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			source   string
			want     map[string]string
		}{
			{"Project", "main.spx", "onStart => {\n\tshowVar(x|)\n}\n", map[string]string{`"score"`: "xgo:main?Game.score", `"volume"`: ""}},
			{"Sprite", "Runner.spx", "onStart => {\n\tshowVar(x|)\n}\n", map[string]string{`"hp"`: "xgo:main?Runner.hp"}},
			{"ExplicitReceiver", "main.spx", "onStart => {\n\tRunner.showVar(x|)\n}\n", map[string]string{`"hp"`: "xgo:main?Runner.hp"}},
			{"EmbeddedMethod", "Runner.spx", "showVar(|\n", map[string]string{`"hp"`: "xgo:main?Runner.hp", `"xpos"`: "xgo:github.com/goplus/spx/v3?Sprite.xpos"}},
			{"InsideString", "main.spx", "showVar(\"s|\n", map[string]string{"score": "xgo:main?Game.score"}},
			{"LineDirective", "main.spx", "//line virtual.spx:100:20\nshowVar \"s|\"\n", map[string]string{"score": "xgo:main?Game.score"}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"main.spx":                         []byte("var score int\n"),
					"Runner.spx":                       []byte("var hp int\n"),
					"assets/index.json":                []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{}`),
				}
				source := string(files[tt.filename]) + tt.source
				before := source[:strings.IndexByte(source, '|')]
				files[tt.filename] = []byte(strings.Replace(source, "|", "", 1))
				s := newSpxTestServer(t, files)
				items := completionItemsAt(t, s, tt.filename, Position{
					Line:      uint32(strings.Count(before, "\n")),
					Character: uint32(UTF16Len(before[strings.LastIndex(before, "\n")+1:])),
				})
				for label, id := range tt.want {
					item := completionItemByLabel(items, label)
					require.NotNil(t, item, label)
					if id != "" {
						data := requireValueAs[*CompletionItemData](t, item.Data)
						assert.Equal(t, id, data.Definition.String())
					}
				}
				if tt.name == "InsideString" {
					assert.NotContains(t, completionItemLabels(items), `"score"`)
				}
			})
		}
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
