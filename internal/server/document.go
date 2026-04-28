package server

import (
	"cmp"
	"slices"

	"github.com/goplus/xgo/ast"
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
		if !result.spxResourceSet.Contains(spxResourceRef.ID) {
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
	astPkg, _ := result.proj.ASTPackage()

	// Add links for spx definitions.
	links = slices.Grow(links, len(typeInfo.Defs)+len(typeInfo.Uses))
	addLinksForIdent := func(ident *ast.Ident) {
		if ident.Implicit() || xgoutil.NodeFilename(result.proj.Fset, ident) != spxFile {
			return
		}
		if xgoutil.IsBlankIdent(ident) || xgoutil.IsSyntheticThisIdent(result.proj.Fset, typeInfo, astPkg, ident) {
			return
		}
		if spxDefs := result.spxDefinitionsForIdent(ident); spxDefs != nil {
			links = appendSpxDefinitionDocumentLinks(links, RangeForNode(result.proj, ident), spxDefs)
		}
	}
	for ident := range typeInfo.Defs {
		addLinksForIdent(ident)
	}
	for ident := range typeInfo.Uses {
		addLinksForIdent(ident)
	}
	links = append(links, kwargDocumentLinks(result, astFile)...)
	sortDocumentLinks(links)
	return links, nil
}

// kwargDocumentLinks returns spx definition links for kwarg names in astFile.
func kwargDocumentLinks(result *compileResult, astFile *ast.File) []DocumentLink {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}

	var links []DocumentLink
	ast.Inspect(astFile, func(node ast.Node) bool {
		callExpr, ok := node.(*ast.CallExpr)
		if !ok || len(callExpr.Kwargs) == 0 {
			return true
		}

		for _, kwarg := range callExpr.Kwargs {
			for _, target := range lookupCallExprKwargTargets(result.proj, typeInfo, callExpr, kwarg.Name.Name) {
				var spxDefs []SpxDefinition
				switch {
				case target.Field != nil:
					spxDefs = result.spxDefinitionsFor(target.Field, getTypeFromObject(typeInfo, target.Field))
				case target.Method != nil:
					spxDefs = result.spxDefinitionsFor(target.Method, getTypeFromObject(typeInfo, target.Method))
				default:
					continue
				}
				links = appendSpxDefinitionDocumentLinks(links, RangeForNode(result.proj, kwarg.Name), spxDefs)
			}
		}
		return true
	})
	return links
}

// appendSpxDefinitionDocumentLinks appends document links for spxDefs at
// linkRange.
func appendSpxDefinitionDocumentLinks(links []DocumentLink, linkRange Range, spxDefs []SpxDefinition) []DocumentLink {
	for _, spxDef := range spxDefs {
		target := URI(spxDef.ID.String())
		links = append(links, DocumentLink{
			Range:  linkRange,
			Target: &target,
		})
	}
	return links
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
