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
			name: "FunctionWithAliasToSoundNameParameter",
			fun: func() *types.Func {
				pkg := GetSpxPkg()
				aliasType := types.NewAlias(
					types.NewTypeName(token.NoPos, pkg, "MySoundName", nil),
					GetSpxSoundNameType(),
				)
				param := types.NewParam(token.NoPos, pkg, "sound", aliasType)
				params := types.NewTuple(param)
				sig := types.NewSignatureType(nil, nil, nil, params, nil, false)
				return types.NewFunc(token.NoPos, pkg, "withAliasSound", sig)
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

func TestCanonicalSpxResourceNameType(t *testing.T) {
	pkg := types.NewPackage("example.com/pkg", "pkg")
	soundAlias := types.NewAlias(
		types.NewTypeName(token.NoPos, pkg, "MySoundName", nil),
		GetSpxSoundNameType(),
	)
	soundAliasChain := types.NewAlias(
		types.NewTypeName(token.NoPos, pkg, "MySoundNameChain", nil),
		soundAlias,
	)

	for _, tt := range []struct {
		name string
		typ  types.Type
		want types.Type
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
			typ:  types.Typ[types.String],
			want: nil,
		},
		{
			name: "AliasToBasicString",
			typ:  types.NewAlias(types.NewTypeName(token.NoPos, pkg, "MyString", nil), types.Typ[types.String]),
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

func TestGetSpxDefinitionForVarKinds(t *testing.T) {
	pkg := types.NewPackage("main", "main")

	newVar := func(name string, kind types.VarKind) *types.Var {
		v := types.NewVar(token.NoPos, pkg, name, types.Typ[types.Int])
		v.SetKind(kind)
		return v
	}
	newParam := func(name string, kind types.VarKind) *types.Var {
		v := types.NewParam(token.NoPos, pkg, name, types.Typ[types.Int])
		v.SetKind(kind)
		return v
	}

	tests := []struct {
		name               string
		v                  *types.Var
		selectorTypeName   string
		forceVar           bool
		wantOverviewPrefix string
		wantItemKind       CompletionItemKind
	}{
		{
			name:               "PackageVar",
			v:                  newVar("pkgVar", types.PackageVar),
			wantOverviewPrefix: "var pkgVar int",
			wantItemKind:       VariableCompletion,
		},
		{
			name:               "LocalVar",
			v:                  newVar("localVar", types.LocalVar),
			wantOverviewPrefix: "var localVar int",
			wantItemKind:       VariableCompletion,
		},
		{
			name:               "ParamVar",
			v:                  newParam("paramVar", types.ParamVar),
			wantOverviewPrefix: "param paramVar int",
			wantItemKind:       VariableCompletion,
		},
		{
			name:               "OptionalParamVar",
			v:                  newParam("optionalParamVar", optionalParamVarKind),
			wantOverviewPrefix: "param optionalParamVar int",
			wantItemKind:       VariableCompletion,
		},
		{
			name:               "RecvVar",
			v:                  newParam("recvVar", types.RecvVar),
			wantOverviewPrefix: "recv recvVar int",
			wantItemKind:       VariableCompletion,
		},
		{
			name:               "ResultVar",
			v:                  newParam("resultVar", types.ResultVar),
			wantOverviewPrefix: "result resultVar int",
			wantItemKind:       VariableCompletion,
		},
		{
			name:               "FieldVar",
			v:                  types.NewField(token.NoPos, pkg, "fieldVar", types.Typ[types.Int], false),
			selectorTypeName:   "Sprite",
			wantOverviewPrefix: "field fieldVar int",
			wantItemKind:       FieldCompletion,
		},
		{
			name:               "ForcedFieldVar",
			v:                  types.NewField(token.NoPos, pkg, "forcedFieldVar", types.Typ[types.Int], false),
			selectorTypeName:   "Sprite",
			forceVar:           true,
			wantOverviewPrefix: "var forcedFieldVar int",
			wantItemKind:       VariableCompletion,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := GetSpxDefinitionForVar(tt.v, tt.selectorTypeName, tt.forceVar, nil)
			assert.Equal(t, tt.wantOverviewPrefix, def.Overview)
			assert.Equal(t, tt.wantItemKind, def.CompletionItemKind)
		})
	}
}
