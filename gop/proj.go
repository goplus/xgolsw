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
)

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
	builders     map[string]func(proj *Project) (any, error)
	fileBuilders map[string]func(path string, file File) (any, error)
}

// NewProject creates a new project.
func NewProject(files map[string]File, feats uint) *Project {
	ret := &Project{
		builders:     make(map[string]func(root *Project) (any, error)),
		fileBuilders: make(map[string]func(path string, file File) (any, error)),
	}
	for path, file := range files {
		ret.files.Store(path, file)
	}
	// TODO(xsw): support features
	return ret
}

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

// GetFile gets a file from the project.
func (p *Project) GetFile(path string) (ret File, ok bool) {
	v, ok := p.files.Load(path)
	if ok {
		ret = v.(File)
	}
	return
}

// -----------------------------------------------------------------------------

// InitFileCache initializes a file level cache.
func (p *Project) InitFileCache(kind string, builder func(path string, file File) (any, error)) {
	p.fileBuilders[kind] = builder
}

// InitCache initializes a project level cache.
func (p *Project) InitCache(kind string, builder func(root *Project) (any, error)) {
	p.builders[kind] = builder
}

func (p *Project) GetFileCache(kind, path string) (any, error) {
	key := fileKey{kind, path}
	if v, ok := p.fileCaches.Load(key); ok {
		return decodeDataOrErr(v)
	}
	builder, ok := p.fileBuilders[kind]
	if !ok {
		return nil, ErrUnknownKind
	}
	data, err := builder(path, File{})
	p.fileCaches.Store(key, encodeDataOrErr(data, err))
	return data, err
}

// GetCache gets a project level cache.
func (p *Project) GetCache(kind string) (any, error) {
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
