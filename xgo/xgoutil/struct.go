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
)

// StructMember describes a resolved struct member and the selector type used
// to refer to it.
type StructMember struct {
	// Member is the field or method object yielded from the traversal.
	Member gotypes.Object
	// Selector is the named type used to select Member, or nil for a direct
	// field of an unnamed struct.
	Selector *gotypes.Named
}

// IsNamedStructType reports whether the given named type is a struct type.
func IsNamedStructType(named *gotypes.Named) bool {
	if named == nil {
		return false
	}
	_, ok := named.Underlying().(*gotypes.Struct)
	return ok
}

// IsXGoClassStructType reports whether the given named type is an XGo class struct type.
func IsXGoClassStructType(named *gotypes.Named) bool {
	if named == nil {
		return false
	}
	obj := named.Obj()
	if obj == nil {
		return false
	}
	pkg := obj.Pkg()
	if !IsMarkedAsXGoPackage(pkg) {
		return false
	}

	// FIXME: This is a workaround for the fact that XGo does not have the ability to
	// recognize XGo class struct types.
	switch PkgPath(pkg) + "." + obj.Name() {
	case "github.com/goplus/spx/v3.Game",
		"github.com/goplus/spx/v3.SpriteImpl":
		return true
	}

	return false
}

// StructMembers returns an iterator over exported or main-package struct fields
// and methods of a named or unnamed struct. It includes embedded members in
// XGo's depth-first lookup order and skips shadowed member names.
func StructMembers(typ gotypes.Type) iter.Seq[StructMember] {
	return func(yield func(StructMember) bool) {
		switch typ := typ.(type) {
		case *gotypes.Named:
			if !IsNamedStructType(typ) {
				return
			}
		case *gotypes.Struct:
		default:
			return
		}
		walked := make(map[gotypes.Type]struct{})
		seenMembers := make(map[string]struct{})
		var walk func(gotypes.Type, []*gotypes.Named) bool
		walk = func(typ gotypes.Type, namedPath []*gotypes.Named) bool {
			typ = gotypes.Unalias(DerefType(gotypes.Unalias(typ)))
			if _, ok := walked[typ]; ok {
				return true
			}
			walked[typ] = struct{}{}
			named, _ := typ.(*gotypes.Named)
			if named != nil {
				namedPath = append(namedPath, named)
			}

			selector := named
			for _, selectorNamed := range namedPath {
				if !IsExportedOrInMainPkg(selectorNamed.Obj()) {
					break
				}
				selector = selectorNamed
				if IsXGoClassStructType(selector) {
					break
				}
			}
			yieldMember := func(member gotypes.Object) bool {
				if _, ok := seenMembers[member.Name()]; ok || !IsExportedOrInMainPkg(member) {
					return true
				}
				seenMembers[member.Name()] = struct{}{}

				return yield(StructMember{Member: member, Selector: selector})
			}

			if st, ok := typ.Underlying().(*gotypes.Struct); ok {
				for field := range st.Fields() {
					if !yieldMember(field) {
						return false
					}
				}
			}
			if named != nil {
				for method := range named.Methods() {
					if !yieldMember(method) {
						return false
					}
				}
			}
			switch underlying := typ.Underlying().(type) {
			case *gotypes.Struct:
				for field := range underlying.Fields() {
					if field.Embedded() && !walk(field.Type(), namedPath) {
						return false
					}
				}
			case *gotypes.Interface:
				for method := range underlying.ExplicitMethods() {
					if !yieldMember(method) {
						return false
					}
				}
				for embedded := range underlying.EmbeddedTypes() {
					if !walk(embedded, namedPath) {
						return false
					}
				}
			}
			return true
		}
		walk(typ, nil)
	}
}
