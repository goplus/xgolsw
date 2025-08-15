package server

import (
	"go/token"
	"go/types"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHasSpxResourceNameTypeParams(t *testing.T) {
	nonMainPkgSpxResourceNameTypeFuncCache = sync.Map{}

	for _, tt := range []struct {
		name string
		fun  func() *types.Func
		want bool
	}{
		{
			name: "NilFunction",
			fun: func() *types.Func {
				return nil
			},
			want: false,
		},
		{
			name: "FunctionWithNoParameters",
			fun: func() *types.Func {
				pkg := types.NewPackage("test", "test")
				sig := types.NewSignatureType(nil, nil, nil, nil, nil, false)
				return types.NewFunc(token.NoPos, pkg, "noParams", sig)
			},
			want: false,
		},
		{
			name: "FunctionWithBasicTypeParameters",
			fun: func() *types.Func {
				pkg := types.NewPackage("test", "test")
				param1 := types.NewParam(token.NoPos, pkg, "p1", types.Typ[types.Int])
				param2 := types.NewParam(token.NoPos, pkg, "p2", types.Typ[types.String])
				params := types.NewTuple(param1, param2)
				sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
				return types.NewFunc(token.NoPos, pkg, "basicParams", sig)
			},
			want: false,
		},
		{
			name: "FunctionWithBackdropNameParameter",
			fun: func() *types.Func {
				pkg := GetSpxPkg()
				param := types.NewParam(token.NoPos, pkg, "backdrop", GetSpxBackdropNameType())
				params := types.NewTuple(param)
				sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
				return types.NewFunc(token.NoPos, pkg, "withBackdrop", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithSpriteNameParameter",
			fun: func() *types.Func {
				pkg := GetSpxPkg()
				param := types.NewParam(token.NoPos, pkg, "sprite", GetSpxSpriteNameType())
				params := types.NewTuple(param)
				sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
				return types.NewFunc(token.NoPos, pkg, "withSprite", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithSpriteCostumeNameParameter",
			fun: func() *types.Func {
				pkg := GetSpxPkg()
				param := types.NewParam(token.NoPos, pkg, "costume", GetSpxSpriteCostumeNameType())
				params := types.NewTuple(param)
				sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
				return types.NewFunc(token.NoPos, pkg, "withCostume", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithSpriteAnimationNameParameter",
			fun: func() *types.Func {
				pkg := GetSpxPkg()
				param := types.NewParam(token.NoPos, pkg, "animation", GetSpxSpriteAnimationNameType())
				params := types.NewTuple(param)
				sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
				return types.NewFunc(token.NoPos, pkg, "withAnimation", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithSoundNameParameter",
			fun: func() *types.Func {
				pkg := GetSpxPkg()
				param := types.NewParam(token.NoPos, pkg, "sound", GetSpxSoundNameType())
				params := types.NewTuple(param)
				sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
				return types.NewFunc(token.NoPos, pkg, "withSound", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithWidgetNameParameter",
			fun: func() *types.Func {
				pkg := GetSpxPkg()
				param := types.NewParam(token.NoPos, pkg, "widget", GetSpxWidgetNameType())
				params := types.NewTuple(param)
				sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
				return types.NewFunc(token.NoPos, pkg, "withWidget", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithPointerToBackdropNameParameter",
			fun: func() *types.Func {
				pkg := GetSpxPkg()
				ptrType := types.NewPointer(GetSpxBackdropNameType())
				param := types.NewParam(token.NoPos, pkg, "backdrop", ptrType)
				params := types.NewTuple(param)
				sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
				return types.NewFunc(token.NoPos, pkg, "withBackdropPtr", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithSliceOfSpriteNameParameter",
			fun: func() *types.Func {
				pkg := GetSpxPkg()
				sliceType := types.NewSlice(GetSpxSpriteNameType())
				param := types.NewParam(token.NoPos, pkg, "sprites", sliceType)
				params := types.NewTuple(param)
				sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
				return types.NewFunc(token.NoPos, pkg, "withSpriteSlice", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithVariadicSoundNameParameter",
			fun: func() *types.Func {
				pkg := GetSpxPkg()
				sliceType := types.NewSlice(GetSpxSoundNameType())
				param := types.NewParam(token.NoPos, pkg, "sounds", sliceType)
				params := types.NewTuple(param)
				sig := types.NewSignatureType(nil, nil, nil, params, nil, true) // variadic = true
				return types.NewFunc(token.NoPos, pkg, "withVariadicSounds", sig)
			},
			want: true,
		},
		{
			name: "FunctionWithMixedParameters",
			fun: func() *types.Func {
				pkg := GetSpxPkg()
				param1 := types.NewParam(token.NoPos, pkg, "id", types.Typ[types.Int])
				param2 := types.NewParam(token.NoPos, pkg, "backdrop", GetSpxBackdropNameType())
				param3 := types.NewParam(token.NoPos, pkg, "name", types.Typ[types.String])
				params := types.NewTuple(param1, param2, param3)
				sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
				return types.NewFunc(token.NoPos, pkg, "withMixed", sig)
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
		param := types.NewParam(token.NoPos, pkg, "backdrop", GetSpxBackdropNameType())
		params := types.NewTuple(param)
		sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := types.NewFunc(token.NoPos, pkg, "testFunc", sig)

		result1 := HasSpxResourceNameTypeParams(fun)
		assert.True(t, result1)

		cached, ok := nonMainPkgSpxResourceNameTypeFuncCache.Load(fun)
		assert.True(t, ok)
		assert.True(t, cached.(bool))

		result2 := HasSpxResourceNameTypeParams(fun)
		assert.True(t, result2)
		assert.Equal(t, result1, result2)
	})

	t.Run("MainPackageFunctionIsNotCached", func(t *testing.T) {
		mainPkg := types.NewPackage("main", "main")
		param := types.NewParam(token.NoPos, mainPkg, "backdrop", GetSpxBackdropNameType())
		params := types.NewTuple(param)
		sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
		fun := types.NewFunc(token.NoPos, mainPkg, "mainFunc", sig)

		result := HasSpxResourceNameTypeParams(fun)
		assert.True(t, result)

		_, ok := nonMainPkgSpxResourceNameTypeFuncCache.Load(fun)
		assert.False(t, ok)
	})
}
