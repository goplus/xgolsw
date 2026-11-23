package xgoutil

import (
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"testing"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallExprFun(t *testing.T) {
	for _, tt := range []struct {
		name    string
		command bool
	}{
		{name: "Command", command: true},
		{name: "Call"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ident := ast.NewIdent("Save")
			callee := &ast.IndexListExpr{
				X:       &ast.SelectorExpr{X: ast.NewIdent("store"), Sel: ident},
				Indices: []ast.Expr{ast.NewIdent("int"), ast.NewIdent("string")},
			}
			wrap := &ast.ErrWrapExpr{X: callee, Tok: token.NOT}
			call := &ast.CallExpr{Fun: wrap}
			if tt.command {
				call.NoParenEnd = token.Pos(1)
				assert.Same(t, callee, CallExprFun(call))
				assert.Same(t, ident, CallExprFunIdent(call))
			} else {
				assert.Same(t, wrap, CallExprFun(call))
				assert.Nil(t, CallExprFunIdent(call))
			}
		})
	}
}

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
				var variableType gotypes.Type = typ
				_, indexed := call.Fun.(*ast.IndexExpr)
				if indexed {
					variableType = gotypes.NewSlice(typ)
				}
				variable := gotypes.NewVar(token.NoPos, pkg, ident.Name, variableType)
				info.Uses[ident] = variable
				if indexed {
					assert.Nil(t, CallExprImplementation(info, call))
				} else {
					assert.Same(t, variable, CallExprImplementation(info, call))
				}
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

	t.Run("PointerMethodExpressionWithValueReceiver", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/methods", "methods")
		typ := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "Record", nil), gotypes.NewStruct(nil, nil), nil)
		receiver := &ast.Ident{Name: "Record"}
		selector := &ast.SelectorExpr{X: &ast.ParenExpr{X: &ast.StarExpr{X: receiver}}, Sel: &ast.Ident{Name: "Read"}}
		method := gotypes.NewFunc(token.NoPos, pkg, "Read",
			gotypes.NewSignatureType(gotypes.NewVar(token.NoPos, pkg, "", typ), nil, nil, nil, nil, false))
		typ.AddMethod(method)
		info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{receiver: typ.Obj(), selector.Sel: method})
		info.Types[selector.X] = gotypes.TypeAndValue{Type: gotypes.NewPointer(typ)}
		info.Types[selector] = gotypes.TypeAndValue{Type: method.Signature()}

		fun, sig, params := ResolveCallExprSignature(info, &ast.CallExpr{Fun: selector})
		assert.Nil(t, fun)
		assert.Nil(t, sig)
		assert.Nil(t, params)
	})
}

func TestResolveCallExprSignatureForVariableOverloads(t *testing.T) {
	for _, tt := range []struct {
		name               string
		multiple, selected bool
		wantSignature      bool
	}{
		{name: "SingleCandidate", wantSignature: true},
		{name: "UnselectedMultipleCandidates", multiple: true},
		{name: "SelectedMultipleCandidates", multiple: true, selected: true, wantSignature: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := gotypes.NewPackage("example.com/callbacks", "callbacks")
			param := gotypes.NewParam(token.NoPos, pkg, "value", gotypes.Typ[gotypes.Int])
			sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(param), nil, false)
			variable := gotypes.NewVar(token.NoPos, pkg, "ReadValue", sig)
			candidates := []gotypes.Object{variable}
			if tt.multiple {
				other := gotypes.NewParam(token.NoPos, pkg, "text", gotypes.Typ[gotypes.String])
				candidates = append(candidates, newTestFunc(pkg, "ReadText", false, other))
			}
			group := gogen.NewOverloadFunc(token.NoPos, pkg, "Read", candidates...)
			var selected gotypes.Object = group
			if tt.selected {
				selected = variable
			}
			ident := &ast.Ident{Name: "read"}
			info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: selected})
			info.Overloads = map[*ast.Ident]gotypes.Object{ident: group}
			call := &ast.CallExpr{Fun: &ast.ParenExpr{X: ident}}

			fun, resolved, params := ResolveCallExprSignature(info, call)
			assert.Nil(t, fun)
			if tt.wantSignature {
				assert.Same(t, sig, resolved)
				assert.Same(t, sig.Params(), params)
			} else {
				assert.Nil(t, resolved)
				assert.Nil(t, params)
			}
		})
	}
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

