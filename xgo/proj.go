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
	"errors"
	"fmt"
	"go/token"
	"go/types"
	"io/fs"
	"iter"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/goplus/mod/xgomod"
	"golang.org/x/sync/singleflight"
)

const (
	// FeatASTCache enables AST cache building.
	FeatASTCache = 1 << iota

	// FeatTypeInfoCache enables TypeInfo cache building.
	FeatTypeInfoCache

	// FeatPkgDocCache enables PkgDoc cache building.
	FeatPkgDocCache

	// FeatAll enables all features.
	FeatAll = FeatASTCache | FeatTypeInfoCache | FeatPkgDocCache
)

// ErrUnknownCacheKind represents an error of unknown cache kind.
var ErrUnknownCacheKind = errors.New("unknown cache kind")

// -----------------------------------------------------------------------------

// CacheBuilder represents a project level cache builder.
type CacheBuilder = func(proj *Project) (any, error)

// FileCacheBuilder represents a file level cache builder.
type FileCacheBuilder = func(proj *Project, path string, file *File) (any, error)

// CacheKind represents a kind of cache.
type CacheKind = any

// fileCacheKey represents a key for file-level cache entries.
// It combines a cache kind with a file path to uniquely identify cached data.
type fileCacheKey struct {
	kind CacheKind
	path string
}

// -----------------------------------------------------------------------------

// File represents a file in an XGo project.
type File struct {
	Content []byte
	// Deprecated: ModTime is no longer supported due to lsp text sync specification. Use Version instead.
	ModTime time.Time
	Version int
}

// Project represents an XGo project.
type Project struct {
	PkgPath  string
	Mod      *xgomod.Module
	Importer types.Importer
	Fset     *token.FileSet

	mu            sync.RWMutex
	files         map[string]*File
	filesSnapshot atomic.Pointer[map[string]*File] // Immutable snapshot for lock-free file reads.

	cacheBuilders map[CacheKind]CacheBuilder
	caches        map[CacheKind]dataOrErr
	cacheSFG      singleflight.Group

	fileCacheBuilders map[CacheKind]FileCacheBuilder
	fileCaches        map[fileCacheKey]dataOrErr
	fileCacheSFG      singleflight.Group
}

// NewProject creates a new project with optional static files and features.
func NewProject(fset *token.FileSet, files map[string]*File, feats uint) *Project {
	if fset == nil {
		fset = token.NewFileSet()
	}
	proj := &Project{
		Fset:              fset,
		files:             make(map[string]*File),
		cacheBuilders:     make(map[CacheKind]CacheBuilder),
		caches:            make(map[CacheKind]dataOrErr),
		fileCacheBuilders: make(map[CacheKind]FileCacheBuilder),
		fileCaches:        make(map[fileCacheKey]dataOrErr),
	}
	if files != nil {
		maps.Copy(proj.files, files)
	}
	proj.updateFilesSnapshot()
	for _, f := range supportedFeats {
		if f.feat&feats != 0 {
			if f.fileFeat {
				proj.RegisterFileCacheBuilder(f.kind, f.builder.(FileCacheBuilder))
			} else {
				proj.RegisterCacheBuilder(f.kind, f.builder.(CacheBuilder))
			}
		}
	}
	return proj
}

// -----------------------------------------------------------------------------

// Snapshot creates a snapshot of the project.
func (p *Project) Snapshot() *Project {
	p.mu.RLock()
	defer p.mu.RUnlock()

	proj := &Project{
		PkgPath:           p.PkgPath,
		Mod:               p.Mod,
		Importer:          p.Importer,
		Fset:              p.Fset,
		files:             maps.Clone(p.files),
		cacheBuilders:     maps.Clone(p.cacheBuilders),
		caches:            maps.Clone(p.caches),
		fileCacheBuilders: maps.Clone(p.fileCacheBuilders),
		fileCaches:        maps.Clone(p.fileCaches),
	}
	proj.updateFilesSnapshot()
	return proj
}

// -----------------------------------------------------------------------------

// Files returns an iterator over all file path-content pairs in the project.
func (p *Project) Files() iter.Seq2[string, *File] {
	snapshot := p.filesSnapshot.Load()
	return maps.All(*snapshot)
}

// File gets a file from the project.
func (p *Project) File(path string) (file *File, ok bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	file, ok = p.files[path]
	return
}

// PutFile puts a file into the project.
func (p *Project) PutFile(path string, file *File) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.files[path] = file
	p.updateFilesSnapshot()
	p.deleteFileCache(path)
}

// DeleteFile deletes a file from the project.
func (p *Project) DeleteFile(path string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.files[path]; ok {
		delete(p.files, path)
		p.updateFilesSnapshot()
		p.deleteFileCache(path)
		return nil
	}
	return fs.ErrNotExist
}

// RenameFile renames a file in the project.
func (p *Project) RenameFile(oldPath, newPath string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	file, ok := p.files[oldPath]
	if !ok {
		return fs.ErrNotExist
	}
	if _, ok := p.files[newPath]; ok {
		return fs.ErrExist
	}

	p.files[newPath] = file
	delete(p.files, oldPath)
	p.updateFilesSnapshot()
	p.deleteFileCache(oldPath)
	return nil
}

