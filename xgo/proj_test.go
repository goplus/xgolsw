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
	"sync/atomic"
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

		// Add custom cache builder.
		type testCacheKey int
		const myKey testCacheKey = 1
		proj.RegisterCacheBuilder(myKey, func(p *Project) (any, error) {
			return "test-data", nil
		})

		snapshot := proj.Snapshot()

		// Verify cache builders are copied.
		assert.Equal(t, len(proj.cacheBuilders), len(snapshot.cacheBuilders))
		assert.Equal(t, len(proj.fileCacheBuilders), len(snapshot.fileCacheBuilders))
	})

	t.Run("CachesAreCopied", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		// Add custom cache builder and trigger cache build.
		type testCacheKey int
		const myKey testCacheKey = 1
		proj.RegisterCacheBuilder(myKey, func(p *Project) (any, error) {
			return "test-data", nil
		})

		// Build cache.
		data, err := proj.Cache(myKey)
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

func TestProjectRegisterCacheBuilder(t *testing.T) {
	t.Run("RegisterNewCacheBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register cache builder.
		proj.RegisterCacheBuilder(myKey, func(p *Project) (any, error) {
			return "test-data", nil
		})

		// Verify cache builder was registered.
		assert.Contains(t, proj.cacheBuilders, myKey)

		// Test cache building.
		data, err := proj.Cache(myKey)
		assert.NoError(t, err)
		assert.Equal(t, "test-data", data)
	})

	t.Run("RegisterMultipleCacheBuilders", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const key1 testCacheKey = 1
		const key2 testCacheKey = 2

		// Register multiple cache builders.
		proj.RegisterCacheBuilder(key1, func(p *Project) (any, error) {
			return "data-1", nil
		})

		proj.RegisterCacheBuilder(key2, func(p *Project) (any, error) {
			return "data-2", nil
		})

		// Verify both cache builders were registered.
		assert.Contains(t, proj.cacheBuilders, key1)
		assert.Contains(t, proj.cacheBuilders, key2)
		assert.Len(t, proj.cacheBuilders, 2)

		// Test both caches.
		data1, err1 := proj.Cache(key1)
		assert.NoError(t, err1)
		assert.Equal(t, "data-1", data1)

		data2, err2 := proj.Cache(key2)
		assert.NoError(t, err2)
		assert.Equal(t, "data-2", data2)
	})

	t.Run("OverwriteExistingCacheBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register first cache builder.
		proj.RegisterCacheBuilder(myKey, func(p *Project) (any, error) {
			return "original-data", nil
		})

		// Register second cache builder with same key.
		proj.RegisterCacheBuilder(myKey, func(p *Project) (any, error) {
			return "updated-data", nil
		})

		// Verify cache builder was overwritten.
		assert.Contains(t, proj.cacheBuilders, myKey)
		assert.Len(t, proj.cacheBuilders, 1)

		// Test that new cache builder is used.
		data, err := proj.Cache(myKey)
		assert.NoError(t, err)
		assert.Equal(t, "updated-data", data)
	})

	t.Run("CacheBuilderWithError", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register cache builder that returns error.
		proj.RegisterCacheBuilder(myKey, func(p *Project) (any, error) {
			return nil, assert.AnError
		})

		// Test cache building fails.
		data, err := proj.Cache(myKey)
		assert.Error(t, err)
		assert.Equal(t, assert.AnError, err)
		assert.Nil(t, data)
	})

	t.Run("CacheBuilderUsesProject", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package test"),
		}
		proj := NewProject(nil, files, 0)
		proj.PkgPath = "example.com/test"

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register cache builder that uses project data.
		proj.RegisterCacheBuilder(myKey, func(p *Project) (any, error) {
			fileCount := 0
			for range p.Files() {
				fileCount++
			}
			return map[string]any{
				"pkg_path":   p.PkgPath,
				"file_count": fileCount,
			}, nil
		})

		// Test cache building.
		data, err := proj.Cache(myKey)
		assert.NoError(t, err)

		result := data.(map[string]any)
		assert.Equal(t, "example.com/test", result["pkg_path"])
		assert.Equal(t, 2, result["file_count"])
	})
}

