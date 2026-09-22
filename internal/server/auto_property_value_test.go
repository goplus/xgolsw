package server

import (
	"slices"
	"strings"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerXGoGetInputSlotsAutoPropertyValues(t *testing.T) {
	for _, tt := range []struct{ name, methods, globals, expression, parameter string }{
		{name: "Ordinary", methods: `func (a *App) Label() string { return "" }`},
		{name: "Template", methods: `func XGot_App_Label(a *App) string { return "" }`},
		{name: "Inferred", methods: `func (a *App) Value() string { return "" }
func XGot_App_Label[T any](a interface{ Value() T }) T { return a.Value() }`},
		{name: "Overload", methods: `func (a *App) Label__0(value int) int { return value }
func (a *App) Label__1() string { return "" }`},
		{name: "PackageFunction", globals: `func Label() string { return "" }`},
		{name: "Variable", globals: `var label string`},
		{name: "MethodValue", methods: `func (a *App) Label() string { return "" }`, expression: "Label", parameter: "func() string"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			expression := tt.expression
			if expression == "" {
				expression = "label"
			}
			parameter := tt.parameter
			if parameter == "" {
				parameter = "any"
			}
			s := newImportTestServer(t, map[string][]byte{
				"main_fixture.gox": []byte("func run() { use " + expression + " }\n"),
				"globals.xgo":      []byte("func Use(value " + parameter + ") {}\n" + tt.globals + "\n"),
			})
			if tt.methods != "" {
				setImportTestFrameworkMethods(t, s, tt.methods)
			}
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}}})
			require.NoError(t, err)
			require.Len(t, slots, 1)
			wantType := XGoInputTypeString
			if tt.name == "MethodValue" {
				wantType = XGoInputTypeUnknown
			}
			assert.Equal(t, XGoInput{Kind: XGoInputKindPredefined, Type: wantType, Name: expression}, slots[0].Input)
			assert.Equal(t, XGoInputTypeUnknown, slots[0].Accept.Type)
		})
	}
}

func TestServerClassfilePromotedAutoProperties(t *testing.T) {
	for _, tt := range []struct{ name, methods, local string }{
		{name: "Ordinary", methods: `func (a *App) Label() string { return "" }`},
		{name: "Template", methods: `func XGot_App_Label(a *App) string { return "" }`},
		{name: "Inferred", methods: `func (a *App) Value() string { return "" }
func XGot_App_Label[T any](a interface{ Value() T }) T { return a.Value() }`},
		{name: "Overload", methods: `func (a *App) Label__0(value int) int { return value }
func (a *App) Label__1() string { return "" }`},
		{name: "LocalShadow", methods: `func (a *App) Label() string { return "" }`, local: "label := 1\n_ = label\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, selector := range []bool{false, true} {
				name, prefix, placeholder := "Bare", "", `"text"`
				if selector {
					name, prefix, placeholder = "Selector", "this.", "this.Label"
				}
				t.Run(name, func(t *testing.T) {
					source, pos := typeDisplayTestSource(t, "var Label []int\nfunc run() {\n"+tt.local+"use "+prefix+"|"+strings.TrimPrefix(placeholder, prefix)+"\n}\n")
					s := newImportTestServer(t, map[string][]byte{
						"main_fixture.gox": []byte(source),
						"globals.xgo":      []byte("func Use(value string) {}\n"),
					})
					setImportTestFrameworkMethods(t, s, tt.methods)
					properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "App"})
					require.NoError(t, err)
					assert.True(t, slices.ContainsFunc(properties, func(p XGoProperty) bool { return p.Name == "label" }))
					want := selector || tt.local == ""
					items := completionItemsAt(t, s, "main_fixture.gox", pos)
					assert.Equal(t, want, slices.Contains(completionItemLabels(items), "label"))
					if !selector {
						slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}}})
						require.NoError(t, err)
						require.NotEmpty(t, slots)
						assert.Equal(t, want, slices.Contains(slots[len(slots)-1].PredefinedNames, "label"))
					}
					s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(strings.Replace(source, placeholder, prefix+"label", 1)), Version: 1}})
					_, err = s.requestProject().TypeInfo()
					if want {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
				})
			}
		})
	}
}

