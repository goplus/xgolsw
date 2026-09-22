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

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/types"
)

// InnermostScopeAt returns the innermost scope that contains the given
// position. It returns nil if not found.
func InnermostScopeAt(fset *token.FileSet, typeInfo *types.Info, astPkg *ast.Package, pos token.Pos) *gotypes.Scope {
	if fset == nil || typeInfo == nil || astPkg == nil || !pos.IsValid() {
		return nil
	}

	astFile := PosASTFile(fset, astPkg, pos)
	if astFile == nil {
		return nil
	}

	for node := range PathEnclosingIntervalNodes(astFile, pos, pos, false) {
		if scope := ScopeAtNode(typeInfo, node, pos); scope != nil {
			return scope
		}
	}
	return nil
}

// ScopeAtNode returns the scope associated with node at pos, or nil when node
// has no recorded scope. Range inputs precede iteration variables in evaluation
// order. Comprehension elements run inside their clauses despite appearing first.
func ScopeAtNode(typeInfo *types.Info, node ast.Node, pos token.Pos) *gotypes.Scope {
	scope := typeInfo.Scopes[node]
	var input ast.Expr
	switch node := node.(type) {
	case *ast.RangeStmt:
		input = node.X
	case *ast.ForPhrase:
		input = node.X
		if input.End() < pos {
			scope = forPhraseBodyScope(typeInfo, node)
		}
	case *ast.ForPhraseStmt:
		input = node.X
	case *ast.ComprehensionExpr:
		if node.Elt != nil && node.Elt.Pos() <= pos && pos <= node.Elt.End() && len(node.Fors) > 0 {
			// The compiler evaluates for clauses from last to first.
			return forPhraseBodyScope(typeInfo, node.Fors[0])
		}
	case *ast.FuncDecl:
		if scope == nil {
			scope = typeInfo.Scopes[node.Type]
		}
	case *ast.FuncLit:
		if scope == nil {
			scope = typeInfo.Scopes[node.Type]
			if scope == nil {
				scope = typeInfo.Scopes[node.Body]
			}
		}
	}
	if scope != nil && input != nil && input.Pos() <= pos && pos <= input.End() {
		return scope.Parent()
	}
	return scope
}

// forPhraseBodyScope includes variables declared by a comprehension filter's
// initializer. The compiler records the range scope on the clause and the
// filter scope on its declared objects, without recording the generated if node.
func forPhraseBodyScope(info *types.Info, clause *ast.ForPhrase) *gotypes.Scope {
	if init, ok := clause.Init.(*ast.AssignStmt); ok {
		for _, expr := range init.Lhs {
			if ident, ok := expr.(*ast.Ident); ok {
				if obj := info.Defs[ident]; obj != nil && obj.Parent() != nil {
					return obj.Parent()
				}
			}
		}
	}
	return info.Scopes[clause]
}
