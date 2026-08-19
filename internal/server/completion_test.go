package server

import (
	gotypes "go/types"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/protocol"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletion(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`

MySprite.
`),
			"MySprite.spx": []byte(`
onStart => {
	MySprite.turn Right
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		emptyLineItemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 0},
			},
		})
		require.NoError(t, err)
		emptyLineItems := emptyLineItemsResult.([]CompletionItem)
		require.NotNil(t, emptyLineItems)
		assert.NotEmpty(t, emptyLineItems)
		assert.True(t, containsCompletionItemLabel(emptyLineItems, "println"))
		assert.True(t, containsCompletionSpxDefinitionID(emptyLineItems, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MySprite"),
		}))

		assert.Contains(t, emptyLineItems, SpxDefinition{
			ID: SpxDefinitionIdentifier{
				Package: ToPtr(SpxPkgPath),
				Name:    ToPtr("Game.getWidget"),
			},
			Overview: "func getWidget(T Type, name WidgetName) *T",
			Detail:   "GetWidget returns the widget instance (in given type) with given name. It panics if not found.\n",

			CompletionItemLabel:            "getWidget",
			CompletionItemKind:             FunctionCompletion,
			CompletionItemInsertText:       "getWidget",
			CompletionItemInsertTextFormat: PlainTextTextFormat,
		}.CompletionItem())

		mySpriteDotItemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 9},
			},
		})
		require.NoError(t, err)
		mySpriteDotItems := mySpriteDotItemsResult.([]CompletionItem)
		require.NotNil(t, mySpriteDotItems)
		assert.NotEmpty(t, mySpriteDotItems)
		assert.False(t, containsCompletionItemLabel(mySpriteDotItems, "println"))
		assert.True(t, containsCompletionSpxDefinitionID(mySpriteDotItems, SpxDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.turn"),
			OverloadID: ToPtr("0"),
		}))
		assert.True(t, containsCompletionSpxDefinitionID(mySpriteDotItems, SpxDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.turn"),
			OverloadID: ToPtr("0"),
		}))
		assert.True(t, containsCompletionSpxDefinitionID(mySpriteDotItems, SpxDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.turn"),
			OverloadID: ToPtr("1"),
		}))
		assert.True(t, containsCompletionSpxDefinitionID(mySpriteDotItems, SpxDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.clone"),
			OverloadID: ToPtr("0"),
		}))
		assert.True(t, containsCompletionSpxDefinitionID(mySpriteDotItems, SpxDefinitionIdentifier{
			Package:    ToPtr(SpxPkgPath),
			Name:       ToPtr("Sprite.clone"),
			OverloadID: ToPtr("1"),
		}))
	})

	t.Run("InSpxEventHandler", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {

}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.False(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr(SpxPkgPath),
			Name:    ToPtr("Sprite.onStart"),
		}))
		assert.False(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr(SpxPkgPath),
			Name:    ToPtr("Sprite.onClick"),
		}))
	})

	t.Run("FuncDecoratorArgument", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`const (
	count   = 1
	comment = "text"
)

func retry(times int, fn func()) {
	fn()
}

@retry(co)
func run() {
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 9},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		assert.True(t, containsCompletionItemLabel(items, "count"))
		assert.False(t, containsCompletionItemLabel(items, "comment"))
	})

	t.Run("FuncDecoratorImplicitArgument", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`const (
	count   = 1
	comment = "text"
)

func retry(times int, fn func()) {
	fn()
}

@retry(1, co)
func run() {
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 12},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		assert.True(t, containsCompletionItemLabel(items, "count"))
		assert.True(t, containsCompletionItemLabel(items, "comment"))
	})

	t.Run("NestedFuncDecoratorArgument", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`const (
	count   = 1
	comment = "text"
)

func retry(times int, fn func()) {
	fn()
}

func parse(text string) int {
	return len(text)
}

@retry(parse(co))
func run() {
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 13, Character: 15},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		assert.Falsef(t, containsCompletionItemLabel(items, "count"), "%v", completionItemLabels(items))
		assert.True(t, containsCompletionItemLabel(items, "comment"))
	})

	t.Run("NestedCallArgument", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`const (
	count   = 1
	comment = "text"
)

func consume(value int) {
}

func parse(text string) int {
	return len(text)
}

func run() {
	consume(parse(co))
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 13, Character: 17},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		assert.Falsef(t, containsCompletionItemLabel(items, "count"), "%v", completionItemLabels(items))
		assert.True(t, containsCompletionItemLabel(items, "comment"))
	})

	t.Run("PartialXGoxFunction", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`import "example.com/typeargs"

const (
	count   = 1
	comment = "text"
)

onStart => {
	typeargs.convert(string, count)
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})
		s.workspaceRootFS.Importer = xgoxTestImporter{fallback: s.workspaceRootFS.Importer}

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 31},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		assert.True(t, containsCompletionItemLabel(items, "count"))
		assert.False(t, containsCompletionItemLabel(items, "comment"))
	})

	t.Run("EnumType", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`type TrafficLight const (
	Red = iota
)

type Signal = TrafficLight

func run() {
	var light Tra
	var signal Sig
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		for _, tt := range []struct {
			name     string
			position Position
			label    string
		}{
			{name: "Declaration", position: Position{Line: 7, Character: 14}, label: "TrafficLight"},
			{name: "Alias", position: Position{Line: 8, Character: 15}, label: "Signal"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				itemsResult, err := s.textDocumentCompletion(&CompletionParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
						Position:     tt.position,
					},
				})
				require.NoError(t, err)
				items := requireValueAs[[]CompletionItem](t, itemsResult)
				item := completionItemByLabel(items, tt.label)
				require.NotNilf(t, item, "%v", completionItemLabels(items))
				assert.Equal(t, EnumCompletion, item.Kind)
			})
		}
	})

	t.Run("EnumValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`const Result = "text"

func run() {
	type Color const (
		// Red documentation.
		Red = iota
		Green
	)

	var color Color = R
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 20},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		item := completionItemByLabel(items, "Red")
		require.NotNilf(t, item, "%v", completionItemLabels(items))
		assert.Equal(t, EnumMemberCompletion, item.Kind)
		require.NotNil(t, item.Documentation)
		documentation := requireValueAs[MarkupContent](t, item.Documentation.Value)
		assert.Contains(t, documentation.Value, "Red documentation.")
		assert.False(t, containsCompletionItemLabel(items, "Result"))
	})

	t.Run("StringEnumValueForIndex", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`type Word const (
	Hello = "hello"
)

func run() {
	var first byte = H[0]
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 19},
			},
		})
		require.NoError(t, err)
		items := requireValueAs[[]CompletionItem](t, itemsResult)
		item := completionItemByLabel(items, "Hello")
		require.NotNilf(t, item, "%v", completionItemLabels(items))
		assert.Equal(t, EnumMemberCompletion, item.Kind)
	})

	t.Run("EnumValueForUnassignableTarget", func(t *testing.T) {
		sourcePrefix := `type Color const (
	Red = iota
)

type Size const (
	Small = iota
)

func run() {
`
		for _, tt := range []struct {
			name       string
			assignment string
			wantLabel  string
		}{
			{name: "DifferentEnum", assignment: "\tvar size Size = R", wantLabel: "Small"},
			{name: "ConvertibleBasic", assignment: "\tvar number float64 = R"},
			{name: "DereferenceOperand", assignment: "\tvar color Color = *R"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				m := map[string][]byte{
					"main.spx": []byte(sourcePrefix + tt.assignment + "\n}\n"),
				}
				s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

				itemsResult, err := s.textDocumentCompletion(&CompletionParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
						Position: Position{
							Line:      uint32(strings.Count(sourcePrefix, "\n")),
							Character: uint32(len(tt.assignment)),
						},
					},
				})
				require.NoError(t, err)
				items := requireValueAs[[]CompletionItem](t, itemsResult)
				assert.Falsef(t, containsCompletionItemLabel(items, "Red"), "%v", completionItemLabels(items))
				if tt.wantLabel != "" {
					assert.Truef(t, containsCompletionItemLabel(items, tt.wantLabel), "%v", completionItemLabels(items))
				}
			})
		}
	})

	t.Run("EnumSwitchCase", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`type First const (
	// First member documentation.
	Unknown = iota
)

type Second const (
	// Second member documentation.
	Unknown = iota
)

func run(value Second) {
	switch value {
	case U:
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 12, Character: 7},
			},
		})
		require.NoError(t, err)
		items := requireValueAs[[]CompletionItem](t, itemsResult)
		item := completionItemByLabel(items, "Unknown")
		require.NotNilf(t, item, "%v", completionItemLabels(items))
		assert.Equal(t, 1, countCompletionItemLabel(items, "Unknown"))
		assert.Equal(t, EnumMemberCompletion, item.Kind)
		require.NotNil(t, item.Documentation)
		documentation := requireValueAs[MarkupContent](t, item.Documentation.Value)
		assert.Contains(t, documentation.Value, "Second member documentation.")
		assert.NotContains(t, documentation.Value, "First member documentation.")
	})

	t.Run("EnumBlankIdentifier", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`type Color const (
	_ = iota
	Red
)

func run() {
	var color Color =
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 6, Character: 19},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		assert.Falsef(t, containsCompletionItemLabel(items, "_"), "%v", completionItemLabels(items))
		assert.Truef(t, containsCompletionItemLabel(items, "Red"), "%v", completionItemLabels(items))
	})

	t.Run("EnumMemberSharedWithRegularConstant", func(t *testing.T) {
		sourcePrefix := `const (
	// Regular documentation.
	Shared = 1
)

type Color const (
	// Enum documentation.
	Shared = 1
)

func run() {
`
		for _, tt := range []struct {
			name        string
			assignment  string
			position    Position
			wantKind    CompletionItemKind
			wantDoc     string
			unwantedDoc string
		}{
			{
				name:        "RegularContext",
				assignment:  "\tvar value any = Sh",
				position:    Position{Line: 11, Character: 19},
				wantKind:    ConstantCompletion,
				wantDoc:     "Regular documentation.",
				unwantedDoc: "Enum documentation.",
			},
			{
				name:        "EnumContext",
				assignment:  "\tvar value Color = Sh",
				position:    Position{Line: 11, Character: 21},
				wantKind:    EnumMemberCompletion,
				wantDoc:     "Enum documentation.",
				unwantedDoc: "Regular documentation.",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				m := map[string][]byte{
					"main.spx": []byte(sourcePrefix + tt.assignment + "\n}\n"),
				}
				s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

				itemsResult, err := s.textDocumentCompletion(&CompletionParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
						Position:     tt.position,
					},
				})
				require.NoError(t, err)
				items := itemsResult.([]CompletionItem)
				item := completionItemByLabel(items, "Shared")
				require.NotNilf(t, item, "%v", completionItemLabels(items))
				assert.Equal(t, 1, countCompletionItemLabel(items, "Shared"))
				assert.Equal(t, tt.wantKind, item.Kind)
				require.NotNil(t, item.Documentation)
				documentation := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, documentation.Value, tt.wantDoc)
				assert.NotContains(t, documentation.Value, tt.unwantedDoc)
			})
		}
	})

	t.Run("EnumMemberShadowedByLocal", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`type Color const (
	// Enum documentation.
	Red = iota
)

func run() {
	var Red Color
	var color Color = R
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 7, Character: 20},
			},
		})
		require.NoError(t, err)
		items := requireValueAs[[]CompletionItem](t, itemsResult)
		item := completionItemByLabel(items, "Red")
		require.NotNilf(t, item, "%v", completionItemLabels(items))
		assert.Equal(t, VariableCompletion, item.Kind)
		require.NotNil(t, item.Documentation)
		documentation := requireValueAs[MarkupContent](t, item.Documentation.Value)
		assert.NotContains(t, documentation.Value, "Enum documentation.")
	})

	t.Run("EnumValueForBroadAssignmentTarget", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`type Color const (
	Red = iota
)

func run() {
	var declared any = R
	var assigned any
	assigned = R
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		for _, position := range []Position{
			{Line: 5, Character: 21},
			{Line: 7, Character: 13},
		} {
			itemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     position,
				},
			})
			require.NoError(t, err)
			items := itemsResult.([]CompletionItem)
			item := completionItemByLabel(items, "Red")
			require.NotNilf(t, item, "%v", completionItemLabels(items))
			assert.Equal(t, EnumMemberCompletion, item.Kind)
		}
	})

	t.Run("EnumValueForBasicTypeContext", func(t *testing.T) {
		sourcePrefix := `type Integer const (
	IntegerValue = iota
)

type Float const (
	FloatValue = 1.5
)

type ComplexNumber const (
	ComplexValue = 1i
)

type Text const (
	StringValue = "value"
)

type Toggle const (
	BoolValue = true
)

var (
	integerMap map[Integer]int
	integers []Integer
	bytes []byte
)

func run() {
`
		labels := []string{"IntegerValue", "FloatValue", "ComplexValue", "StringValue", "BoolValue"}
		for _, tt := range []struct {
			name       string
			expression string
			wantLabels []string
		}{
			{name: "IfCondition", expression: "\tif I {}", wantLabels: []string{"BoolValue"}},
			{name: "ForCondition", expression: "\tfor I {}", wantLabels: []string{"BoolValue"}},
			{name: "LogicalOperand", expression: "\t_ = true && I", wantLabels: []string{"BoolValue"}},
			{name: "UnaryNot", expression: "\t_ = !I", wantLabels: []string{"BoolValue"}},
			{name: "UnaryAdd", expression: "\t_ = +I", wantLabels: []string{"IntegerValue", "FloatValue", "ComplexValue"}},
			{
				name:       "NumericAddition",
				expression: "\t_ = 1 + I",
				wantLabels: []string{"IntegerValue", "FloatValue", "ComplexValue"},
			},
			{
				name:       "FractionalAddition",
				expression: "\t_ = 1.5 + I",
				wantLabels: []string{"FloatValue", "ComplexValue"},
			},
			{
				name:       "IntegralFloatAddition",
				expression: "\t_ = 1.0 + I",
				wantLabels: []string{"IntegerValue", "FloatValue", "ComplexValue"},
			},
			{name: "ImaginaryAddition", expression: "\t_ = 1i + I", wantLabels: []string{"ComplexValue"}},
			{name: "StringAddition", expression: "\t_ = \"\" + I", wantLabels: []string{"StringValue"}},
			{
				name:       "Subtraction",
				expression: "\t_ = 1 - I",
				wantLabels: []string{"IntegerValue", "FloatValue", "ComplexValue"},
			},
			{name: "BitwiseOperand", expression: "\t_ = 1 & I", wantLabels: []string{"IntegerValue"}},
			{name: "BooleanComparison", expression: "\t_ = I == false", wantLabels: []string{"BoolValue"}},
			{name: "NumericComparison", expression: "\t_ = I < 1", wantLabels: []string{"IntegerValue", "FloatValue"}},
			{name: "StringComparison", expression: "\t_ = I < \"\"", wantLabels: []string{"StringValue"}},
			{name: "NilComparison", expression: "\t_ = I == nil"},
			{name: "ShiftCount", expression: "\t_ = 1 << I", wantLabels: []string{"IntegerValue"}},
			{name: "Index", expression: "\t_ = []int{1}[I]", wantLabels: []string{"IntegerValue"}},
			{name: "SliceBound", expression: "\t_ = []int{1}[I:]", wantLabels: []string{"IntegerValue"}},
			{name: "CompositeLiteralIndex", expression: "\t_ = []int{I: 1}", wantLabels: []string{"IntegerValue"}},
			{name: "IndexContainer", expression: "\t_ = I[0]", wantLabels: []string{"StringValue"}},
			{name: "Len", expression: "\t_ = len(I)", wantLabels: []string{"StringValue"}},
			{name: "Cap", expression: "\t_ = cap(I)"},
			{name: "MakeLength", expression: "\t_ = make([]int, I)", wantLabels: []string{"IntegerValue"}},
			{name: "ComplexReal", expression: "\t_ = complex(I, 1)", wantLabels: []string{"FloatValue"}},
			{name: "Real", expression: "\t_ = real(I)", wantLabels: []string{"ComplexValue"}},
			{name: "AppendElement", expression: "\t_ = append(integers, I)", wantLabels: []string{"IntegerValue"}},
			{name: "AppendString", expression: "\t_ = append(bytes, I...)", wantLabels: []string{"StringValue"}},
			{name: "AppendListContainer", expression: "\t_ = append([I], IntegerValue)", wantLabels: []string{"IntegerValue"}},
			{name: "AppendListContainerString", expression: "\t_ = append([I], \"value\"...)"},
			{name: "AppendableSend", expression: "\tintegers <- I", wantLabels: []string{"IntegerValue"}},
			{name: "AppendableSendString", expression: "\tbytes <- I...", wantLabels: []string{"StringValue"}},
			{name: "AppendableSendEllipsis", expression: "\tintegers <- I..."},
			{name: "DeleteKey", expression: "\tdelete(integerMap, I)", wantLabels: []string{"IntegerValue"}},
			{name: "AppendContainer", expression: "\t_ = append(I, IntegerValue)"},
			{name: "Clear", expression: "\tclear(I)"},
			{name: "Close", expression: "\tclose(I)"},
			{name: "Copy", expression: "\t_ = copy(I, integers)"},
			{name: "CopyString", expression: "\t_ = copy(bytes, I)", wantLabels: []string{"StringValue"}},
			{name: "CopyListSource", expression: "\t_ = copy(integers, [I])", wantLabels: []string{"IntegerValue"}},
			{name: "CopyListDestination", expression: "\t_ = copy([I], integers)", wantLabels: []string{"IntegerValue"}},
			{name: "IncompleteCopyDestination", expression: "\t_ = copy([I])", wantLabels: labels},
			{name: "New", expression: "\t_ = new(I)"},
			{name: "Range", expression: "\tfor value <- I { _ = value }", wantLabels: []string{"IntegerValue", "StringValue"}},
			{name: "RangeExpressionStart", expression: "\tfor value <- I:10 { _ = value }", wantLabels: []string{"IntegerValue", "FloatValue"}},
			{
				name:       "ListComprehensionRange",
				expression: "\t_ = [value for value <- I]",
				wantLabels: []string{"IntegerValue", "StringValue"},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				m := map[string][]byte{
					"main.spx": []byte(sourcePrefix + tt.expression + "\n}\n"),
				}
				s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

				itemsResult, err := s.textDocumentCompletion(&CompletionParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
						Position: Position{
							Line:      uint32(strings.Count(sourcePrefix, "\n")),
							Character: uint32(strings.Index(tt.expression, "I") + 1),
						},
					},
				})
				require.NoError(t, err)
				items := requireValueAs[[]CompletionItem](t, itemsResult)
				for _, label := range labels {
					item := completionItemByLabel(items, label)
					if slices.Contains(tt.wantLabels, label) {
						require.NotNilf(t, item, "%v", completionItemLabels(items))
						assert.Equal(t, EnumMemberCompletion, item.Kind)
					} else {
						assert.Nilf(t, item, "%v", completionItemLabels(items))
					}
				}
			})
		}
	})

	t.Run("EnumValueForPointerTarget", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`type Color const (
	Red = iota
)

func use(*Color) {}

func run() {
	use(R)
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 7, Character: 6},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		assert.Falsef(t, containsCompletionItemLabel(items, "Red"), "%v", completionItemLabels(items))
	})

	t.Run("EnumValueForPointerConversionTarget", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`type Color const (
	Red = iota
)

type ColorPtr = *Color

func run() {
	_ = ColorPtr(R)
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 7, Character: 15},
			},
		})
		require.NoError(t, err)
		items := requireValueAs[[]CompletionItem](t, itemsResult)
		assert.Falsef(t, containsCompletionItemLabel(items, "Red"), "%v", completionItemLabels(items))
	})

	t.Run("EnumValueForExplicitConversion", func(t *testing.T) {
		sourcePrefix := `type Color const (
	Red = iota
)

type Size const (
	Small = iota
)

func run() {
`
		for _, tt := range []struct {
			name       string
			expression string
		}{
			{name: "Direct", expression: "R"},
			{name: "BinaryExpression", expression: "R + 1"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				line := "\t_ = Size(" + tt.expression + ")"
				m := map[string][]byte{
					"main.spx": []byte(sourcePrefix + line + "\n}\n"),
				}
				s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

				itemsResult, err := s.textDocumentCompletion(&CompletionParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
						Position: Position{
							Line:      uint32(strings.Count(sourcePrefix, "\n")),
							Character: uint32(strings.Index(line, "R") + len("R")),
						},
					},
				})
				require.NoError(t, err)
				items := requireValueAs[[]CompletionItem](t, itemsResult)
				item := completionItemByLabel(items, "Red")
				require.NotNilf(t, item, "%v", completionItemLabels(items))
				assert.Equal(t, EnumMemberCompletion, item.Kind)
			})
		}
	})

	t.Run("DuplicateEnumValue", func(t *testing.T) {
		sourcePrefix := `type First const (
	// First member documentation.
	Unknown = iota
)

type Second const (
	// Second member documentation.
	Unknown = iota
)

func run() {
`
		for _, tt := range []struct {
			name        string
			assignment  string
			position    Position
			wantDoc     string
			unwantedDoc string
		}{
			{
				name:        "First",
				assignment:  "\tvar value First = Un",
				position:    Position{Line: 11, Character: 21},
				wantDoc:     "First member documentation.",
				unwantedDoc: "Second member documentation.",
			},
			{
				name:        "Second",
				assignment:  "\tvar value Second = U",
				position:    Position{Line: 11, Character: 21},
				wantDoc:     "Second member documentation.",
				unwantedDoc: "First member documentation.",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				m := map[string][]byte{
					"main.spx": []byte(sourcePrefix + tt.assignment + "\n}\n"),
				}
				s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

				itemsResult, err := s.textDocumentCompletion(&CompletionParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
						Position:     tt.position,
					},
				})
				require.NoError(t, err)
				items := itemsResult.([]CompletionItem)
				item := completionItemByLabel(items, "Unknown")
				require.NotNilf(t, item, "%v", completionItemLabels(items))
				assert.Equal(t, 1, countCompletionItemLabel(items, "Unknown"))
				assert.Equal(t, EnumMemberCompletion, item.Kind)
				require.NotNil(t, item.Documentation)
				documentation := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, documentation.Value, tt.wantDoc)
				assert.NotContains(t, documentation.Value, tt.unwantedDoc)
				assert.Falsef(t, containsCompletionItemLabel(items, "_Unknown_1"), "%v", completionItemLabels(items))
				assert.Falsef(t, containsCompletionItemLabel(items, "_Unknown_2"), "%v", completionItemLabels(items))
			})
		}
	})

	t.Run("ContextualDuplicateEnumValue", func(t *testing.T) {
		sourcePrefix := `type First const (
	// First member documentation.
	Unknown = iota
)

type Second const (
	// Second member documentation.
	Unknown = iota
)

func useSlice([]Second) {}
func useMatrix([][]Second) {}

func run(second Second, values map[Second]int, ch chan Second) {
`
		for _, tt := range []struct {
			name         string
			expression   string
			wantFirstDoc bool
		}{
			{name: "SliceLiteral", expression: "\t_ = []Second{Un}"},
			{name: "BinaryExpression", expression: "\t_ = second == Un"},
			{name: "BinaryWithUntypedOperand", expression: "\tvar value Second = Un + 1"},
			{name: "UnaryExpression", expression: "\tvar value Second = +Un"},
			{name: "MapIndex", expression: "\t_ = values[Un]"},
			{name: "Send", expression: "\tch <- Un"},
			{name: "ShiftCount", expression: "\t_ = second << Un", wantFirstDoc: true},
			{name: "SliceBound", expression: "\tvar slice []int = []int{1}[Un:]", wantFirstDoc: true},
			{name: "XGoSliceLiteral", expression: "\tuseSlice([Un])"},
			{name: "XGoMatrixLiteral", expression: "\tuseMatrix([Un; Unknown])"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				m := map[string][]byte{
					"main.spx": []byte(sourcePrefix + tt.expression + "\n}\n"),
				}
				s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

				itemsResult, err := s.textDocumentCompletion(&CompletionParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
						Position: Position{
							Line:      uint32(strings.Count(sourcePrefix, "\n")),
							Character: uint32(strings.Index(tt.expression, "Un") + len("Un")),
						},
					},
				})
				require.NoError(t, err)
				items := requireValueAs[[]CompletionItem](t, itemsResult)
				item := completionItemByLabel(items, "Unknown")
				require.NotNilf(t, item, "%v", completionItemLabels(items))
				assert.Equal(t, EnumMemberCompletion, item.Kind)
				require.NotNil(t, item.Documentation)
				documentation := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, documentation.Value, "Second member documentation.")
				if tt.wantFirstDoc {
					assert.Contains(t, documentation.Value, "First member documentation.")
				} else {
					assert.NotContains(t, documentation.Value, "First member documentation.")
				}
			})
		}
	})

	t.Run("EnumValueForXGoExpressionContext", func(t *testing.T) {
		declarations := `type First const (
	// First member documentation.
	Unknown = iota
)

type Second const (
	// Second member documentation.
	Unknown = iota
)

type Unique const (
	SecondValue = iota
)

type Key const (
	KeyValue = iota
)

type Element const (
	ElementValue = iota
)

type RangeValue const (
	RangeStart = iota
	RangeEnd
	RangeStep
)

type OtherRange const (
	OtherRangeValue = iota
)

type FirstText const (
	TextFirst = "first"
)

type SecondText const (
	TextSecond = "second"
)

type FirstToggle const (
	FirstEnabled = true
)

type SecondToggle const (
	SecondEnabled = true
)

func loadSecond() (Second, error) { return Unknown, nil }

`
		for _, tt := range []struct {
			name        string
			body        string
			cursorText  string
			label       string
			absentLabel string
			wantDoc     string
			unwantedDoc string
		}{
			{
				name: "TupleFirstElement",
				body: `func use(First, Second) {}

func run() {
	use((Un, Unknown))
}
`,
				cursorText:  "Un,",
				label:       "Unknown",
				wantDoc:     "First member documentation.",
				unwantedDoc: "Second member documentation.",
			},
			{
				name: "TupleSecondElement",
				body: `func use(First, Second) {}

func run() {
	use((Unknown, Un))
}
`,
				cursorText:  "Un))",
				label:       "Unknown",
				wantDoc:     "Second member documentation.",
				unwantedDoc: "First member documentation.",
			},
			{
				name: "ListComprehensionElement",
				body: `func run() {
	var values []Unique = [Se for _ <- [1]]
}
`,
				cursorText: "Se for",
				label:      "SecondValue",
			},
			{
				name: "MapComprehensionKey",
				body: `func run() {
	var values map[Key]Element = {Ke: ElementValue for _ <- [1]}
}
`,
				cursorText: "Ke:",
				label:      "KeyValue",
			},
			{
				name: "MapComprehensionValue",
				body: `func run() {
	var values map[Key]Element = {KeyValue: El for _ <- [1]}
}
`,
				cursorText: "El for",
				label:      "ElementValue",
			},
			{
				name: "NestedMapComprehensionValue",
				body: `func run() {
	var values map[Key][]Element = {KeyValue: [El] for _ <- [1]}
}
`,
				cursorText: "El]",
				label:      "ElementValue",
			},
			{
				name: "LambdaResult",
				body: `func use(func() Second) {}

func run() {
	use(=> Un)
}
`,
				cursorText:  "Un)",
				label:       "Unknown",
				wantDoc:     "Second member documentation.",
				unwantedDoc: "First member documentation.",
			},
			{
				name: "AppendNestedList",
				body: `func run() {
	var values [][]Second
	_ = append(values, [Un])
}
`,
				cursorText:  "Un]",
				label:       "Unknown",
				wantDoc:     "Second member documentation.",
				unwantedDoc: "First member documentation.",
			},
			{
				name: "AppendNestedListEllipsis",
				body: `func run() {
	var values []Second
	_ = append(values, [Un]...)
}
`,
				cursorText:  "Un]",
				label:       "Unknown",
				wantDoc:     "Second member documentation.",
				unwantedDoc: "First member documentation.",
			},
			{
				name: "AppendLambda",
				body: `func run() {
	var funcs []func() Second
	_ = append(funcs, => Un)
}
`,
				cursorText:  "Un)",
				label:       "Unknown",
				wantDoc:     "Second member documentation.",
				unwantedDoc: "First member documentation.",
			},
			{
				name: "LambdaBlockReturn",
				body: `func use(func() Second) {}

func run() {
	use(=> { return Un })
}
`,
				cursorText:  "Un }",
				label:       "Unknown",
				wantDoc:     "Second member documentation.",
				unwantedDoc: "First member documentation.",
			},
			{
				name: "LambdaCollectionResult",
				body: `func use(func() []Second) {}

func run() {
	use(=> [Un])
}
`,
				cursorText:  "Un]",
				label:       "Unknown",
				wantDoc:     "Second member documentation.",
				unwantedDoc: "First member documentation.",
			},
			{
				name: "LambdaSelectComprehensionResult",
				body: `func use(func() Second) {}

func run() {
	use(=> ({Un for _ <- [1]}))
}
`,
				cursorText:  "Un for",
				label:       "Unknown",
				wantDoc:     "Second member documentation.",
				unwantedDoc: "First member documentation.",
			},
			{
				name: "LambdaErrorWrapDefault",
				body: `func use(func() Second) {}

func run() {
	use(=> loadSecond()?:Un)
}
`,
				cursorText:  "Un)",
				label:       "Unknown",
				wantDoc:     "Second member documentation.",
				unwantedDoc: "First member documentation.",
			},
			{
				name: "LambdaStringSliceResult",
				body: `func use(func() SecondText) {}

func run() {
	use(=> Te[:])
}
`,
				cursorText:  "Te[:",
				label:       "TextSecond",
				absentLabel: "TextFirst",
			},
			{
				name: "CompoundShiftCount",
				body: `func run() {
	var value First = Unknown
	value <<= Se
}
`,
				cursorText: "Se\n",
				label:      "SecondValue",
			},
			{
				name: "LogicalExpressionResult",
				body: `func run() {
	var value SecondToggle = Se && true
}
`,
				cursorText:  "Se &&",
				label:       "SecondEnabled",
				absentLabel: "FirstEnabled",
			},
			{
				name: "RangeExpressionEnd",
				body: `func run() {
	for value <- RangeStart:Ra { _ = value }
}
`,
				cursorText:  "Ra {",
				label:       "RangeEnd",
				absentLabel: "OtherRangeValue",
			},
			{
				name: "RangeExpressionStep",
				body: `func run() {
	for value <- RangeStart:RangeEnd:Ra { _ = value }
}
`,
				cursorText:  "Ra {",
				label:       "RangeStep",
				absentLabel: "OtherRangeValue",
			},
			{
				name: "RangeExpressionDefaultStart",
				body: `func run() {
	for value <- :Ra { _ = value }
}
`,
				cursorText:  "Ra {",
				absentLabel: "RangeEnd",
			},
			{
				name: "AssignmentTarget",
				body: `func run() {
	Un = 1
}
`,
				cursorText:  "Un =",
				absentLabel: "Unknown",
			},
			{
				name: "IncrementTarget",
				body: `func run() {
	Un++
}
`,
				cursorText:  "Un++",
				absentLabel: "Unknown",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := declarations + tt.body
				offset := strings.LastIndex(source, tt.cursorText)
				require.NotEqual(t, -1, offset)
				offset += 2
				prefix := source[:offset]
				m := map[string][]byte{"main.spx": []byte(source)}
				s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

				itemsResult, err := s.textDocumentCompletion(&CompletionParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
						Position: Position{
							Line:      uint32(strings.Count(prefix, "\n")),
							Character: uint32(len(prefix) - strings.LastIndex(prefix, "\n") - 1),
						},
					},
				})
				require.NoError(t, err)
				items := requireValueAs[[]CompletionItem](t, itemsResult)
				if tt.absentLabel != "" {
					assert.Nilf(t, completionItemByLabel(items, tt.absentLabel), "%v", completionItemLabels(items))
				}
				if tt.label == "" {
					return
				}
				item := completionItemByLabel(items, tt.label)
				require.NotNilf(t, item, "%v", completionItemLabels(items))
				assert.Equal(t, EnumMemberCompletion, item.Kind)
				if tt.wantDoc == "" {
					return
				}
				require.NotNil(t, item.Documentation)
				documentation := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, documentation.Value, tt.wantDoc)
				assert.NotContains(t, documentation.Value, tt.unwantedDoc)
			})
		}
	})

	t.Run("OverloadedDuplicateEnumValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`type First const (
	// First member documentation.
	Unknown = iota
)

type Second const (
	// Second member documentation.
	Unknown = iota
)

func useFirst(First) {}
func useSecond(Second) {}
func use = (
	useFirst
	useSecond
)

func run() {
	use(Un)
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 18, Character: 7},
			},
		})
		require.NoError(t, err)
		items := requireValueAs[[]CompletionItem](t, itemsResult)
		item := completionItemByLabel(items, "Unknown")
		require.NotNilf(t, item, "%v", completionItemLabels(items))
		require.NotNil(t, item.Documentation)
		documentation := requireValueAs[MarkupContent](t, item.Documentation.Value)
		assert.Contains(t, documentation.Value, "First member documentation.")
		assert.Contains(t, documentation.Value, "Second member documentation.")
	})

	t.Run("InStringLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
echo "a
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 7},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("InComment", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
// Run My G
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 11},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("InImportStringLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "f
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 9},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "fmt"))
	})

	t.Run("NoCompletionInFuncDeclName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func d
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 6},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("NoCompletionInImportAlias", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import f "fmt"
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 8},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("NoCompletionInTypeDeclName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Fo struct{}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 7},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("NoCompletionInVarDeclName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var foo int
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 8},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("NoCompletionInConstDeclName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
const foo = 1
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 8},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("NoCompletionInPackageName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`package main

func test() {}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 0, Character: 10},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("NoCompletionInFuncReceiverName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type T struct{}

func (t T) test() {}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 7},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("NoCompletionInFuncParamName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func test(foo int) {}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 11},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("NoCompletionInFuncResultName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func test() (result int) { return 0 }
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 19},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("NoCompletionInStructFieldName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type T struct {
	field int
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 4},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("NoCompletionInInterfaceMethodName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type T interface {
	Run()
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 3},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("NoCompletionInLabelName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func test() {
loop:
	for {
		break
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 3},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("IncompleteMapLiteralInCall", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
println {"key": }
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 15},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
	})

	t.Run("InImportGroupStringLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import (
	"f
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 3},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "fmt"))
	})

	t.Run("PackageMember", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "fmt"
fmt.
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 4},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "println"))
	})

	t.Run("GeneralOrUnknown", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`

onStart => {

}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items1Result, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 1},
			},
		})
		require.NoError(t, err)
		items1 := items1Result.([]CompletionItem)
		require.NotNil(t, items1)
		assert.NotEmpty(t, items1)
		assert.True(t, containsCompletionItemLabel(items1, "len"))

		items2Result, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 12},
			},
		})
		require.NoError(t, err)
		items2 := items2Result.([]CompletionItem)
		require.NotNil(t, items2)
		assert.Empty(t, items2)

		items3Result, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 1},
			},
		})
		require.NoError(t, err)
		items3 := items3Result.([]CompletionItem)
		require.NotNil(t, items3)
		assert.NotEmpty(t, items3)
		assert.True(t, containsCompletionItemLabel(items3, "len"))
	})

	t.Run("VarDecl", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func test() {}
onStart => {
	var x i
}
`),
			"MySprite.spx": []byte(`
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 8},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "int"))
		assert.True(t, containsCompletionItemLabel(items, "MySprite"))
		assert.True(t, containsCompletionItemLabel(items, "Sprite"))
		assert.False(t, containsCompletionItemLabel(items, "len"))
		assert.False(t, containsCompletionItemLabel(items, "test"))
		assert.False(t, containsCompletionItemLabel(items, "play"))
	})

	t.Run("VarDeclAndAssign", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var x SpriteName = "m"
}
`),
			"MySprite.spx": []byte(`
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 22},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "MySprite"))
	})

	t.Run("VarDeclAndAssignWithAlias", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MySpriteName = SpriteName

onStart => {
	var x MySpriteName = "m"
}
`),
			"MySprite.spx":                       []byte(``),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 24},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "MySprite"))
	})

	t.Run("SpxSoundResourceStringLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
play "r"
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sounds/recording/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 7},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "recording"))
	})

	t.Run("FuncOverloads", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
play r
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sounds/recording/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 6},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, `"recording"`))
	})

	t.Run("WithImplicitSpxSpriteResource", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
`),
			"MySprite.spx": []byte(`
onClick => {
	setCostume "c"
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{"costumes":[{"name":"costume"}]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 2, Character: 14},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "costume"))
	})

	t.Run("WithExplicitSpxSpriteResource", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
MySprite.setCostume "c"
`),
			"MySprite.spx":                       []byte(``),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{"costumes":[{"name":"costume"}]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 22},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "costume"))
	})

	t.Run("WithCrossSpxSpriteResource", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
`),
			"Sprite1.spx": []byte(`
onClick => {
	Sprite2.setCostume "c"
}
`),
			"Sprite2.spx":                       []byte(``),
			"assets/index.json":                 []byte(`{}`),
			"assets/sprites/Sprite1/index.json": []byte(`{"costumes":[{"name":"Sprite1Costume"}]}`),
			"assets/sprites/Sprite2/index.json": []byte(`{"costumes":[{"name":"Sprite2Costume"}]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///Sprite1.spx"},
				Position:     Position{Line: 2, Character: 22},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "Sprite2Costume"))
	})

	t.Run("WithCrossSpxSpriteResourceInGoStmt", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(``),
			"Sprite1.spx": []byte(`
onClick => {
	go Sprite2.setCostume("c")
}
`),
			"Sprite2.spx":                       []byte(``),
			"assets/index.json":                 []byte(`{}`),
			"assets/sprites/Sprite1/index.json": []byte(`{"costumes":[{"name":"Sprite1Costume"}]}`),
			"assets/sprites/Sprite2/index.json": []byte(`{"costumes":[{"name":"Sprite2Costume"}]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///Sprite1.spx"},
				Position:     Position{Line: 2, Character: 25},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "Sprite2Costume"))
	})

	t.Run("WithCrossSpxSpriteResourceInDeferStmt", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(``),
			"Sprite1.spx": []byte(`
onClick => {
	defer Sprite2.setCostume("c")
}
`),
			"Sprite2.spx":                       []byte(``),
			"assets/index.json":                 []byte(`{}`),
			"assets/sprites/Sprite1/index.json": []byte(`{"costumes":[{"name":"Sprite1Costume"}]}`),
			"assets/sprites/Sprite2/index.json": []byte(`{"costumes":[{"name":"Sprite2Costume"}]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///Sprite1.spx"},
				Position:     Position{Line: 2, Character: 28},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "Sprite2Costume"))
	})

	t.Run("SpriteCostumeNameInImplicitCallUsesCurrentSprite", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(``),
			"Sprite1.spx": []byte(`
onStart => {
	setCostume C
}
`),
			"Sprite2.spx":                       []byte(``),
			"Sprite3.spx":                       []byte(``),
			"assets/index.json":                 []byte(`{}`),
			"assets/sprites/Sprite1/index.json": []byte(`{"costumes":[{"name":"Crab2"},{"name":"Crab3"}]}`),
			"assets/sprites/Sprite2/index.json": []byte(`{"costumes":[{"name":"Crab2"}]}`),
			"assets/sprites/Sprite3/index.json": []byte(`{"costumes":[{"name":"Crab2"}]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///Sprite1.spx"},
				Position:     Position{Line: 2, Character: 13},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Equal(t, 1, countCompletionItemLabel(items, `"Crab2"`))
		assert.Equal(t, 1, countCompletionItemLabel(items, `"Crab3"`))
	})

	t.Run("SpriteCostumeNameInDeclDeduplicatesCrossSpriteNames", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var costume SpriteCostumeName = C
}
`),
			"Sprite1.spx":                       []byte(``),
			"Sprite2.spx":                       []byte(``),
			"Sprite3.spx":                       []byte(``),
			"assets/index.json":                 []byte(`{}`),
			"assets/sprites/Sprite1/index.json": []byte(`{"costumes":[{"name":"Crab2"},{"name":"Crab3"}]}`),
			"assets/sprites/Sprite2/index.json": []byte(`{"costumes":[{"name":"Crab2"}]}`),
			"assets/sprites/Sprite3/index.json": []byte(`{"costumes":[{"name":"Crab2"}]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 34},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Equal(t, 1, countCompletionItemLabel(items, `"Crab2"`))
		assert.Equal(t, 1, countCompletionItemLabel(items, `"Crab3"`))
	})

	t.Run("StepToOverloadsDeduplicateSpriteNameSuggestions", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(``),
			"Runner.spx": []byte(`
onStart => {
	stepTo C
}
`),
			"Crab2.spx":                        []byte(``),
			"Crab3.spx":                        []byte(``),
			"assets/index.json":                []byte(`{}`),
			"assets/sprites/Runner/index.json": []byte(`{"costumes":[]}`),
			"assets/sprites/Crab2/index.json":  []byte(`{"costumes":[]}`),
			"assets/sprites/Crab3/index.json":  []byte(`{"costumes":[]}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///Runner.spx"},
				Position:     Position{Line: 2, Character: 10},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Equal(t, 1, countCompletionItemLabel(items, `"Crab2"`))
		assert.Equal(t, 1, countCompletionItemLabel(items, `"Crab3"`))
	})

	t.Run("AtLineStartWithAnIdentifier", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onClick => {
	pr
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 3},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "println"))
	})

	t.Run("AtLineStartWithAMemberAccessExpression", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
MySprite.setCo`), // Cursor at EOF.
			"MySprite.spx": []byte(`
onClick => {
	MySprite.setCo
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items1Result, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 14},
			},
		})
		require.NoError(t, err)
		items1 := items1Result.([]CompletionItem)
		require.NotNil(t, items1)
		assert.NotEmpty(t, items1)
		assert.True(t, containsCompletionItemLabel(items1, "setCostume"))

		items2Result, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 2, Character: 15},
			},
		})
		require.NoError(t, err)
		items2 := items2Result.([]CompletionItem)
		require.NotNil(t, items2)
		assert.NotEmpty(t, items2)
		assert.True(t, containsCompletionItemLabel(items2, "setCostume"))
	})

	t.Run("WithXGoBuiltins", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onClick => {
	var n in
}
`),
			"MySprite.spx": []byte(`
onClick => {
	ec
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		items1Result, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 9},
			},
		})
		require.NoError(t, err)
		items1 := items1Result.([]CompletionItem)
		require.NotNil(t, items1)
		assert.NotEmpty(t, items1)
		assert.True(t, containsCompletionItemLabel(items1, "int128"))

		items2Result, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 2, Character: 3},
			},
		})
		require.NoError(t, err)
		items2 := items2Result.([]CompletionItem)
		require.NotNil(t, items2)
		assert.NotEmpty(t, items2)
		assert.True(t, containsCompletionItemLabel(items2, "echo"))
	})

	t.Run("MainPackageInterfaceMethod", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Runner interface {
	Run()
}

type MyRunner struct {}
func (r *MyRunner) Run() {}

onStart => {}
	var r Runner = new(MyRunner)
	r.
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 10, Character: 3},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("Runner.Run"),
		}))
		assert.False(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MyRunner.Run"),
		}))
	})

	t.Run("MainPackageInterfaceMethodWithAlias", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Runner interface {
	Run()
}

type RunnerAlias = Runner

type MyRunner struct {}
func (r *MyRunner) Run() {}

onStart => {
	var r RunnerAlias = new(MyRunner)
	r.
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 12, Character: 3},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("Runner.Run"),
		}))
		assert.False(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MyRunner.Run"),
		}))
	})

	t.Run("NonMainPackageInterfaceMethod", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "fmt"

type MyStringer struct {}
func (s *MyStringer) String() string {}

onStart => {}
	var s fmt.Stringer = new(MyStringer)
	s.
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 3},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("fmt"),
			Name:    ToPtr("Stringer.string"),
		}))
		assert.False(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MyStringer.String"),
		}))
	})

	t.Run("MainPackageStructLiteralField", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Point struct {
	X int
	Y int
}

onStart => {
	p := Point{}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 7, Character: 12},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			itemData, ok := item.Data.(*CompletionItemData)
			if ok && itemData.Definition.String() == "xgo:main?Point.X" {
				assert.Equal(t, "X: ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
				return true
			}
			return false
		}))
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			itemData, ok := item.Data.(*CompletionItemData)
			if ok && itemData.Definition.String() == "xgo:main?Point.Y" {
				assert.Equal(t, "Y: ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
				return true
			}
			return false
		}))
	})

	t.Run("MainPackageStructLiteralFieldWithAlias", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Point struct {
	X int
	Y int
}

type PointAlias = Point

onStart => {
	p := PointAlias{}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 17},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			itemData, ok := item.Data.(*CompletionItemData)
			if ok && itemData.Definition.String() == "xgo:main?Point.X" {
				assert.Equal(t, "X: ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
				return true
			}
			return false
		}))
	})

	t.Run("MainPackageStructDotWithPointerAlias", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Point struct {
	X int
	Y int
}

type PointPtrAlias = *Point

onStart => {
	var p PointPtrAlias = new(Point)
	p.
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 10, Character: 3},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("Point.X"),
		}))
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("Point.Y"),
		}))
	})

	t.Run("NonMainPackageStructLiteralField", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "image/color"

onStart => {
	c := color.RGBA{}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 17},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			itemData, ok := item.Data.(*CompletionItemData)
			if ok && itemData.Definition.String() == "xgo:image/color?RGBA.R" {
				assert.Equal(t, "R: ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
				return true
			}
			return false
		}))
	})

	t.Run("UnresolvedFuncCall", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStar => {
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 1, Character: 6},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "onStart"))
	})

	t.Run("WithinIdentifier", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var abc bool
	if ab {
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 6},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "abc"))
	})

	t.Run("ErrorInterfaceMethodCall", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type myError struct{}

func (myError) Error() string {
	return "myError"
}

onStart => {
	var err error = myError{}
	echo err.
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 10},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "error"))
	})

	t.Run("StartWithInvalidChar", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
“”var (
	maps []int
)
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 10},
			},
		})
		require.NoError(t, err)
		var items []CompletionItem
		if itemsResult != nil {
			items = itemsResult.([]CompletionItem)
		}
		require.Nil(t, items)
		assert.Empty(t, items)
	})

	t.Run("MathPackage", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	n := ab
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 8},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "abs"))
	})

	t.Run("StructLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MyStruct struct {
	Foobar int
}

onStart => {
	ms := My
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 6, Character: 9},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "MyStruct"))
	})

	t.Run("StructLitFieldName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MyStruct struct {
	Foobar int
}

onStart => {
	ms := MyStruct{
		Fo
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 7, Character: 4},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "Foobar"))
	})

	t.Run("TypeAssertion", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MyStruct struct {
	Foobar int
}

onStart => {
	var i any = MyStruct{}
	_, ok := i.(My)
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 7, Character: 15},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "MyStruct"))
	})

	t.Run("NoCompletionAfterNumberLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var x = 123.
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 13}, // After "123."
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("NoCompletionAfterNumberLiteralInShortVarDecl", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	x := 123.
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 2, Character: 10}, // After "123."
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Empty(t, items)
	})

	t.Run("XGoStyleMapLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func printMap(m map[string]int) {
	echo m
}

onStart => {
	var foo int
	printMap {
		"foo": f
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 10}, // After "f"
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "foo"))
	})

	t.Run("XGoStyleMapLitWithMultipleValues", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func printMap(m map[string]string) {
	echo m
}

onStart => {
	var bar, baz string
	printMap {
		"first": bar,
		"second": b
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 13}, // After "b" in second value
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "bar"))
		assert.True(t, containsCompletionItemLabel(items, "baz"))
	})

	t.Run("XGoStyleNestedMapLit", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func processData(data map[string]map[string]int) {
	echo data
}

onStart => {
	var count int
	processData {
		"nested": {
			"value": c
		}
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 13}, // After "c" in nested map
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "count"))
	})

	t.Run("XGoMapLiteralWithoutType", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func printData(data any) {
	echo data
}

onStart => {
	var myVar string
	printData {
		"name": m
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 11}, // After "m" in map value
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "myVar"))
	})

	t.Run("RegularStructLitNotAffected", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Config struct {
	Name  string
	Value int
}

func setup(cfg Config) {
	echo cfg
}

onStart => {
	var myName string
	setup Config{
		Name: m
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 13, Character: 9}, // After "m" in struct field value
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "myName"))
	})

	t.Run("TypedMapLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var value int
	var data map[string]int
	data = map[string]int{
		"key": value
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 14}, // After "value" in map value
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
	})

	t.Run("TypedMapLiteralAsArgument", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func processMap(m map[string]int) {
	echo m
}

onStart => {
	var num int
	processMap map[string]int{
		"count": n
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 12}, // After "n" in map value
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "num"))
	})

	t.Run("StructLitFieldValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MyStruct struct {
	Field1 string
	Field2 int
}

onStart => {
	var s MyStruct
	s = MyStruct{
		F
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 3}, // After "F" in struct literal
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		if len(items) > 0 {
			hasField1 := containsCompletionItemLabel(items, "Field1")
			hasField2 := containsCompletionItemLabel(items, "Field2")
			assert.True(t, hasField1 || hasField2, "Should suggest at least one struct field")
		}
	})

	t.Run("SimpleReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getName() string {
	var str string = "myName"
	return s
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 9}, // After "s" in return
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "str"))
		assert.True(t, containsCompletionItemLabel(items, "string"))
	})

	t.Run("SimpleAssign", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var str string = "myName"
	str = s
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 8}, // After "s" in assignment
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "str"))
		assert.True(t, containsCompletionItemLabel(items, "string"))
	})

	t.Run("SimpleCallArg", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var str string = "myName"
	println(s)
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 10}, // After "s" in call argument
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "str"))
		assert.True(t, containsCompletionItemLabel(items, "string"))
	})

	t.Run("TypedMapLitInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func countFunc() int { return 42 }
func countFuncNoReturnValue() {}
func countFuncMultiReturnValues() (int, int) { return 0, 1 }

func getData() map[string]int {
	var count int = 42
	return map[string]int{
		"total": c
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 12}, // After "c" in map value
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "count"))
		assert.True(t, containsCompletionItemLabel(items, "countFunc"))
		assert.False(t, containsCompletionItemLabel(items, "countFuncNoReturnValue"))
		assert.False(t, containsCompletionItemLabel(items, "countFuncMultiReturnValues"))
	})

	t.Run("XGoStyleMapLitInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func appNameFunc() string { return "myApp" }
func appNameFuncNoReturnValue() {}
func appNameFuncMultiReturnValues() (string, string) { return "app1", "app2" }

func getConfig() map[string]string {
	var appName = "myApp"
	return {
		"name": a
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 11}, // After "a" in map value
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "appName"))
		assert.True(t, containsCompletionItemLabel(items, "appNameFunc"))
		assert.False(t, containsCompletionItemLabel(items, "appNameFuncNoReturnValue"))
		assert.False(t, containsCompletionItemLabel(items, "appNameFuncMultiReturnValues"))
	})

	t.Run("XGoStyleNestedMapLitInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getNestedData() map[string]map[string]int {
	var total int = 100
	return {
		"stats": {
			"count": t
		}
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 13}, // After "t" in nested map value
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "total"))
	})

	t.Run("MapLitInMultiReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getResult() (map[string]int, error) {
	var result int = 42
	return {
		"value": r
	}, nil
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 12}, // After "r" in map value
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "result"))
	})

	t.Run("TypedStructLitInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Person struct {
	Name string
	Age  int
}

func getPerson() Person {
	var myName = "Alice"
	var myAge = 25
	return Person{
		Name: m
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 10, Character: 9}, // After "m" in struct field value
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "myName"))
		assert.True(t, containsCompletionItemLabel(items, "myAge"))
	})

	t.Run("PointerStructLitInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Config struct {
	Host string
	Port int
}

func getConfig() *Config {
	var defaultHost = "localhost"
	var defaultPort = 8080
	return &Config{
		Host: d
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 10, Character: 9}, // After "d" in struct field value
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "defaultHost"))
		assert.True(t, containsCompletionItemLabel(items, "defaultPort"))
	})

	t.Run("FuncLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var myCallback = func(x int) int {
		var result = x * 2
		return r
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 10}, // After "r" in return statement
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "result"))
	})

	t.Run("FuncLiteralAsArgument", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func process(fn func(int) int) {
	echo fn(10)
}

onStart => {
	var multiplier = 3
	process func(x int) int {
		var product = x * multiplier
		return p
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 10}, // After "p" in return statement
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "product"))
		assert.False(t, containsCompletionItemLabel(items, "process"))
	})

	t.Run("SliceLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var first = 10
	var second = 20
	var nums = []int{
		f
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 3}, // After "f" in slice literal
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "first"))
	})

	t.Run("SliceLiteralInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getNumbers() []int {
	var num1 = 100
	var num2 = 200
	return []int{
		n
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 3}, // After "n" in slice literal
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "num1"))
		assert.True(t, containsCompletionItemLabel(items, "num2"))
	})

	t.Run("XGoStyleSliceLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func printSlice(s []string) {
	echo s
}

onStart => {
	var item1 = "hello"
	var item2 = "world"
	printSlice [
		i
	]
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 3}, // After "i" in slice literal
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "item1"))
		assert.True(t, containsCompletionItemLabel(items, "item2"))
	})

	t.Run("XGoStyleMatrixLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func printMatrix(m [][]string) {
	echo m
}

onStart => {
	var item1 = "hello"
	var item2 = "world"
	printMatrix [
		item1, i
		item2, ""
	]
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 10}, // After "i" in matrix literal
			},
		})
		require.NoError(t, err)
		items, ok := itemsResult.([]CompletionItem)
		require.True(t, ok)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "item1"))
		assert.True(t, containsCompletionItemLabel(items, "item2"))
	})

	t.Run("XGoStyleSliceLiteralInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getItems() []string {
	var item1 = "hello"
	var item2 = "world"
	return [
		i
	]
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 3}, // After "i" in slice literal
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "item1"))
		assert.True(t, containsCompletionItemLabel(items, "item2"))
	})

	t.Run("NestedSliceLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func processMatrix(m [][]int) {
	echo m
}

onStart => {
	var value = 42
	processMatrix [][]int{
		[]int{v},
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 9}, // After "v" in nested slice
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "value"))
	})

	t.Run("ArrayLiteral", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var element1 = "a"
	var element2 = "b"
	var arr = [3]string{
		e
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 3}, // After "e" in array literal
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "element1"))
		assert.True(t, containsCompletionItemLabel(items, "element2"))
	})

	t.Run("FuncLiteralInReturn", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getHandler() func(int) int {
	var factor = 5
	return func(x int) int {
		var result = x * factor
		return r
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 10}, // After "r" in inner return
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "result"))
	})

	t.Run("VarDeclWithValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var str string = "test"
	var x string = s
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 17}, // After "s" in var declaration with value
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "str"))
		assert.True(t, containsCompletionItemLabel(items, "string"))
	})

	t.Run("ConstDeclWithValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var str string = "test"
	const x = s
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 12}, // After "s" in const declaration
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)

		assert.True(t, containsCompletionItemLabel(items, "str"))
		assert.True(t, containsCompletionItemLabel(items, "string"))
	})

	t.Run("ShortVarDecl", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var str string = "test"
	x := s
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 7}, // After "s" in short var decl
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "str"))
		assert.True(t, containsCompletionItemLabel(items, "string"))
	})

	t.Run("MultipleReceiverAssignment", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getTwoValues() (string, int) { return "hello", 42 }
func getSingleValue() string { return "world" }
func getThreeValues() (string, int, bool) { return "test", 123, true }

onStart => {
	var x string
	var y int
	x, y = g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 9}, // After "g" in assignment
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "getTwoValues"), "Should suggest function returning (string, int)")
		assert.True(t, containsCompletionItemLabel(items, "getSingleValue"), "Should suggest single value functions for flexible use")
		assert.False(t, containsCompletionItemLabel(items, "getThreeValues"), "Should not suggest function returning three values")
	})

	t.Run("MultipleReceiverShortVarDecl", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getTwoInts() (int, int) { return 1, 2 }
func getTwoStrings() (string, string) { return "a", "b" }
func getSingleInt() int { return 42 }

onStart => {
	x, y := g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 6, Character: 10}, // After "g" in short var decl
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "getTwoInts"), "Should suggest functions returning two values (int, int)")
		assert.True(t, containsCompletionItemLabel(items, "getTwoStrings"), "Should suggest functions returning two values (string, string)")
		assert.True(t, containsCompletionItemLabel(items, "getSingleInt"), "Should suggest single value functions for flexible use")
	})

	t.Run("MultipleExpressionAssignment", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getInt() int { return 1 }
func getString() string { return "hello" }
func getTwoInts() (int, int) { return 1, 2 }

onStart => {
	var x int
	var y int
	x, y = getInt(), g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 19}, // After "g" in second expression
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "getInt"), "Should suggest function returning int for second position")
		assert.False(t, containsCompletionItemLabel(items, "getString"), "Should not suggest function returning string")
		assert.False(t, containsCompletionItemLabel(items, "getTwoInts"), "Should not suggest function returning multiple values")
	})

	t.Run("MultipleReceiverWithError", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "errors"

func getTwoValuesWithError() (string, error) { return "hello", nil }
func getSingleValueWithError() error { return nil }
func getThreeValues() (string, int, error) { return "test", 123, nil }

onStart => {
	var s string
	var err error
	s, err = g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 10, Character: 11}, // After "g" in assignment
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "getTwoValuesWithError"), "Should suggest function returning (string, error)")
		assert.False(t, containsCompletionItemLabel(items, "getSingleValueWithError"), "Should not suggest single value function with incompatible type")
		assert.False(t, containsCompletionItemLabel(items, "getThreeValues"), "Should not suggest function returning three values")
	})

	t.Run("NestedMultipleReceiverAssignment", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getTwoBools() (bool, bool) { return true, false }
func getSingleBool() bool { return true }

onStart => {
	if x, y := g; x && y {
		// Inside if statement with short var decl
	}
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 13}, // After "g" in if statement init
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.NotEmpty(t, items)
		assert.True(t, containsCompletionItemLabel(items, "getTwoBools"), "Should suggest function returning two bools")
		assert.True(t, containsCompletionItemLabel(items, "getSingleBool"), "Should suggest single value functions for flexible use")
	})

	t.Run("TypeConversion", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type UserID int
type OrderID int

func getUserID() UserID { return 123 }
func getOrderID() OrderID { return 456 }
func getInt() int { return 789 }

onStart => {
	var id int = g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 15}, // After "g"
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.True(t, containsCompletionItemLabel(items, "getInt"), "Should show exact type match")
		assert.True(t, containsCompletionItemLabel(items, "getUserID"), "Should show convertible type UserID")
		assert.True(t, containsCompletionItemLabel(items, "getOrderID"), "Should show convertible type OrderID")
	})

	t.Run("TypeConversionExclusion", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getPort() int { return 8080 }
func getHost() string { return "localhost" }

onStart => {
	var port int = g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 5, Character: 17}, // After "g" in int assignment
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		assert.True(t, containsCompletionItemLabel(items, "getPort"), "Should show int function")
		assert.False(t, containsCompletionItemLabel(items, "getHost"), "Should not suggest string→int conversion")
	})

	t.Run("SelfReferenceInValueExpression", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	var counter int = 10
	counter = counter + c
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 22}, // After "c"
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		assert.True(t, containsCompletionItemLabel(items, "counter"), "Should show counter in value expression")
	})

	t.Run("CombinedSingleReturns", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func getX() int { return 1 }
func getY() int { return 2 }
func getPair() (int, int) { return 1, 2 }

onStart => {
	var x, y int
	x, y = g
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 7, Character: 9}, // After "g"
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		assert.True(t, containsCompletionItemLabel(items, "getPair"), "Should show function with matching return count")
		assert.True(t, containsCompletionItemLabel(items, "getX"), "Should show single return for flexible use")
		assert.True(t, containsCompletionItemLabel(items, "getY"), "Should show single return for flexible use")
	})

	t.Run("SpriteInterfaceEmbedding", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type MyInterface interface {
	Sprite
	methodOne()
}

onStart => {
	var iface MyInterface = MySprite
	iface.on
}
`),
			"MySprite.spx": []byte(`
func methodOne() {}
onStart => {}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 9}, // After "n"
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		assert.True(t, containsCompletionItemLabel(items, "onClick"))
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("github.com/goplus/spx/v3"),
			Name:    ToPtr("Sprite.onClick"),
		}))
		assert.True(t, containsCompletionItemLabel(items, "methodOne"))
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MyInterface.methodOne"),
		}))
	})

	t.Run("PropertyNameCompletionInMainSpx", func(t *testing.T) {
		// showVar in main.spx → getPropertyTarget returns "Game"
		// → collectPropertyNames("Game") → property methods from embedded spx.Game appear
		m := map[string][]byte{
			"main.spx": []byte(`
var score int
onStart => {
	showVar(x)
}
`),
			"MySprite.spx":                       []byte(`onStart => {}`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 10}, // inside 'x' arg of showVar
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		// score is declared in main.spx and becomes a Game field.
		assert.True(t, containsCompletionItemLabel(items, `"score"`))
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("Game.score"),
		}))
		// Property method from embedded spx.Game.
		assert.True(t, containsCompletionItemLabel(items, `"volume"`))
	})

	t.Run("PropertyNameCompletionInSpriteSpx", func(t *testing.T) {
		// showVar in MySprite.spx → getPropertyTarget returns "MySprite" (not "Game").
		// hp is a field of MySprite, so its appearance confirms the correct target is used.
		m := map[string][]byte{
			"main.spx": []byte(`
`),
			"MySprite.spx": []byte(`
var hp int

onStart => {
	showVar(x)
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				// Line 4: "\tshowVar(x)" — tab(0)+showVar(1-7)+(8)+x(9)
				Position: Position{Line: 4, Character: 10},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		// hp is a direct field of MySprite — confirms target is "MySprite", not "Game".
		assert.True(t, containsCompletionItemLabel(items, `"hp"`))
	})

	t.Run("PropertyNameCompletionExplicitReceiver", func(t *testing.T) {
		// MySprite.showVar(x) in main.spx → getPropertyTarget returns "MySprite"
		m := map[string][]byte{
			"main.spx": []byte(`
onStart => {
	MySprite.showVar(x)
}
`),
			"MySprite.spx": []byte(`
var hp int
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				// Line 3: "\tMySprite.showVar(x)" — tab(0)+MySprite(1-8)+.(9)+showVar(10-16)+(17)+x(18)
				Position: Position{Line: 2, Character: 19},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.True(t, containsCompletionItemLabel(items, `"hp"`))
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("main"),
			Name:    ToPtr("MySprite.hp"),
		}))
	})

	t.Run("PropertyNameCompletionEmbeddedMethod", func(t *testing.T) {
		// SpriteImpl is embedded in MySprite; its property methods should appear.
		m := map[string][]byte{
			"main.spx": []byte(`
`),
			"MySprite.spx": []byte(`
var hp int

showVar(
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
				Position:     Position{Line: 3, Character: 8},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		// Direct field of MySprite.
		assert.True(t, containsCompletionItemLabel(items, `"hp"`))
		// Property method from embedded spx.SpriteImpl (e.g. "xpos" → "Xpos").
		assert.True(t, containsCompletionItemLabel(items, `"xpos"`))
		assert.True(t, containsCompletionSpxDefinitionID(items, SpxDefinitionIdentifier{
			Package: ToPtr("github.com/goplus/spx/v3"),
			Name:    ToPtr("Sprite.xpos"),
		}))
	})

	t.Run("PropertyNameCompletionInsideStringLit", func(t *testing.T) {
		// When cursor is inside a string literal, insert text should NOT be quoted.
		m := map[string][]byte{
			"main.spx": []byte(`
var score int

showVar("s
`),
			"assets/index.json": []byte(`{}`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 3, Character: 10},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		// Inside string literal: label/insertText is unquoted.
		assert.True(t, containsCompletionItemLabel(items, "score"))
		assert.False(t, containsCompletionItemLabel(items, `"score"`))
	})

	t.Run("ChainedCompletionAfterPackagePropertyLikeFunc", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "time"

onStart => {
	echo time.now.y
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		timeNowItemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 4, Character: 16},
			},
		})
		require.NoError(t, err)
		timeNowItems := timeNowItemsResult.([]CompletionItem)
		require.NotNil(t, timeNowItems)
		assert.True(t, containsCompletionItemLabel(timeNowItems, "year"))
		assert.False(t, containsCompletionItemLabel(timeNowItems, "Now"))
	})

	t.Run("ChainedCompletionAfterLocalPropertyLikeFunc", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
import "time"

func Now() time.Time {
	return time.now
}

onStart => {
	echo now.y
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		nowItemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 8, Character: 11},
			},
		})
		require.NoError(t, err)
		nowItems := nowItemsResult.([]CompletionItem)
		require.NotNil(t, nowItems)
		assert.True(t, containsCompletionItemLabel(nowItems, "year"))
		assert.False(t, containsCompletionItemLabel(nowItems, "Now"))
	})

	t.Run("KwargNameCompletion", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
	Count int
	Name string
}

