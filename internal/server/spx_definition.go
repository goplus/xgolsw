package server

import (
	"fmt"
	gotypes "go/types"
	"slices"
	"sync"

	"github.com/goplus/xgolsw/internal"
	"github.com/goplus/xgolsw/pkgdoc"
)

// SpxPkgPath is the path to the spx package.
const SpxPkgPath = "github.com/goplus/spx/v3"

var (
	// GetSpxPkg returns the spx package.
	GetSpxPkg = sync.OnceValue(func() *gotypes.Package {
		spxPkg, err := internal.Importer.Import(SpxPkgPath)
		if err != nil {
			panic(fmt.Errorf("failed to import spx package: %w", err))
		}
		return spxPkg
	})

	// GetSpxBackdropNameType returns the [spx.BackdropName] type.
	GetSpxBackdropNameType = sync.OnceValue(func() *gotypes.Alias {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("BackdropName").Type().(*gotypes.Alias)
	})

	// GetSpxSpriteType returns the [spx.Sprite] type.
	GetSpxSpriteType = sync.OnceValue(func() *gotypes.Named {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("Sprite").Type().(*gotypes.Named)
	})

	// GetSpxSpriteImplType returns the [spx.SpriteImpl] type.
	GetSpxSpriteImplType = sync.OnceValue(func() *gotypes.Named {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("SpriteImpl").Type().(*gotypes.Named)
	})

	// GetSpxSpriteNameType returns the [spx.SpriteName] type.
	GetSpxSpriteNameType = sync.OnceValue(func() *gotypes.Alias {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("SpriteName").Type().(*gotypes.Alias)
	})

	// GetSpxSpriteCostumeNameType returns the [spx.SpriteCostumeName] type.
	GetSpxSpriteCostumeNameType = sync.OnceValue(func() *gotypes.Alias {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("SpriteCostumeName").Type().(*gotypes.Alias)
	})

	// GetSpxSpriteAnimationNameType returns the [spx.SpriteAnimationName] type.
	GetSpxSpriteAnimationNameType = sync.OnceValue(func() *gotypes.Alias {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("SpriteAnimationName").Type().(*gotypes.Alias)
	})

	// GetSpxSoundNameType returns the [spx.SoundName] type.
	GetSpxSoundNameType = sync.OnceValue(func() *gotypes.Alias {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("SoundName").Type().(*gotypes.Alias)
	})

	// GetSpxWidgetNameType returns the [spx.WidgetName] type.
	GetSpxWidgetNameType = sync.OnceValue(func() *gotypes.Alias {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("WidgetName").Type().(*gotypes.Alias)
	})

	// GetSpxDirectionType returns the [spx.Direction] type.
	GetSpxDirectionType = sync.OnceValue(func() *gotypes.Alias {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("Direction").Type().(*gotypes.Alias)
	})

	// GetSpxLayerActionType returns the [spx.LayerAction] type.
	GetSpxLayerActionType = sync.OnceValue(func() *gotypes.Named {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("layerAction").Type().(*gotypes.Named)
	})

	// GetSpxDirActionType returns the [spx.DirLayer] type.
	GetSpxDirActionType = sync.OnceValue(func() *gotypes.Named {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("dirAction").Type().(*gotypes.Named)
	})

	// GetSpxEffectKindType returns the [spx.EffectKind] type.
	GetSpxEffectKindType = sync.OnceValue(func() *gotypes.Named {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("EffectKind").Type().(*gotypes.Named)
	})

	// GetSpxKeyType returns the [spx.Key] type.
	GetSpxKeyType = sync.OnceValue(func() *gotypes.Alias {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("Key").Type().(*gotypes.Alias)
	})

	// GetSpxSpecialObjType returns the [spx.SpecialObj] type.
	GetSpxSpecialObjType = sync.OnceValue(func() *gotypes.Named {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("Edge").Type().(*gotypes.Named)
	})

	// GetSpxRotationStyleType returns the [spx.RotationStyle] type.
	GetSpxRotationStyleType = sync.OnceValue(func() *gotypes.Named {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("RotationStyle").Type().(*gotypes.Named)
	})

	// GetSpxPropertyNameType returns the [spx.PropertyName] type.
	GetSpxPropertyNameType = sync.OnceValue(func() *gotypes.Alias {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("PropertyName").Type().(*gotypes.Alias)
	})

	// GetSpxHSBFunc returns the [spx.HSB] type.
	GetSpxHSBFunc = sync.OnceValue(func() *gotypes.Func {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("HSB").(*gotypes.Func)
	})

	// GetSpxHSBAFunc returns the [spx.HSBA] type.
	GetSpxHSBAFunc = sync.OnceValue(func() *gotypes.Func {
		spxPkg := GetSpxPkg()
		return spxPkg.Scope().Lookup("HSBA").(*gotypes.Func)
	})
)

