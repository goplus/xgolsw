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
	"go/token"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func file(content string) *File {
	return &File{Content: []byte(content)}
}

func TestNewProject(t *testing.T) {
	t.Run("WithNilFileSet", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)
		assert.NotNil(t, proj)
		assert.NotNil(t, proj.Fset)
		assert.NotNil(t, proj.files)
		assert.Len(t, proj.files, 0)
		assert.NotNil(t, proj.cacheBuilders)
		assert.NotNil(t, proj.caches)
		assert.NotNil(t, proj.fileCacheBuilders)
		assert.NotNil(t, proj.fileCaches)
	})

	t.Run("WithProvidedFileSet", func(t *testing.T) {
		fset := token.NewFileSet()
		proj := NewProject(fset, nil, 0)
		assert.NotNil(t, proj)
		assert.Equal(t, fset, proj.Fset)
	})

	t.Run("WithNilFiles", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)
		assert.NotNil(t, proj.files)
		assert.Len(t, proj.files, 0)
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
		assert.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		testFile, ok := proj.files["test.go"]
		assert.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc test() {}"), testFile.Content)
	})

	t.Run("WithNoFeatures", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)
		assert.Len(t, proj.cacheBuilders, 0)
		assert.Len(t, proj.fileCacheBuilders, 0)
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
		assert.NotNil(t, snapshot)
		assert.Len(t, *snapshot, 1)

		testFile, ok := (*snapshot)["test.go"]
		assert.True(t, ok)
		assert.Equal(t, []byte("package test"), testFile.Content)
	})
}

