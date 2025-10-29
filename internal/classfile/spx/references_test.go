package spx

import (
	"go/constant"
	"go/token"
	"go/types"
	"strconv"
	"testing"

	xgoast "github.com/goplus/xgo/ast"
	xgotoken "github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/xgo"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectResourceRefsWithoutTypeInfo(t *testing.T) {
	proj := newTestProject(defaultProjectFiles(), 0)
	refs, diags := collectResourceRefs(proj, nil, func(s string) string { return s })
	assert.Nil(t, refs)
	assert.Nil(t, diags)
}

func TestResourceRefCollectorCollectSpriteTypes(t *testing.T) {
	rrc := newResourceRefCollectorForTest(t, nil)
	pkg := types.NewPackage("main", "main")
	heroObj := types.NewTypeName(token.NoPos, pkg, "Hero", nil)
	heroType := types.NewNamed(heroObj, types.NewStruct(nil, nil), nil)
	pkg.Scope().Insert(heroObj)
	rrc.typeInfo.Pkg = pkg

	rrc.collectSpriteTypes()

	assert.True(t, rrc.hasSpriteType(heroType))
}

func TestResourceRefCollectorInspectSpriteAutoBindings(t *testing.T) {
	rrc := newResourceRefCollectorForTest(t, nil)
	pkg := types.NewPackage("main", "main")
	rrc.typeInfo.Pkg = pkg

	spriteField := types.NewVar(token.NoPos, pkg, "Hero", SpriteType())
	gameStruct := types.NewStruct([]*types.Var{spriteField}, []string{"Hero"})
	gameObj := types.NewTypeName(token.NoPos, pkg, "Game", nil)
	types.NewNamed(gameObj, gameStruct, nil)
	pkg.Scope().Insert(gameObj)

	rrc.spriteTypes[SpriteType()] = struct{}{}
	rrc.typeInfo.Uses[&xgoast.Ident{Name: "Hero", NamePos: xgotoken.Pos(10)}] = spriteField

	rrc.inspectSpriteAutoBindings()

	require.NotEmpty(t, rrc.spriteAutoBindings)
	require.Len(t, rrc.refs, 1)
	assert.Equal(t, SpriteResourceID{SpriteName: "Hero"}, rrc.refs[0].ID)
}

func TestResourceRefCollectorInspectDefinitions(t *testing.T) {
	rrc := newResourceRefCollectorForTest(t, nil)
	pkg := types.NewPackage("main", "main")
	rrc.typeInfo.Pkg = pkg

	value := newStringLiteral("Hero")
	ident := &xgoast.Ident{Name: "heroName", NamePos: xgotoken.Pos(1)}
	valueSpec := &xgoast.ValueSpec{
		Names:  []*xgoast.Ident{ident},
		Values: []xgoast.Expr{value},
	}
	ident.Obj = &xgoast.Object{Kind: xgoast.Con, Decl: valueSpec}

	constObj := types.NewConst(token.NoPos, pkg, "heroName", SpriteNameType(), constant.MakeString("Hero"))
	rrc.typeInfo.Defs[ident] = constObj
	setExprTypeValue(rrc, value, SpriteNameType(), "Hero")

	rrc.inspectDefinitions()

	require.Len(t, rrc.refs, 1)
	assert.Equal(t, SpriteResourceID{SpriteName: "Hero"}, rrc.refs[0].ID)
}

func TestResourceRefCollectorInspectExpressions(t *testing.T) {
	t.Run("CallLiteralArgs", func(t *testing.T) {
		rrc := newResourceRefCollectorForTest(t, nil)
		pkg := types.NewPackage("main", "main")
		rrc.typeInfo.Pkg = pkg

		param := types.NewParam(token.NoPos, pkg, "name", SoundNameType())
		sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(param), nil, false)
		fun := types.NewFunc(token.NoPos, pkg, "PlaySound", sig)

		callIdent := &xgoast.Ident{Name: "PlaySound"}
		callExpr := &xgoast.CallExpr{
			Fun:  callIdent,
			Args: []xgoast.Expr{newStringLiteral("Click")},
		}
		rrc.typeInfo.Info.Uses[callIdent] = fun
		setExprTypeValue(rrc, callExpr.Args[0], SoundNameType(), "Click")
		rrc.typeInfo.Types[callExpr] = types.TypeAndValue{Type: sig}

		rrc.inspectExpressions()

		require.Len(t, rrc.refs, 1)
		assert.Equal(t, SoundResourceID{SoundName: "Click"}, rrc.refs[0].ID)
	})

	t.Run("SliceArgs", func(t *testing.T) {
		rrc := newResourceRefCollectorForTest(t, nil)
		pkg := types.NewPackage("main", "main")
		rrc.typeInfo.Pkg = pkg

		param := types.NewParam(token.NoPos, pkg, "names", types.NewSlice(SoundNameType()))
		sig := types.NewSignatureType(nil, nil, nil, types.NewTuple(param), nil, false)
		fun := types.NewFunc(token.NoPos, pkg, "PlayAll", sig)

		callIdent := &xgoast.Ident{Name: "PlayAll"}
		slice := &xgoast.SliceLit{
			Elts: []xgoast.Expr{
				newStringLiteral("Click"),
				newStringLiteral("Explosion"),
			},
		}
		callExpr := &xgoast.CallExpr{
			Fun:  callIdent,
			Args: []xgoast.Expr{slice},
		}
		rrc.typeInfo.Info.Uses[callIdent] = fun
		for idx, elt := range slice.Elts {
			setExprTypeValue(rrc, elt, SoundNameType(), []string{"Click", "Explosion"}[idx])
		}
		rrc.typeInfo.Types[callExpr] = types.TypeAndValue{Type: sig}

		rrc.inspectExpressions()

		require.Len(t, rrc.refs, 2)
		assert.ElementsMatch(t, []ResourceID{
			SoundResourceID{SoundName: "Click"},
			SoundResourceID{SoundName: "Explosion"},
		}, []ResourceID{rrc.refs[0].ID, rrc.refs[1].ID})
	})
}

