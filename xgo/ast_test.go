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

package xgo

import (
	"go/scanner"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildASTFileCache(t *testing.T) {
	t.Run("ValidFile", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"main.spx": file(`
// Package documentation.  
var x int

func Test() {
	println("test")
}
`),
		}, FeatAll)

		cache, err := buildASTFileCache(proj, "main.spx", proj.files["main.spx"])
		require.NoError(t, err)
		require.NotNil(t, cache)

		astFileCache, ok := cache.(*astFileCache)
		require.True(t, ok)
		require.NotNil(t, astFileCache.astFile)
		assert.NoError(t, astFileCache.parserErr)

		// Verify the AST structure.
		astFile := astFileCache.astFile
		assert.NotNil(t, astFile.Name)
		assert.Equal(t, "main", astFile.Name.Name)
		assert.NotEmpty(t, astFile.Decls)
	})

	t.Run("InvalidFile", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"invalid.spx": file(`invalid syntax {{{`),
		}, FeatAll)

		cache, err := buildASTFileCache(proj, "invalid.spx", proj.files["invalid.spx"])
		require.NoError(t, err)
		require.NotNil(t, cache)

		astFileCache, ok := cache.(*astFileCache)
		require.True(t, ok)

		// Should have parser error.
		assert.Error(t, astFileCache.parserErr)

		// Check if it's a scanner error list.
		if el, ok := astFileCache.parserErr.(scanner.ErrorList); ok {
			assert.NotEmpty(t, el)
		}
	})

	t.Run("DifferentFileTypes", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"test.gop": file(`var x int`),
			"test.xgo": file(`var y string`),
		}, FeatAll)

		// Test .gop file.
		cache1, err1 := buildASTFileCache(proj, "test.gop", proj.files["test.gop"])
		require.NoError(t, err1)
		require.NotNil(t, cache1)

		// Test .xgo file.
		cache2, err2 := buildASTFileCache(proj, "test.xgo", proj.files["test.xgo"])
		require.NoError(t, err2)
		require.NotNil(t, cache2)

		astFileCache1 := cache1.(*astFileCache)
		astFileCache2 := cache2.(*astFileCache)
		assert.NotNil(t, astFileCache1.astFile)
		assert.NotNil(t, astFileCache2.astFile)
	})
}

func TestProjectASTFile(t *testing.T) {
	t.Run("ValidFile", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"main.spx": file(`
// Package documentation.
var x int

func Test() {
	println("test")
}
`),
		}, FeatAll)

		astFile, err := proj.ASTFile("main.spx")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		assert.NotNil(t, astFile.Name)
		assert.Equal(t, "main", astFile.Name.Name)
		assert.NotEmpty(t, astFile.Decls)
	})

	t.Run("InvalidFile", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"invalid.spx": file(`invalid syntax {{{`),
		}, FeatAll)

		astFile, err := proj.ASTFile("invalid.spx")
		// Should have error but may still return partial AST.
		assert.Error(t, err)

		// May or may not have partial AST depending on how severe the error is.
		_ = astFile

		// Check if it's a scanner error list.
		if el, ok := err.(scanner.ErrorList); ok {
			assert.NotEmpty(t, el)
		}
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"main.spx": file(`var x int`),
		}, FeatAll)

		astFile, err := proj.ASTFile("nonexistent.spx")
		assert.Error(t, err)
		assert.Nil(t, astFile)
	})
}

func TestBuildASTPackageCache(t *testing.T) {
	t.Run("ValidPackage", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"main.spx": file(`
var mainVar int
func MainFunc() {}
`),
			"sprite.spx": file(`
var spriteVar string  
func SpriteFunc() {}
`),
		}, FeatAll)

		cache, err := buildASTPackageCache(proj)
		require.NoError(t, err)
		require.NotNil(t, cache)

		astPackageCache, ok := cache.(*astPackageCache)
		require.True(t, ok)
		require.NotNil(t, astPackageCache.astPkg)
		assert.NoError(t, astPackageCache.parserErr)

		// Verify the package structure.
		astPkg := astPackageCache.astPkg
		assert.Equal(t, "main", astPkg.Name)
		assert.Len(t, astPkg.Files, 2)
		assert.Contains(t, astPkg.Files, "main.spx")
		assert.Contains(t, astPkg.Files, "sprite.spx")
	})

	t.Run("PartiallyValidPackage", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"valid.spx": file(`
var validVar int
func ValidFunc() {}
`),
			"invalid.spx": file(`invalid syntax {{{`),
		}, FeatAll)

		cache, err := buildASTPackageCache(proj)
		require.NoError(t, err)
		require.NotNil(t, cache)

		astPackageCache, ok := cache.(*astPackageCache)
		require.True(t, ok)
		require.NotNil(t, astPackageCache.astPkg)

		// Should have parser error.
		assert.Error(t, astPackageCache.parserErr)

		// Should still contain the valid file.
		astPkg := astPackageCache.astPkg
		assert.Equal(t, "main", astPkg.Name)
		assert.Contains(t, astPkg.Files, "valid.spx")
		assert.NotNil(t, astPkg.Files["valid.spx"])
	})
}

func TestProjectASTPackage(t *testing.T) {
	t.Run("ValidPackage", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"main.spx": file(`
// Main file.
var mainVar int

func MainFunc() {
	println("main")
}
`),
			"sprite.spx": file(`
// Sprite file.
var spriteVar string

func SpriteFunc() {
	println("sprite")
}
`),
		}, FeatAll)

		astPkg, err := proj.ASTPackage()
		require.NoError(t, err)
		require.NotNil(t, astPkg)

		assert.Equal(t, "main", astPkg.Name)
		assert.Len(t, astPkg.Files, 2)
		assert.Contains(t, astPkg.Files, "main.spx")
		assert.Contains(t, astPkg.Files, "sprite.spx")

		// Check that both files are parsed.
		assert.NotNil(t, astPkg.Files["main.spx"])
		assert.NotNil(t, astPkg.Files["sprite.spx"])
	})

	t.Run("Cache", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"main.spx": file(`var x int`),
		}, FeatAll)

		// First call.
		astPkg1, err1 := proj.ASTPackage()
		require.NoError(t, err1)
		require.NotNil(t, astPkg1)

		// Second call should return the same cached instance.
		astPkg2, err2 := proj.ASTPackage()
		require.NoError(t, err2)
		require.NotNil(t, astPkg2)

		// Should be the same instance due to caching.
		assert.Same(t, astPkg1, astPkg2)
	})
}
