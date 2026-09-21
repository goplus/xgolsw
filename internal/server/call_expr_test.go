package server

import (
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerCallExpressionKwargs(t *testing.T) {
	const options = "type Options struct { Count int }\n"
	for _, tt := range []struct {
		name         string
		declarations string
		call         string
	}{
		{"FunctionVariable", "var configure = func(opts Options?) {}\n", "configure(count = 1)"},
		{"FunctionField", "type Handler struct { Configure func(opts Options) }\nvar handler Handler\n", "handler.Configure(count = 1)"},
		{"FunctionResult", "func factory() func(opts Options) { return nil }\n", "factory()(count = 1)"},
		{"Parenthesized", "func configure(opts Options?) {}\n", "(configure)(count = 1)"},
		{"MethodExpression", "type Handler struct{}\nvar handler Handler\nfunc (h Handler) Configure(opts Options?) {}\n", "Handler.Configure(handler, count = 1)"},
		{"VariadicFunctionValue", "var configure = func(opts Options?, values ...int) {}\n", "configure(7, 8, count = 1)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"functions.xgo": []byte(options + tt.declarations), "main.xgo": []byte(tt.call + "\n")})
			s.replier = newMockReplier()
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			position := Position{Character: uint32(strings.Index(tt.call, "count"))}
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position}
			definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			wantDefinition := Location{URI: "file:///functions.xgo", Range: Range{Start: Position{Character: 22}, End: Position{Character: 27}}}
			assert.Equal(t, wantDefinition, requireValueAs[Location](t, definition))
			refs, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: params, Context: ReferenceContext{IncludeDeclaration: true}})
			require.NoError(t, err)
			callRange := Range{Start: position, End: Position{Character: position.Character + 5}}
			assert.ElementsMatch(t, []Location{wantDefinition, {URI: params.TextDocument.URI, Range: callRange}}, refs)
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: params.TextDocument, Position: position, NewName: "total"})
			require.NoError(t, err)
			assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
				wantDefinition.URI:      {{Range: wantDefinition.Range, NewText: "Total"}},
				params.TextDocument.URI: {{Range: callRange, NewText: "total"}},
			})
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: params.TextDocument}})
			require.NoError(t, err)
			valuePosition := Position{Character: uint32(strings.LastIndex(tt.call, "1"))}
			index := slices.IndexFunc(slots, func(slot XGoInputSlot) bool { return slot.Range.Start == valuePosition })
			require.NotEqual(t, -1, index)
			assert.Equal(t, XGoInputTypeInteger, slots[index].Accept.Type)
			s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(strings.Replace(tt.call, "count", "co", 1) + "\n"), Version: 1}})
			items := completionItemsAt(t, s, "main.xgo", Position{Character: position.Character + 2})
			assert.Contains(t, completionItemLabels(items), "count")
		})
	}
}

func TestServerCallExpressionVariadics(t *testing.T) {
	for _, tt := range []struct {
		name      string
		call      string
		wantTypes []string
	}{
		{"Elements", "use(1, 2)", []string{"int", "int"}},
		{"Slice", "use(values...)", []string{"[]int"}},
		{"TupleElements", "use((1, 2))", []string{"int", "int"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"functions.xgo": []byte("type Consumer func(values ...int)\nvar use Consumer\nvar values []int\n"),
				"main.xgo":      []byte(tt.call + "\n"),
			})
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			call := resourceTestCall(t, proj, "main.xgo")
			var types []string
			for arg := range resolvedCallExprArgs(info, call) {
				assert.Nil(t, arg.Fun)
				assert.False(t, arg.IsTypeArg())
				assert.Zero(t, arg.ParamIndex)
				types = append(types, arg.ExpectedType.String())
			}
			assert.Equal(t, tt.wantTypes, types)
			hints, err := s.textDocumentInlayHint(&InlayHintParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Range: Range{End: Position{Line: 1}}})
			require.NoError(t, err)
			hintPosition := uint32(4)
			if strings.HasPrefix(tt.name, "Tuple") {
				hintPosition++
			}
			assert.Equal(t, []InlayHint{{Position: Position{Character: hintPosition}, Label: "values...", Kind: Parameter}}, hints)
		})
	}
}

