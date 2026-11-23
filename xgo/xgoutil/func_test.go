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
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"testing"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemberAliasName(t *testing.T) {
	for _, tt := range []struct {
		name, declared, alias string
	}{
		{name: "ASCII", declared: "Read", alias: "read"},
		{name: "Acronym", declared: "URL", alias: "uRL"},
		{name: "ReservedAlias", declared: "For", alias: "For"},
		{name: "Protocol", declared: "XGo_Read", alias: "_Read"},
		{name: "NonASCII", declared: "\u0394elta", alias: "\u0394elta"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.alias, MemberAliasName(tt.declared))
			assert.Equal(t, tt.declared, MemberNameForAlias(tt.alias))
		})
	}
}

func TestIsXgotMethodName(t *testing.T) {
	t.Run("ValidXGotMethodName", func(t *testing.T) {
		assert.True(t, IsXGotMethodName("XGot_Type_Method"))
	})

	t.Run("ValidXGotMethodNameWithUnderscore", func(t *testing.T) {
		assert.True(t, IsXGotMethodName("XGot_MyType_MyMethod"))
	})

	t.Run("InvalidPrefix", func(t *testing.T) {
		assert.False(t, IsXGotMethodName("Type_Method"))
	})

	t.Run("InvalidXGoxPrefix", func(t *testing.T) {
		assert.False(t, IsXGotMethodName("XGox_Type_Method"))
	})

	t.Run("NoUnderscore", func(t *testing.T) {
		assert.False(t, IsXGotMethodName("XGotTypeMethod"))
	})

	t.Run("OnlyPrefix", func(t *testing.T) {
		assert.False(t, IsXGotMethodName("XGot_"))
	})

	t.Run("OnlyPrefixAndType", func(t *testing.T) {
		assert.False(t, IsXGotMethodName("XGot_Type"))
	})

	t.Run("EmptyString", func(t *testing.T) {
		assert.False(t, IsXGotMethodName(""))
	})
}

func TestSplitXGotMethodName(t *testing.T) {
	t.Run("ValidXGotMethodName", func(t *testing.T) {
		recvTypeName, methodName, ok := SplitXGotMethodName("XGot_Type_Method", false)
		require.True(t, ok)
		assert.Equal(t, "Type", recvTypeName)
		assert.Equal(t, "Method", methodName)
	})

	t.Run("ValidXGotMethodNameWithXGoxPrefix", func(t *testing.T) {
		recvTypeName, methodName, ok := SplitXGotMethodName("XGot_Type_XGox_Method", false)
		require.True(t, ok)
		assert.Equal(t, "Type", recvTypeName)
		assert.Equal(t, "XGox_Method", methodName)
	})

	t.Run("ValidXGotMethodNameTrimXGox", func(t *testing.T) {
		recvTypeName, methodName, ok := SplitXGotMethodName("XGot_Type_XGox_Method", true)
		require.True(t, ok)
		assert.Equal(t, "Type", recvTypeName)
		assert.Equal(t, "Method", methodName)
	})

	t.Run("InvalidPrefix", func(t *testing.T) {
		_, _, ok := SplitXGotMethodName("Type_Method", false)
		assert.False(t, ok)
	})

	t.Run("NoUnderscore", func(t *testing.T) {
		_, _, ok := SplitXGotMethodName("XGotTypeMethod", false)
		assert.False(t, ok)
	})

	t.Run("OnlyPrefix", func(t *testing.T) {
		_, _, ok := SplitXGotMethodName("XGot_", false)
		assert.False(t, ok)
	})

	t.Run("OnlyPrefixAndType", func(t *testing.T) {
		_, _, ok := SplitXGotMethodName("XGot_Type", false)
		assert.False(t, ok)
	})

	t.Run("EmptyString", func(t *testing.T) {
		_, _, ok := SplitXGotMethodName("", false)
		assert.False(t, ok)
	})

	t.Run("EscapedHistoricalTemplate", func(t *testing.T) {
		recvTypeName, methodName, ok := SplitXGotMethodName("Gopt__Type_Name__XGox_Method_Name", true)
		require.True(t, ok)
		assert.Equal(t, "Type_Name", recvTypeName)
		assert.Equal(t, "Method_Name", methodName)
	})

	t.Run("MultipleUnderscores", func(t *testing.T) {
		recvTypeName, methodName, ok := SplitXGotMethodName("XGot_MyType_My_Method", false)
		require.True(t, ok)
		assert.Equal(t, "MyType", recvTypeName)
		assert.Equal(t, "My_Method", methodName)
	})
}

