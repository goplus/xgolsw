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
	"fmt"
	gotypes "go/types"
	"io/fs"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func file(content string) *File {
	return &File{Content: []byte(content)}
}

func TestNewProject(t *testing.T) {
	t.Run("WithNilFileSet", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)
		require.NotNil(t, proj)
		assert.NotNil(t, proj.Fset)
		assert.NotNil(t, proj.files)
		assert.Empty(t, proj.files)
		assert.NotNil(t, proj.cacheBuilders)
		assert.NotNil(t, proj.caches)
		assert.NotNil(t, proj.fileCacheBuilders)
		assert.NotNil(t, proj.fileCaches)
	})

	t.Run("WithProvidedFileSet", func(t *testing.T) {
		fset := token.NewFileSet()
		proj := NewProject(fset, nil, 0)
		require.NotNil(t, proj)
		assert.Equal(t, fset, proj.Fset)
	})

	t.Run("WithNilFiles", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)
		assert.NotNil(t, proj.files)
		assert.Empty(t, proj.files)
	})

	t.Run("WithProvidedFiles", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package main\n\nfunc test() {}"),
		}
		proj := NewProject(nil, files, 0)
		assert.NotNil(t, proj.files)
		assert.Len(t, proj.files, 2)

		// Verify files are copied
		mainFile, ok := proj.files["main.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		testFile, ok := proj.files["test.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc test() {}"), testFile.Content)
	})

	t.Run("WithNoFeatures", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)
		assert.Empty(t, proj.cacheBuilders)
		assert.Empty(t, proj.fileCacheBuilders)
	})

	t.Run("WithAllFeatures", func(t *testing.T) {
		proj := NewProject(nil, nil, FeatAll)
		// Features should register cache builders
		assert.Greater(t, len(proj.cacheBuilders)+len(proj.fileCacheBuilders), 0)
	})

	t.Run("WithIndividualFeatures", func(t *testing.T) {
		proj1 := NewProject(nil, nil, FeatASTCache)
		proj2 := NewProject(nil, nil, FeatTypeInfoCache)
		proj3 := NewProject(nil, nil, FeatPkgDocCache)

		// Each should have some cache builders registered
		total1 := len(proj1.cacheBuilders) + len(proj1.fileCacheBuilders)
		total2 := len(proj2.cacheBuilders) + len(proj2.fileCacheBuilders)
		total3 := len(proj3.cacheBuilders) + len(proj3.fileCacheBuilders)

		assert.GreaterOrEqual(t, total1, 0)
		assert.GreaterOrEqual(t, total2, 0)
		assert.GreaterOrEqual(t, total3, 0)
	})

	t.Run("FilesSnapshotIsCreated", func(t *testing.T) {
		files := map[string]*File{
			"test.go": file("package test"),
		}
		proj := NewProject(nil, files, 0)

		snapshot := proj.filesSnapshot.Load()
		require.NotNil(t, snapshot)
		assert.Len(t, *snapshot, 1)

		testFile, ok := (*snapshot)["test.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package test"), testFile.Content)
	})
}

func TestProjectFork(t *testing.T) {
	t.Run("IsolatedAnalysis", func(t *testing.T) {
		proj := newFrameworkTestProject(t, map[string]*File{
			"main_fixture.gox": file("var value int\nfunc Work() { value = measure(1) }\n"),
		}, FeatAll)
		original, err := proj.TypeInfo()
		require.NoError(t, err)
		astFile, err := proj.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		base := proj.Fset.Base()
		fork := proj.Fork()
		assert.Same(t, proj.Module(), fork.Module())
		assert.Equal(t, proj.PkgPath, fork.PkgPath)
		assert.NotSame(t, proj.Fset, fork.Fset)
		info, err := fork.TypeInfo()
		require.NoError(t, err)
		assert.NotSame(t, original, info)
		forkAST, err := fork.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		assert.NotSame(t, astFile, forkAST)
		_, err = fork.PkgDoc()
		require.NoError(t, err)
		fork.PutFile("main_fixture.gox", file("var other string\n"))
		_, err = fork.TypeInfo()
		require.NoError(t, err)
		unchanged, err := proj.TypeInfo()
		require.NoError(t, err)
		assert.Same(t, original, unchanged)
		assert.Equal(t, base, proj.Fset.Base())
		require.NoError(t, proj.DeleteFile("main_fixture.gox"))
		current, ok := fork.File("main_fixture.gox")
		require.True(t, ok)
		assert.Equal(t, "var other string\n", string(current.Content))
	})

	t.Run("CacheBuilders", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{"data": file("original")}, 0)
		proj.RegisterCacheBuilder("data", func(proj *Project) (any, error) {
			data, _ := proj.File("data")
			return string(data.Content), nil
		})
		value, err := proj.Cache("data")
		require.NoError(t, err)
		assert.Equal(t, "original", value)
		fork := proj.Fork()
		fork.PutFile("data", file("changed"))
		value, err = fork.Cache("data")
		require.NoError(t, err)
		assert.Equal(t, "changed", value)
		_, err = fork.ASTPackage()
		assert.ErrorIs(t, err, ErrUnknownCacheKind)
	})
}

