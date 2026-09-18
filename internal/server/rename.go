package server

import (
	"fmt"
	gotypes "go/types"

	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_prepareRename
func (s *Server) textDocumentPrepareRename(params *PrepareRenameParams) (*Range, error) {
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

	ident, obj, kwargTarget := objectAtPosition(proj, typeInfo, astFile, position)
	if xgoutil.IsBlankIdent(ident) || xgoutil.IsSyntheticThisIdent(proj.Fset, typeInfo, astPkg, ident) {
		return nil, nil
	}
	if !xgoutil.IsRenameable(obj) {
		return nil, nil
	}
	if kwargTarget != nil {
		return ToPtr(RangeForNode(proj, kwargTarget.ident)), nil
	}
	defIdent := typeInfo.ObjToDef[obj]
	if defIdent == nil || defIdent.Implicit() || xgoutil.NodeTokenFile(proj.Fset, defIdent) == nil {
		return nil, nil
	}

	return ToPtr(RangeForNode(proj, ident)), nil
}

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_rename
func (s *Server) textDocumentRename(params *RenameParams) (*WorkspaceEdit, error) {
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
	if astFile == nil {
		return nil, nil
	}
	position := ToPosition(proj, astFile, params.Position)

	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}

	ident, obj, kwargTarget := objectAtPosition(proj, typeInfo, astFile, position)
	if xgoutil.IsBlankIdent(ident) || xgoutil.IsSyntheticThisIdent(proj.Fset, typeInfo, astPkg, ident) {
		return nil, nil
	}
	if !xgoutil.IsRenameable(obj) {
		return nil, nil
	}
	if kwargTarget != nil {
		kwargParams := *params
		kwargParams.NewName = kwargDefinitionRenameText(obj, params.NewName)
		params = &kwargParams
	}
	return s.renameObject(proj, params, typeInfo, obj)
}

// renameObject builds a workspace edit for renaming obj.
func (s *Server) renameObject(proj *xgo.Project, params *RenameParams, typeInfo *types.Info, obj gotypes.Object) (*WorkspaceEdit, error) {
	defIdent := typeInfo.ObjToDef[obj]
	if defIdent == nil || xgoutil.NodeTokenFile(proj.Fset, defIdent) == nil {
		return nil, fmt.Errorf("failed to find definition of object %q", obj.Name())
	}

	defLoc := s.locationForNode(proj, defIdent)

	workspaceEdit := WorkspaceEdit{
		Changes: map[DocumentURI][]TextEdit{
			defLoc.URI: {
				{
					Range:   defLoc.Range,
					NewText: params.NewName,
				},
			},
		},
	}
	refLocs := s.findReferenceLocations(proj, obj)
	kwargRefLocs := s.kwargReferenceLocations(proj, obj)
	kwargNewName := kwargRenameText(obj, params.NewName)
	kwargRefSet := make(map[Location]struct{}, len(kwargRefLocs))
	for _, refLoc := range kwargRefLocs {
		kwargRefSet[refLoc] = struct{}{}
	}

	seenRefLocs := make(map[Location]struct{}, len(refLocs)+len(kwargRefLocs))
	appendRefEdit := func(refLoc Location, newText string) {
		if _, ok := seenRefLocs[refLoc]; ok {
			return
		}
		seenRefLocs[refLoc] = struct{}{}
		workspaceEdit.Changes[refLoc.URI] = append(workspaceEdit.Changes[refLoc.URI], TextEdit{
			Range:   refLoc.Range,
			NewText: newText,
		})
	}

	for _, refLoc := range refLocs {
		newText := params.NewName
		if _, ok := kwargRefSet[refLoc]; ok {
			newText = kwargNewName
		}
		appendRefEdit(refLoc, newText)
	}
	for _, refLoc := range kwargRefLocs {
		appendRefEdit(refLoc, kwargNewName)
	}

	// Check if the renamed object is a property and send notification if needed
	if (&definitionContext{proj: proj}).isPropertyOfEnclosingType(obj) {
		s.notifyPropertyRenamed(obj, params)
	}
	return &workspaceEdit, nil
}
