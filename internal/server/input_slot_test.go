package server

import (
	"encoding/json"
	gotypes "go/types"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/goplus/mod/xgomod"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerXGoGetInputSlots(t *testing.T) {
	t.Run("FuncDecorator", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`func withCount(count int, fn func()) {}

@withCount(3)
func run() {}
`),
		}
		s := newTestServer(t, m)

		inputSlots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		}})
		require.NoError(t, err)

		slot := findInputSlot(inputSlots, int64(3), "", XGoInputTypeInteger, XGoInputKindInPlace)
		require.NotNil(t, slot)
		assert.Equal(t, XGoInputTypeInteger, slot.Accept.Type)
		assert.Equal(t, Range{
			Start: Position{Line: 2, Character: 11},
			End:   Position{Line: 2, Character: 12},
		}, slot.Range)
	})

	t.Run("PartialXGoxFunction", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`import "example.com/typeargs"

func main() {
	println typeargs.convert(string, 100)
}
`),
		}
		s := newTestServer(t, m)
		s.workspaceRootFS.Importer = xgoxTestImporter{fallback: s.workspaceRootFS.Importer}

		inputSlots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
		}})
		require.NoError(t, err)

		slot := findInputSlot(inputSlots, int64(100), "", XGoInputTypeInteger, XGoInputKindInPlace)
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
			"main.xgo": []byte(`
// Missing closing parenthesis.
var (
	count     int
	message   string
`),
		}
		s := newTestServer(t, m)

		params := []XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}}
		inputSlots, err := s.xgoGetInputSlots(params)
		require.NoError(t, err)
		assert.Nil(t, inputSlots)
	})

	t.Run("IncompleteRange", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			body string
		}{
			{name: "MissingClosingBraces", body: "for i := range [1] {"},
			{name: "MissingOpeningBrace", body: "for i := range [1]"},
			{name: "MissingRangeExpression", body: "for i := range"},
			{name: "PartialLoopBody", body: "for i, v := range [1] {\nprintln i, v\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := "func main() {\nprintln 99\n" + tt.body
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				astFile, err := s.workspaceRootFS.ASTFile("main.xgo")
				require.Error(t, err)
				require.NotNil(t, astFile)
				var loops []*ast.RangeStmt
				ast.Inspect(astFile, func(node ast.Node) bool {
					if loop, ok := node.(*ast.RangeStmt); ok {
						loops = append(loops, loop)
					}
					return true
				})
				require.Len(t, loops, 1)
				require.NotNil(t, loops[0].Body)

				slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				}})
				require.NoError(t, err)
				slot := findInputSlot(slots, int64(99), "", XGoInputTypeInteger, XGoInputKindInPlace)
				require.NotNil(t, slot)
				assert.Equal(t, Range{
					Start: Position{Line: 1, Character: 8},
					End:   Position{Line: 1, Character: 10},
				}, slot.Range)
			})
		}
	})

	t.Run("EmptyFile", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(``),
		}
		s := newTestServer(t, m)

		params := []XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}}
		inputSlots, err := s.xgoGetInputSlots(params)
		require.NoError(t, err)
		assert.Empty(t, inputSlots)
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		m := map[string][]byte{}
		s := newTestServer(t, m)

		params := []XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///nonexistent.xgo"}}}
		inputSlots, err := s.xgoGetInputSlots(params)
		require.NoError(t, err)
		assert.Nil(t, inputSlots)
	})

	t.Run("MultipleParams", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`var a = 1`),
		}
		s := newTestServer(t, m)

		params := []XGoGetInputSlotsParams{
			{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}},
			{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}},
		}
		inputSlots, err := s.xgoGetInputSlots(params)
		require.Error(t, err)
		assert.Nil(t, inputSlots)
		assert.ErrorContains(t, err, "only supports one document")
	})

	t.Run("EmptyParams", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`var a = 1`),
		}
		s := newTestServer(t, m)

		params := []XGoGetInputSlotsParams{}
		inputSlots, err := s.xgoGetInputSlots(params)
		require.NoError(t, err)
		assert.Nil(t, inputSlots)
	})

	t.Run("IncompleteMethodDeclaration", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Foo struct {
    bar string
}

func (Foo) Bar`),
		}
		s := newTestServer(t, m)

		params := []XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}}

		var (
			inputSlots []XGoInputSlot
			err        error
		)
		assert.NotPanics(t, func() {
			inputSlots, err = s.xgoGetInputSlots(params)
		})
		require.NoError(t, err)
		assert.Nil(t, inputSlots)
	})

	t.Run("KwargValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

func main() {
	configure count = 5
}
`),
		}
		s := newTestServer(t, m)

		params := []XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}}
		inputSlots, err := s.xgoGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)

		slot := findInputSlot(inputSlots, int64(5), "", XGoInputTypeInteger, XGoInputKindInPlace)
		require.NotNil(t, slot)
		assert.Equal(t, XGoInputSlotKindValue, slot.Kind)
		assert.Equal(t, XGoInputTypeInteger, slot.Accept.Type)
		assert.Equal(t, XGoInputKindInPlace, slot.Input.Kind)
		assert.Equal(t, int64(5), slot.Input.Value)
	})

	t.Run("OverloadKwargValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Worker struct{}

type Options struct {
	Count int
}

var worker Worker

func (w *Worker) handleCount(opts Options?) {}

func (Worker).handle = (
	(Worker).handleCount
)

func main() {
	worker.handle count = 5
}
`),
		}
		s := newTestServer(t, m)

		params := []XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}}
		inputSlots, err := s.xgoGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)

		slot := findInputSlot(inputSlots, int64(5), "", XGoInputTypeInteger, XGoInputKindInPlace)
		require.NotNil(t, slot)
		assert.Equal(t, XGoInputSlotKindValue, slot.Kind)
		assert.Equal(t, XGoInputTypeInteger, slot.Accept.Type)
		assert.Equal(t, XGoInputKindInPlace, slot.Input.Kind)
		assert.Equal(t, int64(5), slot.Input.Value)
	})

	t.Run("UnknownKwargValue", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

func main() {
	configure count = 5
	configure unknown = 9
}
`),
		}
		s := newTestServer(t, m)

		params := []XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}}
		inputSlots, err := s.xgoGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)

		countSlot := findInputSlot(inputSlots, int64(5), "", XGoInputTypeInteger, XGoInputKindInPlace)
		require.NotNil(t, countSlot)
		assert.Equal(t, XGoInputSlotKindValue, countSlot.Kind)

		unknownSlot := findInputSlot(inputSlots, int64(9), "", XGoInputTypeInteger, XGoInputKindInPlace)
		assert.Nil(t, unknownSlot)
	})

	t.Run("XGoUnitValue", func(t *testing.T) {
		s := newXGoUnitTestServer(t, `import "time"

func wait(d time.Duration) {}

func main() {
	wait 1m
}
`)

		params := []XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}}
		inputSlots, err := s.xgoGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)

		slot := findInputSlotByRange(inputSlots, Range{
			Start: Position{Line: 5, Character: 6},
			End:   Position{Line: 5, Character: 7},
		})
		require.NotNil(t, slot)
		assert.Equal(t, XGoInputSlotKindValue, slot.Kind)
		assert.Equal(t, XGoInputTypeInteger, slot.Accept.Type)
		assert.Equal(t, XGoInputKindInPlace, slot.Input.Kind)
		assert.Equal(t, XGoInputTypeInteger, slot.Input.Type)
		assert.Equal(t, int64(1), slot.Input.Value)
	})

	t.Run("XGoUnitImportedAliasFallbackValue", func(t *testing.T) {
		s := newXGoUnitTestServer(t, `import "example.com/unit"

func wait(d unit.Delay) {}

func main() {
	wait 1ms
}
`)

		params := []XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}}
		inputSlots, err := s.xgoGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)

		slot := findInputSlotByRange(inputSlots, Range{
			Start: Position{Line: 5, Character: 6},
			End:   Position{Line: 5, Character: 7},
		})
		require.NotNil(t, slot)
		assert.Equal(t, XGoInputSlotKindValue, slot.Kind)
		assert.Equal(t, XGoInputTypeInteger, slot.Accept.Type)
		assert.Equal(t, XGoInputKindInPlace, slot.Input.Kind)
		assert.Equal(t, XGoInputTypeInteger, slot.Input.Type)
		assert.Equal(t, int64(1), slot.Input.Value)
	})

	t.Run("XGoUnitInterfaceKwargValue", func(t *testing.T) {
		s := newXGoUnitTestServer(t, `import "time"

type Params interface {
	Delay(time.Duration) Params
}

type Client struct{}

var c Client

func (c *Client) Params() Params { return nil }
func (c *Client) Run(params Params) {}

func main() {
	c.Run delay = 1ms
}
`)

		params := []XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}}
		inputSlots, err := s.xgoGetInputSlots(params)
		require.NoError(t, err)
		require.NotNil(t, inputSlots)

		slot := findInputSlotByRange(inputSlots, Range{
			Start: Position{Line: 14, Character: 15},
			End:   Position{Line: 14, Character: 16},
		})
		require.NotNil(t, slot)
		assert.Equal(t, XGoInputSlotKindValue, slot.Kind)
		assert.Equal(t, XGoInputTypeInteger, slot.Accept.Type)
		assert.Equal(t, XGoInputKindInPlace, slot.Input.Kind)
		assert.Equal(t, XGoInputTypeInteger, slot.Input.Type)
		assert.Equal(t, int64(1), slot.Input.Value)
	})

	t.Run("XGoUnitUnsupportedContexts", func(t *testing.T) {
		s := newXGoUnitTestServer(t, `import "time"

type Options struct {
	Delay time.Duration
}

func waitPtr(d *time.Duration) {}
func configure(opts *Options) {}

func duration() time.Duration {
	return 1m
}

func main() {
	waitPtr 1m
	configure delay = 1m
	var delay time.Duration = 1m
	delay = 1m
}
`)

		params := []XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}}
		inputSlots, err := s.xgoGetInputSlots(params)
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
			{name: "OtherFrameworkWithSpxExtension", filename: "main.spx"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{tt.filename: []byte("var Count int\nCount = 5\n")}
				if tt.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := newTestServer(t, files)
				if tt.name == "OtherFrameworkWithSpxExtension" {
					s.workspaceRootFS.Mod = xgomod.New(modload.Module{
						Opt: &modfile.File{Projects: []*modfile.Project{{
							Ext: ".spx", FullExt: "main.spx", Class: "App", PkgPaths: []string{testframework.PkgPath},
							Works: []*modfile.Class{{Ext: ".spx", Class: "Item", Embedded: true}},
						}}},
					})
					require.NoError(t, s.workspaceRootFS.Mod.ImportClasses())
				}
				_, err := s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)
				slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
				}})
				require.NoError(t, err)
				require.Len(t, slots, 2)
				assert.Equal(t, XGoInputSlotKindAddress, slots[0].Kind)
				assert.Equal(t, "Count", slots[0].Input.Name)
				assert.Equal(t, XGoInput{Kind: XGoInputKindInPlace, Type: XGoInputTypeInteger, Value: int64(5)}, slots[1].Input)
				assert.Equal(t, XGoInputSlotAccept{Type: XGoInputTypeInteger}, slots[1].Accept)
				assert.Equal(t, Range{Start: Position{Line: 1, Character: 8}, End: Position{Line: 1, Character: 9}}, slots[1].Range)
				assert.Contains(t, slots[1].PredefinedNames, "Count")
			})
		}
	})

	t.Run("CrossFileKwargs", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo":    []byte("configure count = 5\n"),
			"options.xgo": []byte("type Options struct { Count int }\nfunc configure(options Options?) {}\n"),
		})
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
		require.NoError(t, err)
		require.Len(t, slots, 1)
		assert.Equal(t, XGoInputTypeInteger, slots[0].Accept.Type)
		assert.Equal(t, int64(5), slots[0].Input.Value)
		assert.Equal(t, Range{Start: Position{Line: 0, Character: 18}, End: Position{Line: 0, Character: 19}}, slots[0].Range)
	})

	t.Run("DocumentUpdates", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("println 11\n")})
		params := []XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}}
		before, err := s.xgoGetInputSlots(params)
		require.NoError(t, err)
		require.Len(t, before, 1)
		assert.Equal(t, int64(11), before[0].Input.Value)
		s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte("println 22\nprintln missing\n"), Version: 1}})
		after, err := s.xgoGetInputSlots(params)
		require.NoError(t, err)
		_, err = s.workspaceRootFS.TypeInfo()
		require.Error(t, err)
		require.NotNil(t, findInputSlot(after, int64(22), "", XGoInputTypeInteger, XGoInputKindInPlace))
		assert.Nil(t, findInputSlot(after, int64(11), "", XGoInputTypeInteger, XGoInputKindInPlace))
	})

	t.Run("ExecuteCommand", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			command string
		}{
			{name: "XGo", command: CommandXGoGetInputSlots},
			{name: "Spx", command: CommandSpxGetInputSlots},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte("println 5\n")})
				result, err := s.workspaceExecuteCommand(&ExecuteCommandParams{
					Command: tt.command, Arguments: []json.RawMessage{json.RawMessage(`{"textDocument":{"uri":"file:///main.xgo"}}`)},
				})
				require.NoError(t, err)
				slots := requireValueAs[[]XGoInputSlot](t, result)
				require.Len(t, slots, 1)
				encoded, err := json.Marshal(slots[0])
				require.NoError(t, err)
				assert.JSONEq(t, `{"range":{"start":{"line":0,"character":8},"end":{"line":0,"character":9}},"kind":"value","accept":{"type":"unknown"},"input":{"kind":"in-place","type":"integer","value":5},"predefinedNames":[]}`, string(encoded))
			})
		}
	})

	t.Run("InvalidCommandArguments", func(t *testing.T) {
		s := newTestServer(t, nil)
		result, err := s.workspaceExecuteCommand(&ExecuteCommandParams{
			Command: CommandXGoGetInputSlots, Arguments: []json.RawMessage{json.RawMessage(`{"textDocument":42}`)},
		})
		require.ErrorContains(t, err, "failed to unmarshal command argument")
		assert.Nil(t, result)
	})

	t.Run("InvalidURI", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("println 5\n")})
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "https://example.com/main.xgo"}}})
		require.ErrorContains(t, err, "failed to get file path")
		assert.Nil(t, slots)
	})

	t.Run("InitializedDeclaration", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("var Count int = 5\n")})
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
		require.NoError(t, err)
		require.Len(t, slots, 1)
		assert.Equal(t, XGoInputTypeInteger, slots[0].Accept.Type)
		assert.Equal(t, int64(5), slots[0].Input.Value)
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

