package server

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateSpxColorInputSlot(t *testing.T) {
	for _, tt := range []struct {
		name        string
		args        string
		constructor XGoInputTypeSpxColorConstructor
		want        []float64
	}{
		{"HSB", "12, 34, 56", XGoInputTypeSpxColorConstructorHSB, []float64{12, 34, 56}},
		{"HSBA", "12, 34, 56, 78", XGoInputTypeSpxColorConstructorHSBA, []float64{12, 34, 56, 78}},
		{"Float", "1.25, .5, 2e1", XGoInputTypeSpxColorConstructorHSB, []float64{1.25, .5, 20}},
		{"IntegerBases", "0x12, 0o34, 0b101", XGoInputTypeSpxColorConstructorHSB, []float64{18, 28, 5}},
		{"Underscores", "1_0, 2_0.5, 0x1p2", XGoInputTypeSpxColorConstructorHSB, []float64{10, 20.5, 4}},
		{"Empty", "", XGoInputTypeSpxColorConstructorHSB, nil},
		{"TooFew", "12, 34", XGoInputTypeSpxColorConstructorHSB, nil},
		{"TooMany", "12, 34, 56, 78", XGoInputTypeSpxColorConstructorHSB, nil},
		{"AlphaTooFew", "12, 34, 56", XGoInputTypeSpxColorConstructorHSBA, nil},
		{"AlphaTooMany", "12, 34, 56, 78, 90", XGoInputTypeSpxColorConstructorHSBA, nil},
		{"ExtraExpression", "12, 34, 56, sample()", XGoInputTypeSpxColorConstructorHSB, nil},
		{"Identifier", "12, component, 56", XGoInputTypeSpxColorConstructorHSB, nil},
		{"Unary", "12, -34, 56", XGoInputTypeSpxColorConstructorHSB, nil},
		{"Parenthesized", "12, (34), 56", XGoInputTypeSpxColorConstructorHSB, nil},
		{"Expression", "12, 30+4, 56", XGoInputTypeSpxColorConstructorHSB, nil},
		{"String", "12, \"34\", 56", XGoInputTypeSpxColorConstructorHSB, nil},
		{"IntegerOverflow", "12, 9223372036854775808, 56", XGoInputTypeSpxColorConstructorHSB, nil},
		{"FloatOverflow", "12, 1e1000, 56", XGoInputTypeSpxColorConstructorHSB, nil},
		{"Ellipsis", "12, 34, 56 ...", XGoInputTypeSpxColorConstructorHSB, nil},
		{"Kwargs", "12, 34, 56, extra=78", XGoInputTypeSpxColorConstructorHSB, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const prefix = `type Swatch struct {}
var available Swatch
var unrelated string
const component = 34
func sample(values ...any) Swatch { return Swatch{} }
func run() {
` + "\tprintln \"\U0001f600\", "
			expression := "sample(" + tt.args + ")"
			const suffix = "\n}\n"
			source := prefix + expression + suffix
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			ctx := inputSlotTestContext(t, s, "main.xgo")
			call := inputSlotCall(t, ctx, "sample")
			target := ctx.typeInfo.Pkg.Scope().Lookup("Swatch")
			require.NotNil(t, target)
			slot := createSpxColorInputSlot(ctx, call, target.Type(), tt.constructor)
			if tt.want == nil {
				assert.Nil(t, slot)
				return
			}
			require.NotNil(t, slot)
			assert.Equal(t, XGoInputSlotKindValue, slot.Kind)
			assert.Equal(t, XGoInputSlotAccept{Type: XGoInputTypeSpxColor}, slot.Accept)
			assert.Equal(t, XGoInput{
				Kind: XGoInputKindInPlace, Type: XGoInputTypeSpxColor,
				Value: XGoInputSpxColorValue{Constructor: tt.constructor, Args: tt.want},
			}, slot.Input)
			assert.Equal(t, []string{"available"}, slot.PredefinedNames)
			assert.Equal(t, Range{Start: Position{Line: 6, Character: 15}, End: Position{Line: 6, Character: uint32(15 + len(expression))}}, slot.Range)
			start := PositionOffset([]byte(source), slot.Range.Start)
			end := PositionOffset([]byte(source), slot.Range.End)
			assert.Equal(t, expression, source[start:end])
			updated := source[:start] + "available" + source[end:]
			assert.Equal(t, prefix+"available"+suffix, updated)
			_, err := newTestServer(t, map[string][]byte{"main.xgo": []byte(updated)}).getProj().TypeInfo()
			require.NoError(t, err)
		})
	}

	t.Run("PhysicalSource", func(t *testing.T) {
		const source = "func sample(values ...any) {}\n//line virtual.xgo:100:20\r\nsample(12,\r\n34, 56)\r\n"
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		ctx := inputSlotTestContext(t, s, "main.xgo")
		call := inputSlotCall(t, ctx, "sample")
		slot := createSpxColorInputSlot(ctx, call, nil, XGoInputTypeSpxColorConstructorHSB)
		require.NotNil(t, slot)
		assert.Equal(t, Range{Start: Position{Line: 2}, End: Position{Line: 3, Character: 7}}, slot.Range)
		assert.Equal(t, []float64{12, 34, 56}, requireValueAs[XGoInputSpxColorValue](t, slot.Input.Value).Args)
	})

	t.Run("SourceKinds", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			filename  string
			newServer testServerFactory
		}{
			{"XGo", "main.xgo", newTestServer},
			{"StandaloneClass", "Record.gox", newTestServer},
			{"ProjectClass", "main_fixture.gox", newFrameworkTestServer},
			{"WorkClass", "Worker_fixture.gox", newFrameworkTestServer},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					tt.filename: []byte("type Swatch struct {}\nvar available Swatch\nfunc sample(values ...any) Swatch { return Swatch{} }\nprintln sample(12, 34, 56)\n"),
				}
				if tt.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := tt.newServer(t, files)
				ctx := inputSlotTestContext(t, s, tt.filename)
				_, err := ctx.proj.TypeInfo()
				require.NoError(t, err)
				target := ctx.typeInfo.Pkg.Scope().Lookup("Swatch")
				require.NotNil(t, target)
				slot := createSpxColorInputSlot(ctx, inputSlotCall(t, ctx, "sample"), target.Type(), XGoInputTypeSpxColorConstructorHSB)
				require.NotNil(t, slot)
				assert.Equal(t, []string{"available"}, slot.PredefinedNames)
				assert.Equal(t, XGoInputSpxColorValue{Constructor: XGoInputTypeSpxColorConstructorHSB, Args: []float64{12, 34, 56}}, slot.Input.Value)
			})
		}
	})
}