func TestSplitXGosMemberName(t *testing.T) {
	for _, tt := range []struct {
		name, owner, member string
	}{
		{name: "XGos_Type_Name", owner: "Type", member: "Name"},
		{name: "Gops__Type_Name__Member_Name", owner: "Type_Name", member: "Member_Name"},
		{name: "XGos__Type_Name"},
		{name: "XGos_Type_"},
		{name: "Type_Name"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			owner, member, ok := SplitXGosMemberName(tt.name)
			assert.Equal(t, tt.owner != "", ok)
			assert.Equal(t, tt.owner, owner)
			assert.Equal(t, tt.member, member)
		})
	}
}

func TestSplitXGoxFuncName(t *testing.T) {
	t.Run("ValidXGoxFuncName", func(t *testing.T) {
		funcName, ok := SplitXGoxFuncName("XGox_Method")
		require.True(t, ok)
		assert.Equal(t, "Method", funcName)
	})

	t.Run("HistoricalPrefix", func(t *testing.T) {
		funcName, ok := SplitXGoxFuncName("Gopx_Method")
		require.True(t, ok)
		assert.Equal(t, "Method", funcName)
	})

	t.Run("ValidXGoxFuncNameWithUnderscores", func(t *testing.T) {
		funcName, ok := SplitXGoxFuncName("XGox_My_Method")
		require.True(t, ok)
		assert.Equal(t, "My_Method", funcName)
	})

	t.Run("InvalidPrefix", func(t *testing.T) {
		_, ok := SplitXGoxFuncName("Method")
		assert.False(t, ok)
	})

	t.Run("InvalidXGotPrefix", func(t *testing.T) {
		_, ok := SplitXGoxFuncName("XGot_Method")
		assert.False(t, ok)
	})

	t.Run("OnlyPrefix", func(t *testing.T) {
		_, ok := SplitXGoxFuncName("XGox_")
		assert.False(t, ok)
	})

	t.Run("EmptyString", func(t *testing.T) {
		_, ok := SplitXGoxFuncName("")
		assert.False(t, ok)
	})
}

func TestParseXGoFuncName(t *testing.T) {
	t.Run("RegularFunctionName", func(t *testing.T) {
		parsedName, overloadID := ParseXGoFuncName("MyFunction")
		assert.Equal(t, "myFunction", parsedName)
		assert.Nil(t, overloadID)
	})

	t.Run("OverloadedFunctionName", func(t *testing.T) {
		parsedName, overloadID := ParseXGoFuncName("MyFunction__a")
		assert.Equal(t, "myFunction", parsedName)
		require.NotNil(t, overloadID)
		assert.Equal(t, "a", *overloadID)
	})

	t.Run("OverloadedFunctionNameWithNumber", func(t *testing.T) {
		parsedName, overloadID := ParseXGoFuncName("MyFunction__1")
		assert.Equal(t, "myFunction", parsedName)
		require.NotNil(t, overloadID)
		assert.Equal(t, "1", *overloadID)
	})

	t.Run("OverloadedFunctionNameWithZero", func(t *testing.T) {
		parsedName, overloadID := ParseXGoFuncName("MyFunction__0")
		assert.Equal(t, "myFunction", parsedName)
		require.NotNil(t, overloadID)
		assert.Equal(t, "0", *overloadID)
	})

	t.Run("FunctionNameWithSingleUnderscore", func(t *testing.T) {
		parsedName, overloadID := ParseXGoFuncName("My_Function")
		assert.Equal(t, "my_Function", parsedName)
		assert.Nil(t, overloadID)
	})

	t.Run("FunctionNameWithDoubleUnderscoreButInvalidSuffix", func(t *testing.T) {
		parsedName, overloadID := ParseXGoFuncName("MyFunction__AA")
		assert.Equal(t, "myFunction__AA", parsedName)
		assert.Nil(t, overloadID)
	})

	t.Run("EmptyString", func(t *testing.T) {
		parsedName, overloadID := ParseXGoFuncName("")
		assert.Empty(t, parsedName)
		assert.Nil(t, overloadID)
	})

	t.Run("PascalCaseFunction", func(t *testing.T) {
		parsedName, overloadID := ParseXGoFuncName("PascalCaseFunction")
		assert.Equal(t, "pascalCaseFunction", parsedName)
		assert.Nil(t, overloadID)
	})
}

