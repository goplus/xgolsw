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
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/x/typesutil"
)

// Info is an enhanced version of [typesutil.Info] for XGo projects. It embeds
// [typesutil.Info] and adds additional functionality and context.
type Info struct {
	typesutil.Info

	// Pkg is the package associated with this type information.
	Pkg *gotypes.Package

	// ObjToDef is a reverse mapping for O(1) object-to-identifier lookup.
	// For identifiers that do not denote objects, the object is nil and
	// they are excluded from this mapping.
	ObjToDef map[gotypes.Object]*ast.Ident

	// FuncDecorators distinguishes decorator calls from ordinary calls, which
	// have different argument expansion rules.
	FuncDecorators map[*ast.CallExpr]bool
}

// RefIdentsFor returns all identifiers where the given object is referenced,
// including uses through different generic instantiations and type switch cases.
func (i *Info) RefIdentsFor(obj gotypes.Object) []*ast.Ident {
	if obj == nil {
		return nil
	}
	obj = i.ObjectDeclaration(obj)
	def := i.ObjToDef[obj]
	var idents []*ast.Ident
	for ident, used := range i.Uses {
		if overload := i.Overloads[ident]; overload != nil {
			used = overload
		}
		// Range expressions reuse the declaration identifier in generated uses.
		if ident != def && i.ObjectDeclaration(used) == obj {
			idents = append(idents, ident)
		}
	}
	return idents
}

// SourceObjectOf returns the object named by ident before overload selection.
// ObjectOf still provides the selected implementation for typed navigation.
func (i *Info) SourceObjectOf(ident *ast.Ident) gotypes.Object {
	if overload := i.Overloads[ident]; overload != nil {
		return overload
	}
	return i.ObjectOf(ident)
}

// ObjectDeclaration returns the object at the shared declaration of obj,
// including generic members and the branch variables of a type switch.
// Objects without a recorded declaration, including nil, retain their origin.
func (i *Info) ObjectDeclaration(obj gotypes.Object) gotypes.Object {
	obj = ObjectOrigin(obj)
	if ident := i.ObjToDef[obj]; ident != nil {
		if declaration := i.Defs[ident]; declaration != nil {
			return declaration
		}
	}
	return obj
}

// ObjectOrigin returns the declaration of a field or method before generic
// instantiation. Other objects, including nil, are returned unchanged.
func ObjectOrigin(obj gotypes.Object) gotypes.Object {
	switch obj := obj.(type) {
	case *gotypes.Var:
		return obj.Origin()
	case *gotypes.Func:
		return obj.Origin()
	default:
		return obj
	}
}