func TestCreateSpxResourceInputSlot(t *testing.T) {
	for _, tt := range []struct {
		name    string
		id      SpxResourceID
		uri     SpxResourceURI
		context SpxResourceContextURI
	}{
		{
			name: "Backdrop", id: SpxBackdropResourceID{"Item"},
			uri: "spx://resources/backdrops/Item", context: "spx://resources/backdrops",
		},
		{
			name: "Sound", id: SpxSoundResourceID{"Item"},
			uri: "spx://resources/sounds/Item", context: "spx://resources/sounds",
		},
		{
			name: "Sprite", id: SpxSpriteResourceID{"Item"},
			uri: "spx://resources/sprites/Item", context: "spx://resources/sprites",
		},
		{
			name: "Costume", id: SpxSpriteCostumeResourceID{"Runner", "Item"},
			uri:     "spx://resources/sprites/Runner/costumes/Item",
			context: "spx://resources/sprites/Runner/costumes",
		},
		{
			name: "Animation", id: SpxSpriteAnimationResourceID{"Runner", "Item"},
			uri:     "spx://resources/sprites/Runner/animations/Item",
			context: "spx://resources/sprites/Runner/animations",
		},
		{
			name: "Widget", id: SpxWidgetResourceID{"Item"},
			uri: "spx://resources/widgets/Item", context: "spx://resources/widgets",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("const choice = \"other\"\nvar number int\necho \"Item\", \"Item\"\n")})
			ctx := inputSlotTestContext(t, s, "main.xgo")
			call := inputSlotCall(t, ctx, "echo")
			require.Len(t, call.Args, 2)
			first := requireValueAs[*ast.BasicLit](t, call.Args[0])
			second := requireValueAs[*ast.BasicLit](t, call.Args[1])
			ctx.spxResult = newCompileResult(s.getProj(), s.lookupPkgDoc)
			ctx.spxResult.addSpxResourceRef(SpxResourceRef{ID: tt.id, Kind: SpxResourceRefKindStringLiteral, Node: second})
			assert.Nil(t, createSpxResourceInputSlot(ctx, first, gotypes.Typ[gotypes.String]))
			slot := createSpxResourceInputSlot(ctx, second, gotypes.Typ[gotypes.String])
			require.NotNil(t, slot)
			assert.Equal(t, XGoInputSlotKindValue, slot.Kind)
			assert.Equal(t, XGoInputSlotAccept{Type: XGoInputTypeSpxResourceName, ResourceContext: ToPtr(tt.context)}, slot.Accept)
			assert.Equal(t, XGoInput{Kind: XGoInputKindInPlace, Type: XGoInputTypeSpxResourceName, Value: tt.uri}, slot.Input)
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
				ctx.spxResult = newCompileResult(s.getProj(), s.lookupPkgDoc)
				ctx.spxResult.addSpxResourceRef(SpxResourceRef{ID: SpxSoundResourceID{"Item"}, Node: lit})
				slot := createSpxResourceInputSlot(ctx, lit, nil)
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
		result := newCompileResult(s.getProj(), s.lookupPkgDoc)
		result.addSpxResourceRef(SpxResourceRef{ID: SpxSoundResourceID{"Item"}, Node: lit})
		ctx.spxResult = result
		require.NotNil(t, createSpxResourceInputSlot(ctx, lit, nil))
		s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(source), Version: 1}})
		ctx = inputSlotTestContext(t, s, "main.xgo")
		ctx.spxResult = result
		assert.Nil(t, createSpxResourceInputSlot(ctx, inputSlotLiteral(t, ctx, `"Item"`), nil))
	})
}

func TestSpxEnumInput(t *testing.T) {
	for _, tt := range []struct {
		name      string
		inputType XGoInputType
		value     string
		want      any
	}{
		{"Direction", XGoInputTypeSpxDirection, "-90", float64(-90)},
		{"FractionalDirection", XGoInputTypeSpxDirection, "1.5", 1.5},
		{"LayerAction", XGoInputTypeSpxLayerAction, "1", "Option"},
		{"DirAction", XGoInputTypeSpxDirAction, "2", "Option"},
		{"EffectKind", XGoInputTypeSpxEffectKind, `"effect"`, "Option"},
		{"Key", XGoInputTypeSpxKey, "3", "Option"},
		{"SpecialObj", XGoInputTypeSpxSpecialObj, "4", "Option"},
		{"RotationStyle", XGoInputTypeSpxRotationStyle, "5", "Option"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("const Option = " + tt.value + "\n")})
			info, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			cnst := requireValueAs[*gotypes.Const](t, info.Pkg.Scope().Lookup("Option"))
			assert.Equal(t, XGoInput{Kind: XGoInputKindInPlace, Type: tt.inputType, Value: tt.want}, spxEnumInput(cnst, tt.inputType))
		})
	}
}
