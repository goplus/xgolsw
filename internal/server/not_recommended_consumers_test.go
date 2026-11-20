package server

import (
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerNotRecommendedIdentifierCompletions(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `//xgols:notRecommended
func HiddenAction() {}
func VisibleAction() {}
//xgols:notRecommended
var HiddenValue int
var VisibleValue int
//xgols:notRecommended
const HiddenLimit = 1
const VisibleLimit = 2
//xgols:notRecommended
type HiddenRecord struct{}
type VisibleRecord struct{}
func run() {
|
}
`)
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
	for _, name := range []string{"hiddenAction", "HiddenAction", "HiddenValue", "HiddenLimit", "HiddenRecord"} {
		assert.NotContains(t, labels, name)
	}
	for _, name := range []string{"VisibleAction", "VisibleValue", "VisibleLimit", "VisibleRecord"} {
		assert.Contains(t, labels, name)
	}
}

func TestServerNotRecommendedMemberCompletions(t *testing.T) {
	for _, tt := range []struct {
		name, expression, want string
	}{
		{name: "Call", expression: "record.|VisibleAction(1)", want: "VisibleAction"},
		{name: "AutoProperty", expression: "println record.|visibleText", want: "VisibleText"},
		{name: "MethodExpression", expression: "_ = Record.|VisibleAction", want: "VisibleAction"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "var record Record\n"+tt.expression+"\n")
			s := newTestServer(t, map[string][]byte{
				"main.xgo": []byte(source),
				"Record.gox": []byte(`var (
//xgols:notRecommended
HiddenField string
VisibleField string
)
//xgols:notRecommended
func HiddenAction(value int) {}
func VisibleAction(value int) {}
//xgols:notRecommended
func HiddenText() string { return "hidden" }
func VisibleText() string { return "visible" }
`),
			})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			for _, name := range []string{"HiddenField", "hiddenAction", "HiddenAction", "hiddenText", "HiddenText"} {
				assert.NotContains(t, labels, name)
			}
			assert.Contains(t, labels, tt.want)
		})
	}
}

func TestServerNotRecommendedOverloadCompletions(t *testing.T) {
	for _, tt := range []struct {
		name, implementation, alias string
		want                        int
	}{
		{name: "Implementation", implementation: "//xgols:notRecommended\n", want: 1},
		{name: "Alias", alias: "//xgols:notRecommended\n"},
		{name: "Unannotated", want: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.implementation+`func integer(value int) int { return value }
func text(value string) string { return value }
`+tt.alias+`func Choose = (integer; text)
func run() {
|
}
`)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, "main.xgo", pos)
			assert.Equal(t, tt.want, countCompletionItemLabel(items, "Choose"))
			labels := completionItemLabels(items)
			assert.Contains(t, labels, "text")
			if tt.implementation == "" {
				assert.Contains(t, labels, "integer")
			} else {
				assert.NotContains(t, labels, "integer")
			}
		})
	}
}

func TestServerNotRecommendedPredefinedNames(t *testing.T) {
	s := newTestServer(t, map[string][]byte{"Record.gox": []byte(`var (
//xgols:notRecommended
HiddenField string
VisibleField string
)
//xgols:notRecommended
func HiddenText() string { return "hidden" }
func VisibleText() string { return "visible" }
func run() { use "text" }
`), "values.xgo": []byte(`func Use(value string) {}
//xgols:notRecommended
var hiddenValue string
var visibleValue string
//xgols:notRecommended
const hiddenLimit = "hidden"
const visibleLimit = "visible"
//xgols:notRecommended
func HiddenPackageText() string { return "hidden" }
func VisiblePackageText() string { return "visible" }
`)})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///Record.gox"}}})
	require.NoError(t, err)
	require.NotEmpty(t, slots)
	names := slots[len(slots)-1].PredefinedNames
	for _, name := range []string{"HiddenField", "hiddenText", "hiddenValue", "hiddenLimit", "hiddenPackageText"} {
		assert.NotContains(t, names, name)
	}
	for _, name := range []string{"VisibleField", "visibleText", "visibleValue", "visibleLimit", "visiblePackageText"} {
		assert.Contains(t, names, name)
	}
}

