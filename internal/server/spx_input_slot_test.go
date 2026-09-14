//go:build !test_no_pkgdata

package server

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerSpxGetInputSlots(t *testing.T) {
	t.Run("ColorCalls", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			expression string
			want       *XGoInputSpxColorValue
		}{
			{"HSB", "sdk.HSB(12, 34, 56)", &XGoInputSpxColorValue{Constructor: XGoInputTypeSpxColorConstructorHSB, Args: []float64{12, 34, 56}}},
			{"HSBA", "sdk.HSBA(12, 34, 56, 78)", &XGoInputSpxColorValue{Constructor: XGoInputTypeSpxColorConstructorHSBA, Args: []float64{12, 34, 56, 78}}},
			{"TooMany", "sdk.HSB(12, 34, 56, 78)", nil},
			{"ExtraExpression", "sdk.HSB(12, 34, 56, rand(100))", nil},
			{"ExtraKwarg", "sdk.HSB(12, 34, 56, extra=78)", nil},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const prefix = "import sdk \"github.com/goplus/spx/v3\"\nvar color = "
				source := prefix + tt.expression + "\n"
				s := newSpxTestServer(t, map[string][]byte{"main.spx": []byte(source), "assets/index.json": []byte(`{}`)})
				slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}})
				require.NoError(t, err)
				if tt.want == nil {
					for _, slot := range slots {
						assert.NotEqual(t, XGoInputTypeSpxColor, slot.Input.Type)
					}
					for _, value := range []int64{12, 34, 56} {
						assert.NotNil(t, findInputSlot(slots, value, "", XGoInputTypeInteger, XGoInputKindInPlace))
					}
					return
				}
				require.Len(t, slots, 1)
				slot := slots[0]
				assert.Equal(t, XGoInput{Kind: XGoInputKindInPlace, Type: XGoInputTypeSpxColor, Value: *tt.want}, slot.Input)
				assert.Equal(t, Range{Start: Position{Line: 1, Character: 12}, End: Position{Line: 1, Character: uint32(12 + len(tt.expression))}}, slot.Range)
				start := PositionOffset([]byte(source), slot.Range.Start)
				end := PositionOffset([]byte(source), slot.Range.End)
				updated := source[:start] + "sdk.HSB(10, 20, 30)" + source[end:]
				assert.Equal(t, prefix+"sdk.HSB(10, 20, 30)\n", updated)
				_, err = newSpxTestServer(t, map[string][]byte{"main.spx": []byte(updated)}).getProj().TypeInfo()
				require.NoError(t, err)
			})
		}
	})

	t.Run("ResourceContextsWithLineDirectives", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			receiver string
		}{
			{"Implicit", "Runner.spx", ""},
			{"Explicit", "main.spx", "Runner."},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"main.spx": nil, "Runner.spx": nil,
					"assets/index.json":                []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"runner"}],"fAnimations":{"walk":{}}}`),
				}
				files[tt.filename] = []byte("onStart => {\n\tconst Costume = \"runner\"\n\tconst Animation = \"walk\"\n//line virtual.spx:100:20\n\t" +
					tt.receiver + "setCostume \"runner\"\n\t" + tt.receiver + "setCostume Costume\n\t" +
					tt.receiver + "animate \"walk\"\n\t" + tt.receiver + "animate Animation\n}\n")
				s := newSpxTestServer(t, files)
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}}})
				require.NoError(t, err)
				for _, want := range []struct {
					line    uint32
					context SpxResourceContextURI
				}{
					{4, FormatSpxSpriteCostumeResourceContextURI("Runner")},
					{5, FormatSpxSpriteCostumeResourceContextURI("Runner")},
					{6, FormatSpxSpriteAnimationResourceContextURI("Runner")},
					{7, FormatSpxSpriteAnimationResourceContextURI("Runner")},
				} {
					var matching []XGoInputSlot
					for _, slot := range slots {
						if slot.Range.Start.Line == want.line {
							matching = append(matching, slot)
						}
					}
					require.Len(t, matching, 1, "line %d", want.line)
					assert.Equal(t, SpxInputTypeResourceName, matching[0].Accept.Type)
					assert.Equal(t, ToPtr(want.context), matching[0].Accept.ResourceContext)
				}
			})
		}
	})

	t.Run("InPlaceValues", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx": []byte(`onStart => {
	direction := Left
	layerAction := Front
	dirAction := Forward
	myColor := HSB(255, 0, 0)
	otherColor := HSBA(0, 255, 0, 128)
	Runner.stepTo "Other"
	Runner.stepTo Other
}
`),
			"Runner.spx": nil, "Other.spx": nil,
			"assets/index.json":                []byte(`{}`),
			"assets/sprites/Runner/index.json": []byte(`{}`),
			"assets/sprites/Other/index.json":  []byte(`{}`),
		})
		_, err := s.getProj().TypeInfo()
		require.NoError(t, err)
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}})
		require.NoError(t, err)
		var values []XGoInputSlot
		for _, slot := range slots {
			if slot.Kind == XGoInputSlotKindValue {
				values = append(values, slot)
			}
		}
		require.Len(t, values, 7)
		for i, want := range []struct {
			inputType  XGoInputType
			value      any
			accept     XGoInputSlotAccept
			start, end uint32
			emptyNames bool
		}{
			{
				inputType: XGoInputTypeSpxDirection, value: float64(-90),
				accept: XGoInputSlotAccept{Type: XGoInputTypeDecimal},
				start:  14, end: 18,
			},
			{
				inputType: XGoInputTypeSpxLayerAction, value: "Front",
				accept: XGoInputSlotAccept{Type: XGoInputTypeSpxLayerAction},
				start:  16, end: 21,
				emptyNames: true,
			},
			{
				inputType: XGoInputTypeSpxDirAction, value: "Forward",
				accept: XGoInputSlotAccept{Type: XGoInputTypeSpxDirAction},
				start:  14, end: 21,
				emptyNames: true,
			},
			{
				inputType: XGoInputTypeSpxColor,
				value:     XGoInputSpxColorValue{Constructor: XGoInputTypeSpxColorConstructorHSB, Args: []float64{255, 0, 0}},
				accept:    XGoInputSlotAccept{Type: XGoInputTypeSpxColor},
				start:     12, end: 26,
				emptyNames: true,
			},
			{
				inputType: XGoInputTypeSpxColor,
				value:     XGoInputSpxColorValue{Constructor: XGoInputTypeSpxColorConstructorHSBA, Args: []float64{0, 255, 0, 128}},
				accept:    XGoInputSlotAccept{Type: XGoInputTypeSpxColor},
				start:     15, end: 35,
			},
			{
				inputType: XGoInputTypeSpxResourceName, value: SpxResourceURI("spx://resources/sprites/Other"),
				accept: XGoInputSlotAccept{Type: XGoInputTypeSpxResourceName, ResourceContext: ToPtr(SpxSpriteResourceContextURI)},
				start:  15, end: 22,
			},
			{
				inputType: XGoInputTypeSpxSpriteInstance, value: SpxResourceURI("spx://resources/sprites/Other"),
				accept: XGoInputSlotAccept{Type: XGoInputTypeSpxSpriteInstance, ResourceContext: ToPtr(SpxSpriteResourceContextURI)},
				start:  15, end: 20,
			},
		} {
			slot := values[i]
			assert.Equal(t, XGoInput{Kind: XGoInputKindInPlace, Type: want.inputType, Value: want.value}, slot.Input, "slot %d", i)
			assert.Equal(t, want.accept, slot.Accept, "slot %d", i)
			assert.Equal(t, Range{Start: Position{Line: uint32(i + 1), Character: want.start}, End: Position{Line: uint32(i + 1), Character: want.end}}, slot.Range, "slot %d", i)
			if want.emptyNames {
				assert.Empty(t, slot.PredefinedNames, "slot %d", i)
			} else {
				assert.NotEmpty(t, slot.PredefinedNames, "slot %d", i)
			}
		}
		assert.Equal(t, []string{"myColor"}, values[4].PredefinedNames)
		assert.Contains(t, values[6].PredefinedNames, "Other")
	})

	t.Run("ResourceIdentifiers", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			call    string
			context SpxResourceContextURI
		}{
			{"Backdrop", "setBackdrop choice", SpxBackdropResourceContextURI},
			{"Sound", "play choice", SpxSoundResourceContextURI},
			{"Sprite", "Runner.stepTo choice", SpxSpriteResourceContextURI},
			{"Costume", "Runner.setCostume choice", FormatSpxSpriteCostumeResourceContextURI("Runner")},
			{"Animation", "Runner.animate choice", FormatSpxSpriteAnimationResourceContextURI("Runner")},
			{"Widget", "getWidget Monitor, choice", SpxWidgetResourceContextURI},
		} {
			t.Run(tt.name, func(t *testing.T) {
				for _, declaration := range []struct {
					name   string
					source string
				}{
					{"Constant", `const choice = "Item"`},
					{"Variable", `var choice = "Item"`},
				} {
					t.Run(declaration.name, func(t *testing.T) {
						source := "onStart => {\n\t" + declaration.source + "\n\t" + tt.call + "\n}\n"
						s := newSpxTestServer(t, map[string][]byte{
							"main.spx": []byte(source), "Runner.spx": nil, "Item.spx": nil,
							"assets/index.json":                []byte(`{"costumes":[{"name":"Item"}],"zorder":[{"name":"Item","type":"monitor"}]}`),
							"assets/sounds/Item/index.json":    []byte(`{}`),
							"assets/sprites/Item/index.json":   []byte(`{}`),
							"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"Item"}],"fAnimations":{"Item":{}}}`),
						})
						_, err := s.getProj().TypeInfo()
						require.NoError(t, err)
						slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}})
						require.NoError(t, err)
						slot := findInputSlot(slots, nil, "choice", XGoInputTypeString, XGoInputKindPredefined)
						require.NotNil(t, slot)
						assert.Equal(t, XGoInputSlotKindValue, slot.Kind)
						assert.Equal(t, XGoInput{Kind: XGoInputKindPredefined, Type: XGoInputTypeString, Name: "choice"}, slot.Input)
						assert.Equal(t, XGoInputSlotAccept{Type: XGoInputTypeSpxResourceName, ResourceContext: ToPtr(tt.context)}, slot.Accept)
						assert.Contains(t, slot.PredefinedNames, "choice")
						assert.Equal(t, Range{Start: Position{Line: 2, Character: uint32(1 + len(tt.call) - len("choice"))}, End: Position{Line: 2, Character: uint32(1 + len(tt.call))}}, slot.Range)
					})
				}
			})
		}
	})

	t.Run("UnboundResourceReceiver", func(t *testing.T) {
		for _, receiver := range []struct {
			name        string
			declaration string
			expression  string
		}{
			{"Local", "target := Runner", "target"},
			{"Shadowed", "Runner := Other", "Runner"},
		} {
			t.Run(receiver.name, func(t *testing.T) {
				for _, method := range []struct {
					name string
					call string
				}{
					{"Costume", "setCostume"},
					{"Animation", "animate"},
				} {
					t.Run(method.name, func(t *testing.T) {
						source := "onStart => {\n\tconst choice = \"Item\"\n\t" + receiver.declaration + "\n\t" + receiver.expression + "." + method.call + " choice\n}\n"
						s := newSpxTestServer(t, map[string][]byte{
							"main.spx": nil, "Host.spx": []byte(source), "Runner.spx": nil, "Other.spx": nil,
							"assets/index.json":                []byte(`{}`),
							"assets/sprites/Host/index.json":   []byte(`{"costumes":[{"name":"Item"}],"fAnimations":{"Item":{}}}`),
							"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"Item"}],"fAnimations":{"Item":{}}}`),
							"assets/sprites/Other/index.json":  []byte(`{"costumes":[{"name":"Item"}],"fAnimations":{"Item":{}}}`),
						})
						_, err := s.getProj().TypeInfo()
						require.NoError(t, err)
						slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///Host.spx"}}})
						require.NoError(t, err)
						assert.NotNil(t, findInputSlot(slots, "Item", "", XGoInputTypeString, XGoInputKindInPlace))
						for _, slot := range slots {
							assert.NotEqual(t, uint32(3), slot.Range.Start.Line, "an unbound receiver cannot supply a resource context")
						}
					})
				}
			})
		}
	})

	t.Run("SpxSpriteInstanceVariable", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var target Sprite

onStart => {
	target = OtherSprite
	MySprite.stepTo target
}
`),
			"MySprite.spx":                          []byte(``),
			"OtherSprite.spx":                       []byte(``),
			"assets/index.json":                     []byte(`{}`),
			"assets/sprites/MySprite/index.json":    []byte(`{}`),
			"assets/sprites/OtherSprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		inputSlots, err := s.xgoGetInputSlots([]SpxGetInputSlotsParams{
			{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}},
		})
		require.NoError(t, err)

		slot := findInputSlot(inputSlots, nil, "target", SpxInputTypeSpriteInstance, SpxInputKindPredefined)
		require.NotNil(t, slot)
		assert.Equal(t, SpxInputTypeSpriteInstance, slot.Accept.Type)
		assert.Equal(t, ToPtr(SpxSpriteResourceContextURI), slot.Accept.ResourceContext)
		assert.Contains(t, slot.PredefinedNames, "target")
		assert.Contains(t, slot.PredefinedNames, "OtherSprite")
		assert.Equal(t, Range{
			Start: Position{Line: 5, Character: 17},
			End:   Position{Line: 5, Character: 23},
		}, slot.Range)
	})

	t.Run("KwargSpriteInstanceValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
	Target Sprite
}

func configure(opts Options?) {}

onStart => {
	configure target = OtherSprite
}
`),
			"MySprite.spx":                          []byte(``),
			"OtherSprite.spx":                       []byte(``),
			"assets/index.json":                     []byte(`{}`),
			"assets/sprites/MySprite/index.json":    []byte(`{}`),
			"assets/sprites/OtherSprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}}
		inputSlots, err := s.xgoGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)

		slot := findInputSlot(
			inputSlots,
			SpxResourceURI("spx://resources/sprites/OtherSprite"),
			"",
			SpxInputTypeSpriteInstance,
			SpxInputKindInPlace,
		)
		require.NotNil(t, slot)
		assert.Equal(t, SpxInputSlotKindValue, slot.Kind)
		assert.Equal(t, SpxInputTypeSpriteInstance, slot.Accept.Type)
		assert.Equal(t, ToPtr(SpxSpriteResourceContextURI), slot.Accept.ResourceContext)
		assert.Contains(t, slot.PredefinedNames, "OtherSprite")
	})

	t.Run("MissingProjectFile", func(t *testing.T) {
		files := map[string][]byte{"Worker.spx": []byte("println 5\n")}
		s := newSpxTestServer(t, files)
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///Worker.spx"}}})
		require.NoError(t, err)
		assert.Nil(t, slots)
	})

	t.Run("IncompleteColorCall", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx":          []byte("println HSB(1, 2, 3"),
			"assets/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, files)
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}})
		require.NoError(t, err)
		require.Len(t, slots, 3)
		for _, slot := range slots {
			assert.Less(t, comparePositions(slot.Range.Start, slot.Range.End), 0)
		}
	})

	t.Run("ListValues", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx":          []byte(`var values List = NewList(1 + 2, "value")`),
			"assets/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, files)
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}})
		require.NoError(t, err)
		require.Len(t, slots, 3)
		assert.Equal(t, int64(1), slots[0].Input.Value)
		assert.Equal(t, int64(2), slots[1].Input.Value)
		assert.Equal(t, "value", slots[2].Input.Value)
		assert.Equal(t, XGoInputTypeUnknown, slots[2].Accept.Type)
	})

	t.Run("LocalSpriteVariable", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx": []byte(`onStart => {
	var target Sprite
	MySprite.stepTo target
}
`),
			"MySprite.spx":                       nil,
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, files)
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}})
		require.NoError(t, err)
		slot := findInputSlot(slots, nil, "target", SpxInputTypeSpriteInstance, XGoInputKindPredefined)
		require.NotNil(t, slot)
		assert.Equal(t, ToPtr(SpxSpriteResourceContextURI), slot.Accept.ResourceContext)
		assert.Equal(t, "target", slot.Input.Name)
		assert.Contains(t, slot.PredefinedNames, "target")
	})

}

func TestFindInputSlotsSpx(t *testing.T) {
	m := map[string][]byte{
		"main.spx": []byte(`
