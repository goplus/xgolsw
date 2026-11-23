package server

import (
	"go/constant"
	gotypes "go/types"
	"slices"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newXGoUnitTestServer(t *testing.T, source string) *Server {
	t.Helper()
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	s.workspaceRootFS.Importer = xgoUnitTestImporter{fallback: s.workspaceRootFS.Importer}
	return s
}

type xgoUnitTestImporter struct {
	fallback gotypes.Importer
}

func (i xgoUnitTestImporter) Import(path string) (*gotypes.Package, error) {
	if path == "example.com/unit" {
		timePkg, err := i.fallback.Import("time")
		if err != nil {
			return nil, err
		}
		pkg := gotypes.NewPackage(path, "unit")
		distanceObj := gotypes.NewTypeName(token.NoPos, pkg, "Distance", nil)
		gotypes.NewNamed(distanceObj, gotypes.Typ[gotypes.Int], nil)
		pkg.Scope().Insert(distanceObj)
		secondsObj := gotypes.NewTypeName(token.NoPos, pkg, "Seconds", nil)
		gotypes.NewAlias(secondsObj, gotypes.Typ[gotypes.Float64])
		pkg.Scope().Insert(secondsObj)
		metersObj := gotypes.NewTypeName(token.NoPos, pkg, "Meters", nil)
		gotypes.NewAlias(metersObj, gotypes.Typ[gotypes.Float64])
		pkg.Scope().Insert(metersObj)
		delayObj := gotypes.NewTypeName(token.NoPos, pkg, "Delay", nil)
		gotypes.NewAlias(delayObj, timePkg.Scope().Lookup("Duration").Type())
		pkg.Scope().Insert(delayObj)
		pkg.Scope().Insert(gotypes.NewConst(
			token.NoPos,
			pkg,
			"XGou_Distance",
			gotypes.Typ[gotypes.UntypedString],
			constant.MakeString("mm=1,cm=10,m=1000"),
		))
		pkg.Scope().Insert(gotypes.NewConst(
			token.NoPos,
			pkg,
			"XGou_Seconds",
			gotypes.Typ[gotypes.UntypedString],
			constant.MakeString("s=1,ms=0.001"),
		))
		pkg.Scope().Insert(gotypes.NewConst(
			token.NoPos,
			pkg,
			"XGou_Meters",
			gotypes.Typ[gotypes.UntypedString],
			constant.MakeString("m=1,km=1000"),
		))
		pkg.MarkComplete()
		return pkg, nil
	}
	return i.fallback.Import(path)
}

func assertCompletionItemTextEdit(t *testing.T, items []CompletionItem, label string, want TextEdit) {
	t.Helper()

	item := completionItemByLabel(items, label)
	require.NotNil(t, item)
	require.Equal(t, UnitCompletion, item.Kind)
	require.NotNil(t, item.TextEdit)
	assert.Equal(t, want, item.TextEdit.Value)
}

func completionItemByLabel(items []CompletionItem, label string) *CompletionItem {
	for i := range items {
		if items[i].Label == label {
			return &items[i]
		}
	}
	return nil
}

func completionItemLabels(items []CompletionItem) []string {
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels
}

func containsCompletionItemKind(items []CompletionItem, kind CompletionItemKind) bool {
	for _, item := range items {
		if item.Kind == kind {
			return true
		}
	}
	return false
}

const keywordUnitTestTypes = `type Distance int
const XGou_Distance = "cm=1,m=100,cm=-1"
type Seconds = float64
const XGou_Seconds = "ms=0.001,s=1"
`

func TestServerUnitsInDynamicKeywordArguments(t *testing.T) {
	const declarations = `import unit "example.com/framework"
type Options interface {
	Set(string, interface{}) Options
	Repeat(string) Options
}
type Builder struct{}
`
	const client = `type Client struct{}
func (Client) Options() Builder { return Builder{} }
func (Client) Run(options Options) {}
var client Client
client.Run distance = 2|cm`
	for _, tt := range []struct{ name, methods, following string }{
		{"TypedSet", "func (Builder) Set(key string, value unit.Distance) Options { return nil }\n", ""},
		{"OverloadedSet", `type RepeatBuilder struct{}
func (RepeatBuilder) Repeat(value unit.Distance) Options { return nil }
func (Builder) setDelay(key string, value unit.Seconds) Options { return nil }
func (Builder) setDistance(key string, value unit.Distance) RepeatBuilder { return RepeatBuilder{} }
func (Builder).Set = (
	(Builder).setDelay
	(Builder).setDistance
)
`, ", repeat = 3m"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, suffixStart := typeDisplayTestSource(t, declarations+tt.methods+client+tt.following+"\n")
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, keywordUnitTestTypes)
			requireNoDiagnostics(t, s)

			doc := TextDocumentIdentifier{URI: s.toDocumentURI(filename)}
			suffixEnd := Position{Line: suffixStart.Line, Character: suffixStart.Character + 2}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: doc, Position: suffixStart,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Equal(t, Range{Start: suffixStart, End: suffixEnd}, hover.Range)
			assert.Contains(t, hover.Contents.Value, "unit `cm` for `unit.Distance`")
			assert.Contains(t, hover.Contents.Value, "Multiplier: `-1`")

			completions := completionListAt(t, s, filename, suffixEnd)
			assertCompletionItemTextEdit(t, completions.Items, "m", TextEdit{
				Range: Range{Start: suffixStart, End: suffixEnd}, NewText: "m",
			})

			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: doc}})
			require.NoError(t, err)
			valueStart := Position{Line: suffixStart.Line, Character: suffixStart.Character - 1}
			wantRange := Range{Start: valueStart, End: suffixStart}
			index := slices.IndexFunc(slots, func(slot XGoInputSlot) bool { return slot.Range == wantRange })
			require.GreaterOrEqual(t, index, 0)
			assert.Equal(t, int64(2), slots[index].Input.Value)
			if tt.following != "" {
				beforeUnit := Position{Line: suffixEnd.Line, Character: suffixEnd.Character + uint32(len(tt.following)-1)}
				afterUnit := Position{Line: beforeUnit.Line, Character: beforeUnit.Character + 1}
				assertCompletionItemTextEdit(t, completionListAt(t, s, filename, afterUnit).Items, "cm", TextEdit{
					Range: Range{Start: beforeUnit, End: afterUnit}, NewText: "cm",
				})
			}
		})
	}
}