func TestServerGenericCallExpressions(t *testing.T) {
	for _, tt := range []struct {
		name      string
		call      string
		label     string
		paramType string
	}{
		{"Explicit", "api.Use[int](1)", "use(value int)", "int"},
		// XGo currently retains the uninstantiated signature for inferred calls.
		{"Inferred", "api.Use(1)", "use(value T)", "T"},
		{"Parenthesized", "(api.Use[int])(1)", "use(value int)", "int"},
		{"TupleTypeArguments", "api.Pair[string, int]((\"key\", 1))", "pair(key string, value int)", "int"},
		{"TypeArguments", "api.Pair[string, int](\"key\", 1)", "pair(key string, value int)", "int"},
		{"GenericMethodExpression", "api.Box[int].Use(box, 1)", "use(api.Box[int], value int)", "int"},
		{"GenericBoundMethod", "box.Use(1)", "use(value int)", "int"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const declarations = "package api\nfunc Use[T any](value T) {}\nfunc Pair[K, V any](key K, value V) {}\ntype Box[T any] struct{}\nfunc (b Box[T]) Use(value T) {}\n"
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("import api \"example.com/api\"\nvar box api.Box[int]\n" + tt.call + "\n")})
			fset := token.NewFileSet()
			file, err := goparser.ParseFile(fset, "api.go", declarations, 0)
			require.NoError(t, err)
			pkg, err := new(gotypes.Config).Check("example.com/api", fset, []*goast.File{file}, nil)
			require.NoError(t, err)
			base := s.getProj().Importer
			s.getProj().Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
				if path == pkg.Path() {
					return pkg, nil
				}
				return base.Import(path)
			})
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			call := resourceTestCall(t, proj, "main.xgo")
			_, sig, params := xgoutil.ResolveCallExprSignature(info, call)
			require.NotNil(t, sig)
			assert.Equal(t, tt.paramType, params.At(params.Len()-1).Type().String())
			position := Position{Line: 2, Character: uint32(strings.LastIndex(tt.call, "1"))}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position}})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, tt.label, help.Signatures[0].Label)
		})
	}
}

