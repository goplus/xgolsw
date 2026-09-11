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
	"testing"

	"github.com/goplus/xgo/scanner"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPkgDocCache(t *testing.T) {
	t.Run("ValidProject", func(t *testing.T) {
		proj := newFrameworkTestProject(t, map[string]*File{
			"main_fixture.gox": file(`var (
	// Total counts the items.
	total int
)

// Reset clears the total.
func Reset() {
	total = 0
}
`),
		}, FeatASTCache)

		cache, err := buildPkgDocCache(proj)
		require.NoError(t, err)
		docCache, ok := cache.(*pkgDocCache)
		require.True(t, ok)
		require.NotNil(t, docCache.pkgDoc)

		doc := docCache.pkgDoc
		assert.Equal(t, proj.PkgPath, doc.Path)
		assert.Equal(t, "main", doc.Name)
		require.Len(t, doc.Types, 1)
		app := doc.Types["App"]
		require.NotNil(t, app)
		assert.Equal(t, map[string]string{"total": "Total counts the items.\n"}, app.Fields)
		assert.Equal(t, map[string]string{"Reset": "Reset clears the total.\n"}, app.Methods)
		assert.Empty(t, doc.Vars)
		assert.Empty(t, doc.Funcs)
	})

	t.Run("ASTCacheUnavailable", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{"main.xgo": file(`var value int`)}, 0)

		cache, err := buildPkgDocCache(proj)
		assert.ErrorIs(t, err, ErrUnknownCacheKind)
		assert.Nil(t, cache)
	})

	t.Run("ParserError", func(t *testing.T) {
		proj := newFrameworkTestProject(t, map[string]*File{
			"invalid_fixture.gox": file(`invalid syntax {{{`),
		}, FeatASTCache)

		cache, err := buildPkgDocCache(proj)
		var parserErrs scanner.ErrorList
		require.ErrorAs(t, err, &parserErrs)
		assert.NotEmpty(t, parserErrs)
		assert.Nil(t, cache)
	})
}

