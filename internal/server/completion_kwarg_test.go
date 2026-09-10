package server

import (
	"slices"
	"strings"
	"testing"

	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletionKwargs(t *testing.T) {
	t.Run("CrossFileSourceKinds", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			filename     string
			needsProject bool
		}{
			{name: "XGo", filename: "main.xgo"},
			{name: "LegacyXGo", filename: "main.gop"},
			{name: "StandaloneClass", filename: "Record.gox"},
			{name: "ProjectClass", filename: "main_fixture.gox"},
			{name: "WorkClass", filename: "Worker_fixture.gox", needsProject: true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"options.xgo": []byte(`type Options struct {
	Count int
	// Name identifies the configured task.
	Name string
}
`),
					tt.filename: []byte(`func configure(opts Options?) {}
func run() {
	configure count = 1, na = "task"
}
`),
				}
				if tt.needsProject {
					files["main_fixture.gox"] = nil
				}
				s := newTestServer(t, files)
				items := completionItemsAt(t, s, tt.filename, Position{Line: 2, Character: 23})
				assert.NotContains(t, completionItemLabels(items), "count")
				item := completionItemByLabel(items, "name")
				require.NotNil(t, item)
				assert.Equal(t, 1, countCompletionItemLabel(items, "name"))
				assert.Equal(t, FieldCompletion, item.Kind)
				assert.Equal(t, "name = ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
				data := requireValueAs[*CompletionItemData](t, item.Data)
				assert.Equal(t, "xgo:main?Options.Name", data.Definition.String())
				require.NotNil(t, item.Documentation)
				doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, doc.Value, "Name identifies the configured task.")
			})
		}
	})

	t.Run("ClassfileCallbacks", func(t *testing.T) {
		for _, class := range []struct {
			name     string
			filename string
			callback string
		}{
			{name: "Project", filename: "main_fixture.gox", callback: `onEvent "tick", value => {`},
			{name: "Work", filename: "Worker_fixture.gox", callback: "onValue value => {"},
		} {
			t.Run(class.name, func(t *testing.T) {
				t.Run("FrameworkKwargName", func(t *testing.T) {
					for _, tt := range []struct {
						name string
						call string
					}{
						{name: "BareIdent", call: "\tconfigure va"},
						{name: "KwargExpr", call: "\tconfigure va = value"},
					} {
						t.Run(tt.name, func(t *testing.T) {
							files := map[string][]byte{"main_fixture.gox": nil}
							files[class.filename] = []byte("func configure(opts Item?) {}\n" + class.callback + "\n" + tt.call + "\n}\n")
							s := newTestServer(t, files)
							items := completionItemsAt(t, s, class.filename, Position{Line: 2, Character: 13})
							item := completionItemByLabel(items, "value")
							require.NotNil(t, item)
							assert.Equal(t, FieldCompletion, item.Kind)
							assert.Equal(t, "value = ${1:}", item.InsertText)
							assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
							data := requireValueAs[*CompletionItemData](t, item.Data)
							assert.Equal(t, "xgo:"+testframework.PkgPath+"?Item.Value", data.Definition.String())
							require.NotNil(t, item.Documentation)
							doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
							assert.Contains(t, doc.Value, "Value stores the work item's value.")
						})
					}
				})

				t.Run("InferredKwargValue", func(t *testing.T) {
					for _, tt := range []struct {
						name   string
						prefix string
						eol    string
					}{
						{name: "ASCII", prefix: "\t", eol: "\n"},
						{name: "UTF16Position", prefix: "\t/* \U0001f600\U0001f600 */ ", eol: "\r\n"},
					} {
						t.Run(tt.name, func(t *testing.T) {
							files := map[string][]byte{"main_fixture.gox": nil}
							files[class.filename] = []byte(strings.Join([]string{
								"func configure(opts Item?) {}",
								class.callback,
								"\tvar title string",
								tt.prefix + "configure value = value",
								"\techo title",
								"}",
								"",
							}, tt.eol))
							s := newTestServer(t, files)
							_, err := s.workspaceRootFS.TypeInfo()
							require.NoError(t, err)

							items := completionItemsAt(t, s, class.filename, Position{
								Line:      3,
								Character: uint32(UTF16Len(tt.prefix + "configure value = v")),
							})
							labels := completionItemLabels(items)
							assert.Contains(t, labels, "value")
							assert.NotContains(t, labels, "title")
						})
					}
				})
			})
		}
	})

	t.Run("ClassfileInterfaceKwargs", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			factory  string
			receiver string
			want     bool
		}{
			{name: "WithFactory", factory: "func Params() Params { return nil }\n", receiver: "client.", want: true},
			{name: "WithoutFactory", receiver: "client."},
			{name: "ImplicitReceiver", factory: "func Params() Params { return nil }\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"options.xgo": []byte(`type Params interface {
	Count(n int) Params
	Name(v string) Params
}
`),
					"main_fixture.gox": nil,
					"Worker_fixture.gox": []byte(tt.factory + `func configure(opts Params?) {}
onValue value => {
	var client Worker
	` + tt.receiver + `configure name = "task", cou = 1
	echo client, value
}
`),
				})
				items := completionItemsAt(t, s, "Worker_fixture.gox", Position{
					Line:      uint32(strings.Count(tt.factory, "\n")) + 3,
					Character: uint32(UTF16Len("\t" + tt.receiver + `configure name = "task", co`)),
				})
				if !tt.want {
					labels := completionItemLabels(items)
					assert.NotContains(t, labels, "count")
					assert.NotContains(t, labels, "name")
					return
				}
				item := completionItemByLabel(items, "count")
				require.NotNil(t, item)
				assert.NotContains(t, completionItemLabels(items), "name")
				assert.Equal(t, FunctionCompletion, item.Kind)
				assert.Equal(t, "count = ${1:}", item.InsertText)
				assert.Equal(t, ToPtr(SnippetTextFormat), item.InsertTextFormat)
				data := requireValueAs[*CompletionItemData](t, item.Data)
				assert.Equal(t, "xgo:main?Params.Count", data.Definition.String())
				require.NotNil(t, item.Documentation)
				doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, doc.Value, "func Count(n int) main.Params")
			})
		}
	})

	t.Run("KwargNameCompletion", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
	Count int
	Name string
}

func configure(opts Options?) {}

func run() {
	configure cou = 1
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 13})
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			return item.Label == "count" &&
				item.InsertText == "count = ${1:}" &&
				item.InsertTextFormat != nil &&
				*item.InsertTextFormat == SnippetTextFormat
		}))
		assert.Contains(t, completionItemLabels(items), "name")
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

func run() {
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

func run() {
	configure cou = 1
}
`,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte(tt.code),
				})

				items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 13})
				labels := completionItemLabels(items)
				assert.Contains(t, labels, "count")
				assert.Contains(t, labels, "name")
			})
		}
	})

	t.Run("KwargNameCompletionSkipsLaterLocalFieldName", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
	Count int
	count string
}