func TestCallExprArgsForSignature(t *testing.T) {
	for _, tt := range []struct {
		name             string
		methodExpression bool
		wantExpanded     bool
	}{
		{name: "Function"},
		{name: "NamedFunction", wantExpanded: true},
		{name: "MethodExpression", methodExpression: true, wantExpanded: true},
		{name: "OverloadedMethodExpression", methodExpression: true},
		{name: "UnresolvedMethodExpression", methodExpression: true, wantExpanded: true},
		{name: "NoTypeInfo", methodExpression: true, wantExpanded: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := gotypes.NewPackage("example.com/tuple", "tuple")
			intType := gotypes.Typ[gotypes.Int]
			tupleType := gotypes.NewStruct([]*gotypes.Var{
				gotypes.NewField(token.NoPos, pkg, "X_0", intType, false),
				gotypes.NewField(token.NoPos, pkg, "X_1", intType, false),
			}, nil)
			pair := gotypes.NewParam(token.NoPos, pkg, "pair", tupleType)
			extras := gotypes.NewParam(token.NoPos, pkg, "extras", gotypes.NewSlice(intType))
			params := gotypes.NewTuple(pair, extras)
			ident := ast.NewIdent("Use")
			tuple := &ast.TupleLit{Elts: []ast.Expr{ast.NewIdent("first"), ast.NewIdent("second")}}
			call := &ast.CallExpr{Fun: ident, Args: []ast.Expr{tuple}}
			var owner *gotypes.Named
			var recv *gotypes.Var
			if tt.methodExpression {
				owner = gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "Client", nil), gotypes.NewStruct(nil, nil), nil)
				recv = gotypes.NewParam(token.NoPos, pkg, "", owner)
				call.Fun = &ast.SelectorExpr{X: ast.NewIdent("Client"), Sel: ident}
			}
			sig := gotypes.NewSignatureType(recv, nil, nil, params, nil, true)
			if tt.methodExpression {
				params = gotypes.NewTuple(recv, pair, extras)
			}
			info := newTestTypeInfo(nil, nil)
			switch tt.name {
			case "NamedFunction":
				named := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "Consumer", nil), sig, nil)
				info.Types[call.Fun] = gotypes.TypeAndValue{Type: named}
			case "MethodExpression":
				info.Uses[ident] = gotypes.NewFunc(token.NoPos, pkg, ident.Name, sig)
			case "OverloadedMethodExpression":
				impl := gotypes.NewFunc(token.NoPos, pkg, "Use__0", sig)
				info.Uses[ident] = gogen.NewOverloadMethod(owner, token.NoPos, pkg, ident.Name, impl)
			case "NoTypeInfo":
				info = nil
			}
			args, ellipsis := CallExprArgsForSignature(info, call, sig, params)
			wantArgs := call.Args
			if tt.wantExpanded {
				wantArgs = tuple.Elts
			}
			assert.Equal(t, wantArgs, args)
			assert.False(t, ellipsis)
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