func TestServerIncompleteAutoPropertyCandidates(t *testing.T) {
	for _, tt := range []struct{ name, declarations string }{
		{"Missing", "func Value = (missing)"},
		{"PartiallyResolved", "func number() int { return 0 }\nfunc Value = (number; missing)"},
		{"Nested", "func nested = (missing)\nfunc Value = (nested)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.declarations+"\nfunc run() { use |\"text\" }\n")
			s := newImportTestServer(t, map[string][]byte{
				"main_fixture.gox": []byte(source),
				"globals.xgo":      []byte("func Use(value string) {}\n"),
			})
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}}})
			require.NoError(t, err)
			require.NotEmpty(t, slots)
			assert.NotContains(t, slots[len(slots)-1].PredefinedNames, "value")
			labels := completionItemLabels(completionItemsAt(t, s, "main_fixture.gox", pos))
			assert.NotContains(t, labels, "value")
			for _, name := range []string{"XGoo_App_Value", "XGoo_App_nested"} {
				assert.NotContains(t, slots[len(slots)-1].PredefinedNames, name)
				assert.NotContains(t, labels, name)
			}
		})
	}
}

func TestTypeDisplayAutoPropertyQualifierShadowing(t *testing.T) {
	for _, tt := range []struct {
		name, methods string
		want          bool
	}{
		{name: "Ordinary", methods: `func (a *App) Label() string { return "" }`},
		{name: "Template", methods: `func XGot_App_Label(a *App) string { return "" }`},
		{name: "RequiredArgument", methods: `func (a *App) Label(value int) string { return "" }`, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "var (\nLabel []int\nvalue label.Name\n)\nfunc run() { _ = |value }\n")
			s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)}, &modfile.Import{Name: "label", Path: importTestPkgPath})
			setImportTestFrameworkMethods(t, s, tt.methods)
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			file, err := proj.ASTFile("main_fixture.gox")
			require.NoError(t, err)
			_, obj, _ := objectAtPosition(proj, info, file, ToPosition(proj, file, pos))
			require.NotNil(t, obj)
			display := newTypeDisplay(proj, file, PosAt(proj, file, pos))
			name, ok := display.sourceTypeString(obj.Type())
			assert.Equal(t, tt.want, ok)
			if tt.want {
				assert.Equal(t, "label.Name", name)
			} else {
				assert.Empty(t, name)
			}
			s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(source + "func convert() { _ = label.Name(\"text\") }\n"), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			if tt.want {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestServerCompletionPropertyFunctionResult(t *testing.T) {
	for _, tt := range []struct {
		name, filename, receiver string
		want                     bool
	}{
		{name: "Package", filename: "main.xgo", receiver: "current"},
		{name: "Class", filename: "main_fixture.gox", receiver: "current"},
		{name: "Selector", filename: "main_fixture.gox", receiver: "this.current"},
		{name: "Parenthesized", filename: "main_fixture.gox", receiver: "(current)"},
		{name: "MethodValue", filename: "main_fixture.gox", receiver: "Current"},
		{name: "GetterCall", filename: "main_fixture.gox", receiver: "Current()"},
		{name: "ResultCall", filename: "main_fixture.gox", receiver: "Current()()", want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "func Current() func() Result { return nil }\nfunc run() { _ = "+tt.receiver+".|Text }\n")
			s := newImportTestServer(t, map[string][]byte{tt.filename: []byte(source), "types.xgo": []byte("type Result struct { Text string }\n")})
			_, err := s.requestProject().TypeInfo()
			if tt.want {
				require.NoError(t, err)
			} else {
				require.Error(t, err, "a function value has no Text member")
			}
			assert.Equal(t, tt.want, slices.Contains(completionItemLabels(completionItemsAt(t, s, tt.filename, pos)), "Text"))
		})
	}
}

