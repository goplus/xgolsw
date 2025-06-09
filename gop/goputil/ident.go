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
	"go/types"

	"github.com/goplus/gop/ast"
	"github.com/goplus/gop/token"
	"github.com/goplus/gop/x/typesutil"
	"github.com/goplus/goxlsw/gop"
)

// IdentsAtLine returns the identifiers at the given line in the given AST file.
func IdentsAtLine(proj *gop.Project, astFile *ast.File, line int) (idents []*ast.Ident) {
	fset := proj.Fset
	astFilePos := fset.Position(astFile.Pos())
	collectIdentAtLine := func(ident *ast.Ident) {
		identPos := fset.Position(ident.Pos())
		if identPos.Filename == astFilePos.Filename && identPos.Line == line {
			idents = append(idents, ident)
		}
	}
	_, typeInfo, _, _ := proj.TypeInfo()
	for ident := range typeInfo.Defs {
		if ident.Implicit() {
			continue
		}
		collectIdentAtLine(ident)
	}
	for ident, obj := range typeInfo.Uses {
		if defIdent := DefIdentFor(typeInfo, obj); defIdent != nil && defIdent.Implicit() {
			continue
		}
		collectIdentAtLine(ident)
	}
	return
}

// IdentAtPosition returns the identifier at the given position in the given AST file.
func IdentAtPosition(proj *gop.Project, astFile *ast.File, position token.Position) *ast.Ident {
	var (
		bestIdent    *ast.Ident
		bestNodeSpan int
	)
	for _, ident := range IdentsAtLine(proj, astFile, position.Line) {
		identPos := proj.Fset.Position(ident.Pos())
		identEnd := proj.Fset.Position(ident.End())
		if position.Column < identPos.Column || position.Column > identEnd.Column {
			continue
		}

		nodeSpan := identEnd.Column - identPos.Column
		if bestIdent == nil || nodeSpan < bestNodeSpan {
			bestIdent = ident
			bestNodeSpan = nodeSpan
		}
	}
	return bestIdent
}

// DefIdentFor returns the identifier where the given object is defined.
func DefIdentFor(typeInfo *typesutil.Info, obj types.Object) *ast.Ident {
	if typeInfo == nil || obj == nil {
		return nil
	}
	for ident, o := range typeInfo.Defs {
		if o == obj {
			return ident
		}
	}
	return nil
}

// RefIdentsFor returns all identifiers where the given object is referenced.
func RefIdentsFor(typeInfo *typesutil.Info, obj types.Object) []*ast.Ident {
	if typeInfo == nil || obj == nil {
		return nil
	}
	var idents []*ast.Ident
	for ident, o := range typeInfo.Uses {
		if o == obj {
			idents = append(idents, ident)
		}
	}
	return idents
}
