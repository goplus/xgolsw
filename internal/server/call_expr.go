/*
 * Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
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

package server

import (
	gotypes "go/types"
	"iter"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// callExprFromNode returns the call expression represented by node.
func callExprFromNode(typeInfo *types.Info, node ast.Node) *ast.CallExpr {
	switch node := node.(type) {
	case *ast.CallExpr:
		return node
	case *ast.FuncDecorator:
		return &node.CallExpr
	case *ast.BranchStmt:
		return xgoutil.CreateCallExprFromBranchStmt(typeInfo, node)
	default:
		return nil
	}
}

// funcDecoratorParams returns the source-visible parameters of a valid
// decorator function signature.
func funcDecoratorParams(sig *gotypes.Signature) (*gotypes.Tuple, bool) {
	params := sig.Params()
	if params.Len() == 0 {
		return nil, false
	}

	fn, ok := params.At(params.Len() - 1).Type().(*gotypes.Signature)
	if !ok || fn.Params().Len() != 0 {
		return nil, false
	}
	switch fn.Results().Len() {
	case 0:
	case 1:
		errorType := gotypes.Universe.Lookup("error").Type()
		if fn.Results().At(0).Type() != errorType {
			return nil, false
		}
	default:
		return nil, false
	}

	visible := make([]*gotypes.Var, params.Len()-1)
	for i := range visible {
		visible[i] = params.At(i)
	}
	return gotypes.NewTuple(visible...), true
}

// callArgValueTypes yields call argument values and their resolved target types,
// including elements of XGo collection and tuple literals.
func callArgValueTypes(typeInfo *types.Info, call *ast.CallExpr) iter.Seq2[ast.Expr, gotypes.Type] {
	return func(yield func(ast.Expr, gotypes.Type) bool) {
		for expr, typ := range builtinArgValueTypes(typeInfo, call) {
			if !yield(astutil.Unparen(expr), typ) {
				return
			}
		}
		for arg := range resolvedCallExprArgs(typeInfo, call) {
			for expr, typ := range valueElementTypes(typeInfo, arg.Arg, arg.ExpectedType) {
				if !yield(astutil.Unparen(expr), typ) {
					return
				}
			}
		}
	}
}

// expectedTypesForCallArg returns expected types when target is a direct
// call argument.
func expectedTypesForCallArg(
	typeInfo *types.Info,
	call *ast.CallExpr,
	target ast.Expr,
) ([]gotypes.Type, bool) {
	expected := builtinArgTypes(typeInfo, call, xgoutil.SourceExprIndex(call.Args, target))

	if arg, ok := xgoutil.ResolveCallExprArg(typeInfo, call, target); ok {
		expected = append(expected, validExpectedType(arg.ExpectedType)...)
	} else if _, sig, _ := xgoutil.ResolveCallExprSignature(typeInfo, call); sig == nil {
		for _, overload := range callExprFuncOverloads(typeInfo, call) {
			if typ, matches := matchOverloadCallExprArg(typeInfo, call, overload, target, -1); matches {
				expected = append(expected, validExpectedType(typ)...)
			}
		}
	}
	allowConversion := false
	if len(call.Args) == 1 && call.Args[0] == target {
		if tv, ok := typeInfo.Types[call.Fun]; ok && tv.IsType() && xgoutil.IsValidType(tv.Type) {
			expected = append(expected, gotypes.Unalias(tv.Type))
			allowConversion = true
		}
	}
	return deduplicateTypes(expected), allowConversion
}
