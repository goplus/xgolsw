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
	"github.com/goplus/xgolsw/xgo/types"
)

// IsSourceIdent reports whether ident matches the source text in file. Compiler
// generated identifiers can reuse real positions without being marked implicit.
// The file and code must describe the same source.
func IsSourceIdent(file *token.File, code []byte, ident *ast.Ident) bool {
	if ident.Implicit() {
		return false
	}
	start := int(ident.Pos()) - file.Base()
	end := start + len(ident.Name)
	return start >= 0 && end <= len(code) && string(code[start:end]) == ident.Name
}

// IdentAtPosition returns the identifier at a physical source position in astFile,
// ignoring line directives.
func IdentAtPosition(fset *token.FileSet, typeInfo *types.Info, astFile *ast.File, position token.Position) *ast.Ident {
	if fset == nil || typeInfo == nil || astFile == nil {
		return nil
	}

	astFilePosition := fset.PositionFor(astFile.Pos(), false)
	if astFilePosition.Filename != position.Filename {
		return nil
	}

	tokenFile := PosTokenFile(fset, astFile.Pos())
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

	var (
		bestIdent    *ast.Ident
		bestNodeSpan int
	)
	checkIdent := func(ident *ast.Ident) (isBestPossibleMatch bool) {
		if !IsSourceIdent(tokenFile, astFile.Code, ident) {
			return
		}

		identPos := ident.Pos()
		if identPos < linePos || identPos >= lineEnd {
			return
		}
		identPosPosition := fset.PositionFor(identPos, false)
		identEndPosition := fset.PositionFor(ident.End(), false)
		if identPosPosition.Column > position.Column || identEndPosition.Column <= position.Column {
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

// IsBlankIdent reports whether ident is the blank identifier "_".
func IsBlankIdent(ident *ast.Ident) bool {
	return ident != nil && ident.Name == "_"
}

// IsSyntheticThisIdent reports whether the identifier is the compiler-generated
// receiver "this" that XGo inserts at the start of a classfile. It matches both
// the defining identifier and any reference whose definition maps to that
// synthetic receiver.
func IsSyntheticThisIdent(fset *token.FileSet, typeInfo *types.Info, astPkg *ast.Package, ident *ast.Ident) bool {
	if fset == nil || typeInfo == nil || astPkg == nil || ident == nil || ident.Name != "this" {
		return false
	}

	pos := ident.Pos()
	if obj := typeInfo.ObjectOf(ident); obj != nil {
		pos = obj.Pos()
		if declaration := typeInfo.ObjToDef[obj]; declaration != nil {
			pos = declaration.Pos()
		}
	}
	astFile := PosASTFile(fset, astPkg, pos)
	if astFile == nil {
		return false
	}
	return astFile.IsClass && pos == astFile.Pos()
}