func TestResourceRefCollectorResolveAssignedExpr(t *testing.T) {
	proj := newTestProject(map[string]string{
		"main.spx": `package main

func example() {
	hero := "Ghost"
	hero = "Hero"
}
`,
	}, xgo.FeatASTCache)

	rrc := newResourceRefCollectorForTest(t, proj)
	ident := findAssignIdent(t, proj, "main.spx", "hero")

	expr := rrc.resolveAssignedExpr(ident)
	lit, ok := expr.(*xgoast.BasicLit)
	require.True(t, ok)
	assert.Equal(t, `"Hero"`, lit.Value)
}

func TestResourceRefCollectorResolveReturnType(t *testing.T) {
	proj := newTestProject(map[string]string{
		"main.spx": `package main

func resourceName() {
	return "Hero"
}
`,
	}, xgo.FeatASTCache)

	rrc := newResourceRefCollectorForTest(t, proj)
	file := mustASTFile(t, proj, "main.spx")
	funcDecl := findFuncDecl(t, file, "resourceName")
	require.NotNil(t, funcDecl)
	retStmt := findReturnStmt(funcDecl)
	require.NotNil(t, retStmt)
	require.Len(t, retStmt.Results, 1)

	pkg := types.NewPackage("main", "main")
	resultVar := types.NewVar(token.NoPos, pkg, "", SpriteNameType())
	sig := types.NewSignature(nil, types.NewTuple(), types.NewTuple(resultVar), false)
	funcObj := types.NewFunc(token.NoPos, pkg, "resourceName", sig)

	rrc.typeInfo = &xgotypes.Info{
		Info: typesutil.Info{
			Defs: map[*xgoast.Ident]types.Object{
				funcDecl.Name: funcObj,
			},
		},
		ObjToDef: map[types.Object]*xgoast.Ident{
			funcObj: funcDecl.Name,
		},
	}

	typ := rrc.resolveReturnType(retStmt.Results[0])
	require.NotNil(t, typ)
	assert.Same(t, SpriteNameType(), typ)
}

func TestResourceRefCollectorResolveSpriteContext(t *testing.T) {
	proj := newTestProject(map[string]string{
		"main.spx": `package main

func example() {
	hero.Play()
}
`,
	}, xgo.FeatASTCache)

	rrc := newResourceRefCollectorForTest(t, proj)
	callExpr := findSelectorCallExpr(t, proj, "main.spx", "hero", "Play")
	require.NotNil(t, callExpr)

	pkg := types.NewPackage("main", "main")
	heroObj := types.NewVar(token.NoPos, pkg, "Hero", SpriteType())
	rrc.spriteAutoBindings[heroObj] = struct{}{}

	selector := callExpr.Fun.(*xgoast.SelectorExpr)
	heroIdent := selector.X.(*xgoast.Ident)
	rrc.typeInfo.Info.Uses[heroIdent] = heroObj

	recv := types.NewVar(token.NoPos, pkg, "", SpriteType())
	sig := types.NewSignature(recv, types.NewTuple(), types.NewTuple(), false)
	rrc.typeInfo.Types[callExpr.Fun] = types.TypeAndValue{Type: sig}

	sprite := rrc.resolveSpriteContext(callExpr)
	require.NotNil(t, sprite)
	assert.Equal(t, "Hero", sprite.Name)
}

