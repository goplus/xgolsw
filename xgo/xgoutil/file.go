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
)

// PosFilename returns the logical filename for pos, honoring line directives.
// Use [PosTokenFile] for the physical file that contains the position.
func PosFilename(fset *token.FileSet, pos token.Pos) string {
	if fset == nil || !pos.IsValid() {
		return ""
	}
	return fset.Position(pos).Filename
}

// NodeFilename returns the logical filename for node, honoring line directives.
// Use [NodeTokenFile] for the physical file that contains the node.
func NodeFilename(fset *token.FileSet, node ast.Node) string {
	if fset == nil || node == nil {
		return ""
	}
	return PosFilename(fset, node.Pos())
}

// PosTokenFile returns the token file for the given position.
func PosTokenFile(fset *token.FileSet, pos token.Pos) *token.File {
	if fset == nil || !pos.IsValid() {
		return nil
	}
	return fset.File(pos)
}

// NodeTokenFile returns the token file for the given node.
func NodeTokenFile(fset *token.FileSet, node ast.Node) *token.File {
	if fset == nil || node == nil {
		return nil
	}
	return PosTokenFile(fset, node.Pos())
}

// PosASTFile returns the physical AST file for the given position, ignoring
// filename remapping by line directives.
func PosASTFile(fset *token.FileSet, astPkg *ast.Package, pos token.Pos) *ast.File {
	file := PosTokenFile(fset, pos)
	if file == nil || astPkg == nil {
		return nil
	}
	return astPkg.Files[file.Name()]
}

// NodeASTFile returns the AST file for the given node.
func NodeASTFile(fset *token.FileSet, astPkg *ast.Package, node ast.Node) *ast.File {
	if fset == nil || astPkg == nil || node == nil {
		return nil
	}
	return PosASTFile(fset, astPkg, node.Pos())
}
