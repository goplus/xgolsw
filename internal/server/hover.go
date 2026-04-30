package server

import (
	godoc "go/doc"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification#textDocument_hover
func (s *Server) textDocumentHover(params *HoverParams) (*Hover, error) {
	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if astFile == nil {
		return nil, nil
	}
	if !astFile.Pos().IsValid() {
		return nil, nil
	}
	position := ToPosition(result.proj, astFile, params.Position)

	if spxResourceRef := result.spxResourceRefAtPosition(position); spxResourceRef != nil {
		return &Hover{
			Contents: MarkupContent{
				Kind:  Markdown,
				Value: spxResourceRef.ID.URI().HTML(),
			},
			Range: RangeForNode(result.proj, spxResourceRef.Node),
		}, nil
	}

	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	if hover := hoverForXGoUnit(result.proj, typeInfo, astFile, position); hover != nil {
		return hover, nil
	}
	ident, obj, kwargTarget := objectAtPosition(result.proj, typeInfo, astFile, position)
	if kwargTarget != nil {
		return hoverForSpxDefs(result.proj, result.spxDefinitionsFor(obj, getTypeFromObject(typeInfo, obj)), kwargTarget.ident), nil
	}
	if ident == nil {
		// Check if the position is within an import declaration.
		// If so, return the package documentation.
		rpkg := result.spxImportsAtASTFilePosition(astFile, position)
		if rpkg != nil {
			return &Hover{
				Contents: MarkupContent{
					Kind:  Markdown,
					Value: godoc.Synopsis(rpkg.Pkg.Doc),
				},
				Range: RangeForNode(result.proj, rpkg.Node),
			}, nil
		}
		return nil, nil
	}
	if ident.Name == "this" {
		astPkg, _ := result.proj.ASTPackage()
		if xgoutil.IsSyntheticThisIdent(result.proj.Fset, typeInfo, astPkg, ident) {
			return nil, nil
		}
	}
	return hoverForSpxDefs(result.proj, result.spxDefinitionsForIdent(ident), ident), nil
}

// hoverForSpxDefs renders spx definitions into a hover at node.
func hoverForSpxDefs(proj *xgo.Project, spxDefs []SpxDefinition, node ast.Node) *Hover {
	if len(spxDefs) == 0 {
		return nil
	}

	var hoverContent strings.Builder
	for _, spxDef := range spxDefs {
		hoverContent.WriteString(spxDef.HTML())
	}
	return &Hover{
		Contents: MarkupContent{
			Kind:  Markdown,
			Value: hoverContent.String(),
		},
		Range: RangeForNode(proj, node),
	}
}
