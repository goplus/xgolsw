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

import "github.com/goplus/xgolsw/pkgdoc"

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
	return &pkgDocCache{pkgdoc.NewXGo(proj.PkgPath, pkg)}, nil
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
