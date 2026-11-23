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
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallValueArgMatchesType(t *testing.T) {
	reflectPkg := gotypes.NewPackage("reflect", "reflect")
	reflectType := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, reflectPkg, "Type", nil),
		gotypes.NewInterfaceType(nil, nil), nil)
	arg := &ast.Ident{Name: "int"}
	info := new(types.Info)
	info.Uses = map[*ast.Ident]gotypes.Object{arg: gotypes.Universe.Lookup("int")}

	assert.True(t, callValueArgMatchesType(info, arg, reflectType, nil))
	assert.False(t, callValueArgMatchesType(info, arg, gotypes.Typ[gotypes.Int], nil))

	t.Run("UncheckedArrowResult", func(t *testing.T) {
		arrow := &ast.ArrowExpr{
			Rhs: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"text"`}},
		}
		results := gotypes.NewTuple(gotypes.NewParam(token.NoPos, nil, "", gotypes.Typ[gotypes.Int]))
		sig := gotypes.NewSignatureType(nil, nil, nil, nil, results, false)

		assert.True(t, callValueArgMatchesType(nil, arrow, sig, nil))
		assert.True(t, callValueArgMatchesType(new(types.Info), arrow, sig, nil))
	})
}

func TestServerOverloadArgumentMatching(t *testing.T) {
	const declarations = `var candidateText string
var candidateNumber int
var candidateFlag bool
var candidateList []int
var candidateComplex complex64

func unary(callback func(int) int, value string) {}
func pair(callback func(int) (int, int), value int) {}
func variadic(callback func(...int) int, value bool) {}
func nullary(callback func() int, value []int) {}
func plain(callback int, value complex64) {}
func use = (
	unary
	pair
	variadic
	nullary
	plain
)

func usingNumber(callback func(int) int, value int) {}
func usingText(callback func(string) string, value string) {}
func useResult = (
	usingText
	usingNumber
)

func block(callback func(int), value string) {}
func variadicBlock(callback func(...int), value int) {}
func useBlock = (
	block
	variadicBlock
)

type TextOptions struct {
	Callback func(int) int
	Value string
}
type NumberOptions struct {
	Callback func(int) (int, int)
	Value int
}
func withText(options TextOptions) {}
func withNumber(options NumberOptions) {}
func useOptions = (
	withText
	withNumber
)

func small(value uint8, label string) {}
func flag(enabled bool, label int) {}
func useNumber = (
	small
	flag
)

type Amount struct {
	Cents uint8
}
func Amount_Init(cents uint8) Amount { return Amount{cents} }
func fromAmount(value Amount, label string) {}
func fromInt(value int, label int) {}
func useAmount = (
	fromAmount
	flag
	fromInt
)
var cents uint8
`
	for _, tt := range []struct {
		name, call, wantSignature, wantCompletion string
		wantActiveParameter                       uint32
	}{
		{
			name:                "Arrow",
			call:                "use(value => 1, cand|idate)",
			wantSignature:       "use(callback func(int) int, value string)",
			wantCompletion:      "candidateText",
			wantActiveParameter: 1,
		},
		{
			name:                "MultipleResults",
			call:                "use(value => (1, 2), cand|idate)",
			wantSignature:       "use(callback func(int) (int, int), value int)",
			wantCompletion:      "candidateNumber",
			wantActiveParameter: 1,
		},
		{
			name:                "LiteralResultType",
			call:                `useResult(value => "text", cand|idate)`,
			wantSignature:       "useResult(callback func(string) string, value string)",
			wantCompletion:      "candidateText",
			wantActiveParameter: 1,
		},
		{
			name:                "Block",
			call:                "useBlock(value => {}, cand|idate)",
			wantSignature:       "useBlock(callback func(int), value string)",
			wantCompletion:      "candidateText",
			wantActiveParameter: 1,
		},
		{
			name:           "Keyword",
			call:           "useOptions(callback = value => 1, value = cand|idate)",
			wantSignature:  "useOptions(options TextOptions)",
			wantCompletion: "candidateText",
		},
		{
			name:                "IntegralFloat",
			call:                "useNumber(42.0, cand|idate)",
			wantSignature:       "useNumber(value uint8, label string)",
			wantCompletion:      "candidateText",
			wantActiveParameter: 1,
		},
		{
			name:                "ImplicitInitializer",
			call:                "useAmount(cents, cand|idate)",
			wantSignature:       "useAmount(value Amount, label string)",
			wantCompletion:      "candidateText",
			wantActiveParameter: 1,
		},
		{
			name:                "IncompatibleInitializer",
			call:                "useAmount(candidateFlag, cand|idate)",
			wantSignature:       "useAmount(enabled bool, label int)",
			wantCompletion:      "candidateNumber",
			wantActiveParameter: 1,
		},
		{
			name:                "OverflowingInitializerInput",
			call:                "useAmount(256, cand|idate)",
			wantSignature:       "useAmount(value int, label int)",
			wantCompletion:      "candidateNumber",
			wantActiveParameter: 1,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, pos := typeDisplayTestSource(t, declarations+tt.call+"\n")
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			_, err := s.getProj().ASTFile(filename)
			require.NoError(t, err)
			_, err = s.getProj().TypeInfo()
			require.Error(t, err)

			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: pos,
			}})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, tt.wantSignature, help.Signatures[0].Label)
			assert.Equal(t, tt.wantActiveParameter, help.ActiveParameter)

			labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
			candidates := slices.DeleteFunc(labels, func(label string) bool { return !strings.HasPrefix(label, "candidate") })
			assert.Equal(t, []string{tt.wantCompletion}, candidates)

			completed := strings.Replace(tt.call, "cand|idate", tt.wantCompletion, 1)
			s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(declarations + completed + "\n"), Version: 1}})
			requireNoDiagnostics(t, s)
		})
	}

	t.Run("ParameterDependentResult", func(t *testing.T) {
		const filename = "main.xgo"
		source, pos := typeDisplayTestSource(t, declarations+"useResult(value => value, cand|idate)\n")
		s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
		t.Cleanup(s.Close)

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: pos,
		}})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 2)
		assert.Equal(t, "useResult(callback func(string) string, value string)", help.Signatures[0].Label)
		assert.Equal(t, "useResult(callback func(int) int, value int)", help.Signatures[1].Label)

		labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
		assert.Contains(t, labels, "candidateNumber")
		assert.Contains(t, labels, "candidateText")
	})
}

func TestServerUnitOverloadArgumentMatching(t *testing.T) {
	const filename = "main.xgo"
	const imports = "import unit \"example.com/framework\"\n"
	newServer := func(t *testing.T, source string) *Server {
		t.Helper()
		s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
		t.Cleanup(s.Close)
		setImportTestFrameworkMethods(t, s, `type Distance int64
const XGou_Distance = "cm=1,m=100"
type Seconds = float64
const XGou_Seconds = "ms=0.001,s=1"
type Meters = float64
const XGou_Meters = "m=1,km=1000"
`)
		return s
	}

	const declarations = imports + `var candidateDistance int
var candidateTime string
var candidateMeters bool
var candidateList []string

func forDistance(amount unit.Distance, count int) {}
func forTime(amount unit.Seconds, label string) {}
func forMeters(amount unit.Meters, enabled bool) {}
func forNumber(amount int, values []string) {}
func measure = (
	forDistance
	forTime
	forMeters
	forNumber
)
`
	for _, tt := range []struct {
		name, unit             string
		candidates, signatures []string
	}{
		{name: "DefinedType", unit: "cm", candidates: []string{"candidateDistance"},
			signatures: []string{"measure(amount unit.Distance, count int)"}},
		{name: "Alias", unit: "ms", candidates: []string{"candidateTime"},
			signatures: []string{"measure(amount unit.Seconds, label string)"}},
		{name: "Shared", unit: "m", candidates: []string{"candidateDistance", "candidateMeters"},
			signatures: []string{"measure(amount unit.Distance, count int)", "measure(amount unit.Meters, enabled bool)"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			call := "measure(1" + tt.unit + ", cand|idate)\n"
			source, pos := typeDisplayTestSource(t, declarations+call)
			s := newServer(t, source)
			_, err := s.requestProject().TypeInfo()
			require.Error(t, err)

			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}, Position: pos,
			}})
			require.NoError(t, err)
			require.NotNil(t, help)
			var signatures []string
			for _, signature := range help.Signatures {
				signatures = append(signatures, signature.Label)
			}
			assert.Equal(t, tt.signatures, signatures)
			assert.Equal(t, uint32(1), help.ActiveParameter)

			labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
			candidates := slices.DeleteFunc(labels, func(label string) bool { return !strings.HasPrefix(label, "candidate") })
			assert.ElementsMatch(t, tt.candidates, candidates)

			for i, candidate := range tt.candidates {
				completed := strings.Replace(call, "cand|idate", candidate, 1)
				s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(declarations + completed), Version: i + 1}})
				requireNoDiagnostics(t, s)
			}
		})
	}

	t.Run("EmptyArgument", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, declarations+"measure(1cm, |)\n")
		s := newServer(t, source)
		labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
		candidates := slices.DeleteFunc(labels, func(label string) bool { return !strings.HasPrefix(label, "candidate") })
		assert.Equal(t, []string{"candidateDistance"}, candidates)
	})

	t.Run("KeywordNames", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, imports+`type DistanceOptions struct { Count int }
type TimeOptions struct { Label string }
func forDistance(amount unit.Distance, options DistanceOptions?) {}
func forTime(amount unit.Seconds, options TimeOptions?) {}
func measure = (forDistance; forTime)
measure(1cm, |unknown)
`)
		s := newServer(t, source)
		labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
		assert.Contains(t, labels, "count")
		assert.NotContains(t, labels, "label")
	})

	t.Run("EditingUnit", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, declarations+"measure(1m|, missing)\n")
		s := newServer(t, source)
		labels := completionItemLabels(completionListAt(t, s, filename, pos).Items)
		assert.Contains(t, labels, "cm")
		assert.Contains(t, labels, "ms")
	})
}

func TestServerKeywordUnitOverloadArgumentMatching(t *testing.T) {
	const declarations = `import unit "example.com/framework"
type DistanceOptions interface { Set(string, interface{}) DistanceOptions }
type DelayOptions interface { Wait(unit.Seconds) DelayOptions }
type AnyOptions interface { Set(string, interface{}) AnyOptions }
type DistanceBuilder struct{}
func (DistanceBuilder) Set(key string, value unit.Distance) DistanceOptions { return nil }
type Client struct{}
func (Client) DistanceOptions() DistanceBuilder { return DistanceBuilder{} }
func (Client) DelayOptions() DelayOptions { return nil }
func (Client) AnyOptions() AnyOptions { return nil }
func (Client) forDistance(count int, options DistanceOptions) {}
func (Client) forDelay(label string, options DelayOptions) {}
func (Client) forAny(values []string, options AnyOptions) {}
func (Client).Run = (
	(Client).forDistance
	(Client).forDelay
	(Client).forAny
)
var client Client
var candidateCount int
var candidateLabel string
var candidateList []string
`
	for _, tt := range []struct {
		name, unit, candidate, signature string
	}{
		{"DynamicSet", "cm", "candidateCount", "Run(count int, options DistanceOptions)"},
		{"NamedMethod", "ms", "candidateLabel", "Run(label string, options DelayOptions)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			call := "client.Run(cand|idate, wait = 2" + tt.unit + ")\n"
			source, pos := typeDisplayTestSource(t, declarations+call)
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, keywordUnitTestTypes)
			_, err := s.requestProject().TypeInfo()
			require.ErrorContains(t, err, "undefined: candidate")

			doc := TextDocumentIdentifier{URI: s.toDocumentURI(filename)}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: doc, Position: pos,
			}})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, tt.signature, help.Signatures[0].Label)

			labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
			candidates := slices.DeleteFunc(labels, func(label string) bool { return !strings.HasPrefix(label, "candidate") })
			assert.Equal(t, []string{tt.candidate}, candidates)

			completed := strings.Replace(call, "cand|idate", tt.candidate, 1)
			s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(declarations + completed), Version: 1}})
			requireNoDiagnostics(t, s)
		})
	}
}

func TestFuncDecoratorParams(t *testing.T) {
	for _, tt := range []struct {
		name, declarations, parameters string
		wantCount                      int
		wantValid                      bool
	}{
		{"Plain", "", "value int, fn func()", 1, true},
		{"PlainError", "", "value int, fn func() error", 1, true},
		{"CallbackAlias", "type Callback = func()", "value int, fn Callback", 1, true},
		{"CallbackAliasChain", "type Callback = func()\ntype Next = Callback", "value int, fn Next", 1, true},
		{"ErrorAlias", "type Failure = error", "value int, fn func() Failure", 1, true},
		{"ErrorAliasChain", "type Failure = error\ntype Next = Failure", "value int, fn func() Next", 1, true},
		{"CallbackAndErrorAliases", "type Failure = error\ntype Callback = func() Failure", "value int, fn Callback", 1, true},
		{"GenericCallbackAlias", "type Callback[T any] = func() T", "value int, fn Callback[error]", 1, true},
		{"NoExplicitArguments", "type Callback = func()", "fn Callback", 0, true},
		{"MultipleExplicitArguments", "type Callback = func()", "value int, label string, fn Callback", 2, true},
		{"NoCallback", "", "", 0, false},
		{"NonFunctionAlias", "type Callback = int", "value int, fn Callback", 0, false},
		{"CallbackWithArguments", "type Callback = func(int)", "value int, fn Callback", 0, false},
		{"VariadicCallback", "type Callback = func(...int)", "value int, fn Callback", 0, false},
		{"WrongResult", "type Callback = func() int", "value int, fn Callback", 0, false},
		{"MultipleResults", "type Callback = func() (int, error)", "value int, fn Callback", 0, false},
		{"DefinedCallback", "type Callback func()", "value int, fn Callback", 0, false},
		{"AliasToDefinedCallback", "type Callback func()\ntype Next = Callback", "value int, fn Next", 0, false},
		{"DefinedError", "type Failure interface { Error() string }", "value int, fn func() Failure", 0, false},
		{"AliasToDefinedError", "type Failure interface { Error() string }\ntype Next = Failure", "value int, fn func() Next", 0, false},
		{"AnonymousErrorAlias", "type Failure = interface { Error() string }", "value int, fn func() Failure", 0, false},
		{"VariadicDecorator", "", "value int, fn ...func()", 0, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := goparser.ParseFile(fset, "decorator.go", "package decorator\n"+tt.declarations+"\nfunc Decorate("+tt.parameters+") {}\n", 0)
			require.NoError(t, err)
			pkg, err := new(gotypes.Config).Check("decorator", fset, []*goast.File{file}, nil)
			require.NoError(t, err)
			function := requireValueAs[*gotypes.Func](t, pkg.Scope().Lookup("Decorate"))
			signature := function.Signature()
			params, valid := funcDecoratorParams(signature)
			require.Equal(t, tt.wantValid, valid)
			if !valid {
				assert.Nil(t, params)
				return
			}
			require.Equal(t, tt.wantCount, params.Len())
			for i := range params.Len() {
				assert.Same(t, signature.Params().At(i), params.At(i))
			}
		})
	}
}

func TestServerCallExpressionDecoratorAliases(t *testing.T) {
	for _, tt := range []struct {
		name, declarations, callback string
		returnsError                 bool
	}{
		{"Plain", "", "func()", false},
		{"CallbackAlias", "type Callback = func()", "Callback", false},
		{"CallbackAliasChain", "type Callback = func()\ntype Next = Callback", "Next", false},
		{"ErrorAlias", "type Failure = error", "func() Failure", true},
		{"ErrorAliasChain", "type Failure = error\ntype Next = Failure", "func() Next", true},
		{"CallbackAndErrorAliases", "type Failure = error\ntype Callback = func() Failure", "Callback", true},
		{"GenericCallbackAlias", "type Callback[T any] = func() T", "Callback[error]", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			function := "func run() {}\n"
			if tt.returnsError {
				function = "func run() error { return nil }\n"
			}
			source, pos := typeDisplayTestSource(t, `import . "example.com/framework"
const (
count = 1
comment = "text"
)
@decorate(co|unt)
`+function)
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, tt.declarations+"\nfunc Decorate(value int, fn "+tt.callback+") {}\n")
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			assert.Empty(t, s.getDiagnostics(t, "main.xgo"))
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Contains(t, labels, "count")
			assert.NotContains(t, labels, "comment")
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: pos,
			}})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, "decorate(value int)", help.Signatures[0].Label)
			assert.Equal(t, []ParameterInformation{{Label: "value int"}}, help.Signatures[0].Parameters)
		})
	}
}

func TestServerCallExpressionKwargs(t *testing.T) {
	const options = "type Options struct { Count int }\n"
	for _, tt := range []struct {
		name         string
		declarations string
		call         string
	}{
		{"FunctionVariable", "var configure = func(opts Options?) {}\n", "configure(count = 1)"},
		{"FunctionField", `type Handler struct { Configure func(opts Options) }
var handler Handler
`, "handler.Configure(count = 1)"},
		{"FunctionResult", "func factory() func(opts Options) { return nil }\n", "factory()(count = 1)"},
		{"Parenthesized", "func configure(opts Options?) {}\n", "(configure)(count = 1)"},
		{"MethodExpression", `type Handler struct{}
var handler Handler
func (h Handler) Configure(opts Options?) {}
`, "Handler.Configure(handler, count = 1)"},
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

func TestServerOverloadedKeywordContainers(t *testing.T) {
	const functions = `type Alias = Options
var candidateText string
var candidateNumber int
`
	for _, tt := range []struct {
		name, declarations, callee string
		owner, member, renamed     string
		wantKind                   xgoutil.ResolvedCallExprKwargTargetKind
	}{
		{
			name: "AliasFirst",
			declarations: `type Options struct { count string; Count int }
func first(opts Alias?) int { return 1 }
func second(opts Options?) string { return "" }
func choose = (first; second)
`,
			callee: "choose", owner: "Options", member: "count", renamed: "label",
			wantKind: xgoutil.ResolvedCallExprKwargTargetStructField,
		},
		{
			name: "NamedFirst",
			declarations: `type Options struct { count int; Count string }
func first(opts Options?) int { return 1 }
func second(opts Alias?) string { return "" }
func choose = (first; second)
`,
			callee: "choose", owner: "Options", member: "Count", renamed: "Label",
			wantKind: xgoutil.ResolvedCallExprKwargTargetStructField,
		},
		{
			name: "Map",
			declarations: `type Options struct { count int }
func Options_Init(opts map[string]string) Options { return Options{} }
func first(opts Options?) int { return 1 }
func second(opts map[string]string) string { return "" }
func choose = (first; second)
`,
			callee: "choose", wantKind: xgoutil.ResolvedCallExprKwargTargetMap,
		},
		{
			name: "InferredMap",
			declarations: `type Options struct { count int }
func Options_Init(opts map[string]string) Options { return Options{} }
func first(opts Options?) int { return 1 }
func second(opts any) string { return "" }
func choose = (first; second)
`,
			callee: "choose", wantKind: xgoutil.ResolvedCallExprKwargTargetMap,
		},
		{
			name: "Builder",
			declarations: `type Options struct { count int }
type Params interface { Count(value string) Params }
type Client struct{}
func Options_Init(opts Params) Options { return Options{} }
func (Client) Params() Params { return nil }
func (Client) first(opts Options?) int { return 1 }
func (Client) second(opts Params?) string { return "" }
func (Client).Choose = ((Client).first; (Client).second)
var client Client
`,
			callee: "client.choose", owner: "Params", member: "Count",
			wantKind: xgoutil.ResolvedCallExprKwargTargetInterfaceMethod,
		},
		{
			name: "ConcreteBuilder",
			declarations: `type Options struct { Count int }
type Params interface { Count(value bool) Params }
type Builder struct{}
func (Builder) Count(value string) Builder { return Builder{} }
func Options_Init(value Builder) Options { return Options{} }
func Params_Init(value Builder) Params { return nil }
type Client struct{}
func (Client) Params() Builder { return Builder{} }
func (Client) first(opts Options?) int { return 1 }
func (Client) second(opts Params?) string { return "" }
func (Client).Choose = ((Client).first; (Client).second)
var client Client
`,
			callee: "client.choose", owner: "Params", member: "Count",
			wantKind: xgoutil.ResolvedCallExprKwargTargetInterfaceMethod,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, pos := typeDisplayTestSource(t, tt.callee+"(co|unt = candidateText)\n")
			s := newImportTestServer(t, map[string][]byte{
				"functions.xgo": []byte(tt.declarations + functions),
				filename:        []byte(source),
			})
			t.Cleanup(s.Close)
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			call := resourceTestCall(t, proj, filename)
			selected := xgoutil.CallExprImplementation(info, call)
			require.NotNil(t, selected)
			assert.Equal(t, "first", selected.Name())

			args := slices.Collect(xgoutil.ResolvedCallExprArgs(info, call))
			require.Len(t, args, 1)
			assert.Same(t, gotypes.Typ[gotypes.String], args[0].ExpectedType)
			require.NotNil(t, args[0].KwargTarget)
			assert.Equal(t, tt.wantKind, args[0].KwargTarget.Kind)

			document := TextDocumentIdentifier{URI: s.toDocumentURI(filename)}
			valuePos := Position{Character: uint32(strings.Index(source, "candidateText") + len("candidate"))}
			labels := completionItemLabels(completionItemsAt(t, s, filename, valuePos))
			assert.Contains(t, labels, "candidateText")
			assert.NotContains(t, labels, "candidateNumber")
			params := TextDocumentPositionParams{TextDocument: document, Position: pos}
			definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			refs, err := s.textDocumentReferences(&ReferenceParams{
				TextDocumentPositionParams: params, Context: ReferenceContext{IncludeDeclaration: true},
			})
			require.NoError(t, err)
			if tt.member == "" {
				assert.Nil(t, definition)
				assert.Empty(t, refs)
				return
			}
			owner := info.Pkg.Scope().Lookup(tt.owner)
			require.NotNil(t, owner)
			member, _, _ := gotypes.LookupFieldOrMethod(owner.Type(), false, info.Pkg, tt.member)
			require.NotNil(t, member)
			wantDefinition := s.objectDefinitionLocation(proj, info, member)
			require.NotNil(t, wantDefinition)
			assert.Equal(t, *wantDefinition, definition)
			use := RangeForNode(proj, call.Kwargs[0].Name)
			assert.ElementsMatch(t, []Location{*wantDefinition, {URI: document.URI, Range: use}}, refs)
			if tt.renamed != "" {
				edit, err := s.textDocumentRename(&RenameParams{TextDocument: document, Position: pos, NewName: "label"})
				require.NoError(t, err)
				assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
					wantDefinition.URI: {{Range: wantDefinition.Range, NewText: tt.renamed}},
					document.URI:       {{Range: use, NewText: "label"}},
				})
			}
		})
	}
}

func TestServerKeywordArgumentPositions(t *testing.T) {
	for _, tt := range []struct {
		name, source, methods string
		wantParams            []int
		wantCall, wantHint    string
	}{
		{
			name: "AppendedToVariadicTail",
			source: `type Head struct { Count int }
type Tail struct{}
func Head_Init(name string) Head { return Head{} }
func Tail_Init(opts map[string]string) Tail { return Tail{} }
func first(head Head?, parts ...Tail) int { return 1 }
func incompatible(opts map[string]string, parts ...int) bool { return false }
func second(name string, part Tail, opts map[string]string) string { return "" }
func choose = (first; incompatible; second)
choose("text", Tail{}, count = "label")
`,
			wantParams: []int{0, 1, 1},
			wantCall:   "first",
			wantHint:   "head",
		},
		{
			name: "InsertedBeforePositionalArgument",
			source: `type Head struct{}
type Tail struct { Count int }
func Head_Init(opts map[string]string) Head { return Head{} }
func Tail_Init(text string) Tail { return Tail{} }
func first(head Head, part Tail?) int { return 1 }
func second(opts map[string]string, parts ...string) string { return "" }
func choose = (first; second)
choose("text", count = "label")
`,
			wantParams: []int{1, 0},
			wantCall:   "first",
			wantHint:   "part",
		},
		{
			name: "VariadicOnly",
			source: `type Tail struct{}
func Tail_Init(opts map[string]string) Tail { return Tail{} }
func first(parts ...Tail) int { return 1 }
func second(opts map[string]string) string { return "" }
func choose = (first; second)
choose(count = "label")
`,
			wantParams: []int{0},
			wantCall:   "first",
		},
		{
			name: "BoundTemplateReceiver",
			source: `import f "example.com/framework"
var item f.Item
item.collect("text", count = "label")
`,
			methods:    "func XGot_Item_Collect(item Item, head string, parts ...map[string]string) {}",
			wantParams: []int{0, 1},
			wantCall:   "XGot_Item_Collect",
			wantHint:   "head",
		},
		{
			name: "StaticOverload",
			source: `import f "example.com/framework"
f.Item.choose("text", count = "label")
`,
			methods: `type Head struct { Count int }
type Tail struct{}
func Head_Init(name string) Head { return Head{} }
func Tail_Init(opts map[string]string) Tail { return Tail{} }
func XGos_Item_Choose__0(__xgo_optional_head Head, parts ...Tail) int { return 1 }
func XGos_Item_Choose__1(name string, opts map[string]string) string { return "" }
`,
			wantParams: []int{0, 1},
			wantCall:   "XGos_Item_Choose__0",
			wantHint:   "head",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			s := newImportTestServer(t, map[string][]byte{filename: []byte(tt.source)})
			t.Cleanup(s.Close)
			if tt.methods != "" {
				setImportTestFrameworkMethods(t, s, tt.methods)
			}
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			call := resourceTestCall(t, proj, filename)
			selected := xgoutil.CallExprImplementation(info, call)
			require.NotNil(t, selected)
			assert.Equal(t, tt.wantCall, selected.Name())

			args := slices.Collect(xgoutil.ResolvedCallExprArgs(info, call))
			require.Len(t, args, len(tt.wantParams))
			for i, index := range tt.wantParams {
				assert.Equal(t, index, args[i].ParamIndex)
			}
			keyword := args[len(args)-1]
			assert.Same(t, gotypes.Typ[gotypes.String], keyword.ExpectedType)
			document := TextDocumentIdentifier{URI: s.toDocumentURI(filename)}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: document,
					Position:     RangeForNode(proj, keyword.Arg).Start,
				},
			})
			require.NoError(t, err)
			require.NotNil(t, help)
			assert.Equal(t, uint32(tt.wantParams[len(tt.wantParams)-1]), help.ActiveParameter)

			if tt.wantHint != "" {
				hints, err := s.textDocumentInlayHint(&InlayHintParams{
					TextDocument: document,
					Range:        RangeForNode(proj, call),
				})
				require.NoError(t, err)
				require.NotEmpty(t, hints)
				assert.Equal(t, tt.wantHint, hints[0].Label)
			}
		})
	}
}

func TestServerVariadicTemplateKeywordField(t *testing.T) {
	const filename = "main.xgo"
	source, pos := typeDisplayTestSource(t, `type Head struct { Count int }
type Tail struct { count int; Count string }
type Client struct{}
var client Client
var candidateText string
func XGot_Client_Collect(client Client, head Head, parts ...Tail) {}
func direct = (XGot_Client_Collect)
client.collect(Head{}, co|unt = candidateText)
`)
	s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
	t.Cleanup(s.Close)
	proj := s.requestProject()
	info, err := proj.TypeInfo()
	require.NoError(t, err)
	call := resourceTestCall(t, proj, filename)
	args := slices.Collect(xgoutil.ResolvedCallExprArgs(info, call))
	require.Len(t, args, 2)
	assert.Equal(t, 1, args[1].ParamIndex)
	assert.Same(t, gotypes.Typ[gotypes.String], args[1].ExpectedType)
	require.NotNil(t, args[1].KwargTarget)
	field := args[1].KwargTarget.Field
	require.NotNil(t, field)
	assert.Equal(t, "Count", field.Name())

	_, sig, params := xgoutil.ResolveCallExprSignature(info, call)
	require.NotNil(t, sig)
	candidate := xgoutil.ResolveCallExprKwargAtArgCount(info, call, sig, params, len(call.Args))
	require.NotNil(t, candidate)
	targets := xgoutil.ListResolvedCallExprKwargTargets(candidate)
	require.Len(t, targets, 1)
	assert.Equal(t, "count", targets[0].Name)
	assert.Same(t, field, targets[0].Field)

	doc := TextDocumentIdentifier{URI: s.toDocumentURI(filename)}
	position := TextDocumentPositionParams{TextDocument: doc, Position: pos}
	definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: position})
	require.NoError(t, err)
	location := s.objectDefinitionLocation(proj, info, field)
	require.NotNil(t, location)
	assert.Equal(t, *location, definition)
	refs, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: position, Context: ReferenceContext{IncludeDeclaration: true}})
	require.NoError(t, err)
	assert.ElementsMatch(t, []Location{*location, {URI: doc.URI, Range: RangeForNode(proj, args[1].Kwarg.Name)}}, refs)

	edit, err := s.textDocumentRename(&RenameParams{TextDocument: doc, Position: pos, NewName: "label"})
	require.NoError(t, err)
	require.NotNil(t, edit)
	updated := applyResourceRenameTestEdits(t, source, edit.Changes[doc.URI])
	assert.Contains(t, updated, "struct { count int; Label string }")
	assert.Contains(t, updated, "client.collect(Head{}, Label = candidateText)")
}

func TestServerKeywordFieldsInNonMainPackage(t *testing.T) {
	const filename = "options.xgo"
	source, pos := typeDisplayTestSource(t, `package library
type Options struct { count int }
func configure(opts Options) {}
func run() { configure co|unt = 1 }
`)
	s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
	t.Cleanup(s.Close)
	s.getProj().PkgPath = "example.com/library"
	requireNoDiagnostics(t, s)

	document := TextDocumentIdentifier{URI: s.toDocumentURI(filename)}
	loc, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: document,
		Position:     pos,
	}})
	require.NoError(t, err)
	want := Location{
		URI:   document.URI,
		Range: Range{Start: Position{Line: 1, Character: 22}, End: Position{Line: 1, Character: 27}},
	}
	assert.Equal(t, want, requireValueAs[Location](t, loc))

	partial := strings.Replace(source, "configure count", "configure co", 1)
	s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(partial), Version: 1}})
	assert.Contains(t, completionItemLabels(completionItemsAt(t, s, filename, pos)), "count")
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
				"functions.xgo": []byte(`type Consumer func(values ...int)
var use Consumer
var values []int
`),
				"main.xgo": []byte(tt.call + "\n"),
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
			const declarations = `package api
func Use[T any](value T) {}
func Pair[K, V any](key K, value V) {}
type Box[T any] struct{}
func (b Box[T]) Use(value T) {}
`
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte("import api \"example.com/api\"\nvar box api.Box[int]\n" + tt.call + "\n")})
			t.Cleanup(s.Close)
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
				{"FunctionField", `type Handler struct { Use func(value int) }
var handler Handler
`, "handler.Use(1)", "Use(value int)", 0},
				{"Parenthesized", "func use(value int) {}\n", "((use))(1)", "use(value int)", 0},
				{"FunctionResult", "func factory() func(value int) { return nil }\n", "factory()(1)", "func(value int)", 0},
				{"FunctionLiteral", "", "(func(value int) {})(1)", "func(value int)", 0},
				{"IndexedFunction", "var callbacks []func(value int)\n", "callbacks[0](1)", "callbacks(value int)", 0},
				{"PointerFunction", "var callback = func(value int) {}\nvar use = &callback\n", "(*use)(1)", "func(value int)", 0},
				{"MethodExpression", `type Handler struct{}
func (h Handler) Use(value int) {}
var handler Handler
`, "Handler.Use(handler, 1)", "Use(Handler, value int)", 1},
				{"PointerMethodExpression", `type Handler struct{}
func (h *Handler) Use(value int) {}
var handler Handler
`, "(*Handler).Use(&handler, 1)", "Use(*Handler, value int)", 1},
				{"ImplicitPointerMethodExpression", `type Handler struct{}
func (h *Handler) Use(value int) {}
var handler Handler
`, "Handler.Use(&handler, 1)", "Use(*Handler, value int)", 1},
				{"PromotedPointerMethodExpression", `type Base struct{}
func (b *Base) Use(value int) {}
type Handler struct { *Base }
var handler Handler
`, "Handler.Use(&handler, 1)", "Use(*Handler, value int)", 1},
				{"PromotedMethodExpression", `type Base struct{}
func (b Base) Use(value int) {}
type Handler struct { Base }
var handler Handler
`, "(Handler.Use)(handler, 1)", "Use(Handler, value int)", 1},
				{"InterfaceMethodExpression", `type Handler interface { Use(value int) }
var handler Handler
`, "Handler.Use(handler, 1)", "Use(Handler, value int)", 1},
				{"MethodExpressionValue", `type Handler struct{}
func (h Handler) Use(value int) {}
var handler Handler
var use = Handler.Use
`, "use(handler, 1)", "use(Handler, value int)", 1},
				{"MethodValue", `type Handler struct{}
func (h Handler) Use(value int) {}
var handler Handler
var use = handler.Use
`, "use(1)", "use(value int)", 0},
				{"Tuple", "func use(first string, value int) {}\n", "use((\"first\", 1))", "use(first string, value int)", 1},
				{"TupleFunctionValue", "var use = func(first string, value int) {}\n", "use((\"first\", 1))", "use(first string, value int)", 1},
				{"TupleMethodExpression", `type Handler struct{}
func (h Handler) Use(value int) {}
var handler Handler
`, "Handler.Use((handler, 1))", "Use(Handler, value int)", 1},
				{"BoundMethod", `type Handler struct{}
func (h Handler) Use(value int) {}
var handler Handler
`, "handler.Use(1)", "Use(value int)", 0},
				{"DereferencedReceiver", `type Handler struct{}
func (h Handler) Use(value int) {}
var handler *Handler
`, "(*handler).Use(1)", "Use(value int)", 0},
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

func TestServerFunctionVariableCallArguments(t *testing.T) {
	const declarations = `import dep "example.com/dependency"
var record dep.Record
const value = 42
`
	for _, tt := range []struct {
		name, call string
		wantActive uint32
	}{
		{name: "Bound", call: "record.applyVariable(|1)"},
		{name: "Parenthesized", call: "(record.applyVariable)(|1)"},
		{name: "Unbound", call: "dep.Record.applyVariable(record, |1)", wantActive: 1},
		{name: "Encoded", call: "dep.XGot_Record_ApplyVariable(record, |1)", wantActive: 1},
		{name: "PackageOverload", call: "(dep.applyCallback)(record, |1)", wantActive: 1},
		{name: "ChainedTemplate", call: "(record.invokeVariable)(|1)"},
		{name: "StaticOverload", call: "(dep.Record.applyStatic)(record, |1)", wantActive: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, declarations+"_ = "+tt.call+"\n")
			s := newNotRecommendedImportServer(t, source)
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)

			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			file, err := proj.ASTFile("main.xgo")
			require.NoError(t, err)
			call, _ := enclosingCallExprAtPosition(info, file, PosAt(proj, file, pos))
			require.NotNil(t, call)
			implementation, sig, params := xgoutil.ResolveCallExprSignature(info, call)
			assert.Nil(t, implementation)
			require.NotNil(t, sig)
			require.Equal(t, int(tt.wantActive)+1, params.Len())
			assert.Equal(t, gotypes.Typ[gotypes.Int], params.At(int(tt.wantActive)).Type())

			document := TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: document, Position: pos},
			})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			require.Len(t, help.Signatures[0].Parameters, int(tt.wantActive)+1)
			assert.Equal(t, "value int", help.Signatures[0].Parameters[tt.wantActive].Label)
			assert.Equal(t, tt.wantActive, help.ActiveParameter)

			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Contains(t, labels, "value")
			assert.NotContains(t, labels, "record")
			hints, err := s.textDocumentInlayHint(&InlayHintParams{
				TextDocument: document,
				Range:        Range{Start: Position{Line: pos.Line}, End: Position{Line: pos.Line + 1}},
			})
			require.NoError(t, err)
			assert.Contains(t, hints, InlayHint{Position: pos, Label: "value", Kind: Parameter})
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: document}})
			require.NoError(t, err)
			index := slices.IndexFunc(slots, func(slot XGoInputSlot) bool { return slot.Range.Start == pos })
			require.NotEqual(t, -1, index)
			assert.Equal(t, XGoInputTypeInteger, slots[index].Accept.Type)
		})
	}
}

func TestServerReconstructedCallResults(t *testing.T) {
	const imports = "import dep \"example.com/dependency\"\n"
	const pairs = `func pair() (string, int) { return "", 1 }
func flagPair() (int, bool) { return 1, true }
func wrongPair() (string, bool) { return "", true }
func single() string { return "" }
`
	const genericResults = `func pair() (int, int) { return 1, 2 }
func mixedPair() (int, string) { return 1, "" }
func single() int { return 1 }
`
	for _, tt := range []struct {
		name, source, wantError string
		want, absent            []string
	}{
		{name: "TemplateVariable", source: `var record dep.Record
func pair() (dep.Record, int) { return record, 1 }
func wrongPair() (dep.Record, string) { return record, "" }
func single() dep.Record { return record }
_ = dep.Record.applyVariable(pa|ir())
`, want: []string{"pair"}, absent: []string{"wrongPair", "single"}},
		{name: "ParenthesizedOverload", source: pairs + "_ = (dep.applyPair)(pa|ir())\n",
			want: []string{"pair"}, absent: []string{"flagPair", "wrongPair", "single"}},
		{name: "ParenthesizedOverloadAlternatives", source: pairs + "_ = (dep.applyPairs)(pa|ir())\n",
			want: []string{"pair", "flagPair"}, absent: []string{"wrongPair", "single"}},
		{name: "DirectOverloadAlternatives", source: pairs + "_ = dep.applyPairs(pa|ir())\n",
			want: []string{"pair", "flagPair"}, absent: []string{"wrongPair", "single"}},
		{name: "MixedOverloadArity", source: pairs + "_ = (dep.applyValues)(pa|ir())\n",
			want: []string{"pair", "single"}, absent: []string{"flagPair", "wrongPair"}},
		{name: "QualifiedCall", source: "_ = (dep.applyPair)(dep.providePa|ir())\n",
			want: []string{"providePair"}, absent: []string{"provideHiddenPair", "provideWrongPair", "provideSingle"}},
		{name: "UnconstrainedQualifiedCall", source: "_ = dep.applyAnyValues(dep.providePa|ir())\n",
			want: []string{"providePair", "provideWrongPair", "provideSingle", "VisibleValue"}, absent: []string{"provideHiddenPair"}},
		{name: "BoundTemplate", source: pairs + "var record dep.Record\n_ = record.applyTextPair(pa|ir())\n",
			wantError: "not enough arguments", want: []string{"single"}, absent: []string{"pair", "flagPair", "wrongPair"}},
		{name: "ImportedOptionalParameter", source: pairs + "_ = dep.applyOptionalValues(si|ngle())\n",
			want: []string{"single"}, absent: []string{"pair", "flagPair", "wrongPair"}},
		{name: "UnnamedLocalOptionalParameter", source: pairs + "func use(string, int?) {}\nuse(si|ngle())\n",
			want: []string{"single"}, absent: []string{"pair", "flagPair", "wrongPair"}},
		{name: "LocalParameterWithImportedPrefix", source: pairs + "func use(first string, __xgo_optional_second int) {}\nuse(pa|ir())\n",
			want: []string{"pair"}, absent: []string{"single", "flagPair", "wrongPair"}},
		{name: "VariadicParameter", source: pairs + `func triple() (string, int, int) { return "", 1, 2 }
_ = dep.applyVariadicValues(pa|ir())
`, want: []string{"single", "pair", "triple"}, absent: []string{"flagPair", "wrongPair"}},
		{name: "AutoclosureParameter", source: `func pair() (string, func() int) { return "", func() int { return 1 } }
func plainPair() (string, int) { return "", 1 }
_ = dep.applyClosurePair(pa|ir())
`, want: []string{"pair"}, absent: []string{"plainPair"}},
		{name: "InitializerParameter", source: `type Amount struct{ Cents int }
func Amount_Init(cents int) Amount { return Amount{cents} }
func use(value Amount, label string) {}
func pair() (int, string) { return 1, "" }
func wrongPair() (bool, string) { return true, "" }
use(pa|ir())
`, want: []string{"pair"}, absent: []string{"wrongPair"}},
		{name: "InferredGeneric", source: genericResults + "_ = dep.applySamePair(pa|ir())\n",
			wantError: "not enough arguments", want: []string{"single"}, absent: []string{"pair", "mixedPair"}},
		{name: "InstantiatedGeneric", source: genericResults + "_ = dep.applySamePair[int](pa|ir())\n",
			want: []string{"pair"}, absent: []string{"single", "mixedPair"}},
		{name: "InferredVariadicGeneric", source: genericResults + "_ = dep.applySameValues(pa|ir())\n",
			wantError: "does not satisfy", want: []string{"single"}, absent: []string{"pair", "mixedPair"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, imports+tt.source)
			s := newNotRecommendedImportServer(t, source)
			t.Cleanup(s.Close)
			if tt.wantError != "" {
				_, err := s.requestProject().TypeInfo()
				require.ErrorContains(t, err, tt.wantError)
			} else {
				requireNoDiagnostics(t, s)
			}

			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			for _, want := range tt.want {
				assert.Contains(t, labels, want)
			}
			for _, absent := range tt.absent {
				assert.NotContains(t, labels, absent)
			}
		})
	}

	t.Run("SignatureHelp", func(t *testing.T) {
		source, pos := typeDisplayTestSource(t, imports+pairs+"_ = (dep.applyPa|irs)(pair())\n")
		s := newNotRecommendedImportServer(t, source)
		t.Cleanup(s.Close)
		requireNoDiagnostics(t, s)

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")},
				Position:     pos,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, "applyPairs(first string, second int) int", help.Signatures[0].Label)
	})
}

func TestServerInitializerValueCompletion(t *testing.T) {
	const declarations = `type Amount struct { Cents uint8 }
const Amount_Max = 100
func Amount_Init(cents uint8) Amount { return Amount{cents} }
const candidateSmall = 20
const candidateLarge = 101
const candidateFraction = 0.5
var candidateCents uint8
var candidateAmount Amount
var candidateFlag bool
func provideCents() uint8 { return candidateCents }
func provideAmount() Amount { return candidateAmount }
func provideFlag() bool { return candidateFlag }
func CandidateCurrent() uint8 { return candidateCents }
func use(value Amount) {}
`
	for _, tt := range []struct {
		name, source string
		want, absent []string
	}{
		{"Argument", "use(cand|idateCents)",
			[]string{"cents", "candidateCents", "candidateAmount"}, []string{"candidateFlag"}},
		{"CallResult", "use(prov|ideCents())",
			[]string{"provideCents", "provideAmount"}, []string{"provideFlag"}},
		{"Assignment", "var amount Amount = cand|idateSmall",
			[]string{"candidateCents", "candidateAmount", "candidateSmall"}, []string{"candidateFlag", "candidateLarge", "candidateFraction"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, pos := typeDisplayTestSource(t, declarations+tt.source+"\n")
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)

			labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
			for _, label := range tt.want {
				assert.Contains(t, labels, label)
			}
			for _, label := range tt.absent {
				assert.NotContains(t, labels, label)
			}
		})
	}

	t.Run("InputSlot", func(t *testing.T) {
		const filename = "main.xgo"
		const call = "use(candidateSmall)\n"
		s := newImportTestServer(t, map[string][]byte{filename: []byte(declarations + call)})
		t.Cleanup(s.Close)
		requireNoDiagnostics(t, s)
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)}}})
		require.NoError(t, err)
		require.NotEmpty(t, slots)
		slot := slots[len(slots)-1]
		require.Equal(t, "candidateSmall", slot.Input.Name)
		for i, name := range []string{"candidateSmall", "candidateCents", "candidateAmount", "candidateCurrent"} {
			assert.Contains(t, slot.PredefinedNames, name)
			s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(declarations + "use(" + name + ")\n"), Version: i + 2}})
			requireNoDiagnostics(t, s)
		}
		for _, name := range []string{"candidateLarge", "candidateFraction", "candidateFlag"} {
			assert.NotContains(t, slot.PredefinedNames, name)
		}
	})
}

func TestServerNamedCallableKeywordArguments(t *testing.T) {
	const variables = `var candidateNumber int
var candidateText string
var explicit map[string]int
`
	const variadic = "type Handler func(label string, values ...map[string]int)\nvar handle Handler\n"
	for _, tt := range []struct {
		name, declarations, call string
		wantIndexes              []int
	}{
		{"VariadicOnly", "type Handler func(values ...map[string]int)\nvar handle Handler\n",
			"handle count = candidate|Number", []int{0}},
		{"VariadicTail", variadic, `handle "label", count = candidate|Number`, []int{0, 1}},
		{"AfterVariadicValue", variadic, `handle "label", explicit, count = candidate|Number`, []int{0, 1, 1}},
		{"StructInitializer", `type Options struct { Count string }
func Options_Init(values map[string]int) Options { return Options{} }
type Handler func(opts Options)
var handle Handler
`, "handle count = candidate|Number", []int{0}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, pos := typeDisplayTestSource(t, variables+tt.declarations+tt.call+"\n")
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)

			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			call := resourceTestCall(t, proj, filename)
			args := slices.Collect(xgoutil.ResolvedCallExprArgs(info, call))
			require.Len(t, args, len(tt.wantIndexes))
			for i, wantIndex := range tt.wantIndexes {
				assert.Equal(t, wantIndex, args[i].ParamIndex)
			}
			kwarg := args[len(args)-1]
			assert.Equal(t, xgoutil.ResolvedCallExprArgKeyword, kwarg.Kind)
			assert.Same(t, gotypes.Typ[gotypes.Int], kwarg.ExpectedType)
			require.NotNil(t, kwarg.KwargTarget)
			assert.Equal(t, xgoutil.ResolvedCallExprKwargTargetMap, kwarg.KwargTarget.Kind)

			document := TextDocumentIdentifier{URI: s.toDocumentURI(filename)}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: document, Position: pos},
			})
			require.NoError(t, err)
			require.NotNil(t, help)
			assert.Equal(t, uint32(kwarg.ParamIndex), help.ActiveParameter)

			keyword := TextDocumentPositionParams{
				TextDocument: document,
				Position:     RangeForNode(proj, kwarg.Kwarg.Name).Start,
			}
			loc, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: keyword})
			require.NoError(t, err)
			assert.Nil(t, loc)
			prepare, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: keyword})
			require.NoError(t, err)
			assert.Nil(t, prepare)

			labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
			assert.Contains(t, labels, "candidateNumber")
			assert.NotContains(t, labels, "candidateText")
		})
	}
}

func TestServerKeywordMapInitializer(t *testing.T) {
	const declarations = `type Options []int
type Alias = Options
var candidateNumber int
var candidateText string
`
	const direct = "func Options_Init(values map[string]int) Options { return nil }\n"
	const overloaded = `func fromValues(values map[string]int) Options { return nil }
func fromCount(value int) Options { return nil }
func Options_Init = (
	fromValues
	fromCount
)
`
	for _, tt := range []struct {
		name, initializer, params, call string
		wantParamIndexes                []int
	}{
		{"Direct", direct, "opts Options", "configure count = candidate|Number", []int{0}},
		{"Alias", direct, "opts Alias", "configure count = candidate|Number", []int{0}},
		{"Variadic", direct, "opts Options, values ...string", "configure candidateText, count = candidate|Number", []int{1, 0}},
		{"Overloaded", overloaded, "opts Options", "configure count = candidate|Number", []int{0}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, pos := typeDisplayTestSource(t, declarations+tt.initializer+"func configure("+tt.params+") {}\n"+tt.call+"\n")
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)

			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			call := resourceTestCall(t, proj, filename)
			var paramIndexes []int
			for arg := range xgoutil.ResolvedCallExprArgs(info, call) {
				paramIndexes = append(paramIndexes, arg.ParamIndex)
				if arg.Kind == xgoutil.ResolvedCallExprArgKeyword {
					require.NotNil(t, arg.KwargTarget)
					assert.Equal(t, xgoutil.ResolvedCallExprKwargTargetMap, arg.KwargTarget.Kind)
					assert.Same(t, gotypes.Typ[gotypes.Int], arg.ExpectedType)
				}
			}
			assert.Equal(t, tt.wantParamIndexes, paramIndexes)
			labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
			assert.Contains(t, labels, "candidateNumber")
			assert.NotContains(t, labels, "candidateText")
		})
	}
}

func TestServerInterfaceKeywordArguments(t *testing.T) {
	const client = `var candidateNumber int
var candidateText string
var candidateFlag bool
type Client struct{}
var client Client
func (Client) Complete(opts Options) {}
`
	const promoted = `type Options interface { Count(any) Options }
type First struct{}
func (First) Count(int) Options { return nil }
type Deeper struct { First }
type Second struct{}
func (Second) Count(any) Options { return nil }
type Builder struct { Deeper; Second }
func (Client) Options() Builder { return Builder{} }
`
	for _, tt := range []struct {
		name, declarations, call string
		want, absent             string
		wantTypes                []string
		typeError                string
	}{
		{
			name: "InterfaceAutoclosure",
			declarations: `type Options interface { Count(__xgo_autoclosure_count func() int) Options }
func (Client) Options() Options { return nil }
`,
			call:      "client.complete count = candidate|Number",
			want:      "candidateNumber",
			absent:    "candidateText",
			wantTypes: []string{"int"},
		},
		{
			name: "ConcreteAutoclosure",
			declarations: `type Options interface { Count(func() int) Options }
type Builder struct{}
func (Builder) Count(__xgo_autoclosure_count func() int) Options { return nil }
func (Client) Options() Builder { return Builder{} }
`,
			call:      "client.complete count = candidate|Number",
			want:      "candidateNumber",
			absent:    "candidateText",
			wantTypes: []string{"int"},
		},
		{
			name: "BuilderChain",
			declarations: `type Options interface { Count(string) Options; Enable(bool) Options }
type Builder struct{}
type Next struct{}
func (Builder) Count(int) Next { return Next{} }
func (Next) Enable(bool) Options { return nil }
func (Client) Options() Builder { return Builder{} }
`,
			call:      "client.complete count = candidateNumber, enable = candidateFlag, count = candidate|Text",
			want:      "candidateText",
			absent:    "candidateNumber",
			wantTypes: []string{"int", "bool", "string"},
		},
		{
			name: "DynamicSet",
			declarations: `type Options interface { Set(name string, value interface{}) Options }
type Builder struct{}
func (Builder) Set(name string, value int) Options { return nil }
func (Client) Options() Builder { return Builder{} }
`,
			call:      "client.complete count = candidate|Number",
			want:      "candidateNumber",
			absent:    "candidateText",
			wantTypes: []string{"int"},
		},
		{
			name: "OverloadedBuilder",
			declarations: `type Options interface { Count(int) Options }
type Builder struct{}
func (Builder) number(value int) Options { return nil }
func (Builder) text(value string) Options { return nil }
func (Builder).Count = (
	(Builder).number
	(Builder).text
)
func (Client) Options() Builder { return Builder{} }
`,
			call:      "client.complete count = candidate|Number",
			want:      "candidateNumber",
			absent:    "candidateText",
			wantTypes: []string{"int"},
		},
		{
			name:         "PromotedBuilderMethod",
			declarations: promoted,
			call:         "client.complete count = candidate|Number",
			want:         "candidateNumber",
			absent:       "candidateText",
			wantTypes:    []string{"int"},
		},
		{
			name:         "PromotedBuilderMethodWithIncompleteValue",
			declarations: promoted,
			call:         "client.complete count = candidate|",
			want:         "candidateNumber",
			absent:       "candidateText",
			wantTypes:    []string{"int"},
			typeError:    "undefined: candidate",
		},
		{
			name:         "PromotedPointerBuilderMethod",
			declarations: strings.Replace(promoted, "Builder { return Builder{} }", "*Builder { return &Builder{} }", 1),
			call:         "client.complete count = candidate|Number",
			want:         "candidateNumber",
			absent:       "candidateText",
			wantTypes:    []string{"int"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, pos := typeDisplayTestSource(t, client+tt.declarations+tt.call+"\n")
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			if tt.typeError == "" {
				require.NoError(t, err)
				requireNoDiagnostics(t, s)
			} else {
				require.ErrorContains(t, err, tt.typeError)
			}
			file, err := proj.ASTFile(filename)
			require.NoError(t, err)
			at := PosAt(proj, file, pos)
			path, _ := xgoutil.PathEnclosingInterval(file, at-1, at)
			call := xgoutil.EnclosingNode[*ast.CallExpr](path)
			require.NotNil(t, call)
			var argTypes []string
			for arg := range xgoutil.ResolvedCallExprArgs(info, call) {
				require.NotNil(t, arg.KwargTarget)
				require.NotNil(t, arg.ExpectedType)
				argTypes = append(argTypes, arg.ExpectedType.String())
			}
			assert.Equal(t, tt.wantTypes, argTypes)

			labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
			assert.Contains(t, labels, tt.want)
			assert.NotContains(t, labels, tt.absent)
		})
	}
}

func TestServerInterfaceKeywordTemplateMethods(t *testing.T) {
	const client = `import f "example.com/framework"
var client f.Client
var candidateNumber int
var candidateText string
var candidateBuilder f.Builder
`
	const options = `type Options interface {
	Count(int) Options
	Label(string) Options
}
type Client struct{}
func (Client) Complete(opts Options) {}
`
	for _, tt := range []struct {
		name, builder, call string
		want, absent        string
		wantTypes           []string
	}{
		{
			name: "Direct",
			builder: `type Builder struct{}
func (Client) Options() Builder { return Builder{} }
func XGot_Builder_Count(builder Builder, count int) Options { return nil }
`,
			call:      "client.complete count = candidate|Number",
			want:      "candidateNumber",
			absent:    "candidateText",
			wantTypes: []string{"int"},
		},
		{
			name: "InferredFromValue",
			builder: `type Builder struct{}
func (Client) Options() Builder { return Builder{} }
func XGot_Builder_Count[N ~int](builder Builder, count N) Options { return nil }
`,
			call:      "client.complete count = candidate|Number",
			want:      "candidateNumber",
			absent:    "candidateText",
			wantTypes: []string{"int"},
		},
		{
			name: "InferredFromReceiver",
			builder: `type Builder struct{}
type Next[T any] interface { Label(T) Options }
func (Client) Options() Builder { return Builder{} }
func XGot_Builder_Count[T any](builder T, count int) Next[T] { return nil }
`,
			call:      "client.complete count = 1, label = candidate|Builder",
			want:      "candidateBuilder",
			absent:    "candidateNumber",
			wantTypes: []string{"int", "example.com/framework.Builder"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, pos := typeDisplayTestSource(t, client+tt.call+"\n")
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, options+tt.builder)
			requireNoDiagnostics(t, s)

			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			call := resourceTestCall(t, proj, filename)
			var argTypes []string
			for arg := range xgoutil.ResolvedCallExprArgs(info, call) {
				require.NotNil(t, arg.KwargTarget)
				assert.Equal(t, xgoutil.ResolvedCallExprKwargTargetInterfaceMethod, arg.KwargTarget.Kind)
				require.NotNil(t, arg.ExpectedType)
				argTypes = append(argTypes, arg.ExpectedType.String())
			}
			assert.Equal(t, tt.wantTypes, argTypes)
			labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
			assert.Contains(t, labels, tt.want)
			assert.NotContains(t, labels, tt.absent)
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
		{"MethodExpressionNoParameters", `type Handler struct{}
var handler Handler
func (h Handler) Use() {}
Handler.Use(|handler)
`, "Use(Handler)"},
		{"BlankParameter", "var use func(_ int)\nuse(|1)\n", "use(_ int)"},
		{"FunctionConversion", `type Callback func(value int)
var callback Callback
_ = Callback(|callback)
`, ""},
		{"ParenthesizedConversion", `type Callback func(value int)
var callback Callback
_ = (Callback)(|callback)
`, ""},
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
		"functions.xgo": []byte(`func Goto(value int) {}
var target int
var targetText string
`),
		"main.xgo": []byte("goto target\n"),
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
	s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(`goto target
target:
println 1
`), Version: 2}})
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
		{"Decorator", `func Re|try(times int, fn func()) {}
@retry(2)
func run() {}
`, `func attempt(times int, fn func()) {}
@attempt(2)
func run() {}
`},
		{"DecoratorWithoutArguments", `func Re|try(fn func()) {}
@retry
func run() {}
`, `func attempt(fn func()) {}
@attempt
func run() {}
`},
		{"Goto", `func Go|to(value int) {}
var target int
goto target
`, `func attempt(value int) {}
var target int
attempt target
`},
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
		{"VariadicTupleParameter", "func use(pair (int, string), extras ...int) {}\n", "use((1, \"value\"))", 1, 2},
		{"TupleAndKwarg", `type Options struct { Count int }
func use(pair (int, string), opts Options?) {}
`, "use((1, \"value\"), count = 2)", 1, 3},
		{"TupleAndOtherParameter", "func use(pair (int, string), count int) {}\n", "use((1, \"value\"), 2)", 2, 3},
		{"Decorator", "func use(pair (int, string), fn func()) {}\n", "@use((1, \"value\"))\nfunc run() {}", 1, 2},
		{"Overload", `func integers(first int, second int) {}
func strings(first string, second string) {}
func use = (integers, strings)
`, "use((1, 2))", 2, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newImportTestServer(t, map[string][]byte{"functions.xgo": []byte(tt.declarations), "main.xgo": []byte(tt.call + "\n")})
			t.Cleanup(s.Close)
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

func TestServerVariadicTupleCallArguments(t *testing.T) {
	const common = `type Pair (first int, second bool)
var candidatePair Pair
var candidateNumber int
var candidateFlag bool
`
	for _, tt := range []struct {
		name, declarations, call string
		frameworkMethods         string
		wantLabel, absentLabel   string
		wantActive               uint32
		wantExpanded             bool
	}{
		{
			name:         "Packed",
			declarations: "func use(pair Pair, extras ...int) {}\n",
			call:         "use((1, candidateFl|ag))",
			wantLabel:    "candidateFlag",
			absentLabel:  "candidateNumber",
		},
		{
			name:         "Expanded",
			declarations: "func use(first int, flags ...bool) {}\n",
			call:         "use((1, candidateFl|ag))",
			wantLabel:    "candidateFlag",
			absentLabel:  "candidateNumber",
			wantActive:   1,
			wantExpanded: true,
		},
		{
			name: "Overloaded",
			declarations: `func packed(pair Pair, extras ...int) {}
func expanded(first int, flags ...bool) {}
func use = (packed, expanded)
`,
			call:        "use((1, candidateFl|ag))",
			wantLabel:   "candidateFlag",
			absentLabel: "candidateNumber",
		},
		{
			name: "BoundTemplate",
			declarations: `import f "example.com/framework"
var item f.Item
var frameworkPair f.Pair
`,
			frameworkMethods: `type Pair struct{ X_0 int; X_1 bool }
func XGot_Item_Apply(item Item, pair Pair, extras ...int) {}
`,
			call:         "item.apply((frameworkPair, candidateNum|ber))",
			wantLabel:    "candidateNumber",
			absentLabel:  "candidateFlag",
			wantActive:   1,
			wantExpanded: true,
		},
		{
			name: "NamedFunction",
			declarations: `type Consumer func(pair Pair, extras ...int)
var use Consumer
`,
			call:         "use((candidatePair, candidateNum|ber))",
			wantLabel:    "candidateNumber",
			absentLabel:  "candidateFlag",
			wantActive:   1,
			wantExpanded: true,
		},
		{
			name: "BoundMethod",
			declarations: `type Client struct{}
var client Client
func (Client) Apply(pair Pair, extras ...int) {}
`,
			call:        "client.Apply((1, candidateFl|ag))",
			wantLabel:   "candidateFlag",
			absentLabel: "candidateNumber",
		},
		{
			name:         "TupleReceiver",
			declarations: "func (Pair) Apply(extras ...int) {}\n",
			call:         "Pair.Apply((1, candidateFl|ag))",
			wantLabel:    "candidateFlag",
			absentLabel:  "candidateNumber",
		},
		{
			name: "MethodExpression",
			declarations: `type Client struct{}
var client Client
func (Client) Apply(pair Pair, extras ...int) {}
`,
			call:         "Client.Apply((client, candidatePa|ir))",
			wantLabel:    "candidatePair",
			absentLabel:  "candidateFlag",
			wantActive:   1,
			wantExpanded: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, tt.call+"\n")
			s := newImportTestServer(t, map[string][]byte{
				"functions.xgo": []byte(tt.declarations + common),
				"main.xgo":      []byte(source),
			})
			t.Cleanup(s.Close)
			if tt.frameworkMethods != "" {
				setImportTestFrameworkMethods(t, s, tt.frameworkMethods)
			}
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			call := resourceTestCall(t, proj, "main.xgo")
			args := slices.Collect(xgoutil.ResolvedCallExprArgs(info, call))
			wantArgs := call.Args
			if tt.wantExpanded {
				require.Len(t, call.Args, 1)
				tuple, ok := call.Args[0].(*ast.TupleLit)
				require.True(t, ok)
				wantArgs = tuple.Elts
			}
			require.Len(t, args, len(wantArgs))
			for i, arg := range args {
				assert.Same(t, wantArgs[i], arg.Arg)
				assert.Equal(t, i, arg.ParamIndex)
			}

			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")},
					Position:     pos,
				},
			})
			require.NoError(t, err)
			require.NotNil(t, help)
			assert.Equal(t, tt.wantActive, help.ActiveParameter)

			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Contains(t, labels, tt.wantLabel)
			assert.NotContains(t, labels, tt.absentLabel)
		})
	}
}

func TestServerErrorWrappedCommandArguments(t *testing.T) {
	const variables = "var candidateText string\nvar candidateNumber int\n"
	const function = "func Save(path string) error { return nil }\n"
	for _, tt := range []struct {
		name, declarations, call, signature string
	}{
		{"Function", function, "save! candidateTe|xt", "Save(path string) error"},
		{"PropagateError", function, "func run() error {\n\tsave? candidateTe|xt\n\treturn nil\n}", "Save(path string) error"},
		{"Method", `type Record struct{}
var record Record
func (Record) Save(path string) error { return nil }
`, "record.save! candidateTe|xt", "Save(path string) error"},
		{"StaticMethod", "import f \"example.com/framework\"\n", "f.Item.save! candidateTe|xt", "save(path string) error"},
		{"Overload", `func SaveText(path string) error { return nil }
func SaveFlag(flag bool) error { return nil }
func Save = (
	SaveText
	SaveFlag
)
`, "save! candidateTe|xt", "save(path string) error"},
		{"FunctionValue", `type Saver func(path string) error
var save Saver
`, "save! candidateTe|xt", "save(path string) error"},
		{"InterfaceKeyword", `type Options interface { Label(value string) Options }
type Record struct{}
var record Record
func (Record) Options() Options { return nil }
func (Record) Save(options Options) error { return nil }
`, "record.save! label = candidateTe|xt", "Save(options Options) error"},
		{"WrappedCall", function, "save(candidateTe|xt)!", "Save(path string) error"},
		{"CallReturnedFunction", "func Save() (func(path string), error) { return nil, nil }\n",
			"save()!(candidateTe|xt)", "func(path string)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, pos := typeDisplayTestSource(t, tt.declarations+variables+tt.call+"\n")
			s := newErrorWrapTestServer(t, source)
			t.Cleanup(s.Close)
			if tt.name == "StaticMethod" {
				setImportTestFrameworkMethods(t, s, "func XGos_Item_Save(path string) error { return nil }")
			}
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			file, err := proj.ASTFile(filename)
			require.NoError(t, err)
			call, _ := enclosingCallExprAtPosition(info, file, PosAt(proj, file, pos))
			require.NotNil(t, call)
			args := slices.Collect(xgoutil.ResolvedCallExprArgs(info, call))
			require.Len(t, args, 1)
			assert.Same(t, gotypes.Typ[gotypes.String], args[0].ExpectedType)

			document := TextDocumentIdentifier{URI: s.toDocumentURI(filename)}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: document,
					Position:     pos,
				},
			})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, tt.signature, help.Signatures[0].Label)
			assert.Zero(t, help.ActiveParameter)

			labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
			assert.Contains(t, labels, "candidateText")
			assert.NotContains(t, labels, "candidateNumber")

			hints, err := s.textDocumentInlayHint(&InlayHintParams{
				TextDocument: document,
				Range:        Range{Start: Position{Line: pos.Line}, End: Position{Line: pos.Line + 1}},
			})
			require.NoError(t, err)
			if len(call.Kwargs) != 0 {
				assert.Empty(t, hints)
			} else {
				require.Len(t, hints, 1)
				assert.Equal(t, args[0].Param.Name(), hints[0].Label)
			}
		})
	}
}

func TestServerErrorWrappedOverloadKeywords(t *testing.T) {
	const declarations = `type Options struct { Count int }
func Options_Init(opts map[string]string) Options { return Options{} }
func first(opts Options?) error { return nil }
func second(opts map[string]string) error { return nil }
func choose = (first; second)
var candidateText string
var candidateNumber int
`
	for _, tt := range []struct {
		name, call string
	}{
		{"WrappedCall", "choose(co|unt = candidateText)!"},
		{"WrappedCommand", "choose! co|unt = candidateText"},
		{"PropagatingCommand", "func run() error {\n choose? co|unt = candidateText\n return nil\n}"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, pos := typeDisplayTestSource(t, declarations+tt.call+"\n")
			s := newErrorWrapTestServer(t, source)
			t.Cleanup(s.Close)
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			file, err := proj.ASTFile(filename)
			require.NoError(t, err)
			call, _ := enclosingCallExprAtPosition(info, file, PosAt(proj, file, pos))
			require.NotNil(t, call)
			selection := xgoutil.CallExprImplementation(info, call)
			require.NotNil(t, selection)
			assert.Equal(t, "first", selection.Name())

			args := slices.Collect(xgoutil.ResolvedCallExprArgs(info, call))
			require.Len(t, args, 1)
			assert.Same(t, gotypes.Typ[gotypes.String], args[0].ExpectedType)
			require.NotNil(t, args[0].KwargTarget)
			assert.Equal(t, xgoutil.ResolvedCallExprKwargTargetMap, args[0].KwargTarget.Kind)

			valuePos := RangeForNode(proj, args[0].Arg).Start
			valuePos.Character += uint32(len("candidate"))
			labels := completionItemLabels(completionItemsAt(t, s, filename, valuePos))
			assert.Contains(t, labels, "candidateText")
			assert.NotContains(t, labels, "candidateNumber")

			definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)},
				Position:     pos,
			}})
			require.NoError(t, err)
			assert.Nil(t, definition)
		})
	}
}

func TestServerErrorWrappedCommandCalleeCompletion(t *testing.T) {
	const functions = `func Save(path string) error { return nil }
func SaveAs(path string) (string, error) { return "", nil }
func SaveOld(path string) string { return "" }
func SaveNoResult(path string) {}
`
	for _, tt := range []struct {
		name, declarations, command string
	}{
		{"Function", functions, "sa|ve! \"file.txt\""},
		{"Method", "type Record struct{}\nvar record Record\n" + strings.ReplaceAll(functions, "func ", "func (Record) "),
			"record.sa|ve! \"file.txt\""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, pos := typeDisplayTestSource(t, tt.declarations+tt.command+"\n")
			s := newErrorWrapTestServer(t, source)
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)

			labels := completionItemLabels(completionItemsAt(t, s, filename, pos))
			assert.Contains(t, labels, "Save")
			assert.Contains(t, labels, "SaveAs")
			assert.NotContains(t, labels, "SaveOld")
			assert.NotContains(t, labels, "SaveNoResult")
		})
	}
}

func newErrorWrapTestServer(t *testing.T, source string) *Server {
	t.Helper()

	s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	addErrorWrapTestImport(t, s)
	return s
}

func addErrorWrapTestImport(t *testing.T, s *Server) {
	t.Helper()

	const errorPath = "github.com/qiniu/x/errors"
	proj := s.getProj()
	file, err := goparser.ParseFile(proj.Fset, "errors.go", `package errors
type Frame struct{}
func (*Frame) Error() string { return "" }
func NewFrame(err error, code, file string, line int, fn string, args ...any) *Frame { return nil }
`, 0)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check(errorPath, proj.Fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	fallback := proj.Importer
	proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
		if path == errorPath {
			return pkg, nil
		}
		return fallback.Import(path)
	})
}