func TestIsXGoOverloadedFuncName(t *testing.T) {
	t.Run("ValidOverloadedName", func(t *testing.T) {
		assert.True(t, IsXGoOverloadedFuncName("MyFunction__a"))
	})

	t.Run("ValidOverloadedNameWithNumber", func(t *testing.T) {
		assert.True(t, IsXGoOverloadedFuncName("MyFunction__1"))
	})

	t.Run("ValidOverloadedNameWithZero", func(t *testing.T) {
		assert.True(t, IsXGoOverloadedFuncName("MyFunction__0"))
	})

	t.Run("RegularFunctionName", func(t *testing.T) {
		assert.False(t, IsXGoOverloadedFuncName("MyFunction"))
	})

	t.Run("FunctionNameWithSingleUnderscore", func(t *testing.T) {
		assert.False(t, IsXGoOverloadedFuncName("My_Function"))
	})

	t.Run("FunctionNameWithDoubleUnderscoreButInvalidSuffix", func(t *testing.T) {
		assert.False(t, IsXGoOverloadedFuncName("MyFunction__AA"))
	})

	t.Run("FunctionNameWithDoubleUnderscoreButEmptySuffix", func(t *testing.T) {
		assert.False(t, IsXGoOverloadedFuncName("MyFunction__"))
	})

	t.Run("EmptyString", func(t *testing.T) {
		assert.False(t, IsXGoOverloadedFuncName(""))
	})

	t.Run("OnlyDoubleUnderscore", func(t *testing.T) {
		assert.False(t, IsXGoOverloadedFuncName("__"))
	})
}

func TestIsXGoOverloadableFunc(t *testing.T) {
	t.Run("RegularFunction", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		sig := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.False(t, IsXGoOverloadableFunc(fun))
	})

	t.Run("FunctionWithIntParameter", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		params := gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "x", gotypes.Typ[gotypes.Int]))
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.False(t, IsXGoOverloadableFunc(fun))
	})

	t.Run("FunctionWithMultipleParameters", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		params := gotypes.NewTuple(
			gotypes.NewParam(token.NoPos, pkg, "x", gotypes.Typ[gotypes.Int]),
			gotypes.NewParam(token.NoPos, pkg, "y", gotypes.Typ[gotypes.String]),
		)
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.False(t, IsXGoOverloadableFunc(fun))
	})

	t.Run("OverloadFunction", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		overload1 := gotypes.NewFunc(token.NoPos, pkg, "foo", gotypes.NewSignatureType(nil, nil, nil, nil, nil, false))
		overload2 := gotypes.NewFunc(token.NoPos, pkg, "bar", gotypes.NewSignatureType(nil, nil, nil, nil, nil, false))
		fun := gogen.NewOverloadFunc(token.NoPos, pkg, "TestFunc", overload1, overload2)
		assert.True(t, IsXGoOverloadableFunc(fun))
	})
}

