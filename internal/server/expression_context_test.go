package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletionExpressionContexts(t *testing.T) {
	for _, tt := range []struct{ name, source, want string }{
		{"ComparisonDeclaration", `func candidateInt() int { return 1 }
var valid = candidate|Int() > 0
`, "candidateInt"},
		{"ComparisonReturn", `func candidateInt() int { return 1 }
func valid() bool { return candidate|Int() > 0 }
`, "candidateInt"},
		{"IndexDeclaration", `func candidateInt() int { return 0 }
var values = []string{"value"}
var value = values[candidate|Int()]
`, "candidateInt"},
		{"MapIndexDeclaration", `func candidateInt() int { return 0 }
var values = map[int]string{0: "value"}
var value = values[candidate|Int()]
`, "candidateInt"},
		{"SliceBound", `func candidateInt() int { return 0 }
var values = []int{1}
var subset = values[candidate|Int():]
`, "candidateInt"},
		{"AddressOf", `var target int
var ptr = &tar|get
`, "target"},
		{"Receive", `var channel chan int
var value = <-chan|nel
`, "channel"},
		{"Dereference", `var pointer *int
var value = *poin|ter
`, "pointer"},
		{"IndexReceiver", `func candidateValues() []int { return nil }
var value = candidate|Values()[0]
`, "candidateValues"},
		{"SelectorReceiver", `type Record struct { Value int }
func candidateRecord() Record { return Record{} }
var value = candidate|Record().Value
`, "candidateRecord"},
		{"EmptySliceElement", `func use(values []int) {}
func candidateInt() int { return 1 }
use([|])
`, "candidateInt"},
		{"EmptyTupleElement", `func use(pair (int, string)) {}
func candidateString() string { return "" }
use((1, |))
`, "candidateString"},
		{"ReceiveWithOK", `var channel chan int
var value, ok = <-chan|nel
`, "channel"},
		{"AppendCallback", `func candidate(value int) {}
var callbacks []func(int)
var values = append(callbacks, cand|idate)
`, "candidate"},
		{"IncompleteCall", `func candidateInt() int { return 1 }
var value string = missing(candidate|Int())
`, "candidateInt"},
		{"Send", `func candidateInt() int { return 1 }
var channel chan int
channel <- candidate|Int()
`, "candidateInt"},
		{"SliceAppend", `func candidateInt() int { return 1 }
var values []int
values <- candidate|Int()
`, "candidateInt"},
		{"TupleLeadingSpace", `func use(pair (int, string)) {}
func candidateInt() int { return 1 }
use(( | 1, "value"))
`, "candidateInt"},
		{"EmptyTypedSlice", `func candidateInt() int { return 1 }
var values = []int{|}
`, "candidateInt"},
		{"EmptyMap", `func candidateInt() int { return 1 }
var values = map[int]string{|}
`, "candidateInt"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "func wrongBool() bool { return false }\n"+tt.source)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			if tt.name != "EmptyTupleElement" && tt.name != "IncompleteCall" {
				require.NoError(t, err)
			}
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Contains(t, labels, tt.want)
			if tt.name != "IndexReceiver" && tt.name != "SelectorReceiver" && tt.name != "IncompleteCall" {
				assert.NotContains(t, labels, "wrongBool")
			}
		})
	}
}

