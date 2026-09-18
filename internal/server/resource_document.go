package server

import (
	"fmt"

	"github.com/goplus/xgolsw/xgo"
)

// documentLinksForResources returns links to existing resources in the project.
func (s *Server) documentLinksForResources(proj *xgo.Project, filename string) ([]DocumentLink, error) {
	result, err := s.analyzeFramework(proj)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze resources: %w", err)
	}
	if result == nil {
		return nil, nil
	}
	return result.resources.resourceDocumentLinks(filename), nil
}

// resourceDocumentLinks returns links to existing resources referenced in filename.
func (r *resourceAnalysis) resourceDocumentLinks(filename string) []DocumentLink {
	links := make([]DocumentLink, 0, len(r.resourceRefs))
	for _, ref := range r.resourceRefs {
		if r.proj.Fset.PositionFor(ref.Node.Pos(), false).Filename != filename || (r.contains == nil || !r.contains(ref.ID)) {
			continue
		}
		astFile := sourceASTFile(r.proj, ref.Node.Pos())
		if astFile == nil {
			continue
		}
		target := URI(ref.ID.URI())
		links = append(links, DocumentLink{
			Range:  resourceRange(r.proj, astFile, ref.Node),
			Target: &target,
			Data: XGoResourceRefDocumentLinkData{
				Kind: ref.Kind,
			},
		})
	}
	return links
}
