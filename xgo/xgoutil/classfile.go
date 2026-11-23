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
	gotypes "go/types"
	"iter"
	"path/filepath"
	"slices"
	"strings"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/cl"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/types"
)

// ClassfileExt returns the class extension using the same basename as the parser.
func ClassfileExt(filename string) string {
	return modfile.ClassExt(filepath.Base(filename))
}

// IsFlatClassFragment reports whether file contributes to a flat project
// without declaring its own class or fields.
func IsFlatClassFragment(file *ast.File, filename string, lookupClass func(string) (*modfile.Project, bool)) bool {
	if !file.IsClass || file.IsNormalGox || file.IsProj || lookupClass == nil {
		return false
	}
	framework, ok := lookupClass(ClassfileExt(filename))
	return ok && framework.Flat
}

// SourceClassName returns the nominal class name when only syntax is available.
// Flat fragments have no class of their own and require project type information.
func SourceClassName(file *ast.File, filename string, lookupClass func(string) (*modfile.Project, bool)) string {
	if !file.IsClass {
		return ""
	}
	if !file.IsNormalGox {
		if lookupClass == nil {
			return ""
		}
		if framework, ok := lookupClass(ClassfileExt(filename)); !ok || framework.Flat && !file.IsProj {
			return ""
		}
	}
	name, _ := cl.GetFileClassType(file, filename, lookupClass)
	return strings.TrimPrefix(name, "*")
}

// ClassReceiverAt returns the implicit receiver available to a classfile
// expression at pos, if any.
func ClassReceiverAt(info *types.Info, file *ast.File, pos token.Pos) gotypes.Type {
	if file == nil || !file.IsClass {
		return nil
	}
	// Only file-level declarations can change the receiver. Avoid walking
	// statement lists when resolving many calls in the same classfile.
	for _, decl := range file.Decls {
		if decl.Pos() <= pos && pos < decl.End() {
			return EnclosingClassReceiver(info, file, pos, slices.All([]ast.Node{decl}))
		}
	}
	return nil
}

// EnclosingClassReceiver resolves the receiver for pos from its enclosing
// syntax, visited from innermost to outermost. An explicit receiver in a
// classfile takes precedence over the generated class.
func EnclosingClassReceiver(info *types.Info, file *ast.File, pos token.Pos, enclosing iter.Seq2[int, ast.Node]) gotypes.Type {
	if !file.IsClass {
		return nil
	}
	for _, node := range enclosing {
		switch node := node.(type) {
		case *ast.FuncDecl:
			if node.Static || node.Body == nil || pos < node.Body.Pos() || pos >= node.Body.End() {
				return nil
			}
			if fun, ok := info.Defs[node.Name].(*gotypes.Func); ok {
				if recv := fun.Signature().Recv(); recv != nil {
					return recv.Type()
				}
				return nil
			}
			if !node.IsClass {
				return nil
			}
		case *ast.OverloadFuncDecl:
			// Inline class overloads become methods. Their implicit receiver is
			// available inside each implementation body.
			if !node.IsClass || !slices.ContainsFunc(node.Funcs, func(expr ast.Expr) bool {
				fn, ok := expr.(*ast.FuncLit)
				return ok && fn.Body != nil && fn.Body.Pos() <= pos && pos < fn.Body.End()
			}) {
				return nil
			}
		case *ast.GenDecl:
			if node != file.ClassFields {
				continue
			}
		default:
			continue
		}
		if owner := info.ClassTypes[file]; owner != nil {
			return gotypes.NewPointer(owner)
		}
		return nil
	}
	return nil
}
