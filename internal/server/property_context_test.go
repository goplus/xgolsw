package server

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerXGoGetInputSlotsPackageAliases(t *testing.T) {
	for _, tt := range []struct{ name, filename, imports, declarations string }{
		{"Local", "main.xgo", "", "func Label() string { return \"\" }\n"},
		{"Overload", "main.xgo", "", "func text() string { return \"\" }\nfunc Label = (text)\n"},
		{"DotImport", "main.xgo", "import . \"example.com/framework\"\n", ""},
		{"ClassfileImport", "main_fixture.gox", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newImportTestServer(t, map[string][]byte{tt.filename: []byte(tt.imports + tt.declarations + "func run() { use label }\n"), "types.xgo": []byte("func Use(value string) {}\n")})
			if tt.declarations == "" {
				setImportTestFrameworkMethods(t, s, "func Label() string { return \"\" }\n")
			}
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}}})
			require.NoError(t, err)
			require.NotEmpty(t, slots)
			assert.Equal(t, XGoInputTypeString, slots[len(slots)-1].Input.Type)
			assert.Contains(t, slots[len(slots)-1].PredefinedNames, "label")
		})
	}
}

func TestServerCompletionAutoPropertyValueContexts(t *testing.T) {
	for _, tt := range []struct{ name, declarations, body string }{
		{"Binary", "func Label() string { return \"\" }", "_ = label == |good"},
		{"Switch", "func Label() string { return \"\" }", "switch label { case |good: }"},
		{"Index", "func Lookup() map[string]int { return nil }", "_ = lookup[|good]"},
		{"Send", "func Stream() chan string { return nil }", "stream <- |good"},
		{"MemberBinary", "type Record struct{}\nfunc (Record) Label() string { return \"\" }\nvar record Record", "_ = record.label == |good"},
		{"MemberIndex", "type Record struct{}\nfunc (Record) Lookup() map[string]int { return nil }\nvar record Record", "_ = record.lookup[|good]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.declarations+"\nvar good string\nvar bad []int\nfunc run() { "+tt.body+" }\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, "main.xgo", pos)
			labels := completionItemLabels(items)
			assert.Contains(t, labels, "good")
			assert.NotContains(t, labels, "bad")
		})
	}
}

func TestServerXGoGetInputSlotsFunctionVariableAlias(t *testing.T) {
	s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte("var Label = func() string { return \"\" }\nfunc Use(value string) {}\nuse label\n")})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
	require.NoError(t, err)
	require.NotEmpty(t, slots)
	assert.Equal(t, XGoInputTypeString, slots[len(slots)-1].Input.Type)
	assert.Contains(t, slots[len(slots)-1].PredefinedNames, "label")
}

func TestServerXGoGetInputSlotsWritableNames(t *testing.T) {
	s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte("var count int\nconst Limit = 1\nfunc Value() int { return 1 }\nfunc run() { count = 2 }\n")})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}}})
	require.NoError(t, err)
	found := false
	for _, slot := range slots {
		if slot.Kind != XGoInputSlotKindAddress {
			continue
		}
		found = true
		assert.Contains(t, slot.PredefinedNames, "count")
		assert.NotContains(t, slot.PredefinedNames, "value")
		assert.NotContains(t, slot.PredefinedNames, "Limit")
	}
	require.True(t, found)
}

