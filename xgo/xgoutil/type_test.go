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
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDerefType(t *testing.T) {
	t.Run("PointerTypes", func(t *testing.T) {
		intPtr := types.NewPointer(types.Typ[types.Int])
		assert.Equal(t, types.Typ[types.Int], DerefType(intPtr))

		stringPtr := types.NewPointer(types.Typ[types.String])
		assert.Equal(t, types.Typ[types.String], DerefType(stringPtr))

		intPtrPtr := types.NewPointer(intPtr)
		assert.Equal(t, intPtr, DerefType(intPtrPtr))
	})

	t.Run("NonPointerTypes", func(t *testing.T) {
		assert.Equal(t, types.Typ[types.Int], DerefType(types.Typ[types.Int]))
		assert.Equal(t, types.Typ[types.String], DerefType(types.Typ[types.String]))
		assert.Equal(t, types.Typ[types.Bool], DerefType(types.Typ[types.Bool]))

		intSlice := types.NewSlice(types.Typ[types.Int])
		assert.Equal(t, intSlice, DerefType(intSlice))

		structType := types.NewStruct([]*types.Var{}, []string{})
		assert.Equal(t, structType, DerefType(structType))

		interfaceType := types.NewInterfaceType([]*types.Func{}, []types.Type{})
		assert.Equal(t, interfaceType, DerefType(interfaceType))
	})

	t.Run("ComplexTypes", func(t *testing.T) {
		structType := types.NewStruct([]*types.Var{types.NewField(0, nil, "field", types.Typ[types.String], false)}, []string{})
		structPtr := types.NewPointer(structType)
		assert.Equal(t, structType, DerefType(structPtr))

		sliceType := types.NewSlice(types.Typ[types.Int])
		slicePtr := types.NewPointer(sliceType)
		assert.Equal(t, sliceType, DerefType(slicePtr))

		chanType := types.NewChan(types.SendRecv, types.Typ[types.Int])
		chanPtr := types.NewPointer(chanType)
		assert.Equal(t, chanType, DerefType(chanPtr))
	})

	t.Run("NilType", func(t *testing.T) {
		got := DerefType(nil)
		assert.Nil(t, got)
	})
}

func TestIsValidType(t *testing.T) {
	t.Run("NilType", func(t *testing.T) {
		assert.False(t, IsValidType(nil))
	})

	t.Run("InvalidTypeSentinel", func(t *testing.T) {
		assert.False(t, IsValidType(types.Typ[types.Invalid]))
	})

	t.Run("NamedType", func(t *testing.T) {
		named := types.NewNamed(types.NewTypeName(0, nil, "MyInt", nil), types.Typ[types.Int], nil)
		assert.True(t, IsValidType(named))
	})

	t.Run("BasicTypes", func(t *testing.T) {
		assert.True(t, IsValidType(types.Typ[types.Int]))
		assert.True(t, IsValidType(types.Typ[types.String]))
		assert.True(t, IsValidType(types.Typ[types.Bool]))
	})

	t.Run("CompositeTypes", func(t *testing.T) {
		assert.True(t, IsValidType(types.NewSlice(types.Typ[types.String])))
		assert.True(t, IsValidType(types.NewPointer(types.Typ[types.Int])))
		assert.True(t, IsValidType(types.NewStruct(nil, nil)))
	})
}

