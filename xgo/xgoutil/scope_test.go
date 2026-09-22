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

		pkg := gotypes.NewPackage("main", "main")
		packageScope := gotypes.NewScope(gotypes.Universe, token.NoPos, token.NoPos, "package")
		xVar := gotypes.NewVar(token.NoPos, pkg, "x", gotypes.Typ[gotypes.Int])
		packageScope.Insert(xVar)

		typeInfo := newTestTypeInfo(nil, nil)
		typeInfo.Scopes = map[ast.Node]*gotypes.Scope{
			astFile: packageScope,
		}

		xPos := requireValueSpec(t, requireGenDecl(t, astFile.Decls[0]).Specs[0]).Names[0].Pos()
		scope := InnermostScopeAt(fset, typeInfo, astPkg, xPos)
		require.NotNil(t, scope)

		assert.NotNil(t, scope.Lookup("x"))
	})

	t.Run("CanSeeFunctionLocalVariable", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "func test() { y := 2; println(y) }")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		pkg := gotypes.NewPackage("main", "main")
		packageScope := gotypes.NewScope(gotypes.Universe, token.NoPos, token.NoPos, "package")
		functionScope := gotypes.NewScope(packageScope, token.NoPos, token.NoPos, "function")

		yVar := gotypes.NewVar(token.NoPos, pkg, "y", gotypes.Typ[gotypes.Int])
		functionScope.Insert(yVar)

		typeInfo := newTestTypeInfo(nil, nil)
		funcDecl := requireFuncDecl(t, astFile.Decls[0])
		typeInfo.Scopes = map[ast.Node]*gotypes.Scope{
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

		pkg := gotypes.NewPackage("main", "main")
		packageScope := gotypes.NewScope(gotypes.Universe, token.NoPos, token.NoPos, "package")
		functionScope := gotypes.NewScope(packageScope, token.NoPos, token.NoPos, "function")
		blockScope := gotypes.NewScope(functionScope, token.NoPos, token.NoPos, "block")

		zVar := gotypes.NewVar(token.NoPos, pkg, "z", gotypes.Typ[gotypes.Int])
		blockScope.Insert(zVar)

		// Find the if statement body.
		funcDecl := requireFuncDecl(t, astFile.Decls[0])
		var ifBody *ast.BlockStmt
		for _, stmt := range funcDecl.Body.List {
			if ifStmt, ok := stmt.(*ast.IfStmt); ok {
				ifBody = ifStmt.Body
				break
			}
		}
		require.NotNil(t, ifBody)

		typeInfo := newTestTypeInfo(nil, nil)
		typeInfo.Scopes = map[ast.Node]*gotypes.Scope{
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

		pkg := gotypes.NewPackage("main", "main")
		packageScope := gotypes.NewScope(gotypes.Universe, token.NoPos, token.NoPos, "package")
		functionScope := gotypes.NewScope(packageScope, token.NoPos, token.NoPos, "function")

		paramVar := gotypes.NewVar(token.NoPos, pkg, "param", gotypes.Typ[gotypes.Int])
		functionScope.Insert(paramVar)

		funcDecl := requireFuncDecl(t, astFile.Decls[0])
		typeInfo := newTestTypeInfo(nil, nil)
		typeInfo.Scopes = map[ast.Node]*gotypes.Scope{
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

		pkg := gotypes.NewPackage("main", "main")
		packageScope := gotypes.NewScope(gotypes.Universe, token.NoPos, token.NoPos, "package")
		functionScope := gotypes.NewScope(packageScope, token.NoPos, token.NoPos, "function")

		paramVar := gotypes.NewVar(token.NoPos, pkg, "param", gotypes.Typ[gotypes.String])
		functionScope.Insert(paramVar)

		// Extract the function literal from the variable declaration.
		genDecl := requireGenDecl(t, astFile.Decls[0])
		valueSpec := requireValueSpec(t, genDecl.Specs[0])
		funcLit := requireFuncLit(t, valueSpec.Values[0])

		typeInfo := newTestTypeInfo(nil, nil)
		typeInfo.Scopes = map[ast.Node]*gotypes.Scope{
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

		pkg := gotypes.NewPackage("main", "main")
		packageScope := gotypes.NewScope(gotypes.Universe, token.NoPos, token.NoPos, "package")
		functionScope := gotypes.NewScope(packageScope, token.NoPos, token.NoPos, "function")

		localVar := gotypes.NewVar(token.NoPos, pkg, "local", gotypes.Typ[gotypes.Int])
		functionScope.Insert(localVar)

		// Extract the function literal from the variable declaration.
		genDecl := requireGenDecl(t, astFile.Decls[0])
		valueSpec := requireValueSpec(t, genDecl.Specs[0])
		funcLit := requireFuncLit(t, valueSpec.Values[0])

		typeInfo := newTestTypeInfo(nil, nil)
		typeInfo.Scopes = map[ast.Node]*gotypes.Scope{
			astFile:      packageScope,
			funcLit.Body: functionScope, // Scope from Body for local variables.
		}

		// Get position inside function body.
		bodyPos := funcLit.Body.List[0].Pos()
		scope := InnermostScopeAt(fset, typeInfo, astPkg, bodyPos)
		require.NotNil(t, scope)

		assert.NotNil(t, scope.Lookup("local"))
	})

	t.Run("FuncLitFallbackToBodyScope", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var f = func() { local := 42; println(local) }")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		pkg := gotypes.NewPackage("main", "main")
		packageScope := gotypes.NewScope(gotypes.Universe, token.NoPos, token.NoPos, "package")
		functionScope := gotypes.NewScope(packageScope, token.NoPos, token.NoPos, "function")
		functionScope.Insert(gotypes.NewVar(token.NoPos, pkg, "local", gotypes.Typ[gotypes.Int]))

		genDecl := requireGenDecl(t, astFile.Decls[0])
		valueSpec := requireValueSpec(t, genDecl.Specs[0])
		funcLit := requireFuncLit(t, valueSpec.Values[0])

		typeInfo := newTestTypeInfo(nil, nil)
		typeInfo.Scopes = map[ast.Node]*gotypes.Scope{
			astFile:      packageScope,
			funcLit.Body: functionScope,
		}

		scope := InnermostScopeAt(fset, typeInfo, astPkg, funcLit.Pos())
		require.NotNil(t, scope)
		assert.NotNil(t, scope.Lookup("local"))
	})

	t.Run("PositionOutsidePackage", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var x = 1")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})
		typeInfo := newTestTypeInfo(nil, nil)

		scope := InnermostScopeAt(fset, typeInfo, astPkg, astFile.End()+10)
		assert.Nil(t, scope)
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

func TestScopeAtNode(t *testing.T) {
	t.Run("FilterInitializer", func(t *testing.T) {
		fset, file, err := newTestFile("main.xgo", "println [11 for value <- [44], limit := 22; limit > 33]")
		require.NoError(t, err)
		outer := gotypes.NewScope(gotypes.Universe, file.Pos(), file.End(), "outer")
		rangeScope := gotypes.NewScope(outer, file.Pos(), file.End(), "range")
		filterScope := gotypes.NewScope(rangeScope, file.Pos(), file.End(), "filter")
		info := newTestTypeInfo(nil, nil)
		info.Scopes[file] = outer
		positions := make(map[string]token.Pos)
		var clause *ast.ForPhrase
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.ForPhrase:
				clause = node
				info.Scopes[node] = rangeScope
				init, ok := node.Init.(*ast.AssignStmt)
				require.True(t, ok)
				ident, ok := init.Lhs[0].(*ast.Ident)
				require.True(t, ok)
				variable := gotypes.NewVar(ident.Pos(), nil, ident.Name, gotypes.Typ[gotypes.Int])
				filterScope.Insert(variable)
				info.Defs[ident] = variable
			case *ast.BasicLit:
				positions[node.Value] = node.Pos()
			}
			return true
		})
		require.NotNil(t, clause)
		pkg := newTestPackage(map[string]*ast.File{"main.xgo": file})
		for _, value := range []string{"11", "22", "33"} {
			assert.Same(t, filterScope, InnermostScopeAt(fset, info, pkg, positions[value]))
		}
		assert.Same(t, outer, InnermostScopeAt(fset, info, pkg, positions["44"]))
		// Incomplete type information must retain the known range scope.
		clear(info.Defs)
		assert.Same(t, rangeScope, InnermostScopeAt(fset, info, pkg, positions["11"]))
		assert.Same(t, rangeScope, InnermostScopeAt(fset, info, pkg, positions["33"]))
	})

	for _, tt := range []struct {
		name   string
		source string
	}{
		{name: "Range", source: "for _, value := range [11] { println 22 }"},
		{name: "ForPhrase", source: "for value <- [11] { println 22 }"},
		{name: "Comprehension", source: "println [22 for value <- [11]]"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fset, file, err := newTestFile("main.xgo", tt.source)
			require.NoError(t, err)
			outer := gotypes.NewScope(gotypes.Universe, file.Pos(), file.End(), "outer")
			inner := gotypes.NewScope(outer, file.Pos(), file.End(), "inner")
			info := newTestTypeInfo(nil, nil)
			info.Scopes = map[ast.Node]*gotypes.Scope{file: outer}
			var input, result token.Pos
			ast.Inspect(file, func(node ast.Node) bool {
				switch node := node.(type) {
				case *ast.RangeStmt, *ast.ForPhraseStmt:
					info.Scopes[node] = inner
				case *ast.ComprehensionExpr:
					for _, phrase := range node.Fors {
						info.Scopes[phrase] = inner
					}
				case *ast.BasicLit:
					switch node.Value {
					case "11":
						input = node.Pos()
					case "22":
						result = node.Pos()
					}
				}
				return true
			})
			require.True(t, input.IsValid())
			require.True(t, result.IsValid())
			pkg := newTestPackage(map[string]*ast.File{"main.xgo": file})
			assert.Same(t, outer, InnermostScopeAt(fset, info, pkg, input))
			assert.Same(t, inner, InnermostScopeAt(fset, info, pkg, result))
		})
	}
}
