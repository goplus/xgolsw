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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPosFilename(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var x = 1")
		require.NoError(t, err)

		xPos := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0].Pos()
		filename := PosFilename(fset, xPos)
		assert.Equal(t, "main.xgo", filename)
	})

	t.Run("NilFileSet", func(t *testing.T) {
		filename := PosFilename(nil, token.Pos(1))
		assert.Empty(t, filename)
	})

	t.Run("InvalidPos", func(t *testing.T) {
		fset := token.NewFileSet()
		filename := PosFilename(fset, token.NoPos)
		assert.Empty(t, filename)
	})
}

func TestNodeFilename(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var x = 1")
		require.NoError(t, err)

		xDecl := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0]
		filename := NodeFilename(fset, xDecl)
		assert.Equal(t, "main.xgo", filename)
	})

	t.Run("NilFileSet", func(t *testing.T) {
		filename := NodeFilename(nil, &ast.Ident{Name: "test"})
		assert.Empty(t, filename)
	})

	t.Run("NilNode", func(t *testing.T) {
		fset := token.NewFileSet()
		filename := NodeFilename(fset, nil)
		assert.Empty(t, filename)
	})
}

func TestPosTokenFile(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var x = 1")
		require.NoError(t, err)

		xPos := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0].Pos()
		file := PosTokenFile(fset, xPos)
		assert.NotNil(t, file)
		assert.Equal(t, "main.xgo", file.Name())
	})

	t.Run("NilFileSet", func(t *testing.T) {
		file := PosTokenFile(nil, token.Pos(1))
		assert.Nil(t, file)
	})

	t.Run("InvalidPos", func(t *testing.T) {
		fset := token.NewFileSet()
		file := PosTokenFile(fset, token.NoPos)
		assert.Nil(t, file)
	})
}

func TestNodeTokenFile(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var x = 1")
		require.NoError(t, err)

		xDecl := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0]
		file := NodeTokenFile(fset, xDecl)
		assert.NotNil(t, file)
		assert.Equal(t, "main.xgo", file.Name())
	})

	t.Run("NilFileSet", func(t *testing.T) {
		file := NodeTokenFile(nil, &ast.Ident{Name: "test"})
		assert.Nil(t, file)
	})

	t.Run("NilNode", func(t *testing.T) {
		fset := token.NewFileSet()
		file := NodeTokenFile(fset, nil)
		assert.Nil(t, file)
	})
}

func TestPosASTFile(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var x = 1")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		xPos := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0].Pos()
		file := PosASTFile(fset, astPkg, xPos)
		assert.Equal(t, astFile, file)
	})

	t.Run("NilFileSet", func(t *testing.T) {
		astPkg := newTestPackage(nil)
		file := PosASTFile(nil, astPkg, token.Pos(1))
		assert.Nil(t, file)
	})

	t.Run("NilPackage", func(t *testing.T) {
		fset := token.NewFileSet()
		file := PosASTFile(fset, nil, token.Pos(1))
		assert.Nil(t, file)
	})

	t.Run("InvalidPos", func(t *testing.T) {
		fset := token.NewFileSet()
		astPkg := newTestPackage(nil)
		file := PosASTFile(fset, astPkg, token.NoPos)
		assert.Nil(t, file)
	})
}

func TestNodeASTFile(t *testing.T) {
	t.Run("Normal", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.xgo", "var x = 1")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		xDecl := astFile.Decls[0].(*ast.GenDecl).Specs[0].(*ast.ValueSpec).Names[0]
		file := NodeASTFile(fset, astPkg, xDecl)
		assert.Equal(t, astFile, file)
	})

	t.Run("NilFileSet", func(t *testing.T) {
		astPkg := newTestPackage(nil)
		file := NodeASTFile(nil, astPkg, &ast.Ident{Name: "test"})
		assert.Nil(t, file)
	})

	t.Run("NilPackage", func(t *testing.T) {
		fset := token.NewFileSet()
		file := NodeASTFile(fset, nil, &ast.Ident{Name: "test"})
		assert.Nil(t, file)
	})

	t.Run("NilNode", func(t *testing.T) {
		fset := token.NewFileSet()
		astPkg := newTestPackage(nil)
		file := NodeASTFile(fset, astPkg, nil)
		assert.Nil(t, file)
	})
}
