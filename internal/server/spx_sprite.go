package server

import "go/types"

// isSpxSpriteAPIType reports whether typ is one of the public spx sprite API
// types used by the runtime surface.
func isSpxSpriteAPIType(typ types.Type) bool {
	named := resolvedNamedType(typ)
	return named == GetSpxSpriteType() || named == GetSpxSpriteImplType()
}

// isSpxSpriteSurfaceType reports whether typ behaves like a sprite surface,
// including the public sprite API types and concrete sprite work classes.
func isSpxSpriteSurfaceType(typ types.Type) bool {
	return isSpxSpriteSurfaceTypeRecursive(typ, make(map[*types.Named]struct{}))
}

func isSpxSpriteSurfaceTypeRecursive(typ types.Type, visited map[*types.Named]struct{}) bool {
	named := resolvedNamedType(typ)
	if named == nil || named.Obj() == nil {
		return false
	}
	if isSpxSpriteAPIType(named) {
		return true
	}
	if _, ok := visited[named]; ok {
		return false
	}
	visited[named] = struct{}{}
	structType, ok := named.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for field := range structType.Fields() {
		if !field.Embedded() {
			continue
		}
		if isSpxSpriteSurfaceTypeRecursive(field.Type(), visited) {
			return true
		}
	}
	return false
}

// spxMemberTraversalType returns the concrete named type that should be used
// when enumerating members for a sprite surface.
func spxMemberTraversalType(typ types.Type) *types.Named {
	named := resolvedNamedType(typ)
	if named == GetSpxSpriteType() {
		return GetSpxSpriteImplType()
	}
	return named
}

// isSpxSpriteBindingType reports whether typ can denote an spx sprite resource
// binding in user code.
func (r *compileResult) isSpxSpriteBindingType(typ types.Type) bool {
	named := resolvedNamedType(typ)
	if named == nil {
		return false
	}
	return named == GetSpxSpriteType() || r.hasSpxSpriteType(named)
}

// spxSpriteResourceNameForObject returns the sprite resource name implied by
// obj when it is used as a sprite receiver in user code.
func (r *compileResult) spxSpriteResourceNameForObject(obj types.Object, identName string) string {
	if obj == nil {
		return ""
	}
	named := resolvedNamedType(obj.Type())
	if named == nil {
		return ""
	}
	if named == GetSpxSpriteType() {
		return identName
	}
	if r.hasSpxSpriteType(named) {
		return obj.Name()
	}
	return ""
}

// spxSpriteResourceForObject resolves the sprite resource implied by obj.
func (r *compileResult) spxSpriteResourceForObject(obj types.Object, identName string) *SpxSpriteResource {
	name := r.spxSpriteResourceNameForObject(obj, identName)
	if name == "" {
		return nil
	}
	return r.spxResourceSet.Sprite(name)
}

// spxSpriteResourceForFile resolves the sprite resource implied by spxFile. It
// returns nil for the main work file.
func (r *compileResult) spxSpriteResourceForFile(spxFile string) *SpxSpriteResource {
	name := spxSpriteNameForFile(spxFile, r.mainSpxFile)
	if name == "" {
		return nil
	}
	return r.spxResourceSet.Sprite(name)
}
