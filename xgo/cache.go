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
	"io/fs"
)

// ErrUnknownCacheKind represents an error of unknown cache kind.
var ErrUnknownCacheKind = errors.New("unknown cache kind")

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

// RegisterCacheBuilder registers a project level cache builder.
//
// The kind should be a comparable type to avoid conflicts between packages. It
// is recommended to use a private type defined in your package:
//
//	type myCacheKind struct{}
//
//	proj.RegisterCacheBuilder(myCacheKind{}, myBuilder)
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
//	type myCacheKind struct{}
//
//	proj.RegisterFileCacheBuilder(myCacheKind{}, myBuilder)
func (p *Project) RegisterFileCacheBuilder(kind CacheKind, builder func(proj *Project, path string, file *File) (any, error)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fileCacheBuilders[kind] = builder
}

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