func configure(opts Options?) {}

onStart => {
	configure cou = 1
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 13},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			return item.Label == "count" &&
				item.InsertText == "count = ${1:}" &&
				item.InsertTextFormat != nil &&
				*item.InsertTextFormat == SnippetTextFormat
		}))
		assert.True(t, containsCompletionItemLabel(items, "name"))
	})

	t.Run("NonOptionalKwargNameCompletion", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			code string
		}{
			{
				name: "BareIdent",
				code: `
type Options struct {
	Count int
	Name string
}

func configure(opts Options) {}

onStart => {
	configure cou
}
`,
			},
			{
				name: "KwargExpr",
				code: `
type Options struct {
	Count int
	Name string
}

func configure(opts Options) {}

onStart => {
	configure cou = 1
}
`,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				m := map[string][]byte{
					"main.spx": []byte(tt.code),
				}
				s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

				itemsResult, err := s.textDocumentCompletion(&CompletionParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
						Position:     Position{Line: 9, Character: 13},
					},
				})
				require.NoError(t, err)
				items := itemsResult.([]CompletionItem)
				require.NotNil(t, items)
				assert.True(t, containsCompletionItemLabel(items, "count"))
				assert.True(t, containsCompletionItemLabel(items, "name"))
			})
		}
	})

	t.Run("KwargNameCompletionSkipsLaterLocalFieldName", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
	Count int
	count string
}

