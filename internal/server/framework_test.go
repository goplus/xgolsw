package server

import (
	gotypes "go/types"
	"iter"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testFrameworkAdapter struct {
	pkg *gotypes.Package
}

func (a testFrameworkAdapter) displayTypeName(obj gotypes.Object, name string) string {
	if obj.Pkg() == a.pkg && name == "Item" {
		return "Actor"
	}
	return name
}

func (a testFrameworkAdapter) functionDocumentation(fun *gotypes.Func, _ *pkgdoc.PkgDoc) (string, bool) {
	return "Framework method documentation.", fun.Pkg() == a.pkg
}

func (a testFrameworkAdapter) properties(named *gotypes.Named) iter.Seq[propertyObject] {
	if named.Obj().Pkg() != a.pkg {
		return nil
	}
	return func(yield func(propertyObject) bool) {
		field, _, _ := gotypes.LookupFieldOrMethod(named, false, a.pkg, "Value")
		if field != nil {
			yield(propertyObject{Name: "frameworkValue", Object: field, Type: field.Type()})
		}
	}
}

func (a testFrameworkAdapter) isEventHandler(fun *gotypes.Func) bool {
	return fun.Pkg() == a.pkg && strings.HasPrefix(fun.Name(), "On")
}

func newFrameworkDefinitionTestContext(t *testing.T, s *Server) *definitionContext {
	t.Helper()

	proj := s.getProj()
	pkg, err := proj.Importer.Import(testframework.PkgPath)
	require.NoError(t, err)
	return &definitionContext{
		proj:              proj,
		enumInfo:          &enumInfo{},
		lookupPkgDoc:      s.lookupPkgDoc,
		framework:         testFrameworkAdapter{pkg},
		frameworkResolved: true,
	}
}

func TestDefinitionContextFrameworkAdaptation(t *testing.T) {
	s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": nil})
	ctx := newFrameworkDefinitionTestContext(t, s)
	pkg, err := ctx.proj.Importer.Import(testframework.PkgPath)
	require.NoError(t, err)
	named := requireValueAs[*gotypes.Named](t, pkg.Scope().Lookup("Item").Type())
	method, _, _ := gotypes.LookupFieldOrMethod(gotypes.NewPointer(named), true, pkg, "Label")
	fun := requireValueAs[*gotypes.Func](t, method)
	defs := ctx.definitionsFor(fun, "Item")
	require.Len(t, defs, 1)
	assert.Equal(t, ToPtr("Actor.label"), defs[0].ID.Name)
	assert.Equal(t, "Framework method documentation.", defs[0].Detail)
	assert.Equal(t, "Framework method documentation.", ctx.functionDocumentation(fun))
	properties := slices.Collect(ctx.propertyObjects(named))
	require.Len(t, properties, 1)
	assert.Equal(t, "frameworkValue", properties[0].Name)
	assert.Equal(t, "Value", properties[0].Object.Name())

	other := newFrameworkTestServer(t, nil)
	otherPkg, err := other.getProj().Importer.Import(testframework.PkgPath)
	require.NoError(t, err)
	otherType := requireValueAs[*gotypes.Named](t, otherPkg.Scope().Lookup("Item").Type())
	assert.Equal(t, "Item", ctx.frameworkDisplayTypeName(otherType.Obj(), "Item"))
	assert.Nil(t, ctx.framework.properties(otherType))

	ordinary := &definitionContext{proj: s.getProj(), enumInfo: &enumInfo{}, lookupPkgDoc: s.lookupPkgDoc}
	assert.Nil(t, ordinary.frameworkAdapter())
	assert.Equal(t, "Item", ordinary.frameworkDisplayTypeName(named.Obj(), "Item"))
	assert.False(t, ordinary.isFrameworkEventHandler(fun))
	assert.Len(t, slices.Collect(ordinary.propertyObjects(named)), 2)
	doc, ok := ordinary.frameworkFunctionDocumentation(fun, nil)
	assert.Empty(t, doc)
	assert.False(t, ok)
}

func TestDefinitionContextIsInFrameworkEventHandler(t *testing.T) {
	for _, tt := range []struct {
		name   string
		source string
		want   bool
	}{
		{"Callback", "onStart => {\n |println 1\n}\n", true},
		{"FunctionLiteral", "onStart func() { |println 1 }\n", true},
		{"ExplicitReceiver", "this.onStart => { |println 1 }\n", true},
		{"NestedCall", "func run(fn func()) { fn() }\nonStart => { run => { |println 1 } }\n", true},
		{"OrdinaryCallback", "func run(fn func()) { fn() }\nrun => { |println 1 }\n", false},
		{"Shadowed", "onStart := func(fn func()) { fn() }\nonStart => { |println 1 }\n", false},
		{"Argument", "onEvent |\"start\", => {}\n", false},
		{"Parameter", "onEvent \"start\", |value => { println value }\n", false},
		{"Overload", "onEvent \"start\", value => { |println value }\n", true},
		{"UnknownCall", "onMissing => { |println 1 }\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
			ctx := newFrameworkDefinitionTestContext(t, s)
			file, err := ctx.proj.ASTFile("main_fixture.gox")
			require.NoError(t, err)
			if tt.name != "UnknownCall" {
				_, err = ctx.proj.TypeInfo()
				require.NoError(t, err)
			}
			assert.Equal(t, tt.want, ctx.isInFrameworkEventHandler(PosAt(ctx.proj, file, position)))
		})
	}
}

