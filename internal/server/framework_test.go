package server

import (
	gotypes "go/types"
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

func (a testFrameworkAdapter) isPropertyType(named *gotypes.Named) bool {
	return named.Obj().Pkg() == a.pkg && named.Obj().Name() == "Item"
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
	s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte("println 1\n")})
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
	assert.True(t, ctx.isFrameworkPropertyType(named))
	assert.True(t, ctx.isPropertyMethod(gotypes.NewFunc(0, pkg, "Current", gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(gotypes.NewVar(0, pkg, "", named)), false))))

	other := newFrameworkTestServer(t, nil)
	otherPkg, err := other.getProj().Importer.Import(testframework.PkgPath)
	require.NoError(t, err)
	otherType := requireValueAs[*gotypes.Named](t, otherPkg.Scope().Lookup("Item").Type())
	assert.False(t, ctx.isFrameworkPropertyType(otherType))
	assert.Equal(t, "Item", ctx.frameworkDisplayTypeName(otherType.Obj(), "Item"))

	ordinary := &definitionContext{proj: s.getProj(), enumInfo: &enumInfo{}, lookupPkgDoc: s.lookupPkgDoc}
	assert.Nil(t, ordinary.frameworkAdapter())
	assert.Equal(t, "Item", ordinary.frameworkDisplayTypeName(named.Obj(), "Item"))
	assert.False(t, ordinary.isFrameworkPropertyType(named))
	assert.False(t, ordinary.isFrameworkEventHandler(fun))
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

func TestInputSlotContextFrameworkResources(t *testing.T) {
	s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte("type Asset string\nfunc use(asset Asset) {}\nuse ((\"logo\"))\nprintln \"ordinary\"\n")})
	proj := s.getProj()
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