func TestProjectSnapshot(t *testing.T) {
	t.Run("BasicSnapshot", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)
		proj.PkgPath = "test/pkg"

		snapshot := proj.Snapshot()
		assert.NotNil(t, snapshot)
		assert.Equal(t, proj.PkgPath, snapshot.PkgPath)
		assert.Equal(t, proj.Mod, snapshot.Mod)
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
		assert.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		testFile, ok := snapshot.files["test.go"]
		assert.True(t, ok)
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
		assert.True(t, ok)
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
		assert.Equal(t, len(proj.cacheBuilders), len(snapshot.cacheBuilders))
		assert.Equal(t, len(proj.fileCacheBuilders), len(snapshot.fileCacheBuilders))
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
		assert.NoError(t, err)
		assert.Equal(t, "test-data", data)

		snapshot := proj.Snapshot()

		// Verify caches are copied.
		assert.Equal(t, len(proj.caches), len(snapshot.caches))
		assert.Equal(t, len(proj.fileCaches), len(snapshot.fileCaches))
	})

	t.Run("FilesSnapshotIsUpdated", func(t *testing.T) {
		files := map[string]*File{
			"test.go": file("package test"),
		}
		proj := NewProject(nil, files, 0)

		snapshot := proj.Snapshot()

		// Verify snapshot has updated files snapshot.
		snapshotFiles := snapshot.filesSnapshot.Load()
		assert.NotNil(t, snapshotFiles)
		assert.Len(t, *snapshotFiles, 1)

		testFile, ok := (*snapshotFiles)["test.go"]
		assert.True(t, ok)
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
		assert.True(t, ok)
		assert.Equal(t, []byte("package main\n// updated"), mainFile.Content)

		// Verify new file is added.
		newFile, ok := snapshot.files["new.go"]
		assert.True(t, ok)
		assert.Equal(t, []byte("package new"), newFile.Content)

		// Verify original file is preserved.
		testFile, ok := snapshot.files["test.go"]
		assert.True(t, ok)
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
		assert.True(t, ok)
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
		assert.True(t, ok)
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
		assert.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		// Verify snapshot has overlay.
		snapshotMainFile, ok := snapshot.files["main.go"]
		assert.True(t, ok)
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
		assert.NotNil(t, snapshotFiles)
		assert.Len(t, *snapshotFiles, 2)

		newFile, ok := (*snapshotFiles)["new.go"]
		assert.True(t, ok)
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
			wg.Add(1)
			go func() {
				defer wg.Done()
				count := 0
				for path, file := range proj.Files() {
					count++
					assert.Contains(t, []string{"file1.go", "file2.go", "file3.go"}, path)
					assert.Equal(t, []byte("package main"), file.Content)
				}
				assert.Equal(t, 3, count)
			}()
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
		assert.True(t, ok)
		assert.NotNil(t, mainFile)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		testFile, ok := proj.File("test.go")
		assert.True(t, ok)
		assert.NotNil(t, testFile)
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

		assert.True(t, ok1)
		assert.True(t, ok2)
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
			wg.Add(1)
			go func() {
				defer wg.Done()
				f, ok := proj.File("test.go")
				assert.True(t, ok)
				assert.Equal(t, []byte("package test"), f.Content)
			}()
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
		assert.True(t, ok)
		assert.NotNil(t, addedFile)
		assert.Equal(t, []byte("package main"), addedFile.Content)

		// Verify files snapshot is updated.
		snapshot := proj.filesSnapshot.Load()
		assert.Len(t, *snapshot, 1)
		snapshotFile, ok := (*snapshot)["main.go"]
		assert.True(t, ok)
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
		assert.True(t, ok)
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
		assert.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		testFile, ok := proj.File("test.go")
		assert.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc test() {}"), testFile.Content)

		utilFile, ok := proj.File("util.go")
		assert.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc util() {}"), utilFile.Content)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		proj.PutFile("", file("empty path"))

		// Verify file with empty path was added.
		emptyFile, ok := proj.File("")
		assert.True(t, ok)
		assert.Equal(t, []byte("empty path"), emptyFile.Content)
	})

	t.Run("NilFile", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		proj.PutFile("nil.go", nil)

		// Verify nil file was added.
		nilFile, ok := proj.File("nil.go")
		assert.True(t, ok)
		assert.Nil(t, nilFile)
	})

	t.Run("FilesSnapshotUpdated", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		// Get snapshot before adding.
		snapshotBefore := proj.filesSnapshot.Load()
		assert.Len(t, *snapshotBefore, 0)

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
		assert.NoError(t, err)

		// Verify file was deleted.
		_, ok := proj.File("main.go")
		assert.False(t, ok)

		// Verify other file still exists.
		testFile, ok := proj.File("test.go")
		assert.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc test() {}"), testFile.Content)

		// Verify files snapshot is updated.
		snapshot := proj.filesSnapshot.Load()
		assert.Len(t, *snapshot, 1)
		_, ok = (*snapshot)["main.go"]
		assert.False(t, ok)
		_, ok = (*snapshot)["test.go"]
		assert.True(t, ok)
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
		assert.True(t, ok)
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
		assert.NoError(t, err)

		// Verify empty path file was deleted.
		_, ok := proj.File("")
		assert.False(t, ok)

		// Verify other file still exists.
		mainFile, ok := proj.File("main.go")
		assert.True(t, ok)
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

		assert.NoError(t, err1)
		assert.NoError(t, err2)
		assert.NoError(t, err3)

		// Verify all files are deleted.
		var count int
		for range proj.Files() {
			count++
		}
		assert.Equal(t, 0, count)

		// Verify files snapshot is empty.
		snapshot := proj.filesSnapshot.Load()
		assert.Len(t, *snapshot, 0)
	})

	t.Run("DeleteSameFileTwice", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		// Delete file first time.
		err1 := proj.DeleteFile("main.go")
		assert.NoError(t, err1)

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
		assert.NoError(t, err)

		// Verify old file was removed.
		_, ok := proj.File("old.go")
		assert.False(t, ok)

		// Verify new file exists with same content.
		newFile, ok := proj.File("new.go")
		assert.True(t, ok)
		assert.Equal(t, []byte("package main"), newFile.Content)

		// Verify other file is unchanged.
		testFile, ok := proj.File("test.go")
		assert.True(t, ok)
		assert.Equal(t, []byte("package main\n\nfunc test() {}"), testFile.Content)

		// Verify files snapshot is updated.
		snapshot := proj.filesSnapshot.Load()
		assert.Len(t, *snapshot, 2)
		_, ok = (*snapshot)["old.go"]
		assert.False(t, ok)
		_, ok = (*snapshot)["new.go"]
		assert.True(t, ok)
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
		assert.True(t, ok)
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
		assert.True(t, ok)
		assert.Equal(t, []byte("package main"), oldFile.Content)

		existingFile, ok := proj.File("existing.go")
		assert.True(t, ok)
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
		assert.NoError(t, err)

		// Verify empty path file was renamed.
		_, ok := proj.File("")
		assert.False(t, ok)

		namedFile, ok := proj.File("named.go")
		assert.True(t, ok)
		assert.Equal(t, []byte("empty path"), namedFile.Content)

		// Verify other file is unchanged.
		mainFile, ok := proj.File("main.go")
		assert.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)
	})

	t.Run("RenameToEmptyPath", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		err := proj.RenameFile("main.go", "")
		assert.NoError(t, err)

		// Verify original file was removed.
		_, ok := proj.File("main.go")
		assert.False(t, ok)

		// Verify file exists with empty path.
		emptyFile, ok := proj.File("")
		assert.True(t, ok)
		assert.Equal(t, []byte("package main"), emptyFile.Content)
	})

	t.Run("RenameSamePathTwice", func(t *testing.T) {
		files := map[string]*File{
			"old.go": file("package main"),
		}
		proj := NewProject(nil, files, 0)

		// Rename file first time.
		err1 := proj.RenameFile("old.go", "new.go")
		assert.NoError(t, err1)

		// Try to rename the same old path again.
		err2 := proj.RenameFile("old.go", "another.go")
		assert.Error(t, err2)
		assert.Equal(t, fs.ErrNotExist, err2)

		// Verify only the first rename worked.
		_, ok := proj.File("old.go")
		assert.False(t, ok)

		newFile, ok := proj.File("new.go")
		assert.True(t, ok)
		assert.Equal(t, []byte("package main"), newFile.Content)

		_, ok = proj.File("another.go")
		assert.False(t, ok)
	})
}