func TestServerCompletionPropertyMethodExpressions(t *testing.T) {
	for _, tt := range []struct{ name, receiver, method, alias, parameter string }{
		{"Generic", "f.Box[int]", "Value", "value", "func(f.Box[int]) int"},
		{"Parenthesized", "(f.Box[int])", "Value", "value", "func(f.Box[int]) int"},
		{"Pointer", "(*f.Box[int])", "PointerValue", "pointerValue", "func(*f.Box[int]) int"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "import f \"example.com/framework\"\nfunc use(value "+tt.parameter+") {}\nuse "+tt.receiver+".|"+tt.alias+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			setImportTestFrameworkMethods(t, s, `type Box[T any] struct { Item T }
func (b Box[T]) Value() T { return b.Item }
func (b *Box[T]) PointerValue() T { return b.Item }
`)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, "main.xgo", pos)
			assert.NotContains(t, completionItemLabels(items), tt.alias)
			item := completionItemByLabel(items, tt.method)
			require.NotNil(t, item)
			assert.Equal(t, tt.method, item.InsertText)
			updated := strings.Replace(source, tt.receiver+"."+tt.alias, tt.receiver+"."+item.InsertText, 1)
			s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
		})
	}
}

func TestServerCompletionPropertyAliases(t *testing.T) {
	for _, tt := range []struct {
		name, filename, declarations, receiver, local string
		callback, shadowed                            bool
	}{
		{name: "Direct", filename: "main.xgo", declarations: "type Record struct{}", receiver: "record."},
		{name: "Promoted", filename: "main.xgo", declarations: "type Base struct{}\ntype Record struct { Base; Label []int }", receiver: "record."},
		{name: "FieldShadow", filename: "main.xgo", declarations: "type Base struct{}\ntype Record struct { Base; label int }", receiver: "record.", shadowed: true},
		{name: "ClassBare", filename: "main_fixture.gox"},
		{name: "ClassSelector", filename: "main_fixture.gox", receiver: "this."},
		{name: "LocalShadow", filename: "main_fixture.gox", local: "label := 1\n_ = label\n", shadowed: true},
		{name: "Callback", filename: "main_fixture.gox", callback: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			methods := "func text() string { return \"\" }\nfunc Label = (text)\nfunc Title = (text)\n"
			if tt.declarations != "" {
				owner := "Record"
				if strings.Contains(tt.declarations, "Base") {
					owner = "Base"
				}
				methods = tt.declarations + "\nfunc (" + owner + ") text() string { return \"\" }\nfunc (" + owner + ").Label = ((" + owner + ").text)\nfunc (" + owner + ").Title = ((" + owner + ").text)\nvar record Record\n"
			}
			parameter, placeholder := "string", "title"
			if tt.callback {
				parameter, placeholder = "func() string", "text"
			}
			source, pos := typeDisplayTestSource(t, methods+"func run() {\n"+tt.local+"use "+tt.receiver+"|"+placeholder+"\n}\n")
			s := newImportTestServer(t, map[string][]byte{tt.filename: []byte(source), "globals.xgo": []byte("func Use(value " + parameter + ") {}\n")})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, tt.filename, pos)
			for i, name := range []string{"label", "title"} {
				if tt.callback || tt.shadowed && name == "label" {
					assert.NotContains(t, completionItemLabels(items), name)
					continue
				}
				assert.Equal(t, 1, countCompletionItemLabel(items, name))
				item := completionItemByLabel(items, name)
				require.NotNil(t, item)
				assert.Equal(t, name, item.InsertText)
				updated := strings.Replace(source, "use "+tt.receiver+placeholder, "use "+tt.receiver+item.InsertText, 1)
				s.ModifyFiles([]FileChange{{Path: tt.filename, Content: []byte(updated), Version: i + 1}})
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
			}
			if tt.callback {
				assert.Equal(t, 1, countCompletionItemLabel(items, "text"))
			}
		})
	}
}

