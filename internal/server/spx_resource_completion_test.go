package server

import (
	"go/constant"
	gotypes "go/types"
	"maps"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpxAnalysisCollectSpxResourceNames(t *testing.T) {
	for _, tt := range []struct {
		name   string
		kind   spxResourceCompletionKind
		sprite string
		want   map[string]string
	}{
		{"Backdrops", spxResourceCompletionBackdrop, "", map[string]string{"Studio": "backdrops/Studio"}},
		{"Sounds", spxResourceCompletionSound, "", map[string]string{"Beep": "sounds/Beep"}},
		{"Sprites", spxResourceCompletionSprite, "", map[string]string{"Runner": "sprites/Runner", "Other": "sprites/Other"}},
		{"Widgets", spxResourceCompletionWidget, "", map[string]string{"Score": "widgets/Score"}},
		{"CostumesFromProject", spxResourceCompletionCostume, "", map[string]string{
			"idle": "sprites/Other/costumes/idle", "runner": "sprites/Runner/costumes/runner", "other": "sprites/Other/costumes/other",
		}},
		{"CostumesFromSprite", spxResourceCompletionCostume, "Runner", map[string]string{
			"idle": "sprites/Runner/costumes/idle", "runner": "sprites/Runner/costumes/runner",
		}},
		{"AnimationsFromProject", spxResourceCompletionAnimation, "", map[string]string{
			"walk": "sprites/Other/animations/walk", "jump": "sprites/Other/animations/jump",
		}},
		{"AnimationsFromSprite", spxResourceCompletionAnimation, "Runner", map[string]string{"walk": "sprites/Runner/animations/walk"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, kind := range []MarkupKind{Markdown, PlainText} {
				t.Run(string(kind), func(t *testing.T) {
					s := newTestServer(t, map[string][]byte{
						"assets/index.json":                []byte(`{"backdrops":[{"name":"Studio"}],"zorder":[{"name":"Score"}]}`),
						"assets/sounds/Beep/index.json":    []byte(`{}`),
						"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"idle"},{"name":"runner"},{"name":"frame"}],"fAnimations":{"walk":{"frameFrom":"frame","frameTo":"frame"}}}`),
						"assets/sprites/Other/index.json":  []byte(`{"costumes":[{"name":"idle"},{"name":"idle"},{"name":"other"}],"fAnimations":{"walk":{},"jump":{}}}`),
					})
					set, err := NewSpxResourceSet(s.getProj())
					require.NoError(t, err)
					result := newSpxAnalysis(s.getProj())
					result.spxResourceSet = *set
					ctx := &completionContext{itemSet: newCompletionItemSet(kind)}
					sprite := set.Sprite(tt.sprite)
					if tt.sprite != "" {
						require.NotNil(t, sprite)
					}
					result.collectSpxResourceNames(ctx, tt.kind, sprite)
					require.Len(t, ctx.itemSet.items, len(tt.want))
					for name, path := range tt.want {
						label := `"` + name + `"`
						item := completionItemByLabel(ctx.itemSet.items, label)
						require.NotNil(t, item, label)
						value := "spx://resources/" + path
						if kind == Markdown {
							value = `<resource-preview resource="` + value + `" />` + "\n"
						}
						assert.Equal(t, CompletionItem{
							Label: label, Kind: TextCompletion, InsertText: label,
							InsertTextFormat: ToPtr(PlainTextTextFormat),
							Documentation:    completionDocumentation(MarkupContent{Kind: kind, Value: value}),
						}, *item)
					}
				})
			}
		})
	}

	t.Run("EmptyResources", func(t *testing.T) {
		s := newTestServer(t, nil)
		for _, kind := range []spxResourceCompletionKind{
			spxResourceCompletionBackdrop, spxResourceCompletionSound, spxResourceCompletionSprite,
			spxResourceCompletionCostume, spxResourceCompletionAnimation, spxResourceCompletionWidget,
		} {
			result := newSpxAnalysis(s.getProj())
			ctx := &completionContext{itemSet: newCompletionItemSet(Markdown)}
			result.collectSpxResourceNames(ctx, kind, nil)
			assert.Empty(t, ctx.itemSet.items)
		}
	})

}

func TestServerTextDocumentCompletionSpxResources(t *testing.T) {
	t.Run("ResourceConversions", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
			want   string
			absent string
		}{
			{"Intrinsic", "echo sdk.BackdropName(\"|\")\n", "Studio", "Beep"},
			{"Nested", "echo sdk.BackdropName(string(\"|\"))\n", "Studio", "Beep"},
			{"OuterString", "echo string(sdk.BackdropName(\"|\"))\n", "Studio", "Beep"},
			{"Contextual", "play sdk.BackdropName(\"|\")\n", "Beep", "Studio"},
			{"NestedContextual", "play string(sdk.BackdropName(\"|\"))\n", "Beep", "Studio"},
			{"ReceiverContext", "Runner.setCostume sdk.SpriteCostumeName(\"|\")\n", "runner", "other"},
			{"NestedReceiverContext", "Runner.setCostume string(sdk.SpriteCostumeName(\"|\"))\n", "runner", "other"},
			{"PartialName", "play sdk.BackdropName(\"B|\")\n", "Beep", "Studio"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, position := typeDisplayTestSource(t, "import sdk \"github.com/goplus/spx/v3\"\n"+tt.source)
				s := newSpxTestServer(t, map[string][]byte{
					"main.spx":                      []byte(source),
					"assets/index.json":             []byte(`{"backdrops":[{"name":"Studio"}]}`),
					"assets/sounds/Beep/index.json": []byte(`{}`),
					"Runner.spx":                    nil, "Other.spx": nil,
					"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"runner"}]}`),
					"assets/sprites/Other/index.json":  []byte(`{"costumes":[{"name":"other"}]}`),
				})
				items := completionItemsAt(t, s, "main.spx", position)
				item := completionItemByLabel(items, tt.want)
				require.NotNil(t, item)
				require.NotNil(t, item.TextEdit)
				edit := requireValueAs[TextEdit](t, item.TextEdit.Value)
				updated := applyResourceRenameTestEdits(t, source, []TextEdit{edit})
				assert.Contains(t, updated, `"`+tt.want+`"`)
				_, err := newSpxTestServer(t, map[string][]byte{"main.spx": []byte(updated), "Runner.spx": nil}).getProj().TypeInfo()
				require.NoError(t, err)
				assert.NotContains(t, completionItemLabels(items), tt.absent)
			})
		}
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
		for _, tt := range []struct{ name, typ string }{
			{"Direct", "Sprite"},
			{"Alias", "Target"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, position := typeDisplayTestSource(t, "type Target = Sprite\nvar Runner "+tt.typ+"\nfunc test() {\n    var target Sprite = R|\n}\n")
				files := map[string][]byte{
					"main.spx":                         []byte(source),
					"assets/index.json":                []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{}`),
				}
				s := newSpxTestServer(t, files)
				items := completionItemsAt(t, s, "main.spx", position)
				item := completionItemByLabel(items, "Runner")
				require.NotNil(t, item)
				assert.Equal(t, "Runner", item.InsertText)
				data := requireValueAs[*CompletionItemData](t, item.Data)
				assert.Equal(t, "xgo:main?Game.Runner", data.Definition.String())
				result, err := analyzeSpx(s.getProj())
				require.NoError(t, err)
				info, _ := s.getProj().TypeInfo()
				require.NotNil(t, info)
				game := info.Pkg.Scope().Lookup("Game")
				require.NotNil(t, game)
				field, _, _ := gotypes.LookupFieldOrMethod(game.Type(), true, info.Pkg, "Runner")
				require.NotNil(t, field)
				assert.True(t, result.hasSpxSpriteResourceAutoBinding(field))
			})
		}
	})

	t.Run("InSpxEventHandler", func(t *testing.T) {
		for _, tt := range []struct {
			name, source string
			wantSDK      bool
		}{
			{"Outside", "|onCustom => {}\n", true},
			{"InsideSDKHandler", "onStart => {\n    |onCustom => {}\n}\n", false},
			{"InsideUserHandler", "onCustom => {\n    |onStart => {}\n}\n", true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, position := typeDisplayTestSource(t, "func onCustom(fn func()) {}\n"+tt.source)
				s := newSpxTestServer(t, map[string][]byte{"main.spx": []byte(source)})
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				labels := completionItemLabels(completionItemsAt(t, s, "main.spx", position))
				assert.Contains(t, labels, "onCustom")
				if tt.wantSDK {
					assert.Contains(t, labels, "onStart")
					assert.Contains(t, labels, "onKey")
				} else {
					assert.NotContains(t, labels, "onStart")
					assert.NotContains(t, labels, "onKey")
				}
			})
		}
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
			{"ExplicitThis", "Runner.spx", "this.setCostume \"c|\"\n", []string{"runner"}, []string{"other"}},
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
}

func TestServerTextDocumentCompletionSpxResourceOverloads(t *testing.T) {
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

}