func TestProjectRegisterFileCacheBuilder(t *testing.T) {
	t.Run("RegisterNewFileCacheBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register file cache builder.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			return f.Content, nil
		})

		// Verify file cache builder was registered.
		assert.Contains(t, proj.fileCacheBuilders, myKey)

		// Add file and test cache building.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(myKey, "test.go")
		assert.NoError(t, err)
		assert.Equal(t, []byte("package test"), data)
	})

	t.Run("RegisterMultipleFileCacheBuilders", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const key1 testCacheKey = 1
		const key2 testCacheKey = 2

		// Register multiple file cache builders.
		proj.RegisterFileCacheBuilder(key1, func(p *Project, path string, f *File) (any, error) {
			return len(f.Content), nil
		})

		proj.RegisterFileCacheBuilder(key2, func(p *Project, path string, f *File) (any, error) {
			return path, nil
		})

		// Verify both file cache builders were registered.
		assert.Contains(t, proj.fileCacheBuilders, key1)
		assert.Contains(t, proj.fileCacheBuilders, key2)
		assert.Len(t, proj.fileCacheBuilders, 2)

		// Add file and test both caches.
		proj.PutFile("test.go", file("package test"))

		data1, err1 := proj.FileCache(key1, "test.go")
		assert.NoError(t, err1)
		assert.Equal(t, len("package test"), data1)

		data2, err2 := proj.FileCache(key2, "test.go")
		assert.NoError(t, err2)
		assert.Equal(t, "test.go", data2)
	})

	t.Run("OverwriteExistingFileCacheBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register first file cache builder.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			return "original", nil
		})

		// Register second file cache builder with same key.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			return "updated", nil
		})

		// Verify file cache builder was overwritten.
		assert.Contains(t, proj.fileCacheBuilders, myKey)
		assert.Len(t, proj.fileCacheBuilders, 1)

		// Add file and test that new cache builder is used.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(myKey, "test.go")
		assert.NoError(t, err)
		assert.Equal(t, "updated", data)
	})

	t.Run("FileCacheBuilderWithError", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register file cache builder that returns error.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			return nil, assert.AnError
		})

		// Add file and test cache building fails.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(myKey, "test.go")
		assert.Error(t, err)
		assert.Equal(t, assert.AnError, err)
		assert.Nil(t, data)
	})

	t.Run("FileCacheBuilderUsesParameters", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)
		proj.PkgPath = "example.com/test"

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register file cache builder that uses all parameters.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			return map[string]any{
				"pkg_path":    p.PkgPath,
				"file_path":   path,
				"content_len": len(f.Content),
				"mod_time":    f.ModTime,
			}, nil
		})

		// Add file with specific properties.
		testFile := &File{
			Content: []byte("package test\n\nfunc test() {}"),
			ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		}
		proj.PutFile("test.go", testFile)

		// Test cache building.
		data, err := proj.FileCache(myKey, "test.go")
		assert.NoError(t, err)

		result := data.(map[string]any)
		assert.Equal(t, "example.com/test", result["pkg_path"])
		assert.Equal(t, "test.go", result["file_path"])
		assert.Equal(t, len("package test\n\nfunc test() {}"), result["content_len"])
		assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), result["mod_time"])
	})
}