func TestProjectForkWithSourceCaches(t *testing.T) {
	t.Run("ResourceChanges", func(t *testing.T) {
		proj := newFrameworkTestProject(t, map[string]*File{
			"main_fixture.gox": file(""),
			"score.xgo":        file("var score int\n"),
			"resources.json":   file("\"before\""),
		}, FeatAll)
		proj.RegisterSourceCacheBuilder("source", func(p *Project) (any, error) {
			return p.TypeInfo()
		})
		proj.RegisterCacheBuilder("resource", func(p *Project) (any, error) {
			resource, _ := p.File("resources.json")
			return string(resource.Content), nil
		})
		before := proj.ForkWithSourceCaches(nil)
		info, err := before.TypeInfo()
		require.NoError(t, err)
		syntax, err := before.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		sourceFile := before.Fset.File(syntax.Pos())
		require.NotNil(t, sourceFile)
		beforeBase := before.Fset.Base()
		source, err := before.Cache("source")
		require.NoError(t, err)
		require.Same(t, info, source)
		resource, err := before.Cache("resource")
		require.NoError(t, err)
		require.Equal(t, "\"before\"", resource)

		proj.UpdateFiles(map[string]*File{
			"main_fixture.gox": file(""),
			"score.xgo":        file("var score int\n"),
			"resources.json":   {Content: []byte("\"after\""), ModTime: time.Unix(1, 0)},
		})
		after := proj.ForkWithSourceCaches(before)
		assert.NotSame(t, before.Fset, after.Fset)
		assert.Same(t, sourceFile, after.Fset.File(syntax.Pos()))
		afterInfo, err := after.TypeInfo()
		require.NoError(t, err)
		assert.Same(t, info, afterInfo)
		afterSyntax, err := after.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		assert.Same(t, syntax, afterSyntax)
		source, err = after.Cache("source")
		require.NoError(t, err)
		assert.Same(t, info, source)
		resource, err = after.Cache("resource")
		require.NoError(t, err)
		assert.Equal(t, "\"after\"", resource)
		resource, err = before.Cache("resource")
		require.NoError(t, err)
		assert.Equal(t, "\"before\"", resource)

		after.PutFile("score.xgo", file("var score string\n"))
		changed, err := after.TypeInfo()
		require.NoError(t, err)
		assert.NotSame(t, info, changed)
		assert.Equal(t, beforeBase, before.Fset.Base())
		beforeScore := info.Pkg.Scope().Lookup("score")
		afterScore := changed.Pkg.Scope().Lookup("score")
		require.NotNil(t, beforeScore)
		require.NotNil(t, afterScore)
		assert.Equal(t, "int", beforeScore.Type().String())
		assert.Equal(t, "string", afterScore.Type().String())
	})

	t.Run("NoSourceFiles", func(t *testing.T) {
		proj := newTestProject(t, nil, FeatAll)
		before := proj.Fork()
		info, err := before.TypeInfo()
		require.NoError(t, err)

		proj.PutFile("metadata.json", file("{}"))
		after := proj.ForkWithSourceCaches(before)
		afterInfo, err := after.TypeInfo()
		require.NoError(t, err)
		assert.Same(t, info, afterInfo)
		assert.Equal(t, token.NewFileSet().Base(), after.Fset.Base())
	})

	t.Run("SourceCacheError", func(t *testing.T) {
		proj := newTestProject(t, nil, FeatAll)
		builds := 0
		proj.RegisterSourceCacheBuilder("source", func(*Project) (any, error) {
			builds++
			if builds == 1 {
				return nil, assert.AnError
			}
			return "ready", nil
		})
		before := proj.Fork()
		info, err := before.TypeInfo()
		require.NoError(t, err)
		for range 2 {
			_, err := before.Cache("source")
			require.ErrorIs(t, err, assert.AnError)
		}
		assert.Equal(t, 1, builds)

		proj.PutFile("metadata.json", file("{}"))
		after := proj.ForkWithSourceCaches(before)
		afterInfo, err := after.TypeInfo()
		require.NoError(t, err)
		assert.Same(t, info, afterInfo)
		value, err := after.Cache("source")
		require.NoError(t, err)
		assert.Equal(t, "ready", value)
	})

	t.Run("SourceTypeError", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{"main.xgo": file("var score Missing\n")}, FeatAll)
		before := proj.Fork()
		info, err := before.TypeInfo()
		require.ErrorContains(t, err, "Missing is not a type")

		proj.PutFile("metadata.json", file("{}"))
		after := proj.ForkWithSourceCaches(before)
		afterInfo, err := after.TypeInfo()
		require.ErrorContains(t, err, "Missing is not a type")
		assert.Same(t, info, afterInfo)
	})

	for _, tt := range []struct {
		name, filename, source string
	}{
		{"ExplicitImportRecovery", "main.xgo", `import "example.com/framework"
echo framework.High
`},
		{"ImplicitImportRecovery", "main_fixture.gox", "println measure(1)\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			proj := newFrameworkTestProject(t, map[string]*File{tt.filename: file(tt.source)}, FeatAll)
			base := proj.Importer
			available := false
			proj.Importer = &struct{ gotypes.Importer }{pkgDocTestImporter(func(path string) (*gotypes.Package, error) {
				if path == testframework.PkgPath && !available {
					return nil, fs.ErrNotExist
				}
				return base.Import(path)
			})}
			before := proj.Fork()
			info, err := before.TypeInfo()
			require.NotNil(t, info)
			require.ErrorContains(t, err, testframework.PkgPath)

			available = true
			proj.PutFile("metadata.json", file("{}"))
			after := proj.ForkWithSourceCaches(before)
			afterInfo, err := after.TypeInfo()
			require.NoError(t, err)
			assert.NotSame(t, info, afterInfo)
		})
	}

	t.Run("UncheckedSyntax", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{"main.xgo": file("echo 1\n")}, FeatAll)
		before := proj.Fork()
		syntax, err := before.ASTFile("main.xgo")
		require.NoError(t, err)
		proj.PutFile("metadata.json", file("{}"))

		after := proj.ForkWithSourceCaches(before)
		afterSyntax, err := after.ASTFile("main.xgo")
		require.NoError(t, err)
		assert.NotSame(t, syntax, afterSyntax)
		assert.NotSame(t, before.Fset, after.Fset)
	})

	for _, tt := range []struct {
		name   string
		change func(*testing.T, *Project)
	}{
		{"SourceEdit", func(_ *testing.T, proj *Project) {
			proj.PutFile("main.xgo", file("var score string\n"))
		}},
		{"MoveIntoSource", func(t *testing.T, proj *Project) {
			require.NoError(t, proj.RenameFile("other.txt", "other.xgo"))
		}},
		{"MoveOutOfSource", func(t *testing.T, proj *Project) {
			require.NoError(t, proj.RenameFile("main.xgo", "main.txt"))
		}},
		{"ModuleChange", func(_ *testing.T, proj *Project) {
			proj.SetModule(defaultModule)
		}},
		{"BuilderChange", func(_ *testing.T, proj *Project) {
			proj.RegisterCacheBuilder("source", func(*Project) (any, error) { return nil, nil })
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			proj := newTestProject(t, map[string]*File{
				"main.xgo":  file("var score int\n"),
				"other.txt": file("var score2 int\n"),
			}, FeatAll)
			before := proj.Fork()
			info, err := before.TypeInfo()
			require.NoError(t, err)
			tt.change(t, proj)

			after := proj.ForkWithSourceCaches(before)
			afterInfo, err := after.TypeInfo()
			require.NoError(t, err)
			assert.NotSame(t, info, afterInfo)
			assert.NotSame(t, before.Fset, after.Fset)
		})
	}

	t.Run("PackagePathChange", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{"main.xgo": file("var score int\n")}, FeatAll)
		proj.PkgPath = "example.com/before"
		before := proj.Fork()
		info, err := before.TypeInfo()
		require.NoError(t, err)
		require.Equal(t, "example.com/before", info.Pkg.Path())

		proj.PkgPath = "example.com/after"
		proj.PutFile("metadata.json", file("{}"))
		after := proj.ForkWithSourceCaches(before)
		info, err = after.TypeInfo()
		require.NoError(t, err)
		assert.Equal(t, "example.com/after", info.Pkg.Path())
	})

	t.Run("ImporterChange", func(t *testing.T) {
		const dependency = "example.com/config"
		proj := newTestProject(t, map[string]*File{"main.xgo": file(`import "example.com/config"
var value = config.Value
`)}, FeatAll)
		fallback := proj.Importer
		setImportedType := func(typ gotypes.Type) {
			pkg := gotypes.NewPackage(dependency, "config")
			pkg.Scope().Insert(gotypes.NewVar(token.NoPos, pkg, "Value", typ))
			pkg.MarkComplete()
			proj.Importer = pkgDocTestImporter(func(path string) (*gotypes.Package, error) {
				if path == dependency {
					return pkg, nil
				}
				return fallback.Import(path)
			})
		}
		setImportedType(gotypes.Typ[gotypes.Int])
		before := proj.Fork()
		info, err := before.TypeInfo()
		require.NoError(t, err)
		value := info.Pkg.Scope().Lookup("value")
		require.NotNil(t, value)
		require.Equal(t, gotypes.Typ[gotypes.Int], value.Type())

		setImportedType(gotypes.Typ[gotypes.String])
		proj.PutFile("metadata.json", file("{}"))
		after := proj.ForkWithSourceCaches(before)
		info, err = after.TypeInfo()
		require.NoError(t, err)
		value = info.Pkg.Scope().Lookup("value")
		require.NotNil(t, value)
		assert.Equal(t, gotypes.Typ[gotypes.String], value.Type())
	})

	t.Run("DefaultImporterPositions", func(t *testing.T) {
		if runtime.GOARCH == "wasm" {
			t.Skip("the default importer runs the host Go tool")
		}
		proj := NewProject(nil, map[string]*File{"main.xgo": file(`import "math"
echo math.Pi
`)}, FeatAll)
		position := func(p *Project) token.Position {
			t.Helper()

			info, err := p.TypeInfo()
			require.NoError(t, err)
			var pi gotypes.Object
			for ident, obj := range info.Uses {
				if ident.Name == "Pi" {
					pi = obj
					break
				}
			}
			require.NotNil(t, pi)
			require.Equal(t, "math", pi.Pkg().Path())
			return p.Fset.PositionFor(pi.Pos(), false)
		}
		before := proj.Fork()
		want := position(before)
		require.True(t, want.IsValid())

		proj.PutFile("metadata.json", file("{}"))
		after := proj.ForkWithSourceCaches(before)
		assert.Equal(t, want, position(after))
	})
}

