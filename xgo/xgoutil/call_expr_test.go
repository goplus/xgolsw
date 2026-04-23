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
	"testing"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		assert.Len(t, got.Args, 1)
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

func TestWalkCallExprArgs(t *testing.T) {
	t.Run("NilCallExpr", func(t *testing.T) {
		walkCalled := false
		walkFn := func(fun *gotypes.Func, params *gotypes.Tuple, paramIndex int, arg ast.Expr, argIndex int) bool {
			walkCalled = true
			return true
		}

		WalkCallExprArgs(nil, nil, walkFn)
		assert.False(t, walkCalled)
	})

	t.Run("CallExprWithNilFunction", func(t *testing.T) {
		expr := &ast.CallExpr{
			Fun: &ast.Ident{Name: "unknown"},
		}

		walkCalled := false
		walkFn := func(fun *gotypes.Func, params *gotypes.Tuple, paramIndex int, arg ast.Expr, argIndex int) bool {
			walkCalled = true
			return true
		}

		typeInfo := newTestTypeInfo(nil, nil)

		WalkCallExprArgs(typeInfo, expr, walkFn)
		assert.False(t, walkCalled)
	})

	t.Run("CallExprWithSimpleFunction", func(t *testing.T) {
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

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		var walkCalls []struct {
			paramIndex int
			argIndex   int
			arg        ast.Expr
		}

		walkFn := func(fun *gotypes.Func, params *gotypes.Tuple, paramIndex int, arg ast.Expr, argIndex int) bool {
			walkCalls = append(walkCalls, struct {
				paramIndex int
				argIndex   int
				arg        ast.Expr
			}{paramIndex, argIndex, arg})
			return true
		}

		WalkCallExprArgs(typeInfo, expr, walkFn)

		assert.Len(t, walkCalls, 2)
		assert.Equal(t, 0, walkCalls[0].paramIndex)
		assert.Equal(t, 0, walkCalls[0].argIndex)
		assert.Equal(t, arg1, walkCalls[0].arg)
		assert.Equal(t, 1, walkCalls[1].paramIndex)
		assert.Equal(t, 1, walkCalls[1].argIndex)
		assert.Equal(t, arg2, walkCalls[1].arg)
	})

	t.Run("CallExprWithVariadicFunction", func(t *testing.T) {
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

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		var walkCalls []struct {
			paramIndex int
			argIndex   int
		}

		walkFn := func(fun *gotypes.Func, params *gotypes.Tuple, paramIndex int, arg ast.Expr, argIndex int) bool {
			walkCalls = append(walkCalls, struct {
				paramIndex int
				argIndex   int
			}{paramIndex, argIndex})
			return true
		}

		WalkCallExprArgs(typeInfo, expr, walkFn)

		assert.Len(t, walkCalls, 3)
		assert.Equal(t, 0, walkCalls[0].paramIndex) // First param
		assert.Equal(t, 0, walkCalls[0].argIndex)
		assert.Equal(t, 1, walkCalls[1].paramIndex) // Variadic param
		assert.Equal(t, 1, walkCalls[1].argIndex)
		assert.Equal(t, 1, walkCalls[2].paramIndex) // Still variadic param
		assert.Equal(t, 2, walkCalls[2].argIndex)
	})

	t.Run("CallExprStopsWhenWalkFnReturnsFalse", func(t *testing.T) {
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

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		walkCallCount := 0
		walkFn := func(fun *gotypes.Func, params *gotypes.Tuple, paramIndex int, arg ast.Expr, argIndex int) bool {
			walkCallCount++
			return false // Stop after first call.
		}

		WalkCallExprArgs(typeInfo, expr, walkFn)
		assert.Equal(t, 1, walkCallCount)
	})

	t.Run("CallExprWithXGoPackageXGotMethod", func(t *testing.T) {
		ident := &ast.Ident{Name: "XGot_Sprite_Move"}
		arg1 := &ast.Ident{Name: "arg1"}
		expr := &ast.CallExpr{
			Fun:  ident,
			Args: []ast.Expr{arg1},
		}

		pkg := gotypes.NewPackage("test", "test")
		markAsXGoPackage(pkg)

		recv := gotypes.NewParam(token.NoPos, pkg, "recv", gotypes.Typ[gotypes.Int])
		param1 := gotypes.NewParam(token.NoPos, pkg, "p1", gotypes.Typ[gotypes.Int])
		params := gotypes.NewTuple(recv, param1)
		sig := gotypes.NewSignatureType(recv, nil, nil, params, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "XGot_Sprite_Move", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		var walkCalls []struct {
			paramIndex int
			argIndex   int
		}

		walkFn := func(fun *gotypes.Func, params *gotypes.Tuple, paramIndex int, arg ast.Expr, argIndex int) bool {
			walkCalls = append(walkCalls, struct {
				paramIndex int
				argIndex   int
			}{paramIndex, argIndex})
			return true
		}

		WalkCallExprArgs(typeInfo, expr, walkFn)

		// Should skip the receiver parameter.
		assert.Len(t, walkCalls, 1)
		assert.Equal(t, 0, walkCalls[0].paramIndex) // First non-receiver param index in new tuple.
		assert.Equal(t, 0, walkCalls[0].argIndex)   // First arg.
	})

	t.Run("CallExprWithFuncExFunction", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		ident := &ast.Ident{Name: "testFunc"}
		expr := &ast.CallExpr{
			Fun:  ident,
			Args: []ast.Expr{&ast.Ident{Name: "arg"}},
		}
		fun := gogen.NewOverloadFunc(token.NoPos, pkg, "testFunc",
			gotypes.NewFunc(token.NoPos, pkg, "foo", gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)),
		)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		walkCalled := false
		WalkCallExprArgs(typeInfo, expr, func(fun *gotypes.Func, params *gotypes.Tuple, paramIndex int, arg ast.Expr, argIndex int) bool {
			walkCalled = true
			return true
		})
		assert.False(t, walkCalled)
	})

	t.Run("CallExprWithXGoPackageXGoxMethod", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		markAsXGoPackage(pkg)

		constraint := gotypes.NewInterfaceType(nil, nil)
		constraint.Complete()
		typeParam := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "T", nil), constraint)
		recv := gotypes.NewParam(token.NoPos, pkg, "recv", gotypes.Typ[gotypes.Int])
		param1 := gotypes.NewParam(token.NoPos, pkg, "p1", gotypes.Typ[gotypes.String])
		params := gotypes.NewTuple(recv, param1)
		sig := gotypes.NewSignatureType(nil, nil, []*gotypes.TypeParam{typeParam}, params, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "XGot_Sprite_XGox_Move", sig)

		ident := &ast.Ident{Name: "XGot_Sprite_XGox_Move"}
		expr := &ast.CallExpr{
			Fun: ident,
			Args: []ast.Expr{
				&ast.Ident{Name: "int"},
				&ast.BasicLit{Kind: token.STRING, Value: `"ok"`},
			},
		}

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		var walkCalls []struct {
			paramName  string
			paramIndex int
			argIndex   int
		}
		WalkCallExprArgs(typeInfo, expr, func(fun *gotypes.Func, params *gotypes.Tuple, paramIndex int, arg ast.Expr, argIndex int) bool {
			walkCalls = append(walkCalls, struct {
				paramName  string
				paramIndex int
				argIndex   int
			}{
				paramName:  params.At(paramIndex).Name(),
				paramIndex: paramIndex,
				argIndex:   argIndex,
			})
			return true
		})

		assert.Len(t, walkCalls, 2)
		assert.Equal(t, "T", walkCalls[0].paramName)
		assert.Equal(t, 0, walkCalls[0].paramIndex)
		assert.Equal(t, 0, walkCalls[0].argIndex)
		assert.Equal(t, "p1", walkCalls[1].paramName)
		assert.Equal(t, 1, walkCalls[1].paramIndex)
		assert.Equal(t, 1, walkCalls[1].argIndex)
	})

	t.Run("CallExprWithMoreArgsThanParams", func(t *testing.T) {
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
		params := gotypes.NewTuple(param1)
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "testFunc", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		var walkCalls []struct {
			paramIndex int
			argIndex   int
		}

		walkFn := func(fun *gotypes.Func, params *gotypes.Tuple, paramIndex int, arg ast.Expr, argIndex int) bool {
			walkCalls = append(walkCalls, struct {
				paramIndex int
				argIndex   int
			}{paramIndex, argIndex})
			return true
		}

		WalkCallExprArgs(typeInfo, expr, walkFn)

		// Should only process one argument since function is not variadic.
		assert.Len(t, walkCalls, 1)
		assert.Equal(t, 0, walkCalls[0].paramIndex)
		assert.Equal(t, 0, walkCalls[0].argIndex)
	})

	t.Run("CallExprWithNoParams", func(t *testing.T) {
		ident := &ast.Ident{Name: "testFunc"}
		arg1 := &ast.Ident{Name: "arg1"}
		expr := &ast.CallExpr{
			Fun:  ident,
			Args: []ast.Expr{arg1},
		}

		pkg := gotypes.NewPackage("test", "test")
		params := gotypes.NewTuple()
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "testFunc", sig)

		typeInfo := newTestTypeInfo(nil, map[*ast.Ident]gotypes.Object{
			ident: fun,
		})

		walkCalled := false
		walkFn := func(fun *gotypes.Func, params *gotypes.Tuple, paramIndex int, arg ast.Expr, argIndex int) bool {
			walkCalled = true
			return true
		}

		WalkCallExprArgs(typeInfo, expr, walkFn)

		// Should not call walkFn since function has no parameters and more args than params.
		assert.False(t, walkCalled)
	})
}
