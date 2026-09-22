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
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo/types"
)

// DerefType returns the underlying type of t. For pointer types, it returns
// the element type that the pointer points to. For non-pointer types, it
// returns the type unchanged.
func DerefType(t gotypes.Type) gotypes.Type {
	if ptr, ok := t.(*gotypes.Pointer); ok {
		return ptr.Elem()
	}
	return t
}

// IsValidType reports whether typ is non-nil and not the invalid type sentinel.
func IsValidType(typ gotypes.Type) bool {
	return typ != nil && typ != gotypes.Typ[gotypes.Invalid]
}

// IsTypeExpr reports whether expr denotes a type. The second result is false
// when the recorded type information does not determine this.
func IsTypeExpr(typeInfo *types.Info, expr ast.Expr) (isType, known bool) {
	expr = astutil.Unparen(expr)
	var ident *ast.Ident
	switch expr := expr.(type) {
	// The recorder can mark pointer and generic type expressions as values.
	// Their operands still identify whether the expression denotes a type.
	case *ast.StarExpr:
		return IsTypeExpr(typeInfo, expr.X)
	case *ast.IndexExpr:
		return IsTypeExpr(typeInfo, expr.X)
	case *ast.IndexListExpr:
		return IsTypeExpr(typeInfo, expr.X)
	case *ast.Ident:
		ident = expr
	case *ast.SelectorExpr:
		ident = expr.Sel
	}
	obj := typeInfo.ObjectOf(ident)
	if _, ok := obj.(*gotypes.TypeName); ok {
		return true, true
	}
	if tv, ok := typeInfo.Types[expr]; ok {
		return tv.IsType(), true
	}
	return false, obj != nil
}

// IsTypesCompatible reports whether two types are compatible.
func IsTypesCompatible(got, want gotypes.Type) bool {
	if got == nil || want == nil {
		return false
	}

	if gotypes.AssignableTo(got, want) {
		return true
	}

	switch want := want.(type) {
	case *gotypes.TypeParam:
		constraint := want.Constraint().Underlying().(*gotypes.Interface)
		return gotypes.Satisfies(got, constraint)
	case *gotypes.Interface:
		return gotypes.Implements(got, want)
	case *gotypes.Pointer:
		if gotPtr, ok := got.(*gotypes.Pointer); ok {
			return gotypes.Identical(want.Elem(), gotPtr.Elem())
		}
		return gotypes.Identical(got, want.Elem())
	case *gotypes.Slice:
		gotSlice, ok := got.(*gotypes.Slice)
		return ok && gotypes.Identical(want.Elem(), gotSlice.Elem())
	case *gotypes.Chan:
		gotCh, ok := got.(*gotypes.Chan)
		return ok && gotypes.Identical(want.Elem(), gotCh.Elem()) &&
			(want.Dir() == gotypes.SendRecv || want.Dir() == gotCh.Dir())
	case *gotypes.Signature:
		gotSig, ok := got.(*gotypes.Signature)
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

	if gotSig, ok := got.(*gotypes.Signature); ok {
		if gotSig.Results().Len() != 1 {
			return false
		}
		return IsTypesCompatible(gotSig.Results().At(0).Type(), want)
	}

	if _, ok := got.(*gotypes.Named); ok {
		return gotypes.Identical(got, want)
	}

	return false
}

// IsTypesConvertible reports whether a type can be explicitly converted to another.
func IsTypesConvertible(from, to gotypes.Type) bool {
	if from == nil || to == nil {
		return false
	}

	if !gotypes.ConvertibleTo(from, to) {
		return false
	}

	fromUnderlying := from.Underlying()
	toUnderlying := to.Underlying()

	fromBasic, fromIsBasic := fromUnderlying.(*gotypes.Basic)
	toBasic, toIsBasic := toUnderlying.(*gotypes.Basic)

	if fromIsBasic && toIsBasic {
		// Exclude numeric to string conversions.
		if (fromBasic.Info()&gotypes.IsNumeric) != 0 && toBasic.Kind() == gotypes.String {
			return false
		}
	}
	return true
}