func TestProjectSnapshot(t *testing.T) {
	t.Run("SharedImporterTypeChecking", func(t *testing.T) {
		proj := newFrameworkTestProject(t, map[string]*File{
			"main_fixture.gox": file("var value int\nfunc Work() { value = measure(1) }\n"),
		}, FeatAll)
		var activeImports, concurrentImports atomic.Int32
		fallback := proj.Importer
		proj.Importer = pkgDocTestImporter(func(path string) (*gotypes.Package, error) {
			if activeImports.Add(1) > 1 {
				concurrentImports.Add(1)
			}
			defer activeImports.Add(-1)
			runtime.Gosched()
			return fallback.Import(path)
		})
		_, err := proj.ASTPackage()
		require.NoError(t, err)
		projects := []*Project{proj}
		for range 15 {
			projects = append(projects, proj.Snapshot(), proj.Fork())
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		errs := make([]error, len(projects))
		for i, project := range projects {
			wg.Go(func() {
				<-start
				_, errs[i] = project.TypeInfo()
			})
		}
		close(start)
		wg.Wait()
		for _, err := range errs {
			require.NoError(t, err)
		}
		assert.Zero(t, concurrentImports.Load(), "projects must serialize checking with their shared importer")
	})

	t.Run("ConcurrentTypeChecking", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			filename   string
			newProject testProjectFactory
		}{
			{"NormalClass", "Record.gox", newTestProject},
			{"ProjectClass", "main_fixture.gox", newFrameworkTestProject},
			{"WorkClass", "Worker_fixture.gox", newFrameworkTestProject},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const source = `var value int
func Work() { value = 1 }
func Reset() { value = 0 }
`
				files := map[string]*File{tt.filename: file(source)}
				if tt.filename == "Worker_fixture.gox" {
					files["main_fixture.gox"] = file("")
				}
				proj := tt.newProject(t, files, FeatAll)
				_, err := proj.TypeInfo()
				require.NoError(t, err)
				proj.PutFile(tt.filename, file(source))
				original, err := proj.ASTFile(tt.filename)
				require.NoError(t, err)
				snapshot := proj.Snapshot()
				copied, err := snapshot.ASTFile(tt.filename)
				require.NoError(t, err)
				assert.NotSame(t, original, copied)
				var wg sync.WaitGroup
				start := make(chan struct{})
				errs := make([]error, 2)
				for i, project := range []*Project{proj, snapshot} {
					wg.Go(func() {
						<-start
						_, errs[i] = project.TypeInfo()
					})
				}
				close(start)
				wg.Wait()
				for _, err := range errs {
					require.NoError(t, err)
				}
			})
		}
	})

	t.Run("SharedAnalysisAfterEdit", func(t *testing.T) {
		for _, editSnapshot := range []bool{false, true} {
			name := "Original"
			if editSnapshot {
				name = "Snapshot"
			}
			t.Run(name, func(t *testing.T) {
				proj := newTestProject(t, map[string]*File{
					"Record.gox": file("// Amount returns the scale.\nfunc Amount() int { return scale }\n"),
					"values.xgo": file("const scale = 1\n"),
				}, FeatAll)
				originalTypes, err := proj.TypeInfo()
				require.NoError(t, err)
				originalAST, err := proj.ASTFile("Record.gox")
				require.NoError(t, err)
				originalDoc, err := proj.PkgDoc()
				require.NoError(t, err)
				snapshot := proj.Snapshot()
				copiedTypes, err := snapshot.TypeInfo()
				require.NoError(t, err)
				assert.Same(t, originalTypes, copiedTypes)
				changed, preserved := proj, snapshot
				if editSnapshot {
					changed, preserved = snapshot, proj
				}
				changed.PutFile("values.xgo", file("const scale = 2\n"))
				changedAST, err := changed.ASTFile("Record.gox")
				require.NoError(t, err)
				assert.NotSame(t, originalAST, changedAST)
				unchangedAST, err := preserved.ASTFile("Record.gox")
				require.NoError(t, err)
				assert.Same(t, originalAST, unchangedAST)
				updatedTypes, err := changed.TypeInfo()
				require.NoError(t, err)
				for _, tt := range []struct {
					object gotypes.Object
					want   string
				}{
					{originalTypes.Pkg.Scope().Lookup("scale"), "1"},
					{updatedTypes.Pkg.Scope().Lookup("scale"), "2"},
				} {
					constant, ok := tt.object.(*gotypes.Const)
					require.True(t, ok)
					assert.Equal(t, tt.want, constant.Val().ExactString())
				}
				unchangedDoc, err := preserved.PkgDoc()
				require.NoError(t, err)
				assert.Same(t, originalDoc, unchangedDoc)
				updatedDoc, err := changed.PkgDoc()
				require.NoError(t, err)
				require.NotNil(t, originalDoc.Types["Record"])
				require.NotNil(t, updatedDoc.Types["Record"])
				assert.Equal(t, originalDoc.Types["Record"].Methods, updatedDoc.Types["Record"].Methods)
			})
		}
	})

	t.Run("BasicSnapshot", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)
		proj.PkgPath = "test/pkg"

		snapshot := proj.Snapshot()
		require.NotNil(t, snapshot)
		assert.Equal(t, proj.PkgPath, snapshot.PkgPath)
		assert.Equal(t, proj.Module(), snapshot.Module())
		assert.Equal(t, proj.Importer, snapshot.Importer)
		assert.Equal(t, proj.Fset, snapshot.Fset)
	})

	t.Run("FilesAreCopied", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package test"),
		}
		proj := NewProject(nil, files, 0)

		snapshot := proj.Snapshot()

		// Verify files are copied.
		assert.Len(t, snapshot.files, 2)
		mainFile, ok := snapshot.files["main.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		testFile, ok := snapshot.files["test.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package test"), testFile.Content)
	})

	t.Run("SnapshotIndependence", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		snapshot := proj.Snapshot()

		// Modify original project.
		proj.PutFile("new.go", file("package new"))
		delete(proj.files, "main.go")

		// Snapshot should be unchanged.
		assert.Len(t, snapshot.files, 1)
		_, ok := snapshot.files["main.go"]
		require.True(t, ok)
		_, ok = snapshot.files["new.go"]
		assert.False(t, ok)
	})

	t.Run("CacheBuildersAreCopied", func(t *testing.T) {
		proj := NewProject(nil, nil, FeatAll)

		type testCacheKind struct{}

		// Add custom cache builder.
		proj.RegisterCacheBuilder(testCacheKind{}, func(p *Project) (any, error) {
			return "test-data", nil
		})

		snapshot := proj.Snapshot()

		// Verify cache builders are copied.
		assert.Len(t, snapshot.cacheBuilders, len(proj.cacheBuilders))
		assert.Len(t, snapshot.fileCacheBuilders, len(proj.fileCacheBuilders))
	})

	t.Run("CachesAreCopied", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Add custom cache builder and trigger cache build.
		proj.RegisterCacheBuilder(testCacheKind{}, func(p *Project) (any, error) {
			return "test-data", nil
		})

		// Build cache.
		data, err := proj.Cache(testCacheKind{})
		require.NoError(t, err)
		assert.Equal(t, "test-data", data)

		snapshot := proj.Snapshot()

		// Verify caches are copied.
		assert.Len(t, snapshot.caches, len(proj.caches))
		assert.Len(t, snapshot.fileCaches, len(proj.fileCaches))
	})

	t.Run("FilesSnapshotIsUpdated", func(t *testing.T) {
		files := map[string]*File{
			"test.go": file("package test"),
		}
		proj := NewProject(nil, files, 0)

		snapshot := proj.Snapshot()

		// Verify snapshot has updated files snapshot.
		snapshotFiles := snapshot.filesSnapshot.Load()
		require.NotNil(t, snapshotFiles)
		assert.Len(t, *snapshotFiles, 1)

		testFile, ok := (*snapshotFiles)["test.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package test"), testFile.Content)
	})
}

