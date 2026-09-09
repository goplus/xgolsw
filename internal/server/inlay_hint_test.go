package server

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentInlayHint(t *testing.T) {
	for _, tt := range []struct {
		name     string
		filename string
	}{
		{name: "XGo", filename: "main.xgo"},
		{name: "Gop", filename: "main.gop"},
		{name: "NormalClass", filename: "Record.gox"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"functions.xgo": []byte("func combine(count int, label string) {}\n"),
				tt.filename:     []byte("combine 1, \"value\"\n"),
			})
			hints, err := s.textDocumentInlayHint(&InlayHintParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
				Range:        Range{End: Position{Line: 1}},
			})
			require.NoError(t, err)
			assert.Equal(t, []InlayHint{
				{Position: Position{Line: 0, Character: 8}, Label: "count", Kind: Parameter},
				{Position: Position{Line: 0, Character: 11}, Label: "label", Kind: Parameter},
			}, hints)
			_, err = s.workspaceRootFS.TypeInfo()
			assert.NoError(t, err)
		})
	}

	t.Run("FrameworkMethods", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main_fixture.gox":   []byte("onStart => {\n    Worker.apply 3\n    _ = create(Item, \"sample\")\n}\n"),
			"Worker_fixture.gox": []byte("onValue amount => {\n    apply amount\n}\n"),
		})
		for _, tt := range []struct {
			filename string
			want     []InlayHint
		}{
			{
				filename: "main_fixture.gox",
				want: []InlayHint{
					{Position: Position{Line: 1, Character: 17}, Label: "value", Kind: Parameter},
					{Position: Position{Line: 2, Character: 15}, Label: "T", Kind: Parameter},
					{Position: Position{Line: 2, Character: 21}, Label: "name", Kind: Parameter},
				},
			},
			{
				filename: "Worker_fixture.gox",
				want: []InlayHint{
					{Position: Position{Line: 1, Character: 10}, Label: "value", Kind: Parameter},
				},
			},
		} {
			hints, err := s.textDocumentInlayHint(&InlayHintParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
				Range:        Range{End: Position{Line: 10}},
			})
			require.NoError(t, err)
			assert.Equal(t, tt.want, hints, tt.filename)
		}
		_, err := s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("SpecificRange", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("func use(value int) {}\nuse 1\nuse 2\nuse 3\n"),
		})
		hints, err := s.textDocumentInlayHint(&InlayHintParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Range:        Range{Start: Position{Line: 2}, End: Position{Line: 2, Character: 5}},
		})
		require.NoError(t, err)
		assert.Equal(t, []InlayHint{
			{Position: Position{Line: 2, Character: 4}, Label: "value", Kind: Parameter},
		}, hints)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("UTF16", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("func combine(text string, count int) {}\ncombine \"\U0001F600\", 1\n"),
		})
		hints, err := s.textDocumentInlayHint(&InlayHintParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Range:        Range{Start: Position{Line: 1}, End: Position{Line: 1, Character: 15}},
		})
		require.NoError(t, err)
		assert.Equal(t, []InlayHint{
			{Position: Position{Line: 1, Character: 8}, Label: "text", Kind: Parameter},
			{Position: Position{Line: 1, Character: 14}, Label: "count", Kind: Parameter},
		}, hints)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("FileUpdatesWithTypeErrors", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"functions.xgo": []byte("func combine(count int) {}\n"),
			"main.xgo":      []byte("combine 1\n"),
		})
		params := &InlayHintParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Range:        Range{End: Position{Line: 10}},
		}
		hints, err := s.textDocumentInlayHint(params)
		require.NoError(t, err)
		assert.Equal(t, []InlayHint{
			{Position: Position{Character: 8}, Label: "count", Kind: Parameter},
		}, hints)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		s.ModifyFiles([]FileChange{
			{Path: "functions.xgo", Content: []byte("func combine(value int, label string) {}\nfunc broken() { missing() }\n"), Version: 1},
			{Path: "main.xgo", Content: []byte("\ncombine 1, \"x\"\n"), Version: 1},
		})
		hints, err = s.textDocumentInlayHint(params)
		require.NoError(t, err)
		assert.Equal(t, []InlayHint{
			{Position: Position{Line: 1, Character: 8}, Label: "value", Kind: Parameter},
			{Position: Position{Line: 1, Character: 11}, Label: "label", Kind: Parameter},
		}, hints)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.ErrorContains(t, err, "undefined: missing")
	})

	for _, tt := range []struct {
		name         string
		uri          DocumentURI
		source       string
		wantError    bool
		wantASTError bool
	}{
		{name: "EmptyFile", uri: "file:///main.xgo"},
		{name: "MissingFile", uri: "file:///missing.xgo"},
		{name: "NonSourceFile", uri: "file:///notes.txt"},
		{name: "InvalidURI", uri: "https://example.com/main.xgo", wantError: true},
		{name: "StartWithInvalidChar", uri: "file:///main.xgo", source: "\n\u201c\u201dvar (\n    maps []int\n)\n", wantASTError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"main.xgo":  []byte(tt.source),
				"notes.txt": []byte("func use(value int) {}\nuse 1\n"),
			})
			hints, err := s.textDocumentInlayHint(&InlayHintParams{
				TextDocument: TextDocumentIdentifier{URI: tt.uri},
				Range:        Range{End: Position{Line: 10}},
			})
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Empty(t, hints)
			_, err = s.workspaceRootFS.ASTPackage()
			if tt.wantASTError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	t.Run("FuncDecorator", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`func retry(times int, fn func()) {
	fn()
}

@retry(2)
func run() {
}
`),
		})

		inlayHints, err := s.textDocumentInlayHint(&InlayHintParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 7, Character: 0},
			},
		})
		require.NoError(t, err)
		assert.Contains(t, inlayHints, InlayHint{
			Position: Position{Line: 4, Character: 7},
			Label:    "times",
			Kind:     Parameter,
		})
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("Autoclosure", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`
onStart => {
	runWhen true, => {}
}
`),
		})

		inlayHints, err := s.textDocumentInlayHint(&InlayHintParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 4, Character: 0},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, []InlayHint{
			{
				Position: Position{Line: 2, Character: 9},
				Label:    "condition",
				Kind:     Parameter,
				Tooltip:  &InlayHintTooltip{Value: autoclosureParamDocumentation},
			},
		}, inlayHints)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("PartialXGoxFunction", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`import "example.com/typeargs"

func main() {
	typeargs.convert(string, 100)
}
`),
		})
		s.workspaceRootFS.Importer = xgoxTestImporter{fallback: s.workspaceRootFS.Importer}

		inlayHints, err := s.textDocumentInlayHint(&InlayHintParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 5, Character: 0},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, []InlayHint{
			{
				Position: Position{Line: 3, Character: 18},
				Label:    "To",
				Kind:     Parameter,
			},
			{
				Position: Position{Line: 3, Character: 26},
				Label:    "src",
				Kind:     Parameter,
			},
		}, inlayHints)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})
}

