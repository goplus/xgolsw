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
	"io/fs"
	"testing"

	"github.com/goplus/xgo/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildASTFileCache(t *testing.T) {
	t.Run("ValidFile", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`
// Package documentation.
var x int

func Test() {
	println("test")
}
`),
		}, FeatASTCache)

		cache, err := buildASTFileCache(proj, "main.xgo", proj.files["main.xgo"])
		require.NoError(t, err)
		require.NotNil(t, cache)

		astFileCache, ok := cache.(*astFileCache)
		require.True(t, ok)
		require.NotNil(t, astFileCache.astFile)
		assert.NoError(t, astFileCache.parserErr)

		// Verify the AST structure.
		astFile := astFileCache.astFile
		require.NotNil(t, astFile.Name)
		assert.Equal(t, "main", astFile.Name.Name)
		assert.NotEmpty(t, astFile.Decls)
	})

	t.Run("InvalidFile", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"invalid.xgo": file(`invalid syntax {{{`),
		}, FeatASTCache)

		cache, err := buildASTFileCache(proj, "invalid.xgo", proj.files["invalid.xgo"])
		require.NoError(t, err)
		require.NotNil(t, cache)

		astFileCache, ok := cache.(*astFileCache)
		require.True(t, ok)

		var parserErrs scanner.ErrorList
		require.ErrorAs(t, astFileCache.parserErr, &parserErrs)
		assert.NotEmpty(t, parserErrs)
	})

	t.Run("DifferentFileTypes", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			filename    string
			isClass     bool
			isProj      bool
			isNormalGox bool
		}{
			{name: "XGo", filename: "main.xgo"},
			{name: "Gop", filename: "main.gop"},
			{name: "NormalClass", filename: "Record.gox", isClass: true, isNormalGox: true},
			{name: "ProjectClass", filename: "main_fixture.gox", isClass: true, isProj: true},
			{name: "WorkClass", filename: "Worker_fixture.gox", isClass: true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				proj := newTestProject(t, map[string]*File{tt.filename: file(`var value int`)}, FeatASTCache)
				cache, err := buildASTFileCache(proj, tt.filename, proj.files[tt.filename])
				require.NoError(t, err)
				astCache, ok := cache.(*astFileCache)
				require.True(t, ok)
				require.NoError(t, astCache.parserErr)
				astFile := astCache.astFile
				require.NotNil(t, astFile)
				assert.Equal(t, tt.isClass, astFile.IsClass)
				assert.Equal(t, tt.isProj, astFile.IsProj)
				assert.Equal(t, tt.isNormalGox, astFile.IsNormalGox)
				if tt.isClass {
					assert.NotNil(t, astFile.ClassFields)
				} else {
					assert.Nil(t, astFile.ClassFields)
				}
			})
		}
	})

	t.Run("RecoverFromPanic", func(t *testing.T) {
		cache, err := buildASTFileCache(nil, "main.xgo", file(`var x int`))
		assert.Nil(t, cache)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parser panic")
	})
}

func TestProjectASTFile(t *testing.T) {
	t.Run("ValidFile", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`
// Package documentation.
var x int

func Test() {
	println("test")
}
`),
		}, FeatASTCache)

		astFile, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)
		require.NotNil(t, astFile)

		require.NotNil(t, astFile.Name)
		assert.Equal(t, "main", astFile.Name.Name)
		assert.NotEmpty(t, astFile.Decls)
	})

	t.Run("InvalidFile", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"invalid.xgo": file(`invalid syntax {{{`),
		}, FeatASTCache)

		_, err := proj.ASTFile("invalid.xgo")
		var parserErrs scanner.ErrorList
		require.ErrorAs(t, err, &parserErrs)
		assert.NotEmpty(t, parserErrs)
	})

	t.Run("NonExistentFile", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`var x int`),
		}, FeatASTCache)

		astFile, err := proj.ASTFile("nonexistent.xgo")
		assert.ErrorIs(t, err, fs.ErrNotExist)
		assert.Nil(t, astFile)
	})
}

func TestBuildASTPackageCache(t *testing.T) {
	t.Run("ValidPackage", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`
var mainVar int
func MainFunc() {}
`),
			"Worker.gox": file(`
var workVar string
func WorkFunc() {}
`),
		}, FeatASTCache)

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
		assert.Contains(t, astPkg.Files, "main.xgo")
		assert.Contains(t, astPkg.Files, "Worker.gox")
	})

	t.Run("PartiallyValidPackage", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"valid.xgo": file(`
var validVar int
func ValidFunc() {}
`),
			"invalid.xgo": file(`invalid syntax {{{`),
		}, FeatASTCache)

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
		assert.Contains(t, astPkg.Files, "valid.xgo")
		assert.NotNil(t, astPkg.Files["valid.xgo"])
	})

	t.Run("ASTFileNonScannerError", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`var x int`),
		}, 0)

		cache, err := buildASTPackageCache(proj)
		require.NoError(t, err)
		require.NotNil(t, cache)

		astPackageCache, ok := cache.(*astPackageCache)
		require.True(t, ok)
		require.NotNil(t, astPackageCache.astPkg)
		assert.Error(t, astPackageCache.parserErr)
		assert.Contains(t, astPackageCache.parserErr.Error(), ErrUnknownCacheKind.Error())
		assert.Empty(t, astPackageCache.astPkg.Files)
	})
}

func TestProjectASTPackage(t *testing.T) {
	t.Run("ValidPackage", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main_fixture.gox": file(`
// Main file.
var mainVar int

func MainFunc() {
	println("main")
}
`),
			"Worker_fixture.gox": file(`
// Work file.
var workVar string

func WorkFunc() {
	println("work")
}
`),
		}, FeatASTCache)

		astPkg, err := proj.ASTPackage()
		require.NoError(t, err)
		require.NotNil(t, astPkg)

		assert.Equal(t, "main", astPkg.Name)
		require.Len(t, astPkg.Files, 2)
		projectFile := astPkg.Files["main_fixture.gox"]
		require.NotNil(t, projectFile)
		assert.True(t, projectFile.IsClass)
		assert.True(t, projectFile.IsProj)
		workFile := astPkg.Files["Worker_fixture.gox"]
		require.NotNil(t, workFile)
		assert.True(t, workFile.IsClass)
		assert.False(t, workFile.IsProj)
	})

	t.Run("Cache", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`var x int`),
		}, FeatASTCache)

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