func configure(opts Options?) {}

onStart => {
	configure cou = 1
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 13},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.Equal(t, 1, countCompletionItemLabel(items, "count"))
	})

	t.Run("KwargValueCompletion", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

onStart => {
	var count int
	configure count = cou
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 23},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.True(t, containsCompletionItemLabel(items, "count"))
	})

	t.Run("OverloadKwargNameCompletion", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Worker struct{}

type CountOptions struct {
	Count int
}

type NameOptions struct {
	Name string
}

var worker Worker

func (w *Worker) handleCount(opts CountOptions?) {}
func (w *Worker) handleName(opts NameOptions?) {}

func (Worker).handle = (
	(Worker).handleCount
	(Worker).handleName
)

onStart => {
	worker.handle cou = 1
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 22, Character: 18},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.True(t, containsCompletionItemLabel(items, "count"))
		assert.True(t, containsCompletionItemLabel(items, "name"))
	})

	t.Run("SpxStepToWithKwargNameCompletion", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			code      string
			character uint32
		}{
			{
				name: "BareIdent",
				code: `
onStart => {
	stepToWith "Red", s
}
`,
				character: 20,
			},
			{
				name: "KwargExpr",
				code: `
onStart => {
	stepToWith "Red", spe = 2
}
`,
				character: 21,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				m := map[string][]byte{
					"main.spx":                           []byte(``),
					"MySprite.spx":                       []byte(tt.code),
					"Red.spx":                            []byte(``),
					"assets/index.json":                  []byte(`{}`),
					"assets/sprites/MySprite/index.json": []byte(`{}`),
					"assets/sprites/Red/index.json":      []byte(`{}`),
				}
				s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

				itemsResult, err := s.textDocumentCompletion(&CompletionParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
						Position:     Position{Line: 2, Character: tt.character},
					},
				})
				require.NoError(t, err)
				items := itemsResult.([]CompletionItem)
				require.NotNil(t, items)
				assert.True(t, containsKwargCompletionItem(items, "speed", SpxDefinitionIdentifier{
					Package: ToPtr(SpxPkgPath),
					Name:    ToPtr("MotionOptions.Speed"),
				}))
				assert.True(t, containsKwargCompletionItem(items, "animation", SpxDefinitionIdentifier{
					Package: ToPtr(SpxPkgPath),
					Name:    ToPtr("MotionOptions.Animation"),
				}))
			})
		}
	})

	t.Run("OverloadKwargNameCompletionFiltersByPositionalArg", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Worker struct{}

type CountOptions struct {
	Count int
}

type NameOptions struct {
	Name string
}

var worker Worker

func (w *Worker) handleCount(prefix int, opts CountOptions?) {}
func (w *Worker) handleName(prefix string, opts NameOptions?) {}

func (Worker).handle = (
	(Worker).handleCount
	(Worker).handleName
)

onStart => {
	worker.handle "prefix", na = "x"
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 22, Character: 27},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.False(t, containsCompletionItemLabel(items, "count"))
		assert.True(t, containsCompletionItemLabel(items, "name"))
	})

	t.Run("OverloadKwargPositionalValueCompletionWithVariadicKwargParam", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Worker struct{}

type Options struct {
	Name string
}

var worker Worker

func (w *Worker) handleNumbers(opts Options?, values ...int) {}
func (w *Worker) handleString(prefix string, opts Options?) {}

func (Worker).handle = (
	(Worker).handleNumbers
	(Worker).handleString
)

onStart => {
	var count int
	var title string
	worker.handle co, name = "x"
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 20, Character: 17},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.True(t, containsCompletionItemLabel(items, "count"))
		assert.True(t, containsCompletionItemLabel(items, "title"))
	})

	t.Run("OverloadKwargValueCompletion", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Worker struct{}

type CountOptions struct {
	Count int
}

type NameOptions struct {
	Name string
}

var worker Worker

func (w *Worker) handleCount(opts CountOptions?) {}
func (w *Worker) handleName(opts NameOptions?) {}

func (Worker).handle = (
	(Worker).handleCount
	(Worker).handleName
)

onStart => {
	var count int
	var title string
	worker.handle count = cou
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 24, Character: 27},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.True(t, containsCompletionItemLabel(items, "count"))
		assert.False(t, containsCompletionItemLabel(items, "title"))
	})

	t.Run("EmptyKwargValueCompletion", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

onStart => {
	var count int
	var title string
	configure count =
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 10, Character: 18},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.True(t, containsCompletionItemLabel(items, "count"))
		assert.False(t, containsCompletionItemLabel(items, "title"))
	})

	t.Run("PositionalValueCompletionWithKwargs", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
func configure(opts map[string]string, values ...int) {}

onStart => {
	var count int
	var title string
	configure cou, name = "x"
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 6, Character: 14},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.True(t, containsCompletionItemLabel(items, "count"))
		assert.False(t, containsCompletionItemLabel(items, "title"))
	})

	t.Run("InterfaceKwargNameCompletion", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Client struct{}

type Params interface {
	MaxTokens(n int64) Params
	Temperature(v float64) Params
}

var client Client

func (c Client) Params() Params { return nil }

func (c Client) complete(prompt string, params Params?) {}

onStart => {
	client.complete "hi", maxT = 1
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 15, Character: 27},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		require.NotNil(t, items)
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			return item.Label == "maxTokens" &&
				item.InsertText == "maxTokens = ${1:}" &&
				item.InsertTextFormat != nil &&
				*item.InsertTextFormat == SnippetTextFormat
		}))
		assert.True(t, containsCompletionItemLabel(items, "temperature"))
	})

	t.Run("InterfaceKwargNameCompletionWithoutFactory", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Client struct{}

type Params interface {
	MaxTokens(n int64) Params
	Temperature(v float64) Params
}

var client Client

func (c Client) complete(prompt string, params Params?) {}

onStart => {
	client.complete "hi", maxT = 1
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 13, Character: 27},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		assert.False(t, containsCompletionItemLabel(items, "maxTokens"))
		assert.False(t, containsCompletionItemLabel(items, "temperature"))
	})

	t.Run("FreeFunctionInterfaceKwargNameCompletion", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Params interface {
	MaxTokens(n int64) Params
	Temperature(v float64) Params
}

func complete(prompt string, params Params?) {}

onStart => {
	complete "hi", maxT = 1
}
`),
		}
		s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

		itemsResult, err := s.textDocumentCompletion(&CompletionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 9, Character: 20},
			},
		})
		require.NoError(t, err)
		items := itemsResult.([]CompletionItem)
		assert.False(t, containsCompletionItemLabel(items, "maxTokens"))
		assert.False(t, containsCompletionItemLabel(items, "temperature"))
	})

	t.Run("XGoUnits", func(t *testing.T) {
		t.Run("CallArguments", func(t *testing.T) {
			s := newXGoUnitTestServer(xgoUnitCompletionSource)
			result, _, _, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
			require.NoError(t, err)
			require.Falsef(t, result.hasErrorSeverityDiagnostic, "%#v", result.diagnostics)

			durationItemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 14, Character: 7},
				},
			})
			require.NoError(t, err)
			durationItems := durationItemsResult.(CompletionList).Items
			assert.True(t, containsCompletionItemLabel(durationItems, "ms"))
			assert.True(t, containsCompletionItemLabel(durationItems, "s"))
			assert.True(t, containsCompletionItemLabel(durationItems, "m"))
			assert.True(t, containsCompletionItemLabel(durationItems, "\u00b5s"))
			assert.False(t, containsCompletionItemLabel(durationItems, "wait"))
			assertCompletionItemTextEdit(t, durationItems, "s", TextEdit{
				Range: Range{
					Start: Position{Line: 14, Character: 7},
					End:   Position{Line: 14, Character: 7},
				},
				NewText: "s",
			})
			assert.Equal(t, "1s", completionItemByLabel(durationItems, "s").FilterText)

			durationPartialItemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 15, Character: 8},
				},
			})
			require.NoError(t, err)
			durationPartialItems := durationPartialItemsResult.(CompletionList).Items
			assert.True(t, containsCompletionItemLabel(durationPartialItems, "ms"))
			assertCompletionItemTextEdit(t, durationPartialItems, "ms", TextEdit{
				Range: Range{
					Start: Position{Line: 15, Character: 7},
					End:   Position{Line: 15, Character: 8},
				},
				NewText: "ms",
			})
			assert.Equal(t, "1ms", completionItemByLabel(durationPartialItems, "ms").FilterText)

			distanceItemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 16, Character: 8},
				},
			})
			require.NoError(t, err)
			distanceItems := distanceItemsResult.(CompletionList).Items
			assert.Truef(t, containsCompletionItemLabel(distanceItems, "mm"), "%v", completionItemLabels(distanceItems))
			assert.Truef(t, containsCompletionItemLabel(distanceItems, "cm"), "%v", completionItemLabels(distanceItems))
			assert.False(t, containsCompletionItemLabel(distanceItems, "s"))
			assertCompletionItemTextEdit(t, distanceItems, "cm", TextEdit{
				Range: Range{
					Start: Position{Line: 16, Character: 7},
					End:   Position{Line: 16, Character: 8},
				},
				NewText: "cm",
			})
		})

		t.Run("FuncDecoratorArgument", func(t *testing.T) {
			s := newXGoUnitTestServer(`import "example.com/unit"

func withDistance(distance unit.Distance, fn func()) {}

@withDistance(1)
func run() {}
`)

			itemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 4, Character: 15},
				},
			})
			require.NoError(t, err)
			items := itemsResult.(CompletionList).Items
			assert.True(t, containsCompletionItemLabel(items, "mm"))
			assert.True(t, containsCompletionItemLabel(items, "cm"))
			assert.False(t, containsCompletionItemLabel(items, "s"))
		})

		t.Run("StructKwargUnsupported", func(t *testing.T) {
			s := newXGoUnitTestServer(xgoUnitCompletionSource)

			itemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 17, Character: 20},
				},
			})
			require.NoError(t, err)
			items := itemsResult.([]CompletionItem)
			assert.False(t, containsCompletionItemKind(items, UnitCompletion))
			assert.False(t, containsCompletionItemLabel(items, "s"))
		})

		t.Run("InterfaceKwarg", func(t *testing.T) {
			s := newXGoUnitTestServer(`import "time"

type Params interface {
	Delay(time.Duration) Params
}

type Client struct{}

var c Client

func (c *Client) Params() Params { return nil }
func (c *Client) Run(params Params) {}

onStart => {
	c.Run delay = 1
}
`)

			itemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 14, Character: 16},
				},
			})
			require.NoError(t, err)
			items := itemsResult.(CompletionList).Items
			assert.True(t, containsCompletionItemLabel(items, "ms"))
			assert.True(t, containsCompletionItemLabel(items, "s"))
			assert.False(t, containsCompletionItemLabel(items, "delay"))
		})

		t.Run("UnsupportedContexts", func(t *testing.T) {
			s := newXGoUnitTestServer(`import "time"

type Options struct {
	Delay time.Duration
}

func duration() time.Duration {
	return 1
}

onStart => {
	var delay time.Duration = 1
	delay = 1
	_ = Options{Delay: 1}
}
`)

			for _, tt := range []struct {
				name     string
				position Position
			}{
				{name: "Return", position: Position{Line: 7, Character: 9}},
				{name: "Var", position: Position{Line: 11, Character: 28}},
				{name: "Assign", position: Position{Line: 12, Character: 10}},
				{name: "StructField", position: Position{Line: 13, Character: 21}},
			} {
				t.Run(tt.name, func(t *testing.T) {
					itemsResult, err := s.textDocumentCompletion(&CompletionParams{
						TextDocumentPositionParams: TextDocumentPositionParams{
							TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
							Position:     tt.position,
						},
					})
					require.NoError(t, err)
					items := itemsResult.([]CompletionItem)
					assert.Falsef(t, containsCompletionItemKind(items, UnitCompletion), "%v", completionItemLabels(items))
				})
			}
		})

		t.Run("PointerUnsupported", func(t *testing.T) {
			s := newXGoUnitTestServer(`import "time"

func waitPtr(d *time.Duration) {}

onStart => {
	waitPtr 1
}
`)

			itemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 5, Character: 10},
				},
			})
			require.NoError(t, err)
			items := itemsResult.([]CompletionItem)
			assert.False(t, containsCompletionItemKind(items, UnitCompletion))
			assert.False(t, containsCompletionItemLabel(items, "s"))
		})

		t.Run("CurrentPackageUnsupported", func(t *testing.T) {
			s := newXGoUnitTestServer(`type Distance int

const XGou_Distance = "mm=1,cm=10,m=1000"

func move(d Distance) {}

onStart => {
	move 1
}
`)

			itemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 7, Character: 7},
				},
			})
			require.NoError(t, err)
			items := itemsResult.([]CompletionItem)
			assert.False(t, containsCompletionItemKind(items, UnitCompletion))
			assert.False(t, containsCompletionItemLabel(items, "m"))
		})

		t.Run("CurrentPackageAliasUnsupported", func(t *testing.T) {
			s := newXGoUnitTestServer(`type Seconds = float64

const XGou_Seconds = "s=1,ms=0.001"

func glide(s Seconds) {}

onStart => {
	glide 1
}
`)

			itemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 7, Character: 8},
				},
			})
			require.NoError(t, err)
			items := itemsResult.([]CompletionItem)
			assert.False(t, containsCompletionItemKind(items, UnitCompletion))
			assert.False(t, containsCompletionItemLabel(items, "ms"))
		})

		t.Run("ImportedAlias", func(t *testing.T) {
			s := newXGoUnitTestServer(`import "example.com/unit"

func glide(s unit.Seconds) {}

onStart => {
	glide 1
}
`)

			itemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 5, Character: 8},
				},
			})
			require.NoError(t, err)
			items := itemsResult.(CompletionList).Items
			assert.True(t, containsCompletionItemLabel(items, "s"))
			assert.True(t, containsCompletionItemLabel(items, "ms"))
			assert.False(t, containsCompletionItemLabel(items, "m"))
		})

		t.Run("SpxSeconds", func(t *testing.T) {
			m := map[string][]byte{
				"main.spx": []byte(`onStart => {
	wait 1
}
`),
				"assets/index.json": []byte(`{}`),
			}
			s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

			itemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 1, Character: 7},
				},
			})
			require.NoError(t, err)
			items := itemsResult.(CompletionList).Items
			assert.Truef(t, containsCompletionItemLabel(items, "s"), "%v", completionItemLabels(items))
			assert.Truef(t, containsCompletionItemLabel(items, "ms"), "%v", completionItemLabels(items))
			assert.Equal(t, "1s", completionItemByLabel(items, "s").FilterText)
			assert.Equal(t, "1ms", completionItemByLabel(items, "ms").FilterText)
			assert.False(t, containsCompletionItemLabel(items, "m"))
		})

		t.Run("ImportedAliasFallback", func(t *testing.T) {
			s := newXGoUnitTestServer(`import "example.com/unit"

func wait(d unit.Delay) {}

onStart => {
	wait 1
}
`)

			itemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 5, Character: 7},
				},
			})
			require.NoError(t, err)
			items := itemsResult.(CompletionList).Items
			assert.True(t, containsCompletionItemLabel(items, "ms"))
			assert.True(t, containsCompletionItemLabel(items, "s"))
			assert.False(t, containsCompletionItemLabel(items, "km"))
		})

		t.Run("ImportedAliasOverloads", func(t *testing.T) {
			s := newXGoUnitTestServer(`import "example.com/unit"

type Worker struct{}

var worker Worker

func (w *Worker) handleSeconds(v unit.Seconds) {}
func (w *Worker) handleMeters(v unit.Meters) {}

func (Worker).handle = (
	(Worker).handleSeconds
	(Worker).handleMeters
)

onStart => {
	worker.handle 1
}
`)

			itemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 15, Character: 16},
				},
			})
			require.NoError(t, err)
			items := itemsResult.(CompletionList).Items
			labels := completionItemLabels(items)
			assert.Truef(t, containsCompletionItemLabel(items, "ms"), "%v", labels)
			assert.Truef(t, containsCompletionItemLabel(items, "km"), "%v", labels)
		})

		t.Run("DoesNotSwallowGeneralItems", func(t *testing.T) {
			s := newXGoUnitTestServer(`func plain(n int) {}

onStart => {
	count := 1
	plain 1
}
`)

			itemsResult, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 4, Character: 8},
				},
			})
			require.NoError(t, err)
			items := itemsResult.([]CompletionItem)
			assert.False(t, containsCompletionItemKind(items, UnitCompletion))
			assert.True(t, containsCompletionItemLabel(items, "count"))
		})
	})

	t.Run("LSPResultShape", func(t *testing.T) {
		t.Run("CompleteArray", func(t *testing.T) {
			m := map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			}
			s := New(newProjectWithoutModTime(m), nil, fileMapGetter(m), &MockScheduler{})

			result, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 1, Character: 5},
				},
			})
			require.NoError(t, err)

			items := result.([]CompletionItem)
			assert.NotEmpty(t, items)
		})

		t.Run("IncompleteUnitList", func(t *testing.T) {
			s := newXGoUnitTestServer(xgoUnitCompletionSource)

			result, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 14, Character: 7},
				},
			})
			require.NoError(t, err)

			list := result.(CompletionList)
			assert.True(t, list.IsIncomplete)
			assert.True(t, containsCompletionItemLabel(list.Items, "s"))
			assert.Equal(t, "1s", completionItemByLabel(list.Items, "s").FilterText)
			assertCompletionItemTextEdit(t, list.Items, "s", TextEdit{
				Range: Range{
					Start: Position{Line: 14, Character: 7},
					End:   Position{Line: 14, Character: 7},
				},
				NewText: "s",
			})
		})

		t.Run("IncompleteUnitListUsesCurrentText", func(t *testing.T) {
			s := newXGoUnitTestServer(`import "time"

func wait(d time.Duration) {}

onStart => {
	wait 12
}
`)

			result, err := s.textDocumentCompletion(&CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 5, Character: 8},
				},
			})
			require.NoError(t, err)

			list := result.(CompletionList)
			assert.True(t, list.IsIncomplete)
			assert.Equal(t, "12s", completionItemByLabel(list.Items, "s").FilterText)
			assertCompletionItemTextEdit(t, list.Items, "s", TextEdit{
				Range: Range{
					Start: Position{Line: 5, Character: 8},
					End:   Position{Line: 5, Character: 8},
				},
				NewText: "s",
			})
		})
	})
}

func TestCompletionContextResolvePropertyLikeExprType(t *testing.T) {
	t.Run("NilIdentifierReturnsNil", func(t *testing.T) {
		ctx := newPropertyLikeTestCompletionContext(gotypes.NewPackage("main", "main"), nil, nil)

		assert.Nil(t, ctx.resolvePropertyLikeExprType(nil, nil))
		assert.Nil(t, ctx.resolvePropertyLikeExprType(&ast.Ident{}, nil))
	})

	t.Run("SignatureMatch", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(1), pkg, "Now", gotypes.Typ[gotypes.String])
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		got := ctx.resolvePropertyLikeExprType(ident, fun.Type())
		assert.Same(t, gotypes.Typ[gotypes.String], got)
	})

	t.Run("ValidNonPropertyLikeSignatureReturnsNil", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(1), pkg, "now", gotypes.Typ[gotypes.String])
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		got := ctx.resolvePropertyLikeExprType(ident, fun.Type())
		assert.Nil(t, got)
	})

	t.Run("ValidTypeWithoutResolvedObjectReturnsNil", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(1), pkg, "Now", gotypes.Typ[gotypes.String])
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), nil)

		got := ctx.resolvePropertyLikeExprType(ident, fun.Type())
		assert.Nil(t, got)
	})

	t.Run("InvalidTypeFallsBackToScopeWalk", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(20), pkg, "Now", gotypes.Typ[gotypes.String])
		pkg.Scope().Insert(fun)
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), nil)

		got := ctx.resolvePropertyLikeExprType(ident, nil)
		assert.Same(t, gotypes.Typ[gotypes.String], got)
	})
}

func TestCompletionContextResolvePropertyLikeFuncResultType(t *testing.T) {
	t.Run("NilIdentifierReturnsNil", func(t *testing.T) {
		ctx := newPropertyLikeTestCompletionContext(gotypes.NewPackage("main", "main"), nil, nil)

		assert.Nil(t, ctx.resolvePropertyLikeFuncResultType(nil))
		assert.Nil(t, ctx.resolvePropertyLikeFuncResultType(&ast.Ident{}))
	})

	t.Run("PackageScopeIgnoresDeclarationOrder", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(20), pkg, "Now", gotypes.Typ[gotypes.String])
		pkg.Scope().Insert(fun)
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), nil)

		got := ctx.resolvePropertyLikeFuncResultType(ident)
		assert.Same(t, gotypes.Typ[gotypes.String], got)
	})

	t.Run("LocalScopeSkipsLaterFunction", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		localScope := gotypes.NewScope(pkg.Scope(), token.NoPos, token.NoPos, "local")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(20), pkg, "Now", gotypes.Typ[gotypes.String])
		localScope.Insert(fun)
		ctx := newPropertyLikeTestCompletionContext(pkg, localScope, nil)

		got := ctx.resolvePropertyLikeFuncResultType(ident)
		assert.Nil(t, got)
	})

	t.Run("SkipsFunctionWithParams", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		sig := gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewVar(token.NoPos, nil, "v", gotypes.Typ[gotypes.String])),
			gotypes.NewTuple(gotypes.NewVar(token.NoPos, nil, "", gotypes.Typ[gotypes.String])),
			false,
		)
		fun := gotypes.NewFunc(token.Pos(1), pkg, "Now", sig)
		pkg.Scope().Insert(fun)
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), nil)

		got := ctx.resolvePropertyLikeFuncResultType(ident)
		assert.Nil(t, got)
	})

	t.Run("SkipsLowerCamelFunctionName", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(1), pkg, "now", gotypes.Typ[gotypes.String])
		pkg.Scope().Insert(fun)
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), nil)

		got := ctx.resolvePropertyLikeFuncResultType(ident)
		assert.Nil(t, got)
	})
}

func TestAdaptCompletionItemsForClient(t *testing.T) {
	for _, tt := range []struct {
		name         string
		capabilities CompletionClientCapabilities
		items        []CompletionItem
		want         []CompletionItem
	}{
		{
			name: "DowngradesUnsupportedSnippetAndKind",
			items: []CompletionItem{{
				Label:            "count",
				Kind:             ConstantCompletion,
				InsertText:       "count = ${1:}",
				InsertTextFormat: ToPtr(SnippetTextFormat),
				TextEdit: &Or_CompletionItem_textEdit{Value: TextEdit{
					Range: Range{
						Start: Position{Line: 1, Character: 2},
						End:   Position{Line: 1, Character: 4},
					},
					NewText: "count = ${1:}",
				}},
			}},
			want: []CompletionItem{{
				Label:            "count",
				Kind:             TextCompletion,
				InsertText:       "count",
				InsertTextFormat: ToPtr(PlainTextTextFormat),
				TextEdit: &Or_CompletionItem_textEdit{Value: TextEdit{
					Range: Range{
						Start: Position{Line: 1, Character: 2},
						End:   Position{Line: 1, Character: 4},
					},
					NewText: "count",
				}},
			}},
		},
		{
			name: "KeepsSnippetAndKindWithValueSet",
			capabilities: CompletionClientCapabilities{
				CompletionItem: protocol.ClientCompletionItemOptions{
					SnippetSupport: true,
				},
				CompletionItemKind: &protocol.ClientCompletionItemOptionsKind{
					ValueSet: []CompletionItemKind{TextCompletion},
				},
			},
			items: []CompletionItem{{
				Label:            "count",
				Kind:             ConstantCompletion,
				InsertText:       "count = ${1:}",
				InsertTextFormat: ToPtr(SnippetTextFormat),
			}},
			want: []CompletionItem{{
				Label:            "count",
				Kind:             ConstantCompletion,
				InsertText:       "count = ${1:}",
				InsertTextFormat: ToPtr(SnippetTextFormat),
			}},
		},
		{
			name: "DowngradesUnsupportedSnippetInsertReplaceEdit",
			items: []CompletionItem{{
				Label:            "move",
				Kind:             FunctionCompletion,
				InsertTextFormat: ToPtr(SnippetTextFormat),
				TextEdit: &Or_CompletionItem_textEdit{Value: InsertReplaceEdit{
					NewText: "move ${1:steps}",
					Insert:  Range{Start: Position{Line: 1, Character: 2}, End: Position{Line: 1, Character: 4}},
					Replace: Range{
						Start: Position{Line: 1, Character: 2},
						End:   Position{Line: 1, Character: 6},
					},
				}},
			}},
			want: []CompletionItem{{
				Label:            "move",
				Kind:             FunctionCompletion,
				InsertText:       "move",
				InsertTextFormat: ToPtr(PlainTextTextFormat),
				TextEdit: &Or_CompletionItem_textEdit{Value: InsertReplaceEdit{
					NewText: "move",
					Insert:  Range{Start: Position{Line: 1, Character: 2}, End: Position{Line: 1, Character: 4}},
					Replace: Range{
						Start: Position{Line: 1, Character: 2},
						End:   Position{Line: 1, Character: 6},
					},
				}},
			}},
		},
		{
			name: "KeepsInitialProtocolKindWithoutValueSet",
			items: []CompletionItem{{
				Label: "ref",
				Kind:  ReferenceCompletion,
			}},
			want: []CompletionItem{{
				Label: "ref",
				Kind:  ReferenceCompletion,
			}},
		},
		{
			name: "DowngradesEnumMemberForInitialProtocolClient",
			items: []CompletionItem{
				{Label: "Color", Kind: EnumCompletion},
				{Label: "Red", Kind: EnumMemberCompletion},
			},
			want: []CompletionItem{
				{Label: "Color", Kind: EnumCompletion},
				{Label: "Red", Kind: TextCompletion},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			adaptCompletionItemsForClient(tt.capabilities, tt.items)
			assert.Equal(t, tt.want, tt.items)
		})
	}
}

func newPropertyLikeTestCompletionContext(pkg *gotypes.Package, innermostScope *gotypes.Scope, uses map[*ast.Ident]gotypes.Object) *completionContext {
	if uses == nil {
		uses = make(map[*ast.Ident]gotypes.Object)
	}
	return &completionContext{
		typeInfo: &types.Info{
			Info: typesutil.Info{
				Types:      make(map[ast.Expr]gotypes.TypeAndValue),
				Defs:       make(map[*ast.Ident]gotypes.Object),
				Uses:       uses,
				Selections: make(map[*ast.SelectorExpr]*gotypes.Selection),
				Implicits:  make(map[ast.Node]gotypes.Object),
				Scopes:     make(map[ast.Node]*gotypes.Scope),
			},
			Pkg: pkg,
		},
		innermostScope: innermostScope,
	}
}

func newPropertyLikeTestFunc(pos token.Pos, pkg *gotypes.Package, name string, result gotypes.Type) *gotypes.Func {
	sig := gotypes.NewSignatureType(
		nil,
		nil,
		nil,
		nil,
		gotypes.NewTuple(gotypes.NewVar(token.NoPos, nil, "", result)),
		false,
	)
	return gotypes.NewFunc(pos, pkg, name, sig)
}

func containsCompletionItemLabel(items []CompletionItem, label string) bool {
	return slices.ContainsFunc(items, func(item CompletionItem) bool {
		return item.Label == label
	})
}

func countCompletionItemLabel(items []CompletionItem, label string) int {
	count := 0
	for _, item := range items {
		if item.Label == label {
			count++
		}
	}
	return count
}

func containsKwargCompletionItem(items []CompletionItem, label string, id SpxDefinitionIdentifier) bool {
	return slices.ContainsFunc(items, func(item CompletionItem) bool {
		if item.Label != label ||
			item.InsertText != label+" = ${1:}" ||
			item.InsertTextFormat == nil ||
			*item.InsertTextFormat != SnippetTextFormat {
			return false
		}
		itemData, ok := item.Data.(*CompletionItemData)
		if !ok {
			return false
		}
		return itemData.Definition.String() == id.String()
	})
}

func containsCompletionSpxDefinitionID(items []CompletionItem, id SpxDefinitionIdentifier) bool {
	return slices.ContainsFunc(items, func(item CompletionItem) bool {
		itemData, ok := item.Data.(*CompletionItemData)
		if !ok {
			return false
		}
		return itemData.Definition.String() == id.String()
	})
}
