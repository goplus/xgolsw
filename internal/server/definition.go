package server

import (
	"fmt"
	"go/types"

	"github.com/goplus/xgolsw/xgo/xgoutil"
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
	proj := s.getProjWithFile()
	if proj == nil {
		return nil, nil
	}

	spxFile, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document URI %q: %w", params.TextDocument.URI, err)
	}

	astFile, _ := proj.ASTFile(spxFile)
	if astFile == nil {
		return nil, nil
	}
	position := ToPosition(proj, astFile, params.Position)
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	ident := xgoutil.IdentAtPosition(proj.Fset, typeInfo, astFile, position)

	obj := typeInfo.ObjectOf(ident)
	if !xgoutil.IsInMainPkg(obj) || !obj.Pos().IsValid() {
		return nil, nil
	}

	defIdent := typeInfo.ObjToDef[obj]
	if defIdent == nil {
		// Fall back to the start position of the object identifier in declaration.
		return s.locationForPos(proj, obj.Pos()), nil
	}
	return s.locationForNode(proj, defIdent), nil
}

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_typeDefinition
func (s *Server) textDocumentTypeDefinition(params *TypeDefinitionParams) (any, error) {
	proj := s.getProjWithFile()
	if proj == nil {
		return nil, nil
	}
	spxFile, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document URI %q: %w", params.TextDocument.URI, err)
	}

	astFile, _ := proj.ASTFile(spxFile)
	if astFile == nil {
		return nil, nil
	}
	position := ToPosition(proj, astFile, params.Position)
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	ident := xgoutil.IdentAtPosition(proj.Fset, typeInfo, astFile, position)

	obj := typeInfo.ObjectOf(ident)
	if !xgoutil.IsInMainPkg(obj) {
		return nil, nil
	}

	objType := xgoutil.DerefType(obj.Type())
	named, ok := objType.(*types.Named)
	if !ok {
		return nil, nil
	}

	objPos := named.Obj().Pos()
	if xgoutil.PosTokenFile(proj.Fset, objPos) == nil {
		return nil, nil
	}
	return s.locationForPos(proj, objPos), nil
}
