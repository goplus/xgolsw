package server

import (
	"fmt"
	gotypes "go/types"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// compileForSpxCompletion prepares resource data only for an spx classfile.
func (s *Server) compileForSpxCompletion(proj *xgo.Project, filename string) (*compileResult, error) {
	if path.Ext(filename) != ".spx" {
		return nil, nil
	}
	class, ok := proj.Mod.LookupClass(".spx")
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
					ctx.itemSet.addSpxDefs(ctx.spxDefinitionsFor(spxSprite, "Game")...)
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

	var spxResourceIDs []SpxResourceID
	switch canonicalSpxResourceNameType(typ) {
	case GetSpxBackdropNameType():
		spxResourceIDs = slices.Grow(spxResourceIDs, len(ctx.spxResult.spxResourceSet.backdrops))
		for spxBackdropName := range ctx.spxResult.spxResourceSet.backdrops {
			spxResourceIDs = append(spxResourceIDs, SpxBackdropResourceID{spxBackdropName})
		}
	case GetSpxSpriteNameType():
		spxResourceIDs = slices.Grow(spxResourceIDs, len(ctx.spxResult.spxResourceSet.sprites))
		for spxSpriteName := range ctx.spxResult.spxResourceSet.sprites {
			spxResourceIDs = append(spxResourceIDs, SpxSpriteResourceID{spxSpriteName})
		}
	case GetSpxSpriteCostumeNameType():
		expectedSpxSprite := ctx.getSpxSpriteResource()
		for _, spxSprite := range ctx.spxResult.spxResourceSet.sprites {
			if expectedSpxSprite != nil && spxSprite != expectedSpxSprite {
				continue
			}
			spxResourceIDs = slices.Grow(spxResourceIDs, len(spxSprite.NormalCostumes))
			for _, spxSpriteCostume := range spxSprite.NormalCostumes {
				spxResourceIDs = append(spxResourceIDs, SpxSpriteCostumeResourceID{spxSprite.Name, spxSpriteCostume.Name})
			}
		}
	case GetSpxSpriteAnimationNameType():
		expectedSpxSprite := ctx.getSpxSpriteResource()
		for _, spxSprite := range ctx.spxResult.spxResourceSet.sprites {
			if expectedSpxSprite != nil && spxSprite != expectedSpxSprite {
				continue
			}
			spxResourceIDs = slices.Grow(spxResourceIDs, len(spxSprite.Animations))
			for _, spxSpriteAnimation := range spxSprite.Animations {
				spxResourceIDs = append(spxResourceIDs, SpxSpriteAnimationResourceID{spxSprite.Name, spxSpriteAnimation.Name})
			}
		}
	case GetSpxSoundNameType():
		spxResourceIDs = slices.Grow(spxResourceIDs, len(ctx.spxResult.spxResourceSet.sounds))
		for spxSoundName := range ctx.spxResult.spxResourceSet.sounds {
			spxResourceIDs = append(spxResourceIDs, SpxSoundResourceID{spxSoundName})
		}
	case GetSpxWidgetNameType():
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
		if !ctx.inStringLit {
			name = strconv.Quote(name)
		}
		ctx.itemSet.add(CompletionItem{
			Label:            name,
			Kind:             TextCompletion,
			Documentation:    completionDocumentation(resourceMarkupContent(spxResourceID.URI(), ctx.itemSet.documentationKind)),
			InsertText:       name,
			InsertTextFormat: ToPtr(PlainTextTextFormat),
		})
	}
}

// getSpxSpriteResource returns a [SpxSpriteResource] for the current context.
// It returns nil if no [SpxSpriteResource] can be inferred.
func (ctx *completionContext) getSpxSpriteResource() *SpxSpriteResource {
	callExpr := ctx.getEnclosingCallExpr()
	if callExpr != nil {
		return inferSpxSpriteResourceEnclosingNode(ctx.spxResult, callExpr)
	}
	return ctx.getCurrentFileSpxSpriteResource()
}

// getEnclosingCallExpr returns the closest call expression in the current
// completion context.
func (ctx *completionContext) getEnclosingCallExpr() *ast.CallExpr {
	if callExpr, ok := ctx.enclosingNode.(*ast.CallExpr); ok {
		return callExpr
	}
	return ctx.enclosingCallExpr
}

// getCurrentFileSpxSpriteResource returns the sprite resource represented by
// the current spx file.
func (ctx *completionContext) getCurrentFileSpxSpriteResource() *SpxSpriteResource {
	if ctx.filename == "" || path.Base(ctx.filename) == path.Base(ctx.spxResult.mainSpxFile) {
		return nil
	}
	return ctx.spxResult.spxResourceSet.sprites[strings.TrimSuffix(path.Base(ctx.filename), ".spx")]
}

// getPropertyTarget returns the target type name for property name completions.
// It looks at the enclosing call expression's receiver type (if any) and falls
// back to the current file's type.
func (ctx *completionContext) getPropertyTarget() string {
	if callExpr, ok := ctx.enclosingNode.(*ast.CallExpr); ctx.kind == completionKindCall && ok {
		named := PropertyTargetNamedTypeForCall(ctx.typeInfo, callExpr, ctx.filename, ctx.spxResult.mainSpxFile)
		if named == nil {
			return ""
		}
		// For explicit-receiver calls, only consider main-package types.
		if _, hasSel := callExpr.Fun.(*ast.SelectorExpr); hasSel && !xgoutil.IsInMainPkg(named.Obj()) {
			return ""
		}
		return named.Obj().Name()
	}
	// For implicit receiver calls, derive target from the current file's type.
	if ctx.filename == "" {
		return ""
	}
	if ctx.filename == ctx.spxResult.mainSpxFile {
		return "Game"
	}
	return strings.TrimSuffix(path.Base(ctx.filename), ".spx")
}

// collectPropertyNames collects property name completion items for the given target type.
func (ctx *completionContext) collectPropertyNames(target string) {
	typeName, ok := ctx.typeInfo.Pkg.Scope().Lookup(target).(*gotypes.TypeName)
	if !ok {
		return
	}
	typ := gotypes.Unalias(typeName.Type())
	typ = xgoutil.DerefType(typ)
	namedType, ok := typ.(*gotypes.Named)
	if !ok {
		return
	}

	mainPkgDoc, _ := ctx.proj.PkgDoc()
	for m := range propertyMembers(namedType, makePkgDocFor(mainPkgDoc, ctx.lookupPkgDoc)) {
		insertText := m.Name
		if !ctx.inStringLit {
			insertText = strconv.Quote(m.Name)
		}
		def := m.SpxDef
		// TypeHint must be nil so addSpxDefs does not filter property-name
		// items by expected type compatibility.
		def.TypeHint = nil
		// Regardless of whether the property is backed by a field or a method,
		// it is presented as a property to the user.
		def.CompletionItemKind = PropertyCompletion
		def.CompletionItemLabel = insertText
		def.CompletionItemInsertText = insertText
		def.CompletionItemInsertTextFormat = PlainTextTextFormat
		ctx.itemSet.addSpxDefs(def)
	}
}
