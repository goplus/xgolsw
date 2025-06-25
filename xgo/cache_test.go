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
	"io/fs"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestProjectRegisterCacheBuilder(t *testing.T) {
	t.Run("RegisterNewCacheBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Register cache builder.
		proj.RegisterCacheBuilder(testCacheKind{}, func(p *Project) (any, error) {
			return "test-data", nil
		})

		// Verify cache builder was registered.
		assert.Contains(t, proj.cacheBuilders, testCacheKind{})

		// Test cache building.
		data, err := proj.Cache(testCacheKind{})
		assert.NoError(t, err)
		assert.Equal(t, "test-data", data)
	})

	t.Run("RegisterMultipleCacheBuilders", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind1 struct{}
		type testCacheKind2 struct{}

		// Register multiple cache builders.
		proj.RegisterCacheBuilder(testCacheKind1{}, func(p *Project) (any, error) {
			return "data-1", nil
		})
		proj.RegisterCacheBuilder(testCacheKind2{}, func(p *Project) (any, error) {
			return "data-2", nil
		})

		// Verify both cache builders were registered.
		assert.Contains(t, proj.cacheBuilders, testCacheKind1{})
		assert.Contains(t, proj.cacheBuilders, testCacheKind2{})
		assert.Len(t, proj.cacheBuilders, 2)

		// Test both caches.
		data1, err1 := proj.Cache(testCacheKind1{})
		assert.NoError(t, err1)
		assert.Equal(t, "data-1", data1)

		data2, err2 := proj.Cache(testCacheKind2{})
		assert.NoError(t, err2)
		assert.Equal(t, "data-2", data2)
	})

	t.Run("OverwriteExistingCacheBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Register first cache builder.
		proj.RegisterCacheBuilder(testCacheKind{}, func(p *Project) (any, error) {
			return "original-data", nil
		})

		// Register second cache builder with same kind.
		proj.RegisterCacheBuilder(testCacheKind{}, func(p *Project) (any, error) {
			return "updated-data", nil
		})

		// Verify cache builder was overwritten.
		assert.Contains(t, proj.cacheBuilders, testCacheKind{})
		assert.Len(t, proj.cacheBuilders, 1)

		// Test that new cache builder is used.
		data, err := proj.Cache(testCacheKind{})
		assert.NoError(t, err)
		assert.Equal(t, "updated-data", data)
	})

	t.Run("CacheBuilderWithError", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Register cache builder that returns error.
		proj.RegisterCacheBuilder(testCacheKind{}, func(p *Project) (any, error) {
			return nil, assert.AnError
		})

		// Test cache building fails.
		data, err := proj.Cache(testCacheKind{})
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

		type testCacheKind struct{}

		// Register cache builder that uses project data.
		proj.RegisterCacheBuilder(testCacheKind{}, func(p *Project) (any, error) {
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
		data, err := proj.Cache(testCacheKind{})
		assert.NoError(t, err)

		result := data.(map[string]any)
		assert.Equal(t, "example.com/test", result["pkg_path"])
		assert.Equal(t, 2, result["file_count"])
	})
}