func TestResourceRefCollectorInspectResourceRefForType(t *testing.T) {
	t.Run("SpriteLiteral", func(t *testing.T) {
		rrc := newResourceRefCollectorForTest(t, nil)

		expr := newStringLiteral("Hero")
		setExprTypeValue(rrc, expr, SpriteNameType(), "Hero")

		rrc.inspectResourceRefForType(expr, SpriteNameType(), nil)

		require.Len(t, rrc.refs, 1)
		assert.Equal(t, SpriteResourceID{SpriteName: "Hero"}, rrc.refs[0].ID)
		assert.Equal(t, ResourceRefKindStringLiteral, rrc.refs[0].Kind)
		assert.Empty(t, rrc.diagnostics)
	})

	t.Run("ConstantReference", func(t *testing.T) {
		rrc := newResourceRefCollectorForTest(t, nil)

		ident := &xgoast.Ident{Name: "heroName"}
		setExprTypeValue(rrc, ident, SpriteNameType(), "Hero")

		rrc.inspectResourceRefForType(ident, SpriteNameType(), nil)

		require.Len(t, rrc.refs, 1)
		assert.Equal(t, ResourceRefKindConstantReference, rrc.refs[0].Kind)
	})

	t.Run("MissingSpriteDiagnostic", func(t *testing.T) {
		rrc := newResourceRefCollectorForTest(t, nil)

		expr := newStringLiteral("Ghost")
		setExprTypeValue(rrc, expr, SpriteNameType(), "Ghost")

		rrc.inspectResourceRefForType(expr, SpriteNameType(), nil)

		require.Len(t, rrc.diagnostics, 1)
		assert.Equal(t, "sprite resource \"Ghost\" not found", rrc.diagnostics[0].Msg)
	})

	t.Run("SpriteCostumeContext", func(t *testing.T) {
		rrc := newResourceRefCollectorForTest(t, nil)
		getContext := func() *SpriteResource {
			return rrc.resourceSet.Sprite("Hero")
		}

		expr := newStringLiteral("Idle")
		setExprTypeValue(rrc, expr, SpriteCostumeNameType(), "Idle")

		rrc.inspectResourceRefForType(expr, SpriteCostumeNameType(), getContext)

		require.Len(t, rrc.refs, 1)
		assert.Equal(t, SpriteCostumeResourceID{SpriteName: "Hero", CostumeName: "Idle"}, rrc.refs[0].ID)
		assert.Empty(t, rrc.diagnostics)
	})

	t.Run("EmptySpriteNameDiagnostic", func(t *testing.T) {
		rrc := newResourceRefCollectorForTest(t, nil)

		expr := newStringLiteral("")
		setExprTypeValue(rrc, expr, SpriteNameType(), "")

		rrc.inspectResourceRefForType(expr, SpriteNameType(), nil)

		require.Len(t, rrc.diagnostics, 1)
		assert.Equal(t, "sprite resource name cannot be empty", rrc.diagnostics[0].Msg)
	})

	t.Run("SpriteCostumeMissingDiagnostic", func(t *testing.T) {
		rrc := newResourceRefCollectorForTest(t, nil)
		getContext := func() *SpriteResource {
			return rrc.resourceSet.Sprite("Hero")
		}

		expr := newStringLiteral("Missing")
		setExprTypeValue(rrc, expr, SpriteCostumeNameType(), "Missing")

		rrc.inspectResourceRefForType(expr, SpriteCostumeNameType(), getContext)

		require.Len(t, rrc.diagnostics, 1)
		assert.Equal(t, "costume resource \"Missing\" not found in sprite \"Hero\"", rrc.diagnostics[0].Msg)
	})
}

