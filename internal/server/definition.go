package server

import (
	"fmt"
	gotypes "go/types"

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
	filename, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document URI %q: %w", params.TextDocument.URI, err)
	}

	astFile, _ := proj.ASTFile(filename)
	if astFile == nil {
		return nil, nil
	}
	position := ToPosition(proj, astFile, params.Position)
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	astPkg, _ := proj.ASTPackage()

	ident, obj, _ := objectAtPosition(proj, typeInfo, astFile, position)
	if xgoutil.IsBlankIdent(ident) || xgoutil.IsSyntheticThisIdent(proj.Fset, typeInfo, astPkg, ident) {
		return nil, nil
	}
	if obj == nil {
		return nil, nil
	}
	loc := s.objectDefinitionLocation(proj, typeInfo, obj)
	if loc == nil {
		return nil, nil
	}
	return *loc, nil
}

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_typeDefinition
func (s *Server) textDocumentTypeDefinition(params *TypeDefinitionParams) (any, error) {
	proj := s.getProjWithFile()
	filename, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document URI %q: %w", params.TextDocument.URI, err)
	}

	astFile, _ := proj.ASTFile(filename)
	if astFile == nil {
		return nil, nil
	}
	position := ToPosition(proj, astFile, params.Position)
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	_, obj, _ := objectAtPosition(proj, typeInfo, astFile, position)
	if !xgoutil.IsInMainPkg(obj) {
		return nil, nil
	}

	var typeName *gotypes.TypeName
	switch objType := xgoutil.DerefType(obj.Type()).(type) {
	case *gotypes.Named:
		typeName = objType.Obj()
	case *gotypes.Alias:
		typeName = objType.Obj()
	default:
		return nil, nil
	}

	if typeName.Pkg() != typeInfo.Pkg || xgoutil.PosTokenFile(proj.Fset, typeName.Pos()) == nil {
		return nil, nil
	}
	return s.locationForPos(proj, typeName.Pos()), nil
}