func TestServerCallExpressions(t *testing.T) {
	for _, kind := range []struct {
		name      string
		filename  string
		newServer testServerFactory
	}{
		{"XGo", "main.xgo", newTestServer},
		{"NormalClass", "Record.gox", newTestServer},
		{"ProjectClass", "main_fixture.gox", newFrameworkTestServer},
		{"WorkClass", "Worker_fixture.gox", newFrameworkTestServer},
	} {
		t.Run(kind.name, func(t *testing.T) {
			for _, tt := range []struct {
				name            string
				declarations    string
				call            string
				label           string
				activeParameter uint32
			}{
				{"FunctionVariable", "var use = func(value int) {}\n", "use(1)", "use(value int)", 0},
				{"NamedFunction", "type Callback func(value int)\nvar use Callback\n", "use(1)", "use(value int)", 0},
				{"FunctionField", "type Handler struct { Use func(value int) }\nvar handler Handler\n", "handler.Use(1)", "Use(value int)", 0},
				{"Parenthesized", "func use(value int) {}\n", "((use))(1)", "use(value int)", 0},
				{"FunctionResult", "func factory() func(value int) { return nil }\n", "factory()(1)", "func(value int)", 0},
				{"FunctionLiteral", "", "(func(value int) {})(1)", "func(value int)", 0},
				{"IndexedFunction", "var callbacks []func(value int)\n", "callbacks[0](1)", "callbacks(value int)", 0},
				{"PointerFunction", "var callback = func(value int) {}\nvar use = &callback\n", "(*use)(1)", "func(value int)", 0},
				{"MethodExpression", "type Handler struct{}\nfunc (h Handler) Use(value int) {}\nvar handler Handler\n", "Handler.Use(handler, 1)", "Use(Handler, value int)", 1},
				{"PointerMethodExpression", "type Handler struct{}\nfunc (h *Handler) Use(value int) {}\nvar handler Handler\n", "(*Handler).Use(&handler, 1)", "Use(*Handler, value int)", 1},
				{"PromotedMethodExpression", "type Base struct{}\nfunc (b Base) Use(value int) {}\ntype Handler struct { Base }\nvar handler Handler\n", "(Handler.Use)(handler, 1)", "Use(Handler, value int)", 1},
				{"InterfaceMethodExpression", "type Handler interface { Use(value int) }\nvar handler Handler\n", "Handler.Use(handler, 1)", "Use(Handler, value int)", 1},
				{"MethodExpressionValue", "type Handler struct{}\nfunc (h Handler) Use(value int) {}\nvar handler Handler\nvar use = Handler.Use\n", "use(handler, 1)", "use(Handler, value int)", 1},
				{"MethodValue", "type Handler struct{}\nfunc (h Handler) Use(value int) {}\nvar handler Handler\nvar use = handler.Use\n", "use(1)", "use(value int)", 0},
				{"Tuple", "func use(first string, value int) {}\n", "use((\"first\", 1))", "use(first string, value int)", 1},
				{"TupleFunctionValue", "var use = func(first string, value int) {}\n", "use((\"first\", 1))", "use(first string, value int)", 1},
				{"TupleMethodExpression", "type Handler struct{}\nfunc (h Handler) Use(value int) {}\nvar handler Handler\n", "Handler.Use((handler, 1))", "Use(Handler, value int)", 1},
				{"BoundMethod", "type Handler struct{}\nfunc (h Handler) Use(value int) {}\nvar handler Handler\n", "handler.Use(1)", "Use(value int)", 0},
				{"DereferencedReceiver", "type Handler struct{}\nfunc (h Handler) Use(value int) {}\nvar handler *Handler\n", "(*handler).Use(1)", "Use(value int)", 0},
			} {
				t.Run(tt.name, func(t *testing.T) {
					files := map[string][]byte{"functions.xgo": []byte(tt.declarations), kind.filename: []byte(tt.call + "\n")}
					if kind.filename == "Worker_fixture.gox" {
						files["main_fixture.gox"] = nil
					}
					s := kind.newServer(t, files)
					_, err := s.requestProject().TypeInfo()
					require.NoError(t, err)
					document := TextDocumentIdentifier{URI: s.toDocumentURI(kind.filename)}
					position := Position{Character: uint32(strings.LastIndex(tt.call, "1"))}
					help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: document, Position: position}})
					require.NoError(t, err)
					require.NotNil(t, help)
					require.Len(t, help.Signatures, 1)
					assert.Equal(t, tt.label, help.Signatures[0].Label)
					assert.Equal(t, tt.activeParameter, help.ActiveParameter)
					hints, err := s.textDocumentInlayHint(&InlayHintParams{TextDocument: document, Range: Range{End: Position{Line: 1}}})
					require.NoError(t, err)
					assert.Contains(t, hints, InlayHint{Position: position, Label: "value", Kind: Parameter})
					slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: document}})
					require.NoError(t, err)
					index := slices.IndexFunc(slots, func(slot XGoInputSlot) bool { return slot.Range.Start == position })
					require.NotEqual(t, -1, index)
					assert.Equal(t, XGoInputTypeInteger, slots[index].Accept.Type)
					assert.Equal(t, XGoInput{Kind: XGoInputKindInPlace, Type: XGoInputTypeInteger, Value: int64(1)}, slots[index].Input)
				})
			}
		})
	}
}

func TestServerCallExpressionBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		label  string
	}{
		{"UnnamedParameter", "var use func(int)\nuse(|1)\n", "use(int)"},
		{"NoParameters", "var use func() int\n_ = use(|)\n", "use() int"},
		{"FunctionValueIdentifier", "var use func(value int)\n_ = u|se\n", "use(value int)"},
		{"DeclaredNoParameters", "func use() {}\nuse(|)\n", "use()"},
		{"AnonymousNoParameters", "func factory() func() { return nil }\nfactory()(|)\n", "func()"},
		{"MethodExpressionNoParameters", "type Handler struct{}\nvar handler Handler\nfunc (h Handler) Use() {}\nHandler.Use(|handler)\n", "Use(Handler)"},
		{"BlankParameter", "var use func(_ int)\nuse(|1)\n", "use(_ int)"},
		{"FunctionConversion", "type Callback func(value int)\nvar callback Callback\n_ = Callback(|callback)\n", ""},
		{"ParenthesizedConversion", "type Callback func(value int)\nvar callback Callback\n_ = (Callback)(|callback)\n", ""},
		{"Builtin", "_ = len(|\"value\")\n", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			document := TextDocumentIdentifier{URI: "file:///main.xgo"}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: document, Position: position}})
			require.NoError(t, err)
			if tt.label == "" {
				assert.Nil(t, help)
			} else {
				require.NotNil(t, help)
				require.Len(t, help.Signatures, 1)
				assert.Equal(t, tt.label, help.Signatures[0].Label)
			}
			hints, err := s.textDocumentInlayHint(&InlayHintParams{TextDocument: document, Range: Range{End: Position{Line: 10}}})
			require.NoError(t, err)
			assert.Empty(t, hints)
		})
	}
}

