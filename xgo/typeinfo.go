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
	"go/types"
	"maps"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/qiniu/x/errors"
)

// typeInfoCacheKind is a cache kind type for [TypeInfo].
type typeInfoCacheKind struct{}

// typeInfoCache is a cache for [TypeInfo].
type typeInfoCache struct {
	typeInfo   *TypeInfo
	checkerErr error
}

// buildTypeInfoCache implements [CacheBuilder] to build a [typeInfoCache] for
// the provided XGo project.
func buildTypeInfoCache(proj *Project) (any, error) {
	astPkg, astErr := proj.ASTPackage()
	if astPkg == nil {
		return nil, fmt.Errorf("failed to retrieve AST package: %w", astErr)
	}

	typeInfo := &TypeInfo{
		Info: typesutil.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Defs:       make(map[*ast.Ident]types.Object),
			Uses:       make(map[*ast.Ident]types.Object),
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
			Implicits:  make(map[ast.Node]types.Object),
			Scopes:     make(map[ast.Node]*types.Scope),
		},
		pkg: types.NewPackage(proj.PkgPath, astPkg.Name),
	}

	var checkerErrs errors.List
	if err := typesutil.NewChecker(
		&types.Config{
			Error:    func(err error) { checkerErrs.Add(err) },
			Importer: proj.Importer,
		},
		&typesutil.Config{
			Types: typeInfo.pkg,
			Fset:  proj.Fset,
			Mod:   proj.Mod,
		},
		nil,
		&typeInfo.Info,
	).Files(nil, slices.Collect(maps.Values(astPkg.Files))); err != nil && len(checkerErrs) == 0 {
		checkerErrs.Add(err)
	}
	return &typeInfoCache{typeInfo, checkerErrs.ToError()}, nil
}

// TypeInfo retrieves the [TypeInfo] from the project. The returned
// [TypeInfo] is nil only if building failed.
//
// NOTE: Both the returned [TypeInfo] and error can be non-nil, which
// indicates that only part of the project was type checked successfully.
func (p *Project) TypeInfo() (*TypeInfo, error) {
	cacheIface, err := p.Cache(typeInfoCacheKind{})
	if err != nil {
		return nil, err
	}
	cache := cacheIface.(*typeInfoCache)
	return cache.typeInfo, cache.checkerErr
}

// TypeInfo is an enhanced version of [typesutil.Info] for XGo projects. It
// embeds [typesutil.Info] and adds additional functionality and context.
type TypeInfo struct {
	typesutil.Info

	pkg *types.Package
}

// Pkg returns the package associated with this type information.
func (ti *TypeInfo) Pkg() *types.Package {
	return ti.pkg
}

// DefIdentFor returns the identifier where the given object is defined.
func (ti *TypeInfo) DefIdentFor(obj types.Object) *ast.Ident {
	if obj == nil {
		return nil
	}
	for ident, o := range ti.Defs {
		if o == obj {
			return ident
		}
	}
	return nil
}

// RefIdentsFor returns all identifiers where the given object is referenced.
func (ti *TypeInfo) RefIdentsFor(obj types.Object) []*ast.Ident {
	if obj == nil {
		return nil
	}
	var idents []*ast.Ident
	for ident, o := range ti.Uses {
		if o == obj {
			idents = append(idents, ident)
		}
	}
	return idents
}