func TestIsUnexpandableXGoOverloadableFunc(t *testing.T) {
	t.Run("RegularFunction", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		sig := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.False(t, IsUnexpandableXGoOverloadableFunc(fun))
	})

	t.Run("FunctionWithIntParameter", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		params := gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "x", gotypes.Typ[gotypes.Int]))
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.False(t, IsUnexpandableXGoOverloadableFunc(fun))
	})

	t.Run("FunctionWithMultipleParameters", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		params := gotypes.NewTuple(
			gotypes.NewParam(token.NoPos, pkg, "x", gotypes.Typ[gotypes.Int]),
			gotypes.NewParam(token.NoPos, pkg, "y", gotypes.Typ[gotypes.String]),
		)
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.False(t, IsUnexpandableXGoOverloadableFunc(fun))
	})

	t.Run("UnexpandableFuncEx", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		fun := gotypes.NewFunc(token.NoPos, pkg, "TestFunc", newTestFuncExSignature(pkg, nil, gotypes.Typ[gotypes.Int]))
		assert.True(t, IsUnexpandableXGoOverloadableFunc(fun))
	})
}

func TestExpandXGoOverloadableFunc(t *testing.T) {
	t.Run("IncompleteOverload", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		resolved := gotypes.NewFunc(token.NoPos, pkg, "Resolved", gotypes.NewSignatureType(nil, nil, nil, nil, nil, false))
		fun := gogen.NewOverloadFunc(token.NoPos, pkg, "Read", nil, (*gotypes.Func)(nil), resolved)
		assert.Equal(t, []*gotypes.Func{resolved}, ExpandXGoOverloadableFunc(fun))
		assert.Nil(t, OverloadGroupMarker(fun))
	})

	t.Run("RegularFunction", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		sig := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.Nil(t, ExpandXGoOverloadableFunc(fun))
	})

	t.Run("FunctionWithIntParameter", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		params := gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "x", gotypes.Typ[gotypes.Int]))
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.Nil(t, ExpandXGoOverloadableFunc(fun))
	})

	t.Run("FunctionWithMultipleParameters", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		params := gotypes.NewTuple(
			gotypes.NewParam(token.NoPos, pkg, "x", gotypes.Typ[gotypes.Int]),
			gotypes.NewParam(token.NoPos, pkg, "y", gotypes.Typ[gotypes.String]),
		)
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.Nil(t, ExpandXGoOverloadableFunc(fun))
	})

	t.Run("OverloadFunction", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		overload1 := gotypes.NewFunc(token.NoPos, pkg, "foo", gotypes.NewSignatureType(nil, nil, nil, nil, nil, false))
		overload2 := gotypes.NewFunc(token.NoPos, pkg, "bar", gotypes.NewSignatureType(nil, nil, nil, nil, nil, false))
		fun := gogen.NewOverloadFunc(token.NoPos, pkg, "TestFunc", overload1, overload2)

		expanded := ExpandXGoOverloadableFunc(fun)
		require.Len(t, expanded, 2)
		assert.Same(t, overload1, expanded[0])
		assert.Same(t, overload2, expanded[1])
	})
}

func TestOverloadImplementations(t *testing.T) {
	pkg := gotypes.NewPackage("example.com/encoded", "encoded")
	markAsXGoPackage(pkg)
	param := gotypes.NewTypeParam(gotypes.NewTypeName(token.NoPos, pkg, "T", nil), newTestEmptyInterface())
	params := gotypes.NewTuple(gotypes.NewParam(token.NoPos, pkg, "value", param))
	implementation := gotypes.NewFunc(token.NoPos, pkg, "XGox_Convert", gotypes.NewSignatureType(nil, nil, []*gotypes.TypeParam{param}, params, nil, false))
	pkg.Scope().Insert(implementation)
	gogen.InitXGoPackage(pkg)
	alias, ok := pkg.Scope().Lookup("Convert").(*gotypes.Func)
	require.True(t, ok)
	fallback := newTestFunc(pkg, "Fallback", false)
	group := gogen.NewOverloadFunc(token.NoPos, pkg, "Choose", implementation, alias, fallback)
	candidates := OverloadImplementations(group)
	require.NotNil(t, candidates)

	var implementations []gotypes.Object
	var bindings []*gotypes.Func
	for candidate, binding := range candidates {
		implementations = append(implementations, candidate)
		bindings = append(bindings, binding)
	}
	assert.Equal(t, []gotypes.Object{implementation, implementation, fallback}, implementations)
	assert.Equal(t, []*gotypes.Func{group, alias, group}, bindings)

	var visited []*gotypes.Func
	for _, binding := range candidates {
		visited = append(visited, binding)
		if binding == alias {
			break
		}
	}
	assert.Equal(t, []*gotypes.Func{group, alias}, visited)
}

