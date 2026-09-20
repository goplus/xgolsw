package server

import (
	"fmt"

	"github.com/goplus/xgolsw/xgo/types"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_documentHighlight
func (s *Server) textDocumentDocumentHighlight(params *DocumentHighlightParams) (*[]DocumentHighlight, error) {
	filename, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document URI %q: %w", params.TextDocument.URI, err)
	}
	proj := s.requestProject()
	astPkg, _ := proj.ASTPackage()
	if astPkg == nil {
		return nil, nil
	}
	astFile := astPkg.Files[filename]
	if astFile == nil || !astFile.Pos().IsValid() {
		return nil, nil
	}
	position := ToPosition(proj, astFile, params.Position)
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	_, targetObj, _ := sourceObjectAtPosition(proj, typeInfo, astFile, position)
	if targetObj == nil {
		return nil, nil
	}
	targetObj = typeInfo.ObjectDeclaration(targetObj)
	source, err := sourceInfoForProject(proj)
	if err != nil {
		return nil, err
	}
	var highlights []DocumentHighlight
	seen := make(map[DocumentHighlight]bool)
	for _, refs := range [][]sourceIdent{source.highlights[targetObj], source.kwargs[types.ObjectOrigin(targetObj)]} {
		for _, ref := range refs {
			if ref.file != astFile {
				continue
			}
			highlight := DocumentHighlight{Range: RangeForNode(proj, ref.ident), Kind: ref.kind}
			if !seen[highlight] {
				seen[highlight] = true
				highlights = append(highlights, highlight)
			}
		}
	}
	return &highlights, nil
}