// UpdateFiles updates all files in the project with the provided map of files.
// It removes existing files not present in the new map and updates files from
// the new map.
func (p *Project) UpdateFiles(newFiles map[string]*File) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Delete files that are not in the new map.
	maps.DeleteFunc(p.files, func(path string, _ *File) bool {
		_, ok := newFiles[path]
		if !ok {
			p.deleteFileCache(path)
		}
		return !ok
	})

	// Add or update files from the new map.
	for path, newFile := range newFiles {
		if oldFile, ok := p.files[path]; ok {
			// Only update if ModTime changed.
			if !oldFile.ModTime.Equal(newFile.ModTime) {
				p.files[path] = newFile
				p.deleteFileCache(path)
			}
		} else {
			// New file, always add.
			p.files[path] = newFile
			p.deleteFileCache(path)
		}
	}

	p.updateFilesSnapshot()
}

// updateFilesSnapshot updates the atomic snapshot of files.
func (p *Project) updateFilesSnapshot() {
	snapshot := maps.Clone(p.files)
	p.filesSnapshot.Store(&snapshot)
}

// -----------------------------------------------------------------------------

// RegisterCacheBuilder registers a project level cache builder.
//
// The kind should be a comparable type to avoid conflicts between packages. It
// is recommended to use a private type defined in your package:
//
//	type cacheKey int
//	const myProjectDataKey cacheKey = 1
//
//	proj.RegisterCacheBuilder(myProjectDataKey, myBuilder)
func (p *Project) RegisterCacheBuilder(kind CacheKind, builder func(root *Project) (any, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cacheBuilders[kind] = builder
}

// RegisterFileCacheBuilder registers a file level cache builder.
//
// The kind should be a comparable type to avoid conflicts between packages. It
// is recommended to use a private type defined in your package:
//
//	type cacheKey int
//	const myFileDataKey cacheKey = 1
//
//	proj.RegisterFileCacheBuilder(myFileDataKey, myBuilder)
func (p *Project) RegisterFileCacheBuilder(kind CacheKind, builder func(proj *Project, path string, file *File) (any, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fileCacheBuilders[kind] = builder
}

// -----------------------------------------------------------------------------

// Cache gets a project level cache. It builds the cache if it doesn't exist.
//
// The kind must be the same comparable value that was used with [Project.RegisterCacheBuilder].
func (p *Project) Cache(kind CacheKind) (any, error) {
	p.mu.RLock()
	v, ok := p.caches[kind]
	p.mu.RUnlock()
	if ok {
		return decodeDataOrErr(v)
	}

	data, err, _ := p.cacheSFG.Do(fmt.Sprintf("%T-%v", kind, kind), func() (any, error) {
		p.mu.RLock()
		builder, ok := p.cacheBuilders[kind]
		p.mu.RUnlock()
		if !ok {
			return nil, ErrUnknownCacheKind
		}

		data, err := builder(p)

		p.mu.Lock()
		p.caches[kind] = encodeDataOrErr(data, err)
		p.mu.Unlock()

		return data, err
	})
	return data, err
}

// FileCache gets a file level cache. It builds the cache if it doesn't exist.
//
// The kind must be the same comparable value that was used with [Project.RegisterFileCacheBuilder].
func (p *Project) FileCache(kind CacheKind, path string) (any, error) {
	key := fileCacheKey{kind, path}

	p.mu.RLock()
	v, ok := p.fileCaches[key]
	p.mu.RUnlock()
	if ok {
		return decodeDataOrErr(v)
	}

	data, err, _ := p.fileCacheSFG.Do(fmt.Sprintf("%T-%v-%s", kind, kind, path), func() (any, error) {
		p.mu.RLock()
		builder, ok := p.fileCacheBuilders[kind]
		file, fileExists := p.files[path]
		p.mu.RUnlock()
		if !ok {
			return nil, ErrUnknownCacheKind
		}
		if !fileExists {
			return nil, fs.ErrNotExist
		}

		data, err := builder(p, path, file)

		p.mu.Lock()
		p.fileCaches[key] = encodeDataOrErr(data, err)
		p.mu.Unlock()

		return data, err
	})
	return data, err
}

// deleteFileCache deletes file-specific caches for the given path. It also
// clears project-level caches implicitly if necessary.
func (p *Project) deleteFileCache(path string) {
	clear(p.caches)
	for kind := range p.fileCacheBuilders {
		delete(p.fileCaches, fileCacheKey{kind, path})
	}
}

// -----------------------------------------------------------------------------

// dataOrErr represents a data or an error.
type dataOrErr = any

// encodeDataOrErr selects either data or error to store as a single cache
// value. If err is not nil, stores the error; otherwise stores the data.
func encodeDataOrErr(data any, err error) dataOrErr {
	if err != nil {
		return err
	}
	return data
}

// decodeDataOrErr extracts data and error from a cached value. The cache
// stores either the actual data (if no error occurred) or the error itself.
func decodeDataOrErr(v dataOrErr) (any, error) {
	if err, ok := v.(error); ok {
		return nil, err
	}
	return v, nil
}

// -----------------------------------------------------------------------------
