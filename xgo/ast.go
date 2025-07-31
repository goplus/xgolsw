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
	"go/scanner"
	"go/token"
	"path"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
)

// astFileCacheKind is a cache kind type for [ast.File].
type astFileCacheKind struct{}

// astFileCache is a cache for [ast.File].
type astFileCache struct {
	astFile   *ast.File
	parserErr error
}

// buildASTFileCache implements [FileCacheBuilder] to build an [astFileCache]
// for the provided XGo source file.
func buildASTFileCache(proj *Project, path string, file *File) (cache any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("parser panic: %v", r)
		}
	}()
	mode := parser.ParseComments | parser.AllErrors
	if !strings.HasSuffix(path, ".xgo") && !strings.HasSuffix(path, ".gop") { // TODO(xsw): use xgomod
		mode |= parser.ParseGoPlusClass
	}
	astFile, parserErr := parser.ParseEntry(proj.Fset, path, file.Content, parser.Config{
		Mode: mode,
	})
	cache = &astFileCache{astFile, parserErr}
	return
}

// ASTFile retrieves the [ast.File] for the specified source file from the
// project. The returned [ast.File] is nil only if building failed.
//
// NOTE: Both the returned [ast.File] and error can be non-nil, which indicates
// that only part of the file was parsed successfully.
func (p *Project) ASTFile(path string) (*ast.File, error) {
	cacheIface, err := p.FileCache(astFileCacheKind{}, path)
	if err != nil {
		return nil, err
	}
	cache := cacheIface.(*astFileCache)
	return cache.astFile, cache.parserErr
}

// astPackageCacheKind is a cache kind type for [ast.Package].
type astPackageCacheKind struct{}

// astPackageCache is a cache for [ast.Package].
type astPackageCache struct {
	astPkg    *ast.Package
	parserErr error
}

// buildASTPackageCache implements [CacheBuilder] to build an [astPackageCache]
// for the provided XGo project.
func buildASTPackageCache(proj *Project) (any, error) {
	pkg := &ast.Package{
		Files: make(map[string]*ast.File),
	}
	var parserErrs scanner.ErrorList
	for file := range proj.Files() {
		switch path.Ext(file) { // TODO(xsw): use xgomod
		case ".spx", ".xgo", ".gop", ".gox":
			astFile, err := proj.ASTFile(file)
			if err != nil {
				if el, ok := err.(scanner.ErrorList); ok {
					parserErrs = append(parserErrs, el...)
				} else {
					parserErrs.Add(token.Position{}, err.Error())
				}
			}
			if astFile != nil {
				if pkg.Name == "" {
					pkg.Name = astFile.Name.Name
				}
				pkg.Files[file] = astFile
			}
		}
	}
	return &astPackageCache{pkg, parserErrs.Err()}, nil
}

// ASTPackage retrieves the [ast.Package] from the project. The returned
// [ast.Package] is nil only if building failed.
//
// NOTE: Both the returned [ast.Package] and error can be non-nil, which
// indicates that only part of the project was parsed successfully.
func (p *Project) ASTPackage() (*ast.Package, error) {
	cacheIface, err := p.Cache(astPackageCacheKind{})
	if err != nil {
		return nil, err
	}
	cache := cacheIface.(*astPackageCache)
	return cache.astPkg, cache.parserErr
}
