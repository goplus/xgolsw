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

// DerefType returns the underlying type of t. For pointer types, it returns
// the element type that the pointer points to. For non-pointer types, it
// returns the type unchanged.
func DerefType(t types.Type) types.Type {
	if ptr, ok := t.(*types.Pointer); ok {
		return ptr.Elem()
	}
	return t
}

// IsValidType reports whether typ is non-nil and not the invalid type sentinel.
func IsValidType(typ types.Type) bool {
	return typ != nil && typ != types.Typ[types.Invalid]
}

// IsTypesCompatible reports whether two types are compatible.
func IsTypesCompatible(got, want types.Type) bool {
	if got == nil || want == nil {
		return false
	}

	if types.AssignableTo(got, want) {
		return true
	}

	switch want := want.(type) {
	case *types.Interface:
		return types.Implements(got, want)
	case *types.Pointer:
		if gotPtr, ok := got.(*types.Pointer); ok {
			return types.Identical(want.Elem(), gotPtr.Elem())
		}
		return types.Identical(got, want.Elem())
	case *types.Slice:
		gotSlice, ok := got.(*types.Slice)
		return ok && types.Identical(want.Elem(), gotSlice.Elem())
	case *types.Chan:
		gotCh, ok := got.(*types.Chan)
		return ok && types.Identical(want.Elem(), gotCh.Elem()) &&
			(want.Dir() == types.SendRecv || want.Dir() == gotCh.Dir())
	case *types.Signature:
		gotSig, ok := got.(*types.Signature)
		if !ok {
			return false
		}
		if want.Results().Len() != gotSig.Results().Len() {
			return false
		}
		if want.Results().Len() == 0 {
			return true
		}
		return IsTypesCompatible(gotSig.Results().At(0).Type(), want.Results().At(0).Type())
	}

	if gotSig, ok := got.(*types.Signature); ok {
		if gotSig.Results().Len() != 1 {
			return false
		}
		return IsTypesCompatible(gotSig.Results().At(0).Type(), want)
	}

	if _, ok := got.(*types.Named); ok {
		return types.Identical(got, want)
	}

	return false
}

// IsTypesConvertible reports whether a type can be explicitly converted to another.
func IsTypesConvertible(from, to types.Type) bool {
	if from == nil || to == nil {
		return false
	}

	if !types.ConvertibleTo(from, to) {
		return false
	}

	fromUnderlying := from.Underlying()
	toUnderlying := to.Underlying()

	fromBasic, fromIsBasic := fromUnderlying.(*types.Basic)
	toBasic, toIsBasic := toUnderlying.(*types.Basic)

	if fromIsBasic && toIsBasic {
		// Exclude numeric to string conversions.
		if (fromBasic.Info()&types.IsNumeric) != 0 && toBasic.Kind() == types.String {
			return false
		}
		// Exclude string to numeric conversions.
		if fromBasic.Kind() == types.String && (toBasic.Info()&types.IsNumeric) != 0 {
			return false
		}
		// Exclude bool to numeric or string conversions.
		if fromBasic.Kind() == types.Bool && toBasic.Kind() != types.Bool {
			return false
		}
		// Exclude numeric or string to bool conversions.
		if fromBasic.Kind() != types.Bool && toBasic.Kind() == types.Bool {
			return false
		}
	}
	return true
}
