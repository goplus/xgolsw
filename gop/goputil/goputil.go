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

package goputil

import (
	"github.com/goplus/gop/ast"
	"github.com/goplus/gop/token"
	"github.com/goplus/goxlsw/gop"
)

// RangeASTSpecs iterates all Go+ AST specs.
func RangeASTSpecs(proj *gop.Project, tok token.Token, f func(spec ast.Spec)) {
	proj.RangeASTFiles(func(_ string, file *ast.File) bool {
		for _, decl := range file.Decls {
			if decl, ok := decl.(*ast.GenDecl); ok && decl.Tok == tok {
				for _, spec := range decl.Specs {
					f(spec)
				}
			}
		}
		return true
	})
}

// IsShadow checks if the ident is shadowed.
func IsShadow(proj *gop.Project, ident *ast.Ident) (shadow bool) {
	proj.RangeASTFiles(func(_ string, file *ast.File) bool {
		if e := file.ShadowEntry; e != nil && e.Name == ident {
			shadow = true
			return false
		}
		for _, decl := range file.Decls {
			if funcDecl, ok := decl.(*ast.FuncDecl); ok && funcDecl.Name == ident {
				shadow = funcDecl.Shadow
				return false
			}
		}
		return true
	})
	return
}
