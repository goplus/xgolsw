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
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/pkgdoc"
)

// pkgDocCacheKind is a cache kind type for [pkgdoc.PkgDoc].
type pkgDocCacheKind struct{}

// pkgDocCache is a cache for [pkgdoc.PkgDoc].
type pkgDocCache struct {
	pkgDoc *pkgdoc.PkgDoc
}

// buildPkgDocCache implements [CacheBuilder] to build a [pkgDocCache] for the
// provided XGo project.
func buildPkgDocCache(proj *Project) (any, error) {
	pkg, err := proj.ASTPackage()
	if err != nil {
		return nil, err
	}
	// Only class ownership requires checked types.
	hasClass := false
	for _, file := range pkg.Files {
		if file.IsClass {
			hasClass = true
			break
		}
	}
	var checked *typeInfoCache
	if hasClass {
		// Type checking also acquires astMu on a cache miss.
		if data, err := proj.Cache(typeInfoCacheKind{}); err == nil {
			checked = data.(*typeInfoCache)
			if checked.parserErr != nil {
				return nil, checked.parserErr
			}
		}
	}

	proj.astMu.Lock()
	defer proj.astMu.Unlock()
	if checked == nil {
		return &pkgDocCache{pkgdoc.NewXGo(proj.PkgPath, pkg, proj.Module().LookupClass)}, nil
	}
	// The owners are keyed by the exact syntax nodes used by the checker.
	doc := pkgdoc.NewCheckedXGo(proj.PkgPath, checked.astPkg, checked.module.LookupClass, func(file *ast.File) string {
		if owner := checked.typeInfo.ClassTypes[file]; owner != nil {
			return owner.Obj().Name()
		}
		return ""
	})
	return &pkgDocCache{doc}, nil
}

// PkgDoc retrieves the [pkgdoc.PkgDoc] from the project.
func (p *Project) PkgDoc() (*pkgdoc.PkgDoc, error) {
	cacheIface, err := p.Cache(pkgDocCacheKind{})
	if err != nil {
		return nil, err
	}
	cache := cacheIface.(*pkgDocCache)
	return cache.pkgDoc, nil
}
