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
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInnermostScopeAt(t *testing.T) {
	t.Run("NilTypeInfo", func(t *testing.T) {
		// Create a project that will have nil TypeInfo due to unsupported features.
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file(`invalid syntax`),
		}, 0) // Use feature flag 0 to trigger unsupported cache kind.

		// This should return nil when typeInfo is nil.
		scope := InnermostScopeAt(proj, token.Pos(1))
		assert.Nil(t, scope)
	})

	t.Run("InvalidPosition", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file(`var x = 1`),
		}, xgo.FeatAll)

		scope := InnermostScopeAt(proj, token.NoPos)
		assert.Nil(t, scope)
	})

	t.Run("CanSeeGlobalVariable", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file(`
var x = 1

func test() {
	println(x)
}
`),
		}, xgo.FeatAll)

		typeInfo, _ := proj.TypeInfo()
		require.NotNil(t, typeInfo)

		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)

		// Get position of variable x declaration.
		xPos := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0].Pos()
		scope := InnermostScopeAt(proj, xPos)
		require.NotNil(t, scope)

		// Should be able to see variable x.
		if scope == typeInfo.Scopes[astFile] {
			scope = scope.Parent()
		}
		require.NotNil(t, scope)
		assert.NotNil(t, scope.Lookup("x"))
	})

	t.Run("CanSeeFunctionLocalVariable", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file(`
func test() {
	y := 2
	println(y)
}
`),
		}, xgo.FeatAll)

		typeInfo, _ := proj.TypeInfo()
		require.NotNil(t, typeInfo)

		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)

		// Get position of function body.
		funcDecl := astFile.Decls[0].(*ast.FuncDecl)
		scope := InnermostScopeAt(proj, funcDecl.Body.Pos())
		require.NotNil(t, scope)

		// Should be able to see variable y.
		assert.NotNil(t, scope.Lookup("y"))
	})

	t.Run("CanSeeBlockScopedVariable", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file(`
func test() {
	if true {
		z := 3
		println(z)
	}
}
`),
		}, xgo.FeatAll)

		typeInfo, _ := proj.TypeInfo()
		require.NotNil(t, typeInfo)

		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)

		// Find the if statement body position.
		var ifPos token.Pos
		funcDecl := astFile.Decls[0].(*ast.FuncDecl)
		for _, stmt := range funcDecl.Body.List {
			if ifStmt, ok := stmt.(*ast.IfStmt); ok {
				ifPos = ifStmt.Body.Pos()
				break
			}
		}
		require.NotEqual(t, token.NoPos, ifPos)

		scope := InnermostScopeAt(proj, ifPos)
		require.NotNil(t, scope)

		// Should be able to see variable z.
		assert.NotNil(t, scope.Lookup("z"))
	})

	t.Run("FuncDeclScopeFromType", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file(`
func test(param int) {
	println(param)
}
`),
		}, xgo.FeatAll)

		typeInfo, _ := proj.TypeInfo()
		require.NotNil(t, typeInfo)

		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)

		// Get position inside function body where we might access parameters.
		funcDecl := astFile.Decls[0].(*ast.FuncDecl)
		scope := InnermostScopeAt(proj, funcDecl.Body.Pos())
		require.NotNil(t, scope)

		// Should be able to see function parameter.
		assert.NotNil(t, scope.Lookup("param"))
	})
}
