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

func TestInnermostScopeAt(t *testing.T) {
	t.Run("NilTypeInfo", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var x = 1")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		scope := InnermostScopeAt(fset, nil, astPkg, token.Pos(1))
		assert.Nil(t, scope)
	})

	t.Run("InvalidPosition", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var x = 1")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})
		typeInfo := newTestTypeInfo(nil, nil)

		scope := InnermostScopeAt(fset, typeInfo, astPkg, token.NoPos)
		assert.Nil(t, scope)
	})

	t.Run("CanSeeGlobalVariable", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var x = 1\nfunc test() { println(x) }")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		pkg := types.NewPackage("main", "main")
		packageScope := types.NewScope(types.Universe, token.NoPos, token.NoPos, "package")
		xVar := types.NewVar(token.NoPos, pkg, "x", types.Typ[types.Int])
		packageScope.Insert(xVar)

		typeInfo := newTestTypeInfo(nil, nil)
		typeInfo.Scopes = map[ast.Node]*types.Scope{
			astFile: packageScope,
		}

		xPos := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0].Pos()
		scope := InnermostScopeAt(fset, typeInfo, astPkg, xPos)
		require.NotNil(t, scope)

		assert.NotNil(t, scope.Lookup("x"))
	})

	t.Run("CanSeeFunctionLocalVariable", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "func test() { y := 2; println(y) }")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		pkg := types.NewPackage("main", "main")
		packageScope := types.NewScope(types.Universe, token.NoPos, token.NoPos, "package")
		functionScope := types.NewScope(packageScope, token.NoPos, token.NoPos, "function")

		yVar := types.NewVar(token.NoPos, pkg, "y", types.Typ[types.Int])
		functionScope.Insert(yVar)

		typeInfo := newTestTypeInfo(nil, nil)
		funcDecl := astFile.Decls[0].(*ast.FuncDecl)
		typeInfo.Scopes = map[ast.Node]*types.Scope{
			astFile:       packageScope,
			funcDecl.Body: functionScope,
		}

		scope := InnermostScopeAt(fset, typeInfo, astPkg, funcDecl.Body.Pos())
		require.NotNil(t, scope)

		assert.NotNil(t, scope.Lookup("y"))
	})

	t.Run("CanSeeBlockScopedVariable", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "func test() { if true { z := 3; println(z) } }")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		pkg := types.NewPackage("main", "main")
		packageScope := types.NewScope(types.Universe, token.NoPos, token.NoPos, "package")
		functionScope := types.NewScope(packageScope, token.NoPos, token.NoPos, "function")
		blockScope := types.NewScope(functionScope, token.NoPos, token.NoPos, "block")

		zVar := types.NewVar(token.NoPos, pkg, "z", types.Typ[types.Int])
		blockScope.Insert(zVar)

		// Find the if statement body.
		funcDecl := astFile.Decls[0].(*ast.FuncDecl)
		var ifBody *ast.BlockStmt
		for _, stmt := range funcDecl.Body.List {
			if ifStmt, ok := stmt.(*ast.IfStmt); ok {
				ifBody = ifStmt.Body
				break
			}
		}
		require.NotNil(t, ifBody)

		typeInfo := newTestTypeInfo(nil, nil)
		typeInfo.Scopes = map[ast.Node]*types.Scope{
			astFile:       packageScope,
			funcDecl.Body: functionScope,
			ifBody:        blockScope,
		}

		scope := InnermostScopeAt(fset, typeInfo, astPkg, ifBody.Pos())
		require.NotNil(t, scope)

		assert.NotNil(t, scope.Lookup("z"))
	})

	t.Run("FuncDeclScopeFromType", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "func test(param int) { println(param) }")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		pkg := types.NewPackage("main", "main")
		packageScope := types.NewScope(types.Universe, token.NoPos, token.NoPos, "package")
		functionScope := types.NewScope(packageScope, token.NoPos, token.NoPos, "function")

		paramVar := types.NewVar(token.NoPos, pkg, "param", types.Typ[types.Int])
		functionScope.Insert(paramVar)

		funcDecl := astFile.Decls[0].(*ast.FuncDecl)
		typeInfo := newTestTypeInfo(nil, nil)
		typeInfo.Scopes = map[ast.Node]*types.Scope{
			astFile:       packageScope,
			funcDecl.Type: functionScope, // Scope for function parameters.
		}

		scope := InnermostScopeAt(fset, typeInfo, astPkg, funcDecl.Body.Pos())
		require.NotNil(t, scope)

		assert.NotNil(t, scope.Lookup("param"))
	})

	t.Run("FuncLitScopeFromType", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var f = func(param string) { println(param) }")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		pkg := types.NewPackage("main", "main")
		packageScope := types.NewScope(types.Universe, token.NoPos, token.NoPos, "package")
		functionScope := types.NewScope(packageScope, token.NoPos, token.NoPos, "function")

		paramVar := types.NewVar(token.NoPos, pkg, "param", types.Typ[types.String])
		functionScope.Insert(paramVar)

		// Extract the function literal from the variable declaration.
		genDecl := astFile.Decls[0].(*ast.GenDecl)
		valueSpec := genDecl.Specs[0].(*ast.ValueSpec)
		funcLit := valueSpec.Values[0].(*ast.FuncLit)

		typeInfo := newTestTypeInfo(nil, nil)
		typeInfo.Scopes = map[ast.Node]*types.Scope{
			astFile:      packageScope,
			funcLit.Type: functionScope, // Scope from FuncType for parameters.
		}

		scope := InnermostScopeAt(fset, typeInfo, astPkg, funcLit.Body.Pos())
		require.NotNil(t, scope)

		assert.NotNil(t, scope.Lookup("param"))
	})

	t.Run("FuncLitScopeFromBody", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var f = func() { local := 42; println(local) }")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		pkg := types.NewPackage("main", "main")
		packageScope := types.NewScope(types.Universe, token.NoPos, token.NoPos, "package")
		functionScope := types.NewScope(packageScope, token.NoPos, token.NoPos, "function")

		localVar := types.NewVar(token.NoPos, pkg, "local", types.Typ[types.Int])
		functionScope.Insert(localVar)

		// Extract the function literal from the variable declaration.
		genDecl := astFile.Decls[0].(*ast.GenDecl)
		valueSpec := genDecl.Specs[0].(*ast.ValueSpec)
		funcLit := valueSpec.Values[0].(*ast.FuncLit)

		typeInfo := newTestTypeInfo(nil, nil)
		typeInfo.Scopes = map[ast.Node]*types.Scope{
			astFile:      packageScope,
			funcLit.Body: functionScope, // Scope from Body for local variables.
		}

		// Get position inside function body.
		bodyPos := funcLit.Body.List[0].Pos()
		scope := InnermostScopeAt(fset, typeInfo, astPkg, bodyPos)
		require.NotNil(t, scope)

		assert.NotNil(t, scope.Lookup("local"))
	})

	t.Run("NilPackage", func(t *testing.T) {
		fset := token.NewFileSet()
		typeInfo := newTestTypeInfo(nil, nil)

		scope := InnermostScopeAt(fset, typeInfo, nil, token.Pos(1))
		assert.Nil(t, scope)
	})

	t.Run("NilFileSet", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", "var x = 1")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})
		typeInfo := newTestTypeInfo(nil, nil)

		scope := InnermostScopeAt(nil, typeInfo, astPkg, token.Pos(1))
		assert.Nil(t, scope)
	})
}
