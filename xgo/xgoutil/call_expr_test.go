/*
 * Copyright (c) 2025 The XGo Authors (xgo.dev). All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package xgoutil

import (
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"slices"
	"testing"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addInterfaceKwargFactory(typeInfo *types.Info, receiver ast.Expr, receiverTypeName string, iface *gotypes.Named) {
	pkg := iface.Obj().Pkg()
	recvNamed := gotypes.NewNamed(
		gotypes.NewTypeName(token.NoPos, pkg, receiverTypeName, nil),
		gotypes.NewStruct(nil, nil),
		nil,
	)
	recv := gotypes.NewVar(token.NoPos, pkg, "recv", recvNamed)
	factory := gotypes.NewFunc(token.NoPos, pkg, iface.Obj().Name(), gotypes.NewSignatureType(
		recv,
		nil,
		nil,
		nil,
		gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", iface)),
		false,
	))
	recvNamed.AddMethod(factory)
	typeInfo.Types[receiver] = gotypes.TypeAndValue{Type: recvNamed}
}

func newTestFunc(pkg *gotypes.Package, name string, variadic bool, params ...*gotypes.Var) *gotypes.Func {
	return gotypes.NewFunc(token.NoPos, pkg, name, gotypes.NewSignatureType(
		nil,
		nil,
		nil,
		gotypes.NewTuple(params...),
		nil,
		variadic,
	))
}

func newTestAutoclosureType(pkg *gotypes.Package, resultType gotypes.Type) *gotypes.Signature {
	return gotypes.NewSignatureType(
		nil,
		nil,
		nil,
		nil,
		gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", resultType)),
		false,
	)
}

func newTestKwarg(name, value string) *ast.KwargExpr {
	return &ast.KwargExpr{
		Name:  &ast.Ident{Name: name},
		Value: &ast.Ident{Name: value},
	}
}

func newTestEmptyInterface() *gotypes.Interface {
	iface := gotypes.NewInterfaceType(nil, nil)
	iface.Complete()
	return iface
}

func newTestNamedInterface(pkg *gotypes.Package, name string) *gotypes.Named {
	return gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, name, nil), nil, nil)
}

func setTestNamedInterfaceMethods(named *gotypes.Named, methods ...*gotypes.Func) {
	iface := gotypes.NewInterfaceType(methods, nil)
	iface.Complete()
	named.SetUnderlying(iface)
}

func newTestSelfReturningMethod(pkg *gotypes.Package, name string, self *gotypes.Named, paramName string, paramType gotypes.Type, variadic bool) *gotypes.Func {
	return gotypes.NewFunc(token.NoPos, pkg, name, gotypes.NewSignatureType(
		nil,
		nil,
		nil,
		gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, paramName, paramType)),
		gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", self)),
		variadic,
	))
}

func TestCreateCallExprFromBranchStmt(t *testing.T) {
	t.Run("NilTypeInfo", func(t *testing.T) {
		stmt := &ast.BranchStmt{Tok: token.GOTO}
		assert.Nil(t, CreateCallExprFromBranchStmt(nil, stmt))
	})

	t.Run("NilStatement", func(t *testing.T) {
		typeInfo := newTestTypeInfo(nil, nil)
		assert.Nil(t, CreateCallExprFromBranchStmt(typeInfo, nil))
	})

	t.Run("NonGotoStatement", func(t *testing.T) {
		stmt := &ast.BranchStmt{
			Tok: token.BREAK,
		}
		typeInfo := newTestTypeInfo(nil, nil)
		assert.Nil(t, CreateCallExprFromBranchStmt(typeInfo, stmt))
	})

	t.Run("GotoStatementWithLabelObjectNil", func(t *testing.T) {
		stmt := &ast.BranchStmt{
			Tok:    token.GOTO,
			TokPos: token.Pos(10),
			Label: &ast.Ident{
				NamePos: token.Pos(15),
				Name:    "label",
			},
		}
		typeInfo := newTestTypeInfo(nil, nil)
		assert.Nil(t, CreateCallExprFromBranchStmt(typeInfo, stmt))
	})

	t.Run("GotoStatementWithRealLabel", func(t *testing.T) {
		stmt := &ast.BranchStmt{
			Tok:    token.GOTO,
			TokPos: token.Pos(10),
			Label: &ast.Ident{
				NamePos: token.Pos(15),
				Name:    "label",
			},
		}

		pkg := gotypes.NewPackage("test", "test")
		label := gotypes.NewLabel(token.NoPos, pkg, "label")
		typeInfo := newTestTypeInfo(map[*ast.Ident]gotypes.Object{
			stmt.Label: label,
		}, nil)
		assert.Nil(t, CreateCallExprFromBranchStmt(typeInfo, stmt))
	})

	t.Run("GotoStatementWithoutMatchingIdent", func(t *testing.T) {
		labelIdent := &ast.Ident{
			NamePos: token.Pos(15),
			Name:    "label",
		}
		stmt := &ast.BranchStmt{
			Tok:    token.GOTO,
			TokPos: token.Pos(10),
			Label:  labelIdent,
		}

		pkg := gotypes.NewPackage("test", "test")
		variable := gotypes.NewVar(token.NoPos, pkg, "label", gotypes.Typ[gotypes.Int])
		typeInfo := newTestTypeInfo(map[*ast.Ident]gotypes.Object{
			labelIdent: variable,
		}, nil)
		assert.Nil(t, CreateCallExprFromBranchStmt(typeInfo, stmt))
	})

	t.Run("GotoStatementWithMatchingIdentButNotFunc", func(t *testing.T) {
		labelIdent := &ast.Ident{
			NamePos: token.Pos(15),
			Name:    "label",
		}
		stmt := &ast.BranchStmt{
			Tok:    token.GOTO,
			TokPos: token.Pos(10),
			Label:  labelIdent,
		}

		ident := &ast.Ident{
			NamePos: token.Pos(10),
			Name:    "goto",
		}

		pkg := gotypes.NewPackage("test", "test")
		labelVar := gotypes.NewVar(token.NoPos, pkg, "label", gotypes.Typ[gotypes.Int])
		gotoVar := gotypes.NewVar(token.NoPos, pkg, "goto", gotypes.Typ[gotypes.Int])
		typeInfo := newTestTypeInfo(map[*ast.Ident]gotypes.Object{
			labelIdent: labelVar,
		}, map[*ast.Ident]gotypes.Object{
			ident: gotoVar,
		})

		assert.Nil(t, CreateCallExprFromBranchStmt(typeInfo, stmt))
	})

	t.Run("GotoStatementWithMatchingFuncIdent", func(t *testing.T) {
		labelIdent := &ast.Ident{
			NamePos: token.Pos(15),
			Name:    "label",
		}
		stmt := &ast.BranchStmt{
			Tok:    token.GOTO,
			TokPos: token.Pos(10),
			Label:  labelIdent,
		}

		ident := &ast.Ident{
			NamePos: token.Pos(10),
			Name:    "goto",
		}

		pkg := gotypes.NewPackage("test", "test")
		labelVar := gotypes.NewVar(token.NoPos, pkg, "label", gotypes.Typ[gotypes.Int])
		sig := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "goto", sig)
		typeInfo := newTestTypeInfo(map[*ast.Ident]gotypes.Object{
			labelIdent: labelVar,
		}, map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		got := CreateCallExprFromBranchStmt(typeInfo, stmt)
		require.NotNil(t, got)
		assert.Equal(t, ident, got.Fun)
		require.Len(t, got.Args, 1)
		assert.Equal(t, stmt.Label, got.Args[0])
	})
}

func TestFuncFromCallExpr(t *testing.T) {
	t.Run("NilCallExpr", func(t *testing.T) {
		assert.Nil(t, FuncFromCallExpr(nil, nil))
	})

	t.Run("NilTypeInfo", func(t *testing.T) {
		expr := &ast.CallExpr{
			Fun: &ast.Ident{Name: "test"},
		}
		assert.Nil(t, FuncFromCallExpr(nil, expr))
	})

	t.Run("CallExprWithUnknownFunType", func(t *testing.T) {
		expr := &ast.CallExpr{
			Fun: &ast.BasicLit{
				Kind:  token.STRING,
				Value: "\"test\"",
			},
		}
		typeInfo := newTestTypeInfo(nil, nil)
		assert.Nil(t, FuncFromCallExpr(typeInfo, expr))
	})

	t.Run("CallExprWithIdentFun", func(t *testing.T) {
		ident := &ast.Ident{Name: "testFunc"}
		expr := &ast.CallExpr{
			Fun: ident,
		}

		pkg := gotypes.NewPackage("test", "test")
		sig := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "testFunc", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		got := FuncFromCallExpr(typeInfo, expr)
		require.NotNil(t, got)
		assert.Equal(t, fun, got)
	})

	t.Run("CallExprWithSelectorExprFun", func(t *testing.T) {
		sel := &ast.Ident{Name: "Method"}
		expr := &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "obj"},
				Sel: sel,
			},
		}

		pkg := gotypes.NewPackage("test", "test")
		sig := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "Method", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			sel: fun,
		})

		got := FuncFromCallExpr(typeInfo, expr)
		require.NotNil(t, got)
		assert.Equal(t, fun, got)
	})

	t.Run("CallExprWithIdentFunButNotFunc", func(t *testing.T) {
		ident := &ast.Ident{Name: "testVar"}
		expr := &ast.CallExpr{
			Fun: ident,
		}

		pkg := gotypes.NewPackage("test", "test")
		variable := gotypes.NewVar(token.NoPos, pkg, "testVar", gotypes.Typ[gotypes.Int])

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			ident: variable,
		})

		assert.Nil(t, FuncFromCallExpr(typeInfo, expr))
	})

	t.Run("CallExprWithNilObject", func(t *testing.T) {
		ident := &ast.Ident{Name: "testFunc"}
		expr := &ast.CallExpr{
			Fun: ident,
		}

		typeInfo := newTestTypeInfo(nil, nil)

		assert.Nil(t, FuncFromCallExpr(typeInfo, expr))
	})
}

func TestRequiredParamCount(t *testing.T) {
	for _, tt := range []struct {
		name              string
		params            []string
		variadic          bool
		local             bool
		localOptional     bool
		typeParams        int
		typeArgs          int
		variadicTypeParam bool
		want              int
	}{
		{name: "Empty"},
		{name: "Required", params: []string{"first", "second"}, want: 2},
		{name: "Optional", params: []string{"__xgo_optional_first", "__xgo_optional_second"}},
		{name: "Mixed", params: []string{"first", "__xgo_optional_second", "__xgo_optional_third"}, want: 1},
		{name: "OrdinaryParamWithHistoricalPrefix", params: []string{"first", "__gop_optional_second"}, want: 2},
		{name: "LocalOptional", params: []string{"first", "second"}, local: true, localOptional: true, want: 1},
		{name: "UnnamedLocalOptional", params: []string{"", ""}, local: true, localOptional: true, want: 1},
		{name: "LocalRequiredWithImportedPrefix", params: []string{"first", "__xgo_optional_second"}, local: true, want: 2},
		{name: "Variadic", params: []string{"first", "rest"}, variadic: true, want: 1},
		{name: "VariadicOnly", params: []string{"rest"}, variadic: true},
		{name: "OptionalAndVariadic", params: []string{"first", "__xgo_optional_second", "rest"}, variadic: true, want: 1},
		{name: "GenericRequired", params: []string{"first", "second"}, typeParams: 1, want: 2},
		{name: "GenericOptional", params: []string{"first", "__xgo_optional_second"}, typeParams: 1, want: 2},
		{name: "GenericVariadic", params: []string{"first", "rest"}, variadic: true, typeParams: 1, want: 1},
		{name: "GenericVariadicTypeParam", params: []string{"first", "rest"}, variadic: true, typeParams: 1, variadicTypeParam: true, want: 2},
		{name: "ExplicitTypeArgs", params: []string{"T", "first", "__xgo_optional_second"}, typeParams: 1, typeArgs: 1, want: 2},
		{name: "PartialTypeArgs", params: []string{"T", "first", "second", "__xgo_optional_third"}, typeParams: 2, typeArgs: 1, want: 4},
		{name: "ExplicitVariadicTypeParam", params: []string{"T", "first", "rest"}, variadic: true, typeParams: 1, typeArgs: 1, variadicTypeParam: true, want: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			typeInfo := &types.Info{Pkg: gotypes.NewPackage("main", "main")}
			pkg := typeInfo.Pkg
			if !tt.local {
				pkg = gotypes.NewPackage("example.com/api", "api")
			}
			compiler := &gogen.Package{PkgRef: gogen.PkgRef{Types: pkg}}
			var typeParams []*gotypes.TypeParam
			for i := range tt.typeParams {
				name := string(rune('T' + i))
				typeParams = append(typeParams, gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, name, nil), newTestEmptyInterface()))
			}
			var params []*gotypes.Var
			for i, name := range tt.params {
				var typ gotypes.Type = gotypes.Typ[gotypes.Int]
				if valueIndex := i - tt.typeArgs; valueIndex >= 0 && valueIndex < len(typeParams) {
					typ = typeParams[valueIndex]
				}

				if tt.variadic && i == len(tt.params)-1 {
					if tt.variadicTypeParam {
						typ = typeParams[0]
					}
					typ = gotypes.NewSlice(typ)
				}
				params = append(params, compiler.NewParam(token.NoPos, name, typ, tt.localOptional && i == len(tt.params)-1))
			}
			normalizedParams := gotypes.NewTuple(params...)
			sig := gotypes.NewSignatureType(nil, nil, typeParams, gotypes.NewTuple(params[tt.typeArgs:]...), nil, tt.variadic)
			assert.Equal(t, tt.want, RequiredParamCount(typeInfo, sig, normalizedParams, tt.typeArgs))
		})
	}
}

func TestKeywordMapType(t *testing.T) {
	intType, stringType := gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.String]
	nilType := gotypes.Typ[gotypes.UntypedNil]
	pointerType := gotypes.NewPointer(intType)
	results := gotypes.NewTuple(
		gotypes.NewVar(token.NoPos, nil, "", intType),
		gotypes.NewVar(token.NoPos, nil, "", stringType),
	)
	for _, tt := range []struct {
		name         string
		types        []gotypes.Type
		target, want gotypes.Type
	}{
		{name: "DefaultConstantType", types: []gotypes.Type{gotypes.Typ[gotypes.UntypedInt], intType}, want: gotypes.NewMap(stringType, intType)},
		{name: "MixedValues", types: []gotypes.Type{intType, stringType}, want: gotypes.NewMap(stringType, anyType())},
		{name: "NilWithPointer", types: []gotypes.Type{nilType, pointerType}, want: gotypes.NewMap(stringType, pointerType)},
		{name: "UntypedNil", types: []gotypes.Type{nilType}},
		{name: "NilWithTarget", types: []gotypes.Type{nilType},
			target: gotypes.NewMap(stringType, pointerType), want: gotypes.NewMap(stringType, pointerType)},
		{name: "WrongTargetValue", types: []gotypes.Type{intType}, target: gotypes.NewMap(stringType, stringType)},
		{name: "UncheckedValue", types: []gotypes.Type{intType, nil}},
		{name: "MultipleResults", types: []gotypes.Type{results}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cb := gogen.NewPackage("", "", &gogen.Config{
				NewBuiltin: func(*gogen.Package, *gogen.Config) *gotypes.Package { return nil },
			}).CB()
			cb.Val("sentinel")
			original := cb.Get(-1)
			var kwargs []*ast.KwargExpr
			typesByValue := make(map[ast.Expr]gotypes.Type)
			for i, typ := range tt.types {
				kwarg := newTestKwarg(string(rune('a'+i)), "value")
				kwargs = append(kwargs, kwarg)
				typesByValue[kwarg.Value] = typ
			}
			got := KeywordMapType(cb, kwargs, tt.target, func(value ast.Expr) gotypes.Type { return typesByValue[value] })
			assert.True(t, gotypes.Identical(tt.want, got), "want %v, got %v", tt.want, got)
			require.Equal(t, 1, cb.InternalStack().Len())
			assert.Same(t, original, cb.Get(-1))
		})
	}
}

func TestCallExprSupportsInterfaceKwargs(t *testing.T) {
	for _, tt := range []struct {
		name         string
		arity        []int
		typeReceiver bool
		static       bool
		template     bool
		private      bool
		want         bool
	}{
		{name: "SkipRequiredArgument", arity: []int{1, 0}, want: true},
		{name: "EarlierIncompatibleResult", arity: []int{0, 0}},
		{name: "NoApplicableOverload", arity: []int{1}},
		{name: "TypeWithoutFactory", typeReceiver: true},
		{name: "MethodExpression", arity: []int{1, 0}, typeReceiver: true},
		{name: "StaticOverload", arity: []int{1, 0}, typeReceiver: true, static: true, want: true},
		{name: "StaticAlias", arity: []int{1, 0}, typeReceiver: true, static: true, private: true, want: true},
		{name: "UnboundTemplateOverload", arity: []int{1, 0}, typeReceiver: true, template: true, want: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := gotypes.NewPackage("example.com/client", "client")
			name := "Options"
			if tt.private {
				name = "options"
			}
			options := newTestNamedInterface(pkg, name)
			options.SetUnderlying(gotypes.NewInterfaceType([]*gotypes.Func{newTestFunc(pkg, "Set", false)}, nil).Complete())
			client := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "Client", nil), gotypes.NewStruct(nil, nil), nil)
			var receiver *gotypes.Var
			if !tt.static && !tt.template {
				receiver = gotypes.NewParam(token.NoPos, pkg, "", client)
			}
			var candidates []gotypes.Object
			for i, arity := range tt.arity {
				result := gotypes.Type(options)
				if i == 0 {
					result = gotypes.Typ[gotypes.String]
				}
				params := make([]*gotypes.Var, arity)
				for j := range params {
					params[j] = gotypes.NewParam(token.NoPos, pkg, "", gotypes.Typ[gotypes.Int])
				}
				results := gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", result))
				sig := gotypes.NewSignatureType(receiver, nil, nil, gotypes.NewTuple(params...), results, false)
				candidates = append(candidates, gotypes.NewFunc(token.NoPos, pkg, "Build", sig))
			}
			switch {
			case tt.static:
				overload := gogen.NewOverloadFunc(token.NoPos, pkg, "Build", candidates...)
				gogen.NewStaticMember(client, token.NoPos, pkg, "Options", overload)
			case tt.template:
				overload := gogen.NewOverloadFunc(token.NoPos, pkg, "Build", candidates...)
				gogen.NewTemplateRecvMethod(client, token.NoPos, pkg, "Options", overload)
			case len(candidates) != 0:
				gogen.NewOverloadMethod(client, token.NoPos, pkg, "Options", candidates...)
			}

			info := newTestTypeInfo(nil, nil)
			info.Pkg = pkg
			ident := ast.NewIdent("client")
			info.Types[ident] = gotypes.TypeAndValue{Type: client}
			if tt.typeReceiver {
				info.Uses[ident] = client.Obj()
			}
			call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ident, Sel: ast.NewIdent("Complete")}}
			assert.Equal(t, tt.want, CallExprSupportsInterfaceKwargs(info, call, options))
		})
	}

	t.Run("PackageQualifier", func(t *testing.T) {
		for _, tt := range []struct {
			name, optionName string
			namedCallable    bool
			declaresType     bool
			generic          bool
			want             bool
		}{
			{name: "Factory", optionName: "Options", want: true},
			{name: "FactoryAlias", optionName: "options", want: true},
			{name: "NamedFunctionValue", optionName: "Options", namedCallable: true, want: true},
			{name: "NoAliasForNamedFunctionValue", optionName: "options", namedCallable: true},
			{name: "FunctionType", optionName: "Options", declaresType: true},
			{name: "UnboundTypeParameter", optionName: "Options", generic: true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				current := gotypes.NewPackage("example.com/current", "current")
				sdk := gotypes.NewPackage("example.com/sdk", "sdk")
				options := newTestNamedInterface(current, tt.optionName)
				options.SetUnderlying(newTestEmptyInterface())
				var typeParams []*gotypes.TypeParam
				if tt.generic {
					typeParams = append(typeParams, gotypes.NewTypeParam(
						gotypes.NewTypeName(token.NoPos, sdk, "T", nil), newTestEmptyInterface(),
					))
				}
				results := gotypes.NewTuple(gotypes.NewParam(token.NoPos, sdk, "", options))
				sig := gotypes.NewSignatureType(nil, nil, typeParams, nil, results, false)
				switch {
				case tt.declaresType:
					sdk.Scope().Insert(gotypes.NewTypeName(token.NoPos, sdk, "Options", sig))
				case tt.namedCallable:
					typ := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, sdk, "Factory", nil), sig, nil)
					sdk.Scope().Insert(gotypes.NewVar(token.NoPos, sdk, "Options", typ))
				default:
					sdk.Scope().Insert(gotypes.NewFunc(token.NoPos, sdk, "Options", sig))
				}
				qualifier := ast.NewIdent("sdk")
				info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
					qualifier: gotypes.NewPkgName(token.NoPos, current, "sdk", sdk),
				})
				info.Pkg = current
				call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: qualifier, Sel: ast.NewIdent("Complete")}}
				assert.Equal(t, tt.want, CallExprSupportsInterfaceKwargs(info, call, options))
			})
		}
	})

	t.Run("TemplateFactory", func(t *testing.T) {
		for _, tt := range []struct {
			name, typeParams, paramType string
			pointerReceiver             bool
			explicitTypeArgument        bool
			want                        bool
		}{
			{name: "ValueReceiver", typeParams: "T any", paramType: "Client[T]", want: true},
			{name: "AddressedReceiver", typeParams: "T any", paramType: "*Client[T]", want: true},
			{name: "PointerReceiver", typeParams: "T any", paramType: "*Client[T]", pointerReceiver: true, want: true},
			{name: "VariadicReceiver", typeParams: "T any", paramType: "...Client[T]", want: true},
			{name: "UninferredTypeParameter", typeParams: "T, U any", paramType: "Client[T]"},
			{name: "RequiresExplicitTypeArgument", typeParams: "T any", paramType: "Client[T]", explicitTypeArgument: true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				fset := token.NewFileSet()
				name := "Options"
				if tt.explicitTypeArgument {
					name = "XGox_Options"
				}
				file, err := goparser.ParseFile(fset, "builder.go", `package sdk
const XGoPackage = true
type Client[T any] struct{}
type Options interface { Set(int) Options }
func XGot_Client_`+name+`[`+tt.typeParams+`](client `+tt.paramType+`) Options { return nil }
`, 0)
				require.NoError(t, err)
				sdk, err := new(gotypes.Config).Check("example.com/sdk", fset, []*goast.File{file}, nil)
				require.NoError(t, err)
				gogen.InitXGoPackage(sdk)
				client, err := gotypes.Instantiate(nil, sdk.Scope().Lookup("Client").Type(), []gotypes.Type{gotypes.Typ[gotypes.Int]}, true)
				require.NoError(t, err)
				if tt.pointerReceiver {
					client = gotypes.NewPointer(client)
				}
				receiver := ast.NewIdent("client")
				info := newTestTypeInfo(nil, nil)
				info.Pkg = gotypes.NewPackage("example.com/current", "current")
				info.Types[receiver] = gotypes.TypeAndValue{Type: client}
				call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: receiver, Sel: ast.NewIdent("Complete")}}
				assert.Equal(t, tt.want, CallExprSupportsInterfaceKwargs(info, call, sdk.Scope().Lookup("Options").Type()))
			})
		}
	})
}

func TestResolveCallExprKwargBuilder(t *testing.T) {
	const declarations = `package fixture
const XGoPackage = true
type Client struct{}
func (Client) Complete(options Options) {}
func (Client) Options() Builder { return Builder{} }
`
	for _, tt := range []struct {
		name, builder    string
		keywords         []string
		wantTypes        []gotypes.Type
		wantFirst        []string
		lastArgumentType gotypes.Type
	}{
		{
			name: "Progression",
			builder: `type Options interface { Count(int) Options; Label(string) Options }
type Builder struct{}
type Next struct { Label func(string) Options }
func (Builder) Count(value int, __xgo_optional_label string) Next { return Next{} }
`,
			keywords:  []string{"count", "label", "count"},
			wantTypes: []gotypes.Type{gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Int]},
			wantFirst: []string{"count"},
		},
		{
			name: "AbsentPrecedingKeyword",
			builder: `type Options interface { Count(int) Options; Label(string) Options }
type Builder struct{}
func (Builder) Count(value int) Options { return nil }
`,
			keywords:  []string{"unknown", "count"},
			wantTypes: []gotypes.Type{nil, nil},
			wantFirst: []string{"count"},
		},
		{
			name: "CompatibleConcreteType",
			builder: `type Options interface { Count(func() int) Options }
type Builder struct{}
func (Builder) Count(__xgo_autoclosure_count func() int) Options { return nil }
`,
			keywords:  []string{"count"},
			wantTypes: []gotypes.Type{gotypes.Typ[gotypes.Int]},
			wantFirst: []string{"count"},
		},
		{
			name: "VariadicSet",
			builder: `type Options interface { Set(string, interface{}) Options }
type Builder struct{}
func (Builder) Set(key string, values ...int) Options { return nil }
`,
			keywords:  []string{"count"},
			wantTypes: []gotypes.Type{gotypes.Typ[gotypes.Int]},
		},
		{
			name: "SetRejectsStringKey",
			builder: `type Options interface { Set(string, interface{}) Options }
type Builder struct{}
func (Builder) Set(key int, value int) Options { return nil }
`,
			keywords:  []string{"count"},
			wantTypes: []gotypes.Type{nil},
		},
		{
			name: "InvalidSignature",
			builder: `type Options interface { Count(int) Options; Label(string) Options; Color(int) Options }
type Builder struct { Color int }
func (Builder) Count(value int, extra int) Options { return nil }
func (Builder) Label(value string) (Options, error) { return nil, nil }
`,
			keywords:  []string{"count"},
			wantTypes: []gotypes.Type{nil},
		},
		{
			name: "SelectedBuilderOverload",
			builder: `type Options interface { Count(int) Options }
type Builder struct{}
func (Builder) CountInt(value int) Options { return nil }
func (Builder) CountString(value string) Options { return nil }
const XGoo_Builder_Count = ".CountInt,.CountString"
`,
			keywords:         []string{"count"},
			wantTypes:        []gotypes.Type{gotypes.Typ[gotypes.String]},
			wantFirst:        []string{"count"},
			lastArgumentType: gotypes.Typ[gotypes.String],
		},
		{
			name: "TemplateMethod",
			builder: `type Options interface { Count(int) Options }
type Builder struct{}
func XGot_Builder_Count(builder Builder, count int) Options { return nil }
`,
			keywords:  []string{"count"},
			wantTypes: []gotypes.Type{gotypes.Typ[gotypes.Int]},
			wantFirst: []string{"count"},
		},
		{
			name: "GenericTemplateSet",
			builder: `type Options interface { Set(string, interface{}) Options }
type Builder struct{}
func XGot_Builder_Set[K ~string](builder Builder, key K, value int) Options { return nil }
`,
			keywords:  []string{"count"},
			wantTypes: []gotypes.Type{gotypes.Typ[gotypes.Int]},
		},
		{
			name: "IncompatibleTemplateReceiver",
			builder: `type Options interface { Count(int) Options }
type Builder struct{}
type Other struct{}
func XGot_Builder_Count(builder Other, count int) Options { return nil }
`,
			keywords:  []string{"count"},
			wantTypes: []gotypes.Type{nil},
		},
		{
			name: "TemplateRequiresTypeArgument",
			builder: `type Options interface { Count(int) Options }
type Builder struct{}
func XGot__Builder__XGox_Count[N ~int](builder Builder, count N) Options { return nil }
`,
			keywords:  []string{"count"},
			wantTypes: []gotypes.Type{nil},
		},
		{
			name: "AmbiguousBuilderResult",
			builder: `type Options interface { Count(int) Options; Label(string) Options }
type Builder struct{}
type Next struct{}
func (Builder) CountInt(value int) Next { return Next{} }
func (Builder) CountString(value string) Options { return nil }
const XGoo_Builder_Count = ".CountInt,.CountString"
`,
			keywords:  []string{"count", "label"},
			wantTypes: []gotypes.Type{anyType(), nil},
			wantFirst: []string{"count"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := goparser.ParseFile(fset, "fixture.go", declarations+tt.builder, 0)
			require.NoError(t, err)
			pkg, err := new(gotypes.Config).Check("example.com/fixture", fset, []*goast.File{file}, nil)
			require.NoError(t, err)
			gogen.InitXGoPackage(pkg)

			clientType := pkg.Scope().Lookup("Client").Type()
			complete, _, _ := gotypes.LookupFieldOrMethod(clientType, false, pkg, "Complete")
			require.NotNil(t, complete)
			receiver, method := ast.NewIdent("client"), ast.NewIdent("Complete")
			info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{method: complete})
			info.Pkg = gotypes.NewPackage("example.com/client", "client")
			info.Types[receiver] = gotypes.TypeAndValue{Type: clientType}
			call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: receiver, Sel: method}}
			for _, name := range tt.keywords {
				call.Kwargs = append(call.Kwargs, &ast.KwargExpr{Name: ast.NewIdent(name), Value: ast.NewIdent("value")})
			}
			if tt.lastArgumentType != nil {
				info.Types[call.Kwargs[len(call.Kwargs)-1].Value] = gotypes.TypeAndValue{Type: tt.lastArgumentType}
			}

			binding := ResolveCallExprKwarg(info, call)
			require.NotNil(t, binding)
			var names []string
			for _, target := range ListResolvedCallExprKwargTargets(binding) {
				names = append(names, target.Name)
			}
			assert.ElementsMatch(t, tt.wantFirst, names)

			args := slices.Collect(ResolvedCallExprArgs(info, call))
			require.Len(t, args, len(tt.wantTypes))
			for i, want := range tt.wantTypes {
				if want == nil {
					assert.Nil(t, args[i].KwargTarget)
					continue
				}
				require.NotNil(t, args[i].KwargTarget)
				assert.True(t, gotypes.Identical(want, args[i].ExpectedType))
			}
		})
	}
}

func TestResolveCallExprKwargDefaultMap(t *testing.T) {
	const declarations = `package fixture
const XGoPackage = true
type Options struct { Count string }
func Options_Init(values map[string]int) Options { return Options{} }
type Labels map[string]string
func Labels_Init(values map[string]string) Labels { return nil }
type Flexible map[string]int
func Flexible_Init(values map[string]string) Flexible { return nil }
type Extra struct{}
func Extra_Init(values map[string]int, label string) Extra { return Extra{} }
type Key string
type Slice []int
func Slice_Init(values map[string]int) Slice { return nil }
type PlainSlice []int
type InvalidSlice []int
func InvalidSlice_Init(values map[string]int, label string) InvalidSlice { return nil }
type OverloadedSlice []int
func FromValues(values map[string]int) OverloadedSlice { return nil }
func FromCount(value int) OverloadedSlice { return nil }
const XGoo__OverloadedSlice_Init = "FromValues,FromCount"
type GenericSlice []int
func GenericSlice_Init[T any](values map[string]T) GenericSlice { return nil }
type GenericKeySlice []int
func GenericKeySlice_Init[K ~string](values map[K]int) GenericKeySlice { return nil }
type GenericValueSlice []int
func GenericValueSlice_Init[T any](value T) GenericValueSlice { return nil }
type InterfaceSlice []int
func InterfaceSlice_Init(value any) InterfaceSlice { return nil }
type NestedSlice []int
func NestedSlice_Init(values Slice) NestedSlice { return nil }
`
	for _, tt := range []struct {
		name, parameter string
		declared        bool
		want            gotypes.Type
	}{
		{"Map", "map[string]int", false, gotypes.Typ[gotypes.Int]},
		{"VariadicMap", "...map[string]int", false, gotypes.Typ[gotypes.Int]},
		{"StructInitializer", "Options", false, gotypes.Typ[gotypes.Int]},
		{"SameMapType", "Labels", false, gotypes.Typ[gotypes.String]},
		{"MultipleMapTypes", "Flexible", false, anyType()},
		{"RequiredInitializerArgument", "Extra", false, anyType()},
		{"NamedKey", "map[Key]int", false, anyType()},
		{"Unconstrained", "any", false, anyType()},
		{"DeclaredSliceInitializer", "Slice", true, gotypes.Typ[gotypes.Int]},
		{"DeclaredSliceWithoutInitializer", "PlainSlice", true, nil},
		{"DeclaredInitializerWithRequiredArgument", "InvalidSlice", true, nil},
		{"DeclaredOverloadedInitializer", "OverloadedSlice", true, gotypes.Typ[gotypes.Int]},
		{"DeclaredGenericInitializer", "GenericSlice", true, anyType()},
		{"DeclaredGenericKeyInitializer", "GenericKeySlice", true, anyType()},
		{"DeclaredGenericValueInitializer", "GenericValueSlice", true, anyType()},
		{"DeclaredInterfaceInitializer", "InterfaceSlice", true, anyType()},
		{"DeclaredNestedInitializer", "NestedSlice", true, anyType()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			source := declarations + "type Handler func(opts " + tt.parameter + ")\n"
			if tt.declared {
				source = declarations + "func Handler(opts " + tt.parameter + ") {}\n"
			}
			file, err := goparser.ParseFile(fset, "fixture.go", source, 0)
			require.NoError(t, err)
			pkg, err := new(gotypes.Config).Check("example.com/fixture", fset, []*goast.File{file}, nil)
			require.NoError(t, err)
			gogen.InitXGoPackage(pkg)
			handler := pkg.Scope().Lookup("Handler").Type()
			ident := ast.NewIdent("handle")
			info := newTestTypeInfo(nil, nil)
			info.Pkg = pkg
			info.Types[ident] = gotypes.TypeAndValue{Type: handler}
			call := &ast.CallExpr{
				Fun:    ident,
				Kwargs: []*ast.KwargExpr{{Name: ast.NewIdent("count"), Value: ast.NewIdent("value")}},
			}
			kwarg := ResolveCallExprKwarg(info, call)
			require.NotNil(t, kwarg)
			assert.Zero(t, kwarg.ParamIndex)
			assert.False(t, kwarg.AllowInterfaceTargets)
			assert.Empty(t, ListResolvedCallExprKwargTargets(kwarg))

			target := LookupResolvedCallExprKwargTarget(kwarg, "count")
			if tt.want == nil {
				assert.Nil(t, target)
				return
			}
			require.NotNil(t, target)
			assert.Equal(t, ResolvedCallExprKwargTargetMap, target.Kind)
			assert.True(t, gotypes.Identical(tt.want, target.ValueType))
		})
	}
}

func TestConcreteOverloadSelection(t *testing.T) {
	pkg := gotypes.NewPackage("main", "main")
	sig := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
	first := gotypes.NewFunc(token.NoPos, pkg, "first", sig)
	second := gotypes.NewFunc(token.NoPos, pkg, "second", sig)
	assert.Same(t, first, ConcreteOverloadSelection(first).Implementation)
	assert.Same(t, first, ConcreteOverloadSelection(gogen.NewOverloadFunc(token.NoPos, pkg, "Single", first)).Implementation)
	assert.Empty(t, ConcreteOverloadSelection(gogen.NewOverloadFunc(token.NoPos, pkg, "Group", first, second)))
	assert.Empty(t, ConcreteOverloadSelection(gotypes.NewFunc(token.NoPos, pkg, "_", sig)))
}

func TestCallExprOverloadImplementations(t *testing.T) {
	info := newTestTypeInfo(nil, nil)
	assert.Nil(t, CallExprOverloadImplementations(info, &ast.CallExpr{
		Fun: &ast.CallExpr{Fun: ast.NewIdent("factory")},
	}))
	assert.Nil(t, CallExprOverloadImplementations(info, &ast.CallExpr{Fun: ast.NewIdent("unresolved")}))

	pkg := gotypes.NewPackage("example.com/fixture", "fixture")
	client := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "Client", nil), gotypes.NewStruct(nil, nil), nil)
	first := newTestFunc(pkg, "First", false)
	second := newTestFunc(pkg, "Second", false)
	group := gogen.NewOverloadFunc(token.NoPos, pkg, "Group", first, second)
	t.Run("ValueIndex", func(t *testing.T) {
		name := ast.NewIdent("Group")
		index := ast.NewIdent("index")
		info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			name:  group,
			index: gotypes.NewVar(token.NoPos, pkg, "index", gotypes.Typ[gotypes.Int]),
		})
		call := &ast.CallExpr{Fun: &ast.IndexExpr{X: name, Index: index}}
		assert.Nil(t, CallExprOverloadImplementations(info, call))
	})

	gogen.NewStaticMember(client, token.NoPos, pkg, "Choose", group)
	gogen.NewStaticMember(client, token.NoPos, pkg, "Single", first)

	for _, tt := range []struct {
		name string
		want []gotypes.Object
	}{
		{"Choose", []gotypes.Object{first, second}},
		{"Single", []gotypes.Object{first}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wrapper, _, _ := gotypes.LookupFieldOrMethod(client, false, pkg, tt.name)
			require.NotNil(t, wrapper)
			ident := ast.NewIdent(tt.name)
			info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: wrapper})
			call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent("Client"), Sel: ident}}
			candidates := CallExprOverloadImplementations(info, call)
			require.NotNil(t, candidates)
			var implementations []gotypes.Object
			for implementation, binding := range candidates {
				assert.Same(t, wrapper, binding)
				implementations = append(implementations, implementation)
			}
			assert.Equal(t, tt.want, implementations)

			for implementation := range candidates {
				assert.Same(t, first, implementation)
				break
			}
		})
	}
}

func TestResolveCallExprKwargLoweredBuilder(t *testing.T) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "fixture.go", `package fixture
type Options struct { Count int }
type Params interface { Count(bool) Params }
type Builder struct{}
type Next struct{}
type Client struct{}
func (Builder) CountNumber(int) Builder { return Builder{} }
func (Builder) CountText(string) Next { return Next{} }
const XGoo_Builder_Count = ".CountNumber,.CountText"
func Options_Init(Next) Options { return Options{} }
func Params_Init(Next) Params { return nil }
func (Client) Params() Builder { return Builder{} }
func (Client) WithOptions(prefix string, opts Options) {}
func (Client) WithBuilder(prefix string, opts Params) {}
const XGoo_Client_Choose = ".WithOptions,.WithBuilder"
`, 0)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check("example.com/fixture", fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	gogen.InitXGoPackage(pkg)
	client := pkg.Scope().Lookup("Client").Type()
	protocol := pkg.Scope().Lookup("Params").Type()
	builder := pkg.Scope().Lookup("Builder").Type()
	next := pkg.Scope().Lookup("Next").Type()
	group, _, _ := gotypes.LookupFieldOrMethod(builder, false, pkg, "Count")
	countText, _, _ := gotypes.LookupFieldOrMethod(builder, false, pkg, "CountText")
	require.NotNil(t, group)
	require.NotNil(t, countText)

	for _, tt := range []struct {
		callee, implementation string
	}{
		{"WithBuilder", "WithBuilder"},
		{"Choose", "WithOptions"},
	} {
		t.Run(tt.callee, func(t *testing.T) {
			source, _, _ := gotypes.LookupFieldOrMethod(client, false, pkg, tt.callee)
			selected, _, _ := gotypes.LookupFieldOrMethod(client, false, pkg, tt.implementation)
			require.NotNil(t, source)
			require.NotNil(t, selected)

			receiver, callee := ast.NewIdent("client"), ast.NewIdent(tt.callee)
			arg := newTestKwarg("count", "value")
			call := &ast.CallExpr{
				Fun:    &ast.SelectorExpr{X: receiver, Sel: callee},
				Args:   []ast.Expr{ast.NewIdent("prefix")},
				Kwargs: []*ast.KwargExpr{arg},
			}
			factory := &ast.CallExpr{Fun: &ast.SelectorExpr{X: receiver, Sel: ast.NewIdent("Params")}}
			step := ast.NewIdent("Count")
			lowered := &ast.CallExpr{
				Fun:  &ast.SelectorExpr{X: factory, Sel: step},
				Args: []ast.Expr{arg.Value},
			}
			info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{callee: selected, step: countText})
			info.Pkg = pkg
			info.Overloads = map[*ast.Ident]gotypes.Object{callee: source, step: group}
			info.Types[receiver] = gotypes.TypeAndValue{Type: client}
			info.Types[factory] = gotypes.TypeAndValue{Type: builder}
			info.Types[lowered] = gotypes.TypeAndValue{Type: next}
			info.KeywordArgs = map[*ast.KwargExpr]types.KeywordLowering{
				arg: {Arg: lowered, Factory: factory, BuilderSteps: map[token.Pos]*ast.CallExpr{arg.Name.Pos(): lowered}},
			}

			kwarg := ResolveCallExprKwarg(info, call)
			require.NotNil(t, kwarg)
			assert.Same(t, protocol, kwarg.ContainerType())
			target := kwarg.KeywordTarget(arg)
			require.NotNil(t, target)
			require.NotNil(t, target.Method)
			assert.Same(t, gotypes.Typ[gotypes.String], target.ValueType)
			assert.Same(t, protocol, target.Method.Signature().Recv().Type())
			after := kwarg.AfterKeywords(call.Kwargs)
			require.NotNil(t, after)
			assert.Same(t, next, after.interfaceBuilder)
		})
	}

	for _, tt := range []struct {
		name, keyword             string
		factoryResult, stepResult gotypes.Type
		nonSelector               bool
		want                      gotypes.Type
	}{
		{name: "FactoryType", keyword: "count", factoryResult: protocol, want: protocol},
		{name: "ResultType", keyword: "count", factoryResult: builder, stepResult: protocol, want: protocol},
		{name: "UnmatchedKeyword", keyword: "other", factoryResult: builder, stepResult: protocol},
		{name: "UnrecognizedFactory", keyword: "count", factoryResult: protocol, nonSelector: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			arg := newTestKwarg(tt.keyword, "value")
			receiver := ast.NewIdent("client")
			call := &ast.CallExpr{
				Fun:    &ast.SelectorExpr{X: receiver, Sel: ast.NewIdent("Choose")},
				Kwargs: []*ast.KwargExpr{arg},
			}
			factory := &ast.CallExpr{Fun: &ast.SelectorExpr{X: receiver, Sel: ast.NewIdent("Params")}}
			if tt.nonSelector {
				factory.Fun = ast.NewIdent("Params")
			}
			step := &ast.CallExpr{
				Fun:  &ast.SelectorExpr{X: factory, Sel: ast.NewIdent(UpperFirstASCII(tt.keyword))},
				Args: []ast.Expr{arg.Value},
			}
			lowering := types.KeywordLowering{
				Arg: step, Factory: factory, BuilderSteps: map[token.Pos]*ast.CallExpr{arg.Name.Pos(): step},
			}
			info := newTestTypeInfo(nil, nil)
			info.Types[factory] = gotypes.TypeAndValue{Type: tt.factoryResult}
			info.Types[step] = gotypes.TypeAndValue{Type: tt.stepResult}
			assert.Equal(t, tt.want, keywordBuilderType(info, call, nil, nil, lowering))
		})
	}
}

func TestResolveCallExprKwargLoweredStruct(t *testing.T) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "fixture.go", `package fixture
const XGoPackage = true
type Head struct { Count int }
type Tail struct { count int; Count string }
type Client struct{}
func XGot_Client_Collect(client Client, head Head, parts ...Tail) {}
`, 0)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check("example.com/fixture", fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	gogen.InitXGoPackage(pkg)
	client := pkg.Scope().Lookup("Client").Type()
	head := pkg.Scope().Lookup("Head").Type()
	tail := pkg.Scope().Lookup("Tail").Type()
	method, _, _ := gotypes.LookupFieldOrMethod(client, false, pkg, "Collect")
	implementation := pkg.Scope().Lookup("XGot_Client_Collect")
	field, _, _ := gotypes.LookupFieldOrMethod(tail, false, pkg, "Count")
	require.NotNil(t, method)
	require.NotNil(t, implementation)
	require.NotNil(t, field)

	receiver, callee := ast.NewIdent("client"), ast.NewIdent("collect")
	arg := newTestKwarg("count", "value")
	arg.Name.NamePos = 10
	call := &ast.CallExpr{
		Fun:    &ast.SelectorExpr{X: receiver, Sel: callee},
		Args:   []ast.Expr{ast.NewIdent("head")},
		Kwargs: []*ast.KwargExpr{arg},
	}
	key := &ast.Ident{Name: "Count", NamePos: arg.Name.Pos()}
	lowered := &ast.CompositeLit{Elts: []ast.Expr{&ast.KeyValueExpr{Key: key, Value: arg.Value}}}
	info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{callee: implementation, key: field})
	info.Pkg = pkg
	info.Overloads = map[*ast.Ident]gotypes.Object{callee: method}
	info.Types[receiver] = gotypes.TypeAndValue{Type: client}
	info.Types[lowered] = gotypes.TypeAndValue{Type: tail}
	info.KeywordArgs = map[*ast.KwargExpr]types.KeywordLowering{arg: {Arg: lowered}}

	binding := ResolveCallExprKwarg(info, call)
	require.NotNil(t, binding)
	assert.Same(t, tail, binding.ContainerType())
	assert.Same(t, head, binding.StructSpellingType())
	target := binding.KeywordTarget(arg)
	require.NotNil(t, target)
	assert.Same(t, field, target.Field)
	assert.Same(t, gotypes.Typ[gotypes.String], target.ValueType)
	targets := ListResolvedCallExprKwargTargets(binding)
	require.Len(t, targets, 1)
	assert.Same(t, field, targets[0].Field)
	assert.Equal(t, "count", targets[0].Name)
}

func TestCheckedKeywordArgIndex(t *testing.T) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "fixture.go", `package fixture
type Alias = map[string]string
func Prepend(opts map[string]string, parts ...int) {}
func Append(head string, opts map[string]string) {}
func AppendAlias(head string, opts Alias) {}
func AppendInferred(head string, opts any) {}
func VariadicOnly(parts ...map[string]string) {}
func Selected(head string, parts ...any) {}
func TypedHead(head map[string]int, parts ...any) {}
func Flexible(head any, parts ...any) {}
`, 0)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check("example.com/fixture", fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	stringMap := gotypes.NewMap(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.String])
	alias := pkg.Scope().Lookup("Alias")
	require.NotNil(t, alias)
	for _, tt := range []struct {
		name, selected string
		candidates     []string
		lowered        gotypes.Type
		want           int
	}{
		{"RejectsIncompatiblePlacement", "Selected", []string{"VariadicOnly", "Prepend", "Append"}, stringMap, 1},
		{"AmbiguousPlacement", "Flexible", []string{"Prepend", "Append"}, stringMap, 0},
		{"AliasPlacement", "Flexible", []string{"Prepend", "AppendAlias"}, alias.Type(), 1},
		{"InferredMapPlacement", "TypedHead", []string{"Prepend", "AppendInferred"}, nil, 1},
		{"UnmatchedContainer", "Flexible", []string{"Prepend", "Append"}, anyType(), 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			selected, ok := pkg.Scope().Lookup(tt.selected).(*gotypes.Func)
			require.True(t, ok)
			ident := ast.NewIdent("choose")
			call := &ast.CallExpr{Fun: ident, Args: []ast.Expr{ast.NewIdent("value")}}
			candidates := []gotypes.Object{selected}
			for _, name := range tt.candidates {
				candidates = append(candidates, pkg.Scope().Lookup(name))
			}
			group := gogen.NewOverloadFunc(token.NoPos, pkg, ident.Name, candidates...)
			info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: selected})
			info.Overloads = map[*ast.Ident]gotypes.Object{ident: group}

			sig := selected.Signature()
			assert.Equal(t, tt.want, checkedKeywordArgIndex(info, call, sig, sig.Params(), tt.lowered, tt.lowered, 0))
		})
	}
}

func TestResolveCallExprKwarg(t *testing.T) {
	t.Run("NilCallExpr", func(t *testing.T) {
		assert.Nil(t, ResolveCallExprKwarg(nil, nil))
	})

	t.Run("RecordedContainers", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			container gotypes.Type
			want      gotypes.Type
		}{
			{"TypedMap", gotypes.NewMap(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.String]), gotypes.Typ[gotypes.String]},
			{"InferredMap", nil, anyType()},
		} {
			t.Run(tt.name, func(t *testing.T) {
				pkg := gotypes.NewPackage("main", "main")
				field := gotypes.NewField(token.NoPos, pkg, "Count", gotypes.Typ[gotypes.Int], false)
				options := gotypes.NewStruct([]*gotypes.Var{field}, nil)
				param := gotypes.NewParam(token.NoPos, pkg, "opts", options)
				sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(param), nil, false)
				fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)
				ident := ast.NewIdent(fun.Name())
				arg := &ast.KwargExpr{Name: ast.NewIdent("count"), Value: ast.NewIdent("value")}
				call := &ast.CallExpr{Fun: ident, Kwargs: []*ast.KwargExpr{arg}}
				info := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
				info.Pkg = pkg
				lowered := &ast.CompositeLit{}
				info.Types[lowered] = gotypes.TypeAndValue{Type: tt.container}
				info.KeywordArgs = map[*ast.KwargExpr]types.KeywordLowering{arg: {Arg: lowered}}

				checked := ResolveCallExprKwarg(info, call).KeywordTarget(arg)
				require.NotNil(t, checked)
				assert.Equal(t, ResolvedCallExprKwargTargetMap, checked.Kind)
				assert.True(t, gotypes.Identical(tt.want, checked.ValueType))
				candidate := ResolveCallExprKwargAtArgCount(info, call, sig, sig.Params(), 0).KeywordTarget(arg)
				require.NotNil(t, candidate)
				assert.Same(t, field, candidate.Field)
			})
		}
	})

	t.Run("LocalOptionalParam", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}
		pkg := gotypes.NewPackage("main", "main")
		compiler := &gogen.Package{PkgRef: gogen.PkgRef{Types: pkg}}
		param := compiler.NewParam(token.NoPos, "opts", gotypes.NewMap(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Int]), true)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(param), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		typeInfo.Pkg = pkg

		resolved := ResolveCallExprKwarg(typeInfo, &ast.CallExpr{Fun: ident})
		require.NotNil(t, resolved)
		assert.Equal(t, param, resolved.Param)
		assert.Zero(t, resolved.ParamIndex)
	})

	t.Run("NonOptionalParam", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}

		pkg := gotypes.NewPackage("main", "main")
		param := gotypes.NewParam(token.NoPos, pkg, "opts", gotypes.NewMap(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Int]))
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(param), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		assert.Nil(t, ResolveCallExprKwarg(typeInfo, &ast.CallExpr{Fun: ident}))
	})

	t.Run("NonOptionalParamWithKwargs", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}

		pkg := gotypes.NewPackage("main", "main")
		param := gotypes.NewParam(token.NoPos, pkg, "opts", gotypes.NewMap(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Int]))
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(param), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		resolved := ResolveCallExprKwarg(typeInfo, &ast.CallExpr{
			Fun: ident,
			Kwargs: []*ast.KwargExpr{{
				Name:  &ast.Ident{Name: "count"},
				Value: &ast.BasicLit{Kind: token.INT, Value: "1"},
			}},
		})
		require.NotNil(t, resolved)
		assert.Equal(t, param, resolved.Param)
		assert.Zero(t, resolved.ParamIndex)
	})

	t.Run("CallPositionSelectsOptionalParam", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}

		pkg := gotypes.NewPackage("main", "main")
		requiredParam := gotypes.NewParam(token.NoPos, pkg, "name", gotypes.Typ[gotypes.String])
		firstOptionalParam := gotypes.NewParam(
			token.NoPos,
			pkg,
			"__xgo_optional_first",
			gotypes.NewMap(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Int]),
		)
		secondOptionalParam := gotypes.NewParam(
			token.NoPos,
			pkg,
			"__xgo_optional_second",
			gotypes.NewMap(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.String]),
		)
		sig := gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(requiredParam, firstOptionalParam, secondOptionalParam),
			nil,
			false,
		)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})

		firstResolved := ResolveCallExprKwarg(typeInfo, &ast.CallExpr{
			Fun:  ident,
			Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"first"`}},
		})
		require.NotNil(t, firstResolved)
		assert.Equal(t, firstOptionalParam, firstResolved.Param)
		assert.Equal(t, 1, firstResolved.ParamIndex)

		secondResolved := ResolveCallExprKwarg(typeInfo, &ast.CallExpr{
			Fun: ident,
			Args: []ast.Expr{
				&ast.BasicLit{Kind: token.STRING, Value: `"first"`},
				&ast.BasicLit{Kind: token.STRING, Value: `"second"`},
			},
		})
		require.NotNil(t, secondResolved)
		assert.Equal(t, secondOptionalParam, secondResolved.Param)
		assert.Equal(t, 2, secondResolved.ParamIndex)
	})
}

func TestSourceParamName(t *testing.T) {
	pkg := gotypes.NewPackage("main", "main")
	for _, tt := range []struct {
		name string
		want string
	}{
		{name: "__xgo_autoclosure_condition", want: "condition"},
		{name: "__xgo_optional_opts", want: "opts"},
		{name: "__xgo_loop_body", want: "body"},
		{name: "__xgo_cond_action", want: "action"},
		{name: "__gop_optional_opts", want: "__gop_optional_opts"},
		{name: "__xgo_internal_condition", want: "__xgo_internal_condition"},
		{name: "opts", want: "opts"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			param := gotypes.NewParam(token.NoPos, pkg, tt.name, gotypes.Typ[gotypes.Int])
			assert.Equal(t, tt.want, SourceParamName(param))
		})
	}
}

func TestSourceParamType(t *testing.T) {
	pkg := gotypes.NewPackage("main", "main")
	callbackType := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
	typeParam := gotypes.NewTypeParam(
		gotypes.NewTypeName(token.NoPos, pkg, "T", nil),
		newTestEmptyInterface(),
	)
	namedAutoclosureType := gotypes.NewNamed(
		gotypes.NewTypeName(token.NoPos, pkg, "Condition", nil),
		newTestAutoclosureType(pkg, gotypes.Typ[gotypes.Bool]),
		nil,
	)
	namedResultType := gotypes.NewNamed(
		gotypes.NewTypeName(token.NoPos, pkg, "Flag", nil),
		gotypes.Typ[gotypes.Bool],
		nil,
	)
	withParamType := gotypes.NewSignatureType(
		nil,
		nil,
		nil,
		gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "value", gotypes.Typ[gotypes.Int])),
		gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", gotypes.Typ[gotypes.Bool])),
		false,
	)
	multipleResultType := gotypes.NewSignatureType(
		nil,
		nil,
		nil,
		nil,
		gotypes.NewTuple(
			gotypes.NewParam(token.NoPos, pkg, "", gotypes.Typ[gotypes.Bool]),
			gotypes.NewParam(token.NoPos, pkg, "", gotypes.Typ[gotypes.String]),
		),
		false,
	)

	for _, tt := range []struct {
		name            string
		paramName       string
		paramType       gotypes.Type
		want            gotypes.Type
		wantAutoclosure bool
	}{
		{
			name:            "Autoclosure",
			paramName:       "__xgo_autoclosure_condition",
			paramType:       newTestAutoclosureType(pkg, gotypes.Typ[gotypes.Bool]),
			want:            gotypes.Typ[gotypes.Bool],
			wantAutoclosure: true,
		},
		{
			name:            "NamedAutoclosureType",
			paramName:       "__xgo_autoclosure_condition",
			paramType:       namedAutoclosureType,
			want:            gotypes.Typ[gotypes.Bool],
			wantAutoclosure: true,
		},
		{
			name:            "FunctionResult",
			paramName:       "__xgo_autoclosure_callback",
			paramType:       newTestAutoclosureType(pkg, callbackType),
			want:            callbackType,
			wantAutoclosure: true,
		},
		{
			name:            "NamedResult",
			paramName:       "__xgo_autoclosure_value",
			paramType:       newTestAutoclosureType(pkg, namedResultType),
			want:            namedResultType,
			wantAutoclosure: true,
		},
		{
			name:            "GenericResult",
			paramName:       "__xgo_autoclosure_value",
			paramType:       newTestAutoclosureType(pkg, typeParam),
			want:            typeParam,
			wantAutoclosure: true,
		},
		{
			name:      "OrdinaryCallback",
			paramName: "condition",
			paramType: newTestAutoclosureType(pkg, gotypes.Typ[gotypes.Bool]),
		},
		{
			name:      "NonFunctionAutoclosureType",
			paramName: "__xgo_autoclosure_condition",
			paramType: gotypes.Typ[gotypes.Bool],
		},
		{
			name:      "AutoclosureWithParameter",
			paramName: "__xgo_autoclosure_condition",
			paramType: withParamType,
		},
		{
			name:      "AutoclosureWithoutResult",
			paramName: "__xgo_autoclosure_condition",
			paramType: callbackType,
		},
		{
			name:      "AutoclosureWithMultipleResults",
			paramName: "__xgo_autoclosure_condition",
			paramType: multipleResultType,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			param := gotypes.NewParam(token.NoPos, pkg, tt.paramName, tt.paramType)
			want := tt.want
			if want == nil {
				want = tt.paramType
			}

			resultType, ok := AutoclosureParamResultType(param)
			assert.Equal(t, tt.wantAutoclosure, ok)
			if tt.wantAutoclosure {
				assert.True(t, gotypes.Identical(want, resultType))
			} else {
				assert.Nil(t, resultType)
			}
			assert.True(t, gotypes.Identical(want, SourceParamType(param)))
		})
	}
}

func TestResolveCallableSignatureForCall(t *testing.T) {
	for _, tt := range []struct {
		name              string
		variable, unbound bool
		wantParams        int
	}{
		{name: "BoundFunction", wantParams: 1},
		{name: "BoundVariable", variable: true, wantParams: 1},
		{name: "UnboundVariable", variable: true, unbound: true, wantParams: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := gotypes.NewPackage("example.com/framework", "framework")
			owner := gotypes.NewTypeName(token.NoPos, pkg, "Item", nil)
			named := gotypes.NewNamed(owner, gotypes.NewStruct(nil, nil), nil)
			receiver := gotypes.NewParam(token.NoPos, pkg, "item", named)
			value := gotypes.NewParam(token.NoPos, pkg, "value", gotypes.Typ[gotypes.Int])
			fun := newTestFunc(pkg, "ReadFunc", false, receiver, value)
			var implementation gotypes.Object = fun
			if tt.variable {
				implementation = gotypes.NewVar(token.NoPos, pkg, "ReadValue", fun.Signature())
			}
			var selector gotypes.Object = receiver
			if tt.unbound {
				selector = owner
			}
			wrapper := gogen.NewTemplateRecvMethod(named, token.NoPos, pkg, "Read", implementation)
			ident := &ast.Ident{Name: selector.Name()}
			sel := &ast.Ident{Name: wrapper.Name()}
			info := new(types.Info)
			info.Uses = map[*ast.Ident]gotypes.Object{ident: selector, sel: implementation}
			info.Overloads = map[*ast.Ident]gotypes.Object{sel: wrapper}
			call := &ast.CallExpr{Fun: &ast.SelectorExpr{X: ident, Sel: sel}}

			sig, params := ResolveCallableSignatureForCall(info, call, implementation, nil)
			assert.Same(t, fun.Signature(), sig)
			require.Equal(t, tt.wantParams, params.Len())
			assert.Same(t, value, params.At(params.Len()-1))
			if tt.wantParams == 2 {
				assert.Same(t, receiver, params.At(0))
			}
		})
	}
}

func TestMethodExpressionParams(t *testing.T) {
	base := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, nil, "Base", nil), gotypes.NewStruct(nil, nil), nil)
	embedded := gotypes.NewField(token.NoPos, nil, "Base", base, true)
	outer := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, nil, "Outer", nil), gotypes.NewStruct([]*gotypes.Var{embedded}, nil), nil)
	value := gotypes.NewParam(token.NoPos, nil, "value", gotypes.Typ[gotypes.Int])
	params := gotypes.NewTuple(value)

	withReceiver := MethodExpressionParams(params, outer, base)
	require.Equal(t, 2, withReceiver.Len())
	assert.Same(t, outer, withReceiver.At(0).Type())
	assert.Same(t, value, withReceiver.At(1))
	assert.Nil(t, MethodExpressionParams(params, gotypes.NewPointer(outer), base))
}

func TestResolveCallExprSignatureForTypeSelector(t *testing.T) {
	for _, tt := range []struct {
		name       string
		template   bool
		concrete   bool
		unresolved bool
	}{
		{name: "Static"},
		{name: "Template", template: true},
		{name: "RecordedStaticSignature", concrete: true},
		{name: "UnresolvedTemplate", template: true, unresolved: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := gotypes.NewPackage("example.com/framework", "framework")
			owner := gotypes.NewTypeName(token.NoPos, pkg, "Item", nil)
			named := gotypes.NewNamed(owner, gotypes.NewStruct(nil, nil), nil)
			value := gotypes.NewParam(token.NoPos, pkg, "value", gotypes.Typ[gotypes.Int])
			params := []*gotypes.Var{value}
			if tt.template {
				receiver := gotypes.NewParam(token.NoPos, pkg, "item", named)
				params = append([]*gotypes.Var{receiver}, params...)
			}
			impl := newTestFunc(pkg, "Implementation", false, params...)
			var member *gotypes.Func
			if tt.template {
				member = gogen.NewTemplateRecvMethod(named, token.NoPos, pkg, "Call", impl)
			} else {
				member = gogen.NewStaticMember(named, token.NoPos, pkg, "Call", impl)
			}
			typeIdent := &ast.Ident{Name: "Item"}
			selector := &ast.SelectorExpr{X: typeIdent, Sel: &ast.Ident{Name: "Call"}}
			info := new(types.Info)
			info.Uses = map[*ast.Ident]gotypes.Object{typeIdent: owner, selector.Sel: member}
			call := &ast.CallExpr{Fun: selector}
			wantSig := impl.Signature()
			var recorded gotypes.Type = member.Type()
			if tt.unresolved {
				call.Fun = &ast.IndexExpr{X: selector, Index: &ast.Ident{Name: "string"}}
				recorded = nil
			} else if tt.concrete {
				recorded = wantSig
			}
			info.Types = map[ast.Expr]gotypes.TypeAndValue{call.Fun: {Type: recorded}}
			fun, sig, tuple := ResolveCallExprSignature(info, call)
			if tt.unresolved {
				assert.Nil(t, fun)
				assert.Nil(t, sig)
				assert.Nil(t, tuple)
				return
			}
			assert.Same(t, impl, fun)
			assert.Same(t, wantSig, sig)
			assert.Same(t, wantSig.Params(), tuple)
		})
	}
}

func TestResolveFuncSignatureForUnrecordedInstantiation(t *testing.T) {
	integerConstraint := gotypes.NewInterfaceType(nil, []gotypes.Type{
		gotypes.NewUnion([]*gotypes.Term{gotypes.NewTerm(true, gotypes.Typ[gotypes.Int])}),
	}).Complete()
	for _, tt := range []struct {
		name     string
		args     []string
		concrete bool
	}{
		{name: "FullTypeArguments", args: []string{"int", "string"}, concrete: true},
		{name: "TooManyTypeArguments", args: []string{"int", "string", "bool"}},
		{name: "ValueArgument", args: []string{"int", "true"}},
		{name: "ConstraintMismatch", args: []string{"string", "int"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			first := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, nil, "A", nil), integerConstraint)
			second := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, nil, "B", nil), newTestEmptyInterface())
			param := gotypes.NewParam(token.NoPos, nil, "key", first)
			result := gotypes.NewParam(token.NoPos, nil, "", second)
			decl := gotypes.NewSignatureType(nil, nil, []*gotypes.TypeParam{first, second},
				gotypes.NewTuple(param), gotypes.NewTuple(result), false)
			ident := &ast.Ident{Name: "Choose"}
			fun := gotypes.NewFunc(token.NoPos, nil, ident.Name, decl)
			uses := map[*ast.Ident]gotypes.Object{ident: fun}
			indexed := &ast.IndexListExpr{X: ident}
			for _, name := range tt.args {
				arg := &ast.Ident{Name: name}
				uses[arg] = gotypes.Universe.Lookup(name)
				indexed.Indices = append(indexed.Indices, arg)
			}
			info := newTestTypeInfo(nil, uses)
			sig, params := ResolveFuncSignatureForCall(info, &ast.CallExpr{Fun: indexed}, fun)
			if !tt.concrete {
				assert.Same(t, decl, sig)
				return
			}
			require.NotNil(t, sig)
			require.Equal(t, 1, params.Len())
			require.Equal(t, 1, sig.Results().Len())
			assert.Same(t, gotypes.Typ[gotypes.Int], params.At(0).Type())
			assert.Same(t, gotypes.Typ[gotypes.String], sig.Results().At(0).Type())
		})
	}
}

func TestResolvedCallExprArgs(t *testing.T) {
	t.Run("NilCallExpr", func(t *testing.T) {
		resolved := slices.Collect(ResolvedCallExprArgs(nil, nil))
		assert.Empty(t, resolved)
	})

	t.Run("CallExprWithNilFunction", func(t *testing.T) {
		resolved := slices.Collect(ResolvedCallExprArgs(newTestTypeInfo(nil, nil), &ast.CallExpr{
			Fun: &ast.Ident{Name: "unknown"},
		}))
		assert.Empty(t, resolved)
	})

	t.Run("PositionalArgs", func(t *testing.T) {
		ident := &ast.Ident{Name: "testFunc"}
		arg1 := &ast.Ident{Name: "arg1"}
		arg2 := &ast.Ident{Name: "arg2"}
		expr := &ast.CallExpr{
			Fun:  ident,
			Args: []ast.Expr{arg1, arg2},
		}

		pkg := gotypes.NewPackage("test", "test")
		param1 := gotypes.NewParam(token.NoPos, pkg, "p1", gotypes.Typ[gotypes.Int])
		param2 := gotypes.NewParam(token.NoPos, pkg, "p2", gotypes.Typ[gotypes.String])
		params := gotypes.NewTuple(param1, param2)
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "testFunc", sig)
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})

		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, expr))

		require.Len(t, resolved, 2)
		assert.Equal(t, ResolvedCallExprArgPositional, resolved[0].Kind)
		assert.Equal(t, fun, resolved[0].Fun)
		assert.Equal(t, params, resolved[0].Params)
		assert.Equal(t, param1, resolved[0].Param)
		assert.Equal(t, 0, resolved[0].ParamIndex)
		assert.Equal(t, arg1, resolved[0].Arg)
		assert.Equal(t, 0, resolved[0].ArgIndex)
		assert.Equal(t, gotypes.Typ[gotypes.Int], resolved[0].ExpectedType)
		assert.Equal(t, ResolvedCallExprArgPositional, resolved[1].Kind)
		assert.Equal(t, param2, resolved[1].Param)
		assert.Equal(t, 1, resolved[1].ParamIndex)
		assert.Equal(t, arg2, resolved[1].Arg)
		assert.Equal(t, 1, resolved[1].ArgIndex)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[1].ExpectedType)

		single, ok := ResolveCallExprArg(typeInfo, expr, arg2)
		require.True(t, ok)
		assert.Equal(t, param2, single.Param)
		assert.Equal(t, 1, single.ArgIndex)
		assert.Equal(t, gotypes.Typ[gotypes.String], single.ExpectedType)
		_, ok = ResolveCallExprArg(typeInfo, expr, &ast.Ident{Name: "missing"})
		assert.False(t, ok)
	})

	t.Run("Autoclosure", func(t *testing.T) {
		ident := &ast.Ident{Name: "waitUntil"}
		arg := &ast.Ident{Name: "condition"}
		expr := &ast.CallExpr{Fun: ident, Args: []ast.Expr{arg}}

		pkg := gotypes.NewPackage("test", "test")
		param := gotypes.NewParam(
			token.NoPos,
			pkg,
			"__xgo_autoclosure_condition",
			newTestAutoclosureType(pkg, gotypes.Typ[gotypes.Bool]),
		)
		fun := newTestFunc(pkg, "waitUntil", false, param)
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})

		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, expr))

		require.Len(t, resolved, 1)
		assert.Equal(t, param, resolved[0].Param)
		assert.Equal(t, gotypes.Typ[gotypes.Bool], resolved[0].ExpectedType)
	})

	t.Run("VariadicFunction", func(t *testing.T) {
		ident := &ast.Ident{Name: "testFunc"}
		arg1 := &ast.Ident{Name: "arg1"}
		arg2 := &ast.Ident{Name: "arg2"}
		arg3 := &ast.Ident{Name: "arg3"}
		expr := &ast.CallExpr{
			Fun:  ident,
			Args: []ast.Expr{arg1, arg2, arg3},
		}

		pkg := gotypes.NewPackage("test", "test")
		param1 := gotypes.NewParam(token.NoPos, pkg, "p1", gotypes.Typ[gotypes.Int])
		variadicParam := gotypes.NewParam(token.NoPos, pkg, "args", gotypes.NewSlice(gotypes.Typ[gotypes.String]))
		params := gotypes.NewTuple(param1, variadicParam)
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, true)
		fun := gotypes.NewFunc(token.NoPos, pkg, "testFunc", sig)
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})

		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, expr))

		require.Len(t, resolved, 3)
		assert.Equal(t, 0, resolved[0].ParamIndex)
		assert.Equal(t, 0, resolved[0].ArgIndex)
		assert.Equal(t, gotypes.Typ[gotypes.Int], resolved[0].ExpectedType)
		assert.Equal(t, 1, resolved[1].ParamIndex)
		assert.Equal(t, 1, resolved[1].ArgIndex)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[1].ExpectedType)
		assert.Equal(t, 1, resolved[2].ParamIndex)
		assert.Equal(t, 2, resolved[2].ArgIndex)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[2].ExpectedType)
	})

	t.Run("VariadicFunctionWithEllipsis", func(t *testing.T) {
		ident := &ast.Ident{Name: "testFunc"}
		arg1 := &ast.Ident{Name: "arg1"}
		arg2 := &ast.Ident{Name: "arg2"}
		expr := &ast.CallExpr{
			Fun:      ident,
			Args:     []ast.Expr{arg1, arg2},
			Ellipsis: token.Pos(1),
		}

		pkg := gotypes.NewPackage("test", "test")
		param1 := gotypes.NewParam(token.NoPos, pkg, "p1", gotypes.Typ[gotypes.Int])
		variadicType := gotypes.NewSlice(gotypes.Typ[gotypes.String])
		variadicParam := gotypes.NewParam(token.NoPos, pkg, "args", variadicType)
		params := gotypes.NewTuple(param1, variadicParam)
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, true)
		fun := gotypes.NewFunc(token.NoPos, pkg, "testFunc", sig)
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})

		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, expr))

		require.Len(t, resolved, 2)
		assert.Equal(t, 0, resolved[0].ParamIndex)
		assert.Equal(t, 0, resolved[0].ArgIndex)
		assert.Equal(t, gotypes.Typ[gotypes.Int], resolved[0].ExpectedType)
		assert.Equal(t, 1, resolved[1].ParamIndex)
		assert.Equal(t, 1, resolved[1].ArgIndex)
		assert.Equal(t, variadicType, resolved[1].ExpectedType)
	})

	t.Run("VariadicFunctionWithOptionalParamWithoutKwargs", func(t *testing.T) {
		ident := &ast.Ident{Name: "testFunc"}
		arg1 := &ast.Ident{Name: "arg1"}
		arg2 := &ast.Ident{Name: "arg2"}
		arg3 := &ast.Ident{Name: "arg3"}
		expr := &ast.CallExpr{
			Fun:  ident,
			Args: []ast.Expr{arg1, arg2, arg3},
		}

		pkg := gotypes.NewPackage("test", "test")
		param1 := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_p1", gotypes.Typ[gotypes.Int])
		variadicParam := gotypes.NewParam(token.NoPos, pkg, "args", gotypes.NewSlice(gotypes.Typ[gotypes.String]))
		params := gotypes.NewTuple(param1, variadicParam)
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, true)
		fun := gotypes.NewFunc(token.NoPos, pkg, "testFunc", sig)
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})

		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, expr))

		require.Len(t, resolved, 3)
		assert.Equal(t, param1, resolved[0].Param)
		assert.Equal(t, 0, resolved[0].ParamIndex)
		assert.Equal(t, 0, resolved[0].ArgIndex)
		assert.Equal(t, gotypes.Typ[gotypes.Int], resolved[0].ExpectedType)
		assert.Equal(t, variadicParam, resolved[1].Param)
		assert.Equal(t, 1, resolved[1].ParamIndex)
		assert.Equal(t, 1, resolved[1].ArgIndex)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[1].ExpectedType)
		assert.Equal(t, variadicParam, resolved[2].Param)
		assert.Equal(t, 1, resolved[2].ParamIndex)
		assert.Equal(t, 2, resolved[2].ArgIndex)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[2].ExpectedType)
	})

	t.Run("YieldStopsIteration", func(t *testing.T) {
		ident := &ast.Ident{Name: "testFunc"}
		expr := &ast.CallExpr{
			Fun: ident,
			Args: []ast.Expr{
				&ast.Ident{Name: "arg1"},
				&ast.Ident{Name: "arg2"},
			},
		}

		pkg := gotypes.NewPackage("test", "test")
		param1 := gotypes.NewParam(token.NoPos, pkg, "p1", gotypes.Typ[gotypes.Int])
		param2 := gotypes.NewParam(token.NoPos, pkg, "p2", gotypes.Typ[gotypes.String])
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(param1, param2), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "testFunc", sig)
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})

		callCount := 0
		for range ResolvedCallExprArgs(typeInfo, expr) {
			callCount++
			break
		}
		assert.Equal(t, 1, callCount)
	})

	t.Run("XGoPackageXGotMethod", func(t *testing.T) {
		ident := &ast.Ident{Name: "move"}
		arg := &ast.Ident{Name: "arg1"}
		expr := &ast.CallExpr{
			Fun:  ident,
			Args: []ast.Expr{arg},
		}

		pkg := gotypes.NewPackage("test", "test")
		markAsXGoPackage(pkg)
		owner := gotypes.NewTypeName(token.NoPos, pkg, "Sprite", nil)
		gotypes.NewNamed(owner, gotypes.NewStruct(nil, nil), nil)
		pkg.Scope().Insert(owner)
		recv := gotypes.NewParam(token.NoPos, pkg, "recv", gotypes.Typ[gotypes.Int])
		param := gotypes.NewParam(token.NoPos, pkg, "p1", gotypes.Typ[gotypes.Int])
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(recv, param), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "XGot_Sprite_Move", sig)
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})

		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, expr))

		require.Len(t, resolved, 1)
		assert.Equal(t, param, resolved[0].Param)
		assert.Equal(t, 0, resolved[0].ParamIndex)
		assert.Equal(t, arg, resolved[0].Arg)
		assert.Equal(t, 0, resolved[0].ArgIndex)
	})

	t.Run("FuncExFunction", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		ident := &ast.Ident{Name: "testFunc"}
		fun := gogen.NewOverloadFunc(token.NoPos, pkg, "testFunc",
			gotypes.NewFunc(token.NoPos, pkg, "foo", gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)),
		)
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})

		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun:  ident,
			Args: []ast.Expr{&ast.Ident{Name: "arg"}},
		}))
		assert.Empty(t, resolved)
	})

	t.Run("XGoPackageXGoxMethod", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		markAsXGoPackage(pkg)
		owner := gotypes.NewTypeName(token.NoPos, pkg, "Sprite", nil)
		gotypes.NewNamed(owner, gotypes.NewStruct(nil, nil), nil)
		pkg.Scope().Insert(owner)
		constraint := gotypes.NewInterfaceType(nil, nil)
		constraint.Complete()
		typeParam := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "T", nil), constraint)
		recv := gotypes.NewParam(token.NoPos, pkg, "recv", gotypes.Typ[gotypes.Int])
		param := gotypes.NewParam(token.NoPos, pkg, "p1", gotypes.Typ[gotypes.String])
		sig := gotypes.NewSignatureType(nil, nil, []*gotypes.TypeParam{typeParam}, gotypes.NewTuple(recv, param), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "XGot_Sprite_XGox_Move", sig)

		ident := &ast.Ident{Name: "move"}
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun: ident,
			Args: []ast.Expr{
				&ast.Ident{Name: "int"},
				&ast.BasicLit{Kind: token.STRING, Value: `"ok"`},
			},
		}))

		require.Len(t, resolved, 2)
		assert.Equal(t, "T", resolved[0].Param.Name())
		assert.Equal(t, 0, resolved[0].ParamIndex)
		assert.Equal(t, 0, resolved[0].ArgIndex)
		assert.True(t, resolved[0].IsTypeArg())
		assert.Equal(t, "p1", resolved[1].Param.Name())
		assert.Equal(t, 1, resolved[1].ParamIndex)
		assert.Equal(t, 1, resolved[1].ArgIndex)
		assert.False(t, resolved[1].IsTypeArg())
	})

	t.Run("XGoPackagePartialXGoxFunction", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		markAsXGoPackage(pkg)
		constraint := gotypes.NewInterfaceType(nil, nil)
		constraint.Complete()
		toTypeParam := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "To", nil), constraint)
		fromTypeParam := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "From", nil), constraint)
		srcParam := gotypes.NewParam(token.NoPos, pkg, "src", fromTypeParam)
		sig := gotypes.NewSignatureType(
			nil,
			nil,
			[]*gotypes.TypeParam{toTypeParam, fromTypeParam},
			gotypes.NewTuple(srcParam),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", toTypeParam)),
			false,
		)
		fun := gotypes.NewFunc(token.NoPos, pkg, "XGox_Convert", sig)

		funIdent := &ast.Ident{Name: "convert"}
		typeArg := &ast.Ident{Name: "string"}
		valueArg := &ast.BasicLit{Kind: token.INT, Value: "100"}
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			funIdent: fun,
			typeArg:  gotypes.Universe.Lookup("string"),
		})
		typeInfo.Types[valueArg] = gotypes.TypeAndValue{}

		call := &ast.CallExpr{
			Fun:  funIdent,
			Args: []ast.Expr{typeArg, valueArg},
		}
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, call))

		require.Len(t, resolved, 2)
		assert.Equal(t, "To", resolved[0].Param.Name())
		assert.Equal(t, 0, resolved[0].ParamIndex)
		assert.Equal(t, typeArg, resolved[0].Arg)
		assert.True(t, resolved[0].IsTypeArg())
		assert.Equal(t, "src", resolved[1].Param.Name())
		assert.Equal(t, 1, resolved[1].ParamIndex)
		assert.Equal(t, valueArg, resolved[1].Arg)
		assert.Equal(t, fromTypeParam, resolved[1].ExpectedType)
		assert.False(t, resolved[1].IsTypeArg())

		typeInfo.Types[valueArg] = gotypes.TypeAndValue{Type: gotypes.Typ[gotypes.UntypedInt]}
		resolved = slices.Collect(ResolvedCallExprArgs(typeInfo, call))
		require.Len(t, resolved, 2)
		assert.Same(t, srcParam, resolved[1].Param.Origin())
		assert.Same(t, gotypes.Typ[gotypes.Int], resolved[1].ExpectedType)

		contextual, ok := ResolveCallExprArg(typeInfo, call, valueArg)
		require.True(t, ok)
		assert.Same(t, fromTypeParam, contextual.ExpectedType)
	})

	t.Run("XGoPackagePartialXGoxTemplateMethod", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		markAsXGoPackage(pkg)
		owner := gotypes.NewTypeName(token.NoPos, pkg, "App", nil)
		gotypes.NewNamed(owner, gotypes.NewStruct(nil, nil), nil)
		pkg.Scope().Insert(owner)
		constraint := gotypes.NewInterfaceType(nil, nil)
		constraint.Complete()
		inTypeParam := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "InT", nil), constraint)
		appTypeParam := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "AppT", nil), constraint)
		recvParam := gotypes.NewParam(token.NoPos, pkg, "app", appTypeParam)
		callbackType := gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "input", gotypes.NewPointer(inTypeParam))),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", appTypeParam)),
			false,
		)
		callbackParam := gotypes.NewParam(token.NoPos, pkg, "callback", callbackType)
		sig := gotypes.NewSignatureType(
			nil,
			nil,
			[]*gotypes.TypeParam{inTypeParam, appTypeParam},
			gotypes.NewTuple(recvParam, callbackParam),
			nil,
			false,
		)
		fun := gotypes.NewFunc(token.NoPos, pkg, "XGot_App_XGox_OnCall", sig)

		funIdent := &ast.Ident{Name: "onCall"}
		recvIdent := &ast.Ident{Name: "app"}
		typeArg := &ast.Ident{Name: "Start"}
		valueArg := &ast.Ident{Name: "handler"}
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			funIdent: fun,
			typeArg:  gotypes.NewTypeName(token.NoPos, pkg, "Start", gotypes.Typ[gotypes.Int]),
		})
		typeInfo.Types[valueArg] = gotypes.TypeAndValue{}

		call := &ast.CallExpr{
			Fun:  &ast.SelectorExpr{X: recvIdent, Sel: funIdent},
			Args: []ast.Expr{typeArg, valueArg},
		}
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, call))

		require.Len(t, resolved, 2)
		assert.Equal(t, "InT", resolved[0].Param.Name())
		assert.True(t, resolved[0].IsTypeArg())
		assert.Same(t, callbackParam, resolved[1].Param.Origin())
		assert.Equal(t, "func(input *int) AppT", gotypes.TypeString(resolved[1].ExpectedType, nil))
		assert.False(t, resolved[1].IsTypeArg())

		typeInfo.Uses[recvIdent] = gotypes.NewVar(token.NoPos, pkg, "app", owner.Type())
		resolved = slices.Collect(ResolvedCallExprArgs(typeInfo, call))
		require.Len(t, resolved, 2)
		assert.Same(t, callbackParam, resolved[1].Param.Origin())
		assert.Equal(t, "func(input *int) test.App", gotypes.TypeString(resolved[1].ExpectedType, nil))
	})

	t.Run("MoreArgsThanParams", func(t *testing.T) {
		ident := &ast.Ident{Name: "testFunc"}
		expr := &ast.CallExpr{
			Fun: ident,
			Args: []ast.Expr{
				&ast.Ident{Name: "arg1"},
				&ast.Ident{Name: "arg2"},
				&ast.Ident{Name: "arg3"},
			},
		}

		pkg := gotypes.NewPackage("test", "test")
		param := gotypes.NewParam(token.NoPos, pkg, "p1", gotypes.Typ[gotypes.Int])
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(param), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "testFunc", sig)
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})

		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, expr))

		require.Len(t, resolved, 1)
		assert.Equal(t, 0, resolved[0].ParamIndex)
		assert.Equal(t, 0, resolved[0].ArgIndex)
	})

	t.Run("NoParams", func(t *testing.T) {
		ident := &ast.Ident{Name: "testFunc"}
		pkg := gotypes.NewPackage("test", "test")
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "testFunc", sig)
		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})

		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun:  ident,
			Args: []ast.Expr{&ast.Ident{Name: "arg1"}},
		}))
		assert.Empty(t, resolved)
	})

	t.Run("StructKwargs", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}
		valueArg := &ast.Ident{Name: "countValue"}
		kwarg := &ast.KwargExpr{
			Name:  &ast.Ident{Name: "count"},
			Value: valueArg,
		}

		pkg := gotypes.NewPackage("main", "main")
		countField := gotypes.NewField(token.NoPos, pkg, "Count", gotypes.Typ[gotypes.Int], false)
		nameField := gotypes.NewField(token.NoPos, pkg, "Name", gotypes.Typ[gotypes.String], false)
		optsType := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
			gotypes.NewStruct([]*gotypes.Var{countField, nameField}, nil),
			nil,
		)
		param := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", optsType)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(param), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun:    ident,
			Kwargs: []*ast.KwargExpr{kwarg},
		}))
		require.Len(t, resolved, 1)
		assert.Equal(t, ResolvedCallExprArgKeyword, resolved[0].Kind)
		assert.Equal(t, kwarg, resolved[0].Kwarg)
		assert.Equal(t, gotypes.Typ[gotypes.Int], resolved[0].ExpectedType)
		require.NotNil(t, resolved[0].KwargTarget)
		assert.Equal(t, ResolvedCallExprKwargTargetStructField, resolved[0].KwargTarget.Kind)
		assert.Equal(t, "count", resolved[0].KwargTarget.Name)
		assert.Equal(t, countField, resolved[0].KwargTarget.Field)
	})

	t.Run("MapKwargsOnVariadicFunction", func(t *testing.T) {
		ident := &ast.Ident{Name: "process"}
		firstArg := &ast.Ident{Name: "arg1"}
		kwargValue := &ast.Ident{Name: "nameValue"}
		kwarg := &ast.KwargExpr{
			Name:  &ast.Ident{Name: "name"},
			Value: kwargValue,
		}

		pkg := gotypes.NewPackage("main", "main")
		optsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", gotypes.NewMap(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.String]))
		argsParam := gotypes.NewParam(token.NoPos, pkg, "args", gotypes.NewSlice(gotypes.Typ[gotypes.Int]))
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(optsParam, argsParam), nil, true)
		fun := gotypes.NewFunc(token.NoPos, pkg, "process", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun:    ident,
			Args:   []ast.Expr{firstArg},
			Kwargs: []*ast.KwargExpr{kwarg},
		}))
		require.Len(t, resolved, 2)
		assert.Equal(t, ResolvedCallExprArgPositional, resolved[0].Kind)
		assert.Equal(t, argsParam, resolved[0].Param)
		assert.Equal(t, 1, resolved[0].ParamIndex)
		assert.Zero(t, resolved[0].ArgIndex)
		assert.Equal(t, gotypes.Typ[gotypes.Int], resolved[0].ExpectedType)
		assert.Equal(t, ResolvedCallExprArgKeyword, resolved[1].Kind)
		assert.Equal(t, optsParam, resolved[1].Param)
		assert.Zero(t, resolved[1].ParamIndex)
		assert.Equal(t, 1, resolved[1].ArgIndex)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[1].ExpectedType)
		require.NotNil(t, resolved[1].KwargTarget)
		assert.Equal(t, ResolvedCallExprKwargTargetMap, resolved[1].KwargTarget.Kind)
	})

	t.Run("MapKwargsOnVariadicFunctionWithEllipsis", func(t *testing.T) {
		ident := &ast.Ident{Name: "process"}
		arg := &ast.Ident{Name: "args"}
		kwargValue := &ast.Ident{Name: "nameValue"}
		kwarg := &ast.KwargExpr{
			Name:  &ast.Ident{Name: "name"},
			Value: kwargValue,
		}

		pkg := gotypes.NewPackage("main", "main")
		optsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", gotypes.NewMap(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.String]))
		argsType := gotypes.NewSlice(gotypes.Typ[gotypes.Int])
		argsParam := gotypes.NewParam(token.NoPos, pkg, "args", argsType)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(optsParam, argsParam), nil, true)
		fun := gotypes.NewFunc(token.NoPos, pkg, "process", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun:      ident,
			Args:     []ast.Expr{arg},
			Ellipsis: token.Pos(1),
			Kwargs:   []*ast.KwargExpr{kwarg},
		}))
		require.Len(t, resolved, 2)
		assert.Equal(t, ResolvedCallExprArgPositional, resolved[0].Kind)
		assert.Equal(t, argsParam, resolved[0].Param)
		assert.Equal(t, 1, resolved[0].ParamIndex)
		assert.Zero(t, resolved[0].ArgIndex)
		assert.Equal(t, argsType, resolved[0].ExpectedType)
		assert.Equal(t, ResolvedCallExprArgKeyword, resolved[1].Kind)
		assert.Equal(t, optsParam, resolved[1].Param)
		assert.Zero(t, resolved[1].ParamIndex)
		assert.Equal(t, 1, resolved[1].ArgIndex)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[1].ExpectedType)
		require.NotNil(t, resolved[1].KwargTarget)
		assert.Equal(t, ResolvedCallExprKwargTargetMap, resolved[1].KwargTarget.Kind)
	})

	t.Run("MapKwargsWithNamedStringKey", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}
		kwarg := &ast.KwargExpr{
			Name:  &ast.Ident{Name: "count"},
			Value: &ast.Ident{Name: "countValue"},
		}

		pkg := gotypes.NewPackage("main", "main")
		keyType := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, pkg, "Key", nil), gotypes.Typ[gotypes.String], nil)
		optsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", gotypes.NewMap(keyType, gotypes.Typ[gotypes.Int]))
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(optsParam), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun:    ident,
			Kwargs: []*ast.KwargExpr{kwarg},
		}))
		require.Len(t, resolved, 1)
		assert.Equal(t, gotypes.Typ[gotypes.Int], resolved[0].ExpectedType)
		require.NotNil(t, resolved[0].KwargTarget)
		assert.Equal(t, ResolvedCallExprKwargTargetMap, resolved[0].KwargTarget.Kind)
	})

	t.Run("MapKwargsWithAnyKey", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}
		kwarg := &ast.KwargExpr{
			Name:  &ast.Ident{Name: "count"},
			Value: &ast.Ident{Name: "countValue"},
		}

		pkg := gotypes.NewPackage("main", "main")
		anyIface := gotypes.NewInterfaceType(nil, nil)
		anyIface.Complete()
		optsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", gotypes.NewMap(anyIface, gotypes.Typ[gotypes.Int]))
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(optsParam), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun:    ident,
			Kwargs: []*ast.KwargExpr{kwarg},
		}))
		require.Len(t, resolved, 1)
		assert.Equal(t, gotypes.Typ[gotypes.Int], resolved[0].ExpectedType)
		require.NotNil(t, resolved[0].KwargTarget)
		assert.Equal(t, ResolvedCallExprKwargTargetMap, resolved[0].KwargTarget.Kind)
	})

	t.Run("MapKwargsWithIntKeyStayUnresolved", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}
		kwarg := &ast.KwargExpr{
			Name:  &ast.Ident{Name: "count"},
			Value: &ast.Ident{Name: "countValue"},
		}

		pkg := gotypes.NewPackage("main", "main")
		optsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", gotypes.NewMap(gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.String]))
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(optsParam), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun:    ident,
			Kwargs: []*ast.KwargExpr{kwarg},
		}))
		require.Len(t, resolved, 1)
		assert.Nil(t, resolved[0].ExpectedType)
		assert.Nil(t, resolved[0].KwargTarget)
	})

	t.Run("AnyKwargsFallbackMap", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}
		kwarg := &ast.KwargExpr{
			Name:  &ast.Ident{Name: "count"},
			Value: &ast.Ident{Name: "countValue"},
		}

		pkg := gotypes.NewPackage("main", "main")
		anyIface := gotypes.NewInterfaceType(nil, nil)
		anyIface.Complete()
		optsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", anyIface)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(optsParam), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun:    ident,
			Kwargs: []*ast.KwargExpr{kwarg},
		}))
		require.Len(t, resolved, 1)
		assert.True(t, gotypes.Identical(anyType(), resolved[0].ExpectedType))
		require.NotNil(t, resolved[0].KwargTarget)
		assert.Equal(t, ResolvedCallExprKwargTargetMap, resolved[0].KwargTarget.Kind)
		assert.Equal(t, "count", resolved[0].KwargTarget.Name)
	})

	t.Run("AliasAnyKwargsFallbackMap", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}
		kwarg := &ast.KwargExpr{
			Name:  &ast.Ident{Name: "count"},
			Value: &ast.Ident{Name: "countValue"},
		}

		pkg := gotypes.NewPackage("main", "main")
		anyIface := gotypes.NewInterfaceType(nil, nil)
		anyIface.Complete()
		anyAlias := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "AnyAlias", nil), anyIface)
		optsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", anyAlias)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(optsParam), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun:    ident,
			Kwargs: []*ast.KwargExpr{kwarg},
		}))
		require.Len(t, resolved, 1)
		assert.True(t, gotypes.Identical(anyType(), resolved[0].ExpectedType))
		require.NotNil(t, resolved[0].KwargTarget)
		assert.Equal(t, ResolvedCallExprKwargTargetMap, resolved[0].KwargTarget.Kind)
	})

	t.Run("AliasStructKwargsDoNotUseLocalUnexportedFields", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}
		kwarg := &ast.KwargExpr{
			Name:  &ast.Ident{Name: "count"},
			Value: &ast.Ident{Name: "countValue"},
		}

		pkg := gotypes.NewPackage("main", "main")
		countField := gotypes.NewField(token.NoPos, pkg, "count", gotypes.Typ[gotypes.Int], false)
		optsType := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
			gotypes.NewStruct([]*gotypes.Var{countField}, nil),
			nil,
		)
		optsAlias := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "OptionsAlias", nil), optsType)
		param := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", optsAlias)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(param), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		typeInfo.Pkg = pkg
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun:    ident,
			Kwargs: []*ast.KwargExpr{kwarg},
		}))
		require.Len(t, resolved, 1)
		assert.Nil(t, resolved[0].ExpectedType)
		assert.Nil(t, resolved[0].KwargTarget)
	})

	t.Run("AliasPointerStructKwargsUseLocalUnexportedFields", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}
		kwarg := &ast.KwargExpr{
			Name:  &ast.Ident{Name: "count"},
			Value: &ast.Ident{Name: "countValue"},
		}

		pkg := gotypes.NewPackage("main", "main")
		countField := gotypes.NewField(token.NoPos, pkg, "count", gotypes.Typ[gotypes.Int], false)
		optsType := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
			gotypes.NewStruct([]*gotypes.Var{countField}, nil),
			nil,
		)
		optsAlias := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "OptionsPtrAlias", nil), gotypes.NewPointer(optsType))
		param := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", optsAlias)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(param), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		typeInfo.Pkg = pkg
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun:    ident,
			Kwargs: []*ast.KwargExpr{kwarg},
		}))
		require.Len(t, resolved, 1)
		assert.Equal(t, gotypes.Typ[gotypes.Int], resolved[0].ExpectedType)
		require.NotNil(t, resolved[0].KwargTarget)
		assert.Equal(t, ResolvedCallExprKwargTargetStructField, resolved[0].KwargTarget.Kind)
		assert.Equal(t, countField, resolved[0].KwargTarget.Field)
	})

	t.Run("PointerAliasStructKwargsDoNotUseLocalUnexportedFields", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}
		kwarg := &ast.KwargExpr{
			Name:  &ast.Ident{Name: "count"},
			Value: &ast.Ident{Name: "countValue"},
		}

		pkg := gotypes.NewPackage("main", "main")
		countField := gotypes.NewField(token.NoPos, pkg, "count", gotypes.Typ[gotypes.Int], false)
		optsType := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
			gotypes.NewStruct([]*gotypes.Var{countField}, nil),
			nil,
		)
		optsAlias := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "OptionsAlias", nil), optsType)
		param := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", gotypes.NewPointer(optsAlias))
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(param), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		typeInfo.Pkg = pkg
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun:    ident,
			Kwargs: []*ast.KwargExpr{kwarg},
		}))
		require.Len(t, resolved, 1)
		assert.Nil(t, resolved[0].ExpectedType)
		assert.Nil(t, resolved[0].KwargTarget)
	})

	t.Run("NamedEmptyInterfaceKwargsStayUnresolved", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}
		receiver := &ast.Ident{Name: "client"}
		kwarg := &ast.KwargExpr{
			Name:  &ast.Ident{Name: "count"},
			Value: &ast.Ident{Name: "countValue"},
		}

		pkg := gotypes.NewPackage("main", "main")
		paramsTypeName := gotypes.NewTypeName(token.NoPos, pkg, "Params", nil)
		paramsNamed := gotypes.NewNamed(paramsTypeName, nil, nil)
		emptyIface := gotypes.NewInterfaceType(nil, nil)
		emptyIface.Complete()
		paramsNamed.SetUnderlying(emptyIface)
		optsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", paramsNamed)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(optsParam), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   receiver,
				Sel: ident,
			},
			Kwargs: []*ast.KwargExpr{kwarg},
		}))
		require.Len(t, resolved, 1)
		assert.Nil(t, resolved[0].ExpectedType)
		assert.Nil(t, resolved[0].KwargTarget)
	})

	t.Run("AliasInterfaceKwargsStayUnresolved", func(t *testing.T) {
		ident := &ast.Ident{Name: "Complete"}
		receiver := &ast.Ident{Name: "client"}
		kwarg := &ast.KwargExpr{
			Name:  &ast.Ident{Name: "maxTokens"},
			Value: &ast.Ident{Name: "maxTokensValue"},
		}

		pkg := gotypes.NewPackage("main", "main")
		paramsTypeName := gotypes.NewTypeName(token.NoPos, pkg, "Params", nil)
		paramsNamed := gotypes.NewNamed(paramsTypeName, nil, nil)
		maxTokensMethod := gotypes.NewFunc(token.NoPos, pkg, "MaxTokens", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "n", gotypes.Typ[gotypes.Int64])),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", paramsNamed)),
			false,
		))
		iface := gotypes.NewInterfaceType([]*gotypes.Func{maxTokensMethod}, nil)
		iface.Complete()
		paramsNamed.SetUnderlying(iface)
		paramsAlias := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "ParamsAlias", nil), paramsNamed)

		promptParam := gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.Typ[gotypes.String])
		paramsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsAlias)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(promptParam, paramsParam), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "Complete", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   receiver,
				Sel: ident,
			},
			Args:   []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"hi"`}},
			Kwargs: []*ast.KwargExpr{kwarg},
		}))
		require.Len(t, resolved, 2)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[0].ExpectedType)
		assert.Nil(t, resolved[1].ExpectedType)
		assert.Nil(t, resolved[1].KwargTarget)
	})

	t.Run("InterfaceKwargs", func(t *testing.T) {
		ident := &ast.Ident{Name: "Complete"}
		receiver := &ast.Ident{Name: "client"}
		call := &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   receiver,
				Sel: ident,
			},
			Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"hi"`}},
			Kwargs: []*ast.KwargExpr{
				newTestKwarg("maxTokens", "maxTokensValue"),
			},
		}

		pkg := gotypes.NewPackage("main", "main")
		paramsNamed := newTestNamedInterface(pkg, "Params")
		maxTokensMethod := newTestSelfReturningMethod(pkg, "MaxTokens", paramsNamed, "n", gotypes.Typ[gotypes.Int64], false)
		setTestNamedInterfaceMethods(paramsNamed, maxTokensMethod)
		promptParam := gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.Typ[gotypes.String])
		paramsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed)
		fun := newTestFunc(pkg, "Complete", false, promptParam, paramsParam)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		addInterfaceKwargFactory(typeInfo, receiver, "Client", paramsNamed)
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, call))
		require.Len(t, resolved, 2)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[0].ExpectedType)
		assert.Equal(t, gotypes.Typ[gotypes.Int64], resolved[1].ExpectedType)
		require.NotNil(t, resolved[1].KwargTarget)
		assert.Equal(t, ResolvedCallExprKwargTargetInterfaceMethod, resolved[1].KwargTarget.Kind)
		assert.Equal(t, "maxTokens", resolved[1].KwargTarget.Name)
		assert.Equal(t, maxTokensMethod, resolved[1].KwargTarget.Method)
	})

	t.Run("VariadicInterfaceKwargs", func(t *testing.T) {
		ident := &ast.Ident{Name: "Complete"}
		receiver := &ast.Ident{Name: "client"}
		call := &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   receiver,
				Sel: ident,
			},
			Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"hi"`}},
			Kwargs: []*ast.KwargExpr{
				newTestKwarg("system", "systemValue"),
			},
		}

		pkg := gotypes.NewPackage("main", "main")
		paramsNamed := newTestNamedInterface(pkg, "Params")
		systemMethod := newTestSelfReturningMethod(pkg, "System", paramsNamed, "prompt", gotypes.NewSlice(gotypes.Typ[gotypes.String]), true)
		setTestNamedInterfaceMethods(paramsNamed, systemMethod)
		promptParam := gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.Typ[gotypes.String])
		paramsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed)
		fun := newTestFunc(pkg, "Complete", false, promptParam, paramsParam)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		addInterfaceKwargFactory(typeInfo, receiver, "Client", paramsNamed)
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, call))
		require.Len(t, resolved, 2)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[0].ExpectedType)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[1].ExpectedType)
		require.NotNil(t, resolved[1].KwargTarget)
		assert.Equal(t, ResolvedCallExprKwargTargetInterfaceMethod, resolved[1].KwargTarget.Kind)
		assert.Equal(t, "system", resolved[1].KwargTarget.Name)
		assert.Equal(t, systemMethod, resolved[1].KwargTarget.Method)
	})

	t.Run("FreeFunctionInterfaceKwargs", func(t *testing.T) {
		ident := &ast.Ident{Name: "Complete"}
		call := &ast.CallExpr{
			Fun:  ident,
			Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"hi"`}},
			Kwargs: []*ast.KwargExpr{
				newTestKwarg("maxTokens", "maxTokensValue"),
			},
		}

		pkg := gotypes.NewPackage("main", "main")
		paramsNamed := newTestNamedInterface(pkg, "Params")
		maxTokensMethod := newTestSelfReturningMethod(pkg, "MaxTokens", paramsNamed, "n", gotypes.Typ[gotypes.Int64], false)
		setTestNamedInterfaceMethods(paramsNamed, maxTokensMethod)
		promptParam := gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.Typ[gotypes.String])
		paramsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed)
		fun := newTestFunc(pkg, "Complete", false, promptParam, paramsParam)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, call))
		require.Len(t, resolved, 2)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[0].ExpectedType)
		assert.Nil(t, resolved[1].ExpectedType)
		assert.Nil(t, resolved[1].KwargTarget)
	})

	t.Run("InterfaceKwargsWithoutFactoryStayUnresolved", func(t *testing.T) {
		ident := &ast.Ident{Name: "Complete"}
		receiver := &ast.Ident{Name: "client"}
		call := &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   receiver,
				Sel: ident,
			},
			Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"hi"`}},
			Kwargs: []*ast.KwargExpr{
				newTestKwarg("maxTokens", "maxTokensValue"),
			},
		}

		pkg := gotypes.NewPackage("main", "main")
		paramsNamed := newTestNamedInterface(pkg, "Params")
		maxTokensMethod := newTestSelfReturningMethod(pkg, "MaxTokens", paramsNamed, "n", gotypes.Typ[gotypes.Int64], false)
		setTestNamedInterfaceMethods(paramsNamed, maxTokensMethod)
		promptParam := gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.Typ[gotypes.String])
		paramsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed)
		fun := newTestFunc(pkg, "Complete", false, promptParam, paramsParam)
		clientNamed := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "Client", nil),
			gotypes.NewStruct(nil, nil),
			nil,
		)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		typeInfo.Types[receiver] = gotypes.TypeAndValue{Type: clientNamed}
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, call))
		require.Len(t, resolved, 2)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[0].ExpectedType)
		assert.Nil(t, resolved[1].ExpectedType)
		assert.Nil(t, resolved[1].KwargTarget)
	})

	t.Run("InterfaceSetKwargs", func(t *testing.T) {
		ident := &ast.Ident{Name: "Complete"}
		receiver := &ast.Ident{Name: "client"}
		call := &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   receiver,
				Sel: ident,
			},
			Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"hi"`}},
			Kwargs: []*ast.KwargExpr{
				newTestKwarg("custom", "customValue"),
			},
		}

		pkg := gotypes.NewPackage("main", "main")
		paramsNamed := newTestNamedInterface(pkg, "Params")
		anyIface := newTestEmptyInterface()
		setMethod := gotypes.NewFunc(token.NoPos, pkg, "Set", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(
				gotypes.NewParam(token.NoPos, pkg, "name", gotypes.Typ[gotypes.String]),
				gotypes.NewParam(token.NoPos, pkg, "value", anyIface),
			),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", paramsNamed)),
			false,
		))
		setTestNamedInterfaceMethods(paramsNamed, setMethod)
		promptParam := gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.Typ[gotypes.String])
		paramsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed)
		fun := newTestFunc(pkg, "Complete", false, promptParam, paramsParam)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		addInterfaceKwargFactory(typeInfo, receiver, "Client", paramsNamed)
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, call))
		require.Len(t, resolved, 2)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[0].ExpectedType)
		require.NotNil(t, resolved[1].KwargTarget)
		assert.Equal(t, ResolvedCallExprKwargTargetInterfaceSet, resolved[1].KwargTarget.Kind)
		assert.Equal(t, "custom", resolved[1].KwargTarget.Name)
		assert.True(t, gotypes.Identical(anyType(), resolved[1].ExpectedType))
	})

	t.Run("InterfaceSetWithAliasParamTypesStaysUnresolved", func(t *testing.T) {
		ident := &ast.Ident{Name: "Complete"}
		receiver := &ast.Ident{Name: "client"}
		call := &ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X:   receiver,
				Sel: ident,
			},
			Args: []ast.Expr{&ast.BasicLit{Kind: token.STRING, Value: `"hi"`}},
			Kwargs: []*ast.KwargExpr{
				newTestKwarg("custom", "customValue"),
			},
		}

		pkg := gotypes.NewPackage("main", "main")
		paramsNamed := newTestNamedInterface(pkg, "Params")
		anyIface := newTestEmptyInterface()
		keyAlias := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "Key", nil), gotypes.Typ[gotypes.String])
		anyAlias := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "Any", nil), anyIface)
		setMethod := gotypes.NewFunc(token.NoPos, pkg, "Set", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(
				gotypes.NewParam(token.NoPos, pkg, "name", keyAlias),
				gotypes.NewParam(token.NoPos, pkg, "value", anyAlias),
			),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", paramsNamed)),
			false,
		))
		setTestNamedInterfaceMethods(paramsNamed, setMethod)
		promptParam := gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.Typ[gotypes.String])
		paramsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed)
		fun := newTestFunc(pkg, "Complete", false, promptParam, paramsParam)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		addInterfaceKwargFactory(typeInfo, receiver, "Client", paramsNamed)
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, call))
		require.Len(t, resolved, 2)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[0].ExpectedType)
		assert.Nil(t, resolved[1].ExpectedType)
		assert.Nil(t, resolved[1].KwargTarget)
	})
}

func TestRenamedStructKwargName(t *testing.T) {
	pkg := gotypes.NewPackage("main", "main")
	field := gotypes.NewField(token.NoPos, pkg, "Total", gotypes.Typ[gotypes.Int], false)
	local := gotypes.NewField(token.NoPos, pkg, "count", gotypes.Typ[gotypes.Int], false)
	shadowed := gotypes.NewNamed(
		gotypes.NewTypeName(token.NoPos, pkg, "Shadowed", nil),
		gotypes.NewStruct([]*gotypes.Var{local, field}, nil),
		nil,
	)
	unshadowed := gotypes.NewNamed(
		gotypes.NewTypeName(token.NoPos, pkg, "Unshadowed", nil),
		gotypes.NewStruct([]*gotypes.Var{field, local}, nil),
		nil,
	)
	alias := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "Alias", nil), shadowed)
	for _, tt := range []struct {
		name     string
		receiver gotypes.Type
		newName  string
		want     string
	}{
		{"EarlierLocalField", shadowed, "Count", "Count"},
		{"LaterLocalField", unshadowed, "Count", "count"},
		{"PointerToLocalType", gotypes.NewPointer(shadowed), "Count", "Count"},
		{"Alias", alias, "Count", "count"},
		{"ReservedAlias", shadowed, "For", "For"},
		{"NonReversibleCase", shadowed, "\u212a", "\u212a"},
		{"ShortAlias", shadowed, "\u2c7e", "\u2c7e"},
		{"LongAlias", shadowed, "\u2c7eX", "\u023fX"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, RenamedStructKwargName(tt.receiver, field, tt.newName))
		})
	}
}

func TestListResolvedCallExprKwargTargets(t *testing.T) {
	t.Run("StructSkipsLaterLocalFieldName", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		exportedField := gotypes.NewField(token.NoPos, pkg, "Count", gotypes.Typ[gotypes.Int], false)
		localField := gotypes.NewField(token.NoPos, pkg, "count", gotypes.Typ[gotypes.String], false)
		optsType := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
			gotypes.NewStruct([]*gotypes.Var{exportedField, localField}, nil),
			nil,
		)
		kwarg := &ResolvedCallExprKwarg{
			Param: gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", optsType),
			info:  &types.Info{Pkg: pkg},
		}

		targets := ListResolvedCallExprKwargTargets(kwarg)
		require.Len(t, targets, 1)
		assert.Equal(t, "count", targets[0].Name)
		assert.Equal(t, exportedField, targets[0].Field)
	})

	t.Run("StructUsesDeclaredSpellingOfLaterExportedField", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/library", "library")
		localField := gotypes.NewField(token.NoPos, pkg, "count", gotypes.Typ[gotypes.String], false)
		exportedField := gotypes.NewField(token.NoPos, pkg, "Count", gotypes.Typ[gotypes.Int], false)
		optsType := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
			gotypes.NewStruct([]*gotypes.Var{localField, exportedField}, nil),
			nil,
		)
		kwarg := &ResolvedCallExprKwarg{
			Param: gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", optsType),
			info:  &types.Info{Pkg: pkg},
		}

		targets := ListResolvedCallExprKwargTargets(kwarg)
		require.Len(t, targets, 2)
		assert.Equal(t, "count", targets[0].Name)
		assert.Same(t, localField, targets[0].Field)
		assert.Equal(t, "Count", targets[1].Name)
		assert.Same(t, exportedField, targets[1].Field)
	})

	t.Run("StructUsesExportedFieldFromDistinctPackage", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		local := gotypes.NewPackage("main", "main")
		privateField := gotypes.NewField(token.NoPos, pkg, "count", gotypes.Typ[gotypes.String], false)
		exportedField := gotypes.NewField(token.NoPos, pkg, "Count", gotypes.Typ[gotypes.Int], false)
		optsType := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
			gotypes.NewStruct([]*gotypes.Var{privateField, exportedField}, nil),
			nil,
		)
		kwarg := &ResolvedCallExprKwarg{
			Param: gotypes.NewParam(token.NoPos, pkg, "opts", optsType),
			info:  &types.Info{Pkg: local},
		}

		targets := ListResolvedCallExprKwargTargets(kwarg)
		require.Len(t, targets, 1)
		assert.Equal(t, "count", targets[0].Name)
		assert.Same(t, exportedField, targets[0].Field)
	})

	t.Run("StructUsesUnicodeKeywordName", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		field := gotypes.NewField(token.NoPos, pkg, "\u00c4ge", gotypes.Typ[gotypes.Int], false)
		optsType := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
			gotypes.NewStruct([]*gotypes.Var{field}, nil),
			nil,
		)
		kwarg := &ResolvedCallExprKwarg{
			Param: gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", optsType),
		}

		targets := ListResolvedCallExprKwargTargets(kwarg)
		require.Len(t, targets, 1)
		assert.Equal(t, "\u00e4ge", targets[0].Name)
		assert.Equal(t, field, targets[0].Field)
		assert.Equal(t, field, LookupResolvedCallExprKwargTarget(kwarg, "\u00e4ge").Field)
	})

	t.Run("StructRejectsShortUnicodeKeywordName", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		short := gotypes.NewField(token.NoPos, pkg, "\u2c7e", gotypes.Typ[gotypes.Int], false)
		long := gotypes.NewField(token.NoPos, pkg, "\u2c7ecore", gotypes.Typ[gotypes.Int], false)
		optsType := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
			gotypes.NewStruct([]*gotypes.Var{short, long}, nil),
			nil,
		)
		kwarg := &ResolvedCallExprKwarg{Param: gotypes.NewParam(token.NoPos, pkg, "opts", optsType)}

		targets := ListResolvedCallExprKwargTargets(kwarg)
		require.Len(t, targets, 2)
		assert.Equal(t, "\u2c7e", targets[0].Name)
		assert.Equal(t, "\u023fcore", targets[1].Name)
		assert.Nil(t, LookupResolvedCallExprKwargTarget(kwarg, "\u023f"))
		kwarg.spellingType = optsType
		assert.Nil(t, LookupResolvedCallExprKwargTarget(kwarg, "\u023f"))
		assert.Nil(t, StructKeywordField(optsType, pkg, "\u023f"))
		assert.Same(t, short, StructKeywordField(optsType, pkg, "\u2c7e"))
		assert.Same(t, long, StructKeywordField(optsType, pkg, "\u023fcore"))
	})

	t.Run("InterfaceSkipsShadowedLowercaseMethodName", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		paramsTypeName := gotypes.NewTypeName(token.NoPos, pkg, "Params", nil)
		paramsNamed := gotypes.NewNamed(paramsTypeName, nil, nil)
		exportedMethod := gotypes.NewFunc(token.NoPos, pkg, "MaxTokens", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "n", gotypes.Typ[gotypes.Int64])),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", paramsNamed)),
			false,
		))
		localMethod := gotypes.NewFunc(token.NoPos, pkg, "maxTokens", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "n", gotypes.Typ[gotypes.String])),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", paramsNamed)),
			false,
		))
		iface := gotypes.NewInterfaceType([]*gotypes.Func{exportedMethod, localMethod}, nil)
		iface.Complete()
		paramsNamed.SetUnderlying(iface)
		kwarg := &ResolvedCallExprKwarg{
			Param:                 gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed),
			AllowInterfaceTargets: true,
		}

		targets := ListResolvedCallExprKwargTargets(kwarg)
		require.Len(t, targets, 1)
		assert.Equal(t, "maxTokens", targets[0].Name)
		assert.Equal(t, exportedMethod, targets[0].Method)
	})

	t.Run("InterfaceListsSetKeywordMethod", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		paramsTypeName := gotypes.NewTypeName(token.NoPos, pkg, "Params", nil)
		paramsNamed := gotypes.NewNamed(paramsTypeName, nil, nil)
		setMethod := gotypes.NewFunc(token.NoPos, pkg, "Set", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "enabled", gotypes.Typ[gotypes.Bool])),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", paramsNamed)),
			false,
		))
		iface := gotypes.NewInterfaceType([]*gotypes.Func{setMethod}, nil)
		iface.Complete()
		paramsNamed.SetUnderlying(iface)
		kwarg := &ResolvedCallExprKwarg{
			Param:                 gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed),
			AllowInterfaceTargets: true,
		}

		targets := ListResolvedCallExprKwargTargets(kwarg)
		require.Len(t, targets, 1)
		assert.Equal(t, "set", targets[0].Name)
		assert.Equal(t, setMethod, targets[0].Method)
		assert.Equal(t, setMethod, LookupResolvedCallExprKwargTarget(kwarg, "set").Method)
	})

	t.Run("InterfaceDoesNotListDynamicSetTarget", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		paramsTypeName := gotypes.NewTypeName(token.NoPos, pkg, "Params", nil)
		paramsNamed := gotypes.NewNamed(paramsTypeName, nil, nil)
		anyIface := gotypes.NewInterfaceType(nil, nil)
		anyIface.Complete()
		setMethod := gotypes.NewFunc(token.NoPos, pkg, "Set", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(
				gotypes.NewParam(token.NoPos, pkg, "name", gotypes.Typ[gotypes.String]),
				gotypes.NewParam(token.NoPos, pkg, "value", anyIface),
			),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", paramsNamed)),
			false,
		))
		iface := gotypes.NewInterfaceType([]*gotypes.Func{setMethod}, nil)
		iface.Complete()
		paramsNamed.SetUnderlying(iface)
		kwarg := &ResolvedCallExprKwarg{
			Param:                 gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed),
			AllowInterfaceTargets: true,
		}

		assert.Empty(t, ListResolvedCallExprKwargTargets(kwarg))
		target := LookupResolvedCallExprKwargTarget(kwarg, "custom")
		require.NotNil(t, target)
		assert.Equal(t, ResolvedCallExprKwargTargetInterfaceSet, target.Kind)
	})

	t.Run("InterfaceUsesASCIIKeywordName", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		paramsTypeName := gotypes.NewTypeName(token.NoPos, pkg, "Params", nil)
		paramsNamed := gotypes.NewNamed(paramsTypeName, nil, nil)
		method := gotypes.NewFunc(token.NoPos, pkg, "\u00c4ge", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "n", gotypes.Typ[gotypes.Int])),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", paramsNamed)),
			false,
		))
		iface := gotypes.NewInterfaceType([]*gotypes.Func{method}, nil)
		iface.Complete()
		paramsNamed.SetUnderlying(iface)
		kwarg := &ResolvedCallExprKwarg{
			Param:                 gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed),
			AllowInterfaceTargets: true,
		}

		targets := ListResolvedCallExprKwargTargets(kwarg)
		require.Len(t, targets, 1)
		assert.Equal(t, "\u00c4ge", targets[0].Name)
		assert.Equal(t, method, targets[0].Method)
		assert.Nil(t, LookupResolvedCallExprKwargTarget(kwarg, "\u00e4ge"))
	})
}

func TestSourceExprIndex(t *testing.T) {
	first := &ast.Ident{NamePos: 10, Name: "first"}
	second := &ast.Ident{NamePos: 20, Name: "second"}
	shared := &ast.Ident{NamePos: 20, Name: "shared"}
	last := &ast.Ident{NamePos: 30, Name: "last"}
	for _, tt := range []struct {
		name   string
		target ast.Expr
		want   int
	}{
		{"First", first, 0},
		{"Middle", second, 1},
		{"SharedPosition", shared, 2},
		{"Last", last, 3},
		{"SamePositionDifferentNode", &ast.Ident{NamePos: 20}, -1},
		{"Before", &ast.Ident{NamePos: 5}, -1},
		{"Between", &ast.Ident{NamePos: 25}, -1},
		{"After", &ast.Ident{NamePos: 40}, -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, SourceExprIndex([]ast.Expr{first, second, shared, last}, tt.target))
		})
	}
	assert.Equal(t, -1, SourceExprIndex(nil, first))
}

func TestResolveFuncSignatureForCallTemplateReceiver(t *testing.T) {
	for _, tt := range []struct {
		name, selector            string
		typeSelector, hasReceiver bool
	}{
		{name: "Bound", selector: "choose"},
		{name: "TypeSelector", selector: "Choose", typeSelector: true, hasReceiver: true},
		{name: "EncodedName", selector: "Gopt_Item_Choose", hasReceiver: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := gotypes.NewPackage("example.com/framework", "framework")
			markAsXGoPackage(pkg)
			owner := gotypes.NewTypeName(0, pkg, "Item", nil)
			item := gotypes.NewNamed(owner, gotypes.NewStruct(nil, nil), nil)
			pkg.Scope().Insert(owner)
			receiver := gotypes.NewParam(0, pkg, "item", gotypes.NewPointer(item))
			value := gotypes.NewParam(0, pkg, "value", gotypes.Typ[gotypes.Int])
			signature := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(receiver, value), nil, false)
			fun := gotypes.NewFunc(0, pkg, "Gopt_Item_Choose", signature)

			sel := &ast.Ident{Name: tt.selector}
			var callee ast.Expr = sel
			bindings := map[*ast.Ident]gotypes.Object{sel: fun}
			if tt.selector != fun.Name() {
				expr := &ast.Ident{Name: "item"}
				bindings[expr] = gotypes.NewVar(0, pkg, "item", item)
				if tt.typeSelector {
					expr.Name = "Item"
					bindings[expr] = owner
				}
				callee = &ast.SelectorExpr{X: expr, Sel: sel}
			}
			info := newTestTypeInfo(nil, bindings)
			_, params := ResolveFuncSignatureForCall(info, &ast.CallExpr{Fun: callee}, fun)
			want := []*gotypes.Var{value}
			if tt.hasReceiver {
				want = []*gotypes.Var{receiver, value}
			}
			assert.Equal(t, want, slices.Collect(params.Variables()))
		})
	}
}

func TestResolveFuncSignatureWithMissingTemplateReceiver(t *testing.T) {
	pkg := gotypes.NewPackage("example.com/framework", "framework")
	markAsXGoPackage(pkg)
	owner := gotypes.NewTypeName(0, pkg, "Item", nil)
	gotypes.NewNamed(owner, gotypes.NewStruct(nil, nil), nil)
	pkg.Scope().Insert(owner)
	signature := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
	fun := gotypes.NewFunc(0, pkg, "Gopt_Item_Choose", signature)

	sig, params := ResolveFuncSignature(fun)
	assert.Nil(t, sig)
	assert.Nil(t, params)
	sig, params = ResolveFuncSignatureForCall(nil, &ast.CallExpr{Fun: &ast.Ident{Name: "choose"}}, fun)
	assert.Nil(t, sig)
	assert.Nil(t, params)
}