func TestCallArgValueTypes(t *testing.T) {
	type valueType struct {
		expr string
		typ  string
	}
	for _, tt := range []struct {
		name    string
		source  string
		want    []valueType
		wantErr bool
	}{
		{
			name: "Positional",
			source: `func use(name Name, count int) {}
use (("first")), 2
`,
			want: []valueType{{`"first"`, "main.Name"}, {"2", "int"}},
		},
		{
			name: "Slice",
			source: `func use(names []Name) {}
use ["first", ("second")]
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			name: "ParenthesizedSlice",
			source: `func use(names []Name) {}
use ((["first", "second"]))
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			// Matrix literals retain argument context even though the compiler
			// does not type-check them yet.
			name: "Matrix",
			source: `func use(names [][]Name) {}
use [
    "first"
    "second"
]
`,
			want:    []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
			wantErr: true,
		},
		{
			name: "ParenthesizedMatrix",
			source: `func use(names [][]Name) {}
use (([
    "first"
    "second"
]))
`,
			want:    []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
			wantErr: true,
		},
		{
			name: "Variadic",
			source: `func use(prefix int, names ...Name) {}
use 1, "first", ("second")
`,
			want: []valueType{{"1", "int"}, {`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			name: "VariadicSlice",
			source: `func use(names ...Name) {}
use ["first", "second"]...
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			name: "ParenthesizedVariadicSlice",
			source: `func use(names ...Name) {}
use ((["first", "second"]))...
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			name: "SliceAlias",
			source: `type Names = []Name
func use(names Names) {}
use ["first"]
`,
			want: []valueType{{`"first"`, "main.Name"}},
		},
		{
			name: "DefinedSlice",
			source: `type Names []Name
func use(names Names) {}
use (["first"])
`,
			want: []valueType{{`"first"`, "main.Name"}},
		},
		{
			name: "SliceVariable",
			source: `func use(names []Name) {}
var names []Name
use (names)
`,
			want: []valueType{{"names", "[]main.Name"}},
		},
		{
			name: "StructKwargs",
			source: `type Options struct {
    Name Name
    Names []Name
    Count int
}
func use(opts Options?) {}
use name = ("first"), names = (["second", "third"]), count = 4
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}, {`"third"`, "main.Name"}, {"4", "int"}},
		},
		{
			name: "AliasKwarg",
			source: `type Alias = Name
type Options struct { Name Alias }
func use(opts Options?) {}
use name = "first"
`,
			want: []valueType{{`"first"`, "main.Alias"}},
		},
		{
			name: "MapKwargs",
			source: `func use(opts map[string]Name?) {}
use first = "first", second = ("second")
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			name: "InterfaceKwargs",
			source: `type Options interface { Name(name Name) Options }
type Client struct{}
func (c Client) Options() Options { return nil }
func (c Client) use(opts Options?) {}
var client Client
client.use name = ("first")
`,
			want: []valueType{{`"first"`, "main.Name"}},
		},
		{
			name: "OverloadKwargs",
			source: `type Options struct { Names []Name }
type Worker struct{}
func (w *Worker) useNames(opts Options?) {}
func (Worker).use = (
    (Worker).useNames
)
var worker Worker
worker.use names = (["first", "second"])
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			name: "UnresolvedOverload",
			source: `type Options struct { Names []Name }
type Worker struct{}
func (w *Worker) useInt(prefix int, opts Options?) {}
func (w *Worker) useString(prefix string, opts Options?) {}
func (Worker).use = (
    (Worker).useInt
    (Worker).useString
)
var worker Worker
worker.use missing, names = (["first"])
`,
			want:    []valueType{{"missing", "int"}, {`"first"`, "main.Name"}, {"missing", "string"}, {`"first"`, "main.Name"}},
			wantErr: true,
		},
		{
			name: "UnknownKwarg",
			source: `type Options struct { Name Name }
func use(opts Options?) {}
use unknown = "ignored", name = "first"
`,
			want:    []valueType{{`"first"`, "main.Name"}},
			wantErr: true,
		},
		{
			name: "NonSliceCollection",
			source: `func use(name Name) {}
use ["ignored"]
`,
			wantErr: true,
		},
		{
			name: "DefinedElementType",
			source: `type Label string
func use(names []Label) {}
use ["first", "second"]
`,
			want: []valueType{{`"first"`, "main.Label"}, {`"second"`, "main.Label"}},
		},
		{
			name: "EmptyCollection",
			source: `func use(names []Name) {}
use []
`,
		},
		{
			name:   "NoArguments",
			source: "func use() {}\nuse()\n",
		},
		{
			name:    "UnresolvedCall",
			source:  `use "ignored"`,
			wantErr: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := "type Name = string\n" + tt.source
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			proj := s.getProj()
			info, err := proj.TypeInfo()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, info)
			file, err := proj.ASTFile("main.xgo")
			require.NoError(t, err)
			var call *ast.CallExpr
			ast.Inspect(file, func(node ast.Node) bool {
				if expr, ok := node.(*ast.CallExpr); ok {
					call = expr
					return false
				}
				return true
			})
			require.NotNil(t, call)
			var got []valueType
			for expr, typ := range callArgValueTypes(info, call) {
				file := proj.Fset.File(expr.Pos())
				got = append(got, valueType{source[file.Offset(expr.Pos()):file.Offset(expr.End())], typ.String()})
			}
			assert.Equal(t, tt.want, got)
			count := 0
			for range callArgValueTypes(info, call) {
				count++
				break
			}
			assert.Equal(t, min(1, len(tt.want)), count)
		})
	}
}

