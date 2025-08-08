package server

import (
	"cmp"
	"slices"

	xgoast "github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification#textDocument_documentLink
func (s *Server) textDocumentDocumentLink(params *DocumentLinkParams) ([]DocumentLink, error) {
	result, spxFile, astFile, err := s.compileAndGetASTFileForDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if astFile == nil {
		return nil, nil
	}

	// Add links for spx resource references.
	links := make([]DocumentLink, 0, len(result.spxResourceRefs))
	for _, spxResourceRef := range result.spxResourceRefs {
		if xgoutil.NodeFilename(result.proj.Fset, spxResourceRef.Node) != spxFile {
			continue
		}
		target := URI(spxResourceRef.ID.URI())
		links = append(links, DocumentLink{
			Range:  RangeForNode(result.proj, spxResourceRef.Node),
			Target: &target,
			Data: SpxResourceRefDocumentLinkData{
				Kind: spxResourceRef.Kind,
			},
		})
	}

	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}

	// Add links for spx definitions.
	links = slices.Grow(links, len(typeInfo.Defs)+len(typeInfo.Uses))
	addLinksForIdent := func(ident *xgoast.Ident) {
		if xgoutil.NodeFilename(result.proj.Fset, ident) != spxFile {
			return
		}
		if spxDefs := result.spxDefinitionsForIdent(ident); spxDefs != nil {
			identRange := RangeForNode(result.proj, ident)
			for _, spxDef := range spxDefs {
				target := URI(spxDef.ID.String())
				links = append(links, DocumentLink{
					Range:  identRange,
					Target: &target,
				})
			}
		}
	}
	for ident := range typeInfo.Defs {
		addLinksForIdent(ident)
	}
	for ident := range typeInfo.Uses {
		addLinksForIdent(ident)
	}
	sortDocumentLinks(links)
	return links, nil
}

// sortDocumentLinks sorts the given document links in a stable manner.
func sortDocumentLinks(links []DocumentLink) {
	slices.SortFunc(links, func(a, b DocumentLink) int {
		// First compare by whether target is nil.
		if a.Target == nil && b.Target != nil {
			return -1
		}
		if a.Target != nil && b.Target == nil {
			return 1
		}

		// If both targets are nil, sort by line number.
		if a.Target == nil && b.Target == nil {
			return cmp.Compare(a.Range.Start.Line, b.Range.Start.Line)
		}

		// If both targets have values, sort by their string representation.
		aStr, bStr := string(*a.Target), string(*b.Target)
		if aStr != bStr {
			return cmp.Compare(aStr, bStr)
		}

		// If targets are the same, sort by line number for stability.
		if a.Range.Start.Line != b.Range.Start.Line {
			return cmp.Compare(a.Range.Start.Line, b.Range.Start.Line)
		}

		// If same line, sort by character position.
		return cmp.Compare(a.Range.Start.Character, b.Range.Start.Character)
	})
}