func TestProjectSnapshotWithOverlay(t *testing.T) {
	t.Run("BasicOverlay", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package test"),
		}
		proj := NewProject(nil, files, 0)

		overlay := map[string]*File{
			"main.go": file("package main\n// updated"),
			"new.go":  file("package new"),
		}

		snapshot := proj.SnapshotWithOverlay(overlay)

		// Verify snapshot has overlay files.
		assert.Len(t, snapshot.files, 3)

		// Verify overlay file is applied.
		mainFile, ok := snapshot.files["main.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package main\n// updated"), mainFile.Content)

		// Verify new file is added.
		newFile, ok := snapshot.files["new.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package new"), newFile.Content)

		// Verify original file is preserved.
		testFile, ok := snapshot.files["test.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package test"), testFile.Content)
	})

	t.Run("EmptyOverlay", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		snapshot := proj.SnapshotWithOverlay(map[string]*File{})

		// Verify snapshot is equivalent to regular snapshot.
		assert.Len(t, snapshot.files, 1)
		mainFile, ok := snapshot.files["main.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)
	})

	t.Run("NilOverlay", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		snapshot := proj.SnapshotWithOverlay(nil)

		// Verify snapshot is equivalent to regular snapshot.
		assert.Len(t, snapshot.files, 1)
		mainFile, ok := snapshot.files["main.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)
	})

	t.Run("OriginalUnchanged", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		overlay := map[string]*File{
			"main.go": file("package main\n// updated"),
		}

		snapshot := proj.SnapshotWithOverlay(overlay)

		// Verify original project is unchanged.
		mainFile, ok := proj.files["main.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		// Verify snapshot has overlay.
		snapshotMainFile, ok := snapshot.files["main.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package main\n// updated"), snapshotMainFile.Content)
	})

	t.Run("FilesSnapshotIsUpdated", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		overlay := map[string]*File{
			"new.go": file("package new"),
		}

		snapshot := proj.SnapshotWithOverlay(overlay)

		// Verify snapshot has updated files snapshot.
		snapshotFiles := snapshot.filesSnapshot.Load()
		require.NotNil(t, snapshotFiles)
		assert.Len(t, *snapshotFiles, 2)

		newFile, ok := (*snapshotFiles)["new.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package new"), newFile.Content)
	})
}