func TestServerGotoCall(t *testing.T) {
	s := newTestServer(t, map[string][]byte{
		"functions.xgo": []byte("func Goto(value int) {}\nvar target int\nvar targetText string\n"),
		"main.xgo":      []byte("goto target\n"),
	})
	s.replier = newMockReplier()
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	document := TextDocumentIdentifier{URI: "file:///main.xgo"}
	params := TextDocumentPositionParams{TextDocument: document, Position: Position{Character: 7}}
	help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
	require.NoError(t, err)
	require.NotNil(t, help)
	require.Len(t, help.Signatures, 1)
	assert.Equal(t, "Goto(value int)", help.Signatures[0].Label)
	assert.Zero(t, help.ActiveParameter)
	hints, err := s.textDocumentInlayHint(&InlayHintParams{TextDocument: document, Range: Range{End: Position{Line: 1}}})
	require.NoError(t, err)
	assert.Equal(t, []InlayHint{{Position: Position{Character: 5}, Label: "value", Kind: Parameter}}, hints)
	labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", Position{Character: 7}))
	assert.Contains(t, labels, "target")
	assert.NotContains(t, labels, "targetText")
	slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: document}})
	require.NoError(t, err)
	require.Len(t, slots, 1)
	assert.Equal(t, Position{Character: 5}, slots[0].Range.Start)
	assert.Equal(t, XGoInputTypeInteger, slots[0].Accept.Type)

	params.Position.Character = 1
	declaration := Location{URI: "file:///functions.xgo", Range: Range{Start: Position{Character: 5}, End: Position{Character: 9}}}
	call := Location{URI: document.URI, Range: Range{End: Position{Character: 4}}}
	definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
	require.NoError(t, err)
	assert.Equal(t, declaration, requireValueAs[Location](t, definition))
	refs, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: params, Context: ReferenceContext{IncludeDeclaration: true}})
	require.NoError(t, err)
	assert.ElementsMatch(t, []Location{declaration, call}, refs)
	highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: params})
	require.NoError(t, err)
	require.NotNil(t, highlights)
	assert.Equal(t, []DocumentHighlight{{Range: call.Range, Kind: Read}}, *highlights)

	s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte("goto tar\n"), Version: 1}})
	labels = completionItemLabels(completionItemsAt(t, s, "main.xgo", Position{Character: 8}))
	assert.Contains(t, labels, "target")
	assert.NotContains(t, labels, "targetText")

	// A new snapshot with a real label must not retain the previous call.
	s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte("goto target\ntarget:\nprintln 1\n"), Version: 2}})
	_, err = s.requestProject().TypeInfo()
	require.NoError(t, err)
	params.Position.Character = 7
	help, err = s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
	require.NoError(t, err)
	assert.Nil(t, help)
	hints, err = s.textDocumentInlayHint(&InlayHintParams{TextDocument: document, Range: Range{End: Position{Line: 2}}})
	require.NoError(t, err)
	assert.Empty(t, hints)
	params.TextDocument.URI = declaration.URI
	params.Position = declaration.Range.Start
	refs, err = s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: params, Context: ReferenceContext{IncludeDeclaration: true}})
	require.NoError(t, err)
	assert.Equal(t, []Location{declaration}, refs)
}

