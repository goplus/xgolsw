package server

import (
	"fmt"
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
	position := ToPosition(proj, astFile, params.Position)
	if hover, err := s.hoverForResource(proj, position, markupKind); hover != nil || err != nil {
		return hover, err
	}
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	ctx := &definitionContext{
		proj:         proj,
		typeDisplay:  newTypeDisplay(proj, astFile, PosAt(proj, astFile, params.Position)),
		enumInfo:     newEnumInfo(astPkg, typeInfo),
		lookupPkgDoc: s.lookupPkgDoc,
	}
	if hover := hoverForXGoUnit(proj, typeInfo, astFile, position, markupKind); hover != nil {
		return hover, nil
	}
	if tokenFile := xgoutil.NodeTokenFile(proj.Fset, astFile); tokenFile != nil {
		pos := tokenFile.Pos(position.Offset)
		if member := ctx.enumInfo.declarationMemberAt(pos); member != nil {
			def := ctx.definitionForEnumMembers(member)
			return hoverForDefinitions(proj, []symbolDefinition{def}, member.ident, markupKind), nil
		}
		if ident, obj := ctx.enumInfo.regularConstDeclarationAt(pos); ident != nil {
			return hoverForDefinitions(proj, ctx.definitionsFor(obj, ""), ident, markupKind), nil
		}
	}
	ident, obj, kwargTarget := objectAtPosition(proj, typeInfo, astFile, position)
	if kwargTarget != nil {
		return hoverForDefinitions(
			proj, ctx.definitionsForSelection(obj, kwargTarget.receiver), kwargTarget.ident, markupKind,
		), nil
	}
	if ident == nil {
		// Check if the position is within an import declaration.
		// If so, return the package documentation.
		pkgDoc, imp := ctx.importDocumentationAtPosition(astFile, position)
		if pkgDoc == nil {
			return nil, nil
		}
		return &Hover{
			Contents: MarkupContent{
				Kind:  markupKind,
				Value: godoc.Synopsis(pkgDoc.Doc),
			},
			Range: RangeForNode(proj, imp),
		}, nil
	}
	if ident.Name == "this" && xgoutil.IsSyntheticThisIdent(proj.Fset, typeInfo, astPkg, ident) {
		return nil, nil
	}
	return hoverForDefinitions(proj, ctx.definitionsForIdent(ident), ident, markupKind), nil
}

// hoverForDefinitions renders symbol definitions into a hover at node.
func hoverForDefinitions(proj *xgo.Project, defs []symbolDefinition, node ast.Node, markupKind MarkupKind) *Hover {
	if len(defs) == 0 {
		return nil
	}

	separator := ""
	if markupKind == PlainText {
		separator = "\n\n"
	}
	var hoverContent strings.Builder
	for i, def := range defs {
		if i > 0 {
			hoverContent.WriteString(separator)
		}
		hoverContent.WriteString(def.markupContent(markupKind).Value)
	}
	return &Hover{
		Contents: MarkupContent{
			Kind:  markupKind,
			Value: hoverContent.String(),
		},
		Range: RangeForNode(proj, node),
	}
}
