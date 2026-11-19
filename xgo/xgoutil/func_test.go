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

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func TestXGosMemberName(t *testing.T) {
	assert.Equal(t, "XGos_Type_Name", XGosMemberName("Type", "Name"))
	assert.Equal(t, "XGos__Type__Member_Name", XGosMemberName("Type", "Member_Name"))
	assert.Equal(t, "XGos__Type_Name__Member", XGosMemberName("Type_Name", "Member"))
}
