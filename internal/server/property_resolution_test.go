package server

import (
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerXGoGetPropertiesResolution(t *testing.T) {
	for _, tt := range []struct {
		name, methods, want string
	}{
		{"Ordinary", `func (a *App) Label() string { return "" }`, "string"},
		{"Template", `func XGot_App_Label(a *App) string { return "" }`, "string"},
		{"Inferred", `func (a *App) Value() string { return "" }
func XGot_App_Label[T any](a interface{ Value() T }) T { return a.Value() }`, "string"},
		{"Overload", `func XGot_App_Label__0(a interface{ Missing() }) int { return 0 }
func XGot_App_Label__1(a any) string { return "" }`, "string"},
		{"OptionalFirst", `func (a *App) Label__0(__xgo_optional_value int) int { return 0 }
func (a *App) Label__1() string { return "" }`, "int"},
		{"VariadicFirst", `func (a *App) Label__0(values ...int) int { return 0 }
func (a *App) Label__1() string { return "" }`, "int"},
		{"FunctionResult", `func (a *App) Label() func() string { return nil }`, "func() string"},
		{"NamedResult", `type Text string
func (a *App) Label() Text { return "" }`, "Text"},
		{"InvalidReceiver", `func XGot_App_Label(a interface{ Missing() }) string { return "" }`, ""},
		{"Uninferable", `func XGot_App_Label[T any](a *App) string { return "" }`, ""},
		{"VoidFirst", `func (a *App) Label__0() {}
func (a *App) Label__1() string { return "" }`, ""},
		{"TupleFirst", `func (a *App) Label__0() (string, int) { return "", 0 }
func (a *App) Label__1() string { return "" }`, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": nil})
			setImportTestFrameworkMethods(t, s, tt.methods)
			info, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "App"})
			require.NoError(t, err)
			for _, property := range properties {
				assert.False(t, strings.Contains(property.Name, "__"), property.Name)
			}
			index := slices.IndexFunc(properties, func(property XGoProperty) bool { return property.Name == "label" })
			ctx := &completionContext{
				definitionContext: definitionContext{proj: s.requestProject(), lookupPkgDoc: s.lookupPkgDoc},
				typeInfo:          info, itemSet: newCompletionItemSet(PlainText),
			}
			ctx.collectPropertyNames("App")
			if tt.want == "" {
				assert.Equal(t, -1, index)
				assert.NotContains(t, completionItemLabels(ctx.itemSet.items), `"label"`)
				return
			}
			require.NotEqual(t, -1, index)
			assert.Equal(t, tt.want, properties[index].Type)
			assert.Equal(t, XGoPropertyKindMethod, properties[index].Kind)
			item := completionItemByLabel(ctx.itemSet.items, `"label"`)
			require.NotNil(t, item)
			assert.Equal(t, `"label"`, item.InsertText)
			s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte("func run() {\n_ = label\n}\n"), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			assert.NoError(t, err)
		})
	}
}

func TestServerTextDocumentTypeDefinitionAutoProperty(t *testing.T) {
	for _, tt := range []struct {
		name, expression string
		want             bool
	}{
		{"Variable", "res|ult", true},
		{"AutoProperty", "record.val|ue", true},
		{"ParenthesizedReceiver", "(record).val|ue", true},
		{"MethodValue", "record.Val|ue", false},
		{"ExplicitCall", "record.Val|ue()", false},
		{"ExplicitAliasCall", "record.val|ue()", false},
		{"ParenthesizedCall", "(record.Val|ue)()", false},
		{"MethodExpression", "Record.Val|ue", false},
		{"AliasMethodExpression", "Record.val|ue", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, `type Result struct { Name string }
type Record struct{}
func (Record) Value() Result { return Result{} }
var result Result
var record Record
_ = `+tt.expression+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: pos}
			location, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			if tt.want {
				assert.Equal(t, Location{URI: "file:///main.xgo", Range: Range{Start: Position{Character: 5}, End: Position{Character: 5}}}, location)
			} else {
				assert.Nil(t, location)
			}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, "Result")
		})
	}
}