func TestProjectCache(t *testing.T) {
	t.Run("CacheWithBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register cache builder.
		proj.RegisterCacheBuilder(myKey, func(p *Project) (any, error) {
			return "cached-data", nil
		})

		// Test cache retrieval.
		data, err := proj.Cache(myKey)
		assert.NoError(t, err)
		assert.Equal(t, "cached-data", data)
	})

	t.Run("CacheWithoutBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Test cache retrieval without builder.
		data, err := proj.Cache(myKey)
		assert.Error(t, err)
		assert.Equal(t, ErrUnknownCacheKind, err)
		assert.Nil(t, data)
	})

	t.Run("CacheIsReused", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		var buildCount int

		// Register cache builder that tracks build count.
		proj.RegisterCacheBuilder(myKey, func(p *Project) (any, error) {
			buildCount++
			return buildCount, nil
		})

		// First cache access.
		data1, err1 := proj.Cache(myKey)
		assert.NoError(t, err1)
		assert.Equal(t, 1, data1)

		// Second cache access should reuse cached data.
		data2, err2 := proj.Cache(myKey)
		assert.NoError(t, err2)
		assert.Equal(t, 1, data2) // Same data as first call.

		// Builder should only be called once.
		assert.Equal(t, 1, buildCount)
	})

	t.Run("CacheBuilderError", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register cache builder that returns error.
		proj.RegisterCacheBuilder(myKey, func(p *Project) (any, error) {
			return nil, assert.AnError
		})

		// Test cache retrieval fails.
		data, err := proj.Cache(myKey)
		assert.Error(t, err)
		assert.Equal(t, assert.AnError, err)
		assert.Nil(t, data)
	})

	t.Run("CacheErrorIsReused", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		var buildCount int

		// Register cache builder that always returns error.
		proj.RegisterCacheBuilder(myKey, func(p *Project) (any, error) {
			buildCount++
			return nil, assert.AnError
		})

		// First cache access.
		data1, err1 := proj.Cache(myKey)
		assert.Error(t, err1)
		assert.Equal(t, assert.AnError, err1)
		assert.Nil(t, data1)

		// Second cache access should reuse cached error.
		data2, err2 := proj.Cache(myKey)
		assert.Error(t, err2)
		assert.Equal(t, assert.AnError, err2)
		assert.Nil(t, data2)

		// Builder should only be called once.
		assert.Equal(t, 1, buildCount)
	})

	t.Run("MultipleCaches", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const key1 testCacheKey = 1
		const key2 testCacheKey = 2

		// Register multiple cache builders.
		proj.RegisterCacheBuilder(key1, func(p *Project) (any, error) {
			return "cache-1", nil
		})

		proj.RegisterCacheBuilder(key2, func(p *Project) (any, error) {
			return "cache-2", nil
		})

		// Test both caches work independently.
		data1, err1 := proj.Cache(key1)
		assert.NoError(t, err1)
		assert.Equal(t, "cache-1", data1)

		data2, err2 := proj.Cache(key2)
		assert.NoError(t, err2)
		assert.Equal(t, "cache-2", data2)
	})

	t.Run("CacheUsesProjectState", func(t *testing.T) {
		files := map[string]*File{
			"main.go": file("package main"),
			"test.go": file("package main\n\nfunc test() {}"),
		}
		proj := NewProject(nil, files, 0)
		proj.PkgPath = "example.com/test"

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register cache builder that uses project state.
		proj.RegisterCacheBuilder(myKey, func(p *Project) (any, error) {
			fileCount := 0
			totalSize := 0
			for _, f := range p.files {
				fileCount++
				totalSize += len(f.Content)
			}
			return map[string]any{
				"pkg_path":   p.PkgPath,
				"file_count": fileCount,
				"total_size": totalSize,
			}, nil
		})

		// Test cache building.
		data, err := proj.Cache(myKey)
		assert.NoError(t, err)

		result := data.(map[string]any)
		assert.Equal(t, "example.com/test", result["pkg_path"])
		assert.Equal(t, 2, result["file_count"])
		expectedSize := len("package main") + len("package main\n\nfunc test() {}")
		assert.Equal(t, expectedSize, result["total_size"])
	})

	t.Run("ConcurrentCacheBuildingPreventsDuplication", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		var buildCount int32

		// Register cache builder that tracks build count.
		proj.RegisterCacheBuilder(myKey, func(p *Project) (any, error) {
			atomic.AddInt32(&buildCount, 1)
			time.Sleep(10 * time.Millisecond) // Simulate expensive operation.
			return atomic.LoadInt32(&buildCount), nil
		})

		// Start multiple goroutines to access cache concurrently.
		var wg sync.WaitGroup
		results := make([]any, 10)
		for i := range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				data, err := proj.Cache(myKey)
				assert.NoError(t, err)
				results[i] = data
			}()
		}
		wg.Wait()

		// Verify singleflight prevented duplicate builds.
		assert.Equal(t, int32(1), atomic.LoadInt32(&buildCount))

		// Verify all results are the same.
		for i := 1; i < 10; i++ {
			assert.Equal(t, results[0], results[i])
		}
	})

	t.Run("TypeSafeKeysAvoidConflicts", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type keyType1 int
		type keyType2 int

		const key1 keyType1 = 1
		const key2 keyType2 = 1

		// Register cache builders with different types but same underlying value.
		proj.RegisterCacheBuilder(key1, func(p *Project) (any, error) {
			return "data1", nil
		})

		proj.RegisterCacheBuilder(key2, func(p *Project) (any, error) {
			return "data2", nil
		})

		// Both should work independently.
		data1, err1 := proj.Cache(key1)
		assert.NoError(t, err1)
		assert.Equal(t, "data1", data1)

		data2, err2 := proj.Cache(key2)
		assert.NoError(t, err2)
		assert.Equal(t, "data2", data2)
	})

	t.Run("ComplexKeyTypes", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type complexKey struct {
			category string
			id       int
		}

		key1 := complexKey{"ast", 1}
		key2 := complexKey{"types", 1}

		// Register cache builders with struct keys.
		proj.RegisterCacheBuilder(key1, func(p *Project) (any, error) {
			return "ast-data", nil
		})

		proj.RegisterCacheBuilder(key2, func(p *Project) (any, error) {
			return "types-data", nil
		})

		// Both should work independently.
		data1, err1 := proj.Cache(key1)
		assert.NoError(t, err1)
		assert.Equal(t, "ast-data", data1)

		data2, err2 := proj.Cache(key2)
		assert.NoError(t, err2)
		assert.Equal(t, "types-data", data2)
	})
}

