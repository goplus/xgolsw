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

package xgoutil

import (
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
)

// IdentAtPosition returns the identifier at the given position in the given AST file.
func IdentAtPosition(proj *xgo.Project, astFile *ast.File, position token.Position) *ast.Ident {
	fset := proj.Fset
	astFilePosition := fset.Position(astFile.Pos())
	if astFilePosition.Filename != position.Filename {
		return nil
	}

	tokenFile := PosTokenFile(proj, astFile.Pos())
	if tokenFile == nil {
		return nil
	}
	if position.Line < 1 || position.Line > tokenFile.LineCount() {
		return nil
	}

	var (
		linePos = tokenFile.LineStart(position.Line)
		lineEnd token.Pos
	)
	if position.Line < tokenFile.LineCount() {
		lineEnd = tokenFile.LineStart(position.Line + 1)
	} else {
		lineEnd = token.Pos(tokenFile.Base() + tokenFile.Size())
	}

	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}

	var (
		bestIdent    *ast.Ident
		bestNodeSpan int
	)
	checkIdent := func(ident *ast.Ident) (isBestPossibleMatch bool) {
		if ident.Implicit() {
			return
		}

		identPos := ident.Pos()
		if identPos < linePos || identPos >= lineEnd {
			return
		}
		identPosPosition := fset.Position(identPos)
		identEndPosition := fset.Position(ident.End())
		if identPosPosition.Column > position.Column || identEndPosition.Column < position.Column {
			return
		}

		// Select the identifier with the smallest span when multiple identifiers overlap.
		nodeSpan := identEndPosition.Column - identPosPosition.Column
		if bestIdent == nil || nodeSpan < bestNodeSpan {
			bestIdent = ident
			bestNodeSpan = nodeSpan
			isBestPossibleMatch = bestNodeSpan == 1 && identPosPosition.Column == position.Column
		}
		return
	}
	for ident := range typeInfo.Defs {
		if checkIdent(ident) {
			return ident
		}
	}
	for ident := range typeInfo.Uses {
		if checkIdent(ident) {
			return ident
		}
	}
	return bestIdent
}
