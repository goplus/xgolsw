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
	class, ok := proj.Mod.LookupClass(".spx")
	if !ok || !slices.Contains(class.PkgPaths, SpxPkgPath) {
		return nil, nil
	}
	result, err := s.compileAt(proj)
	if err != nil {
		return nil, fmt.Errorf("failed to compile: %w", err)
	}
	ref := result.spxResourceRefAtPosition(position)
	if ref == nil {
		return nil, nil
	}
	return &Hover{
		Contents: resourceMarkupContent(ref.ID.URI(), markupKind),
		Range:    RangeForNode(proj, ref.Node),
	}, nil
}
