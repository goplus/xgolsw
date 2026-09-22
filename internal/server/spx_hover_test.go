//go:build !test_no_pkgdata

package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentHoverSpx(t *testing.T) {
	t.Run("ExplicitReceivers", func(t *testing.T) {
		for _, tt := range []struct{ name, filename, source, label, wantName string }{
			{"CameraInProject", "main.spx", "Camera.|follow \"MySprite\"\n", "follow", "Camera.follow#1"},
			{"CameraInWork", "MySprite.spx", "Camera.|follow \"MySprite\"\n", "follow", "Camera.follow#1"},
			{"PackageFunction", "main.xgo", "import spx \"github.com/goplus/spx/v3\"\nvar color = spx.|HSB(1, 2, 3)\n", "hSB", "hSB"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, position := typeDisplayTestSource(t, tt.source)
				files := map[string][]byte{"main.spx": nil, "MySprite.spx": nil}
				files[tt.filename] = []byte(source)
				s := newSpxIntegrationTestServer(t, files)
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				wantID := "xgo:github.com/goplus/spx/v3?" + tt.wantName
				hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}, Position: position,
				}})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, `def-id="`+wantID+`"`)
				var ids []string
				for _, item := range completionItemsAt(t, s, tt.filename, position) {
					if item.Label == tt.label {
						data := requireValueAs[*XGoCompletionItemData](t, item.Data)
						ids = append(ids, data.Definition.String())
					}
				}
				assert.Contains(t, ids, wantID)
				links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}})
				require.NoError(t, err)
				assert.Contains(t, links, DocumentLink{Range: hover.Range, Target: ToPtr(URI(wantID))})
			})
		}
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
		s := newSpxIntegrationTestServer(t, m)

		for _, tt := range []struct {
			filename        string
			line, character uint32
			start, end      uint32
			uri             XGoResourceURI
		}{
			{"main.spx", 1, 5, 5, 14, "spx://resources/sounds/MySound"},
			{"main.spx", 2, 0, 0, 8, "spx://resources/sprites/MySprite"},
			{"main.spx", 3, 20, 20, 30, "spx://resources/sprites/MySprite/costumes/costume1"},
			{"MySprite.spx", 7, 14, 13, 23, "spx://resources/sprites/MySprite"},
		} {
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: DocumentURI("file:///" + tt.filename)},
				Position:     Position{Line: tt.line, Character: tt.character},
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Equal(t, &Hover{
				Contents: resourceMarkupContent(tt.uri, Markdown),
				Range:    Range{Start: Position{Line: tt.line, Character: tt.start}, End: Position{Line: tt.line, Character: tt.end}},
			}, hover)
		}

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
		assert.Contains(t, mainSpxCameraFollowHover.Contents.Value, `def-id="xgo:github.com/goplus/spx/v3?Camera.follow#1"`)
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
		s := newSpxIntegrationTestServer(t, m)

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
}