func TestCollectInlayHints(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		want   []InlayHint
	}{
		{
			name:   "NamedParameters",
			source: "func mix(count int, label string, enabled bool) {}\nfunc main() {\n    mix 1, \"text\", true\n}\n",
			want: []InlayHint{
				{Position: Position{Line: 2, Character: 8}, Label: "count", Kind: Parameter},
				{Position: Position{Line: 2, Character: 11}, Label: "label", Kind: Parameter},
				{Position: Position{Line: 2, Character: 19}, Label: "enabled", Kind: Parameter},
			},
		},
		{
			name:   "BuiltinAndImportedFunctions",
			source: "import \"fmt\"\nfunc main() {\n    println \"hello\"\n    fmt.Println(\"world\")\n}\n",
			want: []InlayHint{
				{Position: Position{Line: 2, Character: 12}, Label: "a...", Kind: Parameter},
				{Position: Position{Line: 3, Character: 16}, Label: "a...", Kind: Parameter},
			},
		},
		{
			name:   "LambdaExpressionsSkipped",
			source: "func visit(count int, callback func()) {}\nfunc main() {\n    visit 1, => {}\n    visit 2, () => {}\n}\n",
			want: []InlayHint{
				{Position: Position{Line: 2, Character: 10}, Label: "count", Kind: Parameter},
				{Position: Position{Line: 3, Character: 10}, Label: "count", Kind: Parameter},
			},
		},
		{name: "NoInlayHints", source: "var value = 1\n_ = value\n"},
		{name: "EmptyFile"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(tt.source)})
			proj := s.getProjWithFile()
			astFile, err := proj.ASTFile("main.xgo")
			require.NoError(t, err)
			require.NotNil(t, astFile)
			assert.Equal(t, tt.want, collectInlayHints(proj, astFile, 0, 0))
			_, err = proj.TypeInfo()
			assert.NoError(t, err)
		})
	}

	t.Run("RangeFiltering", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("func use(value int) {}\nuse 1\nuse 2\nuse 3\n"),
		})
		proj := s.getProjWithFile()
		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)
		require.NotNil(t, astFile)
		assert.Equal(t, []InlayHint{
			{Position: Position{Line: 1, Character: 4}, Label: "value", Kind: Parameter},
			{Position: Position{Line: 2, Character: 4}, Label: "value", Kind: Parameter},
			{Position: Position{Line: 3, Character: 4}, Label: "value", Kind: Parameter},
		}, collectInlayHints(proj, astFile, 0, 0))
		start := PosAt(proj, astFile, Position{Line: 2})
		end := PosAt(proj, astFile, Position{Line: 2, Character: 5})
		assert.Equal(t, []InlayHint{
			{Position: Position{Line: 2, Character: 4}, Label: "value", Kind: Parameter},
		}, collectInlayHints(proj, astFile, start, end))
		_, err = proj.TypeInfo()
		assert.NoError(t, err)
	})

	for _, tt := range []struct {
		name       string
		parameters string
		argument   string
		want       []InlayHint
	}{
		{name: "UnresolvedOverload", parameters: "other int, text string", argument: "unknown, unknown"},
		{name: "AmbiguousParameter", parameters: "other int, text string", argument: "1, unknown"},
		{
			name: "SharedOverloadParameter", parameters: "value int, text string", argument: "1, unknown",
			want: []InlayHint{{Position: Position{Line: 9, Character: 18}, Label: "value", Kind: Parameter}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := "type Worker struct{}\n" +
				"var worker Worker\n" +
				"func (w *Worker) handleFlag(value int, flag bool) {}\n" +
				"func (w *Worker) handleText(" + tt.parameters + ") {}\n" +
				"func (Worker).handle = (\n" +
				"    (Worker).handleFlag\n" +
				"    (Worker).handleText\n" +
				")\n" +
				"func main() {\n" +
				"    worker.handle " + tt.argument + "\n" +
				"}\n"
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			proj := s.getProjWithFile()
			astFile, err := proj.ASTFile("main.xgo")
			require.NoError(t, err)
			require.NotNil(t, astFile)
			assert.Equal(t, tt.want, collectInlayHints(proj, astFile, 0, 0))
			_, err = proj.TypeInfo()
			assert.ErrorContains(t, err, "undefined: unknown")
		})
	}

	t.Run("FunctionArgumentWithUnresolvedValue", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func handle(count int) {}

func main() {
	handle unknown
}
`),
		})

		proj := s.getProjWithFile()
		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		inlayHints := collectInlayHints(proj, astFile, 0, 0)
		require.Len(t, inlayHints, 1)
		assert.Equal(t, InlayHint{
			Position: Position{Line: 4, Character: 8},
			Label:    "count",
			Kind:     Parameter,
		}, inlayHints[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.ErrorContains(t, err, "undefined: unknown")
	})

	t.Run("OverloadFunctionArguments", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Worker struct{}

var worker Worker

func (w *Worker) handleCount(count int) {}

func (Worker).handle = (
	(Worker).handleCount
)

func main() {
	worker.handle 5
}
`),
		})

		proj := s.getProjWithFile()
		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		inlayHints := collectInlayHints(proj, astFile, 0, 0)
		require.Len(t, inlayHints, 1)
		assert.Equal(t, InlayHint{
			Position: Position{Line: 12, Character: 15},
			Label:    "count",
			Kind:     Parameter,
		}, inlayHints[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("VariadicFunctionArguments", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func main() {
	echo 1
	echo 1, 2, 3
}
`),
		})

		proj := s.getProjWithFile()
		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		inlayHints := collectInlayHints(proj, astFile, 0, 0)
		require.NotNil(t, inlayHints)
		require.Len(t, inlayHints, 2)
		assert.Equal(t, "a...", inlayHints[0].Label)
		assert.Equal(t, "a...", inlayHints[1].Label)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("VariadicArgumentAfterKwargsParameter", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func process(opts map[string]string?, args ...int) {}

func main() {
	process 1, name = "x"
}
`),
		})

		proj := s.getProjWithFile()
		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		inlayHints := collectInlayHints(proj, astFile, 0, 0)
		require.Len(t, inlayHints, 1)
		assert.Equal(t, InlayHint{
			Position: Position{Line: 4, Character: 9},
			Label:    "args...",
			Kind:     Parameter,
		}, inlayHints[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})
}