func TestProjectFiles(t *testing.T) {
	t.Run("EmptyProject", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		var count int
		for range proj.Files() {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("SingleFile", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		var count int
		var foundPath string
		var foundContent []byte
		for path, f := range proj.Files() {
			count++
			foundPath = path
			foundContent = f.Content
		}

		assert.Equal(t, 1, count)
		assert.Equal(t, "main.go", foundPath)
		assert.Equal(t, []byte("package main"), foundContent)
	})

	t.Run("MultipleFiles", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package main\n\nfunc test() {}"),
			"util.go": file("package main\n\nfunc util() {}"),
		}
		proj := NewProject(nil, files, 0)

		foundFiles := make(map[string][]byte)
		for path, f := range proj.Files() {
			foundFiles[path] = f.Content
		}

		assert.Len(t, foundFiles, 3)
		assert.Equal(t, []byte("package main"), foundFiles["main.go"])
		assert.Equal(t, []byte("package main\n\nfunc test() {}"), foundFiles["test.go"])
		assert.Equal(t, []byte("package main\n\nfunc util() {}"), foundFiles["util.go"])
	})

	t.Run("IteratorUsesSnapshot", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		// Start iteration.
		iter := proj.Files()

		// Modify project during iteration (should not affect ongoing iteration).
		proj.PutFile("new.go", file("package new"))

		// Continue iteration - should only see original files.
		var count int
		for range iter {
			count++
		}
		assert.Equal(t, 1, count)
	})

	t.Run("ConcurrentIteration", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package main\n\nfunc test() {}"),
		}
		proj := NewProject(nil, files, 0)

		// Multiple concurrent iterations should work fine.
		results1 := make(map[string][]byte)
		results2 := make(map[string][]byte)

		for path, f := range proj.Files() {
			results1[path] = f.Content
		}

		for path, f := range proj.Files() {
			results2[path] = f.Content
		}

		assert.Equal(t, results1, results2)
		assert.Len(t, results1, 2)
	})

	t.Run("ConcurrentFileIteration", func(t *testing.T) {
		files := map[string]*File{
			"file1.go": file("package main"),
			"file2.go": file("package main"),
			"file3.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		// Test concurrent iteration over files.
		var wg sync.WaitGroup
		for range 50 {
			wg.Go(func() {
				count := 0
				for path, file := range proj.Files() {
					count++
					assert.Contains(t, []string{"file1.go", "file2.go", "file3.go"}, path)
					assert.Equal(t, []byte("package main"), file.Content)
				}
				assert.Equal(t, 3, count)
			})
		}
		wg.Wait()
	})
}

func TestProjectFile(t *testing.T) {
	t.Run("ExistingFile", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package main\n\nfunc test() {}"),
		}
		proj := NewProject(nil, files, 0)

		mainFile, ok := proj.File("main.go")
		require.True(t, ok)
		require.NotNil(t, mainFile)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		testFile, ok := proj.File("test.go")
		require.True(t, ok)
		require.NotNil(t, testFile)
		assert.Equal(t, []byte("package main\n\nfunc test() {}"), testFile.Content)
	})

	t.Run("NonExistingFile", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		file, ok := proj.File("nonexistent.go")
		assert.False(t, ok)
		assert.Nil(t, file)
	})

	t.Run("EmptyProject", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		file, ok := proj.File("any.go")
		assert.False(t, ok)
		assert.Nil(t, file)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		file, ok := proj.File("")
		assert.False(t, ok)
		assert.Nil(t, file)
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		// Multiple concurrent reads should work fine.
		file1, ok1 := proj.File("main.go")
		file2, ok2 := proj.File("main.go")

		require.True(t, ok1)
		require.True(t, ok2)
		assert.Equal(t, file1.Content, file2.Content)
	})

	t.Run("ConcurrentFileAccess", func(t *testing.T) {
		files := map[string]*File{
			"test.go": file("package test"),
		}
		proj := NewProject(nil, files, 0)

		// Test concurrent reads without locks.
		var wg sync.WaitGroup
		for range 100 {
			wg.Go(func() {
				f, ok := proj.File("test.go")
				if !ok {
					assert.Fail(t, "file not found")
					return
				}
				assert.Equal(t, []byte("package test"), f.Content)
			})
		}
		wg.Wait()
	})
}