onStart => {
	direction := Left
	println LeftRight
	myColor := HSB(255, 0, 0)
	MySprite.stepTo "OtherSprite"
	MySprite.stepTo OtherSprite
	MySprite.turn MySprite.heading
	getWidget Monitor, "myWidget"
}
`),
		"MySprite.spx": []byte(`
onStart => {
	name := "OtherSprite"
	stepTo name
	data := "data"
	clone data
}
`),
		"OtherSprite.spx":                       []byte(``),
		"assets/index.json":                     []byte(`{"zorder":[{"name":"myWidget"}]}`),
		"assets/sprites/MySprite/index.json":    []byte(`{}`),
		"assets/sprites/OtherSprite/index.json": []byte(`{}`),
	}
	s := newSpxTestServer(t, m)

	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
	require.NoError(t, err)
	require.False(t, result.hasErrorSeverityDiagnostic)
	require.NotNil(t, astFile)

	inputSlots := findInputSlots(newSpxInputSlotContext(t, result, astFile))
	require.NotNil(t, inputSlots)
	assert.NotEmpty(t, inputSlots)

	t.Run("ValueSlots", func(t *testing.T) {
		for _, tt := range []struct {
			name                     string
			value                    any
			wantAcceptType           SpxInputType
			wantInputType            SpxInputType
			wantEmptyPredefinedNames bool
		}{
			{
				name:           "RotationStyle",
				value:          "LeftRight",
				wantAcceptType: SpxInputTypeUnknown,
				wantInputType:  SpxInputTypeRotationStyle,
			},
			{
				name:           "Direction",
				value:          float64(-90),
				wantAcceptType: SpxInputTypeDecimal,
				wantInputType:  SpxInputTypeDirection,
			},
			{
				name:           "SpxResourceName",
				value:          SpxResourceURI("spx://resources/sprites/OtherSprite"),
				wantAcceptType: SpxInputTypeResourceName,
				wantInputType:  SpxInputTypeResourceName,
			},
			{
				name:           "SpxSpriteInstance",
				value:          SpxResourceURI("spx://resources/sprites/OtherSprite"),
				wantAcceptType: SpxInputTypeSpriteInstance,
				wantInputType:  SpxInputTypeSpriteInstance,
			},
			{
				name: "ColorHSB",
				value: SpxColorInputValue{
					Constructor: SpxInputTypeSpxColorConstructorHSB,
					Args:        []float64{255, 0, 0},
				},
				wantAcceptType:           SpxInputTypeColor,
				wantInputType:            SpxInputTypeColor,
				wantEmptyPredefinedNames: true,
			},
			{
				name:           "WidgetName",
				value:          SpxResourceURI("spx://resources/widgets/myWidget"),
				wantAcceptType: SpxInputTypeResourceName,
				wantInputType:  SpxInputTypeResourceName,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				slot := findInputSlot(inputSlots, tt.value, "", tt.wantInputType, SpxInputKindInPlace)
				require.NotNil(t, slot)
				assert.Equal(t, SpxInputSlotKindValue, slot.Kind)
				assert.Equal(t, tt.wantAcceptType, slot.Accept.Type)
				assert.Equal(t, SpxInputKindInPlace, slot.Input.Kind)
				assert.Equal(t, tt.wantInputType, slot.Input.Type)
				assert.Equal(t, tt.value, slot.Input.Value)
				if tt.wantEmptyPredefinedNames {
					assert.Empty(t, slot.PredefinedNames)
				} else {
					assert.NotEmpty(t, slot.PredefinedNames)
				}
				assert.NotEmpty(t, slot.Range)
			})
		}
	})

	t.Run("SpxSpriteInstanceContext", func(t *testing.T) {
		slot := findInputSlot(
			inputSlots,
			SpxResourceURI("spx://resources/sprites/OtherSprite"),
			"",
			SpxInputTypeSpriteInstance,
			SpxInputKindInPlace,
		)
		require.NotNil(t, slot)
		assert.Equal(t, ToPtr(SpxSpriteResourceContextURI), slot.Accept.ResourceContext)
		assert.Contains(t, slot.PredefinedNames, "OtherSprite")
	})

	t.Run("SpxSpriteStepTo", func(t *testing.T) {
		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///MySprite.spx")
		require.NoError(t, err)
		require.False(t, result.hasErrorSeverityDiagnostic)
		require.NotNil(t, astFile)

		inputSlots := findInputSlots(newSpxInputSlotContext(t, result, astFile))
		require.NotNil(t, inputSlots)
		assert.NotEmpty(t, inputSlots)

		slot := findInputSlot(inputSlots, nil, "name", SpxInputTypeString, SpxInputKindPredefined)
		require.NotNil(t, slot)
		assert.Equal(t, SpxInputTypeResourceName, slot.Accept.Type)
		assert.Equal(t, SpxInputKindPredefined, slot.Input.Kind)
		assert.Equal(t, SpxInputTypeString, slot.Input.Type)
		assert.Equal(t, "name", slot.Input.Name)
		assert.Contains(t, slot.PredefinedNames, "backdropName")
		assert.Equal(t, slot.Range, Range{
			Start: Position{Line: 3, Character: 8},
			End:   Position{Line: 3, Character: 12},
		})
	})

	t.Run("SpxSpriteClone", func(t *testing.T) {
		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///MySprite.spx")
		require.NoError(t, err)
		require.False(t, result.hasErrorSeverityDiagnostic)
		require.NotNil(t, astFile)

		inputSlots := findInputSlots(newSpxInputSlotContext(t, result, astFile))
		require.NotNil(t, inputSlots)
		assert.NotEmpty(t, inputSlots)

		slot := findInputSlot(inputSlots, nil, "data", SpxInputTypeString, SpxInputKindPredefined)
		require.NotNil(t, slot)
		assert.Equal(t, SpxInputTypeUnknown, slot.Accept.Type)
		assert.Equal(t, SpxInputKindPredefined, slot.Input.Kind)
		assert.Equal(t, SpxInputTypeString, slot.Input.Type)
		assert.Equal(t, "data", slot.Input.Name)
		assert.Contains(t, slot.PredefinedNames, "backdropName")
		assert.Equal(t, slot.Range, Range{
			Start: Position{Line: 5, Character: 7},
			End:   Position{Line: 5, Character: 11},
		})
	})

}

func TestCreateValueInputSlotFromBasicLitSpx(t *testing.T) {
	files := map[string][]byte{
		"main.spx":                              []byte(`MySprite.stepTo "OtherSprite"`),
		"MySprite.spx":                          nil,
		"OtherSprite.spx":                       nil,
		"assets/index.json":                     []byte(`{}`),
		"assets/sprites/MySprite/index.json":    []byte(`{}`),
		"assets/sprites/OtherSprite/index.json": []byte(`{}`),
	}
	s := newSpxTestServer(t, files)
	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
	require.NoError(t, err)
	require.False(t, result.hasErrorSeverityDiagnostic)
	require.NotNil(t, astFile)
	ctx := newSpxInputSlotContext(t, result, astFile)
	literal := inputSlotLiteral(t, ctx, `"OtherSprite"`)
	slot := createValueInputSlotFromBasicLit(ctx, literal, GetSpxSpriteNameType())
	require.NotNil(t, slot)
	assert.Equal(t, XGoInputSlotKindValue, slot.Kind)
	assert.Equal(t, SpxInputTypeResourceName, slot.Accept.Type)
	assert.Equal(t, ToPtr(SpxSpriteResourceContextURI), slot.Accept.ResourceContext)
	assert.Equal(t, XGoInputKindInPlace, slot.Input.Kind)
	assert.Equal(t, SpxInputTypeResourceName, slot.Input.Type)
	assert.Equal(t, SpxResourceURI("spx://resources/sprites/OtherSprite"), slot.Input.Value)
	assert.Equal(t, Range{Start: Position{Line: 0, Character: 16}, End: Position{Line: 0, Character: 29}}, slot.Range)
}

func TestCreateValueInputSlotFromIdentSpx(t *testing.T) {
	for _, tt := range []struct {
		name        string
		declaration string
		expression  string
		inputType   XGoInputType
		want        any
	}{
		{"Direction", "", "Left", XGoInputTypeSpxDirection, float64(-90)},
		{"LayerAction", "", "Front", XGoInputTypeSpxLayerAction, "Front"},
		{"DirAction", "", "Forward", XGoInputTypeSpxDirAction, "Forward"},
		{"SpecialObject", "", "Mouse", XGoInputTypeSpxSpecialObj, "Mouse"},
		{"EffectKind", "", "ColorEffect", XGoInputTypeSpxEffectKind, "ColorEffect"},
		{"Key", "", "KeySpace", XGoInputTypeSpxKey, "KeySpace"},
		{"RotationStyle", "", "LeftRight", XGoInputTypeSpxRotationStyle, "LeftRight"},
		{"SpecialObjectVariable", "var custom = Mouse\n", "custom", XGoInputTypeSpxSpecialObj, nil},
		{"UserConstant", "const custom Key = KeySpace\n", "custom", XGoInputTypeSpxKey, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := tt.declaration + "println " + tt.expression + "\n"
			s := newSpxTestServer(t, map[string][]byte{"main.spx": []byte(source), "assets/index.json": []byte(`{}`)})
			result, _, file, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
			require.NoError(t, err)
			require.False(t, result.hasErrorSeverityDiagnostic)
			ctx := newSpxInputSlotContext(t, result, file)
			call := inputSlotCall(t, ctx, "println")
			require.Len(t, call.Args, 1)
			ident := requireValueAs[*ast.Ident](t, call.Args[0])
			slot := createValueInputSlotFromIdent(ctx, ident, nil)
			require.NotNil(t, slot)
			want := XGoInput{Kind: XGoInputKindInPlace, Type: tt.inputType, Value: tt.want}
			if tt.want == nil {
				want.Kind = XGoInputKindPredefined
				want.Name = tt.expression
			}
			assert.Equal(t, want, slot.Input)
			assert.Equal(t, XGoInputSlotAccept{Type: tt.inputType}, slot.Accept)
			assert.Equal(t, XGoInputSlotKindValue, slot.Kind)
			start := PositionOffset([]byte(source), slot.Range.Start)
			end := PositionOffset([]byte(source), slot.Range.End)
			assert.Equal(t, tt.expression, source[start:end])
		})
	}

	t.Run("AliasDeclaredType", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx":                      []byte("type SoundAlias = SoundName\nconst sound = \"Beep\"\nplay sound\n"),
			"assets/index.json":             []byte(`{}`),
			"assets/sounds/Beep/index.json": []byte(`{}`),
		})
		result, _, file, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.False(t, result.hasErrorSeverityDiagnostic)
		ctx := newSpxInputSlotContext(t, result, file)
		call := inputSlotCall(t, ctx, "play")
		require.Len(t, call.Args, 1)
		ident := requireValueAs[*ast.Ident](t, call.Args[0])
		target := ctx.typeInfo.Pkg.Scope().Lookup("SoundAlias")
		require.NotNil(t, target)
		slot := createValueInputSlotFromIdent(ctx, ident, target.Type())
		require.NotNil(t, slot)
		assert.Equal(t, XGoInputSlotAccept{Type: XGoInputTypeSpxResourceName, ResourceContext: ToPtr(SpxSoundResourceContextURI)}, slot.Accept)
		assert.Equal(t, XGoInput{Kind: XGoInputKindPredefined, Type: XGoInputTypeString, Name: "sound"}, slot.Input)
	})
}

func TestCreateValueInputSlotFromColorFuncCall(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		callee string
		want   *XGoInputSpxColorValue
	}{
		{"HSB", "println HSB(12, 34, 56)\n", "HSB", &XGoInputSpxColorValue{Constructor: XGoInputTypeSpxColorConstructorHSB, Args: []float64{12, 34, 56}}},
		{"HSBA", "println HSBA(12, 34, 56, 78)\n", "HSBA", &XGoInputSpxColorValue{Constructor: XGoInputTypeSpxColorConstructorHSBA, Args: []float64{12, 34, 56, 78}}},
		{"Qualified", "import sdk \"github.com/goplus/spx/v3\"\nprintln sdk.HSB(12, 34, 56)\n", "HSB", &XGoInputSpxColorValue{Constructor: XGoInputTypeSpxColorConstructorHSB, Args: []float64{12, 34, 56}}},
		{"LocalFunction", "func HSB(h, s, b float64) int { return 0 }\nprintln HSB(12, 34, 56)\n", "HSB", nil},
		{"OrdinaryFunction", "println 12, 34, 56\n", "println", nil},
		{"UnknownFunction", "println unknown(12, 34, 56)\n", "unknown", nil},
		{"TooFewArguments", "println HSB(12, 34)\n", "HSB", nil},
		{"TooManyArguments", "println HSB(12, 34, 56, 78)\n", "HSB", nil},
		{"QualifiedExtraArguments", "import sdk \"github.com/goplus/spx/v3\"\nprintln sdk.HSB(12, 34, 56, 78)\n", "HSB", nil},
		{"ExtraExpression", "println HSB(12, 34, 56, rand(100))\n", "HSB", nil},
		{"Kwargs", "println HSB(12, 34, 56, extra=78)\n", "HSB", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newSpxTestServer(t, map[string][]byte{"main.spx": []byte(tt.source), "assets/index.json": []byte(`{}`)})
			ctx := inputSlotTestContext(t, s, "main.spx")
			ctx.spxResult = newCompileResult(s.getProj(), s.lookupPkgDoc)
			call := inputSlotCall(t, ctx, tt.callee)
			slot := createValueInputSlotFromColorFuncCall(ctx, call, nil)
			if tt.want == nil {
				assert.Nil(t, slot)
				return
			}
			require.NotNil(t, slot)
			assert.Equal(t, XGoInput{Kind: XGoInputKindInPlace, Type: XGoInputTypeSpxColor, Value: *tt.want}, slot.Input)
			assert.Equal(t, XGoInputSlotAccept{Type: XGoInputTypeSpxColor}, slot.Accept)
		})
	}

}

func TestInferSpxInputTypeFromType(t *testing.T) {

	t.Run("SpxAliasTypes", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			typeGetter func() *gotypes.Alias
			want       SpxInputType
		}{
			{"BackdropName", GetSpxBackdropNameType, SpxInputTypeResourceName},
			{"SoundName", GetSpxSoundNameType, SpxInputTypeResourceName},
			{"SpriteName", GetSpxSpriteNameType, SpxInputTypeResourceName},
			{"SpriteCostumeName", GetSpxSpriteCostumeNameType, SpxInputTypeResourceName},
			{"SpriteAnimationName", GetSpxSpriteAnimationNameType, SpxInputTypeResourceName},
			{"WidgetName", GetSpxWidgetNameType, SpxInputTypeResourceName},
			{"SpecialDir", GetSpxDirectionType, SpxInputTypeDirection},
			{"Key", GetSpxKeyType, SpxInputTypeKey},
			{"PropertyName", GetSpxPropertyNameType, SpxInputTypePropertyName},
		} {
			t.Run(tt.name, func(t *testing.T) {
				got := inferSpxInputTypeFromType(tt.typeGetter())
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("SpxNamedTypes", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			typeGetter func() *gotypes.Named
			want       SpxInputType
		}{
			{"EffectKind", GetSpxEffectKindType, SpxInputTypeEffectKind},
			{"SpecialObj", GetSpxSpecialObjType, SpxInputTypeSpecialObj},
		} {
			t.Run(tt.name, func(t *testing.T) {
				got := inferSpxInputTypeFromType(tt.typeGetter())
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("AliasFallback", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/pkg", "pkg")
		for _, tt := range []struct {
			name string
			typ  gotypes.Type
			want SpxInputType
		}{
			{
				name: "AliasToSpxResourceName",
				typ:  gotypes.NewAlias(gotypes.NewTypeName(0, pkg, "MySoundName", nil), GetSpxSoundNameType()),
				want: SpxInputTypeResourceName,
			},
			{
				name: "AliasToSpxDirection",
				typ:  gotypes.NewAlias(gotypes.NewTypeName(0, pkg, "MyDirection", nil), GetSpxDirectionType()),
				want: SpxInputTypeDirection,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				got := inferSpxInputTypeFromType(tt.typ)
				assert.Equal(t, tt.want, got)
			})
		}
	})
}

func newSpxInputSlotContext(t *testing.T, result *compileResult, astFile *ast.File) *inputSlotContext {
	t.Helper()

	ctx := newInputSlotContext(result.proj, astFile)
	ctx.spxResult = result
	return ctx
}
