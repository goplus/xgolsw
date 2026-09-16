package server

import (
	gotypes "go/types"
	"maps"
	"slices"

	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// collectSpxTypeSpecific collects spx resource and property name completions.
func (ctx *completionContext) collectSpxTypeSpecific(typ gotypes.Type) {
	if ctx.spxResult == nil || !xgoutil.IsValidType(typ) {
		return
	}

	if named := resolvedNamedType(typ); named != nil {
		switch ctx.spxTypeName(named) {
		case "Sprite", "SpriteImpl":
			file, _ := ctx.proj.ASTFile(ctx.spxResult.mainSpxFile)
			projectType := classTypeForFile(ctx.proj, file)
			for spxSprite := range ctx.spxResult.spxSpriteResourceAutoBindings {
				if resolvedNamedType(spxSprite.Type()) == named {
					ctx.itemSet.addDefinitions(ctx.definitionsForSelection(spxSprite, projectType)...)
				}
			}
		}
	}

	// Handle spx.PropertyName type - provide property name completions.
	if ctx.inferSpxInputTypeFromType(typ) == SpxInputTypePropertyName {
		if target := ctx.getPropertyTarget(); target != "" {
			ctx.collectPropertyNames(target)
		}
		return
	}

	switch ctx.spxResourceNameType(typ) {
	case "BackdropName":
		ctx.collectSpxResourceNames(spxResourceCompletionBackdrop, nil)
	case "SpriteName":
		ctx.collectSpxResourceNames(spxResourceCompletionSprite, nil)
	case "SpriteCostumeName":
		ctx.collectSpxResourceNames(spxResourceCompletionCostume, ctx.getSpxSpriteResource())
	case "SpriteAnimationName":
		ctx.collectSpxResourceNames(spxResourceCompletionAnimation, ctx.getSpxSpriteResource())
	case "SoundName":
		ctx.collectSpxResourceNames(spxResourceCompletionSound, nil)
	case "WidgetName":
		ctx.collectSpxResourceNames(spxResourceCompletionWidget, nil)
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
func (ctx *completionContext) collectSpxResourceNames(kind spxResourceCompletionKind, sprite *SpxSpriteResource) {
	var spxResourceIDs []SpxResourceID
	switch kind {
	case spxResourceCompletionBackdrop:
		spxResourceIDs = slices.Grow(spxResourceIDs, len(ctx.spxResult.spxResourceSet.backdrops))
		for spxBackdropName := range ctx.spxResult.spxResourceSet.backdrops {
			spxResourceIDs = append(spxResourceIDs, SpxBackdropResourceID{spxBackdropName})
		}
	case spxResourceCompletionSprite:
		spxResourceIDs = slices.Grow(spxResourceIDs, len(ctx.spxResult.spxResourceSet.sprites))
		for spxSpriteName := range ctx.spxResult.spxResourceSet.sprites {
			spxResourceIDs = append(spxResourceIDs, SpxSpriteResourceID{spxSpriteName})
		}
	case spxResourceCompletionCostume, spxResourceCompletionAnimation:
		sprites := []*SpxSpriteResource{sprite}
		if sprite == nil {
			sprites = nil
			for _, name := range slices.Sorted(maps.Keys(ctx.spxResult.spxResourceSet.sprites)) {
				sprites = append(sprites, ctx.spxResult.spxResourceSet.sprites[name])
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
		spxResourceIDs = slices.Grow(spxResourceIDs, len(ctx.spxResult.spxResourceSet.sounds))
		for spxSoundName := range ctx.spxResult.spxResourceSet.sounds {
			spxResourceIDs = append(spxResourceIDs, SpxSoundResourceID{spxSoundName})
		}
	case spxResourceCompletionWidget:
		spxResourceIDs = slices.Grow(spxResourceIDs, len(ctx.spxResult.spxResourceSet.widgets))
		for spxWidgetName := range ctx.spxResult.spxResourceSet.widgets {
			spxResourceIDs = append(spxResourceIDs, SpxWidgetResourceID{spxWidgetName})
		}
	}
	seenResourceNames := make(map[string]struct{}, len(spxResourceIDs))
	for _, spxResourceID := range spxResourceIDs {
		name := spxResourceID.Name()
		if _, ok := seenResourceNames[name]; ok {
			continue
		}
		seenResourceNames[name] = struct{}{}
		item := CompletionItem{
			Kind:          TextCompletion,
			Documentation: completionDocumentation(resourceMarkupContent(spxResourceID.URI(), ctx.itemSet.documentationKind)),
		}
		if ctx.setCompletionStringValue(&item, name) {
			ctx.itemSet.add(item)
		}
	}
}

// getSpxSpriteResource returns a [SpxSpriteResource] for the current context.
// It returns nil if no [SpxSpriteResource] can be inferred.
func (ctx *completionContext) getSpxSpriteResource() *SpxSpriteResource {
	callExpr := ctx.getEnclosingCallExpr()
	if callExpr != nil {
		return inferSpxSpriteResourceEnclosingNode(ctx.spxResult, callExpr)
	}
	return spxSpriteResourceForFile(ctx.spxResult, ctx.filename)
}