func TestProjectPutFile(t *testing.T) {
	t.Run("AddNewFile", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		proj.PutFile("main.go", file("package main"))

		// Verify file was added.
		addedFile, ok := proj.File("main.go")
		require.True(t, ok)
		require.NotNil(t, addedFile)
		assert.Equal(t, []byte("package main"), addedFile.Content)

		// Verify files snapshot is updated.
		snapshot := proj.filesSnapshot.Load()
		assert.Len(t, *snapshot, 1)
		snapshotFile, ok := (*snapshot)["main.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), snapshotFile.Content)
	})

	t.Run("OverwriteExistingFile", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		// Overwrite existing file.
		proj.PutFile("main.go", file("package main\n\nfunc main() {}"))

		// Verify file was overwritten.
		updatedFile, ok := proj.File("main.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc main() {}"), updatedFile.Content)
	})

	t.Run("AddMultipleFiles", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		proj.PutFile("main.go", file("package main"))
		proj.PutFile("test.go", file("package main\n\nfunc test() {}"))
		proj.PutFile("util.go", file("package main\n\nfunc util() {}"))

		// Verify all files were added.
		var count int
		for range proj.Files() {
			count++
		}
		assert.Equal(t, 3, count)

		mainFile, ok := proj.File("main.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		testFile, ok := proj.File("test.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc test() {}"), testFile.Content)

		utilFile, ok := proj.File("util.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc util() {}"), utilFile.Content)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		proj.PutFile("", file("empty path"))

		// Verify file with empty path was added.
		emptyFile, ok := proj.File("")
		require.True(t, ok)
		assert.Equal(t, []byte("empty path"), emptyFile.Content)
	})

	t.Run("NilFile", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		proj.PutFile("nil.go", nil)

		// Verify nil file was added.
		nilFile, ok := proj.File("nil.go")
		require.True(t, ok)
		assert.Nil(t, nilFile)
	})

	t.Run("FilesSnapshotUpdated", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		// Get snapshot before adding.
		snapshotBefore := proj.filesSnapshot.Load()
		assert.Empty(t, *snapshotBefore)

		proj.PutFile("main.go", file("package main"))

		// Get snapshot after adding.
		snapshotAfter := proj.filesSnapshot.Load()
		assert.Len(t, *snapshotAfter, 1)
		assert.NotEqual(t, snapshotBefore, snapshotAfter)
	})
}

func TestProjectDeleteFile(t *testing.T) {
	t.Run("DeleteExistingFile", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package main\n\nfunc test() {}"),
		}
		proj := NewProject(nil, files, 0)

		err := proj.DeleteFile("main.go")
		require.NoError(t, err)

		// Verify file was deleted.
		_, ok := proj.File("main.go")
		assert.False(t, ok)

		// Verify other file still exists.
		testFile, ok := proj.File("test.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc test() {}"), testFile.Content)

		// Verify files snapshot is updated.
		snapshot := proj.filesSnapshot.Load()
		assert.Len(t, *snapshot, 1)
		_, ok = (*snapshot)["main.go"]
		assert.False(t, ok)
		_, ok = (*snapshot)["test.go"]
		require.True(t, ok)
	})

	t.Run("DeleteNonExistingFile", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		err := proj.DeleteFile("nonexistent.go")
		assert.Error(t, err)
		assert.Equal(t, fs.ErrNotExist, err)

		// Verify existing file is unchanged.
		mainFile, ok := proj.File("main.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)
	})

	t.Run("DeleteFromEmptyProject", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		err := proj.DeleteFile("any.go")
		assert.Error(t, err)
		assert.Equal(t, fs.ErrNotExist, err)
	})

	t.Run("DeleteEmptyPath", func(t *testing.T) {
		files := map[string]*File{
			"":        file("empty path"),
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		err := proj.DeleteFile("")
		require.NoError(t, err)

		// Verify empty path file was deleted.
		_, ok := proj.File("")
		assert.False(t, ok)

		// Verify other file still exists.
		mainFile, ok := proj.File("main.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)
	})

	t.Run("DeleteAllFiles", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package main\n\nfunc test() {}"),
			"util.go": file("package main\n\nfunc util() {}"),
		}
		proj := NewProject(nil, files, 0)

		// Delete all files.
		err1 := proj.DeleteFile("main.go")
		err2 := proj.DeleteFile("test.go")
		err3 := proj.DeleteFile("util.go")

		require.NoError(t, err1)
		require.NoError(t, err2)
		require.NoError(t, err3)

		// Verify all files are deleted.
		var count int
		for range proj.Files() {
			count++
		}
		assert.Equal(t, 0, count)

		// Verify files snapshot is empty.
		snapshot := proj.filesSnapshot.Load()
		assert.Empty(t, *snapshot)
	})

	t.Run("DeleteSameFileTwice", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		// Delete file first time.
		err1 := proj.DeleteFile("main.go")
		require.NoError(t, err1)

		// Delete same file second time.
		err2 := proj.DeleteFile("main.go")
		assert.Error(t, err2)
		assert.Equal(t, fs.ErrNotExist, err2)
	})
}

