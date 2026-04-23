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
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDerefType(t *testing.T) {
	t.Run("PointerTypes", func(t *testing.T) {
		intPtr := gotypes.NewPointer(gotypes.Typ[gotypes.Int])
		assert.Equal(t, gotypes.Typ[gotypes.Int], DerefType(intPtr))

		stringPtr := gotypes.NewPointer(gotypes.Typ[gotypes.String])
		assert.Equal(t, gotypes.Typ[gotypes.String], DerefType(stringPtr))

		intPtrPtr := gotypes.NewPointer(intPtr)
		assert.Equal(t, intPtr, DerefType(intPtrPtr))
	})

	t.Run("NonPointerTypes", func(t *testing.T) {
		assert.Equal(t, gotypes.Typ[gotypes.Int], DerefType(gotypes.Typ[gotypes.Int]))
		assert.Equal(t, gotypes.Typ[gotypes.String], DerefType(gotypes.Typ[gotypes.String]))
		assert.Equal(t, gotypes.Typ[gotypes.Bool], DerefType(gotypes.Typ[gotypes.Bool]))

		intSlice := gotypes.NewSlice(gotypes.Typ[gotypes.Int])
		assert.Equal(t, intSlice, DerefType(intSlice))

		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		assert.Equal(t, structType, DerefType(structType))

		interfaceType := gotypes.NewInterfaceType([]*gotypes.Func{}, []gotypes.Type{})
		assert.Equal(t, interfaceType, DerefType(interfaceType))
	})

	t.Run("ComplexTypes", func(t *testing.T) {
		structType := gotypes.NewStruct([]*gotypes.Var{gotypes.NewField(0, nil, "field", gotypes.Typ[gotypes.String], false)}, []string{})
		structPtr := gotypes.NewPointer(structType)
		assert.Equal(t, structType, DerefType(structPtr))

		sliceType := gotypes.NewSlice(gotypes.Typ[gotypes.Int])
		slicePtr := gotypes.NewPointer(sliceType)
		assert.Equal(t, sliceType, DerefType(slicePtr))

		chanType := gotypes.NewChan(gotypes.SendRecv, gotypes.Typ[gotypes.Int])
		chanPtr := gotypes.NewPointer(chanType)
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
		assert.False(t, IsValidType(gotypes.Typ[gotypes.Invalid]))
	})

	t.Run("NamedType", func(t *testing.T) {
		named := gotypes.NewNamed(gotypes.NewTypeName(0, nil, "MyInt", nil), gotypes.Typ[gotypes.Int], nil)
		assert.True(t, IsValidType(named))
	})

	t.Run("BasicTypes", func(t *testing.T) {
		assert.True(t, IsValidType(gotypes.Typ[gotypes.Int]))
		assert.True(t, IsValidType(gotypes.Typ[gotypes.String]))
		assert.True(t, IsValidType(gotypes.Typ[gotypes.Bool]))
	})

	t.Run("CompositeTypes", func(t *testing.T) {
		assert.True(t, IsValidType(gotypes.NewSlice(gotypes.Typ[gotypes.String])))
		assert.True(t, IsValidType(gotypes.NewPointer(gotypes.Typ[gotypes.Int])))
		assert.True(t, IsValidType(gotypes.NewStruct(nil, nil)))
	})
}