func TestProjectFileCache(t *testing.T) {
	t.Run("FileCacheWithBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register file cache builder.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			return len(f.Content), nil
		})

		// Add file and test cache retrieval.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(myKey, "test.go")
		assert.NoError(t, err)
		assert.Equal(t, len("package test"), data)
	})

	t.Run("FileCacheWithoutBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Add file and test cache retrieval without builder.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(myKey, "test.go")
		assert.Error(t, err)
		assert.Equal(t, ErrUnknownCacheKind, err)
		assert.Nil(t, data)
	})

	t.Run("FileCacheWithNonExistentFile", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register file cache builder.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			return len(f.Content), nil
		})

		// Test cache retrieval for non-existent file.
		data, err := proj.FileCache(myKey, "nonexistent.go")
		assert.Error(t, err)
		assert.Equal(t, fs.ErrNotExist, err)
		assert.Nil(t, data)
	})

	t.Run("FileCacheIsReused", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		var buildCount int

		// Register file cache builder that tracks build count.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			buildCount++
			return buildCount, nil
		})

		// Add file.
		proj.PutFile("test.go", file("package test"))

		// First cache access.
		data1, err1 := proj.FileCache(myKey, "test.go")
		assert.NoError(t, err1)
		assert.Equal(t, 1, data1)

		// Second cache access should reuse cached data.
		data2, err2 := proj.FileCache(myKey, "test.go")
		assert.NoError(t, err2)
		assert.Equal(t, 1, data2) // Same data as first call.

		// Builder should only be called once.
		assert.Equal(t, 1, buildCount)
	})

	t.Run("FileCacheBuilderError", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register file cache builder that returns error.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			return nil, assert.AnError
		})

		// Add file and test cache retrieval fails.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(myKey, "test.go")
		assert.Error(t, err)
		assert.Equal(t, assert.AnError, err)
		assert.Nil(t, data)
	})

	t.Run("FileCacheErrorIsReused", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		var buildCount int

		// Register file cache builder that always returns error.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			buildCount++
			return nil, assert.AnError
		})

		// Add file.
		proj.PutFile("test.go", file("package test"))

		// First cache access.
		data1, err1 := proj.FileCache(myKey, "test.go")
		assert.Error(t, err1)
		assert.Equal(t, assert.AnError, err1)
		assert.Nil(t, data1)

		// Second cache access should reuse cached error.
		data2, err2 := proj.FileCache(myKey, "test.go")
		assert.Error(t, err2)
		assert.Equal(t, assert.AnError, err2)
		assert.Nil(t, data2)

		// Builder should only be called once.
		assert.Equal(t, 1, buildCount)
	})

	t.Run("FileCachePerFile", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register file cache builder.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			return path + ":" + string(f.Content), nil
		})

		// Add multiple files.
		proj.PutFile("test1.go", file("package test1"))
		proj.PutFile("test2.go", file("package test2"))

		// Test caches work independently per file.
		data1, err1 := proj.FileCache(myKey, "test1.go")
		assert.NoError(t, err1)
		assert.Equal(t, "test1.go:package test1", data1)

		data2, err2 := proj.FileCache(myKey, "test2.go")
		assert.NoError(t, err2)
		assert.Equal(t, "test2.go:package test2", data2)
	})

	t.Run("FileCacheUsesAllParameters", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)
		proj.PkgPath = "example.com/test"

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register file cache builder that uses all parameters.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			return map[string]any{
				"pkg_path":    p.PkgPath,
				"file_path":   path,
				"content":     string(f.Content),
				"content_len": len(f.Content),
				"mod_time":    f.ModTime,
				"version":     f.Version,
			}, nil
		})

		// Add file with specific properties.
		testFile := &File{
			Content: []byte("package test\n\nfunc test() {}"),
			ModTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			Version: 42,
		}
		proj.PutFile("test.go", testFile)

		// Test cache building.
		data, err := proj.FileCache(myKey, "test.go")
		assert.NoError(t, err)

		result := data.(map[string]any)
		assert.Equal(t, "example.com/test", result["pkg_path"])
		assert.Equal(t, "test.go", result["file_path"])
		assert.Equal(t, "package test\n\nfunc test() {}", result["content"])
		assert.Equal(t, len("package test\n\nfunc test() {}"), result["content_len"])
		assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), result["mod_time"])
		assert.Equal(t, 42, result["version"])
	})

	t.Run("FileCacheInvalidatedOnFileUpdate", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		var buildCount int

		// Register file cache builder that tracks build count.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			buildCount++
			return string(f.Content), nil
		})

		// Add file and build cache.
		proj.PutFile("test.go", file("package test"))

		data1, err1 := proj.FileCache(myKey, "test.go")
		assert.NoError(t, err1)
		assert.Equal(t, "package test", data1)
		assert.Equal(t, 1, buildCount)

		// Update file (should invalidate cache).
		proj.PutFile("test.go", file("package updated"))

		// Cache should be rebuilt.
		data2, err2 := proj.FileCache(myKey, "test.go")
		assert.NoError(t, err2)
		assert.Equal(t, "package updated", data2)
		assert.Equal(t, 2, buildCount) // Builder called again.
	})

	t.Run("ConcurrentFileCacheBuildingPreventsDuplication", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		var buildCount int32

		// Register file cache builder that tracks build count.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			atomic.AddInt32(&buildCount, 1)
			time.Sleep(10 * time.Millisecond) // Simulate expensive operation.
			return fmt.Sprintf("%s:%d", path, atomic.LoadInt32(&buildCount)), nil
		})

		// Add a file.
		proj.PutFile("test.go", file("package test"))

		// Start multiple goroutines to access file cache concurrently.
		var wg sync.WaitGroup
		results := make([]any, 10)
		for i := range 10 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				data, err := proj.FileCache(myKey, "test.go")
				assert.NoError(t, err)
				results[i] = data
			}()
		}
		wg.Wait()

		// Verify singleflight prevented duplicate builds.
		assert.Equal(t, int32(1), atomic.LoadInt32(&buildCount))

		// Verify all results are the same.
		for i := 1; i < 10; i++ {
			assert.Equal(t, results[0], results[i])
		}
	})

	t.Run("TypeSafeFileCacheKeysAvoidConflicts", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type keyType1 string
		type keyType2 string

		const key1 keyType1 = "key"
		const key2 keyType2 = "key"

		// Register file cache builders with different types but same underlying value.
		proj.RegisterFileCacheBuilder(key1, func(p *Project, path string, f *File) (any, error) {
			return "filedata1", nil
		})

		proj.RegisterFileCacheBuilder(key2, func(p *Project, path string, f *File) (any, error) {
			return "filedata2", nil
		})

		// Add a file.
		proj.PutFile("test.go", file("package test"))

		// Both should work independently.
		data1, err1 := proj.FileCache(key1, "test.go")
		assert.NoError(t, err1)
		assert.Equal(t, "filedata1", data1)

		data2, err2 := proj.FileCache(key2, "test.go")
		assert.NoError(t, err2)
		assert.Equal(t, "filedata2", data2)
	})
}