func TestSortInlayHints(t *testing.T) {
	t.Run("SortingOrder", func(t *testing.T) {
		hints := []InlayHint{
			{Position: Position{Line: 42, Character: 0}, Label: "Z", Kind: Parameter},
			{Position: Position{Line: 5, Character: 0}, Label: "Y", Kind: Parameter},
			{Position: Position{Line: 100, Character: 0}, Label: "X", Kind: Parameter},
			{Position: Position{Line: 1, Character: 0}, Label: "W", Kind: Parameter},
			{Position: Position{Line: 20, Character: 0}, Label: "V", Kind: Parameter},
		}

		sortInlayHints(hints)

		assert.Equal(t, uint32(1), hints[0].Position.Line)
		assert.Equal(t, "W", hints[0].Label)
		assert.Equal(t, uint32(100), hints[len(hints)-1].Position.Line)
		assert.Equal(t, "X", hints[len(hints)-1].Label)
		for i := range len(hints) - 1 {
			assert.LessOrEqual(t, hints[i].Position.Line, hints[i+1].Position.Line)
		}
	})

	t.Run("CharacterPositionSorting", func(t *testing.T) {
		hints := []InlayHint{
			// Line 5 with different character positions.
			{Position: Position{Line: 5, Character: 20}, Label: "A1", Kind: Parameter},
			{Position: Position{Line: 5, Character: 5}, Label: "A2", Kind: Parameter},
			{Position: Position{Line: 5, Character: 15}, Label: "A3", Kind: Parameter},

			// Line 7 with different character positions.
			{Position: Position{Line: 7, Character: 30}, Label: "B1", Kind: Parameter},
			{Position: Position{Line: 7, Character: 10}, Label: "B2", Kind: Parameter},
		}

		sortInlayHints(hints)

		l5Hints := slices.DeleteFunc(slices.Clone(hints), func(h InlayHint) bool {
			return h.Position.Line != 5
		})
		require.Len(t, l5Hints, 3)
		assert.Equal(t, uint32(5), l5Hints[0].Position.Character)
		assert.Equal(t, uint32(15), l5Hints[1].Position.Character)
		assert.Equal(t, uint32(20), l5Hints[2].Position.Character)

		l7Hints := slices.DeleteFunc(slices.Clone(hints), func(h InlayHint) bool {
			return h.Position.Line != 7
		})
		require.Len(t, l7Hints, 2)
		assert.Equal(t, uint32(10), l7Hints[0].Position.Character)
		assert.Equal(t, "B2", l7Hints[0].Label)
		assert.Equal(t, uint32(30), l7Hints[1].Position.Character)
		assert.Equal(t, "B1", l7Hints[1].Label)
	})

	t.Run("LabelSorting", func(t *testing.T) {
		hints := []InlayHint{
			// Same position (5, 10) with different labels in non-alphabetical order.
			{Position: Position{Line: 5, Character: 10}, Label: "Z", Kind: Parameter},
			{Position: Position{Line: 5, Character: 10}, Label: "A", Kind: Parameter},
			{Position: Position{Line: 5, Character: 10}, Label: "M", Kind: Parameter},

			// Different position.
			{Position: Position{Line: 5, Character: 5}, Label: "X", Kind: Parameter},
		}

		sortInlayHints(hints)

		l5c10Hints := slices.DeleteFunc(slices.Clone(hints), func(hint InlayHint) bool {
			return hint.Position.Line != 5 || hint.Position.Character != 10
		})
		require.Len(t, l5c10Hints, 3)
		assert.Equal(t, "A", l5c10Hints[0].Label)
		assert.Equal(t, "M", l5c10Hints[1].Label)
		assert.Equal(t, "Z", l5c10Hints[2].Label)
	})
}