func TestServerTextDocumentTypeDefinitionPackagePropertyAlias(t *testing.T) {
	for _, tt := range []struct {
		name, expression string
		want             bool
	}{
		{"Property", "cur|rent", true},
		{"Parenthesized", "(cur|rent)", true},
		{"ExplicitCall", "cur|rent()", false},
		{"ImplementationValue", "makeRe|sult", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "func makeResult() Result { return Result{} }\nfunc Current = (makeResult)\n_ = "+tt.expression+"\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source), "types.xgo": []byte("type Result struct{}\n")})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			location, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: pos}})
			require.NoError(t, err)
			if tt.want {
				assert.Equal(t, Location{URI: "file:///types.xgo", Range: Range{Start: Position{Character: 5}, End: Position{Character: 5}}}, location)
			} else {
				assert.Nil(t, location)
			}
		})
	}
}

func TestServerTextDocumentTypeDefinitionInferredProperty(t *testing.T) {
	for _, tt := range []struct {
		name, filename, declarations, expression string
		template                                 bool
	}{
		{name: "GenericReceiver", filename: "main.xgo", declarations: "import f \"example.com/framework\"\nvar box f.Box[Result]\n", expression: "box.get|Value"},
		{name: "PackageFunction", filename: "main.xgo", declarations: "func Value() Result { return Result{} }\n", expression: "val|ue"},
		{name: "ClassMethod", filename: "main_fixture.gox", declarations: "func Value() Result { return Result{} }\n", expression: "val|ue"},
		{name: "TemplateBare", filename: "main_fixture.gox", declarations: "func Value() Result { return Result{} }\n", expression: "la|bel", template: true},
		{name: "TemplateSelector", filename: "main_fixture.gox", declarations: "func Value() Result { return Result{} }\n", expression: "this.la|bel", template: true},
		{name: "TemplateChain", filename: "main_fixture.gox", declarations: "func Value() Result { return Result{} }\n", expression: "this.label.ne|xt", template: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.declarations+"func run() {\n_ = "+tt.expression+"\n}\n")
			s := newImportTestServer(t, map[string][]byte{tt.filename: []byte(source), "types.xgo": []byte("type Result struct{}\nfunc (Result) Next() Result { return Result{} }\n")})
			if tt.name == "GenericReceiver" {
				setImportTestFrameworkMethods(t, s, "type Box[T any] struct { Item T }\nfunc (b Box[T]) GetValue() T { return b.Item }\n")
			}
			if tt.template {
				setImportTestFrameworkMethods(t, s, "func XGot_App_Label[T any](a interface{ Value() T }) T { return a.Value() }\n")
			}
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}, Position: pos}
			location, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			assert.Equal(t, Location{URI: "file:///types.xgo", Range: Range{Start: Position{Character: 5}, End: Position{Character: 5}}}, location)
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, "func ")
			if tt.template && tt.name != "TemplateChain" {
				location, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
				require.NoError(t, err)
				assert.Nil(t, location, "an imported getter has no editable project declaration")
			}
		})
	}
}

func TestServerCompletionInferredPropertyReceiver(t *testing.T) {
	for _, tt := range []struct{ name, receiver string }{
		{"Bare", "current"},
		{"Selector", "this.current"},
		{"Parenthesized", "(this.current)"},
		{"Chain", "this.current.next"},
		{"FieldChain", "this.current.Child"},
		{"FieldMethodChain", "this.current.Child.next"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "func Value() Result { return Result{} }\nfunc run() { use "+tt.receiver+".|Text }\n")
			s := newImportTestServer(t, map[string][]byte{
				"main_fixture.gox": []byte(source),
				"types.xgo":        []byte("type Result struct { Child *Result }\nfunc (Result) Next() Result { return Result{} }\nfunc (Result) Text() string { return \"\" }\nfunc (Result) Count() int { return 0 }\nfunc use(text string) {}\n"),
			})
			setImportTestFrameworkMethods(t, s, "func XGot_App_Current[T any](a interface{ Value() T }) T { return a.Value() }\n")
			items := completionItemsAt(t, s, "main_fixture.gox", pos)
			item := completionItemByLabel(items, "Text")
			require.NotNil(t, item)
			assert.Equal(t, "Text", item.InsertText)
			assert.NotContains(t, completionItemLabels(items), "Count")
			s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Version: 1,
				Content: []byte("func Value() Result { return Result{} }\nfunc run() { use " + tt.receiver + "." + item.InsertText + "() }\n")}})
			_, err := s.requestProject().TypeInfo()
			assert.NoError(t, err)
		})
	}
}