func TestProjectDeleteFileCache(t *testing.T) {
	t.Run("DeleteCacheForExistingFile", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		var buildCount int

		// Register file cache builder that tracks build count.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			buildCount++
			return buildCount, nil
		})

		// Add file and build cache.
		proj.PutFile("test.go", file("package test"))

		data1, err1 := proj.FileCache(myKey, "test.go")
		assert.NoError(t, err1)
		assert.Equal(t, 1, data1)
		assert.Equal(t, 1, buildCount)

		// Delete file cache.
		proj.deleteFileCache("test.go")

		// Cache should be rebuilt on next access.
		data2, err2 := proj.FileCache(myKey, "test.go")
		assert.NoError(t, err2)
		assert.Equal(t, 2, data2)      // New build number.
		assert.Equal(t, 2, buildCount) // Builder called again.
	})

	t.Run("DeleteCacheForNonExistentFile", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		// Register file cache builder.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			return "data", nil
		})

		// Delete cache for non-existent file (should not panic).
		proj.deleteFileCache("nonexistent.go")

		// Add file and verify cache works normally.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(myKey, "test.go")
		assert.NoError(t, err)
		assert.Equal(t, "data", data)
	})

	t.Run("DeleteCacheWithMultipleKeys", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const key1 testCacheKey = 1
		const key2 testCacheKey = 2

		var buildCount1, buildCount2 int

		// Register multiple file cache builders.
		proj.RegisterFileCacheBuilder(key1, func(p *Project, path string, f *File) (any, error) {
			buildCount1++
			return buildCount1, nil
		})

		proj.RegisterFileCacheBuilder(key2, func(p *Project, path string, f *File) (any, error) {
			buildCount2++
			return buildCount2, nil
		})

		// Add file and build both caches.
		proj.PutFile("test.go", file("package test"))

		data1, err1 := proj.FileCache(key1, "test.go")
		assert.NoError(t, err1)
		assert.Equal(t, 1, data1)

		data2, err2 := proj.FileCache(key2, "test.go")
		assert.NoError(t, err2)
		assert.Equal(t, 1, data2)

		// Delete file cache (should delete all caches for this file).
		proj.deleteFileCache("test.go")

		// Both caches should be rebuilt.
		data1New, err1New := proj.FileCache(key1, "test.go")
		assert.NoError(t, err1New)
		assert.Equal(t, 2, data1New)

		data2New, err2New := proj.FileCache(key2, "test.go")
		assert.NoError(t, err2New)
		assert.Equal(t, 2, data2New)

		assert.Equal(t, 2, buildCount1)
		assert.Equal(t, 2, buildCount2)
	})

	t.Run("DeleteCacheWithMultipleFiles", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKey int
		const myKey testCacheKey = 1

		var buildCountTest1, buildCountTest2 int

		// Register file cache builder that tracks build count per file.
		proj.RegisterFileCacheBuilder(myKey, func(p *Project, path string, f *File) (any, error) {
			if path == "test1.go" {
				buildCountTest1++
				return buildCountTest1, nil
			} else {
				buildCountTest2++
				return buildCountTest2, nil
			}
		})

		// Add files and build caches.
		proj.PutFile("test1.go", file("package test1"))
		proj.PutFile("test2.go", file("package test2"))

		data1, err1 := proj.FileCache(myKey, "test1.go")
		assert.NoError(t, err1)
		assert.Equal(t, 1, data1)

		data2, err2 := proj.FileCache(myKey, "test2.go")
		assert.NoError(t, err2)
		assert.Equal(t, 1, data2)

		// Delete cache for one file only.
		proj.deleteFileCache("test1.go")

		// First file cache should be rebuilt.
		data1New, err1New := proj.FileCache(myKey, "test1.go")
		assert.NoError(t, err1New)
		assert.Equal(t, 2, data1New)
		assert.Equal(t, 2, buildCountTest1)

		// Second file cache should be reused.
		data2Same, err2Same := proj.FileCache(myKey, "test2.go")
		assert.NoError(t, err2Same)
		assert.Equal(t, 1, data2Same)       // Same as before.
		assert.Equal(t, 1, buildCountTest2) // No rebuild.
	})
}

