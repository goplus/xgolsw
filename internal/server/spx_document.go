package server

import (
	"fmt"
	"path"
	"slices"

	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// documentLinksForSpxResources returns links to existing resources in an spx classfile.
func (s *Server) documentLinksForSpxResources(proj *xgo.Project, filename string) ([]DocumentLink, error) {
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
	links := make([]DocumentLink, 0, len(result.spxResourceRefs))
	for _, ref := range result.spxResourceRefs {
		if xgoutil.NodeFilename(proj.Fset, ref.Node) != filename || !result.spxResourceSet.Contains(ref.ID) {
			continue
		}
		target := URI(ref.ID.URI())
		links = append(links, DocumentLink{
			Range:  RangeForNode(proj, ref.Node),
			Target: &target,
			Data: SpxResourceRefDocumentLinkData{
				Kind: ref.Kind,
			},
		})
	}
	return links, nil
}