func TestServerNotRecommendedAutoPropertyOverloads(t *testing.T) {
	for _, tt := range []struct {
		name, first, second, alias string
		want                       bool
	}{
		{name: "SelectedImplementation", first: "//xgols:notRecommended\n"},
		{name: "Alias", alias: "//xgols:notRecommended\n"},
		{name: "OtherImplementation", second: "//xgols:notRecommended\n", want: true},
		{name: "Unannotated", want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.first+`func first() string { return "first" }
`+tt.second+`func second() string { return "second" }
`+tt.alias+`func Label = (first; second)
func Use(value string) {}
func run() { use |"text" }
`)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
			require.NoError(t, err)
			require.NotEmpty(t, slots)
			if tt.want {
				assert.Contains(t, labels, "label")
				assert.Contains(t, slots[len(slots)-1].PredefinedNames, "label")
			} else {
				assert.NotContains(t, labels, "label")
				assert.NotContains(t, slots[len(slots)-1].PredefinedNames, "label")
			}
		})
	}
}

func TestServerNotRecommendedShadowing(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `var value string
func Use(value string) {}
func run() {
//xgols:notRecommended
var value string
_ = value
|
use "text"
}
`)
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	assert.NotContains(t, completionItemLabels(completionItemsAt(t, s, "main.xgo", pos)), "value")
	slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
	require.NoError(t, err)
	require.NotEmpty(t, slots)
	assert.NotContains(t, slots[len(slots)-1].PredefinedNames, "value")
}

func TestServerNotRecommendedIndexReuseAndEdits(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `//xgols:notRecommended
var value string
func Use(value string) {}
func run() {
|
use "text"
}
`)
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	var builds int
	s.getProj().RegisterFileCacheBuilder(notRecommendedFileCacheKind{}, func(proj *xgo.Project, path string, file *xgo.File) (any, error) {
		builds++
		return buildNotRecommendedFileCache(proj, path, file)
	})
	for version, annotated := range []bool{true, false, true} {
		updated := source
		if !annotated {
			updated = strings.Replace(updated, "//xgols:notRecommended", "// Ordinary documentation.", 1)
		}
		s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: version + 1}})
		_, err := s.requestProject().TypeInfo()
		require.NoError(t, err)
		labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
		require.NoError(t, err)
		require.NotEmpty(t, slots)
		assert.Equal(t, version+1, builds, "completion and input slots should share the same index within a revision")
		if annotated {
			assert.NotContains(t, labels, "value")
			assert.NotContains(t, slots[len(slots)-1].PredefinedNames, "value")
		} else {
			assert.Contains(t, labels, "value")
			assert.Contains(t, slots[len(slots)-1].PredefinedNames, "value")
		}
	}
}

func TestCompletionItemSetNotRecommendedAfterTypeFiltering(t *testing.T) {
	items := newCompletionItemSet(PlainText)
	items.setExpectedTypes([]gotypes.Type{gotypes.Typ[gotypes.Int]})
	var checked []string
	items.notRecommended = func(def symbolDefinition) bool {
		checked = append(checked, def.Overview)
		return def.Overview == "marked"
	}
	items.addDefinitions(
		symbolDefinition{TypeHint: gotypes.Typ[gotypes.String], Overview: "incompatible", CompletionItemLabel: "unrelated"},
		symbolDefinition{TypeHint: gotypes.Typ[gotypes.Int], Overview: "marked", CompletionItemLabel: "shared"},
		symbolDefinition{TypeHint: gotypes.Typ[gotypes.Int], Overview: "unmarked", CompletionItemLabel: "shared"},
	)
	assert.Equal(t, []string{"marked", "unmarked"}, checked)
	assert.Equal(t, []string{"shared"}, completionItemLabels(items.items))
}
