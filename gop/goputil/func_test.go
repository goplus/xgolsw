package goputil

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsGoptMethodName(t *testing.T) {
	t.Run("ValidGoptMethodName", func(t *testing.T) {
		assert.True(t, IsGoptMethodName("Gopt_Type_Method"))
	})

	t.Run("ValidGoptMethodNameWithUnderscore", func(t *testing.T) {
		assert.True(t, IsGoptMethodName("Gopt_MyType_MyMethod"))
	})

	t.Run("InvalidPrefix", func(t *testing.T) {
		assert.False(t, IsGoptMethodName("Type_Method"))
	})

	t.Run("InvalidGopxPrefix", func(t *testing.T) {
		assert.False(t, IsGoptMethodName("Gopx_Type_Method"))
	})

	t.Run("NoUnderscore", func(t *testing.T) {
		assert.False(t, IsGoptMethodName("GoptTypeMethod"))
	})

	t.Run("OnlyPrefix", func(t *testing.T) {
		assert.False(t, IsGoptMethodName("Gopt_"))
	})

	t.Run("OnlyPrefixAndType", func(t *testing.T) {
		assert.False(t, IsGoptMethodName("Gopt_Type"))
	})

	t.Run("EmptyString", func(t *testing.T) {
		assert.False(t, IsGoptMethodName(""))
	})
}

func TestSplitGoptMethodName(t *testing.T) {
	t.Run("ValidGoptMethodName", func(t *testing.T) {
		recvTypeName, methodName, ok := SplitGoptMethodName("Gopt_Type_Method", false)
		assert.True(t, ok)
		assert.Equal(t, "Type", recvTypeName)
		assert.Equal(t, "Method", methodName)
	})

	t.Run("ValidGoptMethodNameWithGopxPrefix", func(t *testing.T) {
		recvTypeName, methodName, ok := SplitGoptMethodName("Gopt_Type_Gopx_Method", false)
		assert.True(t, ok)
		assert.Equal(t, "Type", recvTypeName)
		assert.Equal(t, "Gopx_Method", methodName)
	})

	t.Run("ValidGoptMethodNameTrimGopx", func(t *testing.T) {
		recvTypeName, methodName, ok := SplitGoptMethodName("Gopt_Type_Gopx_Method", true)
		assert.True(t, ok)
		assert.Equal(t, "Type", recvTypeName)
		assert.Equal(t, "Method", methodName)
	})

	t.Run("InvalidPrefix", func(t *testing.T) {
		_, _, ok := SplitGoptMethodName("Type_Method", false)
		assert.False(t, ok)
	})

	t.Run("NoUnderscore", func(t *testing.T) {
		_, _, ok := SplitGoptMethodName("GoptTypeMethod", false)
		assert.False(t, ok)
	})

	t.Run("OnlyPrefix", func(t *testing.T) {
		_, _, ok := SplitGoptMethodName("Gopt_", false)
		assert.False(t, ok)
	})

	t.Run("OnlyPrefixAndType", func(t *testing.T) {
		_, _, ok := SplitGoptMethodName("Gopt_Type", false)
		assert.False(t, ok)
	})

	t.Run("EmptyString", func(t *testing.T) {
		_, _, ok := SplitGoptMethodName("", false)
		assert.False(t, ok)
	})

	t.Run("MultipleUnderscores", func(t *testing.T) {
		recvTypeName, methodName, ok := SplitGoptMethodName("Gopt_MyType_My_Method", false)
		assert.True(t, ok)
		assert.Equal(t, "MyType", recvTypeName)
		assert.Equal(t, "My_Method", methodName)
	})
}

func TestSplitGopxFuncName(t *testing.T) {
	t.Run("ValidGopxFuncName", func(t *testing.T) {
		funcName, ok := SplitGopxFuncName("Gopx_Method")
		assert.True(t, ok)
		assert.Equal(t, "Method", funcName)
	})

	t.Run("ValidGopxFuncNameWithUnderscores", func(t *testing.T) {
		funcName, ok := SplitGopxFuncName("Gopx_My_Method")
		assert.True(t, ok)
		assert.Equal(t, "My_Method", funcName)
	})

	t.Run("InvalidPrefix", func(t *testing.T) {
		_, ok := SplitGopxFuncName("Method")
		assert.False(t, ok)
	})

	t.Run("InvalidGoptPrefix", func(t *testing.T) {
		_, ok := SplitGopxFuncName("Gopt_Method")
		assert.False(t, ok)
	})

	t.Run("OnlyPrefix", func(t *testing.T) {
		funcName, ok := SplitGopxFuncName("Gopx_")
		assert.True(t, ok)
		assert.Equal(t, "", funcName)
	})

	t.Run("EmptyString", func(t *testing.T) {
		_, ok := SplitGopxFuncName("")
		assert.False(t, ok)
	})
}

