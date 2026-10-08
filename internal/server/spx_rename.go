package server

import (
	"fmt"
	gotypes "go/types"

	"github.com/goplus/xgolsw/xgo"
)

// validateResourceRename checks spx resource namespaces before generating edits.
func (r *spxAnalysis) validateResourceRename(id resourceID, newName string) error {
	if r.spxResourceSetErr != nil {
		return fmt.Errorf("failed to load spx resources: %w", r.spxResourceSetErr)
	}
	if id.Name() == newName {
		return nil
	}
	switch id := id.(type) {
	case SpxBackdropResourceID:
		if r.spxResourceSet.Backdrop(newName) != nil {
			return fmt.Errorf("backdrop resource %q already exists", newName)
		}
	case SpxSoundResourceID:
		if r.spxResourceSet.Sound(newName) != nil {
			return fmt.Errorf("sound resource %q already exists", newName)
		}
	case SpxSpriteResourceID:
		if r.spxResourceSet.Sprite(newName) != nil {
			return fmt.Errorf("sprite resource %q already exists", newName)
		}
	case SpxSpriteCostumeResourceID:
		sprite := r.spxResourceSet.Sprite(id.SpriteName)
		if sprite == nil {
			return fmt.Errorf("sprite resource %q not found", id.SpriteName)
		}
		if sprite.Costume(newName) != nil {
			return fmt.Errorf("sprite costume resource %q already exists", newName)
		}
	case SpxSpriteAnimationResourceID:
		sprite := r.spxResourceSet.Sprite(id.SpriteName)
		if sprite == nil {
			return fmt.Errorf("sprite resource %q not found", id.SpriteName)
		}
		if sprite.Animation(newName) != nil {
			return fmt.Errorf("sprite animation resource %q already exists", newName)
		}
	case SpxWidgetResourceID:
		if r.spxResourceSet.Widget(newName) != nil {
			return fmt.Errorf("widget resource %q already exists", newName)
		}
	default:
		return fmt.Errorf("unsupported spx resource type: %T", id)
	}
	return nil
}

// appendSpxSpriteTypeRenames adds source type references for renamed sprites.
// Aliases retain their own names and enclosing type expressions are not edited.
func (s *Server) appendSpxSpriteTypeRenames(proj *xgo.Project, result *spxAnalysis, renames map[resourceID]string, changes map[DocumentURI][]TextEdit) {
	info, _ := proj.TypeInfo()
	if info == nil {
		return
	}
	for ident, obj := range info.Uses {
		typeName, ok := obj.(*gotypes.TypeName)
		if !ok || typeName.IsAlias() || !result.hasSpxSpriteType(typeName.Type()) || ident.Implicit() {
			continue
		}
		newName, renamed := renames[SpxSpriteResourceID{SpriteName: typeName.Name()}]
		if !renamed {
			continue
		}
		file := sourceASTFile(proj, ident.Pos())
		if file == nil {
			continue
		}
		uri := s.toDocumentURI(proj.Fset.File(ident.Pos()).Name())
		changes[uri] = append(changes[uri], TextEdit{Range: resourceRange(proj, file, ident), NewText: newName})
	}
}
