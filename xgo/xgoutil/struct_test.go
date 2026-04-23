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

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const spxPkgPath = "github.com/goplus/spx/v2"

func TestIsNamedStructType(t *testing.T) {
	t.Run("NilNamedType", func(t *testing.T) {
		var named *gotypes.Named
		assert.False(t, IsNamedStructType(named))
	})

	t.Run("StructType", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		structType := gotypes.NewStruct([]*gotypes.Var{
			gotypes.NewField(token.NoPos, pkg, "field1", gotypes.Typ[gotypes.String], false),
			gotypes.NewField(token.NoPos, pkg, "field2", gotypes.Typ[gotypes.Int], false),
		}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestStruct", structType)
		named := gotypes.NewNamed(typeName, structType, nil)
		assert.True(t, IsNamedStructType(named))
	})

	t.Run("EmptyStructType", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "EmptyStruct", structType)
		named := gotypes.NewNamed(typeName, structType, nil)
		assert.True(t, IsNamedStructType(named))
	})

	t.Run("InterfaceType", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		interfaceType := gotypes.NewInterfaceType([]*gotypes.Func{}, []gotypes.Type{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestInterface", interfaceType)
		named := gotypes.NewNamed(typeName, interfaceType, nil)
		assert.False(t, IsNamedStructType(named))
	})

	t.Run("BasicType", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestInt", gotypes.Typ[gotypes.Int])
		named := gotypes.NewNamed(typeName, gotypes.Typ[gotypes.Int], nil)
		assert.False(t, IsNamedStructType(named))
	})

	t.Run("SliceType", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		sliceType := gotypes.NewSlice(gotypes.Typ[gotypes.String])
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestSlice", sliceType)
		named := gotypes.NewNamed(typeName, sliceType, nil)
		assert.False(t, IsNamedStructType(named))
	})

	t.Run("PointerType", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		pointerType := gotypes.NewPointer(gotypes.Typ[gotypes.Int])
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestPointer", pointerType)
		named := gotypes.NewNamed(typeName, pointerType, nil)
		assert.False(t, IsNamedStructType(named))
	})

	t.Run("FunctionType", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		signature := gotypes.NewSignatureType(nil, nil, nil,
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "x", gotypes.Typ[gotypes.Int])),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", gotypes.Typ[gotypes.String])),
			false)
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestFunc", signature)
		named := gotypes.NewNamed(typeName, signature, nil)
		assert.False(t, IsNamedStructType(named))
	})
}

func TestIsXGoClassStructType(t *testing.T) {
	t.Run("NilNamedType", func(t *testing.T) {
		var named *gotypes.Named
		assert.False(t, IsXGoClassStructType(named))
	})

	t.Run("NamedTypeWithNilObject", func(t *testing.T) {
		named := &gotypes.Named{}
		assert.False(t, IsXGoClassStructType(named))
	})

	t.Run("NonXGoPackage", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "Game", structType)
		named := gotypes.NewNamed(typeName, structType, nil)
		assert.False(t, IsXGoClassStructType(named))
	})

	t.Run("XGoPackageWithNonClassType", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")

		// Mark package as XGo package.
		markAsXGoPackage(pkg)

		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "SomeOtherType", structType)
		named := gotypes.NewNamed(typeName, structType, nil)
		assert.False(t, IsXGoClassStructType(named))
	})

	t.Run("SpxGameType", func(t *testing.T) {
		pkg := gotypes.NewPackage(spxPkgPath, "spx")

		// Mark package as XGo package.
		markAsXGoPackage(pkg)

		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "Game", structType)
		named := gotypes.NewNamed(typeName, structType, nil)
		assert.True(t, IsXGoClassStructType(named))
	})

	t.Run("SpxSpriteImplType", func(t *testing.T) {
		pkg := gotypes.NewPackage(spxPkgPath, "spx")

		// Mark package as XGo package.
		markAsXGoPackage(pkg)

		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "SpriteImpl", structType)
		named := gotypes.NewNamed(typeName, structType, nil)
		assert.True(t, IsXGoClassStructType(named))
	})

	t.Run("SpxGameTypeInNonXGoPackage", func(t *testing.T) {
		pkg := gotypes.NewPackage(spxPkgPath, "spx")
		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "Game", structType)
		named := gotypes.NewNamed(typeName, structType, nil)
		assert.False(t, IsXGoClassStructType(named))
	})

	t.Run("XGoPackageWithDifferentSpxType", func(t *testing.T) {
		pkg := gotypes.NewPackage(spxPkgPath, "spx")

		// Mark package as XGo package.
		markAsXGoPackage(pkg)

		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "Sprite", structType)
		named := gotypes.NewNamed(typeName, structType, nil)
		assert.False(t, IsXGoClassStructType(named))
	})
}