func TestServerCompletionPackagePropertyAliasResult(t *testing.T) {
	for _, tt := range []struct{ name, declarations, receiver string }{
		{"Ordinary", "func Current() Result { return Result{} }", "current"},
		{"Overload", "func makeResult() Result { return Result{} }\nfunc Current = (makeResult)", "current"},
		{"VariadicFirst", "func makeResult(values ...int) Result { return Result{} }\nfunc fallback() int { return 0 }\nfunc Current = (makeResult; fallback)", "current"},
		{"OptionalFirst", "func makeResult(value int?) Result { return Result{} }\nfunc fallback() int { return 0 }\nfunc Current = (makeResult; fallback)", "current"},
		{"Parenthesized", "func makeResult() Result { return Result{} }\nfunc Current = (makeResult)", "(current)"},
		{"MethodChain", "func makeResult() Result { return Result{} }\nfunc Current = (makeResult)", "current.next"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.declarations+"\nfunc run() { use "+tt.receiver+".|Text }\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source), "types.xgo": []byte("type Result struct { Text string; Count int }\nfunc (Result) Next() Result { return Result{} }\nfunc Use(value string) {}\n")})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, "main.xgo", pos)
			item := completionItemByLabel(items, "Text")
			require.NotNil(t, item)
			assert.Equal(t, "Text", item.InsertText)
			assert.NotContains(t, completionItemLabels(items), "Count")
		})
	}
}

func TestServerXGoGetInputSlotsPackagePropertyAlias(t *testing.T) {
	for _, tt := range []struct {
		name, expression string
		want             XGoInputType
	}{
		{"Property", "current", XGoInputTypeString},
		{"Parenthesized", "(current)", XGoInputTypeString},
		{"ImplementationValue", "makeText", XGoInputTypeUnknown},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte("func makeText() (text string) { return }\nfunc Current = (makeText)\nfunc Use(value any) {}\nuse " + tt.expression + "\n")})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
			require.NoError(t, err)
			require.Len(t, slots, 1)
			assert.Equal(t, tt.want, slots[0].Input.Type)
		})
	}
}

func TestServerCompletionImportedPropertyAliasResult(t *testing.T) {
	for _, tt := range []struct{ name, filename, imports, receiver string }{
		{"Named", "main.xgo", "import f \"example.com/framework\"\n", "f.current"},
		{"Dot", "main.xgo", "import . \"example.com/framework\"\n", "current"},
		{"Classfile", "main_fixture.gox", "", "current"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.imports+"func run() { _ = "+tt.receiver+".|Text }\n")
			s := newImportTestServer(t, map[string][]byte{tt.filename: []byte(source)})
			setImportTestFrameworkMethods(t, s, "type Result struct { Text string }\nconst XGoo_Current = \"MakeResult\"\nfunc MakeResult() Result { return Result{} }\n")
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, tt.filename, pos)
			item := completionItemByLabel(items, "Text")
			require.NotNil(t, item)
			assert.Equal(t, "Text", item.InsertText)
		})
	}
}

func TestServerCompletionHiddenPropertyImplementation(t *testing.T) {
	source, pos := typeDisplayTestSource(t, `type Base struct{}
func (Base) Label() string { return "" }
type Record struct { Base; Label []int }
func (Base).Title = ((Base).Label)
var record Record
func Use(value string) {}
use record.|title
`)
	s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	items := completionItemsAt(t, s, "main.xgo", pos)
	assert.NotContains(t, completionItemLabels(items), "Label")
	for i, name := range []string{"label", "title"} {
		item := completionItemByLabel(items, name)
		require.NotNil(t, item)
		assert.Equal(t, 1, countCompletionItemLabel(items, name))
		updated := strings.Replace(source, "record.title", "record."+item.InsertText, 1)
		s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: i + 1}})
		_, err := s.requestProject().TypeInfo()
		require.NoError(t, err)
	}
}

