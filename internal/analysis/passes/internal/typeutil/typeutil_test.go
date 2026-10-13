package typeutil

import (
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallee(t *testing.T) {
	for _, tt := range []struct {
		name         string
		declarations string
		call         string
		wantName     string
		wantStatic   bool
	}{
		{
			name: "Function", declarations: "func get() int { return 1 }",
			call: "get()", wantName: "get", wantStatic: true,
		},
		{
			name: "ParenthesizedFunction", declarations: "func get() int { return 1 }",
			call: "((get))()", wantName: "get", wantStatic: true,
		},
		{
			name: "FunctionVariable", declarations: "var get func() int",
			call: "get()", wantName: "get",
		},
		{
			name: "Method", declarations: "type Worker struct{}\nfunc (w Worker) Get() int { return 1 }\nvar worker Worker",
			call: "worker.Get()", wantName: "Get", wantStatic: true,
		},
		{
			name: "MethodExpression", declarations: "type Worker struct{}\nfunc (w Worker) Get() int { return 1 }\nvar worker Worker",
			call: "Worker.Get(worker)", wantName: "Get", wantStatic: true,
		},
		{
			name: "InterfaceMethod", declarations: "type Worker interface { Get() int }\nvar worker Worker",
			call: "worker.Get()", wantName: "Get",
		},
		{
			name: "FunctionField", declarations: "type Worker struct { Get func() int }\nvar worker Worker",
			call: "worker.Get()", wantName: "Get",
		},
		{
			name: "NamedConversion", declarations: "type Count int",
			call: "Count(1)",
		},
		{
			name: "BuiltinConversion", call: "string(65)",
		},
		{
			name: "IndexedFunction", declarations: "var getters []func() int",
			call: "getters[0]()",
		},
		{
			name: "FunctionLiteral", call: "func() int { return 1 }()",
		},
		{
			name: "FunctionResult", declarations: "func factory() func() int { return nil }",
			call: "factory()()",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			prefix := "package test\n" + tt.declarations + "\nfunc run() { _ = "
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.xgo", prefix+tt.call+" }\n", 0)
			require.NoError(t, err)
			info := &types.Info{Info: typesutil.Info{
				Types:      make(map[ast.Expr]gotypes.TypeAndValue),
				Defs:       make(map[*ast.Ident]gotypes.Object),
				Uses:       make(map[*ast.Ident]gotypes.Object),
				Selections: make(map[*ast.SelectorExpr]*gotypes.Selection),
			}}
			checker := typesutil.NewChecker(&gotypes.Config{}, &typesutil.Config{
				Fset: fset, Types: gotypes.NewPackage("test", "test"),
			}, nil, &info.Info)
			require.NoError(t, checker.Files(nil, []*ast.File{file}))

			var call *ast.CallExpr
			ast.Inspect(file, func(n ast.Node) bool {
				if candidate, ok := n.(*ast.CallExpr); ok && fset.Position(candidate.Pos()).Offset == len(prefix) {
					call = candidate
					return false
				}
				return true
			})
			require.NotNil(t, call)
			got := Callee(info, call)
			if tt.wantName == "" {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, tt.wantName, got.Name())
			}
			if tt.wantStatic {
				assert.Same(t, got, StaticCallee(info, call))
			} else {
				assert.Nil(t, StaticCallee(info, call))
			}
		})
	}

	t.Run("Builtin", func(t *testing.T) {
		ident := &ast.Ident{Name: "len"}
		builtin := gotypes.Universe.Lookup("len")
		info := &types.Info{Info: typesutil.Info{Uses: map[*ast.Ident]gotypes.Object{ident: builtin}}}
		call := &ast.CallExpr{Fun: ident}
		assert.Same(t, builtin, Callee(info, call))
		assert.Nil(t, StaticCallee(info, call))
	})

	t.Run("QualifiedFunction", func(t *testing.T) {
		ident := &ast.Ident{Name: "Get"}
		function := gotypes.NewFunc(token.NoPos, gotypes.NewPackage("example.com/lib", "lib"), "Get",
			gotypes.NewSignatureType(nil, nil, nil, nil, nil, false))
		info := &types.Info{Info: typesutil.Info{Uses: map[*ast.Ident]gotypes.Object{ident: function}}}
		call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: &ast.Ident{Name: "lib"}, Sel: ident}}
		assert.Same(t, function, Callee(info, call))
		assert.Same(t, function, StaticCallee(info, call))
	})

	t.Run("UnresolvedIdentifier", func(t *testing.T) {
		call := &ast.CallExpr{Fun: &ast.Ident{Name: "missing"}}
		assert.Nil(t, Callee(&types.Info{}, call))
		assert.Nil(t, StaticCallee(&types.Info{}, call))
	})

	for _, tt := range []struct {
		name       string
		expression string
		typeNames  []string
	}{
		{name: "InstantiatedFunction", expression: "get[int](1)", typeNames: []string{"T"}},
		{name: "InstantiatedFunctionWithTwoTypes", expression: `get[int, string](1, "other")`, typeNames: []string{"T", "U"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tt.expression)
			require.NoError(t, err)
			call, ok := expr.(*ast.CallExpr)
			require.True(t, ok)
			x, _, _, _ := UnpackIndexExpr(call.Fun)
			ident, ok := x.(*ast.Ident)
			require.True(t, ok)
			var typeParams []*gotypes.TypeParam
			var params []*gotypes.Var
			for _, name := range tt.typeNames {
				param := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, nil, name, nil), gotypes.Universe.Lookup("any").Type())
				typeParams = append(typeParams, param)
				params = append(params, gotypes.NewParam(token.NoPos, nil, "", param))
			}
			signature := gotypes.NewSignatureType(nil, nil, typeParams, gotypes.NewTuple(params...), nil, false)
			function := gotypes.NewFunc(token.NoPos, nil, "get", signature)
			info := &types.Info{Info: typesutil.Info{Uses: map[*ast.Ident]gotypes.Object{ident: function}}}
			assert.Same(t, function, Callee(info, call))
			assert.Same(t, function, StaticCallee(info, call))
		})
	}
}

func TestUnpackIndexExpr(t *testing.T) {
	for _, tt := range []struct {
		name       string
		source     string
		wantCount  int
		wantTarget string
	}{
		{name: "SingleIndex", source: "get[int]", wantCount: 1, wantTarget: "get"},
		{name: "MultipleIndices", source: "get[int, string]", wantCount: 2, wantTarget: "get"},
		{name: "NotAnIndex", source: "get"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			expr, err := parser.ParseExprFrom(fset, "test.xgo", tt.source, 0)
			require.NoError(t, err)
			x, lbrack, indices, rbrack := UnpackIndexExpr(expr)
			if tt.wantCount == 0 {
				assert.Nil(t, x)
				assert.Equal(t, token.NoPos, lbrack)
				assert.Nil(t, indices)
				assert.Equal(t, token.NoPos, rbrack)
				return
			}
			ident, ok := x.(*ast.Ident)
			require.True(t, ok)
			assert.Equal(t, tt.wantTarget, ident.Name)
			assert.Len(t, indices, tt.wantCount)
			assert.Equal(t, strings.Index(tt.source, "["), fset.Position(lbrack).Offset)
			assert.Equal(t, strings.Index(tt.source, "]"), fset.Position(rbrack).Offset)
		})
	}
}
