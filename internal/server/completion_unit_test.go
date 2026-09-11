package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const xgoUnitCompletionSource = `import (
	"time"
	"example.com/unit"
)

type Options struct {
	Delay time.Duration
}

func wait(d time.Duration) {}
func move(d unit.Distance) {}
func configure(opts Options?) {}

func run() {
	wait 1
	wait 1m
	move 1m
	configure delay = 1
}
`

func TestServerTextDocumentCompletionUnits(t *testing.T) {
	t.Run("ClassfileCallbacks", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			callback string
		}{
			{name: "Project", filename: "main_fixture.gox", callback: "onStart => {"},
			{name: "Work", filename: "Worker_fixture.gox", callback: "onValue value => {"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const line = "\t/* \U0001f600 */ move 1m"
				files := map[string][]byte{"main_fixture.gox": nil}
				files[tt.filename] = []byte("import \"example.com/unit\"\r\nfunc move(d unit.Distance) {}\r\n" + tt.callback + "\r\n" + line + "\r\n}\r\n")
				s := newFrameworkTestServer(t, files)
				s.workspaceRootFS.Importer = xgoUnitTestImporter{fallback: s.workspaceRootFS.Importer}
				_, err := s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)

				position := Position{Line: 3, Character: uint32(UTF16Len(line))}
				list := completionListAt(t, s, tt.filename, position)
				assert.True(t, list.IsIncomplete)
				assert.NotContains(t, completionItemLabels(list.Items), "s")
				assertCompletionItemTextEdit(t, list.Items, "cm", TextEdit{
					Range: Range{
						Start: Position{Line: 3, Character: position.Character - 1},
						End:   position,
					},
					NewText: "cm",
				})
				item := completionItemByLabel(list.Items, "cm")
				require.NotNil(t, item)
				assert.Equal(t, "1cm", item.FilterText)
			})
		}
	})

	t.Run("CallArguments", func(t *testing.T) {
		s := newXGoUnitTestServer(t, xgoUnitCompletionSource)
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		durationItems := completionListAt(t, s, "main.xgo", Position{Line: 14, Character: 7}).Items
		durationLabels := completionItemLabels(durationItems)
		assert.Contains(t, durationLabels, "ms")
		assert.Contains(t, durationLabels, "s")
		assert.Contains(t, durationLabels, "m")
		assert.Contains(t, durationLabels, "\u00b5s")
		assert.NotContains(t, durationLabels, "wait")
		assertCompletionItemTextEdit(t, durationItems, "s", TextEdit{
			Range: Range{
				Start: Position{Line: 14, Character: 7},
				End:   Position{Line: 14, Character: 7},
			},
			NewText: "s",
		})
		durationItem := completionItemByLabel(durationItems, "s")
		require.NotNil(t, durationItem)
		assert.Equal(t, "1s", durationItem.FilterText)

		durationPartialItems := completionListAt(t, s, "main.xgo", Position{Line: 15, Character: 8}).Items
		assert.Contains(t, completionItemLabels(durationPartialItems), "ms")
		assertCompletionItemTextEdit(t, durationPartialItems, "ms", TextEdit{
			Range: Range{
				Start: Position{Line: 15, Character: 7},
				End:   Position{Line: 15, Character: 8},
			},
			NewText: "ms",
		})
		durationPartialItem := completionItemByLabel(durationPartialItems, "ms")
		require.NotNil(t, durationPartialItem)
		assert.Equal(t, "1ms", durationPartialItem.FilterText)

		distanceItems := completionListAt(t, s, "main.xgo", Position{Line: 16, Character: 8}).Items
		distanceLabels := completionItemLabels(distanceItems)
		assert.Contains(t, distanceLabels, "mm")
		assert.Contains(t, distanceLabels, "cm")
		assert.NotContains(t, distanceLabels, "s")
		assertCompletionItemTextEdit(t, distanceItems, "cm", TextEdit{
			Range: Range{
				Start: Position{Line: 16, Character: 7},
				End:   Position{Line: 16, Character: 8},
			},
			NewText: "cm",
		})
	})

	t.Run("FuncDecoratorArgument", func(t *testing.T) {
		s := newXGoUnitTestServer(t, `import "example.com/unit"

func withDistance(distance unit.Distance, fn func()) {}

@withDistance(1)
func run() {}
`)

		items := completionListAt(t, s, "main.xgo", Position{Line: 4, Character: 15}).Items
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "mm")
		assert.Contains(t, labels, "cm")
		assert.NotContains(t, labels, "s")
	})

	t.Run("StructKwargUnsupported", func(t *testing.T) {
		s := newXGoUnitTestServer(t, xgoUnitCompletionSource)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 17, Character: 20})
		assert.False(t, containsCompletionItemKind(items, UnitCompletion))
		assert.NotContains(t, completionItemLabels(items), "s")
	})

	t.Run("InterfaceKwarg", func(t *testing.T) {
		s := newXGoUnitTestServer(t, `import "time"

type Params interface {
	Delay(time.Duration) Params
}

type Client struct{}

var c Client

func (c *Client) Params() Params { return nil }
func (c *Client) Run(params Params) {}

func run() {
	c.Run delay = 1
}
`)

		items := completionListAt(t, s, "main.xgo", Position{Line: 14, Character: 16}).Items
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "ms")
		assert.Contains(t, labels, "s")
		assert.NotContains(t, labels, "delay")
	})

	t.Run("UnsupportedContexts", func(t *testing.T) {
		source := `import "time"

type Options struct {
	Delay time.Duration
}

func duration() time.Duration {
	return 1
}

func run() {
	var delay time.Duration = 1
	delay = 1
	_ = Options{Delay: 1}
}
`

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
				s := newXGoUnitTestServer(t, source)
				itemsResult, err := s.textDocumentCompletion(&CompletionParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
						Position:     tt.position,
					},
				})
				require.NoError(t, err)
				items := requireValueAs[[]CompletionItem](t, itemsResult)
				assert.Falsef(t, containsCompletionItemKind(items, UnitCompletion), "%v", completionItemLabels(items))
			})
		}
	})

	t.Run("PointerUnsupported", func(t *testing.T) {
		s := newXGoUnitTestServer(t, `import "time"

func waitPtr(d *time.Duration) {}

func run() {
	waitPtr 1
}
`)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 5, Character: 10})
		assert.False(t, containsCompletionItemKind(items, UnitCompletion))
		assert.NotContains(t, completionItemLabels(items), "s")
	})

	t.Run("CurrentPackageUnsupported", func(t *testing.T) {
		s := newXGoUnitTestServer(t, `type Distance int

const XGou_Distance = "mm=1,cm=10,m=1000"

func move(d Distance) {}

func run() {
	move 1
}
`)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 7, Character: 7})
		assert.False(t, containsCompletionItemKind(items, UnitCompletion))
		assert.NotContains(t, completionItemLabels(items), "m")
	})

	t.Run("CurrentPackageAliasUnsupported", func(t *testing.T) {
		s := newXGoUnitTestServer(t, `type Seconds = float64

const XGou_Seconds = "s=1,ms=0.001"

func glide(s Seconds) {}

func run() {
	glide 1
}
`)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 7, Character: 8})
		assert.False(t, containsCompletionItemKind(items, UnitCompletion))
		assert.NotContains(t, completionItemLabels(items), "ms")
	})

	t.Run("ImportedAlias", func(t *testing.T) {
		s := newXGoUnitTestServer(t, `import "example.com/unit"

func glide(s unit.Seconds) {}

func run() {
	glide 1
}
`)

		items := completionListAt(t, s, "main.xgo", Position{Line: 5, Character: 8}).Items
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "s")
		assert.Contains(t, labels, "ms")
		assert.NotContains(t, labels, "m")
	})

	t.Run("ImportedAliasFallback", func(t *testing.T) {
		s := newXGoUnitTestServer(t, `import "example.com/unit"

func wait(d unit.Delay) {}

func run() {
	wait 1
}
`)

		items := completionListAt(t, s, "main.xgo", Position{Line: 5, Character: 7}).Items
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "ms")
		assert.Contains(t, labels, "s")
		assert.NotContains(t, labels, "km")
	})

	t.Run("ImportedAliasOverloads", func(t *testing.T) {
		s := newXGoUnitTestServer(t, `import "example.com/unit"

type Worker struct{}

var worker Worker

func (w *Worker) handleSeconds(v unit.Seconds) {}
func (w *Worker) handleMeters(v unit.Meters) {}

func (Worker).handle = (
	(Worker).handleSeconds
	(Worker).handleMeters
)

func run() {
	worker.handle 1
}
`)

		items := completionListAt(t, s, "main.xgo", Position{Line: 15, Character: 16}).Items
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "ms")
		assert.Contains(t, labels, "km")
	})

	t.Run("DoesNotSwallowGeneralItems", func(t *testing.T) {
		s := newXGoUnitTestServer(t, `func plain(n int) {}

func run() {
	count := 1
	plain 1
}
`)

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 4, Character: 8})
		assert.False(t, containsCompletionItemKind(items, UnitCompletion))
		assert.Contains(t, completionItemLabels(items), "count")
	})

	t.Run("LSPResultShape", func(t *testing.T) {
		t.Run("CompleteArray", func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"main.xgo": []byte("var x = 100\necho x"),
			})

			items := completionItemsAt(t, s, "main.xgo", Position{Line: 1, Character: 5})
			assert.NotEmpty(t, items)
		})

		t.Run("IncompleteUnitList", func(t *testing.T) {
			s := newXGoUnitTestServer(t, xgoUnitCompletionSource)

			list := completionListAt(t, s, "main.xgo", Position{Line: 14, Character: 7})
			assert.True(t, list.IsIncomplete)
			assert.True(t, containsCompletionItemLabel(list.Items, "s"))
			listItem := completionItemByLabel(list.Items, "s")
			require.NotNil(t, listItem)
			assert.Equal(t, "1s", listItem.FilterText)
			assertCompletionItemTextEdit(t, list.Items, "s", TextEdit{
				Range: Range{
					Start: Position{Line: 14, Character: 7},
					End:   Position{Line: 14, Character: 7},
				},
				NewText: "s",
			})
		})

		t.Run("IncompleteUnitListUsesCurrentText", func(t *testing.T) {
			s := newXGoUnitTestServer(t, `import "time"

func wait(d time.Duration) {}

func run() {
	wait 12
}
`)

			list := completionListAt(t, s, "main.xgo", Position{Line: 5, Character: 8})
			assert.True(t, list.IsIncomplete)
			listItem := completionItemByLabel(list.Items, "s")
			require.NotNil(t, listItem)
			assert.Equal(t, "12s", listItem.FilterText)
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

func completionListAt(t *testing.T, s *Server, filename string, position Position) CompletionList {
	t.Helper()

	result, err := s.textDocumentCompletion(&CompletionParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: position,
	}})
	require.NoError(t, err)
	return requireValueAs[CompletionList](t, result)
}