func TestServerXGoGetPropertiesProjectState(t *testing.T) {
	t.Run("EditsAndSnapshot", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte("func Value() int { return 0 }\n")})
		setImportTestFrameworkMethods(t, s, "func XGot_App_Label[T any](a interface{ Value() T }) T { return a.Value() }\n")
		before := s.requestProject()
		_, err := before.TypeInfo()
		require.NoError(t, err)
		for version, source := range []string{
			"func Value() []int { return nil }\n",
			"func Value() string { return \"\" }\n",
		} {
			s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(source), Version: version + 1}})
			for _, state := range []struct {
				proj *xgo.Project
				want string
			}{
				{before, "int"},
				{s.requestProject(), []string{"[]int", "string"}[version]},
			} {
				info, err := state.proj.TypeInfo()
				require.NoError(t, err)
				named := requirePropertyTestType(t, info.Pkg, "App")
				ctx := &definitionContext{proj: state.proj, lookupPkgDoc: s.lookupPkgDoc}
				properties := ctx.collectPropertiesFromNamedType(named)
				index := slices.IndexFunc(properties, func(p XGoProperty) bool { return p.Name == "label" })
				require.NotEqual(t, -1, index)
				assert.Equal(t, state.want, properties[index].Type)
			}
		}
	})

	t.Run("ConcurrentColdRequests", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, "func Value() Result { return Result{} }\nfunc run() { _ = this.la|bel }\n")
		s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source), "types.xgo": []byte("type Result struct{}\n")})
		setImportTestFrameworkMethods(t, s, "func XGot_App_Label[T any](a interface{ Value() T }) T { return a.Value() }\n")
		const count = 16
		properties := make([][]XGoProperty, count)
		locations := make([]any, count)
		errs := make([]error, count*2)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := range count {
			wg.Go(func() {
				<-start
				properties[i], errs[i] = s.xgoGetProperties(XGoGetPropertiesParams{Target: "App"})
			})
			wg.Go(func() {
				<-start
				locations[i], errs[count+i] = s.textDocumentTypeDefinition(&TypeDefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}, Position: pos,
				}})
			})
		}
		close(start)
		wg.Wait()
		for _, err := range errs {
			require.NoError(t, err)
		}
		for i := range count {
			index := slices.IndexFunc(properties[i], func(p XGoProperty) bool { return p.Name == "label" })
			require.NotEqual(t, -1, index)
			assert.Equal(t, "Result", properties[i][index].Type)
			assert.Equal(t, Location{URI: "file:///types.xgo", Range: Range{Start: Position{Character: 5}, End: Position{Character: 5}}}, locations[i])
		}
	})
}

func TestServerXGoGetPropertiesIncompleteOverload(t *testing.T) {
	for _, tt := range []struct{ name, declarations, candidates string }{
		{"Missing", "", "missing"},
		{"Variable", "var other int", "other"},
		{"PartiallyResolved", "func number() int { return 0 }", "number; missing"},
		{"NestedMissing", "func nested = (missing)", "nested"},
		{"NestedPartiallyResolved", "func number() int { return 0 }\nfunc nested = (number; missing)", "nested"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newImportTestServer(t, map[string][]byte{"Record.gox": []byte("var Count int\n" + tt.declarations + "\nfunc Value = (" + tt.candidates + ")\n")})
			info, _ := s.requestProject().TypeInfo()
			require.NotNil(t, info)
			properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Record"})
			require.NoError(t, err)
			require.Len(t, properties, 1)
			assert.Equal(t, "Count", properties[0].Name)
			assert.Equal(t, "int", properties[0].Type)
		})
	}
}

func TestServerXGoGetPropertiesEmbeddedReceiver(t *testing.T) {
	for _, tt := range []struct{ name, embedded string }{
		{"Value", "framework.App"},
		{"Pointer", "*framework.App"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := "import \"example.com/framework\"\ntype Record struct { " + tt.embedded + " }\nvar record Record\n_ = record.label\n"
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			setImportTestFrameworkMethods(t, s, "func XGot_App_Label(a *App) string { return \"\" }\n")
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Record"})
			require.NoError(t, err)
			require.Len(t, properties, 1)
			assert.Equal(t, "label", properties[0].Name)
			assert.Equal(t, "string", properties[0].Type)
		})
	}
}
