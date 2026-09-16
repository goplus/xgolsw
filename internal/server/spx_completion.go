package server

import (
	"fmt"
	gotypes "go/types"
	"maps"
	"path"
	"slices"

	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// compileForSpxCompletion prepares resource data only for an spx classfile.
func (s *Server) compileForSpxCompletion(proj *xgo.Project, filename string) (*compileResult, error) {
	if path.Ext(filename) != ".spx" {
		return nil, nil
	}
	class, ok := proj.Module().LookupClass(".spx")
	if !ok || !slices.Contains(class.PkgPaths, SpxPkgPath) {
		return nil, nil
	}
	result, err := s.compileAt(proj)
	if err != nil {
		return nil, fmt.Errorf("failed to compile: %w", err)
	}
	return result, nil
}

// collectSpxTypeSpecific collects spx resource and property name completions.
func (ctx *completionContext) collectSpxTypeSpecific(typ gotypes.Type) {
	if ctx.spxResult == nil || !xgoutil.IsValidType(typ) {
		return
	}

	if named := resolvedNamedType(typ); named != nil {
		switch named {
		case GetSpxSpriteType(), GetSpxSpriteImplType():
			for spxSprite := range ctx.spxResult.spxSpriteResourceAutoBindings {
				if spxSprite.Type() == named {
					ctx.itemSet.addDefinitions(ctx.definitionsFor(spxSprite, "Game")...)
				}
			}
		}
	}

	// Handle spx.PropertyName type - provide property name completions.
	if inferSpxInputTypeFromType(typ) == SpxInputTypePropertyName {
		if target := ctx.getPropertyTarget(); target != "" {
			ctx.collectPropertyNames(target)
		}
		return
	}

	switch canonicalSpxResourceNameType(typ) {
	case GetSpxBackdropNameType():
		ctx.collectSpxResourceNames(spxResourceCompletionBackdrop, nil)
	case GetSpxSpriteNameType():
		ctx.collectSpxResourceNames(spxResourceCompletionSprite, nil)
	case GetSpxSpriteCostumeNameType():
		ctx.collectSpxResourceNames(spxResourceCompletionCostume, ctx.getSpxSpriteResource())
	case GetSpxSpriteAnimationNameType():
		ctx.collectSpxResourceNames(spxResourceCompletionAnimation, ctx.getSpxSpriteResource())
	case GetSpxSoundNameType():
		ctx.collectSpxResourceNames(spxResourceCompletionSound, nil)
	case GetSpxWidgetNameType():
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
