//go:build !test_no_pkgdata

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletionSpx(t *testing.T) {
	t.Run("SpriteInterface", func(t *testing.T) {
		for _, tt := range []struct{ name, filename, typ string }{
			{"Imported", "main.xgo", "spx.Sprite"},
			{"Alias", "main.xgo", "Target"},
			{"Classfile", "main.spx", "spx.Sprite"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, position := typeDisplayTestSource(t, "import spx \"github.com/goplus/spx/v3\"\ntype Target = spx.Sprite\nvar sprite "+tt.typ+"\nsprite.|stepWith(1)\n")
				s := newSpxIntegrationTestServer(t, map[string][]byte{tt.filename: []byte(source)})
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				items := completionItemsAt(t, s, tt.filename, position)
				for _, label := range []string{"initFrom", "move"} {
					assert.NotContains(t, completionItemLabels(items), label)
				}
				for _, label := range []string{"step", "stepWith", "stepToTarget", "stepToXYpos", "onClick"} {
					require.NotNil(t, completionItemByLabel(items, label), label)
				}
				assert.Equal(t, 3, countCompletionItemLabel(items, "step"))
				item := completionItemByLabel(items, "stepWith")
				data := requireValueAs[*XGoCompletionItemData](t, item.Data)
				assert.Equal(t, "xgo:github.com/goplus/spx/v3?Sprite.stepWith", data.Definition.String())
				hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}, Position: position,
				}})
				require.NoError(t, err)
				require.NotNil(t, hover)
				doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Equal(t, hover.Contents, doc)
				links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}})
				require.NoError(t, err)
				assert.Contains(t, links, DocumentLink{Range: hover.Range, Target: ToPtr(URI(data.Definition.String()))})
			})
		}
	})

	t.Run("ImportFrameworkPackage", func(t *testing.T) {
		files := map[string][]byte{"main.spx": []byte("import \"github.com/goplus/spx\n")}
		s := newSpxIntegrationTestServer(t, files)
		items := completionItemsAt(t, s, "main.spx", Position{Character: 28})
		item := completionItemByLabel(items, SpxPkgPath)
		require.NotNil(t, item)
		assert.Equal(t, ModuleCompletion, item.Kind)
		assert.Equal(t, SpxPkgPath, item.InsertText)
		data := requireValueAs[*XGoCompletionItemData](t, item.Data)
		assert.Equal(t, ToPtr(SpxPkgPath), data.Definition.Package)
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
				s := newSpxIntegrationTestServer(t, m)

				items := completionItemsAt(t, s, "MySprite.spx", Position{Line: 2, Character: tt.character})
				assert.True(t, containsKwargCompletionItem(items, "speed", XGoDefinitionIdentifier{
					Package: ToPtr(SpxPkgPath),
					Name:    ToPtr("MotionOptions.Speed"),
				}))
				assert.True(t, containsKwargCompletionItem(items, "animation", XGoDefinitionIdentifier{
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
		s := newSpxIntegrationTestServer(t, m)

		emptyLineItems := completionItemsAt(t, s, "main.spx", Position{Line: 1, Character: 0})
		assert.NotEmpty(t, emptyLineItems)
		assert.Contains(t, completionItemLabels(emptyLineItems), "println")
		assert.True(t, containsCompletionDefinitionID(emptyLineItems, XGoDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MySprite"),
		}))

		assert.Contains(t, emptyLineItems, symbolDefinition{
			ID: XGoDefinitionIdentifier{
				Package: ToPtr(SpxPkgPath),
				Name:    ToPtr("Game.getWidget"),
			},
			Overview: "func getWidget(T Type, name WidgetName) *T",
			Detail:   "GetWidget returns the widget instance (in given type) with given name. It panics if not found.\n",

			CompletionItemLabel:            "getWidget",
			CompletionItemKind:             FunctionCompletion,
			CompletionItemInsertText:       "getWidget",
			CompletionItemInsertTextFormat: PlainTextTextFormat,
		}.completionItem(Markdown))

		mySpriteDotItems := completionItemsAt(t, s, "main.spx", Position{Line: 2, Character: 9})
		assert.NotEmpty(t, mySpriteDotItems)
		assert.NotContains(t, completionItemLabels(mySpriteDotItems), "println")
		assert.True(t, containsCompletionDefinitionID(mySpriteDotItems, XGoDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.turn"),
			OverloadID: ToPtr("0"),
		}))
		assert.True(t, containsCompletionDefinitionID(mySpriteDotItems, XGoDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.turn"),
			OverloadID: ToPtr("1"),
		}))
		assert.True(t, containsCompletionDefinitionID(mySpriteDotItems, XGoDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.clone"),
			OverloadID: ToPtr("0"),
		}))
		assert.True(t, containsCompletionDefinitionID(mySpriteDotItems, XGoDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.clone"),
			OverloadID: ToPtr("1"),
		}))
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
		s := newSpxIntegrationTestServer(t, m)

		items := completionItemsAt(t, s, "main.spx", Position{Line: 8, Character: 9}) // After "n"
		assert.Contains(t, completionItemLabels(items), "onClick")
		assert.True(t, containsCompletionDefinitionID(items, XGoDefinitionIdentifier{
			Package: ToPtr("github.com/goplus/spx/v3"),
			Name:    ToPtr("Sprite.onClick"),
		}))
		assert.Contains(t, completionItemLabels(items), "methodOne")
		assert.True(t, containsCompletionDefinitionID(items, XGoDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MyInterface.methodOne"),
		}))
	})

	t.Run("SpxSeconds", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`onStart => {
	wait 1
}
`),
			"assets/index.json": []byte(`{}`),
		}
		s := newSpxIntegrationTestServer(t, m)

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
