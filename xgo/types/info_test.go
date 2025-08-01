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

package types

import (
	"go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInfoRefIdentsFor(t *testing.T) {
	xVar := types.NewVar(0, nil, "x", types.Typ[types.Int])
	yVar := types.NewVar(0, nil, "y", types.Typ[types.Int])

	xDef := &ast.Ident{Name: "x"}
	xUse1 := &ast.Ident{Name: "x"}
	xUse2 := &ast.Ident{Name: "x"}
	xUse3 := &ast.Ident{Name: "x"}
	yDef := &ast.Ident{Name: "y"}
	yUse := &ast.Ident{Name: "y"}

	info := &Info{
		Info: typesutil.Info{
			Defs: map[*ast.Ident]types.Object{
				xDef: xVar,
				yDef: yVar,
			},
			Uses: map[*ast.Ident]types.Object{
				xUse1: xVar,
				xUse2: xVar,
				xUse3: xVar,
				yUse:  yVar,
			},
		},
		Pkg: types.NewPackage("test", "test"),
		ObjToDef: map[types.Object]*ast.Ident{
			xVar: xDef,
			yVar: yDef,
		},
	}

	t.Run("FindReferences", func(t *testing.T) {
		refs := info.RefIdentsFor(xVar)
		require.Len(t, refs, 3)
		for _, ref := range refs {
			assert.Equal(t, "x", ref.Name)
			assert.Contains(t, []*ast.Ident{xUse1, xUse2, xUse3}, ref)
		}

		refs = info.RefIdentsFor(yVar)
		require.Len(t, refs, 1)
		assert.Equal(t, yUse, refs[0])
	})

	t.Run("NoReferences", func(t *testing.T) {
		zVar := types.NewVar(0, nil, "z", types.Typ[types.Int])
		info.ObjToDef[zVar] = &ast.Ident{Name: "z"}

		refs := info.RefIdentsFor(zVar)
		assert.Empty(t, refs)
	})

	t.Run("NilObject", func(t *testing.T) {
		assert.Nil(t, info.RefIdentsFor(nil))
	})

	t.Run("UnknownObject", func(t *testing.T) {
		unknownObj := types.NewVar(0, nil, "unknown", types.Typ[types.Int])

		refs := info.RefIdentsFor(unknownObj)
		assert.Empty(t, refs)
	})
}
