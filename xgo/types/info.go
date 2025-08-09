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

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/x/typesutil"
)

// Info is an enhanced version of [typesutil.Info] for XGo projects. It embeds
// [typesutil.Info] and adds additional functionality and context.
type Info struct {
	typesutil.Info

	// Pkg is the package associated with this type information.
	Pkg *types.Package

	// ObjToDef is a reverse mapping for O(1) object-to-identifier lookup.
	// For identifiers that do not denote objects, the object is nil and
	// they are excluded from this mapping.
	ObjToDef map[types.Object]*ast.Ident
}

// RefIdentsFor returns all identifiers where the given object is referenced.
func (i *Info) RefIdentsFor(obj types.Object) []*ast.Ident {
	if obj == nil {
		return nil
	}
	var idents []*ast.Ident
	for ident, o := range i.Uses {
		if o == obj {
			idents = append(idents, ident)
		}
	}
	return idents
}