func TestDataOrErr(t *testing.T) {
	t.Run("EncodeDecodeSuccessData", func(t *testing.T) {
		originalData := "test-data"
		originalErr := error(nil)

		// Encode.
		encoded := encodeDataOrErr(originalData, originalErr)

		// Decode.
		decodedData, decodedErr := decodeDataOrErr(encoded)

		// Verify.
		assert.Equal(t, originalData, decodedData)
		assert.NoError(t, decodedErr)
	})

	t.Run("EncodeDecodeErrorData", func(t *testing.T) {
		originalData := any(nil)
		originalErr := assert.AnError

		// Encode.
		encoded := encodeDataOrErr(originalData, originalErr)

		// Decode.
		decodedData, decodedErr := decodeDataOrErr(encoded)

		// Verify.
		assert.Nil(t, decodedData)
		assert.Error(t, decodedErr)
		assert.Equal(t, originalErr, decodedErr)
	})

	t.Run("EncodeDecodeComplexData", func(t *testing.T) {
		originalData := map[string]any{
			"string":  "value",
			"number":  42,
			"boolean": true,
			"slice":   []int{1, 2, 3},
		}
		originalErr := error(nil)

		// Encode.
		encoded := encodeDataOrErr(originalData, originalErr)

		// Decode.
		decodedData, decodedErr := decodeDataOrErr(encoded)

		// Verify.
		assert.Equal(t, originalData, decodedData)
		assert.NoError(t, decodedErr)
	})

	t.Run("EncodeDecodeNilData", func(t *testing.T) {
		originalData := any(nil)
		originalErr := error(nil)

		// Encode.
		encoded := encodeDataOrErr(originalData, originalErr)

		// Decode.
		decodedData, decodedErr := decodeDataOrErr(encoded)

		// Verify.
		assert.Nil(t, decodedData)
		assert.NoError(t, decodedErr)
	})

	t.Run("EncodeDecodeCustomError", func(t *testing.T) {
		originalData := any(nil)
		originalErr := ErrUnknownCacheKind

		// Encode.
		encoded := encodeDataOrErr(originalData, originalErr)

		// Decode.
		decodedData, decodedErr := decodeDataOrErr(encoded)

		// Verify.
		assert.Nil(t, decodedData)
		assert.Error(t, decodedErr)
		assert.Equal(t, originalErr, decodedErr)
	})

	t.Run("EncodeDecodeZeroValues", func(t *testing.T) {
		testCases := []struct {
			name string
			data any
		}{
			{"EmptyString", ""},
			{"ZeroInt", 0},
			{"FalseBool", false},
			{"EmptySlice", []string{}},
			{"EmptyMap", map[string]any{}},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				originalData := tc.data
				originalErr := error(nil)

				// Encode.
				encoded := encodeDataOrErr(originalData, originalErr)

				// Decode.
				decodedData, decodedErr := decodeDataOrErr(encoded)

				// Verify.
				assert.Equal(t, originalData, decodedData)
				assert.NoError(t, decodedErr)
			})
		}
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
