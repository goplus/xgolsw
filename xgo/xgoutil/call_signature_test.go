package xgoutil

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveCallExprSignature(t *testing.T) {
	for _, tt := range []struct{ name, source string }{
		{"Variable", "callback(1)"},
		{"Parenthesized", "(callback)(1)"},
		{"Field", "holder.Callback(1)"},
		{"Returned", "factory()(1)"},
		{"Indexed", "callbacks[0](1)"},
		{"Dereferenced", "(*callback)(1)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := gotypes.NewPackage("example.com/callbacks", "callbacks")
			param := gotypes.NewParam(token.NoPos, pkg, "value", gotypes.Typ[gotypes.Int])
			sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(param),
				gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", gotypes.Typ[gotypes.String])), false)
			typ := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "Callback", nil), sig, nil)
			expr, err := parser.ParseExpr(tt.source)
			require.NoError(t, err)
			call, ok := expr.(*ast.CallExpr)
			require.True(t, ok)
			info := newTestTypeInfo(nil, nil)
			info.Types[call.Fun] = gotypes.TypeAndValue{Type: typ}
			if ident := CallExprFunIdent(call); ident != nil {
				info.Uses[ident] = gotypes.NewVar(token.NoPos, pkg, ident.Name, typ)
			}
			fun, resolved, params := ResolveCallExprSignature(info, call)
			assert.Nil(t, fun)
			assert.Same(t, sig, resolved)
			assert.Same(t, sig.Params(), params)
			arg, ok := ResolveCallExprArg(info, call, call.Args[0])
			require.True(t, ok)
			assert.Nil(t, arg.Fun)
			assert.Same(t, param, arg.Param)
			assert.Same(t, sig, arg.Signature)
			assert.Equal(t, gotypes.Typ[gotypes.Int], arg.ExpectedType)
		})
	}

	for _, tt := range []struct {
		name, source string
		multiple     bool
	}{
		{"Generic", "generic[string](value)", false},
		{"MultipleTypeArguments", "generic[int, string](value)", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := gotypes.NewPackage("example.com/generic", "generic")
			expr, err := parser.ParseExpr(tt.source)
			require.NoError(t, err)
			call, ok := expr.(*ast.CallExpr)
			require.True(t, ok)
			typeParam := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "T", nil), newTestEmptyInterface())
			typeParams := []*gotypes.TypeParam{typeParam}
			typeArgs := []gotypes.Type{gotypes.Typ[gotypes.String]}
			if tt.multiple {
				keyParam := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "K", nil), newTestEmptyInterface())
				typeParams = append([]*gotypes.TypeParam{keyParam}, typeParams...)
				typeArgs = append([]gotypes.Type{gotypes.Typ[gotypes.Int]}, typeArgs...)
			}
			sig := gotypes.NewSignatureType(nil, nil, typeParams,
				gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "value", typeParam)), nil, false)
			fun := gotypes.NewFunc(token.NoPos, pkg, "generic", sig)
			actual, err := gotypes.Instantiate(nil, sig, typeArgs, true)
			require.NoError(t, err)
			info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{CallExprFunIdent(call): fun})
			info.Types[call.Fun] = gotypes.TypeAndValue{Type: actual}
			resolvedFun, resolved, params := ResolveCallExprSignature(info, call)
			assert.Same(t, fun, resolvedFun)
			assert.Same(t, actual, resolved)
			require.Equal(t, 1, params.Len())
			assert.Equal(t, gotypes.Typ[gotypes.String], params.At(0).Type())
		})
	}

	t.Run("FunctionTypeConversion", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/callbacks", "callbacks")
		sig := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		typ := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "Callback", nil), sig, nil)
		ident := &ast.Ident{Name: "Callback"}
		info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: typ.Obj()})
		info.Types[ident] = gotypes.TypeAndValue{Type: typ}
		fun, resolved, params := ResolveCallExprSignature(info, &ast.CallExpr{Fun: ident})
		assert.Nil(t, fun)
		assert.Nil(t, resolved)
		assert.Nil(t, params)
	})

	t.Run("PromotedMethodExpression", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/methods", "methods")
		base := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "Base", nil), gotypes.NewStruct(nil, nil), nil)
		outer := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "Outer", nil),
			gotypes.NewStruct([]*gotypes.Var{gotypes.NewField(token.NoPos, pkg, "Base", base, true)}, nil), nil)
		sig := gotypes.NewSignatureType(gotypes.NewVar(token.NoPos, pkg, "recv", base), nil, nil,
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "value", gotypes.Typ[gotypes.Int])), nil, false)
		method := gotypes.NewFunc(token.NoPos, pkg, "Accept", sig)
		base.AddMethod(method)
		receiver := &ast.Ident{Name: "Outer"}
		selector := &ast.SelectorExpr{X: receiver, Sel: &ast.Ident{Name: "Accept"}}
		info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{receiver: outer.Obj(), selector.Sel: method})
		info.Types[receiver] = gotypes.TypeAndValue{Type: outer}
		// XGo can retain the declaring method signature on the selector.
		info.Types[selector] = gotypes.TypeAndValue{Type: sig}
		fun, resolved, params := ResolveCallExprSignature(info, &ast.CallExpr{Fun: selector})
		assert.Same(t, method, fun)
		assert.Same(t, sig, resolved)
		require.Equal(t, 2, params.Len())
		assert.Same(t, outer, params.At(0).Type())
		assert.Equal(t, gotypes.Typ[gotypes.Int], params.At(1).Type())
	})
}

