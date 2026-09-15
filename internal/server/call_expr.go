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
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// callExprFromNode returns the call expression represented by node.
func callExprFromNode(node ast.Node) *ast.CallExpr {
	switch node := node.(type) {
	case *ast.CallExpr:
		return node
	case *ast.FuncDecorator:
		return &node.CallExpr
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

// callArgValueTypes yields call argument values and their resolved target
// types. For XGo slice and matrix literals, it yields elements with the target
// slice's element type. Parentheses do not change the value or its target type.
func callArgValueTypes(typeInfo *types.Info, call *ast.CallExpr) iter.Seq2[ast.Expr, gotypes.Type] {
	return func(yield func(ast.Expr, gotypes.Type) bool) {
		for arg := range resolvedCallExprArgs(typeInfo, call) {
			if arg.ExpectedType == nil {
				continue
			}
			expr := astutil.Unparen(arg.Arg)
			typ := xgoutil.DerefType(arg.ExpectedType)
			var elts []ast.Expr
			switch expr := expr.(type) {
			case *ast.SliceLit:
				elts = expr.Elts
			case *ast.MatrixLit:
				elts = slices.Concat(expr.Elts...)
			default:
				if !yield(expr, typ) {
					return
				}
				continue
			}

			slice, ok := typ.Underlying().(*gotypes.Slice)
			if !ok {
				continue
			}
			elemType := xgoutil.DerefType(slice.Elem())
			for _, elt := range elts {
				if !yield(astutil.Unparen(elt), elemType) {
					return
				}
			}
		}
	}
}
