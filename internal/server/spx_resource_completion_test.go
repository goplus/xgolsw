package server

import (
	"encoding/json"
	"go/constant"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletionContextCollectSpxResourceNames(t *testing.T) {
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
					result := newCompileResult(s.getProj(), s.lookupPkgDoc)
					result.spxResourceSet = *set
					ctx := &completionContext{spxResult: result, itemSet: newCompletionItemSet(kind)}
					sprite := set.Sprite(tt.sprite)
					if tt.sprite != "" {
						require.NotNil(t, sprite)
					}
					ctx.collectSpxResourceNames(tt.kind, sprite)
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
			ctx := &completionContext{spxResult: newCompileResult(s.getProj(), s.lookupPkgDoc), itemSet: newCompletionItemSet(Markdown)}
			ctx.collectSpxResourceNames(kind, nil)
			assert.Empty(t, ctx.itemSet.items)
		}
	})

	t.Run("StringEdits", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			source   string
			resource string
		}{
			{"OutsideLiteral", "echo re|\n", "${name}"},
			{"Quoted", "echo \"re|source\"\n", "A\"B\\C$${name}"},
			{"EscapedPrefix", "echo \"re\\x73|ource\"\n", "resource"},
			{"Unterminated", "echo \"re|", "Studio"},
			{"Raw", "echo `re|source`\n", "A\"B\\C"},
			{"RawWithBacktick", "echo `re|source`\n", "A`B"},
			{"RawWithDollar", "echo `re|source`\n", "$name"},
			{"UTF16", "echo \"\U0001f600\", \"re|source\"\r\n", "\U0001f600"},
			{"LineDirective", "//line virtual.xgo:100:20\necho \"re|source\"\n", "Studio"},
			{"RawCarriageReturn", "echo `re\rs|ource`\n", "Studio"},
			{"RawUnterminated", "echo `re|", "Studio"},
			{"RawUnterminatedCR", "echo `re\r\rsource|", "Studio"},
			{"RawEndWithCR", "echo `re\r\rsource|`\n", "Studio"},
			{"MultilineStart", "echo `re|\r\nsource`\n", "Studio"},
			{"MultilineMiddle", "echo `before\r\nre|source\r\nafter`\n", "A`B"},
			{"MultilineEnd", "echo `before\r\nresource|`\n", "Studio"},
			{"MultilineReplacement", "echo `before\r\nre|source\r\nafter`\n", "A\nB"},
			{"EmptyName", "echo \"re|source\"\n", ""},
			{"Controls", "echo \"re|source\"\n", "A\n\r\t\x00B"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				cursor := strings.IndexByte(tt.source, '|')
				source := strings.Replace(tt.source, "|", "", 1)
				prefix := source[:cursor]
				position := Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(UTF16Len(prefix[strings.LastIndex(prefix, "\n")+1:]))}
				metadata, err := json.Marshal(map[string]any{"backdrops": []map[string]string{{"name": tt.resource}}})
				require.NoError(t, err)
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source), "assets/index.json": metadata})
				ctx := newCompletionTestContext(t, s, "main.xgo", position)
				set, err := NewSpxResourceSet(s.getProj())
				require.NoError(t, err)
				ctx.spxResult = newCompileResult(s.getProj(), s.lookupPkgDoc)
				ctx.spxResult.spxResourceSet = *set
				ctx.collectSpxResourceNames(spxResourceCompletionBackdrop, nil)
				require.Len(t, ctx.itemSet.items, 1)
				item := ctx.itemSet.items[0]
				assert.Equal(t, ToPtr(PlainTextTextFormat), item.InsertTextFormat)
				if tt.name == "EscapedPrefix" {
					assert.Equal(t, `"re\x73ource"`, item.FilterText)
				} else if ctx.stringLit != nil {
					delimiter := string(ctx.stringLit.Value[0])
					wantFilter := delimiter + tt.resource + delimiter
					if tt.name == "MultilineMiddle" || tt.name == "MultilineEnd" || tt.name == "MultilineReplacement" {
						wantFilter = tt.resource
					}
					assert.Equal(t, wantFilter, item.FilterText)
				}
				var edit TextEdit
				if tt.name == "OutsideLiteral" {
					edit = TextEdit{Range: Range{Start: Position{Character: 5}, End: Position{Character: 7}}, NewText: item.InsertText}
				} else {
					require.NotNil(t, item.TextEdit)
					edit = requireValueAs[TextEdit](t, item.TextEdit.Value)
				}
				updated := applyCompletionTestEdits(t, source, position, append([]TextEdit{edit}, item.AdditionalTextEdits...))
				proj := newTestServer(t, map[string][]byte{"main.xgo": []byte(updated)}).getProj()
				call := spxResourceTestCall(t, proj, "main.xgo")
				info, err := proj.TypeInfo()
				require.NoError(t, err, updated)
				value := info.Types[call.Args[len(call.Args)-1]].Value
				require.NotNil(t, value, updated)
				assert.Equal(t, tt.resource, constant.StringVal(value))
			})
		}

		t.Run("EmptyLineInMultilineLiteral", func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"main.xgo":          []byte("type Record struct { Score int }\necho `before\n\nafter`\n"),
				"assets/index.json": []byte(`{"backdrops":[{"name":"Studio"}]}`),
			})
			ctx := newCompletionTestContext(t, s, "main.xgo", Position{Line: 2})
			set, err := NewSpxResourceSet(s.getProj())
			require.NoError(t, err)
			ctx.spxResult = newCompileResult(s.getProj(), s.lookupPkgDoc)
			ctx.spxResult.spxResourceSet = *set
			ctx.collectSpxResourceNames(spxResourceCompletionBackdrop, nil)
			ctx.collectPropertyNames("Record")
			assert.Empty(t, ctx.itemSet.items)
		})
	})
}
