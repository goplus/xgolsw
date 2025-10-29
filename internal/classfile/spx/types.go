package spx

import (
	"fmt"
	"go/types"
	"sync"

	"github.com/goplus/xgolsw/internal"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

const spxPkgPath = "github.com/goplus/spx/v2"

var getSpxPkg = sync.OnceValue(func() *types.Package {
	pkg, err := internal.Importer.Import(spxPkgPath)
	if err != nil {
		panic(fmt.Errorf("failed to import spx package: %w", err))
	}
	return pkg
})

// Shared type accessors used by resource analysis.
var (
	getSpxSpriteType = sync.OnceValue(func() *types.Named {
		return getSpxPkg().Scope().Lookup("Sprite").Type().(*types.Named)
	})
	getSpxSpriteImplType = sync.OnceValue(func() *types.Named {
		return getSpxPkg().Scope().Lookup("SpriteImpl").Type().(*types.Named)
	})
	getSpxBackdropNameType = sync.OnceValue(func() *types.Alias {
		return getSpxPkg().Scope().Lookup("BackdropName").Type().(*types.Alias)
	})
	getSpxSpriteNameType = sync.OnceValue(func() *types.Alias {
		return getSpxPkg().Scope().Lookup("SpriteName").Type().(*types.Alias)
	})
	getSpxSpriteCostumeNameType = sync.OnceValue(func() *types.Alias {
		return getSpxPkg().Scope().Lookup("SpriteCostumeName").Type().(*types.Alias)
	})
	getSpxSpriteAnimationNameType = sync.OnceValue(func() *types.Alias {
		return getSpxPkg().Scope().Lookup("SpriteAnimationName").Type().(*types.Alias)
	})
	getSpxSoundNameType = sync.OnceValue(func() *types.Alias {
		return getSpxPkg().Scope().Lookup("SoundName").Type().(*types.Alias)
	})
	getSpxWidgetNameType = sync.OnceValue(func() *types.Alias {
		return getSpxPkg().Scope().Lookup("WidgetName").Type().(*types.Alias)
	})
)

// GetSpxSpriteType returns the named spx Sprite type.
func GetSpxSpriteType() *types.Named { return getSpxSpriteType() }

// GetSpxSpriteImplType returns the named spx SpriteImpl type.
func GetSpxSpriteImplType() *types.Named { return getSpxSpriteImplType() }

// GetSpxBackdropNameType returns the spx BackdropName alias type.
func GetSpxBackdropNameType() *types.Alias { return getSpxBackdropNameType() }

// GetSpxSpriteNameType returns the spx SpriteName alias type.
func GetSpxSpriteNameType() *types.Alias { return getSpxSpriteNameType() }

// GetSpxSpriteCostumeNameType returns the spx SpriteCostumeName alias type.
func GetSpxSpriteCostumeNameType() *types.Alias { return getSpxSpriteCostumeNameType() }

// GetSpxSpriteAnimationNameType returns the spx SpriteAnimationName alias type.
func GetSpxSpriteAnimationNameType() *types.Alias { return getSpxSpriteAnimationNameType() }

// GetSpxSoundNameType returns the spx SoundName alias type.
func GetSpxSoundNameType() *types.Alias { return getSpxSoundNameType() }

// GetSpxWidgetNameType returns the spx WidgetName alias type.
func GetSpxWidgetNameType() *types.Alias { return getSpxWidgetNameType() }

// IsInSpxPkg reports whether the object is defined in the spx package.
func IsInSpxPkg(obj types.Object) bool {
	return obj != nil && obj.Pkg() == getSpxPkg()
}

// IsSpxResourceNameType reports whether the given type is one of the spx resource name aliases.
func IsSpxResourceNameType(typ types.Type) bool {
	switch typ {
	case GetSpxBackdropNameType(),
		GetSpxSpriteNameType(),
		GetSpxSpriteCostumeNameType(),
		GetSpxSpriteAnimationNameType(),
		GetSpxSoundNameType(),
		GetSpxWidgetNameType():
		return true
	}
	return false
}

// HasSpxResourceNameTypeParams reports whether the function has parameters of resource name types.
func HasSpxResourceNameTypeParams(fun *types.Func) bool {
	if fun == nil {
		return false
	}
	sig, ok := fun.Type().(*types.Signature)
	if !ok {
		return false
	}
	params := sig.Params()
	for i := 0; i < params.Len(); i++ {
		paramType := xgoutil.DerefType(params.At(i).Type())
		if slice, ok := paramType.(*types.Slice); ok {
			paramType = slice.Elem()
		}
		if IsSpxResourceNameType(paramType) {
			return true
		}
	}
	return false
}
