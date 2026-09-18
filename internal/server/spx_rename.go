package server

import (
	"fmt"
	gotypes "go/types"
)

// validateResourceRename checks spx resource namespaces before generating edits.
func (r *spxAnalysis) validateResourceRename(id resourceID, newName string) error {
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
func (s *Server) appendSpxSpriteTypeRenames(result *spxAnalysis, renames map[resourceID]string, changes map[DocumentURI][]TextEdit) {
	info, _ := result.proj.TypeInfo()
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
		file := sourceASTFile(result.proj, ident.Pos())
		if file == nil {
			continue
		}
		uri := s.toDocumentURI(result.proj.Fset.File(ident.Pos()).Name())
		changes[uri] = append(changes[uri], TextEdit{Range: resourceRange(result.proj, file, ident), NewText: newName})
	}
}

// renameSpxResources validates the complete batch and plans its edits together.
func (s *Server) renameSpxResources(result *spxAnalysis, params []XGoRenameResourceParams) (*WorkspaceEdit, error) {
	if result.spxResourceSetErr != nil {
		return nil, fmt.Errorf("failed to load spx resources: %w", result.spxResourceSetErr)
	}
	renames := make(map[resourceID]string)
	type target struct {
		context XGoResourceContextURI
		name    string
	}
	targets := make(map[target]bool)
	for _, param := range params {
		id, err := ParseSpxResourceURI(param.Resource.URI)
		if err != nil {
			return nil, fmt.Errorf("failed to parse spx resource URI: %w", err)
		}
		if newName, ok := renames[id]; ok {
			if newName != param.NewName {
				return nil, fmt.Errorf("conflicting renames for spx resource %q", id.URI())
			}
			continue
		}
		if err := result.validateResourceRename(id, param.NewName); err != nil {
			return nil, fmt.Errorf("failed to rename spx resource %q: %w", param.Resource.URI, err)
		}
		destination := target{id.ContextURI(), param.NewName}
		if targets[destination] {
			return nil, fmt.Errorf("conflicting rename target %q in %q", param.NewName, id.ContextURI())
		}
		targets[destination] = true
		renames[id] = param.NewName
	}
	changes, err := s.renameResourcesAtRefs(result.resourceAnalysis, renames)
	if err != nil {
		return nil, err
	}
	s.appendSpxSpriteTypeRenames(result, renames, changes)
	return &WorkspaceEdit{Changes: changes}, nil
}
