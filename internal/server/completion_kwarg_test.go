package server

import (
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletionKwargs(t *testing.T) {
	t.Run("MemberAsValueOrKeyword", func(t *testing.T) {
		const filename = "Record.gox"
		source, pos := typeDisplayTestSource(t, `var next *Record

func configure(opts *Record?) {}
configure ne|xt
`)
		s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
		t.Cleanup(s.Close)
		requireNoDiagnostics(t, s)

		items := completionItemsAt(t, s, filename, pos)
		var insertions []string
		for _, item := range items {
			if item.Label == "next" {
				insertions = append(insertions, item.InsertText)
			}
		}
		assert.ElementsMatch(t, []string{"next", "next = ${1:}"}, insertions)
	})

	t.Run("FunctionOverloads", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			prefix string
			want   []string
			absent string
		}{
			{"Integer", "1", []string{"count"}, "name"},
			{"String", `"text"`, []string{"name"}, "count"},
			{"Unresolved", "missing", []string{"count", "name"}, ""},
		} {
			t.Run(tt.name, func(t *testing.T) {
				call := "handle " + tt.prefix + ", "
				s := newTestServer(t, map[string][]byte{"functions.xgo": []byte(functionOverloadKwargDeclarations), "main.xgo": []byte(call + "option = 1\n")})
				items := completionItemsAt(t, s, "main.xgo", Position{Character: uint32(UTF16Len(call) + 1)})
				for _, label := range tt.want {
					item := completionItemByLabel(items, label)
					require.NotNil(t, item, label)
					assert.Equal(t, FieldCompletion, item.Kind)
					data := requireValueAs[*CompletionItemData](t, item.Data)
					require.NotNil(t, data.Definition)
					wantID := "xgo:main?CountOptions.Count"
					if label == "name" {
						wantID = "xgo:main?NameOptions.Name"
					}
					assert.Equal(t, wantID, data.Definition.String())
				}
				if tt.absent != "" {
					assert.NotContains(t, completionItemLabels(items), tt.absent)
				}
			})
		}
	})

	t.Run("CrossFileSourceKinds", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			filename     string
			needsProject bool
			newServer    testServerFactory
		}{
			{name: "XGo", filename: "main.xgo", newServer: newTestServer},
			{name: "LegacyXGo", filename: "main.gop", newServer: newTestServer},
			{name: "StandaloneClass", filename: "Record.gox", newServer: newTestServer},
			{name: "ProjectClass", filename: "main_fixture.gox", newServer: newFrameworkTestServer},
			{name: "WorkClass", filename: "Worker_fixture.gox", needsProject: true, newServer: newFrameworkTestServer},
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
				s := tt.newServer(t, files)
				items := completionItemsAt(t, s, tt.filename, Position{Line: 2, Character: 23})
				assert.NotContains(t, completionItemLabels(items), "count")
				item := completionItemByLabel(items, "name")
				require.NotNil(t, item)
				assert.Equal(t, 1, countCompletionItemLabel(items, "name"))
				assert.Equal(t, FieldCompletion, item.Kind)
				assert.Equal(t, "name = ${1:}", item.InsertText)
				assert.Equal(t, new(SnippetTextFormat), item.InsertTextFormat)
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
							s := newFrameworkTestServer(t, files)
							items := completionItemsAt(t, s, class.filename, Position{Line: 2, Character: 13})
							item := completionItemByLabel(items, "value")
							require.NotNil(t, item)
							assert.Equal(t, FieldCompletion, item.Kind)
							assert.Equal(t, "value = ${1:}", item.InsertText)
							assert.Equal(t, new(SnippetTextFormat), item.InsertTextFormat)
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
							s := newFrameworkTestServer(t, files)
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
				s := newFrameworkTestServer(t, map[string][]byte{
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
				assert.Equal(t, new(SnippetTextFormat), item.InsertTextFormat)
				data := requireValueAs[*CompletionItemData](t, item.Data)
				assert.Equal(t, "xgo:main?Params.Count", data.Definition.String())
				require.NotNil(t, item.Documentation)
				doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, doc.Value, "func Count(n int) Params")
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

	t.Run("KwargNameCompletionUsesDeclaredSpelling", func(t *testing.T) {
		const filename = "main.xgo"
		code, pos := typeDisplayTestSource(t, `type Options struct {
	count string
	Count int
}

func Configure(opts Options) {}
configure count = "text", Count = 1
configure co|unt = "text"
`)
		s := newImportTestServer(t, map[string][]byte{filename: []byte(code)})
		t.Cleanup(s.Close)
		requireNoDiagnostics(t, s)

		items := completionItemsAt(t, s, filename, pos)
		assert.NotContains(t, completionItemLabels(items), "count")
		index := slices.IndexFunc(items, func(item CompletionItem) bool { return item.Label == "Count" })
		require.NotEqual(t, -1, index)
		assert.Equal(t, "Count = ${1:}", items[index].InsertText)
	})

	t.Run("OverloadKwargNameCompletionAtCheckedCall", func(t *testing.T) {
		const filename = "main.xgo"
		code, pos := typeDisplayTestSource(t, `type Primary struct {
	Count int
	Shared string
}
type Alternative struct {
	Limit int
	Shared string
	//xgols:notRecommended
	Hidden int
}
type Unrelated struct {
	Noise int
}

func usePrimary(opts Primary) {}
func useOther(opts Alternative) {}
func useUnrelated(opts Unrelated) {}
func Configure = (usePrimary; useOther; useUnrelated)
configure co|unt = 1, shared = "text"
`)
		s := newImportTestServer(t, map[string][]byte{filename: []byte(code)})
		t.Cleanup(s.Close)
		requireNoDiagnostics(t, s)

		labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
		assert.Contains(t, labels, "limit")
		assert.NotContains(t, labels, "hidden")
		assert.NotContains(t, labels, "noise")
		assert.NotContains(t, labels, "shared")
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

	t.Run("PositionalValueCompletionWithMarkedKwargs", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, `type Options struct {
	Name string
	//xgols:notRecommended
	Secret string
}
func configure(opts Options?, values ...int) {}
func run() {
	var count int
	var title string
	configure co|, name = "x"
}
`)
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
		assert.Contains(t, labels, "count")
		assert.NotContains(t, labels, "title")
		assert.NotContains(t, labels, "secret")
	})

	t.Run("InterfaceKwargNameCompletion", func(t *testing.T) {
		for _, tt := range []struct {
			name, client string
			want         bool
		}{
			{"Method", "type Client struct{}\nfunc (Client) Params() Params { return nil }\n", true},
			{"FunctionField", "type Client struct { Params func() Params }\n", true},
			{"OptionalMethod", "type Client struct{}\nfunc (Client) Params(label string?) Params { return nil }\n", true},
			{"VariadicMethod", "type Client struct{}\nfunc (Client) Params(labels ...string) Params { return nil }\n", true},
			{"Overload", `type Client struct{}
func (Client) paramsWithLabel(label string) Params { return nil }
func (Client) params() Params { return nil }
func (Client).Params = (
	(Client).paramsWithLabel
	(Client).params
)
`, true},
			{"OverloadWithWrongResult", `type Client struct{}
func (Client) label() string { return "" }
func (Client) params() Params { return nil }
func (Client).Params = (
	(Client).label
	(Client).params
)
`, false},
			{"NonCallableField", "type Client struct { Params int }\n", false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const filename = "main.xgo"
				source, pos := typeDisplayTestSource(t, `type Params interface {
	MaxTokens(n int64) Params
	Temperature(v float64) Params
}
`+tt.client+`var client Client
func (Client) Complete(prompt string, params Params?) {}
client.complete "hi", maxT| = 1
`)
				s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
				t.Cleanup(s.Close)

				items := completionItemsAt(t, s, filename, pos)
				item := completionItemByLabel(items, "maxTokens")
				if !tt.want {
					assert.Nil(t, item)
					assert.NotContains(t, completionItemLabels(items), "temperature")
					return
				}
				require.NotNil(t, item)
				assert.Equal(t, "maxTokens = ${1:}", item.InsertText)
				require.NotNil(t, item.InsertTextFormat)
				assert.Equal(t, SnippetTextFormat, *item.InsertTextFormat)
				assert.Contains(t, completionItemLabels(items), "temperature")

				completed := strings.Replace(source, "maxT =", "maxTokens =", 1)
				s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(completed), Version: 1}})
				requireNoDiagnostics(t, s)
				uri := s.toDocumentURI(filename)
				definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: uri}, Position: pos,
				}})
				require.NoError(t, err)
				assert.Equal(t, Location{URI: uri, Range: Range{
					Start: Position{Line: 1, Character: 1}, End: Position{Line: 1, Character: 10},
				}}, requireValueAs[Location](t, definition))
			})
		}
	})

	t.Run("InterfaceKwargTypeReceiver", func(t *testing.T) {
		for _, tt := range []struct {
			name, class, call string
			want              bool
		}{
			{
				name: "Static",
				class: `func .Params() Params { return nil }
func .Complete(prompt string, params Params?) {}
`,
				call: `Client.Complete "hi", maxT| = 1`,
				want: true,
			},
			{
				name: "MethodExpression",
				class: `func Params() Params { return nil }
func Complete(prompt string, params Params?) {}
`,
				call: `Client.complete client, "hi", maxT| = 1`,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const filename = "main.xgo"
				source, pos := typeDisplayTestSource(t, `type Params interface {
	MaxTokens(n int64) Params
}
var client Client
`+tt.call+"\n")
				s := newImportTestServer(t, map[string][]byte{filename: []byte(source), "Client.gox": []byte(tt.class)})
				t.Cleanup(s.Close)

				labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
				assert.Equal(t, tt.want, slices.Contains(labels, "maxTokens"))
				completed := strings.Replace(source, "maxT =", "maxTokens =", 1)
				s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(completed), Version: 1}})
				if tt.want {
					requireNoDiagnostics(t, s)
				} else {
					_, err := s.getProj().TypeInfo()
					require.ErrorContains(t, err, "not enough arguments")
				}
			})
		}
	})

	t.Run("InterfaceKwargBuilderChain", func(t *testing.T) {
		const declarations = `type Options interface {
	Count(int) Options
	Label(string) Options
}
type Builder struct{}
func (Builder) Count(int) Options { return nil }
type Client struct{}
func (Client) Options() Builder { return Builder{} }
var client Client
`
		const method = "func (Client) Complete(opts Options) {}\n"
		const overload = `func (Client) withOptions(opts Options) {}
func (Client) withLabel(label string, opts Options) {}
func (Client).Complete = (
	(Client).withOptions
	(Client).withLabel
)
`
		for _, tt := range []struct {
			name, callable, call, want, absent string
		}{
			{"InitialReceiver", method, "client.complete co| = 1", "count", "label"},
			{"ReturnedInterface", method, `client.complete count = 1, la| = "text"`, "label", "count"},
			{"EarlierKeywordOnOverload", overload, `client.complete co| = 1, label = "text"`, "count", "label"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const filename = "main.xgo"
				source, pos := typeDisplayTestSource(t, declarations+tt.callable+tt.call+"\n")
				s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
				t.Cleanup(s.Close)
				labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
				assert.Contains(t, labels, tt.want)
				assert.NotContains(t, labels, tt.absent)
			})
		}
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

func TestServerKwargReservedAlias(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
	}{
		{
			name: "Struct",
			source: `type Params struct { For int }
func configure(opts Params?) {}
configure f| = 1
`,
		},
		{
			name: "Interface",
			source: `type Params interface { For(value int) Params }
type Client struct{}
func (Client) Params() Params { return nil }
func (Client) Configure(opts Params?) {}
var client Client
client.configure f| = 1
`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)

			items := completionItemsAt(t, s, filename, pos)
			item := completionItemByLabel(items, "For")
			require.NotNil(t, item)
			assert.Equal(t, "For = ${1:}", item.InsertText)
			assert.NotContains(t, completionItemLabels(items), "for")
			completed := strings.Replace(source, " f = 1", " For = 1", 1)
			s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(completed), Version: 1}})
			requireNoDiagnostics(t, s)

			uri := s.toDocumentURI(filename)
			definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: uri}, Position: pos,
			}})
			require.NoError(t, err)
			start := uint32(strings.Index(completed, "For"))
			assert.Equal(t, Location{URI: uri, Range: Range{
				Start: Position{Character: start}, End: Position{Character: start + 3},
			}}, requireValueAs[Location](t, definition))

			edit, err := s.textDocumentRename(&RenameParams{
				TextDocument: TextDocumentIdentifier{URI: uri}, Position: pos, NewName: "Switch",
			})
			require.NoError(t, err)
			require.NotNil(t, edit)
			updated := applyResourceRenameTestEdits(t, completed, edit.Changes[uri])
			assert.Equal(t, strings.ReplaceAll(completed, "For", "Switch"), updated)
			s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(updated), Version: 2}})
			requireNoDiagnostics(t, s)
		})
	}
}

