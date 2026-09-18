package server

import (
	"go/constant"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletionContextCollectResourceNames(t *testing.T) {
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
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				ctx := newCompletionTestContext(t, s, "main.xgo", position)
				ctx.collectResourceNames([]resourceID{testResourceID{"scenes", tt.resource}})
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
				call := resourceTestCall(t, proj, "main.xgo")
				info, err := proj.TypeInfo()
				require.NoError(t, err, updated)
				value := info.Types[call.Args[len(call.Args)-1]].Value
				require.NotNil(t, value, updated)
				assert.Equal(t, tt.resource, constant.StringVal(value))
			})
		}

		t.Run("EmptyLineInMultilineLiteral", func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"main.xgo": []byte("type Record struct { Score int }\necho `before\n\nafter`\n"),
			})
			ctx := newCompletionTestContext(t, s, "main.xgo", Position{Line: 2})
			ctx.collectResourceNames([]resourceID{testResourceID{"scenes", "Studio"}})
			ctx.collectPropertyNames("Record")
			assert.Empty(t, ctx.itemSet.items)
		})
	})

	t.Run("StringPrefixes", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			literal    string
			resource   string
			wantFilter string
		}{
			{"DollarEscape", `"$$|name"`, "$name", `"$$name"`},
			{"MixedEscapes", `"\x24$$|name"`, "$$name", `"\x24$$name"`},
			{"EscapedInterpolation", `"$${na|me}"`, "${name}", `"$${name}"`},
			{"RawDollarEscape", "`$$|name`", "$name", "`$$name`"},
			{"UnterminatedDollarEscape", `"$$|`, "$name", `"$$name"`},
			{"IncompleteEscape", `"\x|"`, "$name", `"$name"`},
			{"DynamicPrefix", `"${choice}|suffix"`, "$name", `"$name"`},
		} {
			t.Run(tt.name, func(t *testing.T) {
				cursor := strings.IndexByte(tt.literal, '|')
				source := "echo " + strings.Replace(tt.literal, "|", "", 1) + "\n"
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				position := Position{Character: uint32(5 + cursor)}
				ctx := newCompletionTestContext(t, s, "main.xgo", position)
				ctx.collectResourceNames([]resourceID{testResourceID{"scenes", tt.resource}})
				require.Len(t, ctx.itemSet.items, 1)
				item := ctx.itemSet.items[0]
				assert.Equal(t, tt.wantFilter, item.FilterText)
				require.NotNil(t, item.TextEdit)
				edit := requireValueAs[TextEdit](t, item.TextEdit.Value)
				updated := applyCompletionTestEdits(t, source, position, []TextEdit{edit})
				proj := newTestServer(t, map[string][]byte{"main.xgo": []byte(updated)}).getProj()
				call := resourceTestCall(t, proj, "main.xgo")
				info, err := proj.TypeInfo()
				require.NoError(t, err)
				value := info.Types[call.Args[0]].Value
				require.NotNil(t, value)
				assert.Equal(t, tt.resource, constant.StringVal(value))
			})
		}
	})
}