func TestServerUnitsAfterKeywordLambda(t *testing.T) {
	const filename = "main.xgo"
	const program = `import unit "example.com/framework"
type Options interface {
	Prepare(func(bool)) Options
	Count(string) Options
}
type Builder struct{}
type Counter struct{}
func (Builder) prepareText(callback func(string, string)) Options { return nil }
func (Builder) prepareNumber(callback func(int)) Counter { return Counter{} }
func (Builder).Prepare = (
	(Builder).prepareText
	(Builder).prepareNumber
)
func (Counter) Count(value unit.Distance) Options { return nil }
type Client struct{}
func (Client) Options() Builder { return Builder{} }
func (Client) Run(options Options) {}
var client Client
client.Run prepare = value => {}, count = 2|`
	for _, tt := range []struct {
		name, suffix, want string
		checked            bool
	}{
		{name: "Checked", suffix: "cm", want: "m", checked: true},
		{name: "IncompleteSuffix", suffix: "c", want: "cm"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, suffixStart := typeDisplayTestSource(t, program+tt.suffix+"\n")
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			setImportTestFrameworkMethods(t, s, keywordUnitTestTypes)

			suffixEnd := Position{Line: suffixStart.Line, Character: suffixStart.Character + uint32(len(tt.suffix))}
			if tt.checked {
				requireNoDiagnostics(t, s)

				proj := s.requestProject()
				info, err := expressionTypeInfo(proj)
				require.NoError(t, err)
				file, err := proj.ASTFile(filename)
				require.NoError(t, err)
				expr, _ := enclosingCallExprAtPosition(info, file, PosAt(proj, file, suffixStart))
				require.NotNil(t, expr)
				require.Len(t, expr.Kwargs, 2)
				arg, ok := xgoutil.ResolveCallExprArg(info, expr, expr.Kwargs[0].Value)
				require.True(t, ok)
				want := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(
					gotypes.NewParam(token.NoPos, nil, "", gotypes.Typ[gotypes.Int]),
				), nil, false)
				assert.True(t, gotypes.Identical(want, arg.ExpectedType), "%v", arg.ExpectedType)

				doc := TextDocumentIdentifier{URI: s.toDocumentURI(filename)}
				hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: doc, Position: suffixStart,
				}})
				require.NoError(t, err)
				require.NotNil(t, hover)
				assert.Contains(t, hover.Contents.Value, "unit `cm` for `unit.Distance`")
				assert.Equal(t, Range{Start: suffixStart, End: suffixEnd}, hover.Range)
			}
			assertCompletionItemTextEdit(t, completionListAt(t, s, filename, suffixEnd).Items, tt.want, TextEdit{
				Range: Range{Start: suffixStart, End: suffixEnd}, NewText: tt.want,
			})
		})
	}
}

func TestServerUnitsInOverloadedKeywordInterfaces(t *testing.T) {
	const filename = "main.xgo"
	source, suffixStart := typeDisplayTestSource(t, `import unit "example.com/framework"
type Options interface { Count(string) Options }
type Builder struct{}
type Client struct{}
func (Client) Options() Builder { return Builder{} }
func (Builder) Count(value unit.Distance) unit.Options { return nil }
func (Client) runLocal(options Options) {}
func (Client) runImported(options unit.Options) {}
func (Client).Run = (
	(Client).runLocal
	(Client).runImported
)
var client Client
client.Run count = 2|cm
`)
	s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
	t.Cleanup(s.Close)
	setImportTestFrameworkMethods(t, s, keywordUnitTestTypes+"type Options interface { Set(string, interface{}) Options }\n")
	requireNoDiagnostics(t, s)

	proj := s.requestProject()
	info, err := proj.TypeInfo()
	require.NoError(t, err)
	call := resourceTestCall(t, proj, filename)
	implementation := xgoutil.CallExprImplementation(info, call)
	require.NotNil(t, implementation)
	require.Equal(t, "runImported", implementation.Name())
	binding := xgoutil.ResolveCallExprKwarg(info, call)
	require.NotNil(t, binding)
	local := info.Pkg.Scope().Lookup("Options")
	require.NotNil(t, local)
	assert.Same(t, local.Type(), binding.ContainerType())
	require.Len(t, call.Kwargs, 1)
	target := binding.KeywordTarget(call.Kwargs[0])
	require.NotNil(t, target)
	method, _, _ := gotypes.LookupFieldOrMethod(local.Type(), false, info.Pkg, "Count")
	require.NotNil(t, method)
	assert.Same(t, method, target.Method)

	doc := TextDocumentIdentifier{URI: s.toDocumentURI(filename)}
	wantDefinition := s.objectDefinitionLocation(proj, info, method)
	require.NotNil(t, wantDefinition)
	definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: doc, Position: RangeForNode(proj, call.Kwargs[0].Name).Start,
	}})
	require.NoError(t, err)
	assert.Equal(t, *wantDefinition, definition)
	hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
		TextDocument: doc, Position: suffixStart,
	}})
	require.NoError(t, err)
	require.NotNil(t, hover)
	assert.Contains(t, hover.Contents.Value, "unit `cm` for `unit.Distance`")
}