func TestBindCallExprParamsKeywordInference(t *testing.T) {
	intType, stringType := gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.String]
	for _, tt := range []struct {
		name, source            string
		positional, keyword     gotypes.Type
		edited                  int
		keywordIsType, variadic bool
		want                    gotypes.Type
	}{
		{name: "EditedKeyword", source: "collect(int, value, label = text)", positional: intType, keyword: stringType, edited: 2, want: intType},
		{name: "EditedPositional", source: "collect(int, text, label = value)", positional: stringType, keyword: intType, edited: 1, want: intType},
		{name: "UnresolvedKeyword", source: "collect(int, value, label = missing)", positional: intType, edited: -1, want: intType},
		{name: "TypeAsKeywordValue", source: "collect(int, value, label = int)", keywordIsType: true, edited: 1},
		{name: "UnavailableKeywordSlot", source: "collect(int, value, extra, label = value)", positional: intType, keyword: intType, edited: -1},
		{name: "VariadicLoweringBeforeTypeArguments", source: "collect(int, value, label = value)",
			positional: intType, keyword: intType, variadic: true, edited: -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := gotypes.NewPackage("example.com/options", "options")
			markAsXGoPackage(pkg)
			key := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "Key", nil), anyType())
			value := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "Value", nil), anyType())
			params := []*gotypes.Var{
				gotypes.NewParam(token.NoPos, pkg, "first", value),
				gotypes.NewParam(token.NoPos, pkg, "options", gotypes.NewMap(stringType, value)),
			}
			if tt.variadic {
				params = append(params, gotypes.NewParam(token.NoPos, pkg, "rest", gotypes.NewSlice(value)))
			}
			sig := gotypes.NewSignatureType(nil, nil, []*gotypes.TypeParam{key, value}, gotypes.NewTuple(params...), nil, tt.variadic)
			fun := gotypes.NewFunc(token.NoPos, pkg, "XGox_Collect", sig)
			expr, err := parser.ParseExpr(tt.source)
			require.NoError(t, err)
			call, ok := expr.(*ast.CallExpr)
			require.True(t, ok)
			require.GreaterOrEqual(t, len(call.Args), 2)
			require.Len(t, call.Kwargs, 1)
			typeArg, ok := call.Args[0].(*ast.Ident)
			require.True(t, ok)
			info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{CallExprFunIdent(call): fun, typeArg: gotypes.Universe.Lookup("int")})
			info.Types[call.Args[1]] = gotypes.TypeAndValue{Type: tt.positional}
			keyword := call.Kwargs[0].Value
			if tt.keywordIsType {
				typeIdent, ok := keyword.(*ast.Ident)
				require.True(t, ok)
				info.Uses[typeIdent] = gotypes.Universe.Lookup(typeIdent.Name)
			} else {
				info.Types[keyword] = gotypes.TypeAndValue{Type: tt.keyword}
			}

			sig, normalized := ResolveFuncSignatureForCall(info, call, fun)
			bound, ok := BindCallExprParams(info, call, fun, sig, normalized, tt.edited)
			require.True(t, ok)
			require.Equal(t, len(params)+1, normalized.Len())
			require.Equal(t, normalized.Len(), bound.Len())
			assert.Same(t, normalized.At(0), bound.At(0))
			want := tt.want
			if want == nil {
				want = value
			}
			assert.True(t, gotypes.Identical(want, bound.At(1).Type()))
			assert.True(t, gotypes.Identical(gotypes.NewMap(stringType, want), bound.At(2).Type()))
		})
	}
}

func TestBindCallExprParamsVariadicTail(t *testing.T) {
	intType, stringType := gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.String]
	for _, tt := range []struct {
		name      string
		tail      []gotypes.Type
		ellipsis  token.Pos
		wantBound bool
	}{
		{name: "LaterOperandSuppliesType", tail: []gotypes.Type{stringType, intType}, wantBound: true},
		{name: "OnlyEditedOperand", tail: []gotypes.Type{stringType}},
		{name: "ExtraOperandBeforeEllipsis", tail: []gotypes.Type{stringType, gotypes.NewSlice(intType)}, ellipsis: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := gotypes.NewPackage("example.com/variadic", "variadic")
			markAsXGoPackage(pkg)
			anyType := gotypes.Universe.Lookup("any").Type()
			key := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "Key", nil), anyType)
			item := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "Item", nil), anyType)
			sig := gotypes.NewSignatureType(nil, nil, []*gotypes.TypeParam{key, item}, gotypes.NewTuple(
				gotypes.NewParam(token.NoPos, pkg, "key", key),
				gotypes.NewParam(token.NoPos, pkg, "tail", gotypes.NewSlice(item)),
			), nil, true)
			fun := gotypes.NewFunc(token.NoPos, pkg, "XGox_Collect", sig)
			ident := &ast.Ident{Name: "collect"}
			typeArg := &ast.Ident{Name: "int"}
			keyArg := &ast.BasicLit{Kind: token.INT, Value: "1"}
			call := &ast.CallExpr{Fun: ident, Args: []ast.Expr{typeArg, keyArg}, Ellipsis: tt.ellipsis}
			info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun, typeArg: gotypes.Universe.Lookup("int")})
			info.Types[keyArg] = gotypes.TypeAndValue{Type: intType}
			for _, typ := range tt.tail {
				arg := &ast.Ident{Name: "value"}
				call.Args = append(call.Args, arg)
				info.Types[arg] = gotypes.TypeAndValue{Type: typ}
			}

			sig, params := ResolveFuncSignatureForCall(info, call, fun)
			bound, ok := BindCallExprParams(info, call, fun, sig, params, 2)
			require.True(t, ok)
			require.Equal(t, 3, bound.Len())
			assert.Same(t, params.At(0), bound.At(0))
			var want gotypes.Type = item
			if tt.wantBound {
				want = intType
			}
			assert.True(t, gotypes.Identical(gotypes.NewSlice(want), bound.At(2).Type()))
		})
	}

	t.Run("BoundReceiver", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/variadic", "variadic")
		markAsXGoPackage(pkg)
		app := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "App", nil), gotypes.NewStruct(nil, nil), nil)
		pkg.Scope().Insert(app.Obj())
		anyType := gotypes.Universe.Lookup("any").Type()
		key := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "Key", nil), anyType)
		item := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "Item", nil), anyType)
		tail := gotypes.NewParam(token.NoPos, pkg, "tail", gotypes.NewSlice(item))
		sig := gotypes.NewSignatureType(nil, nil, []*gotypes.TypeParam{key, item}, gotypes.NewTuple(tail), nil, true)
		fun := gotypes.NewFunc(token.NoPos, pkg, "XGot_App_XGox_Collect", sig)
		recv := &ast.Ident{Name: "app"}
		ident := &ast.Ident{Name: "collect"}
		typeArg := &ast.Ident{Name: "int"}
		value := &ast.Ident{Name: "value"}
		call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: recv, Sel: ident}, Args: []ast.Expr{typeArg, value}}
		info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun, typeArg: gotypes.Universe.Lookup("int")})
		info.Types[recv] = gotypes.TypeAndValue{Type: app}
		info.Types[value] = gotypes.TypeAndValue{Type: stringType}

		arg, ok := ResolveCallExprArg(info, call, value)
		require.True(t, ok)
		assert.Equal(t, 1, arg.ParamIndex)
		assert.Same(t, tail, arg.Param.Origin())
		assert.Same(t, app, arg.ExpectedType)
	})
}

