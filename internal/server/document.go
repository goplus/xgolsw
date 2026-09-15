package server

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification#textDocument_documentLink
func (s *Server) textDocumentDocumentLink(params *DocumentLinkParams) ([]DocumentLink, error) {
	filename, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document URI %q: %w", params.TextDocument.URI, err)
	}
	proj := s.getProjWithFile()
	astPkg, _ := proj.ASTPackage()
	if astPkg == nil {
		return nil, nil
	}
	astFile := astPkg.Files[filename]
	if astFile == nil || !astFile.Pos().IsValid() {
		return nil, nil
	}
	links, err := s.documentLinksForSpxResources(proj, filename)
	if err != nil {
		return nil, err
	}
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	ctx := &definitionContext{
		proj:         proj,
		enumInfo:     newEnumInfo(astPkg, typeInfo),
		lookupPkgDoc: s.lookupPkgDoc,
	}

	// Resolve kwarg names from their call context. The compiler can record
	// generated field, method, and factory identifiers at these positions.
	kwargNames := make(map[token.Pos]struct{})
	ast.Inspect(astFile, func(node ast.Node) bool {
		callExpr, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, kwarg := range callExpr.Kwargs {
			kwargNames[kwarg.Name.Pos()] = struct{}{}
			linkRange := RangeForNode(proj, kwarg.Name)
			seen := make(map[URI]struct{})
			for _, target := range lookupCallExprKwargTargets(typeInfo, callExpr, kwarg.Name.Name) {
				obj := kwargTargetObject(target.target)
				if obj == nil {
					continue
				}
				for _, def := range ctx.spxDefinitionsFor(obj, target.selectorTypeName) {
					uri := URI(def.ID.String())
					if _, ok := seen[uri]; ok {
						continue
					}
					seen[uri] = struct{}{}
					links = append(links, DocumentLink{Range: linkRange, Target: &uri})
				}
			}
		}
		return true
	})

	// Add links for symbol definitions and uses in this file.
	addLinksForIdent := func(ident *ast.Ident) {
		if ident.Implicit() || xgoutil.NodeFilename(proj.Fset, ident) != filename {
			return
		}
		if _, ok := kwargNames[ident.Pos()]; ok {
			return
		}
		if xgoutil.IsBlankIdent(ident) || xgoutil.IsSyntheticThisIdent(proj.Fset, typeInfo, astPkg, ident) {
			return
		}
		if spxDefs := ctx.spxDefinitionsForIdent(ident); spxDefs != nil {
			links = appendSpxDefinitionDocumentLinks(links, RangeForNode(proj, ident), spxDefs)
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
