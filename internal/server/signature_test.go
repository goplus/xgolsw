package server

import (
	"testing"

	"github.com/goplus/xgolsw/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentSignatureHelp(t *testing.T) {
	t.Run("TupleArguments", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			declarations string
			call         string
			label        string
		}{
			{"BetweenArguments", "func use(first int, second string) {}\n", "use((1,| \"value\"))", "use(first int, second string)"},
			{"FunctionValue", "var use = func(first int, second string) {}\n", "use((1, \"val|ue\"))", "use(first int, second string)"},
			{"UnresolvedOverload", `func ints(first int, second int) {}
func strings(first string, second string) {}
func use = (ints, strings)
`, "use((1, mis|sing))", "use(first int, second int)"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, position := typeDisplayTestSource(t, tt.call+"\n")
				s := newTestServer(t, map[string][]byte{"functions.xgo": []byte(tt.declarations), "main.xgo": []byte(source)})
				help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position}})
				require.NoError(t, err)
				require.NotNil(t, help)
				require.Len(t, help.Signatures, 1)
				assert.Equal(t, tt.label, help.Signatures[0].Label)
				assert.Equal(t, uint32(1), help.ActiveParameter)
			})
		}
	})

	t.Run("FunctionOverloadUpdates", func(t *testing.T) {
		const declarations = `func handleInt(value int) {}
func handleString(value string) {}
func handle = (
    handleInt
    handleString
)
`
		s := newTestServer(t, map[string][]byte{"functions.xgo": []byte(declarations), "main.xgo": nil})
		for i, tt := range []struct {
			name         string
			argument     string
			declarations string
			parameters   []string
			wantError    bool
		}{
			{name: "Integer", argument: "1", parameters: []string{"value int"}},
			{name: "String", argument: `"text"`, parameters: []string{"value string"}},
			{name: "Unresolved", argument: "missing", parameters: []string{"value int", "value string"}, wantError: true},
			{
				name: "UpdatedDeclaration", argument: "true", parameters: []string{"flag bool"},
				declarations: `func handleFlag(flag bool) {}
func handle = (
    handleFlag
)
`,
			},
		} {
			changes := []FileChange{{Path: "main.xgo", Content: []byte("handle " + tt.argument + "\n"), Version: i + 1}}
			if tt.declarations != "" {
				changes = append(changes, FileChange{Path: "functions.xgo", Content: []byte(tt.declarations), Version: 1})
			}
			s.ModifyFiles(changes)
			_, err := s.getProj().TypeInfo()
			if tt.wantError {
				require.ErrorContains(t, err, "undefined: missing")
			} else {
				require.NoError(t, err)
			}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: Position{Character: 8},
			}})
			require.NoError(t, err)
			var signatures []SignatureInformation
			for _, parameter := range tt.parameters {
				signatures = append(signatures, SignatureInformation{
					Label: "handle(" + parameter + ")", Parameters: []ParameterInformation{{Label: parameter}},
				})
			}
			assert.Equal(t, &SignatureHelp{Signatures: signatures}, help, tt.name)
		}
	})

	t.Run("OverloadIdentifiers", func(t *testing.T) {
		const declarations = `func ints(value int) int { return value }
func texts(value string) string { return value }
`
		for _, tt := range []struct {
			name, source, wantError string
			want                    []string
		}{
			{"Declaration", declarations + "func ch|oose = (ints; texts)\n", "", []string{"choose(value int) int", "choose(value string) string"}},
			{"Reference", declarations + "func choose = (ints; texts)\nch|oose\n", "not enough arguments", []string{"choose(value int) int", "choose(value string) string"}},
			{"ResolvedReference", declarations + "func zero() int { return 0 }\nfunc choose = (zero; ints)\nch|oose\n", "", []string{"choose() int"}},
			{"PartiallyResolved", declarations + "func choose = (ints; missing)\nch|oose\n", "not enough arguments", []string{"choose(value int) int"}},
			{"Unresolved", "func choose = (missing; absent)\nch|oose\n", "undefined: choose", nil},
			{"FunctionVariableDeclaration", "var us|e func(value int) int\n", "", []string{"use(value int) int"}},
			{"IncompleteFunctionVariable", "var use func(value int) int = missing\n_ = us|e\n", "undefined: missing", []string{"use(value int) int"}},
			{"ImplicitFunctionVariable", "var Get = func() int { return 1 }\n_ = ge|t\n", "", []string{"get() int"}},
			{"ImplicitFactoryVariable", "var Get = func() func(int) string { return nil }\n_ = ge|t\n", "", []string{"get() func(int) string"}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, pos := typeDisplayTestSource(t, tt.source)
				s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				t.Cleanup(s.Close)
				_, err := s.requestProject().TypeInfo()
				if tt.wantError != "" {
					require.ErrorContains(t, err, tt.wantError)
				} else {
					require.NoError(t, err)
				}
				help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: pos,
				}})
				require.NoError(t, err)
				if tt.want == nil {
					assert.Nil(t, help)
					return
				}
				require.NotNil(t, help)
				var labels []string
				for _, signature := range help.Signatures {
					labels = append(labels, signature.Label)
				}
				assert.Equal(t, tt.want, labels)
				assert.Zero(t, help.ActiveParameter)
			})
		}
	})

	t.Run("ImportedOverloadIdentifiers", func(t *testing.T) {
		const functions = `func Choose__0(value int) int { return value }
func Choose__1(value string) string { return value }
`
		const methods = `func (Item) Choose__0(value int) int { return value }
func (Item) Choose__1(value string) string { return value }
`
		const pointerMethods = `func (*Item) Choose__0(value int) int { return value }
func (*Item) Choose__1(value string) string { return value }
`
		for _, tt := range []struct {
			name, declarations, source, wantError string
			want                                  []string
		}{
			{"Function", functions, "f.ch|oose", "not enough arguments", []string{"choose(value int) int", "choose(value string) string"}},
			{"FunctionReference", functions, "_ = f.Ch|oose", "", []string{"Choose(value int) int", "Choose(value string) string"}},
			{"Parenthesized", functions, "_ = (f.Ch|oose)", "", []string{"Choose(value int) int", "Choose(value string) string"}},
			{"ResolvedCall", functions, "f.ch|oose(1)", "", []string{"choose(value int) int"}},
			{"FunctionVariable", "var Choose = func(value int) int { return value }\n", "_ = f.Ch|oose", "", []string{"Choose(value int) int"}},
			{"ImplicitFunctionVariable", "var Choose = func() int { return 1 }\n", "_ = f.ch|oose", "", []string{"choose() int"}},
			{"AliasedFunctionVariable", "type Callback = func(value int) int\nvar Choose Callback\n", "_ = f.Ch|oose", "", []string{"Choose(value int) int"}},
			{"FunctionField", "type Callback struct { Choose func(value int) int }\n", "var value f.Callback\n_ = value.Ch|oose", "", []string{"Choose(value int) int"}},
			{"Method", methods, "var item f.Item\nitem.ch|oose", "not enough arguments", []string{"choose(value int) int", "choose(value string) string"}},
			{"PointerMethod", pointerMethods, "var item f.Item\nitem.ch|oose", "not enough arguments", []string{"choose(value int) int", "choose(value string) string"}},
			{"MethodExpression", methods, "_ = f.Item.ch|oose", "", []string{"choose(f.Item, value int) int", "choose(f.Item, value string) string"}},
			{"PlainMethodExpression", "func (Item) Choose(value int) int { return value }\n", "_ = f.Item.ch|oose", "", []string{"choose(f.Item, value int) int"}},
			{"PointerMethodExpression", pointerMethods, "_ = (*f.Item).ch|oose", "", []string{"choose(*f.Item, value int) int", "choose(*f.Item, value string) string"}},
			{"PromotedMethodExpression", methods + "type Record struct { Item }\n", "_ = f.Record.ch|oose", "", []string{"choose(f.Record, value int) int", "choose(f.Record, value string) string"}},
			{"TemplateMethod", `func XGot_Item_Choose__0(item *Item, value int) int { return value }
func XGot_Item_Choose__1(item *Item, value string) string { return value }
`, "var item f.Item\nitem.ch|oose", "not enough arguments", []string{"choose(value int) int", "choose(value string) string"}},
			{"TemplateZeroArgument", `func XGot_Item_Choose__0(item *Item, value int) int { return value }
func XGot_Item_Choose__1(item *Item) int { return 0 }
`, "var item f.Item\nitem.ch|oose", "", []string{"choose() int"}},
			{"StaticMethod", `func XGos_Item_Choose__0(value int) int { return value }
func XGos_Item_Choose__1(value string) string { return value }
`, "_ = f.Item.ch|oose", "", []string{"choose(value int) int", "choose(value string) string"}},
			{"StaticFunction", "func XGos_Item_Choose(value int) int { return value }\n", "_ = f.Item.ch|oose", "", []string{"choose(value int) int"}},
			{"StaticValue", "var XGos_Item_Choose = 1\n", "_ = f.Item.Ch|oose", "", nil},
			{"AutoProperty", `func Choose__0() int { return 0 }
func Choose__1(value int) int { return value }
`, "_ = f.ch|oose", "", []string{"choose() int"}},
			{"StaticZeroArgument", `func XGos_Item_Choose__0(value int) int { return value }
func XGos_Item_Choose__1() int { return 0 }
`, "f.Item.ch|oose", "", []string{"choose() int"}},
			{"UnicodePrefix", functions, "/* \U0001F600 */ f.ch|oose", "not enough arguments", []string{"choose(value int) int", "choose(value string) string"}},
			{"TypeParameters", `func XGox_Choose__0[T any](value T) T { return value }
func XGox_Choose__1[T any](value T, label string) T { return value }
`, "_ = f.Ch|oose", "", []string{"Choose(T Type, value T) T", "Choose(T Type, value T, label string) T"}},
			{"ExplicitSparseGroup", `func Choose__0(value int) int { return value }
func Choose__2(value string) string { return value }
const XGoo_Choose = "Choose__0,Choose__2"
`, "_ = f.Ch|oose", "", []string{"Choose(value int) int", "Choose(value string) string"}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, pos := typeDisplayTestSource(t, "import f \"example.com/framework\"\n"+tt.source+"\n")
				s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				t.Cleanup(s.Close)
				setImportTestFrameworkMethods(t, s, tt.declarations)
				_, err := s.requestProject().TypeInfo()
				if tt.wantError != "" {
					require.ErrorContains(t, err, tt.wantError)
				} else {
					require.NoError(t, err)
				}
				help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: pos,
				}})
				require.NoError(t, err)
				if tt.want == nil {
					assert.Nil(t, help)
					return
				}
				require.NotNil(t, help)
				var labels []string
				for _, signature := range help.Signatures {
					labels = append(labels, signature.Label)
				}
				assert.Equal(t, tt.want, labels)
				assert.Zero(t, help.ActiveParameter)
			})
		}
	})

	t.Run("FrameworkOverloadIdentifier", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, `onStart => {
    mea|sure
}
`)
		s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
		t.Cleanup(s.Close)
		_, err := s.requestProject().TypeInfo()
		require.ErrorContains(t, err, "not enough arguments")
		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main_fixture.gox")}, Position: pos,
		}})
		require.NoError(t, err)
		require.NotNil(t, help)
		assert.Equal(t, []SignatureInformation{
			{
				Label:         "measure(value int) int",
				Documentation: &protocol.Or_SignatureInformation_documentation{Value: "Measure__0 is the integer overload of Measure."},
				Parameters:    []ParameterInformation{{Label: "value int"}},
			},
			{
				Label:         "measure(value string) int",
				Documentation: &protocol.Or_SignatureInformation_documentation{Value: "Measure__1 is the string overload of Measure."},
				Parameters:    []ParameterInformation{{Label: "value string"}},
			},
		}, help.Signatures)
	})

	for _, tt := range []struct {
		name     string
		filename string
	}{
		{name: "XGo", filename: "main.xgo"},
		{name: "Gop", filename: "main.gop"},
		{name: "NormalClass", filename: "Record.gox"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"functions.xgo": []byte("func combine(count int, label string) {}\n"),
				tt.filename:     []byte("combine 1, \"value\"\n"),
			})
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
					Position:     Position{Line: 0, Character: 13},
				},
			})
			require.NoError(t, err)
			assert.Equal(t, &SignatureHelp{
				Signatures: []SignatureInformation{{
					Label:      "combine(count int, label string)",
					Parameters: []ParameterInformation{{Label: "count int"}, {Label: "label string"}},
				}},
				ActiveParameter: 1,
			}, help)
			_, err = s.workspaceRootFS.TypeInfo()
			assert.NoError(t, err)
		})
	}

	t.Run("FrameworkMethods", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			source   string
			position Position
			label    string
			doc      string
		}{
			{
				name: "ProjectOverload", filename: "main_fixture.gox",
				source: `onStart => {
    measure 1
}
`, position: Position{Line: 1, Character: 12},
				label: "measure(value int) int", doc: "Measure__0 is the integer overload of Measure.",
			},
			{
				name: "WorkMethod", filename: "Worker_fixture.gox",
				source: `onValue amount => {
    apply amount
}
`, position: Position{Line: 1, Character: 12},
				label: "apply(value int)", doc: "Apply accepts a value on a work instance.",
			},
			{
				name: "BoundWorkMethod", filename: "main_fixture.gox",
				source: `onStart => {
    Worker.apply 1
}
`, position: Position{Line: 1, Character: 17},
				label: "apply(value int)", doc: "Apply accepts a value on a work instance.",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"main_fixture.gox":   {},
					"Worker_fixture.gox": {},
				}
				files[tt.filename] = []byte(tt.source)
				s := newFrameworkTestServer(t, files)
				help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
						Position:     tt.position,
					},
				})
				require.NoError(t, err)
				assert.Equal(t, &SignatureHelp{
					Signatures: []SignatureInformation{{
						Label:         tt.label,
						Documentation: &protocol.Or_SignatureInformation_documentation{Value: tt.doc},
						Parameters:    []ParameterInformation{{Label: "value int"}},
					}},
				}, help)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("ImportedFunction", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`import "fmt"
fmt.Println "value"
`),
		})
		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 1, Character: 5},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, &SignatureHelp{
			Signatures: []SignatureInformation{{
				Label: "println(a ...any) (n int, err error)", Parameters: []ParameterInformation{{Label: "a ...any"}},
			}},
		}, help)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("UTF16", func(t *testing.T) {
		for _, tt := range []struct {
			name            string
			character       uint32
			activeParameter uint32
		}{
			{name: "StringArgument", character: 11, activeParameter: 0},
			{name: "FollowingArgument", character: 14, activeParameter: 1},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte("func combine(text string, count int) {}\ncombine \"\U0001F600\", 1\n"),
				})
				help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
						Position:     Position{Line: 1, Character: tt.character},
					},
				})
				require.NoError(t, err)
				assert.Equal(t, &SignatureHelp{
					Signatures: []SignatureInformation{{
						Label:      "combine(text string, count int)",
						Parameters: []ParameterInformation{{Label: "text string"}, {Label: "count int"}},
					}},
					ActiveParameter: tt.activeParameter,
				}, help)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("FileUpdatesWithTypeErrors", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"functions.xgo": []byte("func combine(count int) {}\n"),
			"main.xgo":      []byte("combine 1\n"),
		})
		params := &SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Position:     Position{Character: 8},
		}}
		help, err := s.textDocumentSignatureHelp(params)
		require.NoError(t, err)
		assert.Equal(t, &SignatureHelp{
			Signatures: []SignatureInformation{{
				Label: "combine(count int)", Parameters: []ParameterInformation{{Label: "count int"}},
			}},
		}, help)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		s.ModifyFiles([]FileChange{
			{Path: "functions.xgo", Content: []byte(`func combine(value int, label string) {}
func broken() { missing() }
`), Version: 1},
			{Path: "main.xgo", Content: []byte("\ncombine 1, \"x\"\n"), Version: 1},
		})
		params.Position = Position{Line: 1, Character: 12}
		help, err = s.textDocumentSignatureHelp(params)
		require.NoError(t, err)
		assert.Equal(t, &SignatureHelp{
			Signatures: []SignatureInformation{{
				Label:      "combine(value int, label string)",
				Parameters: []ParameterInformation{{Label: "value int"}, {Label: "label string"}},
			}},
			ActiveParameter: 1,
		}, help)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.ErrorContains(t, err, "undefined: missing")
	})

	for _, tt := range []struct {
		name         string
		uri          DocumentURI
		source       string
		position     Position
		wantError    bool
		wantASTError bool
		want         *SignatureHelp
	}{
		{name: "EmptyFile", uri: "file:///main.xgo"},
		{name: "MissingFile", uri: "file:///missing.xgo"},
		{name: "NonSourceFile", uri: "file:///notes.txt"},
		{name: "InvalidURI", uri: "https://example.com/main.xgo", wantError: true},
		{
			name: "PositionBeyondEOF", uri: "file:///main.xgo",
			source: "func use(value int) {}\nuse 1\n", position: Position{Line: 100, Character: 100},
			want: &SignatureHelp{Signatures: []SignatureInformation{{
				Label: "use(value int)", Parameters: []ParameterInformation{{Label: "value int"}},
			}}},
		},
		{name: "NonFunction", uri: "file:///main.xgo", source: `var value int
_ = value
`, position: Position{Line: 1, Character: 4}},
		{name: "StartWithInvalidChar", uri: "file:///main.xgo", source: "\n\u201c\u201dvar (\n    maps []int\n)\n", wantASTError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"main.xgo": []byte(tt.source),
				"notes.txt": []byte(`func use(value int) {}
use 1
`),
			})
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: tt.uri},
					Position:     tt.position,
				},
			})
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, help)
			_, err = s.workspaceRootFS.ASTPackage()
			if tt.wantASTError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			}
		})
	}

	t.Run("Autoclosure", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`
onStart => {
	runWhen true, => {}
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 2, Character: 12},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label:         "runWhen(condition bool, callback func())",
			Documentation: &protocol.Or_SignatureInformation_documentation{Value: "RunWhen accepts a deferred condition and a callback."},
			Parameters: []ParameterInformation{
				{
					Label:         "condition bool",
					Documentation: autoclosureParamDocumentation,
				},
				{
					Label: "callback func()",
				},
			},
		}, help.Signatures[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("FuncDecorator", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`func retry(times int, fn func()) {
	fn()
}

@retry(2)
func run() {
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 8},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, "retry(times int)", help.Signatures[0].Label)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("InvalidFuncDecorator", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, `func wrap(fn func(int)) {}
@wr|ap
func run() {}
`)
		s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		t.Cleanup(s.Close)
		_, err := s.requestProject().TypeInfo()
		require.ErrorContains(t, err, "invalid decorator")
		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: pos,
		}})
		require.NoError(t, err)
		assert.Nil(t, help)
	})

	t.Run("FuncDecoratorReturningError", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`func log(fn func() error) {
	_ = fn()
}

@log
func run() error {
	return nil
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 3},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, "log()", help.Signatures[0].Label)
		assert.Empty(t, help.Signatures[0].Parameters)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	for _, tt := range []struct {
		name   string
		result string
		want   string
	}{
		{name: "SingleResult", result: "int", want: "answer() int"},
		{name: "SingleNamedResult", result: "(n int)", want: "answer() (n int)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"main.xgo": []byte("\nfunc answer() " + tt.result + " { return 42 }\n\nfunc main() {\n\tanswer\n}\n"),
			})
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 4, Character: 4},
				},
			})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, tt.want, help.Signatures[0].Label)
			assert.Empty(t, help.Signatures[0].Parameters)
			_, err = s.workspaceRootFS.TypeInfo()
			assert.NoError(t, err)
		})
	}

	t.Run("XGoxMethod", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`
onStart => {
	create Item, "sample"
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label:         "create(T Type, name string) *T",
			Documentation: &protocol.Or_SignatureInformation_documentation{Value: "XGot_App_XGox_Create provides a method with an explicit type argument."},
			Parameters: []ParameterInformation{
				{
					Label: "T Type",
				},
				{
					Label: "name string",
				},
			},
		}, help.Signatures[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("PartialXGoxFunction", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`import "example.com/typeargs"

func main() {
	println typeargs.convert(string, 100)
}
`),
		})
		s.workspaceRootFS.Importer = xgoxTestImporter{fallback: s.workspaceRootFS.Importer}

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 3, Character: 36},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(1), help.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label: "convert(To Type, src From) To",
			Parameters: []ParameterInformation{
				{
					Label: "To Type",
				},
				{
					Label: "src From",
				},
			},
		}, help.Signatures[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("KwargField", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

func main() {
	configure count = 1
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 8, Character: 13},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label: "configure(opts Options)",
			Parameters: []ParameterInformation{
				{
					Label: "opts Options",
				},
			},
		}, help.Signatures[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("OverloadKwargField", func(t *testing.T) {
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

func main() {
	worker.handle count = 1
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 22, Character: 18},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label: "handle(opts CountOptions)",
			Parameters: []ParameterInformation{
				{
					Label: "opts CountOptions",
				},
			},
		}, help.Signatures[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("OverloadIncompleteKwargName", func(t *testing.T) {
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

func main() {
	worker.handle cou = 1
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 22, Character: 16},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 2)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, "handle(opts CountOptions)", help.Signatures[0].Label)
		assert.Equal(t, "handle(opts NameOptions)", help.Signatures[1].Label)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.Error(t, err)
	})

	t.Run("VariadicKwargActiveParameter", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func process(opts map[string]string?, args ...int) {}

func main() {
    process 1, name = "x"
}
`),
		})

		positionalHelp, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 12},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, positionalHelp)
		require.Len(t, positionalHelp.Signatures, 1)
		assert.Equal(t, uint32(1), positionalHelp.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label: "process(opts map[string]string, args ...int)",
			Parameters: []ParameterInformation{
				{
					Label: "opts map[string]string",
				},
				{
					Label: "args ...int",
				},
			},
		}, positionalHelp.Signatures[0])

		kwargHelp, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 23},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, kwargHelp)
		require.Len(t, kwargHelp.Signatures, 1)
		assert.Equal(t, uint32(0), kwargHelp.ActiveParameter)
		assert.Equal(t, positionalHelp.Signatures[0], kwargHelp.Signatures[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})
}
