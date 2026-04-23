/*
 * Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
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

	"github.com/goplus/mod/xgomod"
	"github.com/goplus/xgolsw/internal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSpxProjectForTest(files map[string]*File, feats uint) *Project {
	proj := NewProject(nil, files, feats)
	proj.PkgPath = "main"
	proj.Importer = internal.Importer
	return proj
}

func TestLoadClassfileResourceSchema(t *testing.T) {
	project, ok := xgomod.Default.LookupClass(".spx")
	require.True(t, ok)

	schema, err := LoadClassfileResourceSchema(project, internal.Importer)
	require.NoError(t, err)

	spriteKind, ok := schema.Kind("sprite")
	require.True(t, ok)
	require.NotNil(t, spriteKind.CanonicalType)

	spriteObj, ok := schema.Package.Scope().Lookup("Sprite").Type().(*types.Named)
	require.True(t, ok)
	handleKind, ok := schema.HandleKindOfType(spriteObj)
	require.True(t, ok)
	assert.Same(t, spriteKind, handleKind)

	spriteIface := spriteObj.Underlying().(*types.Interface)
	var setCostume *types.Func
	for method := range spriteIface.ExplicitMethods() {
		if method.Name() == "SetCostume__0" {
			setCostume = method
			break
		}
	}
	require.NotNil(t, setCostume)

	bindings := schema.APIScopeBindings(setCostume)
	require.Len(t, bindings, 1)
	assert.Equal(t, 0, bindings[0].TargetParam)
	assert.True(t, bindings[0].SourceReceiver)
}

func TestProjectClassfileResourceSet(t *testing.T) {
	t.Run("ImpliedWorkResource", func(t *testing.T) {
		proj := newSpxProjectForTest(map[string]*File{
			"main.spx":          file("onStart => {}\n"),
			"Hero.spx":          file("onStart => {}\n"),
			"assets/index.json": file(`{}`),
		}, FeatAll)

		resourceSet, err := proj.ClassfileResourceSet(".spx")
		require.NoError(t, err)

		sprites := resourceSet.Resources("sprite")
		require.Len(t, sprites, 1)
		assert.Equal(t, "Hero", sprites[0].Name)
		assert.Empty(t, resourceSet.Children(sprites[0], "sprite.costume"))
	})

	t.Run("CacheInvalidation", func(t *testing.T) {
		proj := newSpxProjectForTest(map[string]*File{
			"main.spx":          file("onStart => {}\n"),
			"Hero.spx":          file("onStart => {}\n"),
			"assets/index.json": file(`{}`),
		}, FeatAll)

		resourceSet1, err := proj.ClassfileResourceSet(".spx")
		require.NoError(t, err)
		resourceSet2, err := proj.ClassfileResourceSet(".spx")
		require.NoError(t, err)
		assert.Same(t, resourceSet1, resourceSet2)

		proj.PutFile("assets/sprites/Hero/index.json", file(`{"costumes":[{"name":"idle"}]}`))

		resourceSet3, err := proj.ClassfileResourceSet(".spx")
		require.NoError(t, err)
		assert.NotSame(t, resourceSet1, resourceSet3)

		sprites := resourceSet3.Resources("sprite")
		require.Len(t, sprites, 1)
		costumes := resourceSet3.Children(sprites[0], "sprite.costume")
		require.Len(t, costumes, 1)
		assert.Equal(t, "idle", costumes[0].Name)
	})
}

func TestProjectClassfileResourceInfo(t *testing.T) {
	t.Run("References", func(t *testing.T) {
		proj := newSpxProjectForTest(map[string]*File{
			"main.spx": file(`
const Backdrop1 BackdropName = "backdrop1"
const Backdrop1a = Backdrop1
onStart => {
	var spriteName SpriteName = "MySprite"
	spriteName = "MySprite"
	play ""
	play "MissingSound"
}
`),
			"MySprite.spx": file(`
onStart => {
	MySprite.setCostume "costume1"
	setCostume "costume1"
}
`),
			"assets/index.json":                  file(`{"backdrops":[{"name":"backdrop1"}]}`),
			"assets/sprites/MySprite/index.json": file(`{"costumes":[{"name":"costume1"}]}`),
		}, FeatClassfileResourceCache)

		info, err := proj.ClassfileResourceInfo(".spx")
		require.NoError(t, err)

		assertClassfileResourceReference(t, info, "backdrop", "backdrop1", "", ClassfileResourceReferenceStringLiteral, ClassfileResourceReferenceResolved)
		assertClassfileResourceReference(t, info, "backdrop", "backdrop1", "", ClassfileResourceReferenceConstant, ClassfileResourceReferenceResolved)
		assertClassfileResourceReference(t, info, "sprite", "MySprite", "", ClassfileResourceReferenceStringLiteral, ClassfileResourceReferenceResolved)
		assertClassfileResourceReference(t, info, "sprite", "MySprite", "", ClassfileResourceReferenceHandleExpression, ClassfileResourceReferenceResolved)
		assertClassfileResourceReference(t, info, "sprite.costume", "costume1", "MySprite", ClassfileResourceReferenceStringLiteral, ClassfileResourceReferenceResolved)
		assertClassfileResourceReference(t, info, "sound", "", "", ClassfileResourceReferenceStringLiteral, ClassfileResourceReferenceEmptyName)
		assertClassfileResourceReference(t, info, "sound", "MissingSound", "", ClassfileResourceReferenceStringLiteral, ClassfileResourceReferenceNotFound)
	})

	t.Run("CacheInvalidation", func(t *testing.T) {
		proj := newSpxProjectForTest(map[string]*File{
			"main.spx":                        file("onStart => {\nplay \"Sound1\"\n}\n"),
			"assets/index.json":               file(`{}`),
			"assets/sounds/Sound1/index.json": file(`{}`),
			"assets/sounds/Sound2/index.json": file(`{}`),
		}, FeatClassfileResourceCache)

		info1, err := proj.ClassfileResourceInfo(".spx")
		require.NoError(t, err)
		info2, err := proj.ClassfileResourceInfo(".spx")
		require.NoError(t, err)
		assert.Same(t, info1, info2)

		proj.PutFile("main.spx", file("onStart => {\nplay \"Sound2\"\n}\n"))

		info3, err := proj.ClassfileResourceInfo(".spx")
		require.NoError(t, err)
		assert.NotSame(t, info1, info3)
		assertClassfileResourceReference(t, info3, "sound", "Sound2", "", ClassfileResourceReferenceStringLiteral, ClassfileResourceReferenceResolved)
	})
}

func assertClassfileResourceReference(
	t *testing.T,
	info *ClassfileResourceInfo,
	kind string,
	name string,
	parent string,
	source ClassfileResourceReferenceSource,
	status ClassfileResourceReferenceStatus,
) {
	t.Helper()
	for _, ref := range info.References() {
		if ref.Kind.Name != kind || ref.Name != name || ref.Source != source || ref.Status != status {
			continue
		}
		if parent == "" {
			if ref.Parent == nil {
				return
			}
			continue
		}
		if ref.Parent != nil && ref.Parent.Name == parent {
			return
		}
	}
	require.Failf(t, "expected resource reference", "kind=%s name=%s parent=%s source=%s status=%s", kind, name, parent, source, status)
}
