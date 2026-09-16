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
	"github.com/goplus/xgolsw/internal/testframework"
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
			newProject  testProjectFactory
		}{
			{name: "XGo", filename: "main.xgo", newProject: newTestProject},
			{name: "Gop", filename: "main.gop", newProject: newTestProject},
			{name: "NormalClass", filename: "Record.gox", isClass: true, isNormalGox: true, newProject: newTestProject},
			{name: "ProjectClass", filename: "main_fixture.gox", isClass: true, isProj: true, newProject: newFrameworkTestProject},
			{name: "WorkClass", filename: "Worker_fixture.gox", isClass: true, newProject: newFrameworkTestProject},
		} {
			t.Run(tt.name, func(t *testing.T) {
				proj := tt.newProject(t, map[string]*File{tt.filename: file(`var value int`)}, FeatASTCache)
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
		var parserErrs scanner.ErrorList
		require.ErrorAs(t, astPackageCache.parserErr, &parserErrs)
		require.Len(t, parserErrs, 1)
		assert.Equal(t, "main.xgo", parserErrs[0].Pos.Filename)
		assert.Empty(t, astPackageCache.astPkg.Files)
	})
}

func TestProjectASTPackage(t *testing.T) {
	t.Run("ValidPackage", func(t *testing.T) {
		proj := newFrameworkTestProject(t, map[string]*File{
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

func TestProjectIsSourceFile(t *testing.T) {
	proj := newFrameworkTestProject(t, nil, FeatAll)
	mod := testframework.NewModule(t)
	class := mod.Opt.Projects[0]
	class.Ext = ".fixture"
	class.Works[0].Ext = ".worker"
	proj.SetModule(newTestModule(t, mod.Module))
	for _, tt := range []struct {
		name     string
		filename string
		want     bool
	}{
		{"XGo", "src/main.xgo", true},
		{"LegacyXGo", "main.gop", true},
		{"StandaloneClass", "Record.gox", true},
		{"ProjectClass", "src/main.fixture", true},
		{"WorkClass", "src/Worker.worker", true},
		{"BuiltinClass", "check_test.gox", true},
		{"UnregisteredSpx", "main.spx", false},
		{"UnregisteredClass", "main.other", false},
		{"Go", "main.go", false},
		{"Resource", "assets/index.json", false},
		{"NoExtension", "README", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, proj.IsSourceFile(tt.filename))
		})
	}
}

func TestProjectRegisteredClassfiles(t *testing.T) {
	mod := testframework.NewModule(t)
	class := mod.Opt.Projects[0]
	class.Ext = ".fixture"
	class.Works[0].Ext = ".worker"
	require.NoError(t, mod.ImportClasses())
	proj := newFrameworkTestProjectWithModule(t, map[string]*File{
		"src/main.fixture":  file("// Count documentation.\nvar Count int\necho Count\n"),
		"src/Worker.worker": file("// Health documentation.\nvar Health int\necho Health, Value\n"),
		"Record.gox":        file("var Value int\n"),
		"ignored.spx":       file("invalid source {{{"),
		"ignored.other":     file("invalid source {{{"),
		"assets/index.json": file("{}"),
	}, FeatAll, mod)
	pkg, err := proj.ASTPackage()
	require.NoError(t, err)
	require.Len(t, pkg.Files, 3)
	require.Contains(t, pkg.Files, "src/main.fixture")
	require.Contains(t, pkg.Files, "src/Worker.worker")
	assert.True(t, pkg.Files["src/main.fixture"].IsProj)
	assert.True(t, pkg.Files["src/Worker.worker"].IsClass)
	info, err := proj.TypeInfo()
	require.NoError(t, err)
	require.NotNil(t, info.Pkg.Scope().Lookup("App"))
	require.NotNil(t, info.Pkg.Scope().Lookup("Worker"))
	doc, err := proj.PkgDoc()
	require.NoError(t, err)
	require.Contains(t, doc.Types, "App")
	require.Contains(t, doc.Types, "Worker")
	assert.Equal(t, "Count documentation.\n", doc.Types["App"].Fields["Count"])
	assert.Equal(t, "Health documentation.\n", doc.Types["Worker"].Fields["Health"])
}
