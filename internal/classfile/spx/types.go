package spx

import (
	"fmt"
	"go/types"
	"sync"

	"github.com/goplus/xgolsw/internal"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

const spxPkgPath = "github.com/goplus/spx/v2"

var packageOnce = sync.OnceValue(func() *types.Package {
	pkg, err := internal.Importer.Import(spxPkgPath)
	if err != nil {
		panic(fmt.Errorf("failed to import spx package: %w", err))
	}
	return pkg
})

// Package returns the imported spx package.
func Package() *types.Package { return packageOnce() }

// IsInPackage reports whether the object is defined in the spx package.
func IsInPackage(obj types.Object) bool {
	return obj != nil && obj.Pkg() == Package()
}

// Shared type accessors.
var (
	spriteTypeOnce = sync.OnceValue(func() *types.Named {
		return Package().Scope().Lookup("Sprite").Type().(*types.Named)
	})
	spriteImplTypeOnce = sync.OnceValue(func() *types.Named {
		return Package().Scope().Lookup("SpriteImpl").Type().(*types.Named)
	})
	backdropNameTypeOnce = sync.OnceValue(func() *types.Alias {
		return Package().Scope().Lookup("BackdropName").Type().(*types.Alias)
	})
	spriteNameTypeOnce = sync.OnceValue(func() *types.Alias {
		return Package().Scope().Lookup("SpriteName").Type().(*types.Alias)
	})
	spriteCostumeNameTypeOnce = sync.OnceValue(func() *types.Alias {
		return Package().Scope().Lookup("SpriteCostumeName").Type().(*types.Alias)
	})
	spriteAnimationNameTypeOnce = sync.OnceValue(func() *types.Alias {
		return Package().Scope().Lookup("SpriteAnimationName").Type().(*types.Alias)
	})
	soundNameTypeOnce = sync.OnceValue(func() *types.Alias {
		return Package().Scope().Lookup("SoundName").Type().(*types.Alias)
	})
	widgetNameTypeOnce = sync.OnceValue(func() *types.Alias {
		return Package().Scope().Lookup("WidgetName").Type().(*types.Alias)
	})
)

// SpriteType returns the named spx Sprite type.
func SpriteType() *types.Named { return spriteTypeOnce() }

// SpriteImplType returns the named spx SpriteImpl type.
func SpriteImplType() *types.Named { return spriteImplTypeOnce() }

// BackdropNameType returns the spx BackdropName alias type.
func BackdropNameType() *types.Alias { return backdropNameTypeOnce() }

// SpriteNameType returns the spx SpriteName alias type.
func SpriteNameType() *types.Alias { return spriteNameTypeOnce() }

// SpriteCostumeNameType returns the spx SpriteCostumeName alias type.
func SpriteCostumeNameType() *types.Alias { return spriteCostumeNameTypeOnce() }

// SpriteAnimationNameType returns the spx SpriteAnimationName alias type.
func SpriteAnimationNameType() *types.Alias { return spriteAnimationNameTypeOnce() }

// SoundNameType returns the spx SoundName alias type.
func SoundNameType() *types.Alias { return soundNameTypeOnce() }

// WidgetNameType returns the spx WidgetName alias type.
func WidgetNameType() *types.Alias { return widgetNameTypeOnce() }

// IsResourceNameType reports whether the given type is one of the spx resource
// name aliases.
func IsResourceNameType(typ types.Type) bool {
	switch typ {
	case BackdropNameType(),
		SpriteNameType(),
		SpriteCostumeNameType(),
		SpriteAnimationNameType(),
		SoundNameType(),
		WidgetNameType():
		return true
	}
	return false
}

// HasResourceNameTypeParams reports whether the function has parameters of
// resource name types.
func HasResourceNameTypeParams(fun *types.Func) bool {
	if fun == nil {
		return false
	}
	sig, ok := fun.Type().(*types.Signature)
	if !ok {
		return false
	}
	for param := range sig.Params().Variables() {
		paramType := xgoutil.DerefType(param.Type())
		if slice, ok := paramType.(*types.Slice); ok {
			paramType = slice.Elem()
		}
		if IsResourceNameType(paramType) {
			return true
		}
	}
	return false
}