func TestIsTypesCompatible(t *testing.T) {
	t.Run("NilTypes", func(t *testing.T) {
		assert.False(t, IsTypesCompatible(nil, nil))
		assert.False(t, IsTypesCompatible(types.Typ[types.Int], nil))
		assert.False(t, IsTypesCompatible(nil, types.Typ[types.Int]))
	})

	t.Run("AssignableTypes", func(t *testing.T) {
		assert.True(t, IsTypesCompatible(types.Typ[types.Int], types.Typ[types.Int]))
		assert.True(t, IsTypesCompatible(types.Typ[types.String], types.Typ[types.String]))

		// Untyped constants are assignable to typed values.
		assert.True(t, IsTypesCompatible(types.Typ[types.UntypedInt], types.Typ[types.Int]))
		assert.True(t, IsTypesCompatible(types.Typ[types.UntypedString], types.Typ[types.String]))
	})

	t.Run("InterfaceTypes", func(t *testing.T) {
		stringMethod := types.NewFunc(0, nil, "String", types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(types.NewVar(0, nil, "", types.Typ[types.String])), false))
		stringerIface := types.NewInterfaceType([]*types.Func{stringMethod}, nil)

		structType := types.NewStruct([]*types.Var{}, []string{})
		namedStruct := types.NewNamed(types.NewTypeName(0, nil, "TestStruct", nil), structType, nil)
		namedStruct.AddMethod(stringMethod)

		assert.True(t, IsTypesCompatible(namedStruct, stringerIface))
		assert.False(t, IsTypesCompatible(types.Typ[types.Int], stringerIface))
	})

	t.Run("PointerTypes", func(t *testing.T) {
		intPtr := types.NewPointer(types.Typ[types.Int])
		stringPtr := types.NewPointer(types.Typ[types.String])

		// Both pointers with same element type.
		assert.True(t, IsTypesCompatible(intPtr, intPtr))

		// Different pointer element types.
		assert.False(t, IsTypesCompatible(intPtr, stringPtr))

		// Non-pointer to pointer with same element type.
		assert.True(t, IsTypesCompatible(types.Typ[types.Int], intPtr))

		// Non-pointer to pointer with different element type.
		assert.False(t, IsTypesCompatible(types.Typ[types.String], intPtr))
	})

	t.Run("SliceTypes", func(t *testing.T) {
		intSlice := types.NewSlice(types.Typ[types.Int])
		stringSlice := types.NewSlice(types.Typ[types.String])

		// Same slice element types.
		assert.True(t, IsTypesCompatible(intSlice, intSlice))

		// Different slice element types.
		assert.False(t, IsTypesCompatible(intSlice, stringSlice))

		// Non-slice to slice.
		assert.False(t, IsTypesCompatible(types.Typ[types.Int], intSlice))
	})

	t.Run("ChannelTypes", func(t *testing.T) {
		intChanSendRecv := types.NewChan(types.SendRecv, types.Typ[types.Int])
		intChanSend := types.NewChan(types.SendOnly, types.Typ[types.Int])
		intChanRecv := types.NewChan(types.RecvOnly, types.Typ[types.Int])
		stringChanSendRecv := types.NewChan(types.SendRecv, types.Typ[types.String])

		// Compatible channel directions.
		assert.True(t, IsTypesCompatible(intChanSendRecv, intChanSendRecv))
		assert.True(t, IsTypesCompatible(intChanSend, intChanSend))
		assert.True(t, IsTypesCompatible(intChanRecv, intChanRecv))

		// SendRecv channels are assignable to unidirectional channels.
		assert.True(t, IsTypesCompatible(intChanSendRecv, intChanSend))
		assert.True(t, IsTypesCompatible(intChanSendRecv, intChanRecv))

		// SendRecv want accepts any direction.
		assert.True(t, IsTypesCompatible(intChanSend, intChanSendRecv))
		assert.True(t, IsTypesCompatible(intChanRecv, intChanSendRecv))

		// Incompatible channel directions (unidirectional to different unidirectional).
		assert.False(t, IsTypesCompatible(intChanSend, intChanRecv))
		assert.False(t, IsTypesCompatible(intChanRecv, intChanSend))

		// Different channel element types.
		assert.False(t, IsTypesCompatible(intChanSendRecv, stringChanSendRecv))

		// Non-channel to channel.
		assert.False(t, IsTypesCompatible(types.Typ[types.Int], intChanSendRecv))
	})

	t.Run("SignatureTypes", func(t *testing.T) {
		noResultWant := types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(), false)
		noResultGot := types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(), false)
		assert.True(t, IsTypesCompatible(noResultGot, noResultWant))

		intResultVar := types.NewVar(0, nil, "", types.Typ[types.Int])
		intResultSig := types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(intResultVar), false)
		otherIntResultSig := types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(types.NewVar(0, nil, "", types.Typ[types.Int])), false)
		assert.True(t, IsTypesCompatible(otherIntResultSig, intResultSig))

		stringResultSig := types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(types.NewVar(0, nil, "", types.Typ[types.String])), false)
		assert.False(t, IsTypesCompatible(otherIntResultSig, stringResultSig))

		twoResultsSig := types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(
			types.NewVar(0, nil, "", types.Typ[types.Int]),
			types.NewVar(0, nil, "", types.Typ[types.Int]),
		), false)
		assert.False(t, IsTypesCompatible(twoResultsSig, intResultSig))

		ptrToInt := types.NewPointer(types.Typ[types.Int])
		ptrResultSig := types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(types.NewVar(0, nil, "", ptrToInt)), false)
		ptrWantSig := types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(types.NewVar(0, nil, "", ptrToInt)), false)
		assert.True(t, IsTypesCompatible(ptrResultSig, ptrWantSig))

		ptrToString := types.NewPointer(types.Typ[types.String])
		ptrStringSig := types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(types.NewVar(0, nil, "", ptrToString)), false)
		assert.False(t, IsTypesCompatible(ptrResultSig, ptrStringSig))

		assert.True(t, IsTypesCompatible(otherIntResultSig, types.Typ[types.Int]))
		assert.False(t, IsTypesCompatible(twoResultsSig, types.Typ[types.Int]))
		assert.False(t, IsTypesCompatible(stringResultSig, types.Typ[types.Int]))
	})

	t.Run("NamedTypes", func(t *testing.T) {
		namedInt1 := types.NewNamed(types.NewTypeName(0, nil, "MyInt1", nil), types.Typ[types.Int], nil)
		namedInt2 := types.NewNamed(types.NewTypeName(0, nil, "MyInt2", nil), types.Typ[types.Int], nil)

		// Same named type.
		assert.True(t, IsTypesCompatible(namedInt1, namedInt1))

		// Different named types with same underlying type.
		assert.False(t, IsTypesCompatible(namedInt1, namedInt2))

		// Named type to underlying type.
		assert.False(t, IsTypesCompatible(namedInt1, types.Typ[types.Int]))
		assert.False(t, IsTypesCompatible(types.Typ[types.Int], namedInt1))
	})

	t.Run("TypeConversions", func(t *testing.T) {
		// Named types are convertible to their underlying types.
		userIDType := types.NewNamed(types.NewTypeName(0, nil, "UserID", nil), types.Typ[types.Int], nil)
		orderIDType := types.NewNamed(types.NewTypeName(0, nil, "OrderID", nil), types.Typ[types.Int], nil)

		// Named int types are NOT compatible with int (need conversion).
		assert.False(t, IsTypesCompatible(userIDType, types.Typ[types.Int]))
		assert.False(t, IsTypesCompatible(orderIDType, types.Typ[types.Int]))

		// Named string type is NOT compatible with string.
		namedString := types.NewNamed(types.NewTypeName(0, nil, "MyString", nil), types.Typ[types.String], nil)
		assert.False(t, IsTypesCompatible(namedString, types.Typ[types.String]))

		// But int to string conversions are excluded (impractical).
		assert.False(t, IsTypesCompatible(types.Typ[types.Int], types.Typ[types.String]))
		assert.False(t, IsTypesCompatible(types.Typ[types.String], types.Typ[types.Int]))

		// Bool conversions are excluded.
		assert.False(t, IsTypesCompatible(types.Typ[types.Bool], types.Typ[types.Int]))
		assert.False(t, IsTypesCompatible(types.Typ[types.Int], types.Typ[types.Bool]))
		assert.False(t, IsTypesCompatible(types.Typ[types.Bool], types.Typ[types.String]))
		assert.False(t, IsTypesCompatible(types.Typ[types.String], types.Typ[types.Bool]))

		// Numeric conversions within same category are NOT compatible (need conversion).
		assert.False(t, IsTypesCompatible(types.Typ[types.Int32], types.Typ[types.Int64]))
		assert.False(t, IsTypesCompatible(types.Typ[types.Float32], types.Typ[types.Float64]))
		assert.False(t, IsTypesCompatible(types.Typ[types.Int], types.Typ[types.Float64]))
	})

	t.Run("IncompatibleTypes", func(t *testing.T) {
		// Basic incompatible types.
		assert.False(t, IsTypesCompatible(types.Typ[types.Int], types.Typ[types.String]))
		assert.False(t, IsTypesCompatible(types.Typ[types.Bool], types.Typ[types.Float64]))

		// Complex incompatible types.
		intSlice := types.NewSlice(types.Typ[types.Int])
		stringPtr := types.NewPointer(types.Typ[types.String])
		assert.False(t, IsTypesCompatible(intSlice, stringPtr))
	})
}