func TestResourceReferencesExpressionContexts(t *testing.T) {
	for _, tt := range []struct{ name, source string }{
		{"Append", `var values []Asset
values = append(values, "value")
`},
		{"ChannelSend", `var values chan Asset
values <- "value"
`},
		{"SliceAppend", `var values []Asset
values <- "value"
`},
		{"MapIndex", `var values map[Asset]int
var number = values["value"]
`},
		{"InterfaceCollection", `var values any = []Asset{"value"}
`},
		{"CompositeCall", `type Config struct { Value Asset }
func use(value any) {}
use(Config{Value: "value"})
`},
		{"AppendSlice", `var values []Asset
values = append(values, [Asset("value")]...)
`},
		{"SendSlice", `var values []Asset
values <- [Asset("value")]...
`},
		{"Copy", `var values []Asset
copy(values, []Asset{"value"})
`},
		{"Delete", `var values map[Asset]int
delete(values, "value")
`},
		{"ShadowedAppend", `func append(value Asset) {}
append("value")
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

func TestServerXGoGetInputSlotsExpressionContexts(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		count        int
	}{
		{"Append", `var values []int
values = append(values, 1)
`, 2},
		{"ChannelSend", `var values chan int
values <- 1
`, 1},
		{"SliceAppend", `var values []int
values <- 1
`, 1},
		{"MapIndex", `var values map[int]string
var value = values[1]
`, 1},
		{"ByteAppendString", `var values []byte
values = append(values, "text"...)
`, 2},
		{"ByteSendString", `var values []byte
values <- "text"...
`, 1},
		{"ByteCopyString", `var values []byte
copy(values, "text")
`, 2},
		{"Delete", `var values map[int]string
delete(values, 1)
`, 1},
		{"Make", `var values = make([]int, 1)
`, 1},
		{"SliceBounds", `var values []int
var subset = values[1:2:3]
`, 3},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("var count int\nvar text string\n" + tt.source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
			require.NoError(t, err)
			assert.Len(t, slots, tt.count)
			for _, slot := range slots {
				if slot.Kind != XGoInputSlotKindValue {
					continue
				}
				switch slot.Input.Type {
				case XGoInputTypeInteger:
					assert.Equal(t, XGoInputTypeInteger, slot.Accept.Type)
					assert.Contains(t, slot.PredefinedNames, "count")
					assert.NotContains(t, slot.PredefinedNames, "text")
				case XGoInputTypeString:
					assert.Equal(t, XGoInputTypeString, slot.Accept.Type)
					assert.Contains(t, slot.PredefinedNames, "text")
					assert.NotContains(t, slot.PredefinedNames, "count")
				}
			}
		})
	}
}

func TestServerTextDocumentCompletionKeyedEnums(t *testing.T) {
	for _, tt := range []struct{ name, source string }{
		{"KeyedArrayValue", `var values = [2]First{1: Fir|stMember}`},
		{"KeyedSliceValue", `var values = []First{1: Fir|stMember}`},
		{"MapKey", `var values = map[First]int{Fir|stMember: 1}`},
		{"LambdaField", `type Config struct { Callback func() First }
var config = Config{Callback: () => { return Fir|stMember }}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "type First const (FirstMember = iota)\ntype Second const (SecondMember = iota)\n"+tt.source+"\n")
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Contains(t, labels, "FirstMember")
			assert.NotContains(t, labels, "SecondMember")
		})
	}
}

func TestServerRegisteredClassfileExpressionContexts(t *testing.T) {
	for _, tt := range []struct{ name, expression string }{
		{"Comparison", "candidate|Int() > 0"},
		{"Index", "values[candidate|Int()]"},
		{"Slice", "values[candidate|Int():]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "var (\nvalues []string\n)\nfunc run() {\nvalue := "+tt.expression+"\necho value\n}\n")
			s := newFrameworkTestServer(t, map[string][]byte{
				"main_fixture.gox": []byte(source),
				"helpers.xgo":      []byte("func candidateInt() int { return 0 }\nfunc wrongBool() bool { return false }\n"),
			})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main_fixture.gox", pos))
			assert.Contains(t, labels, "candidateInt")
			assert.NotContains(t, labels, "wrongBool")
		})
	}

	t.Run("Resources", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`var (
assets []Asset
channel chan Asset
index map[Asset]int
)
assets = append(assets, "append")
channel <- "send"
echo index["index"]
`),
			"resources.xgo": []byte("type Asset string\n"),
		})
		proj := s.requestProject()
		_, err := proj.TypeInfo()
		require.NoError(t, err)
		var names []string
		for ref := range resourceReferences(proj, testResourceResolver(t, proj)) {
			names = append(names, ref.ID.Name())
		}
		assert.ElementsMatch(t, []string{"append", "send", "index"}, names)
	})

	t.Run("InputSlots", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(`var (
count int
text string
values []int
channel chan int
index map[int]string
)
values = append(values, 1)
channel <- 2
echo index[3]
`)})
		_, err := s.requestProject().TypeInfo()
		require.NoError(t, err)
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}}})
		require.NoError(t, err)
		var values []any
		for _, slot := range slots {
			if slot.Input.Kind == XGoInputKindInPlace {
				values = append(values, slot.Input.Value)
				assert.Equal(t, XGoInputTypeInteger, slot.Accept.Type)
				assert.Contains(t, slot.PredefinedNames, "count")
				assert.NotContains(t, slot.PredefinedNames, "text")
			}
		}
		assert.Equal(t, []any{int64(1), int64(2), int64(3)}, values)
	})
}
