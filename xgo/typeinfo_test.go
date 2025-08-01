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

package xgo

import (
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTypeInfoCache(t *testing.T) {
	t.Run("ValidProject", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"main.xgo": {
				Content: []byte(`
var x int = 42
var y string = "hello"

func add(a, b int) int {
	return a + b
}

func main() {
	result := add(x, 10)
	println(result, y)
}
`),
			},
		}, FeatAll)

		cache, err := buildTypeInfoCache(proj)
		require.NoError(t, err)
		require.NotNil(t, cache)

		typeInfoCache, ok := cache.(*typeInfoCache)
		require.True(t, ok)
		require.NotNil(t, typeInfoCache.typeInfo)
		assert.NoError(t, typeInfoCache.checkerErr)

		// Verify the type info structure.
		typeInfo := typeInfoCache.typeInfo
		assert.NotNil(t, typeInfo.Pkg)
		assert.Equal(t, proj.PkgPath, typeInfo.Pkg.Path())
		assert.Equal(t, "main", typeInfo.Pkg.Name())
		assert.NotEmpty(t, typeInfo.Defs)
		assert.NotEmpty(t, typeInfo.Uses)
	})

	t.Run("ASTPackageError", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"invalid.xgo": {
				Content: []byte(`invalid syntax {{{`),
			},
		}, 0) // Use minimal features to potentially cause issues

		cache, err := buildTypeInfoCache(proj)
		// Should handle AST package errors gracefully.
		if err != nil {
			assert.Error(t, err)
			assert.Nil(t, cache)
		}
	})

	t.Run("TypeCheckingError", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"main.xgo": {
				Content: []byte(`
var x int = "string" // Type error
var y = undefinedVar // Undefined variable

func test() {
	z := x + y
}
`),
			},
		}, FeatAll)

		cache, err := buildTypeInfoCache(proj)
		require.NoError(t, err)
		require.NotNil(t, cache)

		typeInfoCache, ok := cache.(*typeInfoCache)
		require.True(t, ok)
		require.NotNil(t, typeInfoCache.typeInfo)

		// Should have type checking errors.
		assert.Error(t, typeInfoCache.checkerErr)

		// But still should have some type information.
		typeInfo := typeInfoCache.typeInfo
		assert.NotNil(t, typeInfo.Pkg)
	})
}

func TestProjectTypeInfo(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"main.xgo": {
				Content: []byte(`
var counter int = 0

func increment() {
	counter = counter + 1
}

func getCounter() int {
	return counter
}
`),
			},
		}, FeatAll)

		typeInfo, err := proj.TypeInfo()
		require.NoError(t, err)
		require.NotNil(t, typeInfo)

		assert.NotNil(t, typeInfo.Pkg)
		assert.Equal(t, proj.PkgPath, typeInfo.Pkg.Path())
		assert.Equal(t, "main", typeInfo.Pkg.Name())

		// Verify that we have type information.
		assert.NotEmpty(t, typeInfo.Defs)
		assert.NotEmpty(t, typeInfo.Uses)

		// Check that counter variable is properly typed.
		var counterObj types.Object
		for ident, obj := range typeInfo.Defs {
			if ident.Name == "counter" {
				counterObj = obj
				break
			}
		}
		require.NotNil(t, counterObj)
		assert.Equal(t, "int", counterObj.Type().String())
	})

	t.Run("Cache", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"main.xgo": {
				Content: []byte(`var x int`),
			},
		}, FeatAll)

		// First call.
		typeInfo1, err1 := proj.TypeInfo()
		require.NoError(t, err1)
		require.NotNil(t, typeInfo1)

		// Second call should return the same cached instance.
		typeInfo2, err2 := proj.TypeInfo()
		require.NoError(t, err2)
		require.NotNil(t, typeInfo2)

		// Should be the same instance due to caching.
		assert.Same(t, typeInfo1, typeInfo2)
	})

	t.Run("CacheError", func(t *testing.T) {
		// Create a project without the TypeInfoCache feature enabled.
		// This will cause Cache() to return ErrUnknownCacheKind.
		proj := NewProject(nil, map[string]*File{
			"main.xgo": {
				Content: []byte(`var x int`),
			},
		}, 0) // No features enabled, so typeInfoCacheKind is not registered.

		typeInfo, err := proj.TypeInfo()
		assert.Error(t, err)
		assert.Equal(t, ErrUnknownCacheKind, err)
		assert.Nil(t, typeInfo)
	})
}
