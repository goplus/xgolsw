package server

import (
	"fmt"
	"go/types"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_declaration
func (s *Server) textDocumentDeclaration(params *DeclarationParams) (any, error) {
	return s.textDocumentDefinition(&DefinitionParams{
		TextDocumentPositionParams: params.TextDocumentPositionParams,
		WorkDoneProgressParams:     params.WorkDoneProgressParams,
		PartialResultParams:        params.PartialResultParams,
	})
}

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_definition
func (s *Server) textDocumentDefinition(params *DefinitionParams) (any, error) {
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
	position := s.toPosition(proj, astFile, params.Position)
	obj := getTypeInfo(proj).ObjectOf(s.identAtASTFilePosition(proj, astFile, position))
	if !isMainPkgObject(obj) {
		return nil, nil
	}

	location := Location{
		URI:   s.toDocumentURI(s.posFilename(proj, obj.Pos())),
		Range: s.rangeForASTFilePosition(proj, astFile, proj.Fset.Position(obj.Pos())),
	}
	return location, nil
}

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_typeDefinition
func (s *Server) textDocumentTypeDefinition(params *TypeDefinitionParams) (any, error) {
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
	position := s.toPosition(proj, astFile, params.Position)

	obj := getTypeInfo(proj).ObjectOf(s.identAtASTFilePosition(proj, astFile, position))
	if !isMainPkgObject(obj) {
		return nil, nil
	}

	objType := unwrapPointerType(obj.Type())
	named, ok := objType.(*types.Named)
	if !ok {
		return nil, nil
	}

	objPos := named.Obj().Pos()
	if !s.isInFset(proj, objPos) {
		return nil, nil
	}
	return s.locationForPos(proj, objPos), nil
}