func TestReferencesEncodedFuncName(t *testing.T) {
	pkg := gotypes.NewPackage("example.com/framework", "framework")
	markAsXGoPackage(pkg)
	owner := gotypes.NewTypeName(0, pkg, "Item", nil)
	gotypes.NewNamed(owner, gotypes.NewStruct(nil, nil), nil)
	pkg.Scope().Insert(owner)
	signature := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
	for _, tt := range []struct {
		function, source string
		want             bool
	}{
		{function: "Gopt_Item_Choose", source: "Gopt_Item_Choose", want: true},
		{function: "Gopt_Item_Choose", source: "choose"},
		{function: "Gopt_Item_Choose__0", source: "Gopt_Item_Choose", want: true},
		{function: "Gopt_Item_Choose__0", source: "gopt_Item_Choose", want: true},
		{function: "Gopt_Item_Choose__0", source: "Gopt_Item_Choose__1"},
		{function: "Gopt_Missing_Choose", source: "Gopt_Missing_Choose"},
		{function: "XGos_Item_Choose", source: "XGos_Item_Choose", want: true},
		{function: "XGos_Item_Choose", source: "choose"},
		{function: "XGos_Item_Choose__0", source: "xGos_Item_Choose", want: true},
		{function: "Gops_Item_Choose", source: "Gops_Item_Choose", want: true},
		{function: "XGos_Missing_Choose", source: "XGos_Missing_Choose"},
		{function: "Gopx_Choose", source: "gopx_Choose", want: true},
		{function: "Choose", source: "Choose"},
	} {
		t.Run(tt.function+"/"+tt.source, func(t *testing.T) {
			fun := gotypes.NewFunc(0, pkg, tt.function, signature)
			assert.Equal(t, tt.want, ReferencesEncodedFuncName(fun, tt.source))
		})
	}
}

