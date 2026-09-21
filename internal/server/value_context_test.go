package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletionReceiveContexts(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		invalid      bool
	}{
		{"Value", "var value = <-chan|nel", false},
		{"CommaOK", "var value, ok = <-chan|nel\necho ok", false},
		{"ComparisonLeft", "var value = <-chan|nel == noop()", true},
		{"ComparisonRight", "var value = noop() == <-chan|nel", true},
		{"Addition", "var value = <-chan|nel + noop()", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, kind := range []struct {
				name, filename string
				newServer      testServerFactory
			}{
				{"Plain", "main.xgo", newTestServer},
				{"ProjectClass", "main_fixture.gox", newFrameworkTestServer},
				{"WorkClass", "Worker_fixture.gox", newFrameworkTestServer},
			} {
				t.Run(kind.name, func(t *testing.T) {
					source, pos := typeDisplayTestSource(t, "func run() {\n"+tt.source+"\necho value\n}\n")
					files := map[string][]byte{
						kind.filename: []byte(source),
						"helpers.xgo": []byte("func noop() {}\nvar channel chan int\nvar wrong chan string\n"),
					}
					if kind.filename == "Worker_fixture.gox" {
						files["main_fixture.gox"] = nil
					}
					s := kind.newServer(t, files)
					_, err := s.requestProject().TypeInfo()
					if tt.invalid {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
					}
					labels := completionItemLabels(completionItemsAt(t, s, kind.filename, pos))
					assert.Contains(t, labels, "channel")
					if !tt.invalid {
						assert.NotContains(t, labels, "wrong")
					}
				})
			}
		})
	}
}

func TestServerTextDocumentCompletionValueContexts(t *testing.T) {
	for _, tt := range []struct{ name, source, want, absent string }{
		{"SecondDeclaration", `func candidateInt() int { return 1 }
func candidateString() string { return "" }
var number, text = 1, candidate|String()
`, "candidateString", "candidateInt"},
		{"ForwardResults", `func candidatePair() (int, string) { return 1, "" }
func candidateInt() int { return 1 }
func forward() (int, string) { return candidate|Pair() }
`, "candidatePair", "candidateInt"},
		{"CallInDeclaration", `func candidateInt() int { return 1 }
func candidateString() string { return "" }
func convert(value int) string { return "" }
var text string = convert(candidate|Int())
`, "candidateInt", "candidateString"},
		{"CallInReturn", `func candidateInt() int { return 1 }
func candidateString() string { return "" }
func convert(value int) string { return "" }
func run() string { return convert(candidate|Int()) }
`, "candidateInt", "candidateString"},
		{"CallbackInStruct", `func candidate(value int) {}
func wrong(value string) {}
type Config struct { Callback func(int) }
var config = Config{Callback: candidate|}
`, "candidate", "wrong"},
		{"LambdaInStruct", `func candidateInt() int { return 1 }
func candidateString() string { return "" }
type Config struct { Callback func() int }
var config = Config{Callback: () => { return candidate|Int() }}
`, "candidateInt", "candidateString"},
		{"CallbackInSlice", `func candidate(value int) {}
func wrong(value string) {}
var callbacks []func(int) = [candidate|]
`, "candidate", "wrong"},
		{"DeclarationResults", `func candidatePair() (int, string) { return 1, "" }
func candidateInt() int { return 1 }
var number, text = candidate|Pair()
`, "candidatePair", "candidateInt"},
		{"SecondReturn", `func candidateInt() int { return 1 }
func candidateString() string { return "" }
func pair() (int, string) { return 1, candidate|String() }
`, "candidateString", "candidateInt"},
		{"LambdaInMap", `func candidateInt() int { return 1 }
func candidateString() string { return "" }
var callbacks = map[string]func() int{"value": () => { return candidate|Int() }}
`, "candidateInt", "candidateString"},
		{"ArrowInNestedComposite", `func candidateInt() int { return 1 }
func candidateString() string { return "" }
type Config struct { Callback func() int }
var callbacks = []Config{{Callback: () => candidate|Int()}}
`, "candidateInt", "candidateString"},
		{"LambdaMultipleResults", `func candidatePair() (int, string) { return 1, "" }
func candidateInt() int { return 1 }
func use(callback func() (int, string)) {}
use(() => { return candidate|Pair() })
`, "candidatePair", "candidateInt"},
		{"PointerComposite", `func candidate(value int) {}
func wrong(value string) {}
type Config struct { Callback func(int) }
var config = &Config{Callback: candidate|}
`, "candidate", "wrong"},
		{"CompositeThroughInterface", `func candidate(value int) {}
func wrong(value string) {}
type Config struct { Callback func(int) }
var config any = Config{Callback: candidate|}
`, "candidate", "wrong"},
		{"MapKey", `func candidateInt() int { return 1 }
func candidateString() string { return "" }
var values = map[int]string{candidate|Int(): "value"}
`, "candidateInt", "candidateString"},
		{"MapValue", `func candidateInt() int { return 1 }
func candidateString() string { return "" }
var values = map[string]int{"key": candidate|Int()}
`, "candidateInt", "candidateString"},
		{"ArrayIndex", `const candidateInt = 0
const candidateString = "value"
var values = [2]string{candidate|Int: "value"}
`, "candidateInt", "candidateString"},
		{"PositionalStruct", `func candidate(value int) {}
func wrong(value string) {}
type Config struct { Callback func(int) }
var config = Config{candidate|}
`, "candidate", "wrong"},
		{"NestedComposite", `func candidate(value int) {}
func wrong(value string) {}
type Config struct { Callback func(int) }
var configs = []Config{{Callback: candidate|}}
`, "candidate", "wrong"},
		{"CallInStruct", `func candidateInt() int { return 1 }
func candidateString() string { return "" }
func convert(value int) string { return "" }
type Config struct { Text string }
var config = Config{Text: convert(candidate|Int())}
`, "candidateInt", "candidateString"},
		{"LambdaKwarg", `func candidateInt() int { return 1 }
func candidateString() string { return "" }
type Options struct { Callback func() int }
func use(options Options?) {}
use callback = () => { return candidate|Int() }
`, "candidateInt", "candidateString"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Contains(t, labels, tt.want)
			assert.NotContains(t, labels, tt.absent)
		})
	}
}

