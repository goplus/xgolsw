package server

import (
	gotypes "go/types"
	"sync"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHasSpxResourceNameTypeParams(t *testing.T) {
	nonMainPkgSpxResourceNameTypeFuncCache = sync.Map{}

	for _, tt := range []struct {
		name string
		fun  func() *gotypes.Func
		want bool
	}{
		{
			name: "NilFunction",
			fun: func() *gotypes.Func {
				return nil
			},
			want: false,
		},
		{
			name: "FunctionWithNoParameters",
			fun: func() *gotypes.Func {
				pkg := gotypes.NewPackage("test", "test")
				sig := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
				return gotypes.NewFunc(token.NoPos, pkg, "noParams", sig)
			},
			want: false,
		},
		{
			name: "FunctionWithBasicTypeParameters",
			fun: func() *gotypes.Func {
				pkg := gotypes.NewPackage("test", "test")
				param1 := gotypes.NewParam(token.NoPos, pkg, "p1", gotypes.Typ[gotypes.Int])
				param2 := gotypes.NewParam(token.NoPos, pkg, "p2", gotypes.Typ[gotypes.String])
				params := gotypes.NewTuple(param1, param2)
				sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
				return gotypes.NewFunc(token.NoPos, pkg, "basicParams", sig)
			},
			want: false,
		},
		{
			name: "FunctionWithBackdropNameParameter",
			fun: func() *gotypes.Func {
				pkg := GetSpxPkg()
				param := gotypes.NewParam(token.NoPos, pkg, "backdrop", GetSpxBackdropNameType())
				params := gotypes.NewTuple(param)
				sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
				return gotypes.NewFunc(token.NoPos, pkg, "withBackdrop", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithSpriteNameParameter",
			fun: func() *gotypes.Func {
				pkg := GetSpxPkg()
				param := gotypes.NewParam(token.NoPos, pkg, "sprite", GetSpxSpriteNameType())
				params := gotypes.NewTuple(param)
				sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
				return gotypes.NewFunc(token.NoPos, pkg, "withSprite", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithSpriteCostumeNameParameter",
			fun: func() *gotypes.Func {
				pkg := GetSpxPkg()
				param := gotypes.NewParam(token.NoPos, pkg, "costume", GetSpxSpriteCostumeNameType())
				params := gotypes.NewTuple(param)
				sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
				return gotypes.NewFunc(token.NoPos, pkg, "withCostume", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithSpriteAnimationNameParameter",
			fun: func() *gotypes.Func {
				pkg := GetSpxPkg()
				param := gotypes.NewParam(token.NoPos, pkg, "animation", GetSpxSpriteAnimationNameType())
				params := gotypes.NewTuple(param)
				sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
				return gotypes.NewFunc(token.NoPos, pkg, "withAnimation", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithSoundNameParameter",
			fun: func() *gotypes.Func {
				pkg := GetSpxPkg()
				param := gotypes.NewParam(token.NoPos, pkg, "sound", GetSpxSoundNameType())
				params := gotypes.NewTuple(param)
				sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
				return gotypes.NewFunc(token.NoPos, pkg, "withSound", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithWidgetNameParameter",
			fun: func() *gotypes.Func {
				pkg := GetSpxPkg()
				param := gotypes.NewParam(token.NoPos, pkg, "widget", GetSpxWidgetNameType())
				params := gotypes.NewTuple(param)
				sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
				return gotypes.NewFunc(token.NoPos, pkg, "withWidget", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithAliasToSoundNameParameter",
			fun: func() *gotypes.Func {
				pkg := GetSpxPkg()
				aliasType := gotypes.NewAlias(
					gotypes.NewTypeName(token.NoPos, pkg, "MySoundName", nil),
					GetSpxSoundNameType(),
				)
				param := gotypes.NewParam(token.NoPos, pkg, "sound", aliasType)
				params := gotypes.NewTuple(param)
				sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
				return gotypes.NewFunc(token.NoPos, pkg, "withAliasSound", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithPointerToBackdropNameParameter",
			fun: func() *gotypes.Func {
				pkg := GetSpxPkg()
				ptrType := gotypes.NewPointer(GetSpxBackdropNameType())
				param := gotypes.NewParam(token.NoPos, pkg, "backdrop", ptrType)
				params := gotypes.NewTuple(param)
				sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
				return gotypes.NewFunc(token.NoPos, pkg, "withBackdropPtr", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithSliceOfSpriteNameParameter",
			fun: func() *gotypes.Func {
				pkg := GetSpxPkg()
				sliceType := gotypes.NewSlice(GetSpxSpriteNameType())
				param := gotypes.NewParam(token.NoPos, pkg, "sprites", sliceType)
				params := gotypes.NewTuple(param)
				sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
				return gotypes.NewFunc(token.NoPos, pkg, "withSpriteSlice", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithVariadicSoundNameParameter",
			fun: func() *gotypes.Func {
				pkg := GetSpxPkg()
				sliceType := gotypes.NewSlice(GetSpxSoundNameType())
				param := gotypes.NewParam(token.NoPos, pkg, "sounds", sliceType)
				params := gotypes.NewTuple(param)
				sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, true) // variadic = true
				return gotypes.NewFunc(token.NoPos, pkg, "withVariadicSounds", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithMixedParameters",
			fun: func() *gotypes.Func {
				pkg := GetSpxPkg()
				param1 := gotypes.NewParam(token.NoPos, pkg, "id", gotypes.Typ[gotypes.Int])
				param2 := gotypes.NewParam(token.NoPos, pkg, "backdrop", GetSpxBackdropNameType())
				param3 := gotypes.NewParam(token.NoPos, pkg, "name", gotypes.Typ[gotypes.String])
				params := gotypes.NewTuple(param1, param2, param3)
				sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
				return gotypes.NewFunc(token.NoPos, pkg, "withMixed", sig)
			},
			want: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fun := tt.fun()
			got := HasSpxResourceNameTypeParams(fun)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHasSpxResourceNameTypeParamsCaching(t *testing.T) {
	nonMainPkgSpxResourceNameTypeFuncCache = sync.Map{}

	t.Run("SpxPackageFunctionIsCached", func(t *testing.T) {
		pkg := GetSpxPkg()
		param := gotypes.NewParam(token.NoPos, pkg, "backdrop", GetSpxBackdropNameType())
		params := gotypes.NewTuple(param)
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := gotypes.NewFunc(token.NoPos, pkg, "testFunc", sig)

		result1 := HasSpxResourceNameTypeParams(fun)
		assert.True(t, result1)

		cached, ok := nonMainPkgSpxResourceNameTypeFuncCache.Load(fun)
		require.True(t, ok)
		cachedValue := requireValueAs[bool](t, cached)
		assert.True(t, cachedValue)

		result2 := HasSpxResourceNameTypeParams(fun)
		assert.True(t, result2)
		assert.Equal(t, result1, result2)
	})

	t.Run("MainPackageFunctionIsNotCached", func(t *testing.T) {
		mainPkg := gotypes.NewPackage("main", "main")
		param := gotypes.NewParam(token.NoPos, mainPkg, "backdrop", GetSpxBackdropNameType())
		params := gotypes.NewTuple(param)
		sig := gotypes.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := gotypes.NewFunc(token.NoPos, mainPkg, "mainFunc", sig)

		result := HasSpxResourceNameTypeParams(fun)
		assert.True(t, result)

		_, ok := nonMainPkgSpxResourceNameTypeFuncCache.Load(fun)
		assert.False(t, ok)
	})
}

func TestCanonicalSpxResourceNameType(t *testing.T) {
	pkg := gotypes.NewPackage("example.com/pkg", "pkg")
	soundAlias := gotypes.NewAlias(
		gotypes.NewTypeName(token.NoPos, pkg, "MySoundName", nil),
		GetSpxSoundNameType(),
	)
	soundAliasChain := gotypes.NewAlias(
		gotypes.NewTypeName(token.NoPos, pkg, "MySoundNameChain", nil),
		soundAlias,
	)

	for _, tt := range []struct {
		name string
		typ  gotypes.Type
		want gotypes.Type
	}{
		{
			name: "Nil",
			typ:  nil,
			want: nil,
		},
		{
			name: "DirectBackdropName",
			typ:  GetSpxBackdropNameType(),
			want: GetSpxBackdropNameType(),
		},
		{
			name: "AliasToSoundName",
			typ:  soundAlias,
			want: GetSpxSoundNameType(),
		},
		{
			name: "AliasChainToSoundName",
			typ:  soundAliasChain,
			want: GetSpxSoundNameType(),
		},
		{
			name: "BasicString",
			typ:  gotypes.Typ[gotypes.String],
			want: nil,
		},
		{
			name: "AliasToBasicString",
			typ:  gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "MyString", nil), gotypes.Typ[gotypes.String]),
			want: nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalSpxResourceNameType(tt.typ)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Same(t, tt.want, got)
		})
	}
}
