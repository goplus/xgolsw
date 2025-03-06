/*
 * Copyright (c) 2025 The GoPlus Authors (goplus.org). All rights reserved.
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

package gop

import (
	"errors"
	"go/token"
	"sync"
	"time"
)

var (
	// ErrUnknownKind represents an error of unknown kind.
	ErrUnknownKind = errors.New("unknown kind")

	// ErrNotFound represents an error of not found.
	ErrNotFound = errors.New("not found")

	// ErrFileExists represents an error of file exists.
	ErrFileExists = errors.New("file exists")
)

const (
	// FeatAST represents to build AST cache.
	FeatAST = 1 << iota

	FeatAll = FeatAST
)

// -----------------------------------------------------------------------------

// Builder represents a project level cache builder.
type Builder = func(proj *Project) (any, error)

// FileBuilder represents a file level cache builder.
type FileBuilder = func(proj *Project, path string, file File) (any, error)

// -----------------------------------------------------------------------------

type fileKey struct {
	kind string
	path string
}

// File represents a file.
type File struct {
	Content []byte
	ModTime time.Time
}

// Project represents a project.
type Project struct {
	files sync.Map // path => File

	caches     sync.Map // kind => dataOrErr
	fileCaches sync.Map // (kind, path) => dataOrErr

	// kind => builder
	builders     map[string]Builder
	fileBuilders map[string]FileBuilder

	Fset *token.FileSet
}

// NewProject creates a new project.
// files can be a map[string]File or a func() map[string]File.
func NewProject(fset *token.FileSet, files any, feats uint) *Project {
	if fset == nil {
		fset = token.NewFileSet()
	}
	ret := &Project{
		Fset:         fset,
		builders:     make(map[string]Builder),
		fileBuilders: make(map[string]FileBuilder),
	}
	if files != nil {
		var iniFiles map[string]File
		if v, ok := files.(map[string]File); ok {
			iniFiles = v
		} else if getf, ok := files.(func() map[string]File); ok {
			iniFiles = getf()
		} else {
			panic("NewProject: invalid files")
		}
		for path, file := range iniFiles {
			ret.files.Store(path, file)
		}
	}
	for _, f := range supportedFeats {
		if f.feat&feats != 0 {
			if f.fileFeat {
				ret.InitFileCache(f.kind, f.builder.(FileBuilder))
			} else {
				ret.InitCache(f.kind, f.builder.(Builder))
			}
		}
	}
	return ret
}

// -----------------------------------------------------------------------------

// Snapshot creates a snapshot of the project.
func (p *Project) Snapshot() *Project {
	ret := &Project{
		builders:     p.builders,
		fileBuilders: p.fileBuilders,
		Fset:         p.Fset,
	}
	copyMap(&ret.files, &p.files)
	copyMap(&ret.caches, &p.caches)
	copyMap(&ret.fileCaches, &p.fileCaches)
	return ret
}

func copyMap(dst, src *sync.Map) {
	src.Range(func(k, v any) bool {
		dst.Store(k, v)
		return true
	})
}

// -----------------------------------------------------------------------------

func (p *Project) deleteCache(path string) {
	p.caches.Clear()
	for kind := range p.fileBuilders {
		p.fileCaches.Delete(fileKey{kind, path})
	}
}

// Rename renames a file in the project.
func (p *Project) Rename(oldPath, newPath string) error {
	if v, ok := p.files.Load(oldPath); ok {
		if _, ok := p.files.LoadOrStore(newPath, v); ok {
			return ErrFileExists
		}
		p.files.Delete(oldPath)
		p.deleteCache(oldPath)
		return nil
	}
	return ErrNotFound
}

// DeleteFile deletes a file from the project.
func (p *Project) DeleteFile(path string) error {
	if _, ok := p.files.LoadAndDelete(path); ok {
		p.deleteCache(path)
		return nil
	}
	return ErrNotFound
}

// PutFile puts a file into the project.
func (p *Project) PutFile(path string, file File) {
	p.files.Store(path, file)
	p.deleteCache(path)
}

// File gets a file from the project.
func (p *Project) File(path string) (ret File, ok bool) {
	v, ok := p.files.Load(path)
	if ok {
		ret = v.(File)
	}
	return
}

// -----------------------------------------------------------------------------

// InitFileCache initializes a file level cache.
func (p *Project) InitFileCache(kind string, builder func(proj *Project, path string, file File) (any, error)) {
	p.fileBuilders[kind] = builder
}

// InitCache initializes a project level cache.
func (p *Project) InitCache(kind string, builder func(root *Project) (any, error)) {
	p.builders[kind] = builder
}

// FileCache gets a file level cache.
func (p *Project) FileCache(kind, path string) (any, error) {
	key := fileKey{kind, path}
	if v, ok := p.fileCaches.Load(key); ok {
		return decodeDataOrErr(v)
	}
	builder, ok := p.fileBuilders[kind]
	if !ok {
		return nil, ErrUnknownKind
	}
	file, ok := p.File(path)
	if !ok {
		return nil, ErrNotFound
	}
	data, err := builder(p, path, file)
	p.fileCaches.Store(key, encodeDataOrErr(data, err))
	return data, err
}

// Cache gets a project level cache.
func (p *Project) Cache(kind string) (any, error) {
	if v, ok := p.caches.Load(kind); ok {
		return decodeDataOrErr(v)
	}
	builder, ok := p.builders[kind]
	if !ok {
		return nil, ErrUnknownKind
	}
	data, err := builder(p)
	p.caches.Store(kind, encodeDataOrErr(data, err))
	return data, err
}

func decodeDataOrErr(v any) (any, error) {
	if err, ok := v.(error); ok {
		return nil, err
	}
	return v, nil
}

func encodeDataOrErr(data any, err error) any {
	if err != nil {
		return err
	}
	return data
}

// -----------------------------------------------------------------------------