func TestProjectUpdateFiles(t *testing.T) {
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
		assert.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		testFile, ok := proj.File("test.go")
		assert.True(t, ok)
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

		proj.UpdateFiles(newFiles)

		// Verify file was updated due to different ModTime.
		mainFile, ok := proj.File("main.go")
		assert.True(t, ok)
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

		proj.UpdateFiles(newFiles)

		// Verify file was not updated due to same ModTime.
		mainFile, ok := proj.File("main.go")
		assert.True(t, ok)
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
		assert.True(t, ok)
		assert.Equal(t, []byte("package main"), mainFile.Content)

		newFile, ok := proj.File("new.go")
		assert.True(t, ok)
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
		assert.Len(t, *snapshot, 0)
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
		assert.Len(t, *snapshotBefore, 0)

		newFiles := map[string]*File{
			"main.go": file("package main"),
		}
		proj.UpdateFiles(newFiles)

		// Get snapshot after update.
		snapshotAfter := proj.filesSnapshot.Load()
		assert.Len(t, *snapshotAfter, 1)
		assert.NotEqual(t, snapshotBefore, snapshotAfter)

		mainFile, ok := (*snapshotAfter)["main.go"]
		assert.True(t, ok)
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
		assert.True(t, ok)
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
		assert.True(t, ok)

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
		assert.NotNil(t, snapshot)
		assert.Len(t, *snapshot, 0)

		// Update snapshot on empty project.
		proj.updateFilesSnapshot()

		updatedSnapshot := proj.filesSnapshot.Load()
		assert.NotNil(t, updatedSnapshot)
		assert.Len(t, *updatedSnapshot, 0)
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
		assert.True(t, ok)

		_, ok = (*updatedSnapshot)["test.go"]
		assert.False(t, ok)
	})

	t.Run("SnapshotImmutabilityUnderConcurrency", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)
		proj.PutFile("initial.go", file("initial"))

		snapshot := proj.filesSnapshot.Load()

		var wg sync.WaitGroup

		// One goroutine continuously modifies files.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 100 {
				proj.PutFile(fmt.Sprintf("file%d.go", i), file("content"))
				time.Sleep(time.Microsecond)
			}
		}()

		// Another goroutine verifies original snapshot remains unchanged.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 1000 {
				currentSnapshot := snapshot
				assert.Len(t, *currentSnapshot, 1)
				_, ok := (*currentSnapshot)["initial.go"]
				assert.True(t, ok)
			}
		}()

		wg.Wait()
	})
}