func TestParseGopFuncName(t *testing.T) {
	t.Run("RegularFunctionName", func(t *testing.T) {
		parsedName, overloadID := ParseGopFuncName("MyFunction")
		assert.Equal(t, "myFunction", parsedName)
		assert.Nil(t, overloadID)
	})

	t.Run("OverloadedFunctionName", func(t *testing.T) {
		parsedName, overloadID := ParseGopFuncName("MyFunction__a")
		assert.Equal(t, "myFunction", parsedName)
		assert.NotNil(t, overloadID)
		assert.Equal(t, "a", *overloadID)
	})

	t.Run("OverloadedFunctionNameWithNumber", func(t *testing.T) {
		parsedName, overloadID := ParseGopFuncName("MyFunction__1")
		assert.Equal(t, "myFunction", parsedName)
		assert.NotNil(t, overloadID)
		assert.Equal(t, "1", *overloadID)
	})

	t.Run("OverloadedFunctionNameWithZero", func(t *testing.T) {
		parsedName, overloadID := ParseGopFuncName("MyFunction__0")
		assert.Equal(t, "myFunction", parsedName)
		assert.NotNil(t, overloadID)
		assert.Equal(t, "0", *overloadID)
	})

	t.Run("FunctionNameWithSingleUnderscore", func(t *testing.T) {
		parsedName, overloadID := ParseGopFuncName("My_Function")
		assert.Equal(t, "my_Function", parsedName)
		assert.Nil(t, overloadID)
	})

	t.Run("FunctionNameWithDoubleUnderscoreButInvalidSuffix", func(t *testing.T) {
		parsedName, overloadID := ParseGopFuncName("MyFunction__AA")
		assert.Equal(t, "myFunction__AA", parsedName)
		assert.Nil(t, overloadID)
	})

	t.Run("EmptyString", func(t *testing.T) {
		parsedName, overloadID := ParseGopFuncName("")
		assert.Equal(t, "", parsedName)
		assert.Nil(t, overloadID)
	})

	t.Run("PascalCaseFunction", func(t *testing.T) {
		parsedName, overloadID := ParseGopFuncName("PascalCaseFunction")
		assert.Equal(t, "pascalCaseFunction", parsedName)
		assert.Nil(t, overloadID)
	})
}

func TestIsGopOverloadedFuncName(t *testing.T) {
	t.Run("ValidOverloadedName", func(t *testing.T) {
		assert.True(t, IsGopOverloadedFuncName("MyFunction__a"))
	})

	t.Run("ValidOverloadedNameWithNumber", func(t *testing.T) {
		assert.True(t, IsGopOverloadedFuncName("MyFunction__1"))
	})

	t.Run("ValidOverloadedNameWithZero", func(t *testing.T) {
		assert.True(t, IsGopOverloadedFuncName("MyFunction__0"))
	})

	t.Run("RegularFunctionName", func(t *testing.T) {
		assert.False(t, IsGopOverloadedFuncName("MyFunction"))
	})

	t.Run("FunctionNameWithSingleUnderscore", func(t *testing.T) {
		assert.False(t, IsGopOverloadedFuncName("My_Function"))
	})

	t.Run("FunctionNameWithDoubleUnderscoreButInvalidSuffix", func(t *testing.T) {
		assert.False(t, IsGopOverloadedFuncName("MyFunction__AA"))
	})

	t.Run("FunctionNameWithDoubleUnderscoreButEmptySuffix", func(t *testing.T) {
		assert.False(t, IsGopOverloadedFuncName("MyFunction__"))
	})

	t.Run("EmptyString", func(t *testing.T) {
		assert.False(t, IsGopOverloadedFuncName(""))
	})

	t.Run("OnlyDoubleUnderscore", func(t *testing.T) {
		assert.False(t, IsGopOverloadedFuncName("__"))
	})
}

func TestIsGopOverloadableFunc(t *testing.T) {
	t.Run("RegularFunction", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
		fun := types.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.False(t, IsGopOverloadableFunc(fun))
	})

	t.Run("FunctionWithIntParameter", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		params := types.NewTuple(types.NewParam(token.NoPos, pkg, "x", types.Typ[types.Int]))
		sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := types.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.False(t, IsGopOverloadableFunc(fun))
	})

	t.Run("FunctionWithMultipleParameters", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		params := types.NewTuple(
			types.NewParam(token.NoPos, pkg, "x", types.Typ[types.Int]),
			types.NewParam(token.NoPos, pkg, "y", types.Typ[types.String]),
		)
		sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := types.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.False(t, IsGopOverloadableFunc(fun))
	})
}

func TestIsUnexpandableGopOverloadableFunc(t *testing.T) {
	t.Run("RegularFunction", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
		fun := types.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.False(t, IsUnexpandableGopOverloadableFunc(fun))
	})

	t.Run("FunctionWithIntParameter", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		params := types.NewTuple(types.NewParam(token.NoPos, pkg, "x", types.Typ[types.Int]))
		sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := types.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.False(t, IsUnexpandableGopOverloadableFunc(fun))
	})

	t.Run("FunctionWithMultipleParameters", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		params := types.NewTuple(
			types.NewParam(token.NoPos, pkg, "x", types.Typ[types.Int]),
			types.NewParam(token.NoPos, pkg, "y", types.Typ[types.String]),
		)
		sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := types.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.False(t, IsUnexpandableGopOverloadableFunc(fun))
	})
}

func TestExpandGopOverloadableFunc(t *testing.T) {
	t.Run("RegularFunction", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
		fun := types.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.Nil(t, ExpandGopOverloadableFunc(fun))
	})

	t.Run("FunctionWithIntParameter", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		params := types.NewTuple(types.NewParam(token.NoPos, pkg, "x", types.Typ[types.Int]))
		sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := types.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.Nil(t, ExpandGopOverloadableFunc(fun))
	})

	t.Run("FunctionWithMultipleParameters", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		params := types.NewTuple(
			types.NewParam(token.NoPos, pkg, "x", types.Typ[types.Int]),
			types.NewParam(token.NoPos, pkg, "y", types.Typ[types.String]),
		)
		sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := types.NewFunc(token.NoPos, pkg, "TestFunc", sig)
		assert.Nil(t, ExpandGopOverloadableFunc(fun))
	})
}
