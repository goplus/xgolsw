package server

import (
	gotypes "go/types"
	"slices"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/protocol"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletion(t *testing.T) {
	t.Run("MemberAccessAtLineStart", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			position Position
		}{
			{name: "ProjectEOF", filename: "main_fixture.gox", position: Position{Line: 1, Character: 9}},
			{name: "WorkCallback", filename: "Worker_fixture.gox", position: Position{Line: 1, Character: 10}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newFrameworkTestServer(t, map[string][]byte{
					"main_fixture.gox": []byte("var worker *Worker\nworker.ap"), // Cursor at EOF.
					"Worker_fixture.gox": []byte(`onValue value => {
	worker.ap
}
`),
				})
				items := completionItemsAt(t, s, tt.filename, tt.position)
				assert.Contains(t, completionItemLabels(items), "apply")
			})
		}
	})

	for _, sourceKind := range []struct {
		name         string
		filename     string
		needsProject bool
		newServer    testServerFactory
	}{
		{name: "XGo", filename: "main.xgo", newServer: newTestServer},
		{name: "ProjectClass", filename: "main_fixture.gox", newServer: newFrameworkTestServer},
		{name: "WorkClass", filename: "Worker_fixture.gox", needsProject: true, newServer: newFrameworkTestServer},
	} {
		t.Run(sourceKind.name, func(t *testing.T) {
			t.Run("FuncDecoratorArgument", func(t *testing.T) {
				files := map[string][]byte{
					sourceKind.filename: []byte(`const (
	count   = 1
	comment = "text"
)

func retry(times int, fn func()) {
	fn()
}

@retry(co)
func run() {
}
`),
				}
				if sourceKind.needsProject {
					files["main_fixture.gox"] = nil
				}
				s := sourceKind.newServer(t, files)

				items := completionItemsAt(t, s, sourceKind.filename, Position{Line: 9, Character: 9})
				labels := completionItemLabels(items)
				assert.Contains(t, labels, "count")
				assert.NotContains(t, labels, "comment")
			})

			t.Run("FuncDecoratorImplicitArgument", func(t *testing.T) {
				files := map[string][]byte{
					sourceKind.filename: []byte(`const (
	count   = 1
	comment = "text"
)

func retry(times int, fn func()) {
	fn()
}

@retry(1, co)
func run() {
}
`),
				}
				if sourceKind.needsProject {
					files["main_fixture.gox"] = nil
				}
				s := sourceKind.newServer(t, files)

				items := completionItemsAt(t, s, sourceKind.filename, Position{Line: 9, Character: 12})
				labels := completionItemLabels(items)
				assert.Contains(t, labels, "count")
				assert.Contains(t, labels, "comment")
			})

			t.Run("NestedFuncDecoratorArgument", func(t *testing.T) {
				files := map[string][]byte{
					sourceKind.filename: []byte(`const (
	count   = 1
	comment = "text"
)

func retry(times int, fn func()) {
	fn()
}

func parse(text string) int {
	return len(text)
}

@retry(parse(co))
func run() {
}
`),
				}
				if sourceKind.needsProject {
					files["main_fixture.gox"] = nil
				}
				s := sourceKind.newServer(t, files)

				items := completionItemsAt(t, s, sourceKind.filename, Position{Line: 13, Character: 15})
				labels := completionItemLabels(items)
				assert.NotContains(t, labels, "count")
				assert.Contains(t, labels, "comment")
			})

			t.Run("NestedCallArgument", func(t *testing.T) {
				files := map[string][]byte{
					sourceKind.filename: []byte(`const (
	count   = 1
	comment = "text"
)

func consume(value int) {
}

func parse(text string) int {
	return len(text)
}

func run() {
	consume(parse(co))
}
`),
				}
				if sourceKind.needsProject {
					files["main_fixture.gox"] = nil
				}
				s := sourceKind.newServer(t, files)

				items := completionItemsAt(t, s, sourceKind.filename, Position{Line: 13, Character: 17})
				labels := completionItemLabels(items)
				assert.NotContains(t, labels, "count")
				assert.Contains(t, labels, "comment")
			})

			t.Run("PartialXGoxFunction", func(t *testing.T) {
				files := map[string][]byte{
					sourceKind.filename: []byte(`import "example.com/typeargs"

const (
	count   = 1
	comment = "text"
)

func run() {
	typeargs.convert(string, count)
}
`),
				}
				if sourceKind.needsProject {
					files["main_fixture.gox"] = nil
				}
				s := sourceKind.newServer(t, files)
				s.workspaceRootFS.Importer = xgoxTestImporter{fallback: s.workspaceRootFS.Importer}

				items := completionItemsAt(t, s, sourceKind.filename, Position{Line: 8, Character: 31})
				labels := completionItemLabels(items)
				assert.Contains(t, labels, "count")
				assert.NotContains(t, labels, "comment")
			})
		})
	}

	t.Run("IncompleteMapLiteralInCall", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
println {"key": }
`),
		})

		completionItemsAt(t, s, "main.xgo", Position{Line: 1, Character: 15})
	})

	t.Run("NoCompletionAfterNumberLiteral", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func run() {
	var x = 123.
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 2, Character: 13}) // After "123."
		assert.Empty(t, items)
	})

	t.Run("NoCompletionAfterNumberLiteralInShortVarDecl", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func run() {
	x := 123.
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 2, Character: 10}) // After "123."
		assert.Empty(t, items)
	})

	t.Run("Autoclosure", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`const (
	enabled = true
	entry = "text"
)

onStart => {

	runWhen e, => {}
}
`),
		})

		signatureItems := completionItemsAt(t, s, "main_fixture.gox", Position{Line: 6, Character: 1})
		runWhenItem := completionItemByLabel(signatureItems, "runWhen")
		require.NotNilf(t, runWhenItem, "%v", completionItemLabels(signatureItems))
		require.NotNil(t, runWhenItem.Documentation)
		documentation := requireValueAs[MarkupContent](t, runWhenItem.Documentation.Value)
		assert.Contains(t, documentation.Value, `overview="func runWhen(condition bool, callback func())"`)

		argumentItems := completionItemsAt(t, s, "main_fixture.gox", Position{Line: 7, Character: 10})
		argumentLabels := completionItemLabels(argumentItems)
		assert.Contains(t, argumentLabels, "enabled")
		assert.NotContains(t, argumentLabels, "entry")
	})

	t.Run("GeneralOrUnknown", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`

onStart => {

}
`),
		})

		items1 := completionItemsAt(t, s, "main_fixture.gox", Position{Line: 1, Character: 1})
		assert.NotEmpty(t, items1)
		assert.Contains(t, completionItemLabels(items1), "len")

		items2 := completionItemsAt(t, s, "main_fixture.gox", Position{Line: 2, Character: 12})
		assert.Empty(t, items2)

		items3 := completionItemsAt(t, s, "main_fixture.gox", Position{Line: 3, Character: 1})
		assert.NotEmpty(t, items3)
		assert.Contains(t, completionItemLabels(items3), "len")
	})

	t.Run("VarDecl", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`
func test() {}
onStart => {
	var x i
}
`),
			"Worker_fixture.gox": []byte(`
`),
		})

		items := completionItemsAt(t, s, "main_fixture.gox", Position{Line: 3, Character: 8})
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "int")
		assert.Contains(t, labels, "Worker")
		assert.Contains(t, labels, "Item")
		assert.NotContains(t, labels, "len")
		assert.NotContains(t, labels, "test")
		assert.NotContains(t, labels, "runWhen")
	})

	t.Run("AtLineStartWithAnIdentifier", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`
onStart => {
	pr
}
`),
		})

		items := completionItemsAt(t, s, "main_fixture.gox", Position{Line: 2, Character: 3})
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "println")
	})

	t.Run("WithXGoBuiltins", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`
onStart => {
	var n in
}
`),
			"Worker_fixture.gox": []byte(`
onValue value => {
	ec
}
`),
		})

		items1 := completionItemsAt(t, s, "main_fixture.gox", Position{Line: 2, Character: 9})
		assert.NotEmpty(t, items1)
		assert.Contains(t, completionItemLabels(items1), "int128")

		items2 := completionItemsAt(t, s, "Worker_fixture.gox", Position{Line: 2, Character: 3})
		assert.NotEmpty(t, items2)
		assert.Contains(t, completionItemLabels(items2), "echo")
	})

	t.Run("UnresolvedFuncCall", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`
onStar => {
}
`),
		})

		items := completionItemsAt(t, s, "main_fixture.gox", Position{Line: 1, Character: 6})
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "onStart")
	})
}

func TestCompletionContextResolvePropertyLikeExprType(t *testing.T) {
	t.Run("NilIdentifierReturnsNil", func(t *testing.T) {
		ctx := newPropertyLikeTestCompletionContext(gotypes.NewPackage("main", "main"), nil, nil)

		assert.Nil(t, ctx.resolvePropertyLikeExprType(nil, nil))
		assert.Nil(t, ctx.resolvePropertyLikeExprType(&ast.Ident{}, nil))
	})

	t.Run("SignatureMatch", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(1), pkg, "Now", gotypes.Typ[gotypes.String])
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		got := ctx.resolvePropertyLikeExprType(ident, fun.Type())
		assert.Same(t, gotypes.Typ[gotypes.String], got)
	})

	t.Run("ValidNonPropertyLikeSignatureReturnsNil", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(1), pkg, "now", gotypes.Typ[gotypes.String])
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		got := ctx.resolvePropertyLikeExprType(ident, fun.Type())
		assert.Nil(t, got)
	})

	t.Run("ValidTypeWithoutResolvedObjectReturnsNil", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(1), pkg, "Now", gotypes.Typ[gotypes.String])
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), nil)

		got := ctx.resolvePropertyLikeExprType(ident, fun.Type())
		assert.Nil(t, got)
	})

	t.Run("InvalidTypeFallsBackToScopeWalk", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(20), pkg, "Now", gotypes.Typ[gotypes.String])
		pkg.Scope().Insert(fun)
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), nil)

		got := ctx.resolvePropertyLikeExprType(ident, nil)
		assert.Same(t, gotypes.Typ[gotypes.String], got)
	})
}

func TestCompletionContextResolvePropertyLikeFuncResultType(t *testing.T) {
	t.Run("NilIdentifierReturnsNil", func(t *testing.T) {
		ctx := newPropertyLikeTestCompletionContext(gotypes.NewPackage("main", "main"), nil, nil)

		assert.Nil(t, ctx.resolvePropertyLikeFuncResultType(nil))
		assert.Nil(t, ctx.resolvePropertyLikeFuncResultType(&ast.Ident{}))
	})

	t.Run("PackageScopeIgnoresDeclarationOrder", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(20), pkg, "Now", gotypes.Typ[gotypes.String])
		pkg.Scope().Insert(fun)
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), nil)

		got := ctx.resolvePropertyLikeFuncResultType(ident)
		assert.Same(t, gotypes.Typ[gotypes.String], got)
	})

	t.Run("LocalScopeSkipsLaterFunction", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		localScope := gotypes.NewScope(pkg.Scope(), token.NoPos, token.NoPos, "local")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(20), pkg, "Now", gotypes.Typ[gotypes.String])
		localScope.Insert(fun)
		ctx := newPropertyLikeTestCompletionContext(pkg, localScope, nil)

		got := ctx.resolvePropertyLikeFuncResultType(ident)
		assert.Nil(t, got)
	})

	t.Run("SkipsFunctionWithParams", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		sig := gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewVar(token.NoPos, nil, "v", gotypes.Typ[gotypes.String])),
			gotypes.NewTuple(gotypes.NewVar(token.NoPos, nil, "", gotypes.Typ[gotypes.String])),
			false,
		)
		fun := gotypes.NewFunc(token.Pos(1), pkg, "Now", sig)
		pkg.Scope().Insert(fun)
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), nil)

		got := ctx.resolvePropertyLikeFuncResultType(ident)
		assert.Nil(t, got)
	})

	t.Run("SkipsLowerCamelFunctionName", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		ident := &ast.Ident{Name: "now", NamePos: 10}
		fun := newPropertyLikeTestFunc(token.Pos(1), pkg, "now", gotypes.Typ[gotypes.String])
		pkg.Scope().Insert(fun)
		ctx := newPropertyLikeTestCompletionContext(pkg, pkg.Scope(), nil)

		got := ctx.resolvePropertyLikeFuncResultType(ident)
		assert.Nil(t, got)
	})
}

func TestAdaptCompletionItemsForClient(t *testing.T) {
	for _, tt := range []struct {
		name         string
		capabilities CompletionClientCapabilities
		items        []CompletionItem
		want         []CompletionItem
	}{
		{
			name: "DowngradesUnsupportedSnippetAndKind",
			items: []CompletionItem{{
				Label:            "count",
				Kind:             ConstantCompletion,
				InsertText:       "count = ${1:}",
				InsertTextFormat: ToPtr(SnippetTextFormat),
				TextEdit: &Or_CompletionItem_textEdit{Value: TextEdit{
					Range: Range{
						Start: Position{Line: 1, Character: 2},
						End:   Position{Line: 1, Character: 4},
					},
					NewText: "count = ${1:}",
				}},
			}},
			want: []CompletionItem{{
				Label:            "count",
				Kind:             TextCompletion,
				InsertText:       "count",
				InsertTextFormat: ToPtr(PlainTextTextFormat),
				TextEdit: &Or_CompletionItem_textEdit{Value: TextEdit{
					Range: Range{
						Start: Position{Line: 1, Character: 2},
						End:   Position{Line: 1, Character: 4},
					},
					NewText: "count",
				}},
			}},
		},
		{
			name: "KeepsSnippetAndKindWithValueSet",
			capabilities: CompletionClientCapabilities{
				CompletionItem: protocol.ClientCompletionItemOptions{
					SnippetSupport: true,
				},
				CompletionItemKind: &protocol.ClientCompletionItemOptionsKind{
					ValueSet: []CompletionItemKind{TextCompletion},
				},
			},
			items: []CompletionItem{{
				Label:            "count",
				Kind:             ConstantCompletion,
				InsertText:       "count = ${1:}",
				InsertTextFormat: ToPtr(SnippetTextFormat),
			}},
			want: []CompletionItem{{
				Label:            "count",
				Kind:             ConstantCompletion,
				InsertText:       "count = ${1:}",
				InsertTextFormat: ToPtr(SnippetTextFormat),
			}},
		},
		{
			name: "DowngradesUnsupportedSnippetInsertReplaceEdit",
			items: []CompletionItem{{
				Label:            "move",
				Kind:             FunctionCompletion,
				InsertTextFormat: ToPtr(SnippetTextFormat),
				TextEdit: &Or_CompletionItem_textEdit{Value: InsertReplaceEdit{
					NewText: "move ${1:steps}",
					Insert:  Range{Start: Position{Line: 1, Character: 2}, End: Position{Line: 1, Character: 4}},
					Replace: Range{
						Start: Position{Line: 1, Character: 2},
						End:   Position{Line: 1, Character: 6},
					},
				}},
			}},
			want: []CompletionItem{{
				Label:            "move",
				Kind:             FunctionCompletion,
				InsertText:       "move",
				InsertTextFormat: ToPtr(PlainTextTextFormat),
				TextEdit: &Or_CompletionItem_textEdit{Value: InsertReplaceEdit{
					NewText: "move",
					Insert:  Range{Start: Position{Line: 1, Character: 2}, End: Position{Line: 1, Character: 4}},
					Replace: Range{
						Start: Position{Line: 1, Character: 2},
						End:   Position{Line: 1, Character: 6},
					},
				}},
			}},
		},
		{
			name: "KeepsInitialProtocolKindWithoutValueSet",
			items: []CompletionItem{{
				Label: "ref",
				Kind:  ReferenceCompletion,
			}},
			want: []CompletionItem{{
				Label: "ref",
				Kind:  ReferenceCompletion,
			}},
		},
		{
			name: "DowngradesEnumMemberForInitialProtocolClient",
			items: []CompletionItem{
				{Label: "Color", Kind: EnumCompletion},
				{Label: "Red", Kind: EnumMemberCompletion},
			},
			want: []CompletionItem{
				{Label: "Color", Kind: EnumCompletion},
				{Label: "Red", Kind: TextCompletion},
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			adaptCompletionItemsForClient(tt.capabilities, tt.items)
			assert.Equal(t, tt.want, tt.items)
		})
	}
}

func newPropertyLikeTestCompletionContext(pkg *gotypes.Package, innermostScope *gotypes.Scope, uses map[*ast.Ident]gotypes.Object) *completionContext {
	if uses == nil {
		uses = make(map[*ast.Ident]gotypes.Object)
	}
	return &completionContext{
		typeInfo: &types.Info{
			Info: typesutil.Info{
				Types:      make(map[ast.Expr]gotypes.TypeAndValue),
				Defs:       make(map[*ast.Ident]gotypes.Object),
				Uses:       uses,
				Selections: make(map[*ast.SelectorExpr]*gotypes.Selection),
				Implicits:  make(map[ast.Node]gotypes.Object),
				Scopes:     make(map[ast.Node]*gotypes.Scope),
			},
			Pkg: pkg,
		},
		innermostScope: innermostScope,
	}
}

func newPropertyLikeTestFunc(pos token.Pos, pkg *gotypes.Package, name string, result gotypes.Type) *gotypes.Func {
	sig := gotypes.NewSignatureType(
		nil,
		nil,
		nil,
		nil,
		gotypes.NewTuple(gotypes.NewVar(token.NoPos, nil, "", result)),
		false,
	)
	return gotypes.NewFunc(pos, pkg, name, sig)
}

func containsCompletionItemLabel(items []CompletionItem, label string) bool {
	return slices.ContainsFunc(items, func(item CompletionItem) bool {
		return item.Label == label
	})
}

func countCompletionItemLabel(items []CompletionItem, label string) int {
	count := 0
	for _, item := range items {
		if item.Label == label {
			count++
		}
	}
	return count
}

func containsKwargCompletionItem(items []CompletionItem, label string, id SpxDefinitionIdentifier) bool {
	return slices.ContainsFunc(items, func(item CompletionItem) bool {
		if item.Label != label ||
			item.InsertText != label+" = ${1:}" ||
			item.InsertTextFormat == nil ||
			*item.InsertTextFormat != SnippetTextFormat {
			return false
		}
		itemData, ok := item.Data.(*CompletionItemData)
		if !ok {
			return false
		}
		return itemData.Definition.String() == id.String()
	})
}

func containsCompletionSpxDefinitionID(items []CompletionItem, id SpxDefinitionIdentifier) bool {
	return slices.ContainsFunc(items, func(item CompletionItem) bool {
		itemData, ok := item.Data.(*CompletionItemData)
		if !ok {
			return false
		}
		return itemData.Definition.String() == id.String()
	})
}
