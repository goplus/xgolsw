package server

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletionEnums(t *testing.T) {
	t.Run("SourceKinds", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
		}{
			{name: "XGo", filename: "main.xgo"},
			{name: "LegacyXGo", filename: "main.gop"},
			{name: "StandaloneClass", filename: "Record.gox"},
			{name: "ProjectClass", filename: "main_fixture.gox"},
			{name: "WorkClass", filename: "Worker_fixture.gox"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{tt.filename: []byte(`type Color const (
	// Red documentation.
	Red = iota
)
type Shade = Color
var color Shade = Red
echo color
`)}
				if tt.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := newTestServer(t, files)
				_, err := s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)

				items := completionItemsAt(t, s, tt.filename, Position{Line: 5, Character: 19})
				item := completionItemByLabel(items, "Red")
				require.NotNil(t, item)
				assert.Equal(t, 1, countCompletionItemLabel(items, "Red"))
				assert.Equal(t, EnumMemberCompletion, item.Kind)
				assert.Equal(t, "Red", item.InsertText)
				assert.Equal(t, ToPtr(PlainTextTextFormat), item.InsertTextFormat)
				data := requireValueAs[*CompletionItemData](t, item.Data)
				assert.Equal(t, "xgo:main?Red", data.Definition.String())
				require.NotNil(t, item.Documentation)
				doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, doc.Value, "Red documentation.")
			})
		}
	})

	t.Run("CrossFileDuplicateMembers", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			firstFile  string
			secondFile string
			useFile    string
		}{
			{name: "XGo", firstFile: "first.xgo", secondFile: "second.xgo", useFile: "main.xgo"},
			{name: "ProjectClass", firstFile: "first.xgo", secondFile: "second.xgo", useFile: "main_fixture.gox"},
			{name: "WorkClass", firstFile: "first.xgo", secondFile: "second.xgo", useFile: "Worker_fixture.gox"},
			{name: "ClassfileDeclarations", firstFile: "main_fixture.gox", secondFile: "Worker_fixture.gox", useFile: "main.xgo"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					tt.firstFile: []byte(`type First const (
	// First member documentation.
	Unknown = iota
)
`),
					tt.secondFile: []byte(`type Second const (
	// Second member documentation.
	Unknown = iota
)
`),
					tt.useFile: []byte(`func run(value Second) {
	switch value {
	case Unknown:
	}
}
`),
				}
				if tt.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := newTestServer(t, files)
				_, err := s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)

				items := completionItemsAt(t, s, tt.useFile, Position{Line: 2, Character: 8})
				item := completionItemByLabel(items, "Unknown")
				require.NotNil(t, item)
				assert.Equal(t, 1, countCompletionItemLabel(items, "Unknown"))
				assert.Equal(t, EnumMemberCompletion, item.Kind)
				assert.Equal(t, "Unknown", item.InsertText)
				require.NotNil(t, item.Documentation)
				doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, doc.Value, "Second member documentation.")
				assert.NotContains(t, doc.Value, "First member documentation.")
				assert.NotContains(t, completionItemLabels(items), "_Unknown_1")
				assert.NotContains(t, completionItemLabels(items), "_Unknown_2")
			})
		}
	})

	t.Run("ClassFieldContext", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			filename   string
			expression string
		}{
			{name: "ProjectAppend", filename: "main_fixture.gox", expression: "\t_ = append(values, Un)"},
			{name: "WorkAppend", filename: "Worker_fixture.gox", expression: "\t_ = append(values, Un)"},
			{name: "ProjectSend", filename: "main_fixture.gox", expression: "\tvalues <- Un"},
			{name: "WorkSend", filename: "Worker_fixture.gox", expression: "\tvalues <- Un"},
			{name: "ProjectCopy", filename: "main_fixture.gox", expression: "\t_ = copy([Un], values)"},
			{name: "WorkCopy", filename: "Worker_fixture.gox", expression: "\t_ = copy([Un], values)"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				prefix := `type First const (
	// First member documentation.
	Unknown = iota
)
type Second const (
	// Second member documentation.
	Unknown = iota
)
var values []Second
func run() {
`
				files := map[string][]byte{"main_fixture.gox": nil}
				files[tt.filename] = []byte(prefix + tt.expression + "\n}\n")
				s := newTestServer(t, files)
				items := completionItemsAt(t, s, tt.filename, Position{
					Line:      uint32(strings.Count(prefix, "\n")),
					Character: uint32(UTF16Len(tt.expression[:strings.Index(tt.expression, "Un")+len("Un")])),
				})
				item := completionItemByLabel(items, "Unknown")
				require.NotNil(t, item)
				assert.Equal(t, EnumMemberCompletion, item.Kind)
				require.NotNil(t, item.Documentation)
				doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, doc.Value, "Second member documentation.")
				assert.NotContains(t, doc.Value, "First member documentation.")
			})
		}
	})

	t.Run("CrossFileDocumentUpdates", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"enums.xgo": []byte(`type Color const (
	Ready = iota
	// Red documentation.
	Red
)
`),
			"main_fixture.gox": []byte("var color Color = Ready\n"),
		})
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		items := completionItemsAt(t, s, "main_fixture.gox", Position{Character: 19})
		before := completionItemByLabel(items, "Red")
		require.NotNilf(t, before, "%v", completionItemLabels(items))
		require.NotNil(t, before.Documentation)
		beforeDoc := requireValueAs[MarkupContent](t, before.Documentation.Value)
		assert.Contains(t, beforeDoc.Value, "Red documentation.")

		s.ModifyFiles([]FileChange{{
			Path: "enums.xgo",
			Content: []byte(`type Color const (
	Ready = iota
	// Blue documentation.
	Blue
)
`),
			Version: 1,
		}})
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		items = completionItemsAt(t, s, "main_fixture.gox", Position{Character: 19})
		assert.NotContains(t, completionItemLabels(items), "Red")
		after := completionItemByLabel(items, "Blue")
		require.NotNilf(t, after, "%v", completionItemLabels(items))
		assert.Equal(t, EnumMemberCompletion, after.Kind)
		assert.Equal(t, "Blue", after.InsertText)
		require.NotNil(t, after.Documentation)
		afterDoc := requireValueAs[MarkupContent](t, after.Documentation.Value)
		assert.Contains(t, afterDoc.Value, "Blue documentation.")
		assert.NotContains(t, afterDoc.Value, "Red documentation.")
	})

	t.Run("UTF16Position", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("type Color const (\r\n\tRed = iota\r\n)\r\nfunc use(string, Color) {}\r\nuse \"\U0001f600\", Red\r\n"),
		})
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 4, Character: 11})
		item := completionItemByLabel(items, "Red")
		require.NotNil(t, item)
		assert.Equal(t, EnumMemberCompletion, item.Kind)
	})

	t.Run("EnumType", func(t *testing.T) {
		files := map[string][]byte{
			"main.xgo": []byte(`type TrafficLight const (
	Red = iota
)

type Signal = TrafficLight

func run() {
	var light Tra
	var signal Sig
}
`),
		}
		for _, tt := range []struct {
			name     string
			position Position
			label    string
		}{
			{name: "Declaration", position: Position{Line: 7, Character: 14}, label: "TrafficLight"},
			{name: "Alias", position: Position{Line: 8, Character: 15}, label: "Signal"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, files)
				items := completionItemsAt(t, s, "main.xgo", tt.position)
				item := completionItemByLabel(items, tt.label)
				require.NotNilf(t, item, "%v", completionItemLabels(items))
				assert.Equal(t, EnumCompletion, item.Kind)
			})
		}
	})

	t.Run("EnumValue", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`const Result = "text"

func run() {
	type Color const (
		// Red documentation.
		Red = iota
		Green
	)

	var color Color = R
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 20})
		item := completionItemByLabel(items, "Red")
		require.NotNilf(t, item, "%v", completionItemLabels(items))
		assert.Equal(t, EnumMemberCompletion, item.Kind)
		require.NotNil(t, item.Documentation)
		documentation := requireValueAs[MarkupContent](t, item.Documentation.Value)
		assert.Contains(t, documentation.Value, "Red documentation.")
		assert.NotContains(t, completionItemLabels(items), "Result")
	})

	t.Run("StringEnumValueForIndex", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`type Word const (
	Hello = "hello"
)

func run() {
	var first byte = H[0]
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 5, Character: 19})
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
			{name: "UnicodeIdentifier", assignment: "\tvar caf\u00e9 Size = R", wantLabel: "Small"},
			{name: "ConvertibleBasic", assignment: "\tvar number float64 = R"},
			{name: "DereferenceOperand", assignment: "\tvar color Color = *R"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte(sourcePrefix + tt.assignment + "\n}\n"),
				})

				items := completionItemsAt(t, s, "main.xgo", Position{
					Line:      uint32(strings.Count(sourcePrefix, "\n")),
					Character: uint32(UTF16Len(tt.assignment)),
				})
				assert.NotContains(t, completionItemLabels(items), "Red")
				if tt.wantLabel != "" {
					assert.Contains(t, completionItemLabels(items), tt.wantLabel)
				}
			})
		}
	})

	t.Run("EnumSwitchCase", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`type First const (
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
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 12, Character: 7})
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
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`type Color const (
	_ = iota
	Red
)

func run() {
	var color Color =
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 6, Character: 19})
		assert.NotContains(t, completionItemLabels(items), "_")
		assert.Contains(t, completionItemLabels(items), "Red")
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
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte(sourcePrefix + tt.assignment + "\n}\n"),
				})

				items := completionItemsAt(t, s, "main.xgo", tt.position)
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
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`type Color const (
	// Enum documentation.
	Red = iota
)

func run() {
	var Red Color
	var color Color = R
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 7, Character: 20})
		item := completionItemByLabel(items, "Red")
		require.NotNilf(t, item, "%v", completionItemLabels(items))
		assert.Equal(t, VariableCompletion, item.Kind)
		require.NotNil(t, item.Documentation)
		documentation := requireValueAs[MarkupContent](t, item.Documentation.Value)
		assert.NotContains(t, documentation.Value, "Enum documentation.")
	})

	t.Run("EnumValueForBroadAssignmentTarget", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`type Color const (
	Red = iota
)