func TestServerImportedInterfaceKwargFactories(t *testing.T) {
	const filename = "main.xgo"
	const optionsImport = "import \"example.com/options\"\n"
	const privateOptions = `type params interface {
	Count(int) params
	Label(string) params
}
`
	const packageCall = "sdk.complete cou| = 1\n"
	const typeCall = "sdk.Client.complete cou| = 1\n"
	const aliasCall = "sdk.Alias.Complete cou| = 1\n"
	const valueClient = "var client sdk.Client\nclient.complete cou| = 1\n"
	const genericClient = "var client sdk.Client[int]\nclient.complete cou| = 1\n"
	for _, tt := range []struct {
		name, declarations, call string
		want                     bool
	}{
		{"Package", optionsImport + `func Options() options.Options { return nil }
func Complete(options.Options) {}
`, packageCall, true},
		{"PackageAlias", privateOptions + `func Params() params { return nil }
func Complete(params) {}
`, packageCall, true},
		{"PackageWithoutFactory", optionsImport + "func Complete(options.Options) {}\n", packageCall, false},
		{"PackageWithFunctionType", optionsImport + `type Options func() options.Options
func Complete(options.Options) {}
`, packageCall, false},
		{"PackageAliasWithNamedCallable", privateOptions + `type Factory func() params
var Params Factory
func Complete(params) {}
`, packageCall, false},
		{"StaticValue", optionsImport + `const XGoPackage = true
type Client struct{}
var XGos_Client_Options = func() options.Options { return nil }
func XGos_Client_Complete(options.Options) {}
`, typeCall, true},
		{"TypeAliasWithStaticValueFactory", optionsImport + `const XGoPackage = true
type Client struct{}
type Alias = Client
var XGos_Client_Options = func() options.Options { return nil }
var XGos_Client_Complete = func(options.Options) {}
`, aliasCall, true},
		{"PointerAliasWithStaticValueFactory", optionsImport + `const XGoPackage = true
type Client struct{}
type Alias = *Client
var XGos_Client_Options = func() options.Options { return nil }
var XGos_Client_Complete = func(options.Options) {}
`, aliasCall, true},
		{"TypeAliasWithoutStaticMethod", optionsImport + `const XGoPackage = true
type Client struct{}
type Alias = Client
func XGos_Client_Options() options.Options { return nil }
var XGos_Client_Complete = func(options.Options) {}
`, aliasCall, false},
		{"TypeAliasWithoutTemplateMethod", optionsImport + `const XGoPackage = true
type Client struct{}
type Alias = Client
func XGot_Client_Options(client ...Client) options.Options { return nil }
var XGos_Client_Complete = func(options.Options) {}
`, aliasCall, false},
		{"PromotedStaticValue", optionsImport + `const XGoPackage = true
type Base struct{}
type Client struct{ Base }
var XGos_Base_Options = func() options.Options { return nil }
func XGos_Client_Complete(options.Options) {}
`, typeCall, false},
		{"PromotedStaticFunction", optionsImport + `const XGoPackage = true
type Base struct{}
type Nested struct{ Base }
type Other struct{}
type Client struct{ Nested; Other }
func XGos_Base_Options() options.Options { return nil }
func XGos_Other_Options() string { return "" }
func XGos_Client_Complete(options.Options) {}
`, typeCall, true},
		{"StaticConstantShadowsPromotedFactory", optionsImport + `const XGoPackage = true
type Base struct{}
type Client struct{ Base }
func XGos_Base_Options() options.Options { return nil }
const XGos_Client_Options = "options"
func XGos_Client_Complete(options.Options) {}
`, typeCall, false},
		{"UnboundTemplateFactory", optionsImport + `const XGoPackage = true
type Client struct{}
func XGot_Client_Options(client ...Client) options.Options { return nil }
func XGos_Client_Complete(options.Options) {}
`, typeCall, true},
		{"UnboundTemplateRequiresReceiver", optionsImport + `const XGoPackage = true
type Client struct{}
func XGot_Client_Options(client Client) options.Options { return nil }
func XGos_Client_Complete(options.Options) {}
`, typeCall, false},
		{"UnboundTemplateOverloadFactory", optionsImport + `const XGoPackage = true
type Client struct{}
func XGot_Client_Options__0(client Client) string { return "" }
func XGot_Client_Options__1(client ...Client) options.Options { return nil }
func XGos_Client_Complete(options.Options) {}
`, typeCall, true},
		{"UnboundTemplateVariableFactory", optionsImport + `const GopPackage = true
type Client struct{}
var Gopt_Client_Options = func(__xgo_optional_client Client) options.Options { return nil }
func XGos_Client_Complete(options.Options) {}
`, typeCall, true},
		{"StaticFunctionsOnBuilderValue", optionsImport + `const XGoPackage = true
type Client struct{}
type Builder struct{}
func (Client) Options() Builder { return Builder{} }
func (Client) Complete(options.Options) {}
func XGos_Builder_Count(int) options.Options { return nil }
func XGos_Builder_Label(string) options.Options { return nil }
`, valueClient, false},
		{"MethodAlias", privateOptions + `type Client struct{}
func (Client) Params() params { return nil }
func (Client) Complete(params) {}
`, valueClient, true},
		{"Template", optionsImport + `const XGoPackage = true
type Client struct{}
func (Client) Complete(options.Options) {}
func XGot_Client_Options(client Client) options.Options { return nil }
`, valueClient, true},
		{"GenericTemplate", optionsImport + `const XGoPackage = true
type Client[T any] struct{}
func (Client[T]) Complete(options.Options) {}
func XGot_Client_Options[T any](client Client[T]) options.Options { return nil }
`, genericClient, true},
		{"PointerGenericTemplate", optionsImport + `const XGoPackage = true
type Client[T any] struct{}
func (Client[T]) Complete(options.Options) {}
func XGot_Client_Options[T any](client *Client[T]) options.Options { return nil }
`, genericClient, true},
		{"TemplateOverloadSkipsIncompatibleReceiver", optionsImport + `const XGoPackage = true
type Client struct{}
func (Client) Complete(options.Options) {}
func XGot_Client_Options__0(client string) string { return "" }
func XGot_Client_Options__1(client Client) options.Options { return nil }
`, valueClient, true},
		{"TemplateOverloadUsesValueBeforeAddress", optionsImport + `const XGoPackage = true
type Client struct{}
func (Client) Complete(options.Options) {}
func XGot_Client_Options__0(client *Client) string { return "" }
func XGot_Client_Options__1(client Client) options.Options { return nil }
`, valueClient, true},
		{"TemplateOverloadUsesAddress", optionsImport + `const XGoPackage = true
type Client struct{}
func (Client) Complete(options.Options) {}
func XGot_Client_Options__0(client string) string { return "" }
func XGot_Client_Options__1(client *Client) options.Options { return nil }
`, valueClient, true},
		{"TemplateOverloadKeepsFirstApplicableResult", optionsImport + `const XGoPackage = true
type Client struct{}
type Unrelated struct{}
func (Client) Complete(options.Options) {}
func XGot_Client_Options__0(client string) options.Options { return nil }
func XGot_Client_Options__1(client Client) Unrelated { return Unrelated{} }
func XGot_Client_Options__2(client *Client) options.Options { return nil }
`, valueClient, false},
		{"PromotedPointerTemplate", optionsImport + `const XGoPackage = true
type Base struct{}
type Client struct{ Base }
func (Client) Complete(options.Options) {}
func XGot_Base_Options(client *Base) options.Options { return nil }
`, valueClient, true},
		{"InitializedTemplateReceiver", optionsImport + `const XGoPackage = true
type Client struct{}
type Builder struct{}
func (Client) Complete(options.Options) {}
func Builder_Init(client Client) Builder { return Builder{} }
func XGot_Client_Options(client Builder) options.Options { return nil }
`, valueClient, true},
		{"UninferredTypeParameter", optionsImport + `const XGoPackage = true
type Client[T any] struct{}
func (Client[T]) Complete(options.Options) {}
func XGot_Client_Options[T, U any](client Client[T]) options.Options { return nil }
`, genericClient, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `import "example.com/sdk"
`+tt.call)
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			proj := s.getProj()
			packages := make(map[string]*gotypes.Package)
			fallback := proj.Importer
			importer := testImporterFunc(func(path string) (*gotypes.Package, error) {
				if pkg := packages[path]; pkg != nil {
					return pkg, nil
				}
				return fallback.Import(path)
			})
			for _, fixture := range []struct{ path, code string }{
				{"example.com/options", `package options
type Options interface {
	Count(n int) Options
	Label(s string) Options
}
`},
				{"example.com/sdk", "package sdk\n" + tt.declarations},
			} {
				file, err := goparser.ParseFile(proj.Fset, fixture.path+"/api.go", fixture.code, 0)
				require.NoError(t, err)
				pkg, err := (&gotypes.Config{Importer: importer}).Check(fixture.path, proj.Fset, []*goast.File{file}, nil)
				require.NoError(t, err)
				packages[fixture.path] = pkg
			}
			proj.Importer = importer

			labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
			assert.Equal(t, tt.want, slices.Contains(labels, "count"))
			assert.Equal(t, tt.want, slices.Contains(labels, "label"))

			completed := strings.Replace(source, "cou =", "count =", 1)
			s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(completed), Version: 1}})
			if tt.want {
				requireNoDiagnostics(t, s)
			} else {
				_, err := s.requestProject().TypeInfo()
				require.Error(t, err)
			}
		})
	}
}