func TestCompletionContextFrameworkEventHandlers(t *testing.T) {
	for _, tt := range []struct {
		name      string
		source    string
		adapt     bool
		wantEvent bool
	}{
		{"Callback", "onStart => {\n |println 1\n}\n", true, true},
		{"OutsideCallback", "\n|println 1\nonStart => {}\n", true, false},
		{"WithoutAdapter", "onStart => {\n |println 1\n}\n", false, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
			ctx := newCompletionTestContext(t, s, "main_fixture.gox", position)
			if tt.adapt {
				ctx.framework = newFrameworkDefinitionTestContext(t, s).framework
				ctx.frameworkResolved = true
			}
			require.Nil(t, ctx.frameworkResult)
			ctx.analyze()
			assert.Equal(t, tt.wantEvent, ctx.inFrameworkEventHandler)
			require.NoError(t, ctx.collect())
			labels := completionItemLabels(ctx.sortedItems())
			assert.Contains(t, labels, "println")
			if tt.wantEvent {
				assert.NotContains(t, labels, "onStart")
				assert.NotContains(t, labels, "onEvent")
			} else {
				assert.Contains(t, labels, "onStart")
				assert.Contains(t, labels, "onEvent")
			}
		})
	}
}

func TestCompletionContextFrameworkEventHandlerPropertyArguments(t *testing.T) {
	for _, tt := range []struct {
		name, declaration, value, callee, callback string
		ordinary, noAdapter                        bool
	}{
		{name: "Property", value: "label"},
		{name: "ParenthesizedProperty", value: "(label)"},
		{name: "MemberProperty", value: "this.label"},
		{name: "FunctionLiteral", value: "label", callback: "func() {\n |println 1\n}"},
		{name: "ExplicitCall", value: "Label()"},
		{name: "Literal", value: `"label"`},
		{name: "OrdinaryCallback", value: "label", callee: "choose", ordinary: true},
		{name: "ShadowedHandler", value: "label", declaration: "onChoose := func(value string, handler func(), last int) {}\n", ordinary: true},
		{name: "WithoutAdapter", value: "label", noAdapter: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, state := range []struct{ name, last string }{{"Complete", "1"}, {"Incomplete", "missing"}} {
				t.Run(state.name, func(t *testing.T) {
					callee := tt.callee
					if callee == "" {
						callee = "onChoose"
					}
					callback := tt.callback
					if callback == "" {
						callback = "=> {\n |println 1\n}"
					}
					source, pos := typeDisplayTestSource(t, "func Label() string { return \"\" }\n"+tt.declaration+callee+" "+tt.value+", "+callback+", "+state.last+"\n")
					s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
					setImportTestFrameworkMethods(t, s, `func (a *App) OnStart(handler func()) {}
func (a *App) OnChoose__0(value string, handler func(), last int) {}
func (a *App) OnChoose__1(value int, handler func(), last int) {}
func (a *App) Choose__0(value string, handler func(), last int) {}
func (a *App) Choose__1(value int, handler func(), last int) {}
`)
					proj := s.getProj()
					_, err := proj.TypeInfo()
					if state.name == "Complete" {
						require.NoError(t, err)
					} else {
						require.ErrorContains(t, err, "undefined: missing")
					}
					ctx := newCompletionTestContext(t, s, "main_fixture.gox", pos)
					ctx.frameworkResolved = true
					if !tt.noAdapter {
						ctx.framework = newFrameworkDefinitionTestContext(t, s).framework
					}
					wantEvent := !tt.ordinary && !tt.noAdapter
					assert.Equal(t, wantEvent, ctx.isInFrameworkEventHandler(ctx.pos))
					ctx.analyze()
					assert.Equal(t, wantEvent, ctx.inFrameworkEventHandler)
					require.NoError(t, ctx.collect())
					labels := completionItemLabels(ctx.sortedItems())
					assert.Contains(t, labels, "println")
					if wantEvent {
						assert.NotContains(t, labels, "onStart")
					} else {
						assert.Contains(t, labels, "onStart")
					}
				})
			}
		})
	}
}

func TestInputSlotContextFrameworkResources(t *testing.T) {
	s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte("type Asset string\nfunc use(asset Asset) {}\nuse ((\"logo\"))\nprintln \"ordinary\"\n")})
	proj := s.requestProject()
	info, err := proj.TypeInfo()
	require.NoError(t, err)
	assetType := info.Pkg.Scope().Lookup("Asset").Type()
	result := newTestResourceAnalysis(testResourceID{"files", "logo"})
	for ref := range resourceReferences(proj, testResourceResolver(t, proj)) {
		result.addResourceRef(ref)
	}
	ctx := inputSlotTestContext(t, s, "main_fixture.gox")
	ctx.frameworkResult = &frameworkAnalysis{
		resources: result,
		inputType: func(typ gotypes.Type) XGoInputType {
			if gotypes.Unalias(typ) == assetType {
				return testResourceInputType
			}
			return inferBasicInputType(typ)
		},
		adaptInputSlot: func(ctx *inputSlotContext, expr ast.Expr, typ gotypes.Type, slot *XGoInputSlot) *XGoInputSlot {
			if lit, ok := expr.(*ast.BasicLit); ok && gotypes.Unalias(typ) == assetType {
				return result.createResourceInputSlot(ctx, lit, typ, testResourceInputType)
			}
			return slot
		},
	}
	slots := findInputSlots(ctx)
	resourceSlot := findInputSlot(slots, XGoResourceURI("test://resources/files/logo"), "", testResourceInputType, XGoInputKindInPlace)
	require.NotNil(t, resourceSlot)
	assert.Equal(t, Range{Start: Position{Line: 2, Character: 6}, End: Position{Line: 2, Character: 12}}, resourceSlot.Range)
	assert.Equal(t, ToPtr(XGoResourceContextURI("test://resources/files")), resourceSlot.Accept.ResourceContext)
	assert.NotNil(t, findInputSlot(slots, "ordinary", "", XGoInputTypeString, XGoInputKindInPlace))
	assert.Nil(t, resolveFrameworkAdapter(proj))
}
