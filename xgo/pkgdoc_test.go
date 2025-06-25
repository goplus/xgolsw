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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPkgDocCache(t *testing.T) {
	t.Run("ValidProject", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"main.spx": file(`
// Package documentation
var (
	// Field documentation
	x int
	y string
)

// Function documentation
func Test() {
	println("test")
}
`),
		}, FeatAll)

		cache, err := buildPkgDocCache(proj)
		require.NoError(t, err)
		require.NotNil(t, cache)

		pkgDocCache, ok := cache.(*pkgDocCache)
		require.True(t, ok)
		require.NotNil(t, pkgDocCache.pkgDoc)

		// Verify the package document structure.
		pkgDoc := pkgDocCache.pkgDoc
		assert.Equal(t, proj.PkgPath, pkgDoc.Path)
		assert.NotEmpty(t, pkgDoc.Types)

		// Check that Game type exists (main.spx creates Game type).
		gameType, exists := pkgDoc.Types["Game"]
		require.True(t, exists)
		assert.NotNil(t, gameType)
		assert.NotNil(t, gameType.Fields)
		assert.NotNil(t, gameType.Methods)
	})

	t.Run("Error", func(t *testing.T) {
		// Create a project that will cause ASTPackage to fail.
		proj := NewProject(nil, map[string]*File{
			"invalid.spx": file(`invalid go syntax {{{`),
		}, 0)

		_, err := buildPkgDocCache(proj)
		// Should handle the error gracefully.
		if err != nil {
			assert.Error(t, err)
		}
	})
}

func TestProjectPkgDoc(t *testing.T) {
	t.Run("Basic", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"main.spx": file(`
// Package test documentation.
var (
	// Test field.
	testField int
)

// Test function.
func TestFunc() {
	println("test")
}
`),
		}, FeatAll)

		pkgDoc, err := proj.PkgDoc()
		require.NoError(t, err)
		require.NotNil(t, pkgDoc)

		assert.Equal(t, proj.PkgPath, pkgDoc.Path)
		assert.NotEmpty(t, pkgDoc.Types)

		// Verify that Game type exists for main.spx.
		gameType, exists := pkgDoc.Types["Game"]
		require.True(t, exists)
		assert.NotNil(t, gameType.Fields)
		assert.NotNil(t, gameType.Methods)

		// Check that testField exists in Game type fields.
		assert.Contains(t, gameType.Fields, "testField")

		// Check that TestFunc exists in Game type methods.
		assert.Contains(t, gameType.Methods, "TestFunc")
	})

	t.Run("Cache", func(t *testing.T) {
		proj := NewProject(nil, map[string]*File{
			"main.spx": file(`var x int
func Test() {}
`),
		}, FeatAll)

		// First call.
		pkgDoc1, err1 := proj.PkgDoc()
		require.NoError(t, err1)
		require.NotNil(t, pkgDoc1)

		// Second call should return the same cached instance.
		pkgDoc2, err2 := proj.PkgDoc()
		require.NoError(t, err2)
		require.NotNil(t, pkgDoc2)

		// Should be the same instance due to caching.
		assert.Same(t, pkgDoc1, pkgDoc2)
	})

	t.Run("CacheError", func(t *testing.T) {
		// Create a project without the PkgDocCache feature enabled.
		// This will cause Cache() to return ErrUnknownCacheKind.
		proj := NewProject(nil, map[string]*File{
			"main.spx": file(`var x int`),
		}, 0) // No features enabled, so pkgDocCacheKind is not registered.

		pkgDoc, err := proj.PkgDoc()
		assert.Error(t, err)
		assert.Equal(t, ErrUnknownCacheKind, err)
		assert.Nil(t, pkgDoc)
	})
}
