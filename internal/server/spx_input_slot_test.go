//go:build !test_no_pkgdata

package server

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerSpxGetInputSlots(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	// Literals of different types.
	count := 5
	message := "Hello"
	isVisible := true
	direction := Left
	layerAction := Front
	dirAction := Forward

	// Function calls with different types.
	println 42, 3.14, "text"
	myColor := HSB(255, 0, 0)
	otherColor := HSBA(0, 255, 0, 128)

	// Conditions and calculations.
	if count > 3 && isVisible {
		println "Count is greater than 3 and is visible"
	}

	// Spx resource name.
	MySprite.stepTo "OtherSprite"
	MySprite.stepTo OtherSprite
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
		assert.Greater(t, len(inputSlots), 10)

		t.Run("InPlaceValues", func(t *testing.T) {
			for _, tt := range []struct {
				name                     string
				value                    any
				acceptType               SpxInputType
				inputType                SpxInputType
				inputKind                SpxInputKind
				shouldExist              bool
				wantEmptyPredefinedNames bool
			}{
				{
					name:        "String",
					value:       "Hello",
					acceptType:  SpxInputTypeString,
					inputType:   SpxInputTypeString,
					inputKind:   SpxInputKindInPlace,
					shouldExist: true,
				},
				{
					name:        "Integer",
					value:       int64(5),
					acceptType:  SpxInputTypeInteger,
					inputType:   SpxInputTypeInteger,
					inputKind:   SpxInputKindInPlace,
					shouldExist: true,
				},
				{
					name:        "Boolean",
					value:       true,
					acceptType:  SpxInputTypeBoolean,
					inputType:   SpxInputTypeBoolean,
					inputKind:   SpxInputKindInPlace,
					shouldExist: true,
				},
				{
					name:        "Direction",
					value:       float64(-90),
					acceptType:  SpxInputTypeDecimal,
					inputType:   SpxInputTypeDirection,
					inputKind:   SpxInputKindInPlace,
					shouldExist: true,
				},
				{
					name:                     "LayerAction",
					value:                    "Front",
					acceptType:               SpxInputTypeLayerAction,
					inputType:                SpxInputTypeLayerAction,
					inputKind:                SpxInputKindInPlace,
					shouldExist:              true,
					wantEmptyPredefinedNames: true,
				},
				{
					name:                     "DirAction",
					value:                    "Forward",
					acceptType:               SpxInputTypeDirAction,
					inputType:                SpxInputTypeDirAction,
					inputKind:                SpxInputKindInPlace,
					shouldExist:              true,
					wantEmptyPredefinedNames: true,
				},
				{
					name: "HSB",
					value: SpxColorInputValue{
						Constructor: SpxInputTypeSpxColorConstructorHSB,
						Args:        []float64{255, 0, 0},
					},
					acceptType:               SpxInputTypeColor,
					inputType:                SpxInputTypeColor,
					inputKind:                SpxInputKindInPlace,
					shouldExist:              true,
					wantEmptyPredefinedNames: true,
				},
				{
					name: "HSBA",
					value: SpxColorInputValue{
						Constructor: SpxInputTypeSpxColorConstructorHSBA,
						Args:        []float64{0, 255, 0, 128},
					},
					acceptType:  SpxInputTypeColor,
					inputType:   SpxInputTypeColor,
					inputKind:   SpxInputKindInPlace,
					shouldExist: true,
				},
				{
					name:        "SpxResourceName",
					value:       SpxResourceURI("spx://resources/sprites/OtherSprite"),
					acceptType:  SpxInputTypeResourceName,
					inputType:   SpxInputTypeResourceName,
					inputKind:   SpxInputKindInPlace,
					shouldExist: true,
				},
				{
					name:        "SpxSpriteInstance",
					value:       SpxResourceURI("spx://resources/sprites/OtherSprite"),
					acceptType:  SpxInputTypeSpriteInstance,
					inputType:   SpxInputTypeSpriteInstance,
					inputKind:   SpxInputKindInPlace,
					shouldExist: true,
				},
				{
					name:        "NonExistentValue",
					value:       int64(999),
					acceptType:  SpxInputTypeInteger,
					inputType:   SpxInputTypeInteger,
					inputKind:   SpxInputKindInPlace,
					shouldExist: false,
				},
			} {
				t.Run(tt.name, func(t *testing.T) {
					slot := findInputSlot(inputSlots, tt.value, "", tt.inputType, tt.inputKind)
					if !tt.shouldExist {
						assert.Nil(t, slot)
						return
					}
					require.NotNil(t, slot)
					assert.Equal(t, SpxInputSlotKindValue, slot.Kind)
					assert.Equal(t, tt.acceptType, slot.Accept.Type)
					assert.Equal(t, tt.inputKind, slot.Input.Kind)
					assert.Equal(t, tt.inputType, slot.Input.Type)
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

		t.Run("PredefinedValues", func(t *testing.T) {
			for _, tt := range []struct {
				name        string
				inputName   string
				inputType   SpxInputType
				inputKind   SpxInputKind
				shouldExist bool
			}{
				{"Variable", "count", SpxInputTypeUnknown, SpxInputKindPredefined, true},
				{"NonExistentName", "nonExistent", SpxInputTypeUnknown, SpxInputKindPredefined, false},
			} {
				t.Run(tt.name, func(t *testing.T) {
					slot := findInputSlot(inputSlots, nil, tt.inputName, tt.inputType, tt.inputKind)
					if !tt.shouldExist {
						assert.Nil(t, slot)
						return
					}
					require.NotNil(t, slot)
					assert.Equal(t, tt.inputType, slot.Accept.Type)
					assert.Equal(t, tt.inputKind, slot.Input.Kind)
					assert.Equal(t, tt.inputType, slot.Input.Type)
					assert.Equal(t, tt.inputName, slot.Input.Name)
					assert.NotEmpty(t, slot.PredefinedNames)
					assert.NotEmpty(t, slot.Range)
				})
			}
		})

		t.Run("AddressSlots", func(t *testing.T) {
			for _, tt := range []struct {
				name        string
				inputName   string
				shouldExist bool
			}{
				{"CountVariable", "count", true},
				{"NonExistentVariable", "nonExistent", false},
			} {
				t.Run(tt.name, func(t *testing.T) {
					slot := findAddressInputSlot(inputSlots, tt.inputName)
					if !tt.shouldExist {
						assert.Nil(t, slot)
						return
					}
					require.NotNil(t, slot)
					assert.Equal(t, SpxInputSlotKindAddress, slot.Kind)
					assert.Equal(t, SpxInputTypeUnknown, slot.Accept.Type)
					assert.Equal(t, SpxInputKindPredefined, slot.Input.Kind)
					assert.Equal(t, SpxInputTypeUnknown, slot.Input.Type)
					assert.Equal(t, tt.inputName, slot.Input.Name)
					assert.NotEmpty(t, slot.PredefinedNames)
					assert.NotEmpty(t, slot.Range)
				})
			}
		})
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
	// Initialize variables
	count := 5
	message := "Hello"
	isVisible := true
	direction := Left

	// CallExpr with various arg types
	println 42, 3.14, "text", true, Left, LeftRight

	// BinaryExpr
	sum := 10 + 20
	isEqual := count == 5

	// UnaryExpr
	notTrue := !isVisible

	// AssignStmt
	count = 10
	myColor := HSB(255, 0, 0)

	// IfStmt
	if count > 3 {
		println "Greater than 3"
	}

	// ForStmt
	for i := 0; i < 5; i++ {
		println i
	}

	// ReturnStmt in a function
	calculateValue := func() int {
		return 100
	}

	// SwitchStmt and CaseClause
	switch direction {
	case Left:
		println "Going left"
	case Right:
		println "Going right"
	default:
		println "Other direction"
	}

	// RangeStmt
	numbers := []int{1, 2, 3}
	for index, value := range numbers {
		println index, value
	}

	// IncDecStmt
	count++

	// Spx resource name
	MySprite.stepTo "OtherSprite"
	MySprite.stepTo OtherSprite

	// Other commands
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

func TestCheckValueInputSlotSpx(t *testing.T) {
	m := map[string][]byte{
		"main.spx": []byte(`
onStart => {
	// Basic literals.
	numValue := 42
	floatValue := 3.14
	strValue := "hello"

	// Identifiers.
	dirValue := Left
	boolValue := true

	// Color function calls.
	colorValue := HSB(255, 0, 0)

	// Other expressions.
	arrayValue := []int{1, 2, 3}
}
`),
		"assets/index.json": []byte(`{}`),
	}
	s := newSpxTestServer(t, m)

	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
	require.NoError(t, err)
	require.False(t, result.hasErrorSeverityDiagnostic)
	require.NotNil(t, astFile)
	ctx := newSpxInputSlotContext(t, result, astFile)

	for _, tt := range []struct {
		name           string
		exprPosition   Position
		exprFilter     func(ast.Node) bool
		wantNil        bool
		wantKind       SpxInputSlotKind
		wantAcceptType SpxInputType
		wantInputKind  SpxInputKind
		wantInputType  SpxInputType
		wantInputValue any
		wantInputName  string
	}{
		{
			name:           "DirectionIdentifier",
			exprPosition:   Position{Line: 8, Character: 14},
			exprFilter:     func(node ast.Node) bool { _, ok := node.(*ast.Ident); return ok },
			wantKind:       SpxInputSlotKindValue,
			wantAcceptType: SpxInputTypeDirection,
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeDirection,
			wantInputValue: float64(-90),
		},
		{
			name:           "ColorFunctionCall",
			exprPosition:   Position{Line: 12, Character: 16},
			exprFilter:     func(node ast.Node) bool { _, ok := node.(*ast.CallExpr); return ok },
			wantKind:       SpxInputSlotKindValue,
			wantAcceptType: SpxInputTypeColor,
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeColor,
			wantInputValue: SpxColorInputValue{
				Constructor: SpxInputTypeSpxColorConstructorHSB,
				Args:        []float64{255, 0, 0},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pos := PosAt(result.proj, astFile, tt.exprPosition)
			require.True(t, pos.IsValid())

			var expr ast.Expr
			for node := range xgoutil.PathEnclosingIntervalNodes(astFile, pos, pos, false) {
				if node, ok := node.(ast.Expr); ok && tt.exprFilter(node) {
					expr = node
					break
				}
			}
			require.NotNil(t, expr)

			got := checkValueInputSlot(ctx, expr, nil)
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, tt.wantKind, got.Kind)
				assert.Equal(t, tt.wantAcceptType, got.Accept.Type)
				assert.Equal(t, tt.wantInputKind, got.Input.Kind)
				assert.Equal(t, tt.wantInputType, got.Input.Type)
				assert.Equal(t, tt.wantInputValue, got.Input.Value)
				assert.Equal(t, tt.wantInputName, got.Input.Name)
				assert.NotEmpty(t, got.Range)
			}
		})
	}
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
	m := map[string][]byte{
		"main.spx": []byte(`
var (
	regularVar int
)

onStart => {
	// Boolean
	boolVar := true

	// Direction
	MySprite.turn Left

	// Special object
	if MySprite.touching(Mouse) {}

	// Special object (variable)
	myMouse := Mouse
	if MySprite.touching(myMouse) {}

	// Effect kind
	setGraphicEffect ColorEffect, 0

	// Key
	if keyPressed(KeySpace) {}

	// Regular
	myVar := regularVar
}
`),
		"MySprite.spx":                       []byte(``),
		"assets/index.json":                  []byte(`{}`),
		"assets/sprites/MySprite/index.json": []byte(`{}`),
		"assets/sounds/MySound/index.json":   []byte(`{}`),
	}
	s := newSpxTestServer(t, m)

	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
	require.NoError(t, err)
	require.False(t, result.hasErrorSeverityDiagnostic)
	require.NotNil(t, astFile)
	ctx := newSpxInputSlotContext(t, result, astFile)

	for _, tt := range []struct {
		name           string
		identPosition  Position
		wantInputKind  SpxInputKind
		wantInputType  SpxInputType
		wantInputValue any
		wantInputName  string
		wantBoolValue  *bool
	}{
		{
			name:           "Direction",
			identPosition:  Position{Line: 10, Character: 16},
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeDirection,
			wantInputValue: float64(-90),
		},
		{
			name:           "SpecialObject",
			identPosition:  Position{Line: 13, Character: 23},
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeSpecialObj,
			wantInputValue: "Mouse",
		},
		{
			name:          "SpecialObjectVariable",
			identPosition: Position{Line: 17, Character: 23},
			wantInputKind: SpxInputKindPredefined,
			wantInputType: SpxInputTypeSpecialObj,
			wantInputName: "myMouse",
		},
		{
			name:           "EffectKind",
			identPosition:  Position{Line: 20, Character: 19},
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeEffectKind,
			wantInputValue: "ColorEffect",
		},
		{
			name:           "Key",
			identPosition:  Position{Line: 23, Character: 16},
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeKey,
			wantInputValue: "KeySpace",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pos := PosAt(result.proj, astFile, tt.identPosition)
			require.True(t, pos.IsValid())

			var ident *ast.Ident
			for node := range xgoutil.PathEnclosingIntervalNodes(astFile, pos, pos, false) {
				if node, ok := node.(*ast.Ident); ok {
					ident = node
					break
				}
			}
			require.NotNil(t, ident)

			got := createValueInputSlotFromIdent(ctx, ident, nil)
			require.NotNil(t, got)
			assert.Equal(t, SpxInputSlotKindValue, got.Kind)
			assert.Equal(t, tt.wantInputType, got.Accept.Type)
			assert.Equal(t, tt.wantInputKind, got.Input.Kind)
			assert.Equal(t, tt.wantInputType, got.Input.Type)
			assert.Equal(t, tt.wantInputValue, got.Input.Value)
			assert.Equal(t, tt.wantInputName, got.Input.Name)
			assert.NotEmpty(t, got.Range)
		})
	}

	t.Run("AliasDeclaredType", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
const mySound = "MySound"

onStart => {
	play mySound
}
`),
			"assets/index.json":                []byte(`{}`),
			"assets/sounds/MySound/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.False(t, result.hasErrorSeverityDiagnostic)
		require.NotNil(t, astFile)
		ctx := newSpxInputSlotContext(t, result, astFile)

		pos := PosAt(result.proj, astFile, Position{Line: 4, Character: 7})
		require.True(t, pos.IsValid())

		var ident *ast.Ident
		for node := range xgoutil.PathEnclosingIntervalNodes(astFile, pos, pos, false) {
			if node, ok := node.(*ast.Ident); ok && node.Name == "mySound" {
				ident = node
				break
			}
		}
		require.NotNil(t, ident)

		pkg := gotypes.NewPackage("example.com/pkg", "pkg")
		declaredType := gotypes.NewAlias(gotypes.NewTypeName(0, pkg, "MySoundName", nil), GetSpxSoundNameType())

		got := createValueInputSlotFromIdent(ctx, ident, declaredType)
		require.NotNil(t, got)
		assert.Equal(t, SpxInputTypeResourceName, got.Accept.Type)
		assert.Equal(t, ToPtr(SpxSoundResourceContextURI), got.Accept.ResourceContext)
		assert.Equal(t, SpxInputKindPredefined, got.Input.Kind)
		assert.Equal(t, SpxInputTypeString, got.Input.Type)
		assert.Equal(t, "mySound", got.Input.Name)
	})
}

func TestCreateValueInputSlotFromColorFuncCall(t *testing.T) {
	m := map[string][]byte{
		"main.spx": []byte(`
onStart => {
	// Color functions.
	myColor1 := HSB(255, 0, 0)
	myColor2 := HSBA(255, 0, 0, 128)

	// Non-color function calls.
	println 1, 2, 3
}
`),
		"assets/index.json": []byte(`{}`),
	}
	s := newSpxTestServer(t, m)

	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
	require.NoError(t, err)
	require.False(t, result.hasErrorSeverityDiagnostic)
	require.NotNil(t, astFile)
	ctx := newSpxInputSlotContext(t, result, astFile)

	for _, tt := range []struct {
		name             string
		callExprPosition Position
		wantNil          bool
		wantValue        SpxColorInputValue
	}{
		{
			name:             "HSB",
			callExprPosition: Position{Line: 3, Character: 14},
			wantValue: SpxColorInputValue{
				Constructor: SpxInputTypeSpxColorConstructorHSB,
				Args:        []float64{255, 0, 0},
			},
		},
		{
			name:             "HSBA",
			callExprPosition: Position{Line: 4, Character: 14},
			wantValue: SpxColorInputValue{
				Constructor: SpxInputTypeSpxColorConstructorHSBA,
				Args:        []float64{255, 0, 0, 128},
			},
		},
		{
			name:             "RegularFunction",
			callExprPosition: Position{Line: 7, Character: 2},
			wantNil:          true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pos := PosAt(result.proj, astFile, tt.callExprPosition)
			require.True(t, pos.IsValid())

			var callExpr *ast.CallExpr
			for node := range xgoutil.PathEnclosingIntervalNodes(astFile, pos, pos, false) {
				if node, ok := node.(*ast.CallExpr); ok {
					callExpr = node
					break
				}
			}
			require.NotNil(t, callExpr)

			got := createValueInputSlotFromColorFuncCall(ctx, callExpr, nil)
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, SpxInputSlotKindValue, got.Kind)
				assert.Equal(t, SpxInputTypeColor, got.Accept.Type)
				assert.Equal(t, SpxInputKindInPlace, got.Input.Kind)
				assert.Equal(t, SpxInputTypeColor, got.Input.Type)

				colorValue := requireValueAs[SpxColorInputValue](t, got.Input.Value)
				assert.Equal(t, tt.wantValue.Constructor, colorValue.Constructor)
				assert.ElementsMatch(t, tt.wantValue.Args, colorValue.Args)

				assert.NotEmpty(t, got.Range)
			}
		})
	}

	t.Run("NonIdentifierFunction", func(t *testing.T) {
		callExpr := &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "math"},
				Sel: &ast.Ident{Name: "Max"},
			},
			Args: []ast.Expr{
				&ast.BasicLit{Kind: token.INT, Value: "1"},
				&ast.BasicLit{Kind: token.INT, Value: "2"},
			},
		}
		got := createValueInputSlotFromColorFuncCall(ctx, callExpr, nil)
		assert.Nil(t, got)
	})

	t.Run("NilFunctionType", func(t *testing.T) {
		callExpr := &ast.CallExpr{
			Fun:  &ast.Ident{Name: "unknownFunction"},
			Args: []ast.Expr{&ast.BasicLit{Kind: token.INT, Value: "1"}},
		}
		got := createValueInputSlotFromColorFuncCall(ctx, callExpr, nil)
		assert.Nil(t, got)
	})
}

func TestIsSpxColorFunc(t *testing.T) {
	for _, tt := range []struct {
		name string
		fun  *gotypes.Func
		want bool
	}{
		{"HSB", GetSpxHSBFunc(), true},
		{"HSBA", GetSpxHSBAFunc(), true},
		{"SameNameInOtherPackage", gotypes.NewFunc(token.NoPos,
			gotypes.NewPackage("example.com/colors", "colors"), "HSB",
			gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := isSpxColorFunc(tt.fun)
			assert.Equal(t, tt.want, got)
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

func TestInferSpxSpriteResourceEnclosingNode(t *testing.T) {
	m := map[string][]byte{
		"main.spx": []byte(`
onStart => {
	MySprite.setXYpos 10, 20
}
`),
		"MySprite.spx": []byte(`
onStart => {
	setCostume "costume1"
}
`),
		"DecoratedSprite.spx": []byte(`func withCostume(costume SpriteCostumeName, fn func()) {}

@withCostume("costume1")
func run() {}
`),
		"assets/index.json":                         []byte(`{}`),
		"assets/sprites/MySprite/index.json":        []byte(`{"costumes":[{"name":"costume1"}]}`),
		"assets/sprites/DecoratedSprite/index.json": []byte(`{"costumes":[{"name":"costume1"}]}`),
	}
	s := newSpxTestServer(t, m)

	t.Run("MainFile", func(t *testing.T) {
		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.False(t, result.hasErrorSeverityDiagnostic)
		require.NotNil(t, astFile)

		// MySprite.setXYpos
		pos := PosAt(result.proj, astFile, Position{Line: 2, Character: 11})
		require.True(t, pos.IsValid())

		var callExpr *ast.CallExpr
		for node := range xgoutil.PathEnclosingIntervalNodes(astFile, pos, pos, false) {
			if node, ok := node.(*ast.CallExpr); ok {
				callExpr = node
				break
			}
		}
		require.NotNil(t, callExpr)

		spxSpriteResource := inferSpxSpriteResourceEnclosingNode(result, callExpr)
		require.NotNil(t, spxSpriteResource)
		assert.Equal(t, "MySprite", spxSpriteResource.Name)
	})

	t.Run("SpriteFile", func(t *testing.T) {
		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///MySprite.spx")
		require.NoError(t, err)
		require.False(t, result.hasErrorSeverityDiagnostic)
		require.NotNil(t, astFile)

		// setCostume
		pos := PosAt(result.proj, astFile, Position{Line: 2, Character: 2})
		require.True(t, pos.IsValid())

		var callExpr *ast.CallExpr
		for node := range xgoutil.PathEnclosingIntervalNodes(astFile, pos, pos, false) {
			if node, ok := node.(*ast.CallExpr); ok {
				callExpr = node
				break
			}
		}
		require.NotNil(t, callExpr)

		spxSpriteResource := inferSpxSpriteResourceEnclosingNode(result, callExpr)
		require.NotNil(t, spxSpriteResource)
		assert.Equal(t, "MySprite", spxSpriteResource.Name)
	})

	t.Run("FuncDecorator", func(t *testing.T) {
		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///DecoratedSprite.spx")
		require.NoError(t, err)
		require.False(t, result.hasErrorSeverityDiagnostic)
		require.NotNil(t, astFile)

		var costumeLit *ast.BasicLit
		ast.Inspect(astFile, func(node ast.Node) bool {
			if lit, ok := node.(*ast.BasicLit); ok && lit.Value == `"costume1"` {
				costumeLit = lit
				return false
			}
			return true
		})
		require.NotNil(t, costumeLit)

		spxSpriteResource := inferSpxSpriteResourceEnclosingNode(result, costumeLit)
		require.NotNil(t, spxSpriteResource)
		assert.Equal(t, "DecoratedSprite", spxSpriteResource.Name)
	})

	t.Run("NonSpriteNode", func(t *testing.T) {
		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.False(t, result.hasErrorSeverityDiagnostic)
		require.NotNil(t, astFile)

		// onStart
		pos := PosAt(result.proj, astFile, Position{Line: 1, Character: 2})
		require.True(t, pos.IsValid())

		var callExpr *ast.CallExpr
		for node := range xgoutil.PathEnclosingIntervalNodes(astFile, pos, pos, false) {
			if node, ok := node.(*ast.CallExpr); ok {
				callExpr = node
				break
			}
		}
		require.NotNil(t, callExpr)

		spxSpriteResource := inferSpxSpriteResourceEnclosingNode(result, callExpr)
		require.Nil(t, spxSpriteResource)
	})
}

func newSpxInputSlotContext(t *testing.T, result *compileResult, astFile *ast.File) *inputSlotContext {
	t.Helper()

	ctx := newInputSlotContext(result.proj, astFile)
	ctx.spxResult = result
	return ctx
}
