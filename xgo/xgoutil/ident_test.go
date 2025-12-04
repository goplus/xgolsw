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
	"go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentAtPosition(t *testing.T) {
	t.Run("ExactMatch", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var longVarName = 1\nvar short = 2")
		require.NoError(t, err)

		longVarIdent := findIdent(astFile, "longVarName")
		shortIdent := findIdent(astFile, "short")
		require.NotNil(t, longVarIdent)
		require.NotNil(t, shortIdent)

		pkg := types.NewPackage("main", "main")
		typeInfo := newTestTypeInfo(
			map[*ast.Ident]types.Object{
				longVarIdent: types.NewVar(token.NoPos, pkg, "longVarName", types.Typ[types.Int]),
				shortIdent:   types.NewVar(token.NoPos, pkg, "short", types.Typ[types.Int]),
			},
			nil,
		)

		// Test 'longVarName' at position (1, 5)
		pos := token.Position{Filename: "main.xgo", Line: 1, Column: 5}
		ident := IdentAtPosition(fset, typeInfo, astFile, pos)
		require.NotNil(t, ident)
		assert.Equal(t, "longVarName", ident.Name)

		// Test 'short' at position (2, 5)
		pos = token.Position{Filename: "main.xgo", Line: 2, Column: 5}
		ident = IdentAtPosition(fset, typeInfo, astFile, pos)
		require.NotNil(t, ident)
		assert.Equal(t, "short", ident.Name)
	})

	t.Run("MultipleIdentifiersOnSameLine", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "func test() { result := longVarName + short }")
		require.NoError(t, err)

		resultIdent, resultPos := findIdentWithPos(fset, astFile, "result")
		longVarNameIdent, longVarNamePos := findIdentWithPos(fset, astFile, "longVarName")
		shortIdent, shortPos := findIdentWithPos(fset, astFile, "short")

		pkg := types.NewPackage("main", "main")
		typeInfo := newTestTypeInfo(
			map[*ast.Ident]types.Object{
				resultIdent: types.NewVar(token.NoPos, pkg, "result", types.Typ[types.Int]),
			},
			map[*ast.Ident]types.Object{
				longVarNameIdent: types.NewVar(token.NoPos, pkg, "longVarName", types.Typ[types.Int]),
				shortIdent:       types.NewVar(token.NoPos, pkg, "short", types.Typ[types.Int]),
			},
		)

		// Test each identifier
		ident := IdentAtPosition(fset, typeInfo, astFile, resultPos)
		require.NotNil(t, ident)
		assert.Equal(t, "result", ident.Name)

		ident = IdentAtPosition(fset, typeInfo, astFile, longVarNamePos)
		require.NotNil(t, ident)
		assert.Equal(t, "longVarName", ident.Name)

		ident = IdentAtPosition(fset, typeInfo, astFile, shortPos)
		require.NotNil(t, ident)
		assert.Equal(t, "short", ident.Name)
	})

	t.Run("NoMatch", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var longVarName = 1")
		require.NoError(t, err)

		typeInfo := newTestTypeInfo(nil, nil)

		// Empty position
		pos := token.Position{Filename: "main.xgo", Line: 1, Column: 1}
		ident := IdentAtPosition(fset, typeInfo, astFile, pos)
		assert.Nil(t, ident)

		// After identifier
		pos = token.Position{Filename: "main.xgo", Line: 1, Column: 20}
		ident = IdentAtPosition(fset, typeInfo, astFile, pos)
		assert.Nil(t, ident)
	})

	t.Run("BoundaryConditions", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var longVarName = 1")
		require.NoError(t, err)

		longVarNameIdent := findIdent(astFile, "longVarName")
		require.NotNil(t, longVarNameIdent)

		pkg := types.NewPackage("main", "main")
		typeInfo := newTestTypeInfo(
			map[*ast.Ident]types.Object{
				longVarNameIdent: types.NewVar(token.NoPos, pkg, "longVarName", types.Typ[types.Int]),
			},
			nil,
		)

		identPos := fset.Position(longVarNameIdent.Pos())
		identEnd := fset.Position(longVarNameIdent.End())

		// Just before identifier
		pos := token.Position{Filename: identPos.Filename, Line: identPos.Line, Column: identPos.Column - 1}
		ident := IdentAtPosition(fset, typeInfo, astFile, pos)
		assert.Nil(t, ident)

		// Last character of identifier
		pos = token.Position{Filename: identPos.Filename, Line: identPos.Line, Column: identEnd.Column - 1}
		ident = IdentAtPosition(fset, typeInfo, astFile, pos)
		require.NotNil(t, ident)
		assert.Equal(t, "longVarName", ident.Name)

		// Just after identifier
		pos = token.Position{Filename: identPos.Filename, Line: identPos.Line, Column: identEnd.Column}
		ident = IdentAtPosition(fset, typeInfo, astFile, pos)
		assert.Nil(t, ident)
	})

	t.Run("OverlappingIdentifiers", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var i = 1\nvar ii = 2")
		require.NoError(t, err)

		iIdent, iPos := findIdentWithPos(fset, astFile, "i")
		iiIdent, iiPos := findIdentWithPos(fset, astFile, "ii")

		pkg := types.NewPackage("main", "main")
		typeInfo := newTestTypeInfo(
			map[*ast.Ident]types.Object{
				iIdent:  types.NewVar(token.NoPos, pkg, "i", types.Typ[types.Int]),
				iiIdent: types.NewVar(token.NoPos, pkg, "ii", types.Typ[types.Int]),
			},
			nil,
		)

		// Should find 'i' not 'ii' when at the start of 'i'
		ident := IdentAtPosition(fset, typeInfo, astFile, iPos)
		require.NotNil(t, ident)
		assert.Equal(t, "i", ident.Name)

		// Should find 'ii' when at the start of 'ii'
		ident = IdentAtPosition(fset, typeInfo, astFile, iiPos)
		require.NotNil(t, ident)
		assert.Equal(t, "ii", ident.Name)
	})

	t.Run("EdgeCases", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var x = 1")
		require.NoError(t, err)

		typeInfo := newTestTypeInfo(nil, nil)

		// Invalid line numbers
		pos := token.Position{Filename: "main.xgo", Line: 0, Column: 1}
		ident := IdentAtPosition(fset, typeInfo, astFile, pos)
		assert.Nil(t, ident)

		pos = token.Position{Filename: "main.xgo", Line: 100, Column: 1}
		ident = IdentAtPosition(fset, typeInfo, astFile, pos)
		assert.Nil(t, ident)

		// Wrong filename
		pos = token.Position{Filename: "wrong.xgo", Line: 1, Column: 5}
		ident = IdentAtPosition(fset, typeInfo, astFile, pos)
		assert.Nil(t, ident)

		// Very high column number
		pos = token.Position{Filename: "main.xgo", Line: 1, Column: 1000}
		ident = IdentAtPosition(fset, typeInfo, astFile, pos)
		assert.Nil(t, ident)
	})
}