func TestProjectPkgDoc(t *testing.T) {
	t.Run("FileKinds", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			filename   string
			className  string
			newProject testProjectFactory
		}{
			{name: "XGo", filename: "main.xgo", newProject: newTestProject},
			{name: "Gop", filename: "main.gop", newProject: newTestProject},
			{name: "NormalClass", filename: "Record.gox", className: "Record", newProject: newTestProject},
			{name: "ProjectClass", filename: "main_fixture.gox", className: "App", newProject: newFrameworkTestProject},
			{name: "WorkClass", filename: "items/Worker_fixture.gox", className: "Worker", newProject: newFrameworkTestProject},
			{name: "NamedProject", filename: "Root_fixture.gox", className: "Root", newProject: func(t *testing.T, files map[string]*File, feats uint) *Project {
				t.Helper()

				mod := testframework.NewModule(t)
				class, ok := mod.LookupClass("_fixture.gox")
				require.True(t, ok)
				class.FullExt = "Root_fixture.gox"
				return newFrameworkTestProjectWithModule(t, files, feats, mod)
			}},
			{name: "PrefixedWork", filename: "Worker_fixture.gox", className: "TaskWorker", newProject: func(t *testing.T, files map[string]*File, feats uint) *Project {
				t.Helper()

				mod := testframework.NewModule(t)
				class, ok := mod.LookupClass("_fixture.gox")
				require.True(t, ok)
				class.Works[0].Prefix = "Task"
				return newFrameworkTestProjectWithModule(t, files, feats, mod)
			}},
			{name: "NormalizedClassName", filename: "work-item_fixture.gox", className: "work_item", newProject: newFrameworkTestProject},
			{name: "TestClass", filename: "Check_test.gox", className: "caseCheck", newProject: newTestProject},
		} {
			t.Run(tt.name, func(t *testing.T) {
				proj := tt.newProject(t, map[string]*File{
					tt.filename: file(`var (
	// Value stores the count.
	value int
)

// Increment increases the count.
func Increment() {
	value++
}
`),
				}, FeatASTCache|FeatPkgDocCache)
				doc, err := proj.PkgDoc()
				require.NoError(t, err)
				require.NotNil(t, doc)
				assert.Equal(t, proj.PkgPath, doc.Path)
				assert.Equal(t, "main", doc.Name)

				wantVars := map[string]string{"value": "Value stores the count.\n"}
				wantFuncs := map[string]string{"Increment": "Increment increases the count.\n"}
				if tt.className == "" {
					assert.Empty(t, doc.Types)
					assert.Equal(t, wantVars, doc.Vars)
					assert.Equal(t, wantFuncs, doc.Funcs)
					return
				}
				require.Len(t, doc.Types, 1)
				class := doc.Types[tt.className]
				require.NotNil(t, class)
				assert.Equal(t, wantVars, class.Fields)
				assert.Equal(t, wantFuncs, class.Methods)
				assert.Empty(t, doc.Vars)
				assert.Empty(t, doc.Funcs)
			})
		}
	})

	t.Run("MixedFiles", func(t *testing.T) {
		proj := newFrameworkTestProject(t, map[string]*File{
			"main_fixture.gox": file(`var (
	// Total belongs to the project.
	total int
)

// Reset clears the project total.
func Reset() {}
`),
			"Worker_fixture.gox": file(`var (
	// Value belongs to the worker.
	value int
)

// Reset clears the worker value.
func Reset() {}

println "worker"
`),
			"helpers.xgo": file(`// Package main documents a mixed project.
package main

// Limit bounds the count.
const Limit = 10

// Shared is a package variable.
var shared int

// Reset clears shared state.
func Reset() {}

// Read returns the project total.
func (a *App) Read() int { return a.total }
`),
		}, FeatASTCache|FeatPkgDocCache)

		doc, err := proj.PkgDoc()
		require.NoError(t, err)
		require.NotNil(t, doc)
		assert.Equal(t, "Package main documents a mixed project.\n", doc.Doc)
		assert.Equal(t, map[string]string{"Limit": "Limit bounds the count.\n"}, doc.Consts)
		assert.Equal(t, map[string]string{"shared": "Shared is a package variable.\n"}, doc.Vars)
		assert.Equal(t, map[string]string{"Reset": "Reset clears shared state.\n"}, doc.Funcs)
		require.Len(t, doc.Types, 2)
		app := doc.Types["App"]
		require.NotNil(t, app)
		assert.Equal(t, map[string]string{"total": "Total belongs to the project.\n"}, app.Fields)
		assert.Equal(t, map[string]string{
			"Reset": "Reset clears the project total.\n",
			"Read":  "Read returns the project total.\n",
		}, app.Methods)
		worker := doc.Types["Worker"]
		require.NotNil(t, worker)
		assert.Equal(t, map[string]string{"value": "Value belongs to the worker.\n"}, worker.Fields)
		assert.Equal(t, map[string]string{"Reset": "Reset clears the worker value.\n"}, worker.Methods)
	})

	t.Run("EmptyClassName", func(t *testing.T) {
		mod := testframework.NewModule(t)
		class, ok := mod.LookupClass("_fixture.gox")
		require.True(t, ok)
		class.Class = ""
		proj := newFrameworkTestProjectWithModule(t, map[string]*File{
			"main_fixture.gox": file(`var (
	// Total belongs to the unnamed project class.
	total int
)

// Clear belongs to the unnamed project class.
func Clear() {}
`),
			"Worker_fixture.gox": file(`var (
	// Value belongs to Worker.
	value int
)

// Reset belongs to Worker.
func Reset() {}
`),
			"helpers.xgo": file(`// Reset is a package function.
func Reset() {}
`),
		}, FeatASTCache|FeatPkgDocCache, mod)

		doc, err := proj.PkgDoc()
		require.NoError(t, err)
		require.NotNil(t, doc)
		assert.Empty(t, doc.Vars)
		assert.Equal(t, map[string]string{"Reset": "Reset is a package function.\n"}, doc.Funcs)
		require.Len(t, doc.Types, 1)
		worker := doc.Types["Worker"]
		require.NotNil(t, worker)
		assert.Equal(t, map[string]string{"value": "Value belongs to Worker.\n"}, worker.Fields)
		assert.Equal(t, map[string]string{"Reset": "Reset belongs to Worker.\n"}, worker.Methods)
	})

	t.Run("Enum", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`// Color describes a display color.
type Color const (
	// Red is the first color.
	Red = iota

	// Green is the second color.
	Green
)
`),
		}, FeatASTCache|FeatPkgDocCache)

		doc, err := proj.PkgDoc()
		require.NoError(t, err)
		require.NotNil(t, doc)
		require.Len(t, doc.Types, 1)
		color := doc.Types["Color"]
		require.NotNil(t, color)
		assert.Equal(t, "Color describes a display color.\n", color.Doc)
		assert.Equal(t, map[string]string{
			"Red":   "Red is the first color.\n",
			"Green": "Green is the second color.\n",
		}, color.EnumMembers)
		assert.Empty(t, doc.Consts)
	})

	t.Run("Cache", func(t *testing.T) {
		proj := newFrameworkTestProject(t, map[string]*File{
			"Worker_fixture.gox": file(`var (
	// Value stores the count.
	value int
)

// Reset clears the count.
func Reset() {}
`),
		}, FeatASTCache|FeatPkgDocCache)

		before, err := proj.PkgDoc()
		require.NoError(t, err)
		require.NotNil(t, before)
		cached, err := proj.PkgDoc()
		require.NoError(t, err)
		assert.Same(t, before, cached)

		proj.PutFile("Worker_fixture.gox", file(`var (
	// Label names the worker.
	label string
)
`))
		after, err := proj.PkgDoc()
		require.NoError(t, err)
		require.NotNil(t, after)
		assert.NotSame(t, before, after)
		worker := after.Types["Worker"]
		require.NotNil(t, worker)
		assert.Equal(t, map[string]string{"label": "Label names the worker.\n"}, worker.Fields)
		assert.Empty(t, worker.Methods)
		oldWorker := before.Types["Worker"]
		require.NotNil(t, oldWorker)
		assert.Equal(t, map[string]string{"value": "Value stores the count.\n"}, oldWorker.Fields)
		assert.Equal(t, map[string]string{"Reset": "Reset clears the count.\n"}, oldWorker.Methods)
	})

	t.Run("CacheError", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{"main.xgo": file(`var value int`)}, 0)

		doc, err := proj.PkgDoc()
		assert.ErrorIs(t, err, ErrUnknownCacheKind)
		assert.Nil(t, doc)
	})
}