func TestServerCallExpressionRename(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		want   string
	}{
		{"Decorator", "func Re|try(times int, fn func()) {}\n@retry(2)\nfunc run() {}\n", "func attempt(times int, fn func()) {}\n@attempt(2)\nfunc run() {}\n"},
		{"DecoratorWithoutArguments", "func Re|try(fn func()) {}\n@retry\nfunc run() {}\n", "func attempt(fn func()) {}\n@attempt\nfunc run() {}\n"},
		{"Goto", "func Go|to(value int) {}\nvar target int\ngoto target\n", "func attempt(value int) {}\nvar target int\nattempt target\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			s.replier = newMockReplier()
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position, NewName: "attempt"})
			require.NoError(t, err)
			require.NotNil(t, edit)
			updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
			assert.Equal(t, tt.want, updated)
			s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			require.NoError(t, err)
		})
	}
}

func TestServerTupleCallBoundaries(t *testing.T) {
	for _, tt := range []struct {
		name         string
		declarations string
		call         string
		wantCount    int
		wantSlots    int
	}{
		{"SingleTupleParameter", "func use(pair (int, string)) {}\n", "use((1, \"value\"))", 1, 2},
		{"TupleAndKwarg", "type Options struct { Count int }\nfunc use(pair (int, string), opts Options?) {}\n", "use((1, \"value\"), count = 2)", 1, 3},
		{"TupleAndOtherParameter", "func use(pair (int, string), count int) {}\n", "use((1, \"value\"), 2)", 2, 3},
		{"Decorator", "func use(pair (int, string), fn func()) {}\n", "@use((1, \"value\"))\nfunc run() {}", 1, 2},
		{"Overload", "func integers(first int, second int) {}\nfunc strings(first string, second string) {}\nfunc use = (integers, strings)\n", "use((1, 2))", 2, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"functions.xgo": []byte(tt.declarations), "main.xgo": []byte(tt.call + "\n")})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			hints, err := s.textDocumentInlayHint(&InlayHintParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Range: Range{End: Position{Line: 2}}})
			require.NoError(t, err)
			assert.Len(t, hints, tt.wantCount)
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
			require.NoError(t, err)
			assert.Len(t, slots, tt.wantSlots)
			for _, slot := range slots {
				start := PositionOffset([]byte(tt.call), slot.Range.Start)
				end := PositionOffset([]byte(tt.call), slot.Range.End)
				assert.Contains(t, []string{"1", "2", `"value"`}, tt.call[start:end])
				assert.Equal(t, slot.Input.Type, slot.Accept.Type)
			}
			if tt.wantCount == 1 {
				require.Len(t, hints, 1)
				assert.Equal(t, "pair", hints[0].Label)
				assert.Equal(t, uint32(strings.Index(tt.call, "(1")), hints[0].Position.Character)
			}
		})
	}
}
