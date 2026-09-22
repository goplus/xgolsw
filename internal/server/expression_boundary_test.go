package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletionExpressionBoundaries(t *testing.T) {
	for _, tt := range []struct{ name, source, want, absent string }{
		{"IfCondition", `func candidateBool() bool { return true }
func wrongInt() int { return 1 }
if candidate|Bool() {}
`, "candidateBool", "wrongInt"},
		{"ForCondition", `func candidateBool() bool { return true }
func wrongInt() int { return 1 }
for candidate|Bool() {}
`, "candidateBool", "wrongInt"},
		{"SwitchTag", `func candidateInt() int { return 1 }
switch candidate|Int() {case 1:}
`, "candidateInt", ""},
		{"SwitchCase", `func candidateInt() int { return 1 }
func wrongBool() bool { return false }
switch 1 {case candidate|Int():}
`, "candidateInt", "wrongBool"},
		{"SwitchNoTag", `func candidateBool() bool { return true }
func wrongInt() int { return 1 }
switch {case candidate|Bool():}
`, "candidateBool", "wrongInt"},
		{"SelectSendChannel", `var candidateChannel chan int
select {case candidate|Channel <- 1: default:}
`, "candidateChannel", ""},
		{"SelectReceiveChannel", `var candidateChannel chan int
select {case <-candidate|Channel: default:}
`, "candidateChannel", ""},
		{"CallbackConversion", `type Callback func(int)
func candidate(value int) {}
func wrong(value string) {}
var callback = Callback(cand|idate)
`, "candidate", "wrong"},
		{"TypeAssertionReceiver", `var candidate any
var value = cand|idate.(int)
`, "candidate", ""},
		{"TypeAssertionWithOK", `var candidate any
var value, ok = cand|idate.(int)
`, "candidate", ""},
		{"PointerConversion", `var candidatePointer *int
var value = (*int)(candidate|Pointer)
`, "candidatePointer", ""},
		{"NamedReceive", `type Channel chan int
var candidateChannel Channel
var value = <-candidate|Channel
`, "candidateChannel", ""},
		{"IndexIntegerWidth", `func candidateIndex() int64 { return 0 }
var values []string
var value = values[candidate|Index()]
`, "candidateIndex", ""},
		{"CopyNamedSlice", `type First []int
type Second []int
var destination First
var candidateSource Second
copy(destination, candidate|Source)
`, "candidateSource", ""},
		{"MinCallbackResult", `func candidateInt() int { return 1 }
func wrongBool() bool { return true }
var value = min(1, candidate|Int())
`, "candidateInt", "wrongBool"},
		{"ArrayLength", `const candidateLength = 2
const wrongString = "value"
var values [candidate|Length]int
`, "candidateLength", "wrongString"},
		{"CompoundAssignment", `var value int
func candidateInt() int { return 1 }
func wrongBool() bool { return true }
value += candidate|Int()
`, "candidateInt", "wrongBool"},
		{"RangeSource", `func candidateValues() []int { return nil }
for _, value := range candidate|Values() { echo value }
`, "candidateValues", ""},
		{"ErrorWrapCall", `func candidateRead() (int, error) { return 1, nil }
var value = candidate|Read()!
`, "candidateRead", ""},
		{"ErrorWrapReturn", `func candidateRead() (int, error) { return 1, nil }
func run() int { return candidate|Read()! }
`, "candidateRead", ""},
		{"ErrorPropagation", `func candidateRead() (int, error) { return 1, nil }
func wrongRead() (string, error) { return "", nil }
func run() (int, error) { return candidate|Read()?, nil }
`, "candidateRead", "wrongRead"},
		{"ErrorWrapMultipleResults", `func candidateRead() (int, string, error) { return 1, "", nil }
func wrongRead() (int, error) { return 1, nil }
func run() (int, string) { return candidate|Read()! }
`, "candidateRead", "wrongRead"},
		{"ErrorWrapOnlyError", `func candidateRead() error { return nil }
candidate|Read()!
`, "candidateRead", ""},
		{"ErrorWrapDefault", `func read() (int, error) { return 1, nil }
func candidateInt() int { return 1 }
func wrongBool() bool { return false }
var value = read()?:candidate|Int()
`, "candidateInt", "wrongBool"},
		{"SliceComprehension", `func candidateInt() int { return 1 }
func wrongBool() bool { return false }
var values = [candidate|Int() for _ <- [1]]
`, "candidateInt", "wrongBool"},
		{"MapComprehensionValue", `func candidateInt() int { return 1 }
func wrongBool() bool { return false }
var values = {value: candidate|Int() for value <- [1]}
`, "candidateInt", "wrongBool"},
		{"MapComprehensionKey", `func candidateInt() int { return 1 }
func wrongBool() bool { return false }
var values = {candidate|Int(): value for value <- [1]}
`, "candidateInt", "wrongBool"},
		{"SelectComprehension", `func candidateInt() int { return 1 }
func wrongBool() bool { return false }
var value = {candidate|Int() for _ <- [1]}
`, "candidateInt", "wrongBool"},
		{"ComprehensionCondition", `func candidateBool() bool { return true }
func wrongInt() int { return 1 }
var values = [value for value <- [1], candidate|Bool()]
`, "candidateBool", "wrongInt"},
		{"ComprehensionSource", `func candidateValues() []int { return nil }
var values = [value for value <- candidate|Values()]
`, "candidateValues", ""},
		{"RangeEnd", `func candidateInt() int { return 1 }
func wrongBool() bool { return false }
for value <- 0:candidate|Int() { echo value }
`, "candidateInt", "wrongBool"},
		{"ShiftAssignment", `var value int
func candidateInt() uint64 { return 1 }
func wrongBool() bool { return false }
value <<= candidate|Int()
`, "candidateInt", "wrongBool"},
		{"TypeSwitchCase", `type Candidate struct{}
var value any
switch value.(type) {case Cand|idate:}
`, "Candidate", ""},
		{"SwitchCaseBody", `func candidateVoid() {}
switch 1 {case 1: candidate|Void()}
`, "candidateVoid", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, "main.xgo", pos)
			labels := completionItemLabels(items)
			assert.Contains(t, labels, tt.want)
			if tt.absent != "" {
				assert.NotContains(t, labels, tt.absent)
			}
			if tt.name == "CallbackConversion" {
				item := completionItemByLabel(items, "candidate")
				require.NotNil(t, item)
				assert.Equal(t, "candidate", item.InsertText)
			}
		})
	}

	t.Run("IncompleteBuiltin", func(t *testing.T) {
		for _, name := range []string{"Min", "Max"} {
			t.Run(name, func(t *testing.T) {
				source, pos := typeDisplayTestSource(t, `type Asset string
var asset Asset
func candidateAsset() Asset { return asset }
func wrongBool() bool { return true }
var value = `+strings.ToLower(name)+`(asset, cand|)
`)
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				_, err := s.requestProject().TypeInfo()
				require.Error(t, err)
				labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
				assert.Contains(t, labels, "candidateAsset")
				assert.NotContains(t, labels, "wrongBool")
			})
		}
	})
}