func TestBindCallExprParamsTemplateReceiver(t *testing.T) {
	const declarations = `package fixture
const XGoPackage = true
type Client[T any] struct{}
func XGot_Client_Accept(client *Client[int], value int) {}
func XGot_Client_Reject(client string, value int) {}
func XGot_Client_Use[T any](client *Client[T], value T) {}
func XGot_Client_XGox_Select[T any](client Client[T], value T) {}
func XGot_Client_XGox_Require[T ~int](client Client[T], value T) {}
`
	for _, tt := range []struct {
		name, source, function string
		want                   bool
	}{
		{"CompatibleReceiver", "client.accept(value)", "XGot_Client_Accept", true},
		{"IncompatibleReceiver", "client.reject(value)", "XGot_Client_Reject", false},
		{"BeforeFirstArgument", "client.use()", "XGot_Client_Use", true},
		{"MatchingTypeArgument", "client.select(int, value)", "XGot_Client_XGox_Select", true},
		{"IncompatibleTypeArgument", "client.select(string, value)", "XGot_Client_XGox_Select", false},
		{"UnsatisfiedConstraint", "client.require(string, value)", "XGot_Client_XGox_Require", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := goparser.ParseFile(fset, "fixture.go", declarations, 0)
			require.NoError(t, err)
			pkg, err := new(gotypes.Config).Check("example.com/fixture", fset, []*goast.File{file}, nil)
			require.NoError(t, err)
			intType := gotypes.Typ[gotypes.Int]
			receiver, err := gotypes.Instantiate(nil, pkg.Scope().Lookup("Client").Type(), []gotypes.Type{intType}, true)
			require.NoError(t, err)
			fun, ok := pkg.Scope().Lookup(tt.function).(*gotypes.Func)
			require.True(t, ok)

			expr, err := parser.ParseExpr(tt.source)
			require.NoError(t, err)
			call, ok := expr.(*ast.CallExpr)
			require.True(t, ok)
			sel, ok := call.Fun.(*ast.SelectorExpr)
			require.True(t, ok)
			info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{sel.Sel: fun})
			info.Types[sel.X] = gotypes.TypeAndValue{Type: receiver}
			if len(call.Args) == 2 {
				typeArg, ok := call.Args[0].(*ast.Ident)
				require.True(t, ok)
				info.Uses[typeArg] = gotypes.Universe.Lookup(typeArg.Name)
			}

			sig, params := ResolveFuncSignatureForCall(info, call, fun)
			require.NotNil(t, sig)
			bound, ok := BindCallExprParams(info, call, fun, sig, params, -1)
			assert.Equal(t, tt.want, ok)
			if tt.want {
				require.Positive(t, bound.Len())
				assert.Same(t, intType, bound.At(bound.Len()-1).Type())
			} else {
				assert.Same(t, params, bound)
			}
		})
	}
}
