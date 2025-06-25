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

// cacheFeature represents a cache feature configuration that maps feature
// flags to their corresponding cache builders.
type cacheFeature struct {
	flag    uint
	kind    CacheKind
	builder any
}

// builtinCacheFeatures defines the built-in cache features and their configurations.
var builtinCacheFeatures = []cacheFeature{
	{FeatASTCache, astFileCacheKind{}, buildASTFileCache},
	{FeatASTCache, astPackageCacheKind{}, buildASTPackageCache},
	{FeatTypeInfoCache, typeInfoCacheKind{}, buildTypeInfoCache},
	{FeatPkgDocCache, pkgDocCacheKind{}, buildPkgDocCache},
}

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
	for _, feat := range builtinCacheFeatures {
		if feat.flag&feats != 0 {
			switch feat.builder.(type) {
			case CacheBuilder:
				proj.RegisterCacheBuilder(feat.kind, feat.builder.(CacheBuilder))
			case FileCacheBuilder:
				proj.RegisterFileCacheBuilder(feat.kind, feat.builder.(FileCacheBuilder))
			}
		}
	}
	return proj
}

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
