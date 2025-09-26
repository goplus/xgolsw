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
	"go/constant"
	"go/types"
	"iter"
	"slices"
	"strconv"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
)

// RangeASTSpecs iterates all XGo AST specs.
func RangeASTSpecs(astPkg *ast.Package, tok token.Token, f func(spec ast.Spec)) {
	if astPkg == nil {
		return
	}
	for _, file := range astPkg.Files {
		for _, decl := range file.Decls {
			if decl, ok := decl.(*ast.GenDecl); ok && decl.Tok == tok {
				for _, spec := range decl.Specs {
					f(spec)
				}
			}
		}
	}
}

// IsDefinedInClassFieldsDecl reports whether the given object is defined in the
// class fields declaration of an AST file.
func IsDefinedInClassFieldsDecl(fset *token.FileSet, typeInfo *xgotypes.Info, astPkg *ast.Package, obj types.Object) bool {
	if fset == nil || typeInfo == nil || astPkg == nil || obj == nil {
		return false
	}
	defIdent := typeInfo.ObjToDef[obj]
	if defIdent == nil {
		return false
	}
	astFile := NodeASTFile(fset, astPkg, defIdent)
	if astFile == nil {
		return false
	}
	decl := astFile.ClassFieldsDecl()
	if decl == nil {
		return false
	}
	return defIdent.Pos() >= decl.Pos() && defIdent.End() <= decl.End()
}

// WalkPathEnclosingInterval calls walkFn for each node in the AST path
// enclosing the given [start, end) interval, starting from the innermost node
// and walking outward. The walk stops if walkFn returns false.
func WalkPathEnclosingInterval(root *ast.File, start, end token.Pos, backward bool, walkFn func(node ast.Node) bool) {
	path, _ := PathEnclosingInterval(root, start, end)
	var seq iter.Seq2[int, ast.Node]
	if backward {
		seq = slices.Backward(path)
	} else {
		seq = slices.All(path)
	}
	for _, node := range seq {
		if !walkFn(node) {
			break
		}
	}
}

// EnclosingFuncSignature returns the function signature enclosing the AST path.
// It searches from the innermost node outward and supports both function
// declarations and literals. It returns nil if not found.
func EnclosingFuncSignature(typeInfo *xgotypes.Info, path []ast.Node) *types.Signature {
	if typeInfo == nil {
		return nil
	}
	for _, node := range slices.Backward(path) {
		switch node := node.(type) {
		case *ast.FuncLit:
			if tv, ok := typeInfo.Types[node]; ok {
				if sig, ok := tv.Type.(*types.Signature); ok {
					return sig
				}
			}
		case *ast.FuncDecl:
			obj := typeInfo.ObjectOf(node.Name)
			if obj == nil {
				continue
			}
			fun, ok := obj.(*types.Func)
			if !ok {
				continue
			}
			sig, ok := fun.Type().(*types.Signature)
			if ok {
				return sig
			}
		}
	}
	return nil
}

// EnclosingNode returns the nearest enclosing node of type T in the given AST
// path. It searches from the innermost node outward and returns the zero value
// of T if not found.
func EnclosingNode[T ast.Node](path []ast.Node) T {
	for _, node := range slices.Backward(path) {
		if node, ok := node.(T); ok {
			return node
		}
	}
	var zero T
	return zero
}

// EnclosingReturnStmt returns the nearest enclosing return statement in the
// given AST path. It returns nil if not found.
func EnclosingReturnStmt(path []ast.Node) *ast.ReturnStmt {
	return EnclosingNode[*ast.ReturnStmt](path)
}

// ReturnValueIndex returns the index of target within the provided return
// statement. If the exact node is not present, it falls back to locating the
// expression by source range containment. It returns -1 if not found.
func ReturnValueIndex(stmt *ast.ReturnStmt, target ast.Expr) int {
	if stmt == nil || target == nil {
		return -1
	}
	for i, expr := range stmt.Results {
		if expr == nil {
			continue
		}
		if expr == target {
			return i
		}
		if target.Pos().IsValid() && target.End().IsValid() &&
			target.Pos() >= expr.Pos() && target.End() <= expr.End() {
			return i
		}
	}
	return -1
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
