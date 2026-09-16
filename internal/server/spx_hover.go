package server

import (
	"fmt"
	"path"
	"slices"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
)

// hoverForSpxResource returns a preview for a resource in an spx classfile.
func (s *Server) hoverForSpxResource(proj *xgo.Project, filename string, position token.Position, markupKind MarkupKind) (*Hover, error) {
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
