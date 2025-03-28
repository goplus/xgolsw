package server

import (
	"go/types"

	"github.com/goplus/goxlsw/gop/goputil"
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
	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if astFile == nil {
		return nil, nil
	}
	position := ToPosition(result.proj, astFile, params.Position)

	ident := goputil.IdentAtPosition(result.proj, astFile, position)
	obj := getTypeInfo(result.proj).ObjectOf(ident)
	if !goputil.IsInMainPkg(obj) {
		return nil, nil
	}

	defIdent := goputil.DefIdentFor(result.proj, obj)
	if defIdent == nil {
		objPos := obj.Pos()
		if goputil.PosTokenFile(result.proj, objPos) == nil {
			return nil, nil
		}
		return s.locationForPos(result.proj, objPos), nil
	} else if goputil.NodeTokenFile(result.proj, defIdent) == nil {
		return nil, nil
	}
	return s.locationForNode(result.proj, defIdent), nil
}

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_typeDefinition
func (s *Server) textDocumentTypeDefinition(params *TypeDefinitionParams) (any, error) {
	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if astFile == nil {
		return nil, nil
	}
	position := ToPosition(result.proj, astFile, params.Position)

	ident := goputil.IdentAtPosition(result.proj, astFile, position)
	obj := getTypeInfo(result.proj).ObjectOf(ident)
	if !goputil.IsInMainPkg(obj) {
		return nil, nil
	}

	objType := goputil.DerefType(obj.Type())
	named, ok := objType.(*types.Named)
	if !ok {
		return nil, nil
	}

	objPos := named.Obj().Pos()
	if goputil.PosTokenFile(result.proj, objPos) == nil {
		return nil, nil
	}
	return s.locationForPos(result.proj, objPos), nil
}