func TestServerTextDocumentRenameFunctionVariableAlias(t *testing.T) {
	for _, tt := range []struct{ name, selection, newName, declaration, alias string }{
		{"Declaration", "var La|bel", "Title", "Title", "title"},
		{"Read", "use la|bel", "title", "Title", "title"},
		{"Call", "use la|bel()", "title", "Title", "title"},
		{"Callback", "keep La|bel", "Title", "Title", "title"},
		{"Lowercase", "var La|bel", "title", "title", "title()"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := `var Label = func() string { return "" }
func Use(value string) {}
func Keep(callback func() string) {}
use label
use label()
use Label()
keep Label
Label = func() string { return "next" }
`
			source, pos := typeDisplayTestSource(t, strings.Replace(source, strings.ReplaceAll(tt.selection, "|", ""), tt.selection, 1))
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: pos, NewName: tt.newName})
			require.NoError(t, err)
			require.NotNil(t, edit)
			updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
			assert.Contains(t, updated, "var "+tt.declaration+" =")
			assert.Contains(t, updated, "use "+tt.alias+"\n")
			assert.Contains(t, updated, "use "+strings.TrimSuffix(tt.alias, "()")+"()\n")
			assert.Contains(t, updated, "keep "+tt.declaration+"\n")
			assert.Contains(t, updated, tt.declaration+` = func() string { return "next" }`)
			s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
		})
	}
}

func TestServerCompletionFunctionVariableAlias(t *testing.T) {
	for _, tt := range []struct{ name, filename, imports, prefix, parameter, expression string }{
		{"LocalValue", "main.xgo", "", "", "string", "label"},
		{"LocalCallback", "main.xgo", "", "", "func() string", "Label"},
		{"NamedValue", "main.xgo", "import f \"example.com/framework\"\n", "f.", "string", "label"},
		{"NamedCallback", "main.xgo", "import f \"example.com/framework\"\n", "f.", "func() string", "Label"},
		{"DotValue", "main.xgo", "import . \"example.com/framework\"\n", "", "string", "label"},
		{"ClassfileValue", "main_fixture.gox", "", "", "string", "label"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			declarations := ""
			local := tt.imports == "" && tt.filename == "main.xgo"
			if local {
				declarations = "var Label = func() string { return \"\" }\n"
			}
			source, pos := typeDisplayTestSource(t, tt.imports+declarations+"func run() { use "+tt.prefix+"|"+tt.expression+" }\n")
			s := newImportTestServer(t, map[string][]byte{tt.filename: []byte(source), "types.xgo": []byte("func Use(value " + tt.parameter + ") {}\n")})
			if !local {
				setImportTestFrameworkMethods(t, s, "var Label = func() string { return \"\" }\n")
			}
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, tt.filename, pos)
			item := completionItemByLabel(items, tt.expression)
			require.NotNil(t, item)
			assert.Equal(t, 1, countCompletionItemLabel(items, tt.expression))
			assert.Equal(t, tt.expression, item.InsertText)
			if tt.expression == "Label" {
				assert.NotContains(t, completionItemLabels(items), "label")
			} else {
				assert.NotContains(t, completionItemLabels(items), "Label")
			}
		})
	}
}

func TestCollectPredefinedNamesPropertyContexts(t *testing.T) {
	for _, first := range []XGoInputSlotKind{XGoInputSlotKindValue, XGoInputSlotKindAddress} {
		name := "ValueFirst"
		if first == XGoInputSlotKindAddress {
			name = "AddressFirst"
		}
		t.Run(name, func(t *testing.T) {
			s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte("var count int\nconst Limit = 1\nfunc Value() int { return 1 }\nfunc run() { use 2 }\n"), "types.xgo": []byte("func Use(value any) {}\n")})
			ctx := inputSlotTestContext(t, s, "main_fixture.gox")
			expr := inputSlotLiteral(t, ctx, "2")
			for _, kind := range []XGoInputSlotKind{first, XGoInputSlotKindValue, XGoInputSlotKindAddress, first} {
				names := collectPredefinedNames(ctx, kind, expr, nil)
				assert.Contains(t, names, "count")
				if kind == XGoInputSlotKindValue {
					assert.Contains(t, names, "value")
					assert.Contains(t, names, "Limit")
				} else {
					assert.NotContains(t, names, "value")
					assert.NotContains(t, names, "Limit")
				}
			}
		})
	}
}