func TestCallExprArgs(t *testing.T) {
	for _, tt := range []struct {
		name                            string
		tupleParam, decorator, ellipsis bool
		wantExpanded                    bool
	}{
		{name: "Expanded", wantExpanded: true},
		{name: "TupleParameter", tupleParam: true},
		{name: "Decorator", decorator: true},
		{name: "TupleEllipsis", ellipsis: true, wantExpanded: true},
		{name: "TupleParameterEllipsis", tupleParam: true, ellipsis: true, wantExpanded: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := gotypes.NewPackage("example.com/tuple", "tuple")
			intType, stringType := gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.String]
			params := gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "first", intType), gotypes.NewParam(token.NoPos, pkg, "second", stringType))
			if tt.tupleParam {
				tupleType := gotypes.NewStruct([]*gotypes.Var{
					gotypes.NewField(token.NoPos, pkg, "X_0", intType, false),
					gotypes.NewField(token.NoPos, pkg, "X_1", stringType, false),
				}, nil)
				params = gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "pair", tupleType))
			}
			tuple := &ast.TupleLit{Elts: []ast.Expr{&ast.Ident{Name: "number"}, &ast.Ident{Name: "text"}}}
			if tt.ellipsis {
				tuple.Ellipsis = token.Pos(1)
			}
			call := &ast.CallExpr{Args: []ast.Expr{tuple}}
			info := newTestTypeInfo(nil, nil)
			info.FuncDecorators = map[*ast.CallExpr]bool{call: tt.decorator}
			args, ellipsis := CallExprArgs(info, call, params)
			if tt.wantExpanded {
				assert.Equal(t, tuple.Elts, args)
			} else {
				assert.Equal(t, call.Args, args)
			}
			assert.Equal(t, tt.ellipsis && tt.wantExpanded, ellipsis)
		})
	}
}

func TestResolveCallExprArg(t *testing.T) {
	t.Run("TupleElement", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/tuple", "tuple")
		fun := newTestFunc(pkg, "pair", false,
			gotypes.NewParam(token.NoPos, pkg, "first", gotypes.Typ[gotypes.Int]),
			gotypes.NewParam(token.NoPos, pkg, "second", gotypes.Typ[gotypes.String]))
		expr, err := parser.ParseExpr("pair((number, text))")
		require.NoError(t, err)
		call, ok := expr.(*ast.CallExpr)
		require.True(t, ok)
		tuple, ok := call.Args[0].(*ast.TupleLit)
		require.True(t, ok)
		info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{CallExprFunIdent(call): fun})
		arg, ok := ResolveCallExprArg(info, call, tuple.Elts[1])
		require.True(t, ok)
		assert.Equal(t, 1, arg.ArgIndex)
		assert.Equal(t, 1, arg.ParamIndex)
		assert.Equal(t, gotypes.Typ[gotypes.String], arg.ExpectedType)
		_, ok = ResolveCallExprArg(info, call, call.Fun)
		assert.False(t, ok)
	})

	t.Run("Keyword", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/kwargs", "kwargs")
		fun := newTestFunc(pkg, "configure", false,
			gotypes.NewParam(token.NoPos, pkg, "opts", gotypes.NewMap(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Int])))
		ident := &ast.Ident{Name: "configure"}
		call := &ast.CallExpr{Fun: ident, Kwargs: []*ast.KwargExpr{newTestKwarg("first", "one"), newTestKwarg("second", "two")}}
		info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		arg, ok := ResolveCallExprArg(info, call, call.Kwargs[1].Value)
		require.True(t, ok)
		assert.Equal(t, 1, arg.ArgIndex)
		assert.Same(t, call.Kwargs[1], arg.Kwarg)
		assert.Equal(t, gotypes.Typ[gotypes.Int], arg.ExpectedType)
	})
}

func TestResolvedCallExprArgsForSignature(t *testing.T) {
	pkg := gotypes.NewPackage("example.com/overloads", "overloads")
	selected := newTestFunc(pkg, "use", false, gotypes.NewParam(token.NoPos, pkg, "value", gotypes.Typ[gotypes.String]))
	candidate := newTestFunc(pkg, "useInt", false, gotypes.NewParam(token.NoPos, pkg, "value", gotypes.Typ[gotypes.Int]))
	ident := &ast.Ident{Name: "use"}
	call := &ast.CallExpr{Fun: ident, Args: []ast.Expr{&ast.Ident{Name: "value"}}}
	info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: selected})
	info.Types[ident] = gotypes.TypeAndValue{Type: selected.Signature()}
	sig, params := ResolveFuncSignatureForCall(info, call, candidate)
	count := 0
	for arg := range ResolvedCallExprArgsForSignature(info, call, candidate, sig, params) {
		count++
		assert.Same(t, candidate, arg.Fun)
		assert.Same(t, sig, arg.Signature)
		assert.Same(t, params, arg.Params)
		assert.Equal(t, gotypes.Typ[gotypes.Int], arg.ExpectedType)
		assert.Same(t, call.Args[0], arg.Arg)
	}
	assert.Equal(t, 1, count)
}