func TestIsBlankIdent(t *testing.T) {
	assert.True(t, IsBlankIdent(&ast.Ident{Name: "_"}))
	assert.False(t, IsBlankIdent(&ast.Ident{Name: "x"}))
	assert.False(t, IsBlankIdent(nil))
}

func TestIsSyntheticThisIdent(t *testing.T) {
	newBase := func(t *testing.T) (*token.FileSet, *ast.File, *ast.Package, *types.Package) {
		fset, astFile, err := newTestFile("main.xgo", "package main")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})
		pkg := types.NewPackage("main", "main")
		return fset, astFile, astPkg, pkg
	}

	t.Run("Definition", func(t *testing.T) {
		fset, astFile, astPkg, pkg := newBase(t)
		defIdent := &ast.Ident{NamePos: astFile.Pos(), Name: "this"}
		obj := types.NewVar(defIdent.Pos(), pkg, "this", types.Typ[types.Int])
		typeInfo := newTestTypeInfo(map[*ast.Ident]types.Object{defIdent: obj}, nil)
		typeInfo.ObjToDef = map[types.Object]*ast.Ident{obj: defIdent}

		assert.True(t, IsSyntheticThisIdent(fset, typeInfo, astPkg, defIdent))
	})

	t.Run("ReferenceToSynthetic", func(t *testing.T) {
		fset, astFile, astPkg, pkg := newBase(t)
		defIdent := &ast.Ident{NamePos: astFile.Pos(), Name: "this"}
		refIdent := &ast.Ident{NamePos: defIdent.Pos() + 10, Name: "this"}
		obj := types.NewVar(defIdent.Pos(), pkg, "this", types.Typ[types.Int])
		typeInfo := newTestTypeInfo(map[*ast.Ident]types.Object{defIdent: obj}, map[*ast.Ident]types.Object{refIdent: obj})
		typeInfo.ObjToDef = map[types.Object]*ast.Ident{obj: defIdent}

		assert.True(t, IsSyntheticThisIdent(fset, typeInfo, astPkg, refIdent))
	})

	t.Run("NonSyntheticName", func(t *testing.T) {
		fset, astFile, astPkg, _ := newBase(t)
		nonThis := &ast.Ident{NamePos: astFile.Pos(), Name: "foo"}
		typeInfo := newTestTypeInfo(nil, nil)

		assert.False(t, IsSyntheticThisIdent(fset, typeInfo, astPkg, nonThis))
	})

	t.Run("NonSyntheticPosition", func(t *testing.T) {
		fset, astFile, astPkg, pkg := newBase(t)
		defIdent := &ast.Ident{NamePos: astFile.Pos() + 5, Name: "this"}
		obj := types.NewVar(defIdent.Pos(), pkg, "this", types.Typ[types.Int])
		typeInfo := newTestTypeInfo(map[*ast.Ident]types.Object{defIdent: obj}, nil)
		typeInfo.ObjToDef = map[types.Object]*ast.Ident{obj: defIdent}

		assert.False(t, IsSyntheticThisIdent(fset, typeInfo, astPkg, defIdent))
	})

	t.Run("NilInputs", func(t *testing.T) {
		fset, astFile, astPkg, pkg := newBase(t)
		defIdent := &ast.Ident{NamePos: astFile.Pos(), Name: "this"}
		obj := types.NewVar(defIdent.Pos(), pkg, "this", types.Typ[types.Int])
		typeInfo := newTestTypeInfo(map[*ast.Ident]types.Object{defIdent: obj}, nil)
		typeInfo.ObjToDef = map[types.Object]*ast.Ident{obj: defIdent}

		assert.False(t, IsSyntheticThisIdent(nil, typeInfo, astPkg, defIdent))
		assert.False(t, IsSyntheticThisIdent(fset, nil, astPkg, defIdent))
		assert.False(t, IsSyntheticThisIdent(fset, typeInfo, nil, defIdent))
		assert.False(t, IsSyntheticThisIdent(fset, typeInfo, astPkg, nil))
	})
}