func TestServerCompletionPackagePropertyAliases(t *testing.T) {
	for _, tt := range []struct{ name, filename, imports, prefix string }{
		{"Local", "main.xgo", "", ""},
		{"NamedImport", "main.xgo", "import f \"example.com/framework\"\n", "f."},
		{"DotImport", "main.xgo", "import . \"example.com/framework\"\n", ""},
		{"ClassfileImport", "main_fixture.gox", "", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			declarations := ""
			if tt.name == "Local" {
				declarations = "func makeText() string { return \"\" }\nfunc Label = (makeText)\nfunc Title = (makeText)\n"
			}
			source, pos := typeDisplayTestSource(t, tt.imports+declarations+"func run() { use "+tt.prefix+"|title }\n")
			s := newImportTestServer(t, map[string][]byte{tt.filename: []byte(source), "types.xgo": []byte("func Use(value string) {}\n")})
			if tt.name != "Local" {
				setImportTestFrameworkMethods(t, s, "const XGoo_Label = \"MakeText\"\nconst XGoo_Title = \"MakeText\"\nfunc MakeText() string { return \"\" }\n")
			}
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			items := completionItemsAt(t, s, tt.filename, pos)
			for i, name := range []string{"label", "title"} {
				assert.Equal(t, 1, countCompletionItemLabel(items, name))
				item := completionItemByLabel(items, name)
				require.NotNil(t, item)
				assert.Equal(t, name, item.InsertText)
				s.ModifyFiles([]FileChange{{Path: tt.filename, Content: []byte(strings.Replace(source, tt.prefix+"title", tt.prefix+item.InsertText, 1)), Version: i + 1}})
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
			}
		})
	}
}

func TestServerCompletionPackagePropertyOverloads(t *testing.T) {
	for _, tt := range []struct {
		name, declarations, parameter, expression string
		want                                      bool
	}{
		{name: "Selected", declarations: "func first() string { return \"first\" }\nfunc second() string { return \"second\" }\nfunc Label = (first; second)", want: true},
		{name: "VariadicFirst", declarations: "func first(values ...int) string { return \"\" }\nfunc second() int { return 0 }\nfunc Label = (first; second)", want: true},
		{name: "OptionalFirst", declarations: "func first(value int?) string { return \"\" }\nfunc second() int { return 0 }\nfunc Label = (first; second)", want: true},
		{name: "IncompatibleResult", declarations: "func first() []int { return nil }\nfunc second() string { return \"\" }\nfunc Label = (first; second)"},
		{name: "Uninferred", declarations: "func first[T any]() T { var value T; return value }\nfunc Label = (first)"},
		{name: "CallbackResult", declarations: "func first() func() string { return nil }\nfunc Label = (first)", parameter: "func() string", want: true},
		{name: "CallbackImplementation", declarations: "func first() string { return \"\" }\nfunc Label = (first)", parameter: "func() string", expression: "first"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parameter, expression := tt.parameter, tt.expression
			if parameter == "" {
				parameter = "string"
			}
			if expression == "" {
				expression = "label"
			}
			source, pos := typeDisplayTestSource(t, tt.declarations+"\nfunc run() { use |"+expression+" }\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source), "types.xgo": []byte("func Use(value " + parameter + ") {}\n")})
			_, err := s.requestProject().TypeInfo()
			if tt.want || tt.expression != "" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			items := completionItemsAt(t, s, "main.xgo", pos)
			if tt.want {
				assert.Equal(t, 1, countCompletionItemLabel(items, "label"))
			} else {
				assert.NotContains(t, completionItemLabels(items), "label")
			}
			if tt.expression != "" {
				assert.Contains(t, completionItemLabels(items), expression)
			}
		})
	}
}

