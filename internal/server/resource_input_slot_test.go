package server

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResourceAnalysisCreateResourceInputSlot(t *testing.T) {
	t.Run("LiteralConversion", func(t *testing.T) {
		const source = "type Asset string\necho Asset(\"Item\")\n"
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		ctx := inputSlotTestContext(t, s, "main.xgo")
		result := newTestResourceAnalysis()
		resolve := testResourceResolver(t, ctx.proj)
		collectResourceReferences(ctx.proj, []*resourceProvider{{
			analysis: result,
			resolve:  func(_ *xgo.Project, value resourceValue) (resourceID, bool) { return resolve(value) },
			inspect:  func(_ *xgo.Project, ref resourceRef) { result.addResourceRef(ref) },
		}})
		literal := inputSlotLiteral(t, ctx, `"Item"`)
		slot := result.createResourceInputSlot(ctx, literal, nil)
		require.NotNil(t, slot)
		assert.Equal(t, XGoResourceURI("test://resources/files/Item"), slot.Input.Value)
		assert.Equal(t, Range{Start: Position{Line: 1, Character: 11}, End: Position{Line: 1, Character: 17}}, slot.Range)
	})

	for _, tt := range []struct {
		name    string
		id      resourceID
		uri     XGoResourceURI
		context XGoResourceContextURI
	}{
		{
			name: "Scene", id: testResourceID{"scenes", "Item"},
			uri: "test://resources/scenes/Item", context: "test://resources/scenes",
		},
		{
			name: "Clip", id: testResourceID{"clips", "Item"},
			uri: "test://resources/clips/Item", context: "test://resources/clips",
		},
		{
			name: "Actor", id: testResourceID{"actors", "Item"},
			uri: "test://resources/actors/Item", context: "test://resources/actors",
		},
		{
			name: "Skin", id: testResourceID{"actors/Runner/skins", "Item"},
			uri:     "test://resources/actors/Runner/skins/Item",
			context: "test://resources/actors/Runner/skins",
		},
		{
			name: "Sequence", id: testResourceID{"actors/Runner/sequences", "Item"},
			uri:     "test://resources/actors/Runner/sequences/Item",
			context: "test://resources/actors/Runner/sequences",
		},
		{
			name: "Control", id: testResourceID{"controls", "Item"},
			uri: "test://resources/controls/Item", context: "test://resources/controls",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("const choice = \"other\"\nvar number int\necho \"Item\", \"Item\"\n")})
			ctx := inputSlotTestContext(t, s, "main.xgo")
			call := inputSlotCall(t, ctx, "echo")
			require.Len(t, call.Args, 2)
			first := requireValueAs[*ast.BasicLit](t, call.Args[0])
			second := requireValueAs[*ast.BasicLit](t, call.Args[1])
			result := newTestResourceAnalysis()
			result.expressions = map[ast.Expr]resourceExpression{second: {value: resourceValue{Static: true}, id: tt.id}}
			assert.Nil(t, result.createResourceInputSlot(ctx, first, gotypes.Typ[gotypes.String]))
			slot := result.createResourceInputSlot(ctx, second, gotypes.Typ[gotypes.String])
			require.NotNil(t, slot)
			assert.Equal(t, XGoInputSlotKindValue, slot.Kind)
			assert.Equal(t, XGoInputSlotAccept{Type: XGoInputTypeResourceName, ResourceContext: ToPtr(tt.context)}, slot.Accept)
			assert.Equal(t, XGoInput{Kind: XGoInputKindInPlace, Type: XGoInputTypeResourceName, Value: tt.uri}, slot.Input)
			assert.Equal(t, []string{"choice"}, slot.PredefinedNames)
			assert.Equal(t, Range{Start: Position{Line: 2, Character: 13}, End: Position{Line: 2, Character: 19}}, slot.Range)
		})
	}

	t.Run("SourceRanges", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			literal string
		}{
			{"Quoted", `"Item"`},
			{"Escaped", `"I\x74em"`},
			{"CarriageReturns", "`I\rt\r\rem`"},
			{"Multiline", "`I\r\n\U0001f600tem`"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const prefix = "//line virtual.xgo:100:20\r\necho \"\U0001f600\", "
				const suffix = ", 42\r\n"
				source := prefix + tt.literal + suffix
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				ctx := inputSlotTestContext(t, s, "main.xgo")
				call := inputSlotCall(t, ctx, "echo")
				require.Len(t, call.Args, 3)
				lit := requireValueAs[*ast.BasicLit](t, call.Args[1])
				result := newTestResourceAnalysis()
				result.expressions = map[ast.Expr]resourceExpression{lit: {value: resourceValue{Static: true}, id: testResourceID{"clips", "Item"}}}
				slot := result.createResourceInputSlot(ctx, lit, nil)
				require.NotNil(t, slot)
				start := PositionOffset([]byte(source), slot.Range.Start)
				end := PositionOffset([]byte(source), slot.Range.End)
				assert.Equal(t, tt.literal, source[start:end])
				assert.Equal(t, prefix+`"Other"`+suffix, source[:start]+`"Other"`+source[end:])
			})
		}
	})

	t.Run("SourceChanges", func(t *testing.T) {
		const source = "echo \"Item\"\n"
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		ctx := inputSlotTestContext(t, s, "main.xgo")
		lit := inputSlotLiteral(t, ctx, `"Item"`)
		result := newTestResourceAnalysis()
		result.expressions = map[ast.Expr]resourceExpression{lit: {value: resourceValue{Static: true}, id: testResourceID{"clips", "Item"}}}
		require.NotNil(t, result.createResourceInputSlot(ctx, lit, nil))
		s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(source), Version: 1}})
		ctx = inputSlotTestContext(t, s, "main.xgo")
		assert.Nil(t, result.createResourceInputSlot(ctx, inputSlotLiteral(t, ctx, `"Item"`), nil))
	})
}
