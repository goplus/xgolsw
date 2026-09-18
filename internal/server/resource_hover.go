package server

import (
	"fmt"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
)

// hoverForResource returns a preview for a resource in the project.
func hoverForResource(proj *xgo.Project, position token.Position, markupKind MarkupKind) (*Hover, error) {
	result, err := analyzeFramework(proj)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze resources: %w", err)
	}
	if result == nil {
		return nil, nil
	}
	return result.resources.resourceHover(proj, position, markupKind), nil
}

// resourceHover returns a preview of the resource reference at position.
func (r *resourceAnalysis) resourceHover(proj *xgo.Project, position token.Position, markupKind MarkupKind) *Hover {
	ref, astFile := r.resourceRefAtPosition(proj, position)
	if ref == nil {
		return nil
	}
	return &Hover{
		Contents: resourceMarkupContent(ref.ID.URI(), markupKind),
		Range:    resourceRange(proj, astFile, ref.Node),
	}
}
