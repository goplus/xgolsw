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
	"go/parser"

	"github.com/goplus/gop/ast"
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

const parserMode = parser.ParseComments

func buildAST(proj *Project, path string, file File) (any, error) {
	return parser.ParseFile(proj.Fset, path, file.Content, parserMode)
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
