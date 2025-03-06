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
	"go/types"

	"github.com/goplus/gop/ast"
	"github.com/goplus/gop/parser"
	"github.com/goplus/gop/scanner"
	"github.com/goplus/gop/x/typesutil"
	"github.com/qiniu/x/errors"
)

type supportedFeat struct {
	feat     uint
	kind     string
	builder  any
	fileFeat bool
}

var supportedFeats = []supportedFeat{
	{FeatAST, "ast", buildAST, true},
	{FeatTypeInfo, "typeinfo", buildTypeInfo, false},
}

// -----------------------------------------------------------------------------

// TODO(xsw): always ParseGoPlusClass?
const parserMode = parser.ParseComments | parser.AllErrors | parser.ParseGoPlusClass

func buildAST(proj *Project, path string, file File) (any, error) {
	return parser.ParseEntry(proj.Fset, path, file.Content, parser.Config{
		Mode: parserMode,
	})
}

// AST returns the AST of a Go+ source file.
func (p *Project) AST(path string) (ret *ast.File, err error) {
	c, err := p.FileCache("ast", path)
	if err != nil {
		return
	}
	return c.(*ast.File), nil
}

// ASTFiles returns the AST of all Go+ source files.
func (p *Project) ASTFiles() (ret []*ast.File, err error) {
	ret, errs := p.getASTFiles()
	err = errs.ToError()
	return
}

func (p *Project) getASTFiles() (ret []*ast.File, errs errors.List) {
	p.RangeFiles(func(path string) bool {
		f, e := p.AST(path)
		if e != nil {
			if el, ok := e.(scanner.ErrorList); ok {
				for _, e := range el {
					errs = append(errs, e)
				}
			} else {
				errs = append(errs, e)
			}
		} else {
			ret = append(ret, f)
		}
		return true
	})
	return
}

// -----------------------------------------------------------------------------

func defaultNewTypeInfo() *typesutil.Info {
	return &typesutil.Info{
		Types:      make(map[ast.Expr]types.TypeAndValue),
		Defs:       make(map[*ast.Ident]types.Object),
		Uses:       make(map[*ast.Ident]types.Object),
		Selections: make(map[*ast.SelectorExpr]*types.Selection),
		Implicits:  make(map[ast.Node]types.Object),
		Scopes:     make(map[ast.Node]*types.Scope),
	}
}

func buildTypeInfo(proj *Project) (any, error) {
	var errs errors.List
	pkg := types.NewPackage(proj.Path, proj.Name)
	info := proj.NewTypeInfo()
	chk := typesutil.NewChecker(
		&types.Config{
			Error:    func(err error) { errs.Add(err) },
			Importer: proj.Importer,
		},
		&typesutil.Config{
			Types: pkg,
			Fset:  proj.Fset,
			Mod:   proj.Mod,
		},
		nil,
		info,
	)
	files, err := proj.getASTFiles()
	errs = append(errs, err...)
	if e := chk.Files(nil, files); e != nil && len(errs) == 0 {
		errs.Add(e)
	}
	return &typeInfoRet{pkg, info, errs}, nil
}

type typeInfoRet struct {
	pkg  *types.Package
	info *typesutil.Info
	errs errors.List
}

// TypeInfo returns the type information of a Go+ project.
func (p *Project) TypeInfo() (pkg *types.Package, info *typesutil.Info, err error) {
	c, err := p.Cache("typeinfo")
	if err != nil {
		return
	}
	ret := c.(*typeInfoRet)
	return ret.pkg, ret.info, ret.errs.ToError()
}

// -----------------------------------------------------------------------------