func configure(opts Options?) {}

func run() {
	configure cou = 1
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 13})
		assert.Equal(t, 1, countCompletionItemLabel(items, "count"))
	})

	t.Run("KwargValueCompletion", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

func run() {
	var count int
	configure count = cou
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 23})
		assert.Contains(t, completionItemLabels(items), "count")
	})

	t.Run("OverloadKwargNameCompletion", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
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

func run() {
	worker.handle cou = 1
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 22, Character: 18})
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "count")
		assert.Contains(t, labels, "name")
	})

	t.Run("OverloadKwargNameCompletionFiltersByPositionalArg", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
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

func run() {
	worker.handle "prefix", na = "x"
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 22, Character: 27})
		labels := completionItemLabels(items)
		assert.NotContains(t, labels, "count")
		assert.Contains(t, labels, "name")
	})

	t.Run("OverloadKwargPositionalValueCompletionWithVariadicKwargParam", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
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

func run() {
	var count int
	var title string
	worker.handle co, name = "x"
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 20, Character: 17})
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "count")
		assert.Contains(t, labels, "title")
	})

	t.Run("OverloadKwargValueCompletion", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
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

func run() {
	var count int
	var title string
	worker.handle count = cou
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 24, Character: 27})
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "count")
		assert.NotContains(t, labels, "title")
	})

	t.Run("EmptyKwargValueCompletion", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

func run() {
	var count int
	var title string
	configure count =
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 10, Character: 18})
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "count")
		assert.NotContains(t, labels, "title")
	})

	t.Run("PositionalValueCompletionWithKwargs", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func configure(opts map[string]string, values ...int) {}

func run() {
	var count int
	var title string
	configure cou, name = "x"
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 6, Character: 14})
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "count")
		assert.NotContains(t, labels, "title")
	})

	t.Run("InterfaceKwargNameCompletion", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Client struct{}

type Params interface {
	MaxTokens(n int64) Params
	Temperature(v float64) Params
}

var client Client

func (c Client) Params() Params { return nil }

func (c Client) complete(prompt string, params Params?) {}

func run() {
	client.complete "hi", maxT = 1
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 15, Character: 27})
		assert.True(t, slices.ContainsFunc(items, func(item CompletionItem) bool {
			return item.Label == "maxTokens" &&
				item.InsertText == "maxTokens = ${1:}" &&
				item.InsertTextFormat != nil &&
				*item.InsertTextFormat == SnippetTextFormat
		}))
		assert.Contains(t, completionItemLabels(items), "temperature")
	})

	t.Run("InterfaceKwargNameCompletionWithoutFactory", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Client struct{}

type Params interface {
	MaxTokens(n int64) Params
	Temperature(v float64) Params
}

var client Client

func (c Client) complete(prompt string, params Params?) {}

func run() {
	client.complete "hi", maxT = 1
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 13, Character: 27})
		labels := completionItemLabels(items)
		assert.NotContains(t, labels, "maxTokens")
		assert.NotContains(t, labels, "temperature")
	})

	t.Run("FreeFunctionInterfaceKwargNameCompletion", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Params interface {
	MaxTokens(n int64) Params
	Temperature(v float64) Params
}

func complete(prompt string, params Params?) {}

func run() {
	complete "hi", maxT = 1
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 20})
		labels := completionItemLabels(items)
		assert.NotContains(t, labels, "maxTokens")
		assert.NotContains(t, labels, "temperature")
	})
}