func run() {
	var declared any = R
	var assigned any
	assigned = R
}
`),
		})

		for _, position := range []Position{
			{Line: 5, Character: 21},
			{Line: 7, Character: 13},
		} {
			items := completionItemsAt(t, s, "main.xgo", position)
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
			{name: "UnicodeStringComparison", expression: "\t_ = \"\U0001f600\" < I", wantLabels: []string{"StringValue"}},
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
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte(sourcePrefix + tt.expression + "\n}\n"),
				})

				items := completionItemsAt(t, s, "main.xgo", Position{
					Line:      uint32(strings.Count(sourcePrefix, "\n")),
					Character: uint32(UTF16Len(tt.expression[:strings.Index(tt.expression, "I")+len("I")])),
				})
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
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`type Color const (
	Red = iota
)

func use(*Color) {}

func run() {
	use(R)
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 7, Character: 6})
		assert.NotContains(t, completionItemLabels(items), "Red")
	})

	t.Run("EnumValueForPointerConversionTarget", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`type Color const (
	Red = iota
)

type ColorPtr = *Color

func run() {
	_ = ColorPtr(R)
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 7, Character: 15})
		assert.NotContains(t, completionItemLabels(items), "Red")
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
			{name: "UnicodeComment", expression: "/* \U0001f600 */ R"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				line := "\t_ = Size(" + tt.expression + ")"
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte(sourcePrefix + line + "\n}\n"),
				})

				items := completionItemsAt(t, s, "main.xgo", Position{
					Line:      uint32(strings.Count(sourcePrefix, "\n")),
					Character: uint32(UTF16Len(line[:strings.Index(line, "R")+len("R")])),
				})
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
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte(sourcePrefix + tt.assignment + "\n}\n"),
				})

				items := completionItemsAt(t, s, "main.xgo", tt.position)
				item := completionItemByLabel(items, "Unknown")
				require.NotNilf(t, item, "%v", completionItemLabels(items))
				assert.Equal(t, 1, countCompletionItemLabel(items, "Unknown"))
				assert.Equal(t, EnumMemberCompletion, item.Kind)
				require.NotNil(t, item.Documentation)
				documentation := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, documentation.Value, tt.wantDoc)
				assert.NotContains(t, documentation.Value, tt.unwantedDoc)
				assert.NotContains(t, completionItemLabels(items), "_Unknown_1")
				assert.NotContains(t, completionItemLabels(items), "_Unknown_2")
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
			{name: "UnicodeComment", expression: "\t_ = []Second{/* \U0001f600 */ Un}"},
			{name: "BinaryExpression", expression: "\t_ = second == Un"},
			{name: "BinaryWithUntypedOperand", expression: "\tvar value Second = Un + 1"},
			{name: "UnaryExpression", expression: "\tvar value Second = +Un"},
			{name: "MapIndex", expression: "\t_ = values[Un]"},
			{name: "Send", expression: "\tch <- Un"},
			// Shift counts and slice bounds accept either integer enum, so both member docs are expected.
			{name: "ShiftCount", expression: "\t_ = second << Un", wantFirstDoc: true},
			{name: "SliceBound", expression: "\tvar slice []int = []int{1}[Un:]", wantFirstDoc: true},
			{name: "XGoSliceLiteral", expression: "\tuseSlice([Un])"},
			{name: "XGoMatrixLiteral", expression: "\tuseMatrix([Un; Unknown])"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte(sourcePrefix + tt.expression + "\n}\n"),
				})

				items := completionItemsAt(t, s, "main.xgo", Position{
					Line:      uint32(strings.Count(sourcePrefix, "\n")),
					Character: uint32(UTF16Len(tt.expression[:strings.Index(tt.expression, "Un")+len("Un")])),
				})
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
				prefix := source[:offset+2]
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})

				items := completionItemsAt(t, s, "main.xgo", Position{
					Line:      uint32(strings.Count(prefix, "\n")),
					Character: uint32(UTF16Len(prefix[strings.LastIndex(prefix, "\n")+1:])),
				})
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
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`type First const (
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
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 18, Character: 7})
		item := completionItemByLabel(items, "Unknown")
		require.NotNilf(t, item, "%v", completionItemLabels(items))
		require.NotNil(t, item.Documentation)
		documentation := requireValueAs[MarkupContent](t, item.Documentation.Value)
		assert.Contains(t, documentation.Value, "First member documentation.")
		assert.Contains(t, documentation.Value, "Second member documentation.")
	})
}