func TestIsTypesConvertible(t *testing.T) {
	t.Run("AllowedConversions", func(t *testing.T) {
		// Named types to underlying types.
		namedInt := types.NewNamed(types.NewTypeName(0, nil, "MyInt", nil), types.Typ[types.Int], nil)
		assert.True(t, IsTypesConvertible(namedInt, types.Typ[types.Int]))
		assert.True(t, IsTypesConvertible(types.Typ[types.Int], namedInt))

		// Numeric conversions within same category.
		assert.True(t, IsTypesConvertible(types.Typ[types.Int32], types.Typ[types.Int64]))
		assert.True(t, IsTypesConvertible(types.Typ[types.Int], types.Typ[types.Int32]))
		assert.True(t, IsTypesConvertible(types.Typ[types.Float32], types.Typ[types.Float64]))

		// Numeric conversions between int and float.
		assert.True(t, IsTypesConvertible(types.Typ[types.Int], types.Typ[types.Float64]))
		assert.True(t, IsTypesConvertible(types.Typ[types.Float32], types.Typ[types.Int]))

		// Untyped constants.
		assert.True(t, IsTypesConvertible(types.Typ[types.UntypedInt], types.Typ[types.Int]))
		assert.True(t, IsTypesConvertible(types.Typ[types.UntypedFloat], types.Typ[types.Float64]))
	})

	t.Run("ExcludedConversions", func(t *testing.T) {
		// Numeric to string conversions.
		assert.False(t, IsTypesConvertible(types.Typ[types.Int], types.Typ[types.String]))
		assert.False(t, IsTypesConvertible(types.Typ[types.Int32], types.Typ[types.String]))
		assert.False(t, IsTypesConvertible(types.Typ[types.Float64], types.Typ[types.String]))

		// String to numeric conversions.
		assert.False(t, IsTypesConvertible(types.Typ[types.String], types.Typ[types.Int]))
		assert.False(t, IsTypesConvertible(types.Typ[types.String], types.Typ[types.Float64]))

		// Bool to numeric conversions.
		assert.False(t, IsTypesConvertible(types.Typ[types.Bool], types.Typ[types.Int]))
		assert.False(t, IsTypesConvertible(types.Typ[types.Bool], types.Typ[types.Float64]))

		// Numeric to bool conversions.
		assert.False(t, IsTypesConvertible(types.Typ[types.Int], types.Typ[types.Bool]))
		assert.False(t, IsTypesConvertible(types.Typ[types.Float64], types.Typ[types.Bool]))

		// Bool to string conversions.
		assert.False(t, IsTypesConvertible(types.Typ[types.Bool], types.Typ[types.String]))

		// String to bool conversions.
		assert.False(t, IsTypesConvertible(types.Typ[types.String], types.Typ[types.Bool]))
	})

	t.Run("NonConvertibleTypes", func(t *testing.T) {
		// Types that are not convertible at all.
		intSlice := types.NewSlice(types.Typ[types.Int])
		stringSlice := types.NewSlice(types.Typ[types.String])
		assert.False(t, IsTypesConvertible(intSlice, stringSlice))

		structType1 := types.NewStruct([]*types.Var{types.NewField(0, nil, "a", types.Typ[types.Int], false)}, []string{})
		structType2 := types.NewStruct([]*types.Var{types.NewField(0, nil, "b", types.Typ[types.String], false)}, []string{})
		assert.False(t, IsTypesConvertible(structType1, structType2))
	})
}