func TestServerCompletionAutoPropertyEnums(t *testing.T) {
	for _, body := range []struct{ name, source string }{
		{"Comparison", "_ = current == |Ready"},
		{"Switch", "switch current { case |Ready: }"},
		{"Index", "_ = lookup[|Ready]"},
		{"Send", "stream <- |Ready"},
	} {
		t.Run(body.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `type State const (
Ready = iota
Busy
)
type Other const (
Wrong = iota
)
func Current() State { return Ready }
func Lookup() map[State]int { return nil }
func Stream() chan State { return nil }
func run() { `+body.source+" }\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Contains(t, labels, "Ready")
			assert.Contains(t, labels, "Busy")
			assert.NotContains(t, labels, "Wrong")
		})
	}
}

func TestServerPropertyFunctionVariableShadowing(t *testing.T) {
	for _, tt := range []struct {
		name, declarations, local string
		want                      bool
	}{
		{name: "Unshadowed", want: true},
		{name: "UppercaseLocal", local: "Label := 1\n_ = Label", want: true},
		{name: "LowercaseLocal", local: "label := 1\n_ = label"},
		{name: "LowercasePackage", declarations: "var label int"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "var Label = func() string { return \"\" }\n"+tt.declarations+"\nfunc run() {\n"+tt.local+"\nuse |\"text\"\n}\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source), "types.xgo": []byte("func Use(value string) {}\n")})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Equal(t, tt.want, slices.Contains(labels, "label"))
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
			require.NoError(t, err)
			require.NotEmpty(t, slots)
			assert.Equal(t, tt.want, slices.Contains(slots[len(slots)-1].PredefinedNames, "label"))
			if tt.want {
				s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(strings.Replace(source, `use "text"`, "use label", 1)), Version: 1}})
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
			}
		})
	}
}

func TestServerXGoGetInputSlotsPackageAliasCandidates(t *testing.T) {
	for _, tt := range []struct {
		name, declarations, local string
		want                      bool
	}{
		{name: "NoArguments", declarations: `func Label() string { return "" }`, want: true},
		{name: "FunctionVariable", declarations: `var Label = func() string { return "" }`, want: true},
		{name: "RequiredArgument", declarations: `func Label(value int) string { return "" }`},
		{name: "VariableArgument", declarations: `var Label = func(value int) string { return "" }`},
		{name: "MultipleResults", declarations: `func Label() (string, int) { return "", 0 }`},
		{name: "NoResult", declarations: `func Label() {}`},
		{name: "LocalVariable", local: `Label := func() string { return "" }; _ = Label`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := tt.declarations + "\nfunc run() {\n" + tt.local + "\nuse \"text\"\n}\n"
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source), "types.xgo": []byte("func Use(value string) {}\n")})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
			require.NoError(t, err)
			require.NotEmpty(t, slots)
			assert.Equal(t, tt.want, slices.Contains(slots[len(slots)-1].PredefinedNames, "label"))
			if tt.want {
				s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(strings.Replace(source, `use "text"`, "use label", 1)), Version: 1}})
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
			}
		})
	}
}

func TestServerCompletionFunctionVariableCalls(t *testing.T) {
	for _, tt := range []struct{ name, declarations, expression, parameter, candidate string }{
		{"ExplicitCall", `var Label = func() string { return "" }`, "La|bel()", "string", "Label"},
		{"ExplicitCallbackCall", `var Label = func() func() string { return nil }`, "La|bel()", "func() string", "Label"},
		{"ImplicitCallbackCall", `var Label = func() func() string { return nil }`, "|label", "func() string", "label"},
		{"FunctionField", "type Record struct { Label func() string }\nvar record Record", "record.|Label", "func() string", "Label"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.declarations+"\nfunc run() { use "+tt.expression+" }\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source), "types.xgo": []byte("func Use(value " + tt.parameter + ") {}\n")})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, "main.xgo", pos)
			item := completionItemByLabel(items, tt.candidate)
			require.NotNil(t, item)
			assert.Equal(t, tt.candidate, item.InsertText)
		})
	}
}