func TestIsTypesCompatible(t *testing.T) {
	t.Run("NilTypes", func(t *testing.T) {
		assert.False(t, IsTypesCompatible(nil, nil))
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Int], nil))
		assert.False(t, IsTypesCompatible(nil, gotypes.Typ[gotypes.Int]))
	})

	t.Run("AssignableTypes", func(t *testing.T) {
		assert.True(t, IsTypesCompatible(gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.Int]))
		assert.True(t, IsTypesCompatible(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.String]))

		// Untyped constants are assignable to typed values.
		assert.True(t, IsTypesCompatible(gotypes.Typ[gotypes.UntypedInt], gotypes.Typ[gotypes.Int]))
		assert.True(t, IsTypesCompatible(gotypes.Typ[gotypes.UntypedString], gotypes.Typ[gotypes.String]))
	})

	t.Run("InterfaceTypes", func(t *testing.T) {
		stringMethod := gotypes.NewFunc(0, nil, "String", gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(gotypes.NewVar(0, nil, "", gotypes.Typ[gotypes.String])), false))
		stringerIface := gotypes.NewInterfaceType([]*gotypes.Func{stringMethod}, nil)

		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		namedStruct := gotypes.NewNamed(gotypes.NewTypeName(0, nil, "TestStruct", nil), structType, nil)
		namedStruct.AddMethod(stringMethod)

		assert.True(t, IsTypesCompatible(namedStruct, stringerIface))
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Int], stringerIface))
	})

	t.Run("PointerTypes", func(t *testing.T) {
		intPtr := gotypes.NewPointer(gotypes.Typ[gotypes.Int])
		stringPtr := gotypes.NewPointer(gotypes.Typ[gotypes.String])

		// Both pointers with same element type.
		assert.True(t, IsTypesCompatible(intPtr, intPtr))

		// Different pointer element types.
		assert.False(t, IsTypesCompatible(intPtr, stringPtr))

		// Non-pointer to pointer with same element type.
		assert.True(t, IsTypesCompatible(gotypes.Typ[gotypes.Int], intPtr))

		// Non-pointer to pointer with different element type.
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.String], intPtr))
	})

	t.Run("SliceTypes", func(t *testing.T) {
		intSlice := gotypes.NewSlice(gotypes.Typ[gotypes.Int])
		stringSlice := gotypes.NewSlice(gotypes.Typ[gotypes.String])

		// Same slice element types.
		assert.True(t, IsTypesCompatible(intSlice, intSlice))

		// Different slice element types.
		assert.False(t, IsTypesCompatible(intSlice, stringSlice))

		// Non-slice to slice.
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Int], intSlice))
	})

	t.Run("ChannelTypes", func(t *testing.T) {
		intChanSendRecv := gotypes.NewChan(gotypes.SendRecv, gotypes.Typ[gotypes.Int])
		intChanSend := gotypes.NewChan(gotypes.SendOnly, gotypes.Typ[gotypes.Int])
		intChanRecv := gotypes.NewChan(gotypes.RecvOnly, gotypes.Typ[gotypes.Int])
		stringChanSendRecv := gotypes.NewChan(gotypes.SendRecv, gotypes.Typ[gotypes.String])

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
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Int], intChanSendRecv))
	})

	t.Run("SignatureTypes", func(t *testing.T) {
		noResultWant := gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(), false)
		noResultGot := gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(), false)
		assert.True(t, IsTypesCompatible(noResultGot, noResultWant))

		noResultIntParamSig := gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewVar(0, nil, "", gotypes.Typ[gotypes.Int])),
			gotypes.NewTuple(),
			false,
		)
		noResultStringParamSig := gotypes.NewSignatureType(
			nil,
			nil,
			nil,
			gotypes.NewTuple(gotypes.NewVar(0, nil, "", gotypes.Typ[gotypes.String])),
			gotypes.NewTuple(),
			false,
		)
		assert.True(t, IsTypesCompatible(noResultIntParamSig, noResultStringParamSig))
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Int], noResultWant))

		intResultVar := gotypes.NewVar(0, nil, "", gotypes.Typ[gotypes.Int])
		intResultSig := gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(intResultVar), false)
		otherIntResultSig := gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(gotypes.NewVar(0, nil, "", gotypes.Typ[gotypes.Int])), false)
		assert.True(t, IsTypesCompatible(otherIntResultSig, intResultSig))

		stringResultSig := gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(gotypes.NewVar(0, nil, "", gotypes.Typ[gotypes.String])), false)
		assert.False(t, IsTypesCompatible(otherIntResultSig, stringResultSig))

		twoResultsSig := gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(
			gotypes.NewVar(0, nil, "", gotypes.Typ[gotypes.Int]),
			gotypes.NewVar(0, nil, "", gotypes.Typ[gotypes.Int]),
		), false)
		assert.False(t, IsTypesCompatible(twoResultsSig, intResultSig))

		ptrToInt := gotypes.NewPointer(gotypes.Typ[gotypes.Int])
		ptrResultSig := gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(gotypes.NewVar(0, nil, "", ptrToInt)), false)
		ptrWantSig := gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(gotypes.NewVar(0, nil, "", ptrToInt)), false)
		assert.True(t, IsTypesCompatible(ptrResultSig, ptrWantSig))

		ptrToString := gotypes.NewPointer(gotypes.Typ[gotypes.String])
		ptrStringSig := gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(gotypes.NewVar(0, nil, "", ptrToString)), false)
		assert.False(t, IsTypesCompatible(ptrResultSig, ptrStringSig))

		assert.True(t, IsTypesCompatible(otherIntResultSig, gotypes.Typ[gotypes.Int]))
		assert.False(t, IsTypesCompatible(twoResultsSig, gotypes.Typ[gotypes.Int]))
		assert.False(t, IsTypesCompatible(stringResultSig, gotypes.Typ[gotypes.Int]))
	})

	t.Run("NamedTypes", func(t *testing.T) {
		namedInt1 := gotypes.NewNamed(gotypes.NewTypeName(0, nil, "MyInt1", nil), gotypes.Typ[gotypes.Int], nil)
		namedInt2 := gotypes.NewNamed(gotypes.NewTypeName(0, nil, "MyInt2", nil), gotypes.Typ[gotypes.Int], nil)

		// Same named type.
		assert.True(t, IsTypesCompatible(namedInt1, namedInt1))

		// Different named types with same underlying type.
		assert.False(t, IsTypesCompatible(namedInt1, namedInt2))

		// Named type to underlying type.
		assert.False(t, IsTypesCompatible(namedInt1, gotypes.Typ[gotypes.Int]))
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Int], namedInt1))
	})

	t.Run("TypeConversions", func(t *testing.T) {
		// Named types are convertible to their underlying types.
		userIDType := gotypes.NewNamed(gotypes.NewTypeName(0, nil, "UserID", nil), gotypes.Typ[gotypes.Int], nil)
		orderIDType := gotypes.NewNamed(gotypes.NewTypeName(0, nil, "OrderID", nil), gotypes.Typ[gotypes.Int], nil)

		// Named int types are NOT compatible with int (need conversion).
		assert.False(t, IsTypesCompatible(userIDType, gotypes.Typ[gotypes.Int]))
		assert.False(t, IsTypesCompatible(orderIDType, gotypes.Typ[gotypes.Int]))

		// Named string type is NOT compatible with string.
		namedString := gotypes.NewNamed(gotypes.NewTypeName(0, nil, "MyString", nil), gotypes.Typ[gotypes.String], nil)
		assert.False(t, IsTypesCompatible(namedString, gotypes.Typ[gotypes.String]))

		// But int to string conversions are excluded (impractical).
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.String]))
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Int]))

		// Bool conversions are excluded.
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Bool], gotypes.Typ[gotypes.Int]))
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.Bool]))
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Bool], gotypes.Typ[gotypes.String]))
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Bool]))

		// Numeric conversions within same category are NOT compatible (need conversion).
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Int32], gotypes.Typ[gotypes.Int64]))
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Float32], gotypes.Typ[gotypes.Float64]))
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.Float64]))
	})

	t.Run("UntypedBoolAssignments", func(t *testing.T) {
		namedBool := gotypes.NewNamed(gotypes.NewTypeName(0, nil, "MyBool", nil), gotypes.Typ[gotypes.Bool], nil)

		assert.True(t, gotypes.AssignableTo(gotypes.Typ[gotypes.UntypedBool], gotypes.Typ[gotypes.Bool]))
		assert.True(t, IsTypesCompatible(gotypes.Typ[gotypes.UntypedBool], gotypes.Typ[gotypes.Bool]))

		assert.True(t, gotypes.AssignableTo(gotypes.Typ[gotypes.UntypedBool], namedBool))
		assert.True(t, IsTypesCompatible(gotypes.Typ[gotypes.UntypedBool], namedBool))
	})

	t.Run("IncompatibleTypes", func(t *testing.T) {
		// Basic incompatible types.
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.String]))
		assert.False(t, IsTypesCompatible(gotypes.Typ[gotypes.Bool], gotypes.Typ[gotypes.Float64]))

		// Complex incompatible types.
		intSlice := gotypes.NewSlice(gotypes.Typ[gotypes.Int])
		stringPtr := gotypes.NewPointer(gotypes.Typ[gotypes.String])
		assert.False(t, IsTypesCompatible(intSlice, stringPtr))
	})
}