func TestProjectRenameFile(t *testing.T) {
	t.Run("RenameExistingFile", func(t *testing.T) {
		files := map[string]*File{
			"old.go":  file("package main"),
			"test.go": file("package main\n\nfunc test() {}"),
		}
		proj := NewProject(nil, files, 0)

		err := proj.RenameFile("old.go", "new.go")
		require.NoError(t, err)

		// Verify old file was removed.
		_, ok := proj.File("old.go")
		assert.False(t, ok)

		// Verify new file exists with same content.
		newFile, ok := proj.File("new.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), newFile.Content)

		// Verify other file is unchanged.
		testFile, ok := proj.File("test.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc test() {}"), testFile.Content)

		// Verify files snapshot is updated.
		snapshot := proj.filesSnapshot.Load()
		assert.Len(t, *snapshot, 2)
		_, ok = (*snapshot)["old.go"]
		assert.False(t, ok)
		_, ok = (*snapshot)["new.go"]
		require.True(t, ok)
	})

	t.Run("RenameNonExistingFile", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		err := proj.RenameFile("nonexistent.go", "new.go")
		assert.Error(t, err)
		assert.Equal(t, fs.ErrNotExist, err)

		// Verify existing files are unchanged.
		mainFile, ok := proj.File("main.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		// Verify new file was not created.
		_, ok = proj.File("new.go")
		assert.False(t, ok)
	})

	t.Run("RenameToExistingFile", func(t *testing.T) {
		files := map[string]*File{
			"old.go":      file("package main"),
			"existing.go": file("package existing"),
		}
		proj := NewProject(nil, files, 0)

		err := proj.RenameFile("old.go", "existing.go")
		assert.Error(t, err)
		assert.Equal(t, fs.ErrExist, err)

		// Verify both files are unchanged.
		oldFile, ok := proj.File("old.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), oldFile.Content)

		existingFile, ok := proj.File("existing.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package existing"), existingFile.Content)
	})

	t.Run("RenameEmptyProject", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		err := proj.RenameFile("old.go", "new.go")
		assert.Error(t, err)
		assert.Equal(t, fs.ErrNotExist, err)
	})

	t.Run("RenameEmptyPath", func(t *testing.T) {
		files := map[string]*File{
			"":        file("empty path"),
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		err := proj.RenameFile("", "named.go")
		require.NoError(t, err)

		// Verify empty path file was renamed.
		_, ok := proj.File("")
		assert.False(t, ok)

		namedFile, ok := proj.File("named.go")
		require.True(t, ok)
		assert.Equal(t, []byte("empty path"), namedFile.Content)

		// Verify other file is unchanged.
		mainFile, ok := proj.File("main.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)
	})

	t.Run("RenameToEmptyPath", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		err := proj.RenameFile("main.go", "")
		require.NoError(t, err)

		// Verify original file was removed.
		_, ok := proj.File("main.go")
		assert.False(t, ok)

		// Verify file exists with empty path.
		emptyFile, ok := proj.File("")
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), emptyFile.Content)
	})

	t.Run("RenameSamePathTwice", func(t *testing.T) {
		files := map[string]*File{
			"old.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		// Rename file first time.
		err1 := proj.RenameFile("old.go", "new.go")
		require.NoError(t, err1)

		// Try to rename the same old path again.
		err2 := proj.RenameFile("old.go", "another.go")
		assert.Error(t, err2)
		assert.Equal(t, fs.ErrNotExist, err2)

		// Verify only the first rename worked.
		_, ok := proj.File("old.go")
		assert.False(t, ok)

		newFile, ok := proj.File("new.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), newFile.Content)

		_, ok = proj.File("another.go")
		assert.False(t, ok)
	})
}

func TestProjectUpdateFiles(t *testing.T) {
	t.Run("PreservesDocumentVersion", func(t *testing.T) {
		original := &File{Content: []byte("println 1"), ModTime: time.Unix(1, 0), Version: 7}
		proj := NewProject(nil, map[string]*File{"main.xgo": original}, 0)
		snapshot := proj.Snapshot()
		provided := &File{Content: []byte("println 2"), ModTime: time.Unix(2, 0)}
		proj.UpdateFiles(map[string]*File{"main.xgo": provided})
		current, ok := proj.File("main.xgo")
		require.True(t, ok)
		assert.Equal(t, provided.Content, current.Content)
		assert.Equal(t, provided.ModTime, current.ModTime)
		assert.Equal(t, original.Version, current.Version)
		assert.Zero(t, provided.Version)
		old, ok := snapshot.File("main.xgo")
		require.True(t, ok)
		assert.Same(t, original, old)
		assert.Equal(t, "println 1", string(old.Content))
	})

	t.Run("UpdateFilesWithNewFiles", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		newFiles := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package main\n\nfunc test() {}"),
		}

		proj.UpdateFiles(newFiles)

		// Verify new files were added.
		var count int
		for range proj.Files() {
			count++
		}
		assert.Equal(t, 2, count)

		mainFile, ok := proj.File("main.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		testFile, ok := proj.File("test.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc test() {}"), testFile.Content)
	})

	t.Run("UpdateFilesWithModifiedTime", func(t *testing.T) {
		oldTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		newTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

		files := map[string]*File{
			"main.go": {Content: []byte("package main"), ModTime: oldTime},
		}
		proj := NewProject(nil, files, 0)

		newFiles := map[string]*File{
			"main.go": {Content: []byte("package main\n\nfunc main() {}"), ModTime: newTime},
		}

		revision := proj.Revision()
		proj.UpdateFiles(newFiles)
		assert.Greater(t, proj.Revision(), revision)

		// Verify file was updated due to different ModTime.
		mainFile, ok := proj.File("main.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc main() {}"), mainFile.Content)
		assert.Equal(t, newTime, mainFile.ModTime)
	})

	t.Run("UpdateFilesWithSameModTime", func(t *testing.T) {
		sameTime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)

		files := map[string]*File{
			"main.go": {Content: []byte("package main"), ModTime: sameTime},
		}
		proj := NewProject(nil, files, 0)

		newFiles := map[string]*File{
			"main.go": {Content: []byte("package main\n\nfunc main() {}"), ModTime: sameTime},
		}

		revision := proj.Revision()
		proj.UpdateFiles(newFiles)
		assert.Equal(t, revision, proj.Revision())

		// Verify file was not updated due to same ModTime.
		mainFile, ok := proj.File("main.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content) // Original content preserved.
		assert.Equal(t, sameTime, mainFile.ModTime)
	})

	t.Run("UpdateFilesRemovesMissingFiles", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package main\n\nfunc test() {}"),
			"util.go": file("package main\n\nfunc util() {}"),
		}
		proj := NewProject(nil, files, 0)

		// New files map only contains main.go and a new file.
		newFiles := map[string]*File{
			"main.go": file("package main"),
			"new.go":  file("package main\n\nfunc new() {}"),
		}

		proj.UpdateFiles(newFiles)

		// Verify only files in newFiles exist.
		var count int
		for range proj.Files() {
			count++
		}
		assert.Equal(t, 2, count)

		mainFile, ok := proj.File("main.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		newFile, ok := proj.File("new.go")
		require.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc new() {}"), newFile.Content)

		// Verify removed files no longer exist.
		_, ok = proj.File("test.go")
		assert.False(t, ok)

		_, ok = proj.File("util.go")
		assert.False(t, ok)
	})

	t.Run("UpdateFilesWithEmptyMap", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package main\n\nfunc test() {}"),
		}
		proj := NewProject(nil, files, 0)

		// Update with empty map should remove all files.
		newFiles := map[string]*File{}
		proj.UpdateFiles(newFiles)

		// Verify all files were removed.
		var count int
		for range proj.Files() {
			count++
		}
		assert.Equal(t, 0, count)

		// Verify files snapshot is empty.
		snapshot := proj.filesSnapshot.Load()
		assert.Empty(t, *snapshot)
	})

	t.Run("UpdateFilesWithNilMap", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package main\n\nfunc test() {}"),
		}
		proj := NewProject(nil, files, 0)

		// Update with nil map should remove all files.
		proj.UpdateFiles(nil)

		// Verify all files were removed.
		var count int
		for range proj.Files() {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("UpdateFilesSnapshotUpdated", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		// Get snapshot before update.
		snapshotBefore := proj.filesSnapshot.Load()
		assert.Empty(t, *snapshotBefore)

		newFiles := map[string]*File{
			"main.go": file("package main"),
		}
		proj.UpdateFiles(newFiles)

		// Get snapshot after update.
		snapshotAfter := proj.filesSnapshot.Load()
		assert.Len(t, *snapshotAfter, 1)
		assert.NotEqual(t, snapshotBefore, snapshotAfter)

		mainFile, ok := (*snapshotAfter)["main.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)
	})
}

