package server

import (
	gotypes "go/types"
	"maps"
	"slices"

	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// collectCompletions uses resolved literal contexts before inferred call types.
// This keeps nested conversions in the same resource namespace as references.
func (r *spxAnalysis) collectCompletions(ctx *completionContext) {
	if value, ok := r.resourceLiterals[ctx.stringLit]; ok {
		r.collectTypeCompletions(ctx, value.Type)
		return
	}
	for _, typ := range ctx.expectedTypes {
		r.collectTypeCompletions(ctx, typ)
	}
}

// collectTypeCompletions collects spx resource and property name completions.
func (r *spxAnalysis) collectTypeCompletions(ctx *completionContext, typ gotypes.Type) {
	if !xgoutil.IsValidType(typ) {
		return
	}

	if named := resolvedNamedType(typ); named != nil {
		switch r.spxTypeName(named) {
		case "Sprite", "SpriteImpl":
			file, _ := ctx.proj.ASTFile(r.mainSpxFile)
			projectType := classTypeForFile(ctx.proj, file)
			for spxSprite := range r.spxSpriteResourceAutoBindings {
				if resolvedNamedType(spxSprite.Type()) == named {
					ctx.itemSet.addDefinitions(ctx.definitionsForSelection(spxSprite, projectType)...)
				}
			}
		}
	}

	// Handle spx.PropertyName type - provide property name completions.
	if r.inferSpxInputTypeFromType(typ) == SpxInputTypePropertyName {
		if target := ctx.getPropertyTarget(); target != "" {
			ctx.collectPropertyNames(target)
		}
		return
	}

	switch r.spxResourceNameType(typ) {
	case "BackdropName":
		r.collectSpxResourceNames(ctx, spxResourceCompletionBackdrop, nil)
	case "SpriteName":
		r.collectSpxResourceNames(ctx, spxResourceCompletionSprite, nil)
	case "SpriteCostumeName":
		r.collectSpxResourceNames(ctx, spxResourceCompletionCostume, r.getSpxSpriteResource(ctx))
	case "SpriteAnimationName":
		r.collectSpxResourceNames(ctx, spxResourceCompletionAnimation, r.getSpxSpriteResource(ctx))
	case "SoundName":
		r.collectSpxResourceNames(ctx, spxResourceCompletionSound, nil)
	case "WidgetName":
		r.collectSpxResourceNames(ctx, spxResourceCompletionWidget, nil)
	}
}

// spxResourceCompletionKind selects the resource data used for name completion.
type spxResourceCompletionKind int

const (
	spxResourceCompletionBackdrop spxResourceCompletionKind = iota
	spxResourceCompletionSprite
	spxResourceCompletionCostume
	spxResourceCompletionAnimation
	spxResourceCompletionSound
	spxResourceCompletionWidget
)

// collectSpxResourceNames collects names from project resource data. A nil sprite
// includes costumes and animations from all sprites. Sprites are visited by name
// so overlapping resource names have stable previews.
func (r *spxAnalysis) collectSpxResourceNames(ctx *completionContext, kind spxResourceCompletionKind, sprite *SpxSpriteResource) {
	var spxResourceIDs []resourceID
	switch kind {
	case spxResourceCompletionBackdrop:
		spxResourceIDs = slices.Grow(spxResourceIDs, len(r.spxResourceSet.backdrops))
		for spxBackdropName := range r.spxResourceSet.backdrops {
			spxResourceIDs = append(spxResourceIDs, SpxBackdropResourceID{spxBackdropName})
		}
	case spxResourceCompletionSprite:
		spxResourceIDs = slices.Grow(spxResourceIDs, len(r.spxResourceSet.sprites))
		for spxSpriteName := range r.spxResourceSet.sprites {
			spxResourceIDs = append(spxResourceIDs, SpxSpriteResourceID{spxSpriteName})
		}
	case spxResourceCompletionCostume, spxResourceCompletionAnimation:
		sprites := []*SpxSpriteResource{sprite}
		if sprite == nil {
			sprites = nil
			for _, name := range slices.Sorted(maps.Keys(r.spxResourceSet.sprites)) {
				sprites = append(sprites, r.spxResourceSet.sprites[name])
			}
		}
		for _, sprite := range sprites {
			if kind == spxResourceCompletionCostume {
				spxResourceIDs = slices.Grow(spxResourceIDs, len(sprite.NormalCostumes))
				for _, costume := range sprite.NormalCostumes {
					spxResourceIDs = append(spxResourceIDs, costume.ID)
				}
			} else {
				spxResourceIDs = slices.Grow(spxResourceIDs, len(sprite.Animations))
				for _, animation := range sprite.Animations {
					spxResourceIDs = append(spxResourceIDs, animation.ID)
				}
			}
		}
	case spxResourceCompletionSound:
		spxResourceIDs = slices.Grow(spxResourceIDs, len(r.spxResourceSet.sounds))
		for spxSoundName := range r.spxResourceSet.sounds {
			spxResourceIDs = append(spxResourceIDs, SpxSoundResourceID{spxSoundName})
		}
	case spxResourceCompletionWidget:
		spxResourceIDs = slices.Grow(spxResourceIDs, len(r.spxResourceSet.widgets))
		for spxWidgetName := range r.spxResourceSet.widgets {
			spxResourceIDs = append(spxResourceIDs, SpxWidgetResourceID{spxWidgetName})
		}
	}
	ctx.collectResourceNames(spxResourceIDs)
}

// getSpxSpriteResource returns a [SpxSpriteResource] for the current context.
// It returns nil if no [SpxSpriteResource] can be inferred.
func (r *spxAnalysis) getSpxSpriteResource(ctx *completionContext) *SpxSpriteResource {
	callExpr := ctx.getEnclosingCallExpr()
	if value, ok := r.resourceLiterals[ctx.stringLit]; ok {
		callExpr = value.Call
	}
	if callExpr != nil {
		return inferSpxSpriteResourceEnclosingNode(r, callExpr)
	}
	return spxSpriteResourceForFile(r, ctx.filename)
}
