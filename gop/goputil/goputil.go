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
	"go/constant"
	"go/types"
	"strconv"

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

// IsDefinedInClassFieldsDecl reports whether the given object is defined in
// the class fields declaration of an AST file.
func IsDefinedInClassFieldsDecl(proj *gop.Project, obj types.Object) bool {
	defIdent := DefIdentFor(proj, obj)
	if defIdent == nil {
		return false
	}
	astFile := NodeASTFile(proj, defIdent)
	if astFile == nil {
		return false
	}
	decl := astFile.ClassFieldsDecl()
	if decl == nil {
		return false
	}
	return defIdent.Pos() >= decl.Pos() && defIdent.End() <= decl.End()
}

// WalkNodesFromInterval walks the AST path starting from a position interval,
// calling walkFn for each node. The function walks from the smallest enclosing
// node outward through parent nodes. If walkFn returns false, the walk stops.
func WalkNodesFromInterval(root *ast.File, start, end token.Pos, walkFn func(node ast.Node) bool) {
	path, _ := PathEnclosingInterval(root, start, end)
	for _, node := range path {
		if !walkFn(node) {
			break
		}
	}
}

// ToLowerCamelCase converts the first character of a Go identifier to lowercase.
func ToLowerCamelCase(s string) string {
	if s == "" {
		return s
	}
	return string(s[0]|32) + s[1:]
}

// StringLitOrConstValue attempts to get the value from a string literal or
// constant. It returns the string value and true if successful, or empty string
// and false if the expression is not a string literal or constant, or if the
// value cannot be determined.
func StringLitOrConstValue(expr ast.Expr, tv types.TypeAndValue) (string, bool) {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(e.Value)
		if err != nil {
			return "", false
		}
		return v, true
	case *ast.Ident:
		if tv.Value != nil && tv.Value.Kind() == constant.String {
			// If it's a constant, we can get its value.
			return constant.StringVal(tv.Value), true
		}
		// There is nothing we can do for string variables.
		return "", false
	default:
		return "", false
	}
}
