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
	gotypes "go/types"
	"maps"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/qiniu/x/errors"
)

// typeInfoCacheKind is a cache kind type for [types.Info].
type typeInfoCacheKind struct{}

// typeInfoCache is a cache for [types.Info].
type typeInfoCache struct {
	typeInfo   *types.Info
	checkerErr error
}

// buildTypeInfoCache implements [CacheBuilder] to build a [typeInfoCache] for
// the provided XGo project.
func buildTypeInfoCache(proj *Project) (any, error) {
	astPkg, astErr := proj.ASTPackage()
	if astPkg == nil {
		return nil, fmt.Errorf("failed to retrieve AST package: %w", astErr)
	}
	proj.astMu.Lock()
	defer proj.astMu.Unlock()

	typeInfo := &types.Info{
		Info: typesutil.Info{
			Types:      make(map[ast.Expr]gotypes.TypeAndValue),
			Defs:       make(map[*ast.Ident]gotypes.Object),
			Uses:       make(map[*ast.Ident]gotypes.Object),
			Overloads:  make(map[*ast.Ident]gotypes.Object),
			Selections: make(map[*ast.SelectorExpr]*gotypes.Selection),
			Implicits:  make(map[ast.Node]gotypes.Object),
			Scopes:     make(map[ast.Node]*gotypes.Scope),
		},
		Pkg:            gotypes.NewPackage(proj.PkgPath, astPkg.Name),
		FuncDecorators: make(map[*ast.CallExpr]bool),
	}

	var checkerErrs errors.List
	if err := typesutil.NewChecker(
		&gotypes.Config{
			Error:    func(err error) { checkerErrs.Add(err) },
			Importer: newDependencyImporter(proj.Importer),
		},
		&typesutil.Config{
			Types: typeInfo.Pkg,
			Fset:  proj.Fset,
			Mod:   proj.Module().mod,
		},
		nil,
		&typeInfo.Info,
	).Files(nil, slices.Collect(maps.Values(astPkg.Files))); err != nil && len(checkerErrs) == 0 {
		checkerErrs.Add(err)
	}

	// Build reverse mapping for O(1) object-to-identifier lookup. For
	// identifiers that do not denote objects, the object is nil and they
	// are excluded from this mapping.
	typeInfo.ObjToDef = make(map[gotypes.Object]*ast.Ident, len(typeInfo.Defs))
	for ident, obj := range typeInfo.Defs {
		if obj != nil {
			typeInfo.ObjToDef[obj] = ident
		}
	}
	// Record decorator syntax and recover declarations omitted by the compiler
	// so all consumers share the same source and argument mappings.
	for _, file := range astPkg.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			if decorator, ok := node.(*ast.FuncDecorator); ok {
				typeInfo.FuncDecorators[&decorator.CallExpr] = true
				return true
			}
			if stmt, ok := node.(*ast.TypeSwitchStmt); ok {
				recordTypeSwitchDeclaration(typeInfo, stmt)
				return true
			}
			spec, ok := node.(*ast.TypeSpec)
			if !ok || typeInfo.Defs[spec.Name] != nil {
				return true
			}
			scope := xgoutil.InnermostScopeAt(proj.Fset, typeInfo, astPkg, spec.Name.Pos())
			if scope == nil || scope == typeInfo.Pkg.Scope() {
				return true
			}
			obj, ok := scope.Lookup(spec.Name.Name).(*gotypes.TypeName)
			if ok && typeInfo.ObjToDef[obj] == nil {
				typeInfo.Defs[spec.Name] = obj
				typeInfo.ObjToDef[obj] = spec.Name
			}
			return true
		})
	}

	return &typeInfoCache{typeInfo, checkerErrs.ToError()}, nil
}

// recordTypeSwitchDeclaration links the variables in each case scope to their
// shared declaration without changing their branch-specific types.
func recordTypeSwitchDeclaration(info *types.Info, stmt *ast.TypeSwitchStmt) {
	assign, ok := stmt.Assign.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE {
		return
	}
	ident, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || ident.Name == "_" {
		return
	}
	// The parser identifies type switch guards by their single type assertion.
	assertion := assign.Rhs[0].(*ast.TypeAssertExpr)
	typ := info.TypeOf(assertion.X)
	if typ == nil || !gotypes.IsInterface(typ) {
		return
	}
	obj := gotypes.NewVar(ident.Pos(), info.Pkg, ident.Name, typ)
	info.Defs[ident] = obj
	info.ObjToDef[obj] = ident
	for _, clause := range stmt.Body.List {
		if scope := info.Scopes[clause]; scope != nil {
			if variable, ok := scope.Lookup(ident.Name).(*gotypes.Var); ok {
				info.ObjToDef[variable] = ident
			}
		}
	}
}

// TypeInfo retrieves the [types.Info] from the project. The returned [types.Info]
// is nil only if building failed.
//
// NOTE: Both the returned [types.Info] and error can be non-nil, which
// indicates that only part of the project was type checked successfully.
func (p *Project) TypeInfo() (*types.Info, error) {
	cacheIface, err := p.Cache(typeInfoCacheKind{})
	if err != nil {
		return nil, err
	}
	cache := cacheIface.(*typeInfoCache)
	return cache.typeInfo, cache.checkerErr
}