func TestCheckAddressInputSlot(t *testing.T) {
	m := map[string][]byte{
		"main.xgo": []byte(`
var (
	varA int
)

func main() {
	varA = 10
	println varA
	otherVar := 20
}
`),
	}
	s := newTestServer(t, m)

	ctx := inputSlotTestContext(t, s, "main.xgo")
	astFile := ctx.astFile

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
			pos := PosAt(ctx.proj, astFile, tt.exprPosition)
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
				assert.Equal(t, XGoInputSlotKindAddress, got.Kind)
				assert.Equal(t, XGoInputTypeUnknown, got.Accept.Type)
				assert.Equal(t, XGoInputKindPredefined, got.Input.Kind)
				assert.Equal(t, XGoInputTypeUnknown, got.Input.Type)
				assert.Equal(t, tt.wantName, got.Input.Name)
				assert.NotEmpty(t, got.Range)
			}
		})
	}
}

func TestCreateValueInputSlotFromUnaryExpr(t *testing.T) {
	m := map[string][]byte{
		"main.xgo": []byte(`
func main() {
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
	}
	s := newTestServer(t, m)

	ctx := inputSlotTestContext(t, s, "main.xgo")
	astFile := ctx.astFile

	for _, tt := range []struct {
		name           string
		exprPosition   Position
		wantKind       XGoInputSlotKind
		wantAcceptType XGoInputType
		wantInputKind  XGoInputKind
		wantInputType  XGoInputType
		wantInputValue any
	}{
		{
			name:           "UnaryMinusInteger",
			exprPosition:   Position{Line: 3, Character: 12},
			wantKind:       XGoInputSlotKindValue,
			wantAcceptType: XGoInputTypeInteger,
			wantInputKind:  XGoInputKindInPlace,
			wantInputType:  XGoInputTypeInteger,
			wantInputValue: int64(-42),
		},
		{
			name:           "UnaryMinusFloat",
			exprPosition:   Position{Line: 6, Character: 14},
			wantKind:       XGoInputSlotKindValue,
			wantAcceptType: XGoInputTypeDecimal,
			wantInputKind:  XGoInputKindInPlace,
			wantInputType:  XGoInputTypeDecimal,
			wantInputValue: -3.14,
		},
		{
			name:           "UnaryPlusInteger",
			exprPosition:   Position{Line: 9, Character: 12},
			wantKind:       XGoInputSlotKindValue,
			wantAcceptType: XGoInputTypeInteger,
			wantInputKind:  XGoInputKindInPlace,
			wantInputType:  XGoInputTypeInteger,
			wantInputValue: int64(10),
		},
		{
			name:           "BitwiseComplement",
			exprPosition:   Position{Line: 12, Character: 19},
			wantKind:       XGoInputSlotKindValue,
			wantAcceptType: XGoInputTypeInteger,
			wantInputKind:  XGoInputKindInPlace,
			wantInputType:  XGoInputTypeInteger,
			wantInputValue: int64(^0xFF), // ~255 = -256
		},
		{
			name:           "LogicalNot",
			exprPosition:   Position{Line: 15, Character: 13},
			wantKind:       XGoInputSlotKindValue,
			wantAcceptType: XGoInputTypeBoolean,
			wantInputKind:  XGoInputKindInPlace,
			wantInputType:  XGoInputTypeBoolean,
			wantInputValue: false,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pos := PosAt(ctx.proj, astFile, tt.exprPosition)
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

func TestCheckValueInputSlot(t *testing.T) {
	m := map[string][]byte{
		"main.xgo": []byte(`
func main() {
	// Basic literals.
	numValue := 42
	floatValue := 3.14
	strValue := "hello"

	// Boolean identifier.
	boolValue := true

	// Other expressions.
	arrayValue := []int{1, 2, 3}
}
`),
	}
	s := newTestServer(t, m)

	ctx := inputSlotTestContext(t, s, "main.xgo")
	astFile := ctx.astFile

	for _, tt := range []struct {
		name           string
		exprPosition   Position
		exprFilter     func(ast.Node) bool
		wantNil        bool
		wantKind       XGoInputSlotKind
		wantAcceptType XGoInputType
		wantInputKind  XGoInputKind
		wantInputType  XGoInputType
		wantInputValue any
		wantInputName  string
	}{
		{
			name:           "IntegerLiteral",
			exprPosition:   Position{Line: 3, Character: 14},
			exprFilter:     func(node ast.Node) bool { _, ok := node.(*ast.BasicLit); return ok },
			wantKind:       XGoInputSlotKindValue,
			wantAcceptType: XGoInputTypeInteger,
			wantInputKind:  XGoInputKindInPlace,
			wantInputType:  XGoInputTypeInteger,
			wantInputValue: int64(42),
		},
		{
			name:           "FloatLiteral",
			exprPosition:   Position{Line: 4, Character: 16},
			exprFilter:     func(node ast.Node) bool { _, ok := node.(*ast.BasicLit); return ok },
			wantKind:       XGoInputSlotKindValue,
			wantAcceptType: XGoInputTypeDecimal,
			wantInputKind:  XGoInputKindInPlace,
			wantInputType:  XGoInputTypeDecimal,
			wantInputValue: 3.14,
		},
		{
			name:           "StringLiteral",
			exprPosition:   Position{Line: 5, Character: 14},
			exprFilter:     func(node ast.Node) bool { _, ok := node.(*ast.BasicLit); return ok },
			wantKind:       XGoInputSlotKindValue,
			wantAcceptType: XGoInputTypeString,
			wantInputKind:  XGoInputKindInPlace,
			wantInputType:  XGoInputTypeString,
			wantInputValue: "hello",
		},
		{
			name:           "BooleanIdentifier",
			exprPosition:   Position{Line: 8, Character: 15},
			exprFilter:     func(node ast.Node) bool { _, ok := node.(*ast.Ident); return ok },
			wantKind:       XGoInputSlotKindValue,
			wantAcceptType: XGoInputTypeBoolean,
			wantInputKind:  XGoInputKindInPlace,
			wantInputType:  XGoInputTypeBoolean,
			wantInputValue: true,
		},
		{
			name:         "NonValueNode",
			exprPosition: Position{Line: 11, Character: 16},
			exprFilter:   func(node ast.Node) bool { _, ok := node.(*ast.CompositeLit); return ok },
			wantNil:      true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pos := PosAt(ctx.proj, astFile, tt.exprPosition)
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

func TestCreateValueInputSlotFromBasicLit(t *testing.T) {
	m := map[string][]byte{
		"main.xgo": []byte(`
func main() {
	// Integer literals.
	x := 42
	hexValue := 0xFF

	// Float literals.
	y := 3.14
	scientific := 1.5e2

	// String literals.
	message := "Hello, world!"

}
`),
	}
	s := newTestServer(t, m)

	ctx := inputSlotTestContext(t, s, "main.xgo")
	astFile := ctx.astFile

	for _, tt := range []struct {
		name           string
		litPosition    Position
		declaredType   gotypes.Type
		wantAcceptType XGoInputType
		wantInputType  XGoInputType
		wantInputKind  XGoInputKind
		wantValue      any
	}{
		{
			name:           "String",
			litPosition:    Position{Line: 11, Character: 13},
			wantAcceptType: XGoInputTypeString,
			wantInputType:  XGoInputTypeString,
			wantInputKind:  XGoInputKindInPlace,
			wantValue:      "Hello, world!",
		},
		{
			name:           "Integer",
			litPosition:    Position{Line: 3, Character: 7},
			wantAcceptType: XGoInputTypeInteger,
			wantInputType:  XGoInputTypeInteger,
			wantInputKind:  XGoInputKindInPlace,
			wantValue:      int64(42),
		},
		{
			name:           "HexInteger",
			litPosition:    Position{Line: 4, Character: 14},
			wantAcceptType: XGoInputTypeInteger,
			wantInputType:  XGoInputTypeInteger,
			wantInputKind:  XGoInputKindInPlace,
			wantValue:      int64(255), // 0xFF = 255
		},
		{
			name:           "Float",
			litPosition:    Position{Line: 7, Character: 7},
			wantAcceptType: XGoInputTypeDecimal,
			wantInputType:  XGoInputTypeDecimal,
			wantInputKind:  XGoInputKindInPlace,
			wantValue:      3.14,
		},
		{
			name:           "ScientificFloat",
			litPosition:    Position{Line: 8, Character: 16},
			wantAcceptType: XGoInputTypeDecimal,
			wantInputType:  XGoInputTypeDecimal,
			wantInputKind:  XGoInputKindInPlace,
			wantValue:      150.0, // 1.5e2 = 150
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pos := PosAt(ctx.proj, astFile, tt.litPosition)
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
			assert.Equal(t, XGoInputSlotKindValue, got.Kind)
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
		"main.xgo": []byte(`var regularVar int
func main() {
	boolVar := true
	myVar := regularVar
}
`),
	}
	s := newTestServer(t, m)

	ctx := inputSlotTestContext(t, s, "main.xgo")
	astFile := ctx.astFile

	for _, tt := range []struct {
		name           string
		identPosition  Position
		wantInputKind  XGoInputKind
		wantInputType  XGoInputType
		wantInputValue any
		wantInputName  string
		wantBoolValue  *bool
	}{
		{
			name:           "Boolean",
			identPosition:  Position{Line: 2, Character: 13},
			wantInputKind:  XGoInputKindInPlace,
			wantInputType:  XGoInputTypeBoolean,
			wantInputValue: true,
		},
		{
			name:          "Regular",
			identPosition: Position{Line: 3, Character: 11},
			wantInputKind: XGoInputKindPredefined,
			wantInputType: XGoInputTypeInteger,
			wantInputName: "regularVar",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pos := PosAt(ctx.proj, astFile, tt.identPosition)
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
			assert.Equal(t, XGoInputSlotKindValue, got.Kind)
			assert.Equal(t, tt.wantInputType, got.Accept.Type)
			assert.Equal(t, tt.wantInputKind, got.Input.Kind)
			assert.Equal(t, tt.wantInputType, got.Input.Type)
			assert.Equal(t, tt.wantInputValue, got.Input.Value)
			assert.Equal(t, tt.wantInputName, got.Input.Name)
			assert.NotEmpty(t, got.Range)
		})
	}

}

func TestInferBasicInputType(t *testing.T) {
	t.Run("BasicTypes", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			typ  gotypes.Type
			want XGoInputType
		}{
			{"String", gotypes.Typ[gotypes.String], XGoInputTypeString},
			{"UntypedString", gotypes.Typ[gotypes.UntypedString], XGoInputTypeString},

			{"Int", gotypes.Typ[gotypes.Int], XGoInputTypeInteger},
			{"Int8", gotypes.Typ[gotypes.Int8], XGoInputTypeInteger},
			{"Int16", gotypes.Typ[gotypes.Int16], XGoInputTypeInteger},
			{"Int32", gotypes.Typ[gotypes.Int32], XGoInputTypeInteger},
			{"Int64", gotypes.Typ[gotypes.Int64], XGoInputTypeInteger},
			{"Uint", gotypes.Typ[gotypes.Uint], XGoInputTypeInteger},
			{"Uint8", gotypes.Typ[gotypes.Uint8], XGoInputTypeInteger},
			{"Uint16", gotypes.Typ[gotypes.Uint16], XGoInputTypeInteger},
			{"Uint32", gotypes.Typ[gotypes.Uint32], XGoInputTypeInteger},
			{"Uint64", gotypes.Typ[gotypes.Uint64], XGoInputTypeInteger},
			{"UntypedInt", gotypes.Typ[gotypes.UntypedInt], XGoInputTypeInteger},

			{"Float32", gotypes.Typ[gotypes.Float32], XGoInputTypeDecimal},
			{"Float64", gotypes.Typ[gotypes.Float64], XGoInputTypeDecimal},
			{"UntypedFloat", gotypes.Typ[gotypes.UntypedFloat], XGoInputTypeDecimal},

			{"Bool", gotypes.Typ[gotypes.Bool], XGoInputTypeBoolean},
			{"UntypedBool", gotypes.Typ[gotypes.UntypedBool], XGoInputTypeBoolean},

			{"Complex64", gotypes.Typ[gotypes.Complex64], XGoInputTypeUnknown},
			{"Complex128", gotypes.Typ[gotypes.Complex128], XGoInputTypeUnknown},
		} {
			t.Run(tt.name, func(t *testing.T) {
				got := inferBasicInputType(tt.typ)
				assert.Equal(t, tt.want, got)
			})
		}
	})
	t.Run("NonBasicType", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/pkg", "pkg")
		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		namedType := gotypes.NewNamed(gotypes.NewTypeName(0, pkg, "MyStruct", nil), structType, nil)

		got := inferBasicInputType(namedType)
		assert.Equal(t, XGoInputTypeUnknown, got)
	})
	t.Run("PointerType", func(t *testing.T) {
		pointerType := gotypes.NewPointer(gotypes.Typ[gotypes.Int])

		got := inferBasicInputType(pointerType)
		assert.Equal(t, XGoInputTypeUnknown, got)
	})
	t.Run("AliasFallback", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/pkg", "pkg")
		namedIntType := gotypes.NewNamed(gotypes.NewTypeName(0, pkg, "MyCount", nil), gotypes.Typ[gotypes.Int], nil)

		for _, tt := range []struct {
			name string
			typ  gotypes.Type
			want XGoInputType
		}{
			{
				name: "AliasToBasic",
				typ:  gotypes.NewAlias(gotypes.NewTypeName(0, pkg, "MyInt", nil), gotypes.Typ[gotypes.Int]),
				want: XGoInputTypeInteger,
			},
			{
				name: "AliasToNamedType",
				typ:  gotypes.NewAlias(gotypes.NewTypeName(0, pkg, "MyCountAlias", nil), namedIntType),
				want: XGoInputTypeUnknown,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				got := inferBasicInputType(tt.typ)
				assert.Equal(t, tt.want, got)
			})
		}
	})
}

func TestFindInputSlots(t *testing.T) {
	m := map[string][]byte{
		"main.xgo": []byte(`
var global int

func main() {
	count := 5
	message := "Hello"
	isVisible := true
	direction := 1
	println 42, 3.14, "text", true
	sum := 10 + 20
	isEqual := count == 5
	notTrue := !isVisible
	count = 10
	if count > 3 { println "Greater than 3" }
	for i := 0; i < 5; i++ { println i }
	calculateValue := func() int { return 100 }
	switch direction {
	case 1: println "First"
	case 2: println "Second"
	default: println "Other"
	}
	numbers := [1, 2, 3]
	for index, value := range numbers { println index, value }
	count++
	println message
}
`),
	}
	s := newTestServer(t, m)

	ctx := inputSlotTestContext(t, s, "main.xgo")

	inputSlots := findInputSlots(ctx)
	require.NotNil(t, inputSlots)
	assert.NotEmpty(t, inputSlots)

	t.Run("ValueSlots", func(t *testing.T) {
		for _, tt := range []struct {
			name                     string
			value                    any
			wantAcceptType           XGoInputType
			wantInputType            XGoInputType
			wantEmptyPredefinedNames bool
		}{
			{
				name:           "String",
				value:          "text",
				wantAcceptType: XGoInputTypeUnknown,
				wantInputType:  XGoInputTypeString,
			},
			{
				name:           "Integer",
				value:          int64(42),
				wantAcceptType: XGoInputTypeUnknown,
				wantInputType:  XGoInputTypeInteger,
			},
			{
				name:           "Decimal",
				value:          3.14,
				wantAcceptType: XGoInputTypeUnknown,
				wantInputType:  XGoInputTypeDecimal,
			},
			{
				name:                     "Boolean",
				value:                    true,
				wantAcceptType:           XGoInputTypeBoolean,
				wantInputType:            XGoInputTypeBoolean,
				wantEmptyPredefinedNames: true,
			},
			{
				name:           "BinaryExprResult",
				value:          int64(10),
				wantAcceptType: XGoInputTypeInteger,
				wantInputType:  XGoInputTypeInteger,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				slot := findInputSlot(inputSlots, tt.value, "", tt.wantInputType, XGoInputKindInPlace)
				require.NotNil(t, slot)
				assert.Equal(t, XGoInputSlotKindValue, slot.Kind)
				assert.Equal(t, tt.wantAcceptType, slot.Accept.Type)
				assert.Equal(t, XGoInputKindInPlace, slot.Input.Kind)
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
				assert.Equal(t, XGoInputSlotKindAddress, slot.Kind)
				assert.Equal(t, XGoInputTypeUnknown, slot.Accept.Type)
				assert.Equal(t, XGoInputKindPredefined, slot.Input.Kind)
				assert.Equal(t, XGoInputTypeUnknown, slot.Input.Type)
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
				slot := findInputSlot(inputSlots, nil, tt.wantInputName, XGoInputTypeUnknown, XGoInputKindPredefined)
				require.NotNil(t, slot)
				assert.Equal(t, XGoInputTypeUnknown, slot.Accept.Type)
				assert.Equal(t, XGoInputKindPredefined, slot.Input.Kind)
				assert.Equal(t, XGoInputTypeUnknown, slot.Input.Type)
				assert.Equal(t, tt.wantInputName, slot.Input.Name)
				assert.Contains(t, slot.PredefinedNames, "global")
				assert.NotEmpty(t, slot.Range)
			})
		}
	})

	t.Run("LargeList", func(t *testing.T) {
		const slotCount = 2_001
		files := inputSlotLargeListFiles(slotCount)
		server := newTestServer(t, files)

		ctx := inputSlotTestContext(t, server, "main.xgo")

		slots := findInputSlots(ctx)
		require.Len(t, slots, slotCount)
		for i, slot := range slots {
			assert.Equal(t, XGoInputSlotKindValue, slot.Kind)
			assert.Equal(t, XGoInputTypeUnknown, slot.Accept.Type)
			assert.Equal(t, XGoInputTypeString, slot.Input.Type)
			assert.Equal(t, "value", slot.Input.Value)
			if i > 0 {
				assert.Less(t, slots[i-1].Range.Start.Character, slot.Range.Start.Character)
				assert.False(t, IsRangesOverlap(slots[i-1].Range, slot.Range))
			}
		}
	})

	t.Run("MixedLargeList", func(t *testing.T) {
		const expressionCount = 2_001
		files := inputSlotMixedListFiles(expressionCount)
		server := newTestServer(t, files)

		ctx := inputSlotTestContext(t, server, "main.xgo")

		slots := findInputSlots(ctx)
		require.Len(t, slots, expressionCount*3)
		for i := 1; i < len(slots); i++ {
			assert.LessOrEqual(t, comparePositions(slots[i-1].Range.Start, slots[i].Range.Start), 0)
			assert.False(t, IsRangesOverlap(slots[i-1].Range, slots[i].Range))
		}
	})

	t.Run("DropsDegenerateRanges", func(t *testing.T) {
		files := map[string][]byte{
			"main.xgo": []byte("println(1, 2, 3"),
		}
		server := newTestServer(t, files)

		proj := server.getProjWithFile()
		astFile, err := proj.ASTFile("main.xgo")
		require.Error(t, err)
		require.NotNil(t, astFile)
		ctx := newInputSlotContext(proj, astFile)

		slots := findInputSlots(ctx)
		require.Len(t, slots, 3)
		for _, slot := range slots {
			assert.Less(t, comparePositions(slot.Range.Start, slot.Range.End), 0)
		}
	})

	t.Run("UTF16Ranges", func(t *testing.T) {
		source := []byte("println \"\U0001F600\", \"value\"\r\n")
		files := map[string][]byte{
			"main.xgo": source,
		}
		server := newTestServer(t, files)

		ctx := inputSlotTestContext(t, server, "main.xgo")

		slots := findInputSlots(ctx)
		require.Len(t, slots, 2)
		assert.Equal(t, Range{Start: Position{Line: 0, Character: 8}, End: Position{Line: 0, Character: 12}}, slots[0].Range)
		assert.Equal(t, Range{Start: Position{Line: 0, Character: 14}, End: Position{Line: 0, Character: 21}}, slots[1].Range)
	})
}

func findInputSlot(inputSlots []XGoInputSlot, value any, name string, inputType XGoInputType, kind XGoInputKind) *XGoInputSlot {
	for _, slot := range inputSlots {
		if slot.Input.Kind == kind {
			if kind == XGoInputKindInPlace && reflect.DeepEqual(slot.Input.Value, value) && slot.Input.Type == inputType {
				return &slot
			} else if kind == XGoInputKindPredefined && slot.Input.Name == name && slot.Input.Type == inputType {
				return &slot
			}
		}
	}
	return nil
}

func findInputSlotByRange(inputSlots []XGoInputSlot, inputRange Range) *XGoInputSlot {
	for i := range inputSlots {
		if inputSlots[i].Range == inputRange {
			return &inputSlots[i]
		}
	}
	return nil
}

func findAddressInputSlot(inputSlots []XGoInputSlot, name string) *XGoInputSlot {
	for _, slot := range inputSlots {
		if slot.Kind == XGoInputSlotKindAddress && slot.Input.Name == name {
			return &slot
		}
	}
	return nil
}

func inputSlotTestContext(t *testing.T, s *Server, filename string) *inputSlotContext {
	t.Helper()

	proj := s.getProjWithFile()
	astFile, err := proj.ASTFile(filename)
	require.NoError(t, err)
	require.NotNil(t, astFile)
	ctx := newInputSlotContext(proj, astFile)
	require.NotNil(t, ctx.typeInfo)
	return ctx
}

func inputSlotLargeListFiles(elementCount int) map[string][]byte {
	source := "func list(values ...any) {}\n" +
		`list("value"` + strings.Repeat(`, "value"`, elementCount-1) + ")\n"
	return map[string][]byte{"main.xgo": []byte(source)}
}

func inputSlotMixedListFiles(expressionCount int) map[string][]byte {
	source := "func list(values ...any) {}\n" +
		`list(` + strings.Repeat(`1 + 2, `, expressionCount) +
		`"value"` + strings.Repeat(`, "value"`, expressionCount-1) + ")\n"
	return map[string][]byte{"main.xgo": []byte(source)}
}

func BenchmarkServerGetInputSlotsWithLargeList(b *testing.B) {
	const slotCount = 20_001
	files := inputSlotLargeListFiles(slotCount)
	server := newTestServer(b, files)
	params := []XGoGetInputSlotsParams{{
		TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
	}}
	slots, err := server.xgoGetInputSlots(params)
	require.NoError(b, err)
	require.Len(b, slots, slotCount)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := server.xgoGetInputSlots(params)
		require.NoError(b, err)
	}
}

func BenchmarkServerGetInputSlotsWithMixedLargeList(b *testing.B) {
	const expressionCount = 8_000
	files := inputSlotMixedListFiles(expressionCount)
	server := newTestServer(b, files)
	params := []XGoGetInputSlotsParams{{
		TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
	}}
	slots, err := server.xgoGetInputSlots(params)
	require.NoError(b, err)
	require.Len(b, slots, expressionCount*3)

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := server.xgoGetInputSlots(params)
		require.NoError(b, err)
	}
}