func TestWalkStruct(t *testing.T) {
	t.Run("NilNamedType", func(t *testing.T) {
		var (
			named  *gotypes.Named
			called bool
		)
		WalkStruct(named, func(member gotypes.Object, selector *gotypes.Named) bool {
			called = true
			return true
		})
		assert.False(t, called)
	})

	t.Run("NonStructType", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestInt", gotypes.Typ[gotypes.Int])
		named := gotypes.NewNamed(typeName, gotypes.Typ[gotypes.Int], nil)

		var called bool
		WalkStruct(named, func(member gotypes.Object, selector *gotypes.Named) bool {
			called = true
			return true
		})
		assert.False(t, called)
	})

	t.Run("EmptyStruct", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "EmptyStruct", structType)
		named := gotypes.NewNamed(typeName, structType, nil)

		var called bool
		WalkStruct(named, func(member gotypes.Object, selector *gotypes.Named) bool {
			called = true
			return true
		})
		assert.False(t, called)
	})

	t.Run("SimpleStructWithFields", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		field1 := gotypes.NewField(token.NoPos, pkg, "Field1", gotypes.Typ[gotypes.String], false)
		field2 := gotypes.NewField(token.NoPos, pkg, "Field2", gotypes.Typ[gotypes.Int], false)
		structType := gotypes.NewStruct([]*gotypes.Var{field1, field2}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestStruct", structType)
		named := gotypes.NewNamed(typeName, structType, nil)

		var (
			members   []gotypes.Object
			selectors []*gotypes.Named
		)
		WalkStruct(named, func(member gotypes.Object, selector *gotypes.Named) bool {
			members = append(members, member)
			selectors = append(selectors, selector)
			return true
		})
		require.Len(t, members, 2)
		assert.Equal(t, "Field1", members[0].Name())
		assert.Equal(t, "Field2", members[1].Name())
		assert.Equal(t, named, selectors[0])
		assert.Equal(t, named, selectors[1])
	})

	t.Run("StructWithMethods", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestStruct", structType)
		named := gotypes.NewNamed(typeName, structType, nil)

		// Add a method to the named type.
		signature := gotypes.NewSignatureType(nil, nil, nil,
			gotypes.NewTuple(),
			gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "", gotypes.Typ[gotypes.String])),
			false)
		method := gotypes.NewFunc(token.NoPos, pkg, "Method1", signature)
		named.AddMethod(method)

		var members []gotypes.Object
		WalkStruct(named, func(member gotypes.Object, selector *gotypes.Named) bool {
			members = append(members, member)
			return true
		})
		require.Len(t, members, 1)
		assert.Equal(t, "Method1", members[0].Name())
	})

	t.Run("MethodEarlyTermination", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestStruct", structType)
		named := gotypes.NewNamed(typeName, structType, nil)

		signature := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		named.AddMethod(gotypes.NewFunc(token.NoPos, pkg, "Method1", signature))
		named.AddMethod(gotypes.NewFunc(token.NoPos, pkg, "Method2", signature))

		var members []gotypes.Object
		WalkStruct(named, func(member gotypes.Object, selector *gotypes.Named) bool {
			members = append(members, member)
			return false
		})

		require.Len(t, members, 1)
		assert.Equal(t, "Method1", members[0].Name())
	})

	t.Run("UnexportedMethodSkipped", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		structType := gotypes.NewStruct([]*gotypes.Var{}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestStruct", structType)
		named := gotypes.NewNamed(typeName, structType, nil)

		signature := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		named.AddMethod(gotypes.NewFunc(token.NoPos, pkg, "ExportedMethod", signature))
		named.AddMethod(gotypes.NewFunc(token.NoPos, pkg, "unexportedMethod", signature))

		var members []gotypes.Object
		WalkStruct(named, func(member gotypes.Object, selector *gotypes.Named) bool {
			members = append(members, member)
			return true
		})

		require.Len(t, members, 1)
		assert.Equal(t, "ExportedMethod", members[0].Name())
	})
	t.Run("StructWithEmbeddedStruct", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")

		// Create embedded struct.
		embeddedField := gotypes.NewField(token.NoPos, pkg, "EmbeddedField", gotypes.Typ[gotypes.String], false)
		embeddedStructType := gotypes.NewStruct([]*gotypes.Var{embeddedField}, []string{})
		embeddedTypeName := gotypes.NewTypeName(token.NoPos, pkg, "EmbeddedStruct", embeddedStructType)
		embeddedNamed := gotypes.NewNamed(embeddedTypeName, embeddedStructType, nil)

		// Create main struct with embedded field.
		mainField := gotypes.NewField(token.NoPos, pkg, "MainField", gotypes.Typ[gotypes.Int], false)
		embeddedFieldVar := gotypes.NewField(token.NoPos, pkg, "EmbeddedStruct", embeddedNamed, true) // embedded
		mainStructType := gotypes.NewStruct([]*gotypes.Var{mainField, embeddedFieldVar}, []string{})
		mainTypeName := gotypes.NewTypeName(token.NoPos, pkg, "MainStruct", mainStructType)
		mainNamed := gotypes.NewNamed(mainTypeName, mainStructType, nil)

		var members []gotypes.Object
		WalkStruct(mainNamed, func(member gotypes.Object, selector *gotypes.Named) bool {
			members = append(members, member)
			return true
		})
		require.Len(t, members, 3)
		memberNames := make([]string, len(members))
		for i, member := range members {
			memberNames[i] = member.Name()
		}
		assert.Contains(t, memberNames, "MainField")
		assert.Contains(t, memberNames, "EmbeddedStruct")
		assert.Contains(t, memberNames, "EmbeddedField")
	})

	t.Run("EarlyTermination", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		field1 := gotypes.NewField(token.NoPos, pkg, "Field1", gotypes.Typ[gotypes.String], false)
		field2 := gotypes.NewField(token.NoPos, pkg, "Field2", gotypes.Typ[gotypes.Int], false)
		structType := gotypes.NewStruct([]*gotypes.Var{field1, field2}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestStruct", structType)
		named := gotypes.NewNamed(typeName, structType, nil)

		var members []gotypes.Object
		WalkStruct(named, func(member gotypes.Object, selector *gotypes.Named) bool {
			members = append(members, member)
			return len(members) < 1 // Stop after first member.
		})
		require.Len(t, members, 1)
		assert.Equal(t, "Field1", members[0].Name())
	})

	t.Run("CircularReferenceAvoidance", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")

		// Create type names first.
		typeNameA := gotypes.NewTypeName(token.NoPos, pkg, "StructA", nil)
		typeNameB := gotypes.NewTypeName(token.NoPos, pkg, "StructB", nil)

		// Create placeholder named types.
		namedA := gotypes.NewNamed(typeNameA, nil, nil)
		namedB := gotypes.NewNamed(typeNameB, nil, nil)

		// Create fields that reference each other.
		fieldAToB := gotypes.NewField(token.NoPos, pkg, "StructB", namedB, true)
		fieldBToA := gotypes.NewField(token.NoPos, pkg, "StructA", namedA, true)

		// Create struct types with circular references.
		structATypeWithB := gotypes.NewStruct([]*gotypes.Var{fieldAToB}, []string{})
		structBTypeWithA := gotypes.NewStruct([]*gotypes.Var{fieldBToA}, []string{})

		// Set the underlying types.
		namedA.SetUnderlying(structATypeWithB)
		namedB.SetUnderlying(structBTypeWithA)

		var callCount int
		WalkStruct(namedA, func(member gotypes.Object, selector *gotypes.Named) bool {
			callCount++
			return true
		})
		assert.GreaterOrEqual(t, callCount, 0) // Should not cause infinite recursion.
	})

	t.Run("UnexportedFieldsAndMethods", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		exportedField := gotypes.NewField(token.NoPos, pkg, "ExportedField", gotypes.Typ[gotypes.String], false)
		unexportedField := gotypes.NewField(token.NoPos, pkg, "unexportedField", gotypes.Typ[gotypes.Int], false)
		structType := gotypes.NewStruct([]*gotypes.Var{exportedField, unexportedField}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestStruct", structType)
		named := gotypes.NewNamed(typeName, structType, nil)

		var members []gotypes.Object
		WalkStruct(named, func(member gotypes.Object, selector *gotypes.Named) bool {
			members = append(members, member)
			return true
		})

		// Only exported field should be included.
		require.Len(t, members, 1)
		assert.Equal(t, "ExportedField", members[0].Name())
	})
	t.Run("DuplicateMemberNames", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")

		// Create embedded struct with a field.
		embeddedField := gotypes.NewField(token.NoPos, pkg, "SameName", gotypes.Typ[gotypes.String], false)
		embeddedStructType := gotypes.NewStruct([]*gotypes.Var{embeddedField}, []string{})
		embeddedTypeName := gotypes.NewTypeName(token.NoPos, pkg, "EmbeddedStruct", embeddedStructType)
		embeddedNamed := gotypes.NewNamed(embeddedTypeName, embeddedStructType, nil)

		// Create main struct with same field name.
		mainField := gotypes.NewField(token.NoPos, pkg, "SameName", gotypes.Typ[gotypes.Int], false)
		embeddedFieldVar := gotypes.NewField(token.NoPos, pkg, "EmbeddedStruct", embeddedNamed, true)
		mainStructType := gotypes.NewStruct([]*gotypes.Var{mainField, embeddedFieldVar}, []string{})
		mainTypeName := gotypes.NewTypeName(token.NoPos, pkg, "MainStruct", mainStructType)
		mainNamed := gotypes.NewNamed(mainTypeName, mainStructType, nil)

		var members []gotypes.Object
		WalkStruct(mainNamed, func(member gotypes.Object, selector *gotypes.Named) bool {
			members = append(members, member)
			return true
		})

		// Should see SameName (first occurrence) and EmbeddedStruct, but not the duplicate SameName.
		require.Len(t, members, 2)
		memberNames := make([]string, len(members))
		memberTypes := make([]gotypes.Type, len(members))
		for i, member := range members {
			memberNames[i] = member.Name()
			memberTypes[i] = member.Type()
		}
		assert.Contains(t, memberNames, "SameName")
		assert.Contains(t, memberNames, "EmbeddedStruct")
		// First occurrence should be from main struct (Int type).
		sameNameIndex := -1
		for i, name := range memberNames {
			if name == "SameName" {
				sameNameIndex = i
				break
			}
		}
		assert.NotEqual(t, -1, sameNameIndex)
		assert.Equal(t, gotypes.Typ[gotypes.Int], memberTypes[sameNameIndex])
	})

	t.Run("FieldAndMethodWithSameName", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		field := gotypes.NewField(token.NoPos, pkg, "SameName", gotypes.Typ[gotypes.Int], false)
		structType := gotypes.NewStruct([]*gotypes.Var{field}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "TestStruct", structType)
		named := gotypes.NewNamed(typeName, structType, nil)

		signature := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		named.AddMethod(gotypes.NewFunc(token.NoPos, pkg, "SameName", signature))

		var members []gotypes.Object
		WalkStruct(named, func(member gotypes.Object, selector *gotypes.Named) bool {
			members = append(members, member)
			return true
		})

		require.Len(t, members, 1)
		assert.Equal(t, "SameName", members[0].Name())
		assert.Equal(t, gotypes.Typ[gotypes.Int], members[0].Type())
	})

	t.Run("XGoClassStructSelector", func(t *testing.T) {
		pkg := gotypes.NewPackage(spxPkgPath, "spx")

		// Mark package as XGo package.
		markAsXGoPackage(pkg)

		// Create XGo class struct.
		field := gotypes.NewField(token.NoPos, pkg, "TestField", gotypes.Typ[gotypes.String], false)
		structType := gotypes.NewStruct([]*gotypes.Var{field}, []string{})
		typeName := gotypes.NewTypeName(token.NoPos, pkg, "Game", structType)
		named := gotypes.NewNamed(typeName, structType, nil)

		var selectors []*gotypes.Named
		WalkStruct(named, func(member gotypes.Object, selector *gotypes.Named) bool {
			selectors = append(selectors, selector)
			return true
		})
		require.Len(t, selectors, 1)
		assert.Equal(t, named, selectors[0])
	})
	t.Run("EmbeddedPointerStruct", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")

		// Create embedded struct.
		embeddedField := gotypes.NewField(token.NoPos, pkg, "EmbeddedField", gotypes.Typ[gotypes.String], false)
		embeddedStructType := gotypes.NewStruct([]*gotypes.Var{embeddedField}, []string{})
		embeddedTypeName := gotypes.NewTypeName(token.NoPos, pkg, "EmbeddedStruct", embeddedStructType)
		embeddedNamed := gotypes.NewNamed(embeddedTypeName, embeddedStructType, nil)

		// Create main struct with embedded pointer field.
		mainField := gotypes.NewField(token.NoPos, pkg, "MainField", gotypes.Typ[gotypes.Int], false)
		embeddedPointerType := gotypes.NewPointer(embeddedNamed)
		embeddedFieldVar := gotypes.NewField(token.NoPos, pkg, "EmbeddedStruct", embeddedPointerType, true)
		mainStructType := gotypes.NewStruct([]*gotypes.Var{mainField, embeddedFieldVar}, []string{})
		mainTypeName := gotypes.NewTypeName(token.NoPos, pkg, "MainStruct", mainStructType)
		mainNamed := gotypes.NewNamed(mainTypeName, mainStructType, nil)

		var members []gotypes.Object
		WalkStruct(mainNamed, func(member gotypes.Object, selector *gotypes.Named) bool {
			members = append(members, member)
			return true
		})
		require.Len(t, members, 3)
		memberNames := make([]string, len(members))
		for i, member := range members {
			memberNames[i] = member.Name()
		}
		assert.Contains(t, memberNames, "MainField")
		assert.Contains(t, memberNames, "EmbeddedStruct")
		assert.Contains(t, memberNames, "EmbeddedField")
	})

	t.Run("SelectorStopsAtUnexportedEmbeddedType", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")

		innerField := gotypes.NewField(token.NoPos, pkg, "ExportedField", gotypes.Typ[gotypes.String], false)
		innerStructType := gotypes.NewStruct([]*gotypes.Var{innerField}, []string{})
		innerTypeName := gotypes.NewTypeName(token.NoPos, pkg, "embeddedStruct", innerStructType)
		innerNamed := gotypes.NewNamed(innerTypeName, innerStructType, nil)

		embeddedFieldVar := gotypes.NewField(token.NoPos, pkg, "embeddedStruct", innerNamed, true)
		mainStructType := gotypes.NewStruct([]*gotypes.Var{embeddedFieldVar}, []string{})
		mainTypeName := gotypes.NewTypeName(token.NoPos, pkg, "MainStruct", mainStructType)
		mainNamed := gotypes.NewNamed(mainTypeName, mainStructType, nil)

		var selectorForInnerField *gotypes.Named
		WalkStruct(mainNamed, func(member gotypes.Object, selector *gotypes.Named) bool {
			if member.Name() == "ExportedField" {
				selectorForInnerField = selector
			}
			return true
		})

		require.NotNil(t, selectorForInnerField)
		assert.Equal(t, mainNamed, selectorForInnerField)
	})

	t.Run("EmbeddedNamedNonStructIsIgnored", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")

		namedInt := gotypes.NewNamed(
			gotypes.NewTypeName(token.NoPos, pkg, "EmbeddedInt", gotypes.Typ[gotypes.Int]),
			gotypes.Typ[gotypes.Int],
			nil,
		)
		embeddedFieldVar := gotypes.NewField(token.NoPos, pkg, "EmbeddedInt", namedInt, true)
		mainStructType := gotypes.NewStruct([]*gotypes.Var{embeddedFieldVar}, []string{})
		mainTypeName := gotypes.NewTypeName(token.NoPos, pkg, "MainStruct", mainStructType)
		mainNamed := gotypes.NewNamed(mainTypeName, mainStructType, nil)

		var members []gotypes.Object
		WalkStruct(mainNamed, func(member gotypes.Object, selector *gotypes.Named) bool {
			members = append(members, member)
			return true
		})

		require.Len(t, members, 1)
		assert.Equal(t, "EmbeddedInt", members[0].Name())
	})

	t.Run("EmbeddedWalkEarlyTermination", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")

		embeddedField := gotypes.NewField(token.NoPos, pkg, "EmbeddedField", gotypes.Typ[gotypes.String], false)
		embeddedStructType := gotypes.NewStruct([]*gotypes.Var{embeddedField}, []string{})
		embeddedTypeName := gotypes.NewTypeName(token.NoPos, pkg, "EmbeddedStruct", embeddedStructType)
		embeddedNamed := gotypes.NewNamed(embeddedTypeName, embeddedStructType, nil)

		mainField := gotypes.NewField(token.NoPos, pkg, "MainField", gotypes.Typ[gotypes.Int], false)
		embeddedFieldVar := gotypes.NewField(token.NoPos, pkg, "EmbeddedStruct", embeddedNamed, true)
		mainStructType := gotypes.NewStruct([]*gotypes.Var{mainField, embeddedFieldVar}, []string{})
		mainTypeName := gotypes.NewTypeName(token.NoPos, pkg, "MainStruct", mainStructType)
		mainNamed := gotypes.NewNamed(mainTypeName, mainStructType, nil)

		var members []string
		WalkStruct(mainNamed, func(member gotypes.Object, selector *gotypes.Named) bool {
			members = append(members, member.Name())
			return member.Name() != "EmbeddedField"
		})

		assert.Equal(t, []string{"MainField", "EmbeddedStruct", "EmbeddedField"}, members)
	})
}