func TestResourceReferencesExpressionBoundaries(t *testing.T) {
	for _, tt := range []struct{ name, source string }{
		{"Equality", `var asset Asset
var valid = asset == "value"
`},
		{"SwitchCase", `var asset Asset
switch asset {case "value":}
`},
		{"Min", `var asset Asset
var value = min(asset, "value")
`},
		{"NotEqual", `var asset Asset
var valid = asset != "value"
`},
		{"ReversedEquality", `var asset Asset
var valid = "value" == asset
`},
		{"Max", `var asset Asset
var value = max("value", asset)
`},
		{"ErrorDefault", `var value = load()?:"value"
`},
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
					source := "func run() {\n" + tt.source + "}\n"
					declarations := "type Asset string\n"
					if kind.name == "Plain" {
						source = declarations + source
						declarations = ""
					}
					s := kind.newServer(t, map[string][]byte{
						kind.filename: []byte(source),
						"resources.xgo": []byte(declarations +
							"func load() (Asset, error) { var value Asset; return value, nil }\n"),
					})
					proj := s.requestProject()
					_, err := proj.TypeInfo()
					require.NoError(t, err)
					id := testResourceID{"files", "value"}
					result := newTestResourceAnalysis(id)
					for ref := range resourceReferences(proj, testResourceResolver(t, proj)) {
						result.addResourceRef(ref)
					}
					require.Len(t, result.resourceRefs, 1)
					assert.Equal(t, id, result.resourceRefs[0].ID)
					assert.Len(t, result.resourceDocumentLinks(proj, kind.filename), 1)
					changes, err := s.renameResourcesAtRefs(proj, result, map[resourceID]string{id: "renamed"})
					require.NoError(t, err)
					edited := applyResourceRenameTestEdits(t, source, changes[s.toDocumentURI(kind.filename)])
					assert.Equal(t, strings.ReplaceAll(source, `"value"`, `"renamed"`), edited)
				})
			}
		})
	}
}