func TestServerCompletionPackagePropertyShadowing(t *testing.T) {
	for _, tt := range []struct {
		name, declarations, local string
		want                      bool
	}{
		{name: "LowercaseLocal", local: "label := 1; _ = label"},
		{name: "LowercasePackage", declarations: "var label int"},
		{name: "UppercaseLocal", local: "Label := 1; _ = Label", want: true},
		{name: "ImplementationLocal", local: "text := 1; _ = text", want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "func text() string { return \"\" }\nfunc Label = (text)\n"+tt.declarations+"\nfunc run() {\n"+tt.local+"\nuse |label\n}\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source), "types.xgo": []byte("func Use(value string) {}\n")})
			_, err := s.requestProject().TypeInfo()
			if tt.want {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			assert.Equal(t, tt.want, slices.Contains(completionItemLabels(completionItemsAt(t, s, "main.xgo", pos)), "label"))
		})
	}
}

func TestServerCompletionIncompletePackagePropertyCandidates(t *testing.T) {
	for _, tt := range []struct {
		name, declarations string
		want               bool
	}{
		{name: "Missing", declarations: "func Label = (missing)"},
		{name: "SelectedBeforeMissing", declarations: "func first() string { return \"\" }\nfunc Label = (first; missing)", want: true},
		{name: "Nested", declarations: "func nested = (missing)\nfunc Label = (nested)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.declarations+"\nfunc run() { use |\"text\" }\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source), "types.xgo": []byte("func Use(value string) {}\n")})
			items := completionItemsAt(t, s, "main.xgo", pos)
			assert.Equal(t, tt.want, slices.Contains(completionItemLabels(items), "label"))
			if tt.want {
				s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(strings.Replace(source, "use \"text\"", "use label", 1)), Version: 1}})
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
			}
		})
	}
}

func TestServerCompletionHiddenPropertyCallback(t *testing.T) {
	for _, tt := range []struct{ name, declarations, expression string }{
		{
			name: "PromotedMethod",
			declarations: `type Base struct{}
func (Base) Label() string { return "" }
func (Base).Title = ((Base).Label)
type Record struct { Base; Label []int }
var record Record`,
			expression: "record.|Label",
		},
		{
			name:         "LocalImplementation",
			declarations: "func Label() string { return \"\" }\nfunc Title = (Label)",
			expression:   "|Label",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			local := ""
			if tt.name == "LocalImplementation" {
				local = "Label := []int{}\n_ = Label\n"
			}
			source, pos := typeDisplayTestSource(t, tt.declarations+"\nfunc run() {\n"+local+"use "+tt.expression+"\n}\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source), "types.xgo": []byte("func Use(value func() string) {}\n")})
			_, err := s.requestProject().TypeInfo()
			require.Error(t, err)
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.NotContains(t, labels, "Label")
			assert.NotContains(t, labels, "label")
			assert.NotContains(t, labels, "title")
		})
	}
}

func TestServerCompletionImportedPropertyCallback(t *testing.T) {
	for _, tt := range []struct {
		name, implementation string
		want                 bool
	}{
		{"Exported", "MakeText", true},
		{"Unexported", "makeText", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "import f \"example.com/framework\"\nfunc run() { use f.|MakeText }\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source), "types.xgo": []byte("func Use(value func() string) {}\n")})
			setImportTestFrameworkMethods(t, s, "const XGoo_Label = \""+tt.implementation+"\"\nfunc "+tt.implementation+"() string { return \"\" }\n")
			items := completionItemsAt(t, s, "main.xgo", pos)
			assert.Equal(t, tt.want, slices.Contains(completionItemLabels(items), tt.implementation))
			assert.NotContains(t, completionItemLabels(items), "label")
			if tt.want {
				item := completionItemByLabel(items, tt.implementation)
				require.NotNil(t, item)
				s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(strings.Replace(source, "f.MakeText", "f."+item.InsertText, 1)), Version: 1}})
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
			}
		})
	}
}