func TestProjectRegisterFileCacheBuilder(t *testing.T) {
	t.Run("RegisterNewFileCacheBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Register file cache builder.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
			return f.Content, nil
		})

		// Verify file cache builder was registered.
		assert.Contains(t, proj.fileCacheBuilders, testCacheKind{})

		// Add file and test cache building.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(testCacheKind{}, "test.go")
		assert.NoError(t, err)
		assert.Equal(t, []byte("package test"), data)
	})

	t.Run("RegisterMultipleFileCacheBuilders", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind1 struct{}
		type testCacheKind2 struct{}

		// Register multiple file cache builders.
		proj.RegisterFileCacheBuilder(testCacheKind1{}, func(p *Project, path string, f *File) (any, error) {
			return len(f.Content), nil
		})
		proj.RegisterFileCacheBuilder(testCacheKind2{}, func(p *Project, path string, f *File) (any, error) {
			return path, nil
		})

		// Verify both file cache builders were registered.
		assert.Contains(t, proj.fileCacheBuilders, testCacheKind1{})
		assert.Contains(t, proj.fileCacheBuilders, testCacheKind2{})
		assert.Len(t, proj.fileCacheBuilders, 2)

		// Add file and test both caches.
		proj.PutFile("test.go", file("package test"))

		data1, err1 := proj.FileCache(testCacheKind1{}, "test.go")
		assert.NoError(t, err1)
		assert.Equal(t, len("package test"), data1)

		data2, err2 := proj.FileCache(testCacheKind2{}, "test.go")
		assert.NoError(t, err2)
		assert.Equal(t, "test.go", data2)
	})

	t.Run("OverwriteExistingFileCacheBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Register first file cache builder.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
			return "original", nil
		})

		// Register second file cache builder with same kind.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
			return "updated", nil
		})

		// Verify file cache builder was overwritten.
		assert.Contains(t, proj.fileCacheBuilders, testCacheKind{})
		assert.Len(t, proj.fileCacheBuilders, 1)

		// Add file and test that new cache builder is used.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(testCacheKind{}, "test.go")
		assert.NoError(t, err)
		assert.Equal(t, "updated", data)
	})

	t.Run("FileCacheBuilderWithError", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Register file cache builder that returns error.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
			return nil, assert.AnError
		})

		// Add file and test cache building fails.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(testCacheKind{}, "test.go")
		assert.Error(t, err)
		assert.Equal(t, assert.AnError, err)
		assert.Nil(t, data)
	})

	t.Run("FileCacheBuilderUsesParameters", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)
		proj.PkgPath = "example.com/test"

		type testCacheKind struct{}

		// Register file cache builder that uses all parameters.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
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
		data, err := proj.FileCache(testCacheKind{}, "test.go")
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

		type testCacheKind struct{}

		// Register cache builder.
		proj.RegisterCacheBuilder(testCacheKind{}, func(p *Project) (any, error) {
			return "cached-data", nil
		})

		// Test cache retrieval.
		data, err := proj.Cache(testCacheKind{})
		assert.NoError(t, err)
		assert.Equal(t, "cached-data", data)
	})

	t.Run("CacheWithoutBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Test cache retrieval without builder.
		data, err := proj.Cache(testCacheKind{})
		assert.Error(t, err)
		assert.Equal(t, ErrUnknownCacheKind, err)
		assert.Nil(t, data)
	})

	t.Run("CacheIsReused", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		var buildCount int

		// Register cache builder that tracks build count.
		proj.RegisterCacheBuilder(testCacheKind{}, func(p *Project) (any, error) {
			buildCount++
			return buildCount, nil
		})

		// First cache access.
		data1, err1 := proj.Cache(testCacheKind{})
		assert.NoError(t, err1)
		assert.Equal(t, 1, data1)

		// Second cache access should reuse cached data.
		data2, err2 := proj.Cache(testCacheKind{})
		assert.NoError(t, err2)
		assert.Equal(t, 1, data2) // Same data as first call.

		// Builder should only be called once.
		assert.Equal(t, 1, buildCount)
	})

	t.Run("CacheBuilderError", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Register cache builder that returns error.
		proj.RegisterCacheBuilder(testCacheKind{}, func(p *Project) (any, error) {
			return nil, assert.AnError
		})

		// Test cache retrieval fails.
		data, err := proj.Cache(testCacheKind{})
		assert.Error(t, err)
		assert.Equal(t, assert.AnError, err)
		assert.Nil(t, data)
	})

	t.Run("CacheErrorIsReused", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		var buildCount int

		// Register cache builder that always returns error.
		proj.RegisterCacheBuilder(testCacheKind{}, func(p *Project) (any, error) {
			buildCount++
			return nil, assert.AnError
		})

		// First cache access.
		data1, err1 := proj.Cache(testCacheKind{})
		assert.Error(t, err1)
		assert.Equal(t, assert.AnError, err1)
		assert.Nil(t, data1)

		// Second cache access should reuse cached error.
		data2, err2 := proj.Cache(testCacheKind{})
		assert.Error(t, err2)
		assert.Equal(t, assert.AnError, err2)
		assert.Nil(t, data2)

		// Builder should only be called once.
		assert.Equal(t, 1, buildCount)
	})

	t.Run("MultipleCaches", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind1 struct{}
		type testCacheKind2 struct{}

		// Register multiple cache builders.
		proj.RegisterCacheBuilder(testCacheKind1{}, func(p *Project) (any, error) {
			return "cache-1", nil
		})
		proj.RegisterCacheBuilder(testCacheKind2{}, func(p *Project) (any, error) {
			return "cache-2", nil
		})

		// Test both caches work independently.
		data1, err1 := proj.Cache(testCacheKind1{})
		assert.NoError(t, err1)
		assert.Equal(t, "cache-1", data1)

		data2, err2 := proj.Cache(testCacheKind2{})
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

		type testCacheKind struct{}

		// Register cache builder that uses project state.
		proj.RegisterCacheBuilder(testCacheKind{}, func(p *Project) (any, error) {
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
		data, err := proj.Cache(testCacheKind{})
		assert.NoError(t, err)

		result := data.(map[string]any)
		assert.Equal(t, "example.com/test", result["pkg_path"])
		assert.Equal(t, 2, result["file_count"])
		expectedSize := len("package main") + len("package main\n\nfunc test() {}")
		assert.Equal(t, expectedSize, result["total_size"])
	})

	t.Run("ConcurrentCacheBuildingPreventsDuplication", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		var buildCount int32

		// Register cache builder that tracks build count.
		proj.RegisterCacheBuilder(testCacheKind{}, func(p *Project) (any, error) {
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
				data, err := proj.Cache(testCacheKind{})
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

	t.Run("TypeSafeCacheKindsAvoidConflicts", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind1 int
		type testCacheKind2 int

		const kind1 testCacheKind1 = 1
		const kind2 testCacheKind2 = 1

		// Register cache builders with different types but same underlying value.
		proj.RegisterCacheBuilder(kind1, func(p *Project) (any, error) {
			return "data1", nil
		})
		proj.RegisterCacheBuilder(kind2, func(p *Project) (any, error) {
			return "data2", nil
		})

		// Both should work independently despite same underlying value.
		data1, err1 := proj.Cache(kind1)
		assert.NoError(t, err1)
		assert.Equal(t, "data1", data1)

		data2, err2 := proj.Cache(kind2)
		assert.NoError(t, err2)
		assert.Equal(t, "data2", data2)
	})

	t.Run("ComplexCacheKinds", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type complexCacheKind struct {
			category string
			id       int
		}

		kind1 := complexCacheKind{"ast", 1}
		kind2 := complexCacheKind{"types", 1}

		// Register cache builders with struct cache kinds.
		proj.RegisterCacheBuilder(kind1, func(p *Project) (any, error) {
			return "ast-data", nil
		})
		proj.RegisterCacheBuilder(kind2, func(p *Project) (any, error) {
			return "types-data", nil
		})

		// Both should work independently.
		data1, err1 := proj.Cache(kind1)
		assert.NoError(t, err1)
		assert.Equal(t, "ast-data", data1)

		data2, err2 := proj.Cache(kind2)
		assert.NoError(t, err2)
		assert.Equal(t, "types-data", data2)
	})
}

func TestProjectFileCache(t *testing.T) {
	t.Run("FileCacheWithBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Register file cache builder.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
			return len(f.Content), nil
		})

		// Add file and test cache retrieval.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(testCacheKind{}, "test.go")
		assert.NoError(t, err)
		assert.Equal(t, len("package test"), data)
	})

	t.Run("FileCacheWithoutBuilder", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Add file and test cache retrieval without builder.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(testCacheKind{}, "test.go")
		assert.Error(t, err)
		assert.Equal(t, ErrUnknownCacheKind, err)
		assert.Nil(t, data)
	})

	t.Run("FileCacheWithNonExistentFile", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Register file cache builder.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
			return len(f.Content), nil
		})

		// Test cache retrieval for non-existent file.
		data, err := proj.FileCache(testCacheKind{}, "nonexistent.go")
		assert.Error(t, err)
		assert.Equal(t, fs.ErrNotExist, err)
		assert.Nil(t, data)
	})

	t.Run("FileCacheIsReused", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		var buildCount int

		// Register file cache builder that tracks build count.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
			buildCount++
			return buildCount, nil
		})

		// Add file.
		proj.PutFile("test.go", file("package test"))

		// First cache access.
		data1, err1 := proj.FileCache(testCacheKind{}, "test.go")
		assert.NoError(t, err1)
		assert.Equal(t, 1, data1)

		// Second cache access should reuse cached data.
		data2, err2 := proj.FileCache(testCacheKind{}, "test.go")
		assert.NoError(t, err2)
		assert.Equal(t, 1, data2) // Same data as first call.

		// Builder should only be called once.
		assert.Equal(t, 1, buildCount)
	})

	t.Run("FileCacheBuilderError", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Register file cache builder that returns error.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
			return nil, assert.AnError
		})

		// Add file and test cache retrieval fails.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(testCacheKind{}, "test.go")
		assert.Error(t, err)
		assert.Equal(t, assert.AnError, err)
		assert.Nil(t, data)
	})

	t.Run("FileCacheErrorIsReused", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		var buildCount int

		// Register file cache builder that always returns error.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
			buildCount++
			return nil, assert.AnError
		})

		// Add file.
		proj.PutFile("test.go", file("package test"))

		// First cache access.
		data1, err1 := proj.FileCache(testCacheKind{}, "test.go")
		assert.Error(t, err1)
		assert.Equal(t, assert.AnError, err1)
		assert.Nil(t, data1)

		// Second cache access should reuse cached error.
		data2, err2 := proj.FileCache(testCacheKind{}, "test.go")
		assert.Error(t, err2)
		assert.Equal(t, assert.AnError, err2)
		assert.Nil(t, data2)

		// Builder should only be called once.
		assert.Equal(t, 1, buildCount)
	})

	t.Run("FileCachePerFile", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Register file cache builder.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
			return path + ":" + string(f.Content), nil
		})

		// Add multiple files.
		proj.PutFile("test1.go", file("package test1"))
		proj.PutFile("test2.go", file("package test2"))

		// Test caches work independently per file.
		data1, err1 := proj.FileCache(testCacheKind{}, "test1.go")
		assert.NoError(t, err1)
		assert.Equal(t, "test1.go:package test1", data1)

		data2, err2 := proj.FileCache(testCacheKind{}, "test2.go")
		assert.NoError(t, err2)
		assert.Equal(t, "test2.go:package test2", data2)
	})

	t.Run("FileCacheUsesAllParameters", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)
		proj.PkgPath = "example.com/test"

		type testCacheKind struct{}

		// Register file cache builder that uses all parameters.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
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
		data, err := proj.FileCache(testCacheKind{}, "test.go")
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

		type testCacheKind struct{}

		var buildCount int

		// Register file cache builder that tracks build count.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
			buildCount++
			return string(f.Content), nil
		})

		// Add file and build cache.
		proj.PutFile("test.go", file("package test"))

		data1, err1 := proj.FileCache(testCacheKind{}, "test.go")
		assert.NoError(t, err1)
		assert.Equal(t, "package test", data1)
		assert.Equal(t, 1, buildCount)

		// Update file (should invalidate cache).
		proj.PutFile("test.go", file("package updated"))

		// Cache should be rebuilt.
		data2, err2 := proj.FileCache(testCacheKind{}, "test.go")
		assert.NoError(t, err2)
		assert.Equal(t, "package updated", data2)
		assert.Equal(t, 2, buildCount) // Builder called again.
	})

	t.Run("ConcurrentFileCacheBuildingPreventsDuplication", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		var buildCount int32

		// Register file cache builder that tracks build count.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
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
				data, err := proj.FileCache(testCacheKind{}, "test.go")
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

	t.Run("TypeSafeFileCacheKindsAvoidConflicts", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind1 int
		type testCacheKind2 int

		const kind1 testCacheKind1 = 1
		const kind2 testCacheKind2 = 1

		// Register file cache builders with different types but same underlying value.
		proj.RegisterFileCacheBuilder(kind1, func(p *Project, path string, f *File) (any, error) {
			return "filedata1", nil
		})

		proj.RegisterFileCacheBuilder(kind2, func(p *Project, path string, f *File) (any, error) {
			return "filedata2", nil
		})

		// Add a file.
		proj.PutFile("test.go", file("package test"))

		// Both should work independently despite same underlying value.
		data1, err1 := proj.FileCache(kind1, "test.go")
		assert.NoError(t, err1)
		assert.Equal(t, "filedata1", data1)

		data2, err2 := proj.FileCache(kind2, "test.go")
		assert.NoError(t, err2)
		assert.Equal(t, "filedata2", data2)
	})
}

