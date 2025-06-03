package server

import (
	"cmp"
	"fmt"
	"slices"

	gopast "github.com/goplus/gop/ast"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification#textDocument_documentLink
func (s *Server) textDocumentDocumentLink(params *DocumentLinkParams) (links []DocumentLink, err error) {
	proj := s.getProj()
	if proj == nil {
		return nil, nil
	}
	spxFile, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document URI %q: %w", params.TextDocument.URI, err)
	}

	astFile, _ := proj.AST(spxFile)
	if astFile == nil {
		return nil, nil
	}

	typeInfo := getTypeInfo(proj)

	// Add links for spx resource references.
	// links = slices.Grow(links, len(result.spxResourceRefs))
	// for _, spxResourceRef := range result.spxResourceRefs {
	// 	if s.nodeFilename(proj, spxResourceRef.Node) != spxFile {
	// 		continue
	// 	}
	// 	target := URI(spxResourceRef.ID.URI())
	// 	links = append(links, DocumentLink{
	// 		Range:  s.rangeForNode(proj, spxResourceRef.Node),
	// 		Target: &target,
	// 		Data: SpxResourceRefDocumentLinkData{
	// 			Kind: spxResourceRef.Kind,
	// 		},
	// 	})
	// }

	// Add links for spx definitions.
	links = slices.Grow(links, len(typeInfo.Defs)+len(typeInfo.Uses))
	addLinksForIdent := func(ident *gopast.Ident) {
		if s.nodeFilename(proj, ident) != spxFile {
			return
		}
		if spxDefs := s.spxDefinitionsForIdent(proj, typeInfo, ident); spxDefs != nil {
			identRange := s.rangeForNode(proj, ident)
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
	return
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