// isSpxSymbol reports whether obj belongs to the project's registered spx
// package. It compares package identity through the configured importer.
func (r *definitionContext) isSpxSymbol(obj gotypes.Object) bool {
	pkg := obj.Pkg()
	if pkg == nil || pkg.Path() != SpxPkgPath {
		return false
	}
	class, ok := r.proj.Module().LookupClass(".spx")
	if !ok || !slices.Contains(class.PkgPaths, SpxPkgPath) {
		return false
	}
	imported, err := r.proj.Importer.Import(SpxPkgPath)
	return err == nil && imported == pkg
}

// spxFunctionDocumentation resolves the implementation documentation for a
// public Sprite method. Absent implementation entries leave the declaration's
// documentation in effect.
func (r *definitionContext) spxFunctionDocumentation(fun *gotypes.Func, doc *pkgdoc.PkgDoc) (string, bool) {
	if doc == nil {
		return "", false
	}
	recvTypeName, _, _, _ := displayedFuncName(fun)
	if recvTypeName != "Sprite" || !r.isSpxSymbol(fun) {
		return "", false
	}
	if typeDoc := doc.Types["SpriteImpl"]; typeDoc != nil {
		detail, ok := typeDoc.Methods[fun.Name()]
		return detail, ok
	}
	return "", false
}

// spxDisplayTypeName maps the project's spx implementation type to its public
// name. Other symbols retain their declared type names.
func (r *definitionContext) spxDisplayTypeName(obj gotypes.Object, typeName string) string {
	if typeName == "SpriteImpl" && r.isSpxSymbol(obj) {
		return "Sprite"
	}
	return typeName
}

// canonicalSpxResourceNameType resolves aliases until it finds a canonical spx
// resource name type. It returns nil if typ does not represent one.
func canonicalSpxResourceNameType(typ gotypes.Type) gotypes.Type {
	seen := make(map[gotypes.Type]struct{})
	for typ != nil {
		if _, ok := seen[typ]; ok {
			return nil
		}
		seen[typ] = struct{}{}

		switch typ {
		case GetSpxBackdropNameType():
			return GetSpxBackdropNameType()
		case GetSpxSpriteNameType():
			return GetSpxSpriteNameType()
		case GetSpxSpriteCostumeNameType():
			return GetSpxSpriteCostumeNameType()
		case GetSpxSpriteAnimationNameType():
			return GetSpxSpriteAnimationNameType()
		case GetSpxSoundNameType():
			return GetSpxSoundNameType()
		case GetSpxWidgetNameType():
			return GetSpxWidgetNameType()
		}

		alias, ok := typ.(*gotypes.Alias)
		if !ok {
			return nil
		}

		rhs := alias.Rhs()
		if rhs == nil || rhs == typ {
			return nil
		}
		typ = rhs
	}
	return nil
}

// IsSpxResourceNameType reports whether the given type is a spx resource name type.
func IsSpxResourceNameType(typ gotypes.Type) bool {
	return canonicalSpxResourceNameType(typ) != nil
}

// IsSpxPropertyNameType reports whether the given type is or is an alias of
// [spx.PropertyName], resolving alias chains before comparing.
func IsSpxPropertyNameType(typ gotypes.Type) bool {
	seen := make(map[gotypes.Type]struct{})
	for typ != nil {
		if _, ok := seen[typ]; ok {
			return false
		}
		seen[typ] = struct{}{}

		if typ == GetSpxPropertyNameType() {
			return true
		}

		alias, ok := typ.(*gotypes.Alias)
		if !ok {
			return false
		}
		rhs := alias.Rhs()
		if rhs == nil || rhs == typ {
			return false
		}
		typ = rhs
	}
	return false
}