func TestProjectDeleteFileCache(t *testing.T) {
	t.Run("DeleteCacheForExistingFile", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		var buildCount int

		// Register file cache builder that tracks build count.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
			buildCount++
			return buildCount, nil
		})

		// Add file and build cache.
		proj.PutFile("test.go", file("package test"))

		data1, err1 := proj.FileCache(testCacheKind{}, "test.go")
		assert.NoError(t, err1)
		assert.Equal(t, 1, data1)
		assert.Equal(t, 1, buildCount)

		// Delete file cache.
		proj.deleteFileCache("test.go")

		// Cache should be rebuilt on next access.
		data2, err2 := proj.FileCache(testCacheKind{}, "test.go")
		assert.NoError(t, err2)
		assert.Equal(t, 2, data2)      // New build number.
		assert.Equal(t, 2, buildCount) // Builder called again.
	})

	t.Run("DeleteCacheForNonExistentFile", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		// Register file cache builder.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
			return "data", nil
		})

		// Delete cache for non-existent file (should not panic).
		proj.deleteFileCache("nonexistent.go")

		// Add file and verify cache works normally.
		proj.PutFile("test.go", file("package test"))

		data, err := proj.FileCache(testCacheKind{}, "test.go")
		assert.NoError(t, err)
		assert.Equal(t, "data", data)
	})

	t.Run("DeleteCacheWithMultipleCacheKinds", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind1 struct{}
		type testCacheKind2 struct{}

		var buildCount1, buildCount2 int

		// Register multiple file cache builders.
		proj.RegisterFileCacheBuilder(testCacheKind1{}, func(p *Project, path string, f *File) (any, error) {
			buildCount1++
			return buildCount1, nil
		})
		proj.RegisterFileCacheBuilder(testCacheKind2{}, func(p *Project, path string, f *File) (any, error) {
			buildCount2++
			return buildCount2, nil
		})

		// Add file and build both caches.
		proj.PutFile("test.go", file("package test"))

		data1, err1 := proj.FileCache(testCacheKind1{}, "test.go")
		assert.NoError(t, err1)
		assert.Equal(t, 1, data1)

		data2, err2 := proj.FileCache(testCacheKind2{}, "test.go")
		assert.NoError(t, err2)
		assert.Equal(t, 1, data2)

		// Delete file cache (should delete all caches for this file).
		proj.deleteFileCache("test.go")

		// Both caches should be rebuilt.
		data1New, err1New := proj.FileCache(testCacheKind1{}, "test.go")
		assert.NoError(t, err1New)
		assert.Equal(t, 2, data1New)

		data2New, err2New := proj.FileCache(testCacheKind2{}, "test.go")
		assert.NoError(t, err2New)
		assert.Equal(t, 2, data2New)

		assert.Equal(t, 2, buildCount1)
		assert.Equal(t, 2, buildCount2)
	})

	t.Run("DeleteCacheWithMultipleFiles", func(t *testing.T) {
		proj := NewProject(nil, nil, 0)

		type testCacheKind struct{}

		var buildCountTest1, buildCountTest2 int

		// Register file cache builder that tracks build count per file.
		proj.RegisterFileCacheBuilder(testCacheKind{}, func(p *Project, path string, f *File) (any, error) {
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

		data1, err1 := proj.FileCache(testCacheKind{}, "test1.go")
		assert.NoError(t, err1)
		assert.Equal(t, 1, data1)

		data2, err2 := proj.FileCache(testCacheKind{}, "test2.go")
		assert.NoError(t, err2)
		assert.Equal(t, 1, data2)

		// Delete cache for one file only.
		proj.deleteFileCache("test1.go")

		// First file cache should be rebuilt.
		data1New, err1New := proj.FileCache(testCacheKind{}, "test1.go")
		assert.NoError(t, err1New)
		assert.Equal(t, 2, data1New)
		assert.Equal(t, 2, buildCountTest1)

		// Second file cache should be reused.
		data2Same, err2Same := proj.FileCache(testCacheKind{}, "test2.go")
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
