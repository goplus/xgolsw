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

import "go/types"

// IsNamedStructType reports whether the given named type is a struct type.
func IsNamedStructType(named *types.Named) bool {
	if named == nil {
		return false
	}
	_, ok := named.Underlying().(*types.Struct)
	return ok
}

// WalkStruct walks a struct and calls the given onMember for each field and
// method. If onMember returns false, the walk is stopped.
func WalkStruct(named *types.Named, onMember func(member types.Object, selector *types.Named) bool) {
	if named == nil {
		return
	}
	selector := named
	walked := make(map[*types.Named]struct{})
	seenMembers := make(map[string]struct{})
	var walk func(named *types.Named) bool
	walk = func(named *types.Named) bool {
		if _, ok := walked[named]; ok {
			return true
		}
		walked[named] = struct{}{}

		st, ok := named.Underlying().(*types.Struct)
		if !ok {
			return true
		}

		for field := range st.Fields() {
			if _, ok := seenMembers[field.Name()]; ok || !IsExportedOrInMainPkg(field) {
				continue
			}
			seenMembers[field.Name()] = struct{}{}

			if !onMember(field, selector) {
				return false
			}
		}
		for method := range named.Methods() {
			if _, ok := seenMembers[method.Name()]; ok || !IsExportedOrInMainPkg(method) {
				continue
			}
			seenMembers[method.Name()] = struct{}{}

			if !onMember(method, selector) {
				return false
			}
		}
		for field := range st.Fields() {
			if !field.Embedded() {
				continue
			}
			fieldType := DerefType(field.Type())
			namedField, ok := fieldType.(*types.Named)
			if !ok || !IsNamedStructType(namedField) {
				continue
			}

			if !walk(namedField) {
				return false
			}
		}
		return true
	}
	walk(named)
}
