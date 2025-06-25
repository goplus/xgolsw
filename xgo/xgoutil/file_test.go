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
	"go/token"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPosFilename(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("var x = 1"),
		}, xgo.FeatAll)

		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)

		xPos := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0].Pos()
		filename := PosFilename(proj, xPos)
		require.NotEmpty(t, filename)
		assert.Contains(t, filename, "main.xgo")
	})

	t.Run("NilProject", func(t *testing.T) {
		filename := PosFilename(nil, token.Pos(1))
		assert.Empty(t, filename)
	})

	t.Run("InvalidPos", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("var x = 1"),
		}, xgo.FeatAll)

		filename := PosFilename(proj, token.NoPos)
		assert.Empty(t, filename)
	})
}

func TestNodeFilename(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("var x = 1"),
		}, xgo.FeatAll)

		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)

		xDecl := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0]
		filename := NodeFilename(proj, xDecl)
		require.NotEmpty(t, filename)
		assert.Contains(t, filename, "main.xgo")
	})

	t.Run("NilProject", func(t *testing.T) {
		filename := NodeFilename(nil, &ast.Ident{Name: "test"})
		assert.Empty(t, filename)
	})

	t.Run("NilNode", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("var x = 1"),
		}, xgo.FeatAll)

		filename := NodeFilename(proj, nil)
		assert.Empty(t, filename)
	})
}

func TestPosTokenFile(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("var x = 1"),
		}, xgo.FeatAll)

		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)

		xPos := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0].Pos()
		file := PosTokenFile(proj, xPos)
		assert.NotNil(t, file)
	})

	t.Run("NilProject", func(t *testing.T) {
		file := PosTokenFile(nil, token.Pos(1))
		assert.Nil(t, file)
	})

	t.Run("InvalidPos", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("var x = 1"),
		}, xgo.FeatAll)

		file := PosTokenFile(proj, token.NoPos)
		assert.Nil(t, file)
	})
}

func TestNodeTokenFile(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("var x = 1"),
		}, xgo.FeatAll)

		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)

		xDecl := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0]
		file := NodeTokenFile(proj, xDecl)
		assert.NotNil(t, file)
	})

	t.Run("NilProject", func(t *testing.T) {
		file := NodeTokenFile(nil, &ast.Ident{Name: "test"})
		assert.Nil(t, file)
	})

	t.Run("NilNode", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("var x = 1"),
		}, xgo.FeatAll)

		file := NodeTokenFile(proj, nil)
		assert.Nil(t, file)
	})
}

func TestPosASTFile(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("var x = 1"),
		}, xgo.FeatAll)

		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)

		xPos := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0].Pos()
		file := PosASTFile(proj, xPos)
		assert.Equal(t, astFile, file)
	})

	t.Run("NilProject", func(t *testing.T) {
		file := PosASTFile(nil, token.Pos(1))
		assert.Nil(t, file)
	})

	t.Run("InvalidPos", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("var x = 1"),
		}, xgo.FeatAll)

		file := PosASTFile(proj, token.NoPos)
		assert.Nil(t, file)
	})
}

func TestNodeASTFile(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("var x = 1"),
		}, xgo.FeatAll)

		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)

		xDecl := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0]
		file := NodeASTFile(proj, xDecl)
		assert.Equal(t, astFile, file)
	})

	t.Run("NilProject", func(t *testing.T) {
		file := NodeASTFile(nil, &ast.Ident{Name: "test"})
		assert.Nil(t, file)
	})

	t.Run("NilNode", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("var x = 1"),
		}, xgo.FeatAll)

		file := NodeASTFile(proj, nil)
		assert.Nil(t, file)
	})
}
