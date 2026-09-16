package server

import (
	"fmt"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
)

// hoverForSpxResource returns a preview for a resource in an spx project.
func (s *Server) hoverForSpxResource(proj *xgo.Project, position token.Position, markupKind MarkupKind) (*Hover, error) {
	result, err := s.compileAt(proj)
	if err != nil {
		return nil, fmt.Errorf("failed to compile: %w", err)
	}
	if result == nil {
		return nil, nil
	}
	return result.spxResourceHover(position, markupKind), nil
}

// spxResourceHover returns a preview of the resource reference at position.
func (r *compileResult) spxResourceHover(position token.Position, markupKind MarkupKind) *Hover {
	ref, astFile := r.spxResourceRefAtPosition(position)
	if ref == nil {
		return nil
	}
	return &Hover{
		Contents: resourceMarkupContent(ref.ID.URI(), markupKind),
		Range:    resourceRange(r.proj, astFile, ref.Node),
	}
}