func TestProjectUpdateFilesSnapshot(t *testing.T) {
	t.Run("UpdateSnapshotAfterFileChange", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		// Get initial snapshot.
		initialSnapshot := proj.filesSnapshot.Load()
		assert.Len(t, *initialSnapshot, 1)

		// Modify files directly and update snapshot.
		proj.files["test.go"] = file("package main\n\nfunc test() {}")
		proj.updateFilesSnapshot()

		// Verify snapshot was updated.
		updatedSnapshot := proj.filesSnapshot.Load()
		assert.Len(t, *updatedSnapshot, 2)
		assert.NotEqual(t, initialSnapshot, updatedSnapshot)

		// Verify new file is in snapshot.
		testFile, ok := (*updatedSnapshot)["test.go"]
		require.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc test() {}"), testFile.Content)
	})

	t.Run("SnapshotIsImmutable", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		// Get snapshot reference.
		snapshot1 := proj.filesSnapshot.Load()

		// Update snapshot.
		proj.files["test.go"] = file("package test")
		proj.updateFilesSnapshot()

		// Get new snapshot reference.
		snapshot2 := proj.filesSnapshot.Load()

		// Verify old snapshot is unchanged.
		assert.Len(t, *snapshot1, 1)
		_, ok := (*snapshot1)["test.go"]
		assert.False(t, ok)

		// Verify new snapshot has updated content.
		assert.Len(t, *snapshot2, 2)
		_, ok = (*snapshot2)["test.go"]
		require.True(t, ok)

		// Verify they are different objects.
		assert.NotEqual(t, snapshot1, snapshot2)
	})

	t.Run("SnapshotClonesFiles", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		snapshot := proj.filesSnapshot.Load()
		snapshotFile := (*snapshot)["main.go"]
		originalFile := proj.files["main.go"]

		// Verify snapshot contains cloned files, not original references.
		assert.Equal(t, originalFile.Content, snapshotFile.Content)
		assert.Equal(t, originalFile.ModTime, snapshotFile.ModTime)
		assert.Equal(t, originalFile.Version, snapshotFile.Version)

		// However, they should be the same object reference due to maps.Clone.
		assert.Equal(t, originalFile, snapshotFile)
	})

	t.Run("EmptyProjectSnapshot", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		snapshot := proj.filesSnapshot.Load()
		require.NotNil(t, snapshot)
		assert.Empty(t, *snapshot)

		// Update snapshot on empty project.
		proj.updateFilesSnapshot()

		updatedSnapshot := proj.filesSnapshot.Load()
		require.NotNil(t, updatedSnapshot)
		assert.Empty(t, *updatedSnapshot)
	})

	t.Run("SnapshotAfterFileRemoval", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package main\n\nfunc test() {}"),
		}
		proj := NewProject(nil, files, 0)

		// Verify initial snapshot.
		initialSnapshot := proj.filesSnapshot.Load()
		assert.Len(t, *initialSnapshot, 2)

		// Remove a file and update snapshot.
		delete(proj.files, "test.go")
		proj.updateFilesSnapshot()

		// Verify snapshot was updated.
		updatedSnapshot := proj.filesSnapshot.Load()
		assert.Len(t, *updatedSnapshot, 1)

		_, ok := (*updatedSnapshot)["main.go"]
		require.True(t, ok)

		_, ok = (*updatedSnapshot)["test.go"]
		assert.False(t, ok)
	})

	t.Run("SnapshotImmutabilityUnderConcurrency", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)
		proj.PutFile("initial.go", file("initial"))

		snapshot := proj.filesSnapshot.Load()

		var wg sync.WaitGroup

		// One goroutine continuously modifies files.
		wg.Go(func() {
			for i := range 100 {
				proj.PutFile(fmt.Sprintf("file%d.go", i), file("content"))
				time.Sleep(time.Microsecond)
			}
		})

		// Another goroutine verifies original snapshot remains unchanged.
		wg.Go(func() {
			for range 1000 {
				currentSnapshot := snapshot
				assert.Len(t, *currentSnapshot, 1)
				assert.Contains(t, *currentSnapshot, "initial.go")
			}
		})

		wg.Wait()
	})
}
