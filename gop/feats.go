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
	"github.com/goplus/gop/ast"
	"github.com/goplus/gop/parser"
)

type supportedFeat struct {
	feat     uint
	kind     string
	builder  any
	fileFeat bool
}

var supportedFeats = []supportedFeat{
	{FeatAST, "ast", buildAST, true},
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

// -----------------------------------------------------------------------------