func TestServerRegisteredClassfileExpressionBoundaries(t *testing.T) {
	for _, tt := range []struct{ name, body, want, absent string }{
		{"TypeAssertion", "var candidate any\nvalue := cand|idate.(int)\necho value\n", "candidate", ""},
		{"ArrayLength", "var values [candidate|Length]int\necho values\n", "candidateLength", "wrongBool"},
		{"ErrorWrap", "value := candidate|Read()!\necho value\n", "candidateRead", "wrongRead"},
		{"SliceComprehension", "values := [candidate|Int() for _ <- [1]]\necho values\n", "candidateInt", "wrongBool"},
		{"MapComprehensionKey", "values := {candidate|Int(): value for value <- [1]}\necho values\n", "candidateInt", "wrongBool"},
		{"MapComprehensionValue", "values := {value: candidate|Int() for value <- [1]}\necho values\n", "candidateInt", "wrongBool"},
		{"ComprehensionCondition", "values := [value for value <- [1], candidate|Bool()]\necho values\n", "candidateBool", "candidateInt"},
		{"Switch", "switch 1 {case candidate|Int():}\n", "candidateInt", "wrongBool"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "func run() {\n"+tt.body+"}\n")
			s := newFrameworkTestServer(t, map[string][]byte{
				"main_fixture.gox": []byte(source),
				"helpers.xgo": []byte(`const candidateLength = 2
func candidateInt() int { return 1 }
func candidateBool() bool { return true }
func wrongBool() bool { return false }
func candidateRead() (int, error) { return 1, nil }
func wrongRead() (string, error) { return "", nil }
`),
			})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main_fixture.gox", pos))
			assert.Contains(t, labels, tt.want)
			if tt.absent != "" {
				assert.NotContains(t, labels, tt.absent)
			}
		})
	}
}

func TestServerTextDocumentCompletionDQLContexts(t *testing.T) {
	for _, tt := range []struct{ name, expr, want, absent string }{
		{"SelectReceiver", "cand|idateSelector@node", "candidateSelector", ""},
		{"QuotedSelectReceiver", "cand|idateSelector@\"node-name\"", "candidateSelector", ""},
		{"AnyReceiver", "cand|idateSelector.**.node", "candidateSelector", ""},
		{"WildcardReceiver", "cand|idateSelector.**.*", "candidateSelector", ""},
		{"SelectCallReceiver", "candidate|Source()@node", "candidateSource", ""},
		{"FilterCall", "nodes@candidate|Bool()", "candidateBool", "candidateInt"},
		{"FilterParentheses", "nodes@(candidate|Bool())", "candidateBool", "candidateInt"},
		{"FilterVariable", "nodes@(candidate|Flag)", "candidateFlag", "candidateInt"},
		{"FilterArgument", "nodes@(check(candidate|Int()))", "candidateInt", "candidateBool"},
		{"FilterEnum", "nodes@(candidate|Flag)", "Enabled", "CountOne"},
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
					source, pos := typeDisplayTestSource(t, "func run() {\nvar value = "+tt.expr+"\necho value\n}\n")
					s := kind.newServer(t, map[string][]byte{
						kind.filename: []byte(source),
						"helpers.xgo": []byte(`type Selector struct{ count int }
type Selection struct{ text string }
func (s Selector) XGo_Select(name string) Selection { return Selection{} }
func (s Selector) XGo_Any(name string) Selection { return Selection{} }
var candidateSelector Selector
func candidateSource() Selector { return candidateSelector }
type NodeSet func(func(int) bool)
func (set NodeSet) XGo_Enum() func(func(NodeSet) bool) { return nil }
func (set NodeSet) XGo_first() (int, error) { return 0, nil }
var nodes NodeSet
func candidateBool() bool { return true }
func candidateInt() int { return 1 }
var candidateFlag bool
func check(int) bool { return true }
type Flag const (
Enabled = true
Disabled = false
)
type Count const (
CountOne = 1
CountTwo = 2
)
`),
					})
					_, err := s.requestProject().TypeInfo()
					require.NoError(t, err)
					labels := completionItemLabels(completionItemsAt(t, s, kind.filename, pos))
					assert.Contains(t, labels, tt.want)
					if tt.absent != "" {
						assert.NotContains(t, labels, tt.absent)
					}
				})
			}
		})
	}
}

func TestServerXGoGetInputSlotsExpressionBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name, source, text, want, absent string
	}{
		{"Condition", "if true {}", "true", "flag", "count"},
		{"Switch", "switch count {case 1:}", "1", "count", "flag"},
		{"Comparison", `echo asset == "value"`, `"value"`, "asset", "text"},
		{"Min", `echo min(asset, "value")`, `"value"`, "asset", "text"},
		{"Range", "for value <- 0:3 {echo value}", "3", "count", "flag"},
		{"ShiftAssignment", "count <<= 1", "1", "count", "text"},
		{"NamedArithmeticArgument", "useCount(1 + 2)", "1", "namedCount", "count"},
		{"NamedArithmeticElement", "values := []Count{1 + 2}\necho values", "1", "namedCount", "count"},
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
					source := "func run() {\n" + tt.source + "\n}\n"
					declarations := "type Asset string\nvar asset Asset\nvar count int\nvar flag bool\nvar text string\n" +
						"type Count int\nvar namedCount Count\nfunc useCount(Count) {}\n"
					if kind.name == "Plain" {
						source = declarations + source
						declarations = ""
					}
					s := kind.newServer(t, map[string][]byte{
						kind.filename: []byte(source),
						"values.xgo":  []byte(declarations),
					})
					_, err := s.requestProject().TypeInfo()
					require.NoError(t, err)
					slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(kind.filename)}}})
					require.NoError(t, err)
					var matches []XGoInputSlot
					for _, slot := range slots {
						start := PositionOffset([]byte(source), slot.Range.Start)
						end := PositionOffset([]byte(source), slot.Range.End)
						if source[start:end] == tt.text {
							matches = append(matches, slot)
						}
					}
					require.Len(t, matches, 1)
					assert.Contains(t, matches[0].PredefinedNames, tt.want)
					assert.NotContains(t, matches[0].PredefinedNames, tt.absent)
				})
			}
		})
	}
}

func TestServerXGoGetInputSlotsDQLContexts(t *testing.T) {
	for _, kind := range []struct {
		name, filename string
		newServer      testServerFactory
	}{
		{"Plain", "main.xgo", newTestServer},
		{"Classfile", "main_fixture.gox", newFrameworkTestServer},
	} {
		t.Run(kind.name, func(t *testing.T) {
			source := `func run() {
echo nodes@(true)
echo nodes@flag
echo nodes@"flag-name"
echo nodes.**.flag
}
`
			s := kind.newServer(t, map[string][]byte{
				kind.filename: []byte(source),
				"helpers.xgo": []byte(`type NodeSet func(func(int) bool)
func (set NodeSet) XGo_Enum() func(func(NodeSet) bool) { return nil }
func (set NodeSet) XGo_first() (int, error) { return 0, nil }
func (set NodeSet) XGo_Select(name string) NodeSet { return nil }
func (set NodeSet) XGo_Any(name string) NodeSet { return nil }
var nodes NodeSet
var flag bool
var count int
`),
			})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(kind.filename)}}})
			require.NoError(t, err)
			var conditions []XGoInputSlot
			for _, slot := range slots {
				start := PositionOffset([]byte(source), slot.Range.Start)
				end := PositionOffset([]byte(source), slot.Range.End)
				text := source[start:end]
				assert.NotContains(t, []string{"flag", `"flag-name"`}, text)
				if text == "true" {
					conditions = append(conditions, slot)
				}
			}
			require.Len(t, conditions, 1)
			assert.Equal(t, XGoInputTypeBoolean, conditions[0].Accept.Type)
			assert.Contains(t, conditions[0].PredefinedNames, "flag")
			assert.NotContains(t, conditions[0].PredefinedNames, "count")
		})
	}
}