func TestResourceReferencesCompositeValues(t *testing.T) {
	for _, tt := range []struct{ name, source string }{
		{"StructField", `type Config struct { Value Asset }
var config = Config{Value: "value"}
`},
		{"MapValue", `var values = map[string]Asset{"key": "value"}
`},
		{"LambdaField", `type Config struct { Callback func() Asset }
var config = Config{Callback: () => { return "value" }}
`},
		{"PositionalStruct", `type Config struct { Value Asset }
var config = Config{"value"}
`},
		{"MapKey", `var values = map[Asset]string{"value": "ignored"}
`},
		{"NestedComposite", `type Config struct { Value Asset }
var values = []Config{{Value: "value"}}
`},
		{"InterfaceComposite", `type Config struct { Value Asset }
var value any = Config{Value: "value"}
`},
		{"PointerComposite", `type Config struct { Value Asset }
var value = &Config{Value: "value"}
`},
		{"ArrayValue", `var values = [2]Asset{1: "value"}
`},
		{"CompositeCall", `type Config struct { Value Asset }
func use(value Config) {}
use(Config{Value: "value"})
`},
		{"InterfaceCall", `type Config struct { Value Asset }
func use(value any) {}
use(Config{Value: "value"})
`},
		{"ArrowField", `type Config struct { Callback func() Asset }
var value = Config{Callback: () => "value"}
`},
		{"LambdaInMap", `var values = map[string]func() Asset{"ignored": () => { return "value" }}
`},
		{"ArrowInNestedComposite", `type Config struct { Callback func() Asset }
var values = []Config{{Callback: () => "value"}}
`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("type Asset string\n" + tt.source)})
			proj := s.requestProject()
			_, err := proj.TypeInfo()
			require.NoError(t, err)
			var names []string
			for ref := range resourceReferences(proj, testResourceResolver(t, proj)) {
				names = append(names, ref.ID.Name())
			}
			assert.Equal(t, []string{"value"}, names)
		})
	}
}

func TestServerXGoGetInputSlotsValueContexts(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		count        int
	}{
		{"TupleDeclaration", `var pair (int, string) = (1, "value")`, 2},
		{"TupleReturn", `type Pair (int, string)
func pair() Pair { return (1, "value") }`, 2},
		{"LambdaReturn", `func use(callback func() int) {}
use(() => { return 1 })`, 1},
		{"CompoundAssignment", `count += 1`, 2},
		{"TupleAssignment", `var pair (int, string)
pair = (1, "value")`, 3},
		{"StructField", `type Config struct { Value int }
var config = Config{Value: 1}`, 1},
		{"MapValue", `var values = map[string]int{"value": 1}`, 2},
		{"NestedComposite", `type Config struct { Value int }
var configs = []Config{{Value: 1}}`, 1},
		{"ArrowField", `type Config struct { Callback func() int }
var config = Config{Callback: () => 1}`, 1},
		{"LambdaField", `type Config struct { Callback func() int }
var config = Config{Callback: () => { return 1 }}`, 1},
		{"FunctionReturn", `func current() int { return 1 }`, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("var count int\nvar name string\n" + tt.source + "\n")})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
			require.NoError(t, err)
			assert.Len(t, slots, tt.count)
			for _, slot := range slots {
				if slot.Kind != XGoInputSlotKindValue {
					continue
				}
				switch slot.Accept.Type {
				case XGoInputTypeInteger:
					assert.Contains(t, slot.PredefinedNames, "count")
					assert.NotContains(t, slot.PredefinedNames, "name")
				case XGoInputTypeString:
					assert.Contains(t, slot.PredefinedNames, "name")
					assert.NotContains(t, slot.PredefinedNames, "count")
				}
			}
		})
	}
}

func TestResourceReferencesLiteralCallContext(t *testing.T) {
	for _, tt := range []struct{ name, source string }{
		{"Composite", "type Config struct { Value Asset }\nfunc use(value any) {}\nuse(Config{Value: \"value\"})\n"},
		{"Slice", "func use(values []Asset) {}\nuse([\"value\"])\n"},
		{"Tuple", "func use(value (int, Asset)) {}\nuse((1, \"value\"))\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, available := range []bool{false, true} {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte("type Asset string\n" + tt.source)})
				proj := s.requestProject()
				_, err := proj.TypeInfo()
				require.NoError(t, err)
				resolve := testResourceResolver(t, proj)
				var refs []resourceRef
				for ref := range resourceReferences(proj, func(value resourceValue) (resourceID, bool) {
					id, recognized := resolve(value)
					if recognized {
						assert.NotNil(t, value.Call)
						assert.False(t, value.Intrinsic)
						if !available {
							return nil, true
						}
					}
					return id, recognized
				}) {
					refs = append(refs, ref)
				}
				if available {
					require.Len(t, refs, 1)
					assert.Equal(t, "value", refs[0].ID.Name())
				} else {
					assert.Empty(t, refs)
				}
			}
		})
	}
}