func TestOverloadGroupDeclarations(t *testing.T) {
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "encoded.go", `package encoded
const XGoPackage = true
type Item struct{}
type Item_Name struct{}
type Choice__0 struct{}
func Put__0(value int) {}
func PutString(value string) {}
var PutValue = func(value int) {}
const XGoo_Store = "PutValue"
var XGox_NonCallable = 1
const XGoo_Put = ",PutString"
const XGoo__Put = "PutString"
const XGoo_Pick = "Missing"
const XGoo__Pick = "PutString"
const XGoo_Chained = "Target"
const XGoo_Target = "PutString"
const XGoo__Chained = "Target"
const XGoo_Region_Alias = "PutString"
const XGoo__Region = "Region_Alias"
const XGoo_Choice_Alias = "PutString"
func Put2__0(value int) {}
const XGoo__Put2 = ",PutString"
func Item_Read__0(value int) {}
func (Item) Read__0(value int) {}
const XGoo_Item_Read = ".Read__0"
const XGoo_Item_Show = ".Read"
const XGoo_Item_read = ".Read"
const XGoo__Item__XGo_Inspect = ".Read"
func (Item) Move__1(value int) {}
const XGoo_Item_Move = "Missing"
const XGoo__Item__Move = "Missing,"
func (Item) Skip__1(value int) {}
const XGoo_Item_Skip = ".Missing,.Skip__1"
const XGoo__Item__Skip = ".Skip__1"
func (Item) Draw__1(value int) {}
const XGoo_Item_Draw = "Missing"
const XGoo__Item__Draw = "PutString,"
func (Item_Name) Read__0(value int) {}
const XGoo__Item_Name__Read = ".Read__0"
func Gather(item Item, value int) {}
const XGoo__XGot_Item_Collect = "Gather"
const XGoo_GatherBase = "Gather"
const XGoo__XGot_Item_Apply = "GatherBase"
func Decide(value int) {}
const XGoo__XGos_Item_Choose = "Decide"
const XGoo_DecideBase = "Decide"
const XGoo__XGos_Item_Build = "DecideBase"
func AdaptImpl[T any](item Item, value T) T { return value }
const XGoo__XGot_Item_XGox_Adapt = "AdaptImpl"
func TransformImpl[T any](value T) T { return value }
const XGoo__XGox_Transform = "TransformImpl"
const XGos_Item_Limit = 4
func Independent(value int) {}
func Independent__0(value int) {}
func (Item) Ordinary() {}
func (Item) Ordinary__0() {}
`, 0)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check("example.com/encoded", fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	gogen.InitXGoPackage(pkg)

	lookup := func(t *testing.T, owner, name string) *gotypes.Func {
		t.Helper()
		obj := pkg.Scope().Lookup(name)
		if owner != "" {
			obj, _, _ = gotypes.LookupFieldOrMethod(pkg.Scope().Lookup(owner).Type(), true, pkg, name)
		}
		fun, ok := obj.(*gotypes.Func)
		require.True(t, ok)
		return fun
	}
	for _, tt := range []struct {
		name, owner, group, implementation, marker string
	}{
		{name: "Plain", group: "Put", implementation: "Put__0", marker: "XGoo_Put"},
		{name: "FunctionValue", group: "Store", marker: "XGoo_Store"},
		{name: "UnresolvedPlain", group: "Pick", marker: "XGoo__Pick"},
		{name: "Chained", group: "Chained", marker: "XGoo__Chained"},
		{name: "ReconstructedPrefix", group: "Region_Alias", marker: "XGoo_Region_Alias"},
		{name: "ReconstructedTypePrefix", group: "Choice_Alias", marker: "XGoo_Choice_Alias"},
		{name: "Escaped", group: "Put2", implementation: "Put2__0", marker: "XGoo__Put2"},
		{name: "Method", owner: "Item", group: "Read", implementation: "Read__0", marker: "XGoo_Item_Read"},
		{name: "UnresolvedPlainMethod", owner: "Item", group: "Move", marker: "XGoo__Item__Move"},
		{name: "MissingMethodEntry", owner: "Item", group: "Skip", implementation: "Skip__1", marker: "XGoo_Item_Skip"},
		{name: "MethodWithPackageImplementation", owner: "Item", group: "Draw", marker: "XGoo__Item__Draw"},
		{name: "EscapedMethod", owner: "Item_Name", group: "Read", implementation: "Read__0", marker: "XGoo__Item_Name__Read"},
		{name: "MethodMarkerSharesPackageName", group: "Item_Read", implementation: "Item_Read__0"},
		{name: "Template", owner: "Item", group: "Collect", marker: "XGoo__XGot_Item_Collect"},
		{name: "Static", owner: "Item", group: "Choose", marker: "XGoo__XGos_Item_Choose"},
		{name: "TypeAsParameters", group: "Transform", marker: "XGoo__XGox_Transform"},
		{name: "Independent", group: "Independent"},
		{name: "StaticConstant", owner: "Item", group: "Limit"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			group := lookup(t, tt.owner, tt.group)
			if tt.marker == "" {
				assert.Nil(t, OverloadGroupMarker(group))
			} else {
				assert.Same(t, pkg.Scope().Lookup(tt.marker), OverloadGroupMarker(group))
			}
			if tt.implementation != "" {
				assert.Same(t, group, OverloadGroupForImplementation(lookup(t, tt.owner, tt.implementation)))
			}
		})
	}
	assert.Nil(t, OverloadGroupForImplementation(lookup(t, "", "Independent__0")))
	assert.Nil(t, OverloadGroupForImplementation(lookup(t, "", "PutString")))
	assert.Nil(t, OverloadGroupForImplementation(lookup(t, "Item", "Ordinary__0")))

	read := lookup(t, "Item", "Read")
	show := lookup(t, "Item", "Show")
	client := gotypes.NewPackage("example.com/client", "client")
	clientLookup := NewOverloadSourceLookup(client)
	pkgLookup := NewOverloadSourceLookup(pkg)
	chained := lookup(t, "", "Chained")
	assert.Same(t, chained, clientLookup.Source(lookup(t, "", "Target"), "chained", nil))
	assert.Equal(t, []*gotypes.Func{lookup(t, "", "PutString")}, ExpandXGoOverloadableFunc(chained))
	assert.Equal(t, []gotypes.Object{pkg.Scope().Lookup("PutValue")}, ExpandXGoOverloads(lookup(t, "", "Store")))
	assert.Empty(t, ExpandXGoOverloads(lookup(t, "", "NonCallable")))
	assert.Same(t, show, clientLookup.Source(read, "show", nil))
	assert.Equal(t, []*gotypes.Func{lookup(t, "Item", "Read__0")}, ExpandXGoOverloadableFunc(show))
	assert.Same(t, lookup(t, "Item", "read"), pkgLookup.Source(read, "read", nil))
	assert.Same(t, lookup(t, "Item", "XGo_Inspect"), clientLookup.Source(read, "_Inspect", nil))
	assert.Nil(t, clientLookup.Source(read, "read", nil))
	assert.Nil(t, clientLookup.Source(read, "Read", nil))
	assert.Nil(t, pkgLookup.Source(read, "move", nil))
	assert.Nil(t, pkgLookup.Source(lookup(t, "", "PutString"), "choose", nil))

	owner := pkg.Scope().Lookup("Item").Type()
	assert.Same(t, lookup(t, "Item", "Apply"), clientLookup.Source(lookup(t, "", "GatherBase"), "Apply", owner))
	assert.Same(t, lookup(t, "Item", "Build"), clientLookup.Source(lookup(t, "", "DecideBase"), "build", owner))

	t.Run("SourceLookup", func(t *testing.T) {
		cache := NewOverloadSourceLookup(client)
		apply := lookup(t, "Item", "Apply")
		gather := lookup(t, "", "GatherBase")
		decide := lookup(t, "", "DecideBase")
		assert.Same(t, apply, cache.Source(gather, "apply", gotypes.NewPointer(owner)))
		assert.Nil(t, cache.Source(decide, "apply", gotypes.NewPointer(owner)))
		assert.Same(t, apply, cache.Source(gather, "apply", gotypes.NewPointer(owner)))
		assert.Len(t, cache.members, 1)
		assert.Nil(t, cache.Source(decide, "missing", gotypes.NewPointer(owner)))
		assert.Nil(t, cache.Source(gather, "missing", gotypes.NewPointer(owner)))
		assert.Len(t, cache.members, 2)
	})

	adapt := lookup(t, "", "AdaptImpl")
	sig, params := ResolveSourceFuncSignature(adapt, lookup(t, "Item", "Adapt"), nil)
	require.NotNil(t, sig)
	require.Equal(t, 2, params.Len())
	assert.Equal(t, 1, NormalizedCallExprTypeArgCount(adapt, params))
	assert.Same(t, adapt.Signature().TypeParams().At(0).Constraint().Underlying(), SourceParamType(params.At(0)))
	assert.Same(t, adapt.Signature().Params().At(1), params.At(1))
}

func TestXGosMemberName(t *testing.T) {
	assert.Equal(t, "XGos_Type_Name", XGosMemberName("Type", "Name"))
	assert.Equal(t, "XGos__Type__Member_Name", XGosMemberName("Type", "Member_Name"))
	assert.Equal(t, "XGos__Type_Name__Member", XGosMemberName("Type_Name", "Member"))
}
