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
			labelIdent: variable, // Not a label, so it won't be skipped.
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

		// Create ident that matches position (TokPos=10, "goto" has length 4, so End=14).
		ident := &ast.Ident{
			NamePos: token.Pos(10),
			Name:    "goto",
		}

		pkg := gotypes.NewPackage("test", "test")
		labelVar := gotypes.NewVar(token.NoPos, pkg, "label", gotypes.Typ[gotypes.Int])
		gotoVar := gotypes.NewVar(token.NoPos, pkg, "goto", gotypes.Typ[gotypes.Int])
		typeInfo := newTestTypeInfo(map[*ast.Ident]gotypes.Object{
			labelIdent: labelVar, // Not a label.
		}, map[*ast.Ident]gotypes.Object{
			ident: gotoVar, // Not a function.
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

		// Create ident that matches position (TokPos=10, "goto" has length 4, so End=14).
		ident := &ast.Ident{
			NamePos: token.Pos(10),
			Name:    "goto",
		}

		pkg := gotypes.NewPackage("test", "test")
		labelVar := gotypes.NewVar(token.NoPos, pkg, "label", gotypes.Typ[gotypes.Int])
		sig := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "goto", sig)
		typeInfo := newTestTypeInfo(map[*ast.Ident]gotypes.Object{
			labelIdent: labelVar, // Not a label.
		}, map[*ast.Ident]gotypes.Object{
			ident: fun, // Is a function.
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

func TestResolveCallExprKwarg(t *testing.T) {
	t.Run("NilCallExpr", func(t *testing.T) {
		assert.Nil(t, ResolveCallExprKwarg(nil, nil))
	})

	t.Run("OptionalParamFromDefinition", func(t *testing.T) {
		ident := &ast.Ident{Name: "configure"}
		paramDef := &ast.Ident{Name: "opts", Obj: &ast.Object{}}
		paramField := &ast.Field{Optional: token.Pos(1)}
		paramDef.Obj.Decl = paramField

		pkg := gotypes.NewPackage("main", "main")
		param := gotypes.NewParam(token.NoPos, pkg, "opts", gotypes.NewMap(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Int]))
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(param), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "configure", sig)

		typeInfo := newTestTypeInfo(
			map[*ast.Ident]gotypes.Object{paramDef: param},
			map[*ast.Ident]gotypes.Object{ident: fun},
		)
		typeInfo.ObjToDef = map[gotypes.Object]*ast.Ident{param: paramDef}

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
			"__gop_optional_second",
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
		{name: "__xgo_optional_opts", want: "opts"},
		{name: "__gop_optional_opts", want: "opts"},
		{name: "opts", want: "opts"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			param := gotypes.NewParam(token.NoPos, pkg, tt.name, gotypes.Typ[gotypes.Int])
			assert.Equal(t, tt.want, SourceParamName(param))
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
		ResolvedCallExprArgs(typeInfo, expr)(func(ResolvedCallExprArg) bool {
			callCount++
			return false
		})
		assert.Equal(t, 1, callCount)
	})

	t.Run("XGoPackageXGotMethod", func(t *testing.T) {
		ident := &ast.Ident{Name: "XGot_Sprite_Move"}
		arg := &ast.Ident{Name: "arg1"}
		expr := &ast.CallExpr{
			Fun:  ident,
			Args: []ast.Expr{arg},
		}

		pkg := gotypes.NewPackage("test", "test")
		markAsXGoPackage(pkg)
		recv := gotypes.NewParam(token.NoPos, pkg, "recv", gotypes.Typ[gotypes.Int])
		param := gotypes.NewParam(token.NoPos, pkg, "p1", gotypes.Typ[gotypes.Int])
		sig := gotypes.NewSignatureType(recv, nil, nil, gotypes.NewTuple(recv, param), nil, false)
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
		constraint := gotypes.NewInterfaceType(nil, nil)
		constraint.Complete()
		typeParam := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "T", nil), constraint)
		recv := gotypes.NewParam(token.NoPos, pkg, "recv", gotypes.Typ[gotypes.Int])
		param := gotypes.NewParam(token.NoPos, pkg, "p1", gotypes.Typ[gotypes.String])
		sig := gotypes.NewSignatureType(nil, nil, []*gotypes.TypeParam{typeParam}, gotypes.NewTuple(recv, param), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "XGot_Sprite_XGox_Move", sig)

		ident := &ast.Ident{Name: "XGot_Sprite_XGox_Move"}
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
		assert.Equal(t, "p1", resolved[1].Param.Name())
		assert.Equal(t, 1, resolved[1].ParamIndex)
		assert.Equal(t, 1, resolved[1].ArgIndex)
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
		assert.Equal(t, gotypes.Typ[gotypes.Int], resolved[0].ExpectedType)
		assert.Equal(t, ResolvedCallExprArgKeyword, resolved[1].Kind)
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
			Kwargs: []*ast.KwargExpr{{
				Name:  &ast.Ident{Name: "maxTokens"},
				Value: &ast.Ident{Name: "maxTokensValue"},
			}},
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

		promptParam := gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.Typ[gotypes.String])
		paramsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(promptParam, paramsParam), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "Complete", sig)

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
			Kwargs: []*ast.KwargExpr{{
				Name:  &ast.Ident{Name: "system"},
				Value: &ast.Ident{Name: "systemValue"},
			}},
		}

		pkg := gotypes.NewPackage("main", "main")
		paramsTypeName := gotypes.NewTypeName(token.NoPos, pkg, "Params", nil)
		paramsNamed := gotypes.NewNamed(paramsTypeName, nil, nil)
		systemMethod := gotypes.NewFunc(token.NoPos, pkg, "System", gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.NewSlice(gotypes.Typ[gotypes.String]))),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", paramsNamed)),
			true,
		))
		iface := gotypes.NewInterfaceType([]*gotypes.Func{systemMethod}, nil)
		iface.Complete()
		paramsNamed.SetUnderlying(iface)

		promptParam := gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.Typ[gotypes.String])
		paramsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(promptParam, paramsParam), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "Complete", sig)

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
			Kwargs: []*ast.KwargExpr{{
				Name:  &ast.Ident{Name: "maxTokens"},
				Value: &ast.Ident{Name: "maxTokensValue"},
			}},
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

		promptParam := gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.Typ[gotypes.String])
		paramsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(promptParam, paramsParam), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "Complete", sig)

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
			Kwargs: []*ast.KwargExpr{{
				Name:  &ast.Ident{Name: "maxTokens"},
				Value: &ast.Ident{Name: "maxTokensValue"},
			}},
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

		promptParam := gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.Typ[gotypes.String])
		paramsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(promptParam, paramsParam), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "Complete", sig)
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
			Kwargs: []*ast.KwargExpr{{
				Name:  &ast.Ident{Name: "custom"},
				Value: &ast.Ident{Name: "customValue"},
			}},
		}

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

		promptParam := gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.Typ[gotypes.String])
		paramsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(promptParam, paramsParam), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "Complete", sig)

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
			Kwargs: []*ast.KwargExpr{{
				Name:  &ast.Ident{Name: "custom"},
				Value: &ast.Ident{Name: "customValue"},
			}},
		}

		pkg := gotypes.NewPackage("main", "main")
		paramsTypeName := gotypes.NewTypeName(token.NoPos, pkg, "Params", nil)
		paramsNamed := gotypes.NewNamed(paramsTypeName, nil, nil)
		anyIface := gotypes.NewInterfaceType(nil, nil)
		anyIface.Complete()
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
		iface := gotypes.NewInterfaceType([]*gotypes.Func{setMethod}, nil)
		iface.Complete()
		paramsNamed.SetUnderlying(iface)

		promptParam := gotypes.NewParam(token.NoPos, pkg, "prompt", gotypes.Typ[gotypes.String])
		paramsParam := gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_params", paramsNamed)
		sig := gotypes.NewSignatureType(nil, nil, nil, gotypes.NewTuple(promptParam, paramsParam), nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "Complete", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{ident: fun})
		addInterfaceKwargFactory(typeInfo, receiver, "Client", paramsNamed)
		resolved := slices.Collect(ResolvedCallExprArgs(typeInfo, call))
		require.Len(t, resolved, 2)
		assert.Equal(t, gotypes.Typ[gotypes.String], resolved[0].ExpectedType)
		assert.Nil(t, resolved[1].ExpectedType)
		assert.Nil(t, resolved[1].KwargTarget)
	})
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
		}

		targets := ListResolvedCallExprKwargTargets(kwarg)
		require.Len(t, targets, 1)
		assert.Equal(t, "count", targets[0].Name)
		assert.Equal(t, exportedField, targets[0].Field)
	})

	t.Run("StructSkipsLaterExportedFieldName", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		localField := gotypes.NewField(token.NoPos, pkg, "count", gotypes.Typ[gotypes.String], false)
		exportedField := gotypes.NewField(token.NoPos, pkg, "Count", gotypes.Typ[gotypes.Int], false)
		optsType := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "Options", nil),
			gotypes.NewStruct([]*gotypes.Var{localField, exportedField}, nil),
			nil,
		)
		kwarg := &ResolvedCallExprKwarg{
			Param: gotypes.NewParam(token.NoPos, pkg, "__xgo_optional_opts", optsType),
		}

		targets := ListResolvedCallExprKwargTargets(kwarg)
		require.Len(t, targets, 1)
		assert.Equal(t, "count", targets[0].Name)
		assert.Equal(t, localField, targets[0].Field)
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