func TestIsTypesConvertible(t *testing.T) {
	t.Run("NilTypes", func(t *testing.T) {
		assert.False(t, IsTypesConvertible(nil, gotypes.Typ[gotypes.Int]))
		assert.False(t, IsTypesConvertible(gotypes.Typ[gotypes.Int], nil))
		assert.False(t, IsTypesConvertible(nil, nil))
	})

	t.Run("AllowedConversions", func(t *testing.T) {
		// Named types to underlying types.
		namedInt := gotypes.NewNamed(gotypes.NewTypeName(0, nil, "MyInt", nil), gotypes.Typ[gotypes.Int], nil)
		assert.True(t, IsTypesConvertible(namedInt, gotypes.Typ[gotypes.Int]))
		assert.True(t, IsTypesConvertible(gotypes.Typ[gotypes.Int], namedInt))

		// Numeric conversions within same category.
		assert.True(t, IsTypesConvertible(gotypes.Typ[gotypes.Int32], gotypes.Typ[gotypes.Int64]))
		assert.True(t, IsTypesConvertible(gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.Int32]))
		assert.True(t, IsTypesConvertible(gotypes.Typ[gotypes.Float32], gotypes.Typ[gotypes.Float64]))

		// Numeric conversions between int and float.
		assert.True(t, IsTypesConvertible(gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.Float64]))
		assert.True(t, IsTypesConvertible(gotypes.Typ[gotypes.Float32], gotypes.Typ[gotypes.Int]))

		// Untyped constants.
		assert.True(t, IsTypesConvertible(gotypes.Typ[gotypes.UntypedInt], gotypes.Typ[gotypes.Int]))
		assert.True(t, IsTypesConvertible(gotypes.Typ[gotypes.UntypedFloat], gotypes.Typ[gotypes.Float64]))
	})

	t.Run("UntypedBoolConversions", func(t *testing.T) {
		namedBool := gotypes.NewNamed(gotypes.NewTypeName(0, nil, "MyBool", nil), gotypes.Typ[gotypes.Bool], nil)

		assert.True(t, gotypes.ConvertibleTo(gotypes.Typ[gotypes.UntypedBool], gotypes.Typ[gotypes.Bool]))
		assert.True(t, IsTypesConvertible(gotypes.Typ[gotypes.UntypedBool], gotypes.Typ[gotypes.Bool]))

		assert.True(t, gotypes.ConvertibleTo(gotypes.Typ[gotypes.UntypedBool], namedBool))
		assert.True(t, IsTypesConvertible(gotypes.Typ[gotypes.UntypedBool], namedBool))
	})

	t.Run("ExcludedConversions", func(t *testing.T) {
		// Numeric to string conversions.
		assert.False(t, IsTypesConvertible(gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.String]))
		assert.False(t, IsTypesConvertible(gotypes.Typ[gotypes.Int32], gotypes.Typ[gotypes.String]))
		assert.False(t, IsTypesConvertible(gotypes.Typ[gotypes.Float64], gotypes.Typ[gotypes.String]))

		// String to numeric conversions.
		assert.False(t, IsTypesConvertible(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Int]))
		assert.False(t, IsTypesConvertible(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Float64]))

		// Bool to numeric conversions.
		assert.False(t, IsTypesConvertible(gotypes.Typ[gotypes.Bool], gotypes.Typ[gotypes.Int]))
		assert.False(t, IsTypesConvertible(gotypes.Typ[gotypes.Bool], gotypes.Typ[gotypes.Float64]))

		// Numeric to bool conversions.
		assert.False(t, IsTypesConvertible(gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.Bool]))
		assert.False(t, IsTypesConvertible(gotypes.Typ[gotypes.Float64], gotypes.Typ[gotypes.Bool]))

		// Bool to string conversions.
		assert.False(t, IsTypesConvertible(gotypes.Typ[gotypes.Bool], gotypes.Typ[gotypes.String]))

		// String to bool conversions.
		assert.False(t, IsTypesConvertible(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Bool]))
	})

	t.Run("GoTypesRejectedConversions", func(t *testing.T) {
		assert.False(t, gotypes.ConvertibleTo(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Int]))
		assert.False(t, gotypes.ConvertibleTo(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Float64]))
		assert.False(t, gotypes.ConvertibleTo(gotypes.Typ[gotypes.Bool], gotypes.Typ[gotypes.Int]))
		assert.False(t, gotypes.ConvertibleTo(gotypes.Typ[gotypes.Bool], gotypes.Typ[gotypes.String]))
		assert.False(t, gotypes.ConvertibleTo(gotypes.Typ[gotypes.Int], gotypes.Typ[gotypes.Bool]))
		assert.False(t, gotypes.ConvertibleTo(gotypes.Typ[gotypes.Float64], gotypes.Typ[gotypes.Bool]))
	})

	t.Run("NonConvertibleTypes", func(t *testing.T) {
		// Types that are not convertible at all.
		intSlice := gotypes.NewSlice(gotypes.Typ[gotypes.Int])
		stringSlice := gotypes.NewSlice(gotypes.Typ[gotypes.String])
		assert.False(t, IsTypesConvertible(intSlice, stringSlice))

		structType1 := gotypes.NewStruct([]*gotypes.Var{gotypes.NewField(0, nil, "a", gotypes.Typ[gotypes.Int], false)}, []string{})
		structType2 := gotypes.NewStruct([]*gotypes.Var{gotypes.NewField(0, nil, "b", gotypes.Typ[gotypes.String], false)}, []string{})
		assert.False(t, IsTypesConvertible(structType1, structType2))
	})
}
