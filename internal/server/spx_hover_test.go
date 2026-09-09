package server

import (
	"testing"

	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentHoverSpx(t *testing.T) {
	t.Run("PackageDocumentationLookup", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx":                       []byte("import \"fmt\"\nfmt.Println(1)\nplay \"Sound\"\n"),
			"assets/index.json":              []byte(`{}`),
			"assets/sounds/Sound/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})
		const wantDoc = "Documentation supplied by the server."
		doc := &pkgdoc.PkgDoc{
			Path:  "fmt",
			Funcs: map[string]string{"Println": wantDoc},
		}
		var lookups []string
		s.lookupPkgDoc = func(pkgPath string) (*pkgdoc.PkgDoc, error) {
			assert.Equal(t, "fmt", pkgPath)
			lookups = append(lookups, pkgPath)
			return doc, nil
		}

		result, err := s.compileAt(s.workspaceRootFS)
		require.NoError(t, err)
		pkg, err := result.proj.Importer.Import("fmt")
		require.NoError(t, err)
		defs := result.spxDefinitionsFor(pkg.Scope().Lookup("Println"), "")
		require.Len(t, defs, 1)
		assert.Equal(t, wantDoc, defs[0].Detail)
		assert.Equal(t, []string{"fmt"}, lookups)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 5},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, wantDoc)
		assert.Equal(t, []string{"fmt", "fmt"}, lookups)

		hover, err = s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 7},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Equal(t, resourceMarkupContent("spx://resources/sounds/Sound", Markdown), hover.Contents)
		assert.Equal(t, []string{"fmt", "fmt"}, lookups)
	})

	t.Run("ResourcesAndMembers", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
play "MySound"
MySprite.turn Left
MySprite.setCostume "costume1"
Game.onClick => {}
onClick => {}
Camera.follow "MySprite"
`),
			"MySprite.spx": []byte(`
MySprite.onClick => {}
onClick => {}
onStart => {
	MySprite.turn Right
	clone
}
onTouchStart "MySprite", => {}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{"costumes":[{"name":"costume1"}]}`),
			"assets/sounds/MySound/index.json":   []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		mySoundRefHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 5},
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
				Start: Position{Line: 1, Character: 5},
				End:   Position{Line: 1, Character: 14},
			},
		}, mySoundRefHover)

		mySpriteRefHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 0},
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
				Start: Position{Line: 2, Character: 0},
				End:   Position{Line: 2, Character: 8},
			},
		}, mySpriteRefHover)

		mySpriteCostumeRefHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 20},
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
				Start: Position{Line: 3, Character: 20},
				End:   Position{Line: 3, Character: 30},
			},
		}, mySpriteCostumeRefHover)

		mySpriteSetCostumeFuncHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 9},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mySpriteSetCostumeFuncHover)
		assert.Equal(t, Range{
			Start: Position{Line: 3, Character: 9},
			End:   Position{Line: 3, Character: 19},
		}, mySpriteSetCostumeFuncHover.Range)

		GameOnClickHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 5},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, GameOnClickHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:github.com/goplus/spx/v3?Game.onClick\" overview=\"func onClick(onClick func())\">\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 4, Character: 5},
				End:   Position{Line: 4, Character: 12},
			},
		}, GameOnClickHover)

		mainSpxOnClickHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 0},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mainSpxOnClickHover)
		assert.Equal(t, &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: "<pre is=\"definition-item\" def-id=\"xgo:github.com/goplus/spx/v3?Game.onClick\" overview=\"func onClick(onClick func())\">\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 5, Character: 0},
				End:   Position{Line: 5, Character: 7},
			},
		}, mainSpxOnClickHover)

		mainSpxCameraFollowHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 6, Character: 8},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, mainSpxCameraFollowHover)
		assert.Contains(t, mainSpxCameraFollowHover.Contents.Value, `def-id="xgo:github.com/goplus/spx/v3?Game.follow#1"`)
		assert.Equal(t, Range{
			Start: Position{Line: 6, Character: 7},
			End:   Position{Line: 6, Character: 13},
		}, mainSpxCameraFollowHover.Range)

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
				Value: "<pre is=\"definition-item\" def-id=\"xgo:github.com/goplus/spx/v3?Sprite.onClick\" overview=\"func onClick(onClick func())\">\n</pre>\n",
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
				Value: "<pre is=\"definition-item\" def-id=\"xgo:github.com/goplus/spx/v3?Sprite.onClick\" overview=\"func onClick(onClick func())\">\n</pre>\n",
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
				Value: "<pre is=\"definition-item\" def-id=\"xgo:github.com/goplus/spx/v3?Sprite.clone#0\" overview=\"func clone()\">\n</pre>\n",
			},
			Range: Range{
				Start: Position{Line: 5, Character: 1},
				End:   Position{Line: 5, Character: 6},
			},
		}, mySpriteCloneFuncHover)

		onTouchStartFirstArgHover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 7, Character: 14},
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
				Start: Position{Line: 7, Character: 13},
				End:   Position{Line: 7, Character: 23},
			},
		}, onTouchStartFirstArgHover)
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
		assert.Contains(t, hover.Contents.Value, `def-id="xgo:github.com/goplus/spx/v3?Game.getWidget"`)
		assert.Contains(t, hover.Contents.Value, `overview="func getWidget(T Type, name WidgetName) *T"`)
		assert.Contains(t, hover.Contents.Value, `GetWidget returns the widget instance (in given type) with given name. It panics if not found.`)
		assert.Equal(t, Range{
			Start: Position{Line: 2, Character: 1},
			End:   Position{Line: 2, Character: 10},
		}, hover.Range)
	})

	t.Run("SpriteLineStartShouldNotResolveToSyntheticThis", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`onStart => {}
`),
			"MySprite.spx": []byte(`onStart => {
    step 10
    turn Left
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		// The characters on `onStart` should map to `onStart`, not synthetic `this`.
		for _, ch := range []uint32{0, 1, 2, 3, 4, 5, 6} {
			hover, err := s.textDocumentHover(&HoverParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
					Position:     Position{Line: 0, Character: ch},
				},
			})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, `def-id="xgo:github.com/goplus/spx/v3?Sprite.onStart"`)
			assert.NotContains(t, hover.Contents.Value, `var this`)
		}

		// The first four characters on indented lines are whitespaces and should not produce hover.
		for _, line := range []uint32{1, 2} {
			for _, ch := range []uint32{0, 1, 2, 3} {
				hover, err := s.textDocumentHover(&HoverParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
						Position:     Position{Line: line, Character: ch},
					},
				})
				require.NoError(t, err)
				assert.Nil(t, hover)
			}
		}
	})

}
