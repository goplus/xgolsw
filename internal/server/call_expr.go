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

	"github.com/goplus/xgo/ast"
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
