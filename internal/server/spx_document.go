package server

import (
	"fmt"
	"path"
	"slices"

	"github.com/goplus/xgolsw/xgo"
)

// documentLinksForSpxResources returns links to existing resources in an spx classfile.
func (s *Server) documentLinksForSpxResources(proj *xgo.Project, filename string) ([]DocumentLink, error) {
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
	return result.spxResourceDocumentLinks(filename), nil
}

// spxResourceDocumentLinks returns links to existing resources referenced in filename.
func (r *compileResult) spxResourceDocumentLinks(filename string) []DocumentLink {
	links := make([]DocumentLink, 0, len(r.spxResourceRefs))
	for _, ref := range r.spxResourceRefs {
		if r.proj.Fset.PositionFor(ref.Node.Pos(), false).Filename != filename || !r.spxResourceSet.Contains(ref.ID) {
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
			Data: SpxResourceRefDocumentLinkData{
				Kind: ref.Kind,
			},
		})
	}
	return links
}