func newResourceRefCollectorForTest(t *testing.T, proj *xgo.Project) *resourceRefCollector {
	t.Helper()

	if proj == nil {
		proj = newTestProject(defaultProjectFiles(), 0)
	}
	resourceProj := newTestProject(defaultProjectFiles(), 0)
	set, err := NewResourceSet(resourceProj, "assets")
	require.NoError(t, err)

	return &resourceRefCollector{
		proj: proj,
		typeInfo: &xgotypes.Info{
			Info: typesutil.Info{
				Types: make(map[xgoast.Expr]types.TypeAndValue),
				Uses:  make(map[*xgoast.Ident]types.Object),
				Defs:  make(map[*xgoast.Ident]types.Object),
			},
			ObjToDef: make(map[types.Object]*xgoast.Ident),
		},
		translate:          func(s string) string { return s },
		resourceSet:        set,
		spriteTypes:        make(map[types.Type]struct{}),
		spriteAutoBindings: make(map[types.Object]struct{}),
		seenRefs:           make(map[resourceRefKey]struct{}),
	}
}

func setExprTypeValue(rrc *resourceRefCollector, expr xgoast.Expr, typ types.Type, value string) {
	tv := types.TypeAndValue{Type: typ}
	if value != "" {
		tv.Value = constant.MakeString(value)
	}
	rrc.typeInfo.Types[expr] = tv
}

func newStringLiteral(v string) *xgoast.BasicLit {
	return &xgoast.BasicLit{
		Kind:     xgotoken.STRING,
		Value:    strconv.Quote(v),
		ValuePos: xgotoken.Pos(1),
	}
}

func mustASTFile(t *testing.T, proj *xgo.Project, filename string) *xgoast.File {
	t.Helper()

	astPkg, err := proj.ASTPackage()
	require.NoError(t, err)

	file := astPkg.Files[filename]
	require.NotNil(t, file, "AST file %q not found", filename)
	return file
}

func findFuncDecl(t *testing.T, file *xgoast.File, name string) *xgoast.FuncDecl {
	t.Helper()
	for _, decl := range file.Decls {
		if fn, ok := decl.(*xgoast.FuncDecl); ok && fn.Name.Name == name {
			return fn
		}
	}
	return nil
}

func findReturnStmt(fn *xgoast.FuncDecl) *xgoast.ReturnStmt {
	if fn == nil || fn.Body == nil {
		return nil
	}
	for _, stmt := range fn.Body.List {
		if ret, ok := stmt.(*xgoast.ReturnStmt); ok {
			return ret
		}
	}
	return nil
}

func findSelectorCallExpr(t *testing.T, proj *xgo.Project, filename, identName, selName string) *xgoast.CallExpr {
	t.Helper()
	file := mustASTFile(t, proj, filename)

	var callExpr *xgoast.CallExpr
	xgoast.Inspect(file, func(node xgoast.Node) bool {
		ce, ok := node.(*xgoast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := ce.Fun.(*xgoast.SelectorExpr)
		if !ok {
			return true
		}
		xIdent, _ := sel.X.(*xgoast.Ident)
		if xIdent != nil && xIdent.Name == identName && sel.Sel.Name == selName {
			callExpr = ce
			return false
		}
		return true
	})
	require.NotNil(t, callExpr, "selector call %s.%s not found", identName, selName)
	return callExpr
}

func findAssignIdent(t *testing.T, proj *xgo.Project, filename, name string) *xgoast.Ident {
	t.Helper()
	file := mustASTFile(t, proj, filename)

	var target *xgoast.Ident
	xgoast.Inspect(file, func(node xgoast.Node) bool {
		assign, ok := node.(*xgoast.AssignStmt)
		if !ok {
			return true
		}
		for _, lhs := range assign.Lhs {
			if ident, ok := lhs.(*xgoast.Ident); ok && ident.Name == name {
				target = ident
				return false
			}
		}
		return true
	})
	require.NotNil(t, target, "assign ident %q not found", name)
	return target
}

func TestResourceRefCollectorAddResourceRefDedup(t *testing.T) {
	rrc := newResourceRefCollectorForTest(t, nil)
	lit := newStringLiteral("Hero")
	ref := ResourceRef{
		ID:   SpriteResourceID{SpriteName: "Hero"},
		Kind: ResourceRefKindStringLiteral,
		Node: lit,
	}

	rrc.addResourceRef(ref)
	rrc.addResourceRef(ref)

	require.Len(t, rrc.refs, 1)
}

func TestResourceRefCollectorAddDiagnosticTranslate(t *testing.T) {
	rrc := newResourceRefCollectorForTest(t, nil)
	rrc.translate = func(s string) string {
		return "translated: " + s
	}

	rrc.addDiagnostic(nil, "missing resource")

	require.Len(t, rrc.diagnostics, 1)
	assert.Equal(t, "translated: missing resource", rrc.diagnostics[0].Msg)
}
