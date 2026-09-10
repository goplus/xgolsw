package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletionSpx(t *testing.T) {
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
				s := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})
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
				s := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})
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
		s := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})
		items := completionItemsAt(t, s, "main.spx", Position{Line: 4, Character: 17})
		assert.NotContains(t, completionItemLabels(items), `"score"`)
		assert.NotContains(t, completionItemLabels(items), `"xpos"`)
	})

	t.Run("SpriteAutoBinding", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx":                         []byte("var Runner Sprite\nfunc test() {\n\tvar target Sprite = R\n}\n"),
			"assets/sprites/Runner/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})
		items := completionItemsAt(t, s, "main.spx", Position{Line: 2, Character: 22})
		item := completionItemByLabel(items, "Runner")
		require.NotNil(t, item)
		assert.Equal(t, "Runner", item.InsertText)
		data := requireValueAs[*CompletionItemData](t, item.Data)
		assert.Equal(t, "xgo:main?Game.Runner", data.Definition.String())
	})
}
