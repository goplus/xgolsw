package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletionCalleeContexts(t *testing.T) {
	for _, tt := range []struct {
		name, body, want string
		absent           []string
		incomplete       bool
	}{
		{"Declaration", "var value = cand|idate()()\nreturn value", "candidate", []string{"wrongValue", "wrongParameter", "wrongResult"}, false},
		{"Assignment", "var value int\nvalue = cand|idate()()\nreturn value", "candidate", []string{"wrongValue", "wrongParameter", "wrongResult"}, false},
		{"Argument", "consume(cand|idate()())\nreturn 0", "candidate", []string{"wrongValue", "wrongParameter", "wrongResult"}, false},
		{"Return", "return cand|idate()()", "candidate", []string{"wrongValue", "wrongParameter", "wrongResult"}, false},
		{"Parentheses", "return (cand|idate())()", "candidate", []string{"wrongValue", "wrongParameter", "wrongResult"}, false},
		{"Nested", "return candidate|Nested()()()", "candidateNested", []string{"candidate", "wrongValue"}, false},
		{"Variable", "return call|back()()", "callback", []string{"wrongValue", "wrongParameter", "wrongResult"}, false},
		{"Method", "return factory.Cand|idate()()", "Candidate", nil, false},
		{"Map", "return cand|idateMap[\"key\"]()", "candidateMap", nil, false},
		{"Pointer", "return (*cand|idatePointer)()", "candidatePointer", nil, false},
		{"Incomplete", "return cand|()()", "candidate", nil, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, kind := range []struct {
				name, filename string
				newServer      testServerFactory
			}{
				{"Plain", "main.xgo", newTestServer},
				{"Classfile", "main_fixture.gox", newFrameworkTestServer},
			} {
				t.Run(kind.name, func(t *testing.T) {
					source, pos := typeDisplayTestSource(t, "func run() int {\n"+tt.body+"\n}\n")
					s := kind.newServer(t, map[string][]byte{
						kind.filename: []byte(source),
						"helpers.xgo": []byte(`func candidate() func() int { return nil }
func candidateNested() func() func() int { return nil }
func wrongValue() int { return 0 }
func wrongParameter() func(string) int { return nil }
func wrongResult() func() string { return nil }
func consume(int) {}
var callback func() func() int
var candidateMap map[string]func() int
var candidatePointer *func() int
type Factory struct{}
func (Factory) Candidate() func() int { return nil }
func (Factory) WrongValue() int { return 0 }
var factory Factory
`),
					})
					_, err := s.requestProject().TypeInfo()
					if tt.incomplete {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
					}
					items := completionItemsAt(t, s, kind.filename, pos)
					labels := completionItemLabels(items)
					assert.Contains(t, labels, tt.want)
					for _, absent := range tt.absent {
						assert.NotContains(t, labels, absent)
					}
					if tt.want == "candidate" || tt.want == "candidateNested" {
						item := completionItemByLabel(items, tt.want)
						require.NotNil(t, item)
						assert.Equal(t, tt.want, item.InsertText)
					}
				})
			}
		})
	}
}

