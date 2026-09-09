package server

import (
	gotypes "go/types"
	"reflect"
	"slices"
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
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}}
		inputSlots, err := s.spxGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)
		assert.Greater(t, len(inputSlots), 10)

		t.Run("InPlaceValues", func(t *testing.T) {
			for _, tt := range []struct {
				name        string
				value       any
				acceptType  SpxInputType
				inputType   SpxInputType
				inputKind   SpxInputKind
				shouldExist bool
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
					name:        "LayerAction",
					value:       "Front",
					acceptType:  SpxInputTypeLayerAction,
					inputType:   SpxInputTypeLayerAction,
					inputKind:   SpxInputKindInPlace,
					shouldExist: true,
				},
				{
					name:        "DirAction",
					value:       "Forward",
					acceptType:  SpxInputTypeDirAction,
					inputType:   SpxInputTypeDirAction,
					inputKind:   SpxInputKindInPlace,
					shouldExist: true,
				},
				{
					name: "HSB",
					value: SpxColorInputValue{
						Constructor: SpxInputTypeSpxColorConstructorHSB,
						Args:        []float64{255, 0, 0},
					},
					acceptType:  SpxInputTypeColor,
					inputType:   SpxInputTypeColor,
					inputKind:   SpxInputKindInPlace,
					shouldExist: true,
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
					if tt.shouldExist {
						require.NotNil(t, slot)
						assert.Equal(t, SpxInputSlotKindValue, slot.Kind)
						assert.Equal(t, tt.acceptType, slot.Accept.Type)
						assert.Equal(t, tt.inputKind, slot.Input.Kind)
						assert.Equal(t, tt.inputType, slot.Input.Type)
						assert.Equal(t, tt.value, slot.Input.Value)
						assert.NotEmpty(t, slot.PredefinedNames)
						assert.NotEmpty(t, slot.Range)
					} else {
						assert.Nil(t, slot)
					}
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
					if tt.shouldExist {
						require.NotNil(t, slot)
						assert.Equal(t, tt.inputType, slot.Accept.Type)
						assert.Equal(t, tt.inputKind, slot.Input.Kind)
						assert.Equal(t, tt.inputType, slot.Input.Type)
						assert.Equal(t, tt.inputName, slot.Input.Name)
						assert.NotEmpty(t, slot.PredefinedNames)
						assert.NotEmpty(t, slot.Range)
					} else {
						assert.Nil(t, slot)
					}
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
					if tt.shouldExist {
						require.NotNil(t, slot)
						assert.Equal(t, SpxInputSlotKindAddress, slot.Kind)
						assert.Equal(t, SpxInputTypeUnknown, slot.Accept.Type)
						assert.Equal(t, SpxInputKindPredefined, slot.Input.Kind)
						assert.Equal(t, SpxInputTypeUnknown, slot.Input.Type)
						assert.Equal(t, tt.inputName, slot.Input.Name)
						assert.NotEmpty(t, slot.PredefinedNames)
						assert.NotEmpty(t, slot.Range)
					} else {
						assert.Nil(t, slot)
					}
				})
			}
		})
	})

	t.Run("FuncDecorator", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`func withCount(count int, fn func()) {}

@withCount(3)
func run() {}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		inputSlots, err := s.spxGetInputSlots([]SpxGetInputSlotsParams{{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}})
		require.NoError(t, err)

		slot := findInputSlot(inputSlots, int64(3), "", SpxInputTypeInteger, SpxInputKindInPlace)
		require.NotNil(t, slot)
		assert.Equal(t, SpxInputTypeInteger, slot.Accept.Type)
		assert.Equal(t, Range{
			Start: Position{Line: 2, Character: 11},
			End:   Position{Line: 2, Character: 12},
		}, slot.Range)
	})

	t.Run("PartialXGoxFunction", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`import "example.com/typeargs"

onStart => {
	println typeargs.convert(string, 100)
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		s.workspaceRootFS.Importer = xgoxTestImporter{fallback: s.workspaceRootFS.Importer}

		inputSlots, err := s.spxGetInputSlots([]SpxGetInputSlotsParams{{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}})
		require.NoError(t, err)

		slot := findInputSlot(inputSlots, int64(100), "", SpxInputTypeInteger, SpxInputKindInPlace)
		require.NotNil(t, slot)
		assert.Equal(t, Range{
			Start: Position{Line: 3, Character: 34},
			End:   Position{Line: 3, Character: 37},
		}, slot.Range)
		for _, slot := range inputSlots {
			assert.NotEqual(t, Range{
				Start: Position{Line: 3, Character: 26},
				End:   Position{Line: 3, Character: 32},
			}, slot.Range)
		}
	})

	t.Run("InvalidSyntax", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
// Missing closing parenthesis.
var (
	count     int
	message   string
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}}
		inputSlots, err := s.spxGetInputSlots(params)
		require.NoError(t, err)
		assert.Nil(t, inputSlots)
	})

	t.Run("EmptyFile", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(``),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}}
		inputSlots, err := s.spxGetInputSlots(params)
		require.NoError(t, err)
		assert.Empty(t, inputSlots)
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		m := map[string][]byte{}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///nonexistent.spx"}}}
		inputSlots, err := s.spxGetInputSlots(params)
		require.Error(t, err)
		assert.Nil(t, inputSlots)
	})

	t.Run("MultipleParams", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`var a = 1`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		params := []SpxGetInputSlotsParams{
			{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}},
			{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}},
		}
		inputSlots, err := s.spxGetInputSlots(params)
		require.Error(t, err)
		assert.Nil(t, inputSlots)
		assert.ErrorContains(t, err, "only supports one document")
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
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		inputSlots, err := s.spxGetInputSlots([]SpxGetInputSlotsParams{
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

	t.Run("EmptyParams", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`var a = 1`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		params := []SpxGetInputSlotsParams{}
		inputSlots, err := s.spxGetInputSlots(params)
		require.NoError(t, err)
		assert.Nil(t, inputSlots)
	})

	t.Run("IncompleteMethodDeclaration", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Foo struct {
    bar string
}

func (Foo) Bar`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}}

		var (
			inputSlots []SpxInputSlot
			err        error
		)
		assert.NotPanics(t, func() {
			inputSlots, err = s.spxGetInputSlots(params)
		})
		require.NoError(t, err)
		assert.Nil(t, inputSlots)
	})

	t.Run("KwargValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

onStart => {
	configure count = 5
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}}
		inputSlots, err := s.spxGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)

		slot := findInputSlot(inputSlots, int64(5), "", SpxInputTypeInteger, SpxInputKindInPlace)
		require.NotNil(t, slot)
		assert.Equal(t, SpxInputSlotKindValue, slot.Kind)
		assert.Equal(t, SpxInputTypeInteger, slot.Accept.Type)
		assert.Equal(t, SpxInputKindInPlace, slot.Input.Kind)
		assert.Equal(t, int64(5), slot.Input.Value)
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
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}}
		inputSlots, err := s.spxGetInputSlots(params)
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

	t.Run("OverloadKwargValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Worker struct{}

type Options struct {
	Count int
}

var worker Worker

func (w *Worker) handleCount(opts Options?) {}

func (Worker).handle = (
	(Worker).handleCount
)

onStart => {
	worker.handle count = 5
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}}
		inputSlots, err := s.spxGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)

		slot := findInputSlot(inputSlots, int64(5), "", SpxInputTypeInteger, SpxInputKindInPlace)
		require.NotNil(t, slot)
		assert.Equal(t, SpxInputSlotKindValue, slot.Kind)
		assert.Equal(t, SpxInputTypeInteger, slot.Accept.Type)
		assert.Equal(t, SpxInputKindInPlace, slot.Input.Kind)
		assert.Equal(t, int64(5), slot.Input.Value)
	})

	t.Run("UnknownKwargValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

onStart => {
	configure count = 5
	configure unknown = 9
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}}
		inputSlots, err := s.spxGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)

		countSlot := findInputSlot(inputSlots, int64(5), "", SpxInputTypeInteger, SpxInputKindInPlace)
		require.NotNil(t, countSlot)
		assert.Equal(t, SpxInputSlotKindValue, countSlot.Kind)

		unknownSlot := findInputSlot(inputSlots, int64(9), "", SpxInputTypeInteger, SpxInputKindInPlace)
		assert.Nil(t, unknownSlot)
	})

	t.Run("XGoUnitValue", func(t *testing.T) {
		s := newXGoUnitTestServer(`import "time"

func wait(d time.Duration) {}

onStart => {
	wait 1m
}
`)

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}}
		inputSlots, err := s.spxGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)

		slot := findInputSlotByRange(inputSlots, Range{
			Start: Position{Line: 5, Character: 6},
			End:   Position{Line: 5, Character: 7},
		})
		require.NotNil(t, slot)
		assert.Equal(t, SpxInputSlotKindValue, slot.Kind)
		assert.Equal(t, SpxInputTypeInteger, slot.Accept.Type)
		assert.Equal(t, SpxInputKindInPlace, slot.Input.Kind)
		assert.Equal(t, SpxInputTypeInteger, slot.Input.Type)
		assert.Equal(t, int64(1), slot.Input.Value)
	})

	t.Run("XGoUnitImportedAliasFallbackValue", func(t *testing.T) {
		s := newXGoUnitTestServer(`import "example.com/unit"

func wait(d unit.Delay) {}

onStart => {
	wait 1ms
}
`)

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}}
		inputSlots, err := s.spxGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)

		slot := findInputSlotByRange(inputSlots, Range{
			Start: Position{Line: 5, Character: 6},
			End:   Position{Line: 5, Character: 7},
		})
		require.NotNil(t, slot)
		assert.Equal(t, SpxInputSlotKindValue, slot.Kind)
		assert.Equal(t, SpxInputTypeInteger, slot.Accept.Type)
		assert.Equal(t, SpxInputKindInPlace, slot.Input.Kind)
		assert.Equal(t, SpxInputTypeInteger, slot.Input.Type)
		assert.Equal(t, int64(1), slot.Input.Value)
	})

	t.Run("XGoUnitInterfaceKwargValue", func(t *testing.T) {
		s := newXGoUnitTestServer(`import "time"

type Params interface {
	Delay(time.Duration) Params
}

type Client struct{}

var c Client

func (c *Client) Params() Params { return nil }
func (c *Client) Run(params Params) {}

onStart => {
	c.Run delay = 1ms
}
`)

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}}
		inputSlots, err := s.spxGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)

		slot := findInputSlotByRange(inputSlots, Range{
			Start: Position{Line: 14, Character: 15},
			End:   Position{Line: 14, Character: 16},
		})
		require.NotNil(t, slot)
		assert.Equal(t, SpxInputSlotKindValue, slot.Kind)
		assert.Equal(t, SpxInputTypeInteger, slot.Accept.Type)
		assert.Equal(t, SpxInputKindInPlace, slot.Input.Kind)
		assert.Equal(t, SpxInputTypeInteger, slot.Input.Type)
		assert.Equal(t, int64(1), slot.Input.Value)
	})

	t.Run("XGoUnitUnsupportedContexts", func(t *testing.T) {
		s := newXGoUnitTestServer(`import "time"

type Options struct {
	Delay time.Duration
}

func waitPtr(d *time.Duration) {}
func configure(opts *Options) {}

func duration() time.Duration {
	return 1m
}

onStart => {
	waitPtr 1m
	configure delay = 1m
	var delay time.Duration = 1m
	delay = 1m
}
`)

		params := []SpxGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}}}
		inputSlots, err := s.spxGetInputSlots(params)
		require.NoError(t, err)

		assert.Nil(t, findInputSlotByRange(inputSlots, Range{
			Start: Position{Line: 10, Character: 8},
			End:   Position{Line: 10, Character: 9},
		}))
		assert.Nil(t, findInputSlotByRange(inputSlots, Range{
			Start: Position{Line: 14, Character: 9},
			End:   Position{Line: 14, Character: 10},
		}))
		assert.Nil(t, findInputSlotByRange(inputSlots, Range{
			Start: Position{Line: 15, Character: 19},
			End:   Position{Line: 15, Character: 20},
		}))
		assert.Nil(t, findInputSlotByRange(inputSlots, Range{
			Start: Position{Line: 16, Character: 27},
			End:   Position{Line: 16, Character: 28},
		}))
		assert.Nil(t, findInputSlotByRange(inputSlots, Range{
			Start: Position{Line: 17, Character: 9},
			End:   Position{Line: 17, Character: 10},
		}))
	})
}

func TestFindInputSlots(t *testing.T) {
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
	s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
	require.NoError(t, err)
	require.False(t, result.hasErrorSeverityDiagnostic)
	require.NotNil(t, astFile)

	inputSlots := findInputSlots(result, astFile)
	require.NotNil(t, inputSlots)
	assert.NotEmpty(t, inputSlots)

	t.Run("ValueSlots", func(t *testing.T) {
		for _, tt := range []struct {
			name           string
			value          any
			wantAcceptType SpxInputType
			wantInputType  SpxInputType
		}{
			{
				name:           "String",
				value:          "text",
				wantAcceptType: SpxInputTypeUnknown,
				wantInputType:  SpxInputTypeString,
			},
			{
				name:           "Integer",
				value:          int64(42),
				wantAcceptType: SpxInputTypeUnknown,
				wantInputType:  SpxInputTypeInteger,
			},
			{
				name:           "Decimal",
				value:          3.14,
				wantAcceptType: SpxInputTypeUnknown,
				wantInputType:  SpxInputTypeDecimal,
			},
			{
				name:           "Boolean",
				value:          true,
				wantAcceptType: SpxInputTypeBoolean,
				wantInputType:  SpxInputTypeBoolean,
			},
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
				name:           "BinaryExprResult",
				value:          int64(10),
				wantAcceptType: SpxInputTypeInteger,
				wantInputType:  SpxInputTypeInteger,
			},
			{
				name: "ColorHSB",
				value: SpxColorInputValue{
					Constructor: SpxInputTypeSpxColorConstructorHSB,
					Args:        []float64{255, 0, 0},
				},
				wantAcceptType: SpxInputTypeColor,
				wantInputType:  SpxInputTypeColor,
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
				assert.NotEmpty(t, slot.PredefinedNames)
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

	t.Run("AddressSlots", func(t *testing.T) {
		for _, tt := range []struct {
			name          string
			wantInputName string
		}{
			{name: "AssignmentTarget", wantInputName: "count"},
			{name: "RangeIndex", wantInputName: "index"},
			{name: "RangeValue", wantInputName: "value"},
			{name: "IncDecTarget", wantInputName: "count"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				slot := findAddressInputSlot(inputSlots, tt.wantInputName)
				require.NotNil(t, slot)
				assert.Equal(t, SpxInputSlotKindAddress, slot.Kind)
				assert.Equal(t, SpxInputTypeUnknown, slot.Accept.Type)
				assert.Equal(t, SpxInputKindPredefined, slot.Input.Kind)
				assert.Equal(t, SpxInputTypeUnknown, slot.Input.Type)
				assert.Equal(t, tt.wantInputName, slot.Input.Name)
				assert.NotEmpty(t, slot.PredefinedNames)
				assert.NotEmpty(t, slot.Range)
			})
		}
	})

	t.Run("PredefinedNameSlots", func(t *testing.T) {
		for _, tt := range []struct {
			name          string
			wantInputName string
		}{
			{name: "Variable", wantInputName: "count"},
			{name: "MessageVar", wantInputName: "message"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				slot := findInputSlot(inputSlots, nil, tt.wantInputName, SpxInputTypeUnknown, SpxInputKindPredefined)
				require.NotNil(t, slot)
				assert.Equal(t, SpxInputTypeUnknown, slot.Accept.Type)
				assert.Equal(t, SpxInputKindPredefined, slot.Input.Kind)
				assert.Equal(t, SpxInputTypeUnknown, slot.Input.Type)
				assert.Equal(t, tt.wantInputName, slot.Input.Name)
				assert.Contains(t, slot.PredefinedNames, "backdropName")
				assert.NotEmpty(t, slot.Range)
			})
		}
	})

	t.Run("SpxSpriteStepTo", func(t *testing.T) {
		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///MySprite.spx")
		require.NoError(t, err)
		require.False(t, result.hasErrorSeverityDiagnostic)
		require.NotNil(t, astFile)

		inputSlots := findInputSlots(result, astFile)
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

		inputSlots := findInputSlots(result, astFile)
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

	t.Run("LargeList", func(t *testing.T) {
		const slotCount = 2_001
		files := largeListProjectFiles(slotCount)
		server := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})

		result, _, astFile, err := server.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		slots := findInputSlots(result, astFile)
		require.Len(t, slots, slotCount)
		for i, slot := range slots {
			assert.Equal(t, SpxInputSlotKindValue, slot.Kind)
			assert.Equal(t, SpxInputTypeUnknown, slot.Accept.Type)
			assert.Equal(t, SpxInputTypeString, slot.Input.Type)
			assert.Equal(t, "value", slot.Input.Value)
			if i > 0 {
				assert.Less(t, slots[i-1].Range.Start.Character, slot.Range.Start.Character)
				assert.False(t, IsRangesOverlap(slots[i-1].Range, slot.Range))
			}
		}
	})

	t.Run("MixedLargeList", func(t *testing.T) {
		const expressionCount = 2_001
		files := mixedListProjectFiles(expressionCount)
		server := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})

		result, _, astFile, err := server.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		slots := findInputSlots(result, astFile)
		require.Len(t, slots, expressionCount*3)
		for i := 1; i < len(slots); i++ {
			assert.LessOrEqual(t, comparePositions(slots[i-1].Range.Start, slots[i].Range.Start), 0)
			assert.False(t, IsRangesOverlap(slots[i-1].Range, slots[i].Range))
		}
	})

	t.Run("DropsDegenerateRanges", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx":          []byte("println HSB(1, 2, 3"),
			"assets/index.json": []byte(`{}`),
		}
		server := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})

		result, _, astFile, err := server.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		slots := findInputSlots(result, astFile)
		require.Len(t, slots, 3)
		for _, slot := range slots {
			assert.Less(t, comparePositions(slot.Range.Start, slot.Range.End), 0)
		}
	})

	t.Run("UTF16Ranges", func(t *testing.T) {
		mainSpx := []byte("println \"\U0001F600\", \"value\"\r\n")
		files := map[string][]byte{
			"main.spx":          mainSpx,
			"assets/index.json": []byte(`{}`),
		}
		server := New(newProjectWithoutModTime(files), nil, fileMapGetter(files), &MockScheduler{})

		result, _, astFile, err := server.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		slots := findInputSlots(result, astFile)
		require.Len(t, slots, 2)
		assert.Equal(t, Range{Start: Position{Line: 0, Character: 8}, End: Position{Line: 0, Character: 12}}, slots[0].Range)
		assert.Equal(t, Range{Start: Position{Line: 0, Character: 14}, End: Position{Line: 0, Character: 21}}, slots[1].Range)
	})
}

func TestNormalizeInputSlots(t *testing.T) {
	slot := func(name string, startLine, startCharacter, endLine, endCharacter uint32) XGoInputSlot {
		return XGoInputSlot{
			Range: Range{
				Start: Position{Line: startLine, Character: startCharacter},
				End:   Position{Line: endLine, Character: endCharacter},
			},
			Input: XGoInput{Name: name},
		}
	}

	for _, tt := range []struct {
		name  string
		slots []XGoInputSlot
		want  []string
	}{
		{
			name: "Empty",
		},
		{
			name:  "Single",
			slots: []XGoInputSlot{slot("only", 0, 1, 0, 2)},
			want:  []string{"only"},
		},
		{
			name:  "SingleEmptyRangeIsDropped",
			slots: []XGoInputSlot{slot("empty", 0, 1, 0, 1)},
		},
		{
			name:  "SingleReversedRangeIsDropped",
			slots: []XGoInputSlot{slot("reversed", 0, 2, 0, 1)},
		},
		{
			name: "AlreadyOrderedDisjoint",
			slots: []XGoInputSlot{
				slot("first", 0, 1, 0, 2),
				slot("second", 0, 3, 0, 4),
			},
			want: []string{"first", "second"},
		},
		{
			name: "ReverseOrderedDisjoint",
			slots: []XGoInputSlot{
				slot("second", 0, 3, 0, 4),
				slot("first", 0, 1, 0, 2),
			},
			want: []string{"first", "second"},
		},
		{
			name: "MultilineOrderUsesLineBeforeCharacter",
			slots: []XGoInputSlot{
				slot("second", 1, 0, 1, 1),
				slot("first", 0, 10, 0, 11),
			},
			want: []string{"first", "second"},
		},
		{
			name: "IdenticalRangesKeepDiscoveryOrder",
			slots: []XGoInputSlot{
				slot("first", 0, 1, 0, 4),
				slot("second", 0, 1, 0, 4),
			},
			want: []string{"first"},
		},
		{
			name: "SameStartKeepsOuterSlot",
			slots: []XGoInputSlot{
				slot("inner", 0, 1, 0, 4),
				slot("outer", 0, 1, 0, 8),
			},
			want: []string{"outer"},
		},
		{
			name: "ContainmentKeepsOuterSlot",
			slots: []XGoInputSlot{
				slot("inner", 0, 3, 0, 5),
				slot("outer", 0, 1, 0, 8),
			},
			want: []string{"outer"},
		},
		{
			name: "CrossingKeepsEarlierSlot",
			slots: []XGoInputSlot{
				slot("later", 0, 3, 0, 8),
				slot("earlier", 0, 1, 0, 5),
			},
			want: []string{"earlier"},
		},
		{
			name: "RejectedCrossingDoesNotHideFollowingSlot",
			slots: []XGoInputSlot{
				slot("following", 0, 5, 0, 6),
				slot("crossing", 0, 2, 0, 8),
				slot("earlier", 0, 0, 0, 4),
			},
			want: []string{"earlier", "following"},
		},
		{
			name: "DegenerateRangesAreDropped",
			slots: []XGoInputSlot{
				slot("later", 0, 5, 0, 6),
				slot("empty", 0, 4, 0, 4),
				slot("reversed", 0, 7, 0, 2),
				slot("earlier", 0, 0, 0, 4),
			},
			want: []string{"earlier", "later"},
		},
		{
			name: "AdjacentRangesAreDisjoint",
			slots: []XGoInputSlot{
				slot("second", 0, 4, 1, 2),
				slot("first", 0, 1, 0, 4),
			},
			want: []string{"first", "second"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeInputSlots(tt.slots)
			var gotNames []string
			for i, slot := range got {
				gotNames = append(gotNames, slot.Input.Name)
				if i > 0 {
					assert.LessOrEqual(t, comparePositions(got[i-1].Range.Start, slot.Range.Start), 0)
					assert.False(t, IsRangesOverlap(got[i-1].Range, slot.Range))
				}
			}
			assert.Equal(t, tt.want, gotNames)
		})
	}

	t.Run("Exhaustive", func(t *testing.T) {
		var ranges []Range
		positions := [...]Position{
			{Line: 0, Character: 0},
			{Line: 0, Character: 1},
			{Line: 0, Character: 2},
			{Line: 1, Character: 0},
			{Line: 1, Character: 1},
		}
		for start, startPosition := range positions[:len(positions)-1] {
			for _, endPosition := range positions[start+1:] {
				ranges = append(ranges, Range{
					Start: startPosition,
					End:   endPosition,
				})
			}
		}

		names := [...]string{"first", "second", "third"}
		for first, firstRange := range ranges {
			for second, secondRange := range ranges {
				for third, thirdRange := range ranges {
					slots := []XGoInputSlot{
						{Range: firstRange, Input: XGoInput{Name: names[0]}},
						{Range: secondRange, Input: XGoInput{Name: names[1]}},
						{Range: thirdRange, Input: XGoInput{Name: names[2]}},
					}

					orderedSlots := slices.Clone(slots)
					slices.SortStableFunc(orderedSlots, compareInputSlotPriority)
					want := make([]XGoInputSlot, 0, len(orderedSlots))
					for _, slot := range orderedSlots {
						if slices.ContainsFunc(want, func(accepted XGoInputSlot) bool {
							return IsRangesOverlap(accepted.Range, slot.Range)
						}) {
							continue
						}
						want = append(want, slot)
					}

					got := normalizeInputSlots(slices.Clone(slots))
					require.Equal(t, want, got, "range indices: %d, %d, %d", first, second, third)
				}
			}
		}
	})
}

func TestCheckValueInputSlot(t *testing.T) {
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
	s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
	require.NoError(t, err)
	require.False(t, result.hasErrorSeverityDiagnostic)
	require.NotNil(t, astFile)
	ctx := newInputSlotContext(result, astFile)

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
			name:           "IntegerLiteral",
			exprPosition:   Position{Line: 3, Character: 14},
			exprFilter:     func(node ast.Node) bool { _, ok := node.(*ast.BasicLit); return ok },
			wantKind:       SpxInputSlotKindValue,
			wantAcceptType: SpxInputTypeInteger,
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeInteger,
			wantInputValue: int64(42),
		},
		{
			name:           "FloatLiteral",
			exprPosition:   Position{Line: 4, Character: 16},
			exprFilter:     func(node ast.Node) bool { _, ok := node.(*ast.BasicLit); return ok },
			wantKind:       SpxInputSlotKindValue,
			wantAcceptType: SpxInputTypeDecimal,
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeDecimal,
			wantInputValue: 3.14,
		},
		{
			name:           "StringLiteral",
			exprPosition:   Position{Line: 5, Character: 14},
			exprFilter:     func(node ast.Node) bool { _, ok := node.(*ast.BasicLit); return ok },
			wantKind:       SpxInputSlotKindValue,
			wantAcceptType: SpxInputTypeString,
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeString,
			wantInputValue: "hello",
		},
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
			name:           "BooleanIdentifier",
			exprPosition:   Position{Line: 9, Character: 15},
			exprFilter:     func(node ast.Node) bool { _, ok := node.(*ast.Ident); return ok },
			wantKind:       SpxInputSlotKindValue,
			wantAcceptType: SpxInputTypeBoolean,
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeBoolean,
			wantInputValue: true,
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
		{
			name:         "NonValueNode",
			exprPosition: Position{Line: 15, Character: 16},
			exprFilter:   func(node ast.Node) bool { _, ok := node.(*ast.CompositeLit); return ok },
			wantNil:      true,
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

func TestCheckAddressInputSlot(t *testing.T) {
	m := map[string][]byte{
		"main.spx": []byte(`
var (
	varA int
)

onStart => {
	varA = 10
	println varA
	otherVar := 20
}
`),
		"assets/index.json": []byte(`{}`),
	}
	s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
	require.NoError(t, err)
	require.False(t, result.hasErrorSeverityDiagnostic)
	require.NotNil(t, astFile)
	ctx := newInputSlotContext(result, astFile)

	for _, tt := range []struct {
		name         string
		exprPosition Position
		exprFilter   func(ast.Node) bool
		wantNil      bool
		wantName     string
	}{
		{
			name:         "ExistingIdentifier",
			exprPosition: Position{Line: 6, Character: 2},
			exprFilter:   func(node ast.Node) bool { _, ok := node.(*ast.Ident); return ok },
			wantName:     "varA",
		},
		{
			name:         "CallExpr",
			exprPosition: Position{Line: 7, Character: 2},
			exprFilter:   func(node ast.Node) bool { _, ok := node.(*ast.CallExpr); return ok },
			wantNil:      true,
		},
		{
			name:         "BasicLit",
			exprPosition: Position{Line: 8, Character: 14},
			exprFilter:   func(node ast.Node) bool { _, ok := node.(*ast.BasicLit); return ok },
			wantNil:      true,
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

			got := checkAddressInputSlot(ctx, expr)
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, SpxInputSlotKindAddress, got.Kind)
				assert.Equal(t, SpxInputTypeUnknown, got.Accept.Type)
				assert.Equal(t, SpxInputKindPredefined, got.Input.Kind)
				assert.Equal(t, SpxInputTypeUnknown, got.Input.Type)
				assert.Equal(t, tt.wantName, got.Input.Name)
				assert.NotEmpty(t, got.Range)
			}
		})
	}
}

func TestCreateValueInputSlotFromBasicLit(t *testing.T) {
	m := map[string][]byte{
		"main.spx": []byte(`
onStart => {
	// Integer literals.
	x := 42
	hexValue := 0xFF

	// Float literals.
	y := 3.14
	scientific := 1.5e2

	// String literals.
	message := "Hello, world!"
	MySprite.stepTo "OtherSprite"
}
`),
		"MySprite.spx":                          []byte(``),
		"OtherSprite.spx":                       []byte(``),
		"assets/index.json":                     []byte(`{}`),
		"assets/sprites/MySprite/index.json":    []byte(`{}`),
		"assets/sprites/OtherSprite/index.json": []byte(`{}`),
	}
	s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
	require.NoError(t, err)
	require.False(t, result.hasErrorSeverityDiagnostic)
	require.NotNil(t, astFile)
	ctx := newInputSlotContext(result, astFile)

	for _, tt := range []struct {
		name           string
		litPosition    Position
		declaredType   gotypes.Type
		wantAcceptType SpxInputType
		wantInputType  SpxInputType
		wantInputKind  SpxInputKind
		wantValue      any
	}{
		{
			name:           "String",
			litPosition:    Position{Line: 11, Character: 13},
			wantAcceptType: SpxInputTypeString,
			wantInputType:  SpxInputTypeString,
			wantInputKind:  SpxInputKindInPlace,
			wantValue:      "Hello, world!",
		},
		{
			name:           "Integer",
			litPosition:    Position{Line: 3, Character: 7},
			wantAcceptType: SpxInputTypeInteger,
			wantInputType:  SpxInputTypeInteger,
			wantInputKind:  SpxInputKindInPlace,
			wantValue:      int64(42),
		},
		{
			name:           "HexInteger",
			litPosition:    Position{Line: 4, Character: 14},
			wantAcceptType: SpxInputTypeInteger,
			wantInputType:  SpxInputTypeInteger,
			wantInputKind:  SpxInputKindInPlace,
			wantValue:      int64(255), // 0xFF = 255
		},
		{
			name:           "Float",
			litPosition:    Position{Line: 7, Character: 7},
			wantAcceptType: SpxInputTypeDecimal,
			wantInputType:  SpxInputTypeDecimal,
			wantInputKind:  SpxInputKindInPlace,
			wantValue:      3.14,
		},
		{
			name:           "ScientificFloat",
			litPosition:    Position{Line: 8, Character: 16},
			wantAcceptType: SpxInputTypeDecimal,
			wantInputType:  SpxInputTypeDecimal,
			wantInputKind:  SpxInputKindInPlace,
			wantValue:      150.0, // 1.5e2 = 150
		},
		{
			name:           "SpxResourceString",
			litPosition:    Position{Line: 12, Character: 18},
			declaredType:   GetSpxSpriteNameType(),
			wantAcceptType: SpxInputTypeResourceName,
			wantInputType:  SpxInputTypeResourceName,
			wantInputKind:  SpxInputKindInPlace,
			wantValue:      SpxResourceURI("spx://resources/sprites/OtherSprite"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pos := PosAt(result.proj, astFile, tt.litPosition)
			require.True(t, pos.IsValid())

			var lit *ast.BasicLit
			for node := range xgoutil.PathEnclosingIntervalNodes(astFile, pos, pos, false) {
				if node, ok := node.(*ast.BasicLit); ok {
					lit = node
					break
				}
			}
			require.NotNil(t, lit)

			got := createValueInputSlotFromBasicLit(ctx, lit, tt.declaredType)
			require.NotNil(t, got)
			assert.Equal(t, SpxInputSlotKindValue, got.Kind)
			assert.Equal(t, tt.wantAcceptType, got.Accept.Type)
			assert.Equal(t, tt.wantInputKind, got.Input.Kind)
			assert.Equal(t, tt.wantInputType, got.Input.Type)
			assert.Equal(t, tt.wantValue, got.Input.Value)
			assert.NotEmpty(t, got.Range)
		})
	}

	t.Run("InvalidIntLiteral", func(t *testing.T) {
		invalidIntLit := &ast.BasicLit{
			Kind:  token.INT,
			Value: "not.a.int",
		}
		got := createValueInputSlotFromBasicLit(ctx, invalidIntLit, nil)
		assert.Nil(t, got)
	})

	t.Run("InvalidFloatLiteral", func(t *testing.T) {
		invalidFloatLit := &ast.BasicLit{
			Kind:  token.FLOAT,
			Value: "not.a.float",
		}
		got := createValueInputSlotFromBasicLit(ctx, invalidFloatLit, nil)
		assert.Nil(t, got)
	})

	t.Run("UnsupportedLiteralKind", func(t *testing.T) {
		unsupportedLit := &ast.BasicLit{
			Kind:  token.CHAR,
			Value: "'c'",
		}
		got := createValueInputSlotFromBasicLit(ctx, unsupportedLit, nil)
		assert.Nil(t, got)
	})

	t.Run("InvalidStringLiteral", func(t *testing.T) {
		invalidStringLit := &ast.BasicLit{
			Kind:  token.STRING,
			Value: "\"unclosed string literal", // Missing ending quote.
		}
		got := createValueInputSlotFromBasicLit(ctx, invalidStringLit, nil)
		assert.Nil(t, got)
	})
}

func TestCreateValueInputSlotFromIdent(t *testing.T) {
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
	s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
	require.NoError(t, err)
	require.False(t, result.hasErrorSeverityDiagnostic)
	require.NotNil(t, astFile)
	ctx := newInputSlotContext(result, astFile)

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
			name:           "Boolean",
			identPosition:  Position{Line: 7, Character: 13},
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeBoolean,
			wantInputValue: true,
		},
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
		{
			name:          "Regular",
			identPosition: Position{Line: 26, Character: 11},
			wantInputKind: SpxInputKindPredefined,
			wantInputType: SpxInputTypeInteger,
			wantInputName: "regularVar",
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
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.NoError(t, err)
		require.False(t, result.hasErrorSeverityDiagnostic)
		require.NotNil(t, astFile)
		ctx := newInputSlotContext(result, astFile)

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

func TestCreateValueInputSlotFromUnaryExpr(t *testing.T) {
	m := map[string][]byte{
		"main.spx": []byte(`
onStart => {
	// Unary minus with integer.
	negInt := -42

	// Unary minus with float.
	negFloat := -3.14

	// Unary plus with integer.
	posInt := +10

	// Bitwise complement with integer.
	complementInt := ^0xFF

	// Logical not with boolean.
	notBool := !true
}
`),
		"assets/index.json": []byte(`{}`),
	}
	s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
	require.NoError(t, err)
	require.False(t, result.hasErrorSeverityDiagnostic)
	require.NotNil(t, astFile)
	ctx := newInputSlotContext(result, astFile)

	for _, tt := range []struct {
		name           string
		exprPosition   Position
		wantKind       SpxInputSlotKind
		wantAcceptType SpxInputType
		wantInputKind  SpxInputKind
		wantInputType  SpxInputType
		wantInputValue any
	}{
		{
			name:           "UnaryMinusInteger",
			exprPosition:   Position{Line: 3, Character: 12},
			wantKind:       SpxInputSlotKindValue,
			wantAcceptType: SpxInputTypeInteger,
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeInteger,
			wantInputValue: int64(-42),
		},
		{
			name:           "UnaryMinusFloat",
			exprPosition:   Position{Line: 6, Character: 14},
			wantKind:       SpxInputSlotKindValue,
			wantAcceptType: SpxInputTypeDecimal,
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeDecimal,
			wantInputValue: -3.14,
		},
		{
			name:           "UnaryPlusInteger",
			exprPosition:   Position{Line: 9, Character: 12},
			wantKind:       SpxInputSlotKindValue,
			wantAcceptType: SpxInputTypeInteger,
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeInteger,
			wantInputValue: int64(10),
		},
		{
			name:           "BitwiseComplement",
			exprPosition:   Position{Line: 12, Character: 19},
			wantKind:       SpxInputSlotKindValue,
			wantAcceptType: SpxInputTypeInteger,
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeInteger,
			wantInputValue: int64(^0xFF), // ~255 = -256
		},
		{
			name:           "LogicalNot",
			exprPosition:   Position{Line: 15, Character: 13},
			wantKind:       SpxInputSlotKindValue,
			wantAcceptType: SpxInputTypeBoolean,
			wantInputKind:  SpxInputKindInPlace,
			wantInputType:  SpxInputTypeBoolean,
			wantInputValue: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pos := PosAt(result.proj, astFile, tt.exprPosition)
			require.True(t, pos.IsValid())

			var unaryExpr *ast.UnaryExpr
			for node := range xgoutil.PathEnclosingIntervalNodes(astFile, pos, pos, false) {
				if expr, ok := node.(*ast.UnaryExpr); ok {
					unaryExpr = expr
					break
				}
			}
			require.NotNil(t, unaryExpr)

			got := createValueInputSlotFromUnaryExpr(ctx, unaryExpr, nil)
			require.NotNil(t, got)
			assert.Equal(t, tt.wantKind, got.Kind)
			assert.Equal(t, tt.wantAcceptType, got.Accept.Type)
			assert.Equal(t, tt.wantInputKind, got.Input.Kind)
			assert.Equal(t, tt.wantInputType, got.Input.Type)
			assert.Equal(t, tt.wantInputValue, got.Input.Value)
			assert.NotEmpty(t, got.Range)
		})
	}
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
	s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
	require.NoError(t, err)
	require.False(t, result.hasErrorSeverityDiagnostic)
	require.NotNil(t, astFile)
	ctx := newInputSlotContext(result, astFile)

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
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := isSpxColorFunc(tt.fun)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestInferSpxInputTypeFromType(t *testing.T) {
	t.Run("BasicTypes", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			typ  gotypes.Type
			want SpxInputType
		}{
			{"String", gotypes.Typ[gotypes.String], SpxInputTypeString},
			{"UntypedString", gotypes.Typ[gotypes.UntypedString], SpxInputTypeString},

			{"Int", gotypes.Typ[gotypes.Int], SpxInputTypeInteger},
			{"Int8", gotypes.Typ[gotypes.Int8], SpxInputTypeInteger},
			{"Int16", gotypes.Typ[gotypes.Int16], SpxInputTypeInteger},
			{"Int32", gotypes.Typ[gotypes.Int32], SpxInputTypeInteger},
			{"Int64", gotypes.Typ[gotypes.Int64], SpxInputTypeInteger},
			{"Uint", gotypes.Typ[gotypes.Uint], SpxInputTypeInteger},
			{"Uint8", gotypes.Typ[gotypes.Uint8], SpxInputTypeInteger},
			{"Uint16", gotypes.Typ[gotypes.Uint16], SpxInputTypeInteger},
			{"Uint32", gotypes.Typ[gotypes.Uint32], SpxInputTypeInteger},
			{"Uint64", gotypes.Typ[gotypes.Uint64], SpxInputTypeInteger},
			{"UntypedInt", gotypes.Typ[gotypes.UntypedInt], SpxInputTypeInteger},

			{"Float32", gotypes.Typ[gotypes.Float32], SpxInputTypeDecimal},
			{"Float64", gotypes.Typ[gotypes.Float64], SpxInputTypeDecimal},
			{"UntypedFloat", gotypes.Typ[gotypes.UntypedFloat], SpxInputTypeDecimal},

			{"Bool", gotypes.Typ[gotypes.Bool], SpxInputTypeBoolean},
			{"UntypedBool", gotypes.Typ[gotypes.UntypedBool], SpxInputTypeBoolean},

			{"Complex64", gotypes.Typ[gotypes.Complex64], SpxInputTypeUnknown},
			{"Complex128", gotypes.Typ[gotypes.Complex128], SpxInputTypeUnknown},
		} {
			t.Run(tt.name, func(t *testing.T) {
				got := inferSpxInputTypeFromType(tt.typ)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("NonBasicType", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/pkg", "pkg")
		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		namedType := gotypes.NewNamed(gotypes.NewTypeName(0, pkg, "MyStruct", nil), structType, nil)

		got := inferSpxInputTypeFromType(namedType)
		assert.Equal(t, SpxInputTypeUnknown, got)
	})

	t.Run("PointerType", func(t *testing.T) {
		pointerType := gotypes.NewPointer(gotypes.Typ[gotypes.Int])

		got := inferSpxInputTypeFromType(pointerType)
		assert.Equal(t, SpxInputTypeUnknown, got)
	})

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
		namedIntType := gotypes.NewNamed(gotypes.NewTypeName(0, pkg, "MyCount", nil), gotypes.Typ[gotypes.Int], nil)

		for _, tt := range []struct {
			name string
			typ  gotypes.Type
			want SpxInputType
		}{
			{
				name: "AliasToBasic",
				typ:  gotypes.NewAlias(gotypes.NewTypeName(0, pkg, "MyInt", nil), gotypes.Typ[gotypes.Int]),
				want: SpxInputTypeInteger,
			},
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
			{
				name: "AliasToNamedType",
				typ:  gotypes.NewAlias(gotypes.NewTypeName(0, pkg, "MyCountAlias", nil), namedIntType),
				want: SpxInputTypeUnknown,
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
	s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

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

func TestIsBlank(t *testing.T) {
	for _, tt := range []struct {
		name string
		expr ast.Expr
		want bool
	}{
		{"BlankIdent", &ast.Ident{Name: "_"}, true},
		{"NonBlankIdent", &ast.Ident{Name: "variable"}, false},
		{"BasicLit", &ast.BasicLit{Value: "test"}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := isBlank(tt.expr)
			assert.Equal(t, tt.want, got)
		})
	}
}

func findInputSlot(inputSlots []SpxInputSlot, value any, name string, inputType SpxInputType, kind SpxInputKind) *SpxInputSlot {
	for _, slot := range inputSlots {
		if slot.Input.Kind == kind {
			if kind == SpxInputKindInPlace && reflect.DeepEqual(slot.Input.Value, value) && slot.Input.Type == inputType {
				return &slot
			} else if kind == SpxInputKindPredefined && slot.Input.Name == name && slot.Input.Type == inputType {
				return &slot
			}
		}
	}
	return nil
}

func findInputSlotByRange(inputSlots []SpxInputSlot, inputRange Range) *SpxInputSlot {
	for i := range inputSlots {
		if inputSlots[i].Range == inputRange {
			return &inputSlots[i]
		}
	}
	return nil
}

func findAddressInputSlot(inputSlots []SpxInputSlot, name string) *SpxInputSlot {
	for _, slot := range inputSlots {
		if slot.Kind == SpxInputSlotKindAddress && slot.Input.Name == name {
			return &slot
		}
	}
	return nil
}
