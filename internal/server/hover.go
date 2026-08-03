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
	markupKind := Markdown
	if capabilities, ok := s.hoverClientCapabilities(); ok {
		markupKind = preferredMarkupKind(capabilities.ContentFormat)
	}

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
			Contents: resourceMarkupContent(spxResourceRef.ID.URI(), markupKind),
			Range:    RangeForNode(result.proj, spxResourceRef.Node),
		}, nil
	}

	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	if hover := hoverForXGoUnit(result.proj, typeInfo, astFile, position, markupKind); hover != nil {
		return hover, nil
	}
	if tokenFile := xgoutil.NodeTokenFile(result.proj.Fset, astFile); tokenFile != nil {
		pos := tokenFile.Pos(position.Offset)
		if member := result.enumInfo.declarationMemberAt(pos); member != nil {
			def := result.spxDefinitionForEnumMembers(member)
			return hoverForSpxDefs(result.proj, []SpxDefinition{def}, member.ident, markupKind), nil
		}
		if ident, obj := result.enumInfo.regularConstDeclarationAt(pos); ident != nil {
			return hoverForSpxDefs(result.proj, result.spxDefinitionsFor(obj, ""), ident, markupKind), nil
		}
	}
	ident, obj, kwargTarget := objectAtPosition(result.proj, typeInfo, astFile, position)
	if kwargTarget != nil {
		return hoverForSpxDefs(
			result.proj, result.spxDefinitionsFor(obj, getTypeFromObject(typeInfo, obj)), kwargTarget.ident, markupKind,
		), nil
	}
	if ident == nil {
		// Check if the position is within an import declaration.
		// If so, return the package documentation.
		rpkg := result.spxImportsAtASTFilePosition(astFile, position)
		if rpkg != nil {
			return &Hover{
				Contents: MarkupContent{
					Kind:  markupKind,
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
	return hoverForSpxDefs(result.proj, result.spxDefinitionsForIdent(ident), ident, markupKind), nil
}

// hoverForSpxDefs renders spx definitions into a hover at node.
func hoverForSpxDefs(proj *xgo.Project, spxDefs []SpxDefinition, node ast.Node, markupKind MarkupKind) *Hover {
	if len(spxDefs) == 0 {
		return nil
	}

	separator := ""
	if markupKind == PlainText {
		separator = "\n\n"
	}
	var hoverContent strings.Builder
	for i, spxDef := range spxDefs {
		if i > 0 {
			hoverContent.WriteString(separator)
		}
		hoverContent.WriteString(spxDef.markupContent(markupKind).Value)
	}
	return &Hover{
		Contents: MarkupContent{
			Kind:  markupKind,
			Value: hoverContent.String(),
		},
		Range: RangeForNode(proj, node),
	}
}