func TestServerTextDocumentCompletionValues(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		want   []string
		absent []string
	}{
		{
			name: "Callbacks",
			source: `func use(callback func(int)) {}
func candidateFunc(value int) {}
var candidateVariable func(int)
type Callback func(int)
var candidateNamed Callback
func wrongArgument(value string) {}
func wrongArity() {}
func wrongVariadic(values ...int) {}
use(candidate|Func)
`,
			want:   []string{"candidateFunc", "candidateVariable", "candidateNamed"},
			absent: []string{"wrongArgument", "wrongArity", "wrongVariadic"},
		},
		{
			name: "CallbackInsideImmediateCall",
			source: `func use(callback func(int)) {}
func candidateFunc(value int) {}
func wrongArgument(value string) {}
func() { use(candidate|Func) }()
`,
			want: []string{"candidateFunc"}, absent: []string{"wrongArgument"},
		},
		{
			name: "CallbackInsideCallee",
			source: `func factory(callback func(int)) func() { return nil }
func candidateFunc(value int) {}
func wrongArgument(value string) {}
factory(candidate|Func)()
`,
			want: []string{"candidateFunc"}, absent: []string{"wrongArgument"},
		},
		{
			name: "CallbackAssignment",
			source: `func candidateFunc(value int) {}
func wrongArgument(value string) {}
var callback func(int) = candidate|Func
`,
			want: []string{"candidateFunc"}, absent: []string{"wrongArgument"},
		},
		{
			name: "CallbackResult",
			source: `func use(callback func(int)) {}
func candidateFunc(value int) {}
func factory() func(int) { return nil }
use(fac|tory())
`,
			want: []string{"factory"}, absent: []string{"candidateFunc"},
		},
		{
			name: "MultipleResults",
			source: `func use(first int, second string) {}
func candidatePair() (int, string) { return 1, "" }
func wrongSecond() (int, bool) { return 1, false }
func wrongCount() int { return 1 }
use(candidate|Pair())
`,
			want: []string{"candidatePair"}, absent: []string{"wrongSecond", "wrongCount"},
		},
		{
			name: "VariadicResults",
			source: `func use(first int, rest ...string) {}
func candidatePair() (int, string) { return 1, "" }
func candidateTriple() (int, string, string) { return 1, "", "" }
func candidateSingle() int { return 1 }
func wrongSecond() (int, bool) { return 1, false }
func wrongFirst() string { return "" }
use(candidate|Pair())
`,
			want: []string{"candidatePair", "candidateTriple", "candidateSingle"}, absent: []string{"wrongSecond", "wrongFirst"},
		},
		{
			name: "VariadicOnlyResults",
			source: `func use(values ...int) {}
func candidatePair() (int, int) { return 1, 2 }
func candidateSingle() int { return 1 }
func wrongSecond() (int, bool) { return 1, false }
func wrongCount() {}
use(candidate|Pair())
`,
			want: []string{"candidatePair", "candidateSingle"}, absent: []string{"wrongSecond", "wrongCount"},
		},
		{
			name: "MultipleAssignment",
			source: `func candidatePair() (int, string) { return 1, "" }
func wrongSecond() (int, bool) { return 1, false }
var first int
var second string
first, second = candidate|Pair()
`,
			want: []string{"candidatePair"}, absent: []string{"wrongSecond"},
		},
		{
			name: "LambdaReturn",
			source: `func use(callback func() int) {}
func candidateInt() int { return 1 }
func candidateString() string { return "" }
func run() string {
	use(() => { return candidate|Int() })
	return ""
}
`,
			want: []string{"candidateInt"}, absent: []string{"candidateString"},
		},
		{
			name: "ArrowResult",
			source: `func use(callback func() int) {}
func candidateInt() int { return 1 }
func candidateString() string { return "" }
use(() => candidate|Int())
`,
			want: []string{"candidateInt"}, absent: []string{"candidateString"},
		},
		{
			name: "NestedLambdaReturn",
			source: `func use(callback func() func() int) {}
func candidateInt() int { return 1 }
func candidateString() string { return "" }
use(() => { return () => { return candidate|Int() } })
`,
			want: []string{"candidateInt"}, absent: []string{"candidateString"},
		},
		{
			name: "TupleValue",
			source: `func use(pair (int, string)) {}
func candidateInt() int { return 1 }
func candidateString() string { return "" }
use((1, candidate|String()))
`,
			want: []string{"candidateString"}, absent: []string{"candidateInt"},
		},
		{
			name: "NestedTupleValue",
			source: `type Pair (int, string)
func use(pair (int, Pair)) {}
func candidateInt() int { return 1 }
func candidateString() string { return "" }
use((1, (2, candidate|String())))
`,
			want: []string{"candidateString"}, absent: []string{"candidateInt"},
		},
		{
			name: "MethodExpressionCallback",
			source: `type Handler struct{}
func (h Handler) Apply(value int) {}
func use(callback func(Handler, int)) {}
use(Handler.Ap|ply)
`,
			want: []string{"Apply"},
		},
		{
			name: "PointerMethodExpressionCallback",
			source: `type Handler struct{}
func (h *Handler) Apply(value int) {}
func use(callback func(*Handler, int)) {}
use((*Handler).Ap|ply)
`,
			want: []string{"Apply"},
		},
		{
			name: "PromotedMethodExpressionCallback",
			source: `type Base struct{}
func (b Base) Apply(value int) {}
type Handler struct { Base }
func use(callback func(Handler, int)) {}
use(Handler.Ap|ply)
`,
			want: []string{"Apply"},
		},
		{
			name: "InterfaceMethodExpressionCallback",
			source: `type Handler interface { Apply(int) }
func use(callback func(Handler, int)) {}
use(Handler.Ap|ply)
`,
			want: []string{"Apply"},
		},
		{
			name: "ImportedCallback",
			source: `import "example.com/framework"
func use(callback func() string) {}
var item framework.Item
use(item.La|bel)
`,
			want: []string{"Label"}, absent: []string{"label", "play"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			factory := newTestServer
			if tt.name == "ImportedCallback" {
				factory = newFrameworkTestServer
			}
			s := factory(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, "main.xgo", position)
			labels := completionItemLabels(items)
			for _, label := range tt.want {
				assert.Contains(t, labels, label)
			}
			for _, label := range tt.absent {
				assert.NotContains(t, labels, label)
			}
			if tt.name == "ImportedCallback" {
				item := completionItemByLabel(items, "Label")
				require.NotNil(t, item)
				assert.Equal(t, "Label", item.InsertText)
				s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(strings.Replace(source, "item.Label", "item."+item.InsertText, 1)), Version: 1}})
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
			}
		})
	}
}
