package server

import (
	"fmt"
	gotypes "go/types"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_prepareRename
func (s *Server) textDocumentPrepareRename(params *PrepareRenameParams) (*Range, error) {
	proj := s.requestProject()
	filename, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document URI %q: %w", params.TextDocument.URI, err)
	}

	astFile, _ := proj.ASTFile(filename)
	if astFile == nil || !astFile.Pos().IsValid() {
		return nil, nil
	}
	position := ToPosition(proj, astFile, params.Position)

	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	astPkg, _ := proj.ASTPackage()

	ident, obj, kwargTarget := sourceObjectAtPosition(proj, typeInfo, astFile, position)
	if xgoutil.IsBlankIdent(ident) || xgoutil.IsSyntheticThisIdent(proj.Fset, typeInfo, astPkg, ident) {
		return nil, nil
	}
	obj = renameTarget(typeInfo, obj)
	if !isRenameableObject(proj, obj) {
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

	ident, obj, kwargTarget := sourceObjectAtPosition(proj, typeInfo, astFile, position)
	if xgoutil.IsBlankIdent(ident) || xgoutil.IsSyntheticThisIdent(proj.Fset, typeInfo, astPkg, ident) {
		return nil, nil
	}
	obj = renameTarget(typeInfo, obj)
	if !isRenameableObject(proj, obj) {
		return nil, nil
	}
	if params.NewName == "_" || !token.IsIdentifier(params.NewName) {
		return nil, fmt.Errorf("%w: invalid identifier %q", jsonrpc2.ErrInvalidParams, params.NewName)
	}
	if kwargTarget != nil {
		kwargParams := *params
		kwargParams.NewName = kwargDefinitionRenameText(obj, params.NewName)
		params = &kwargParams
	} else if _, ok := obj.(*gotypes.Func); ok && ident.Name != obj.Name() && functionNameForAlias(ident.Name) == obj.Name() {
		aliasParams := *params
		aliasParams.NewName = functionNameForAlias(params.NewName)
		params = &aliasParams
	}
	if method, ok := obj.(*gotypes.Func); ok && method.Signature().Recv() != nil {
		return s.renameMethod(proj, params, typeInfo, method)
	}
	edit, err := s.renameObject(proj, params, typeInfo, obj)
	if err != nil {
		return nil, err
	}
	if (&definitionContext{proj: proj}).isPropertyOfEnclosingType(obj) {
		s.notifyPropertyRenamed(proj, obj, params)
	}
	return edit, nil
}

// renameMethod renames related interface and implementation declarations
// together so the resulting edits preserve their implementation relationships.
func (s *Server) renameMethod(proj *xgo.Project, params *RenameParams, info *types.Info, target *gotypes.Func) (*WorkspaceEdit, error) {
	methods := relatedMethodDeclarations(info, target)
	for _, method := range methods {
		ident := info.ObjToDef[method]
		if method.Pkg() != info.Pkg || ident == nil || ident.Implicit() {
			return nil, fmt.Errorf("cannot rename %s: related method %s has no editable project declaration", target.Name(), method.FullName())
		}
	}
	if err := checkMethodRenameConflicts(info, methods, params.NewName); err != nil {
		return nil, err
	}
	result := &WorkspaceEdit{Changes: make(map[DocumentURI][]TextEdit)}
	for _, method := range methods {
		edit, err := s.renameObject(proj, params, info, method)
		if err != nil {
			return nil, err
		}
		for uri, edits := range edit.Changes {
			result.Changes[uri] = append(result.Changes[uri], edits...)
		}
	}
	for _, method := range methods {
		if (&definitionContext{proj: proj}).isPropertyOfEnclosingType(method) {
			s.notifyPropertyRenamed(proj, method, params)
		}
	}
	return result, nil
}

// checkMethodRenameConflicts rejects names that collide with existing members,
// including members that would hide a renamed method on an embedding receiver.
func checkMethodRenameConflicts(info *types.Info, methods []*gotypes.Func, name string) error {
	if name == methods[0].Name() {
		return nil
	}
	names := []string{name}
	alias := functionAliasName(name)
	for _, method := range methods {
		if alias == name || !token.IsIdentifier(alias) {
			break
		}
		for _, ident := range info.RefIdentsFor(method) {
			if ident.Name != method.Name() && functionNameForAlias(ident.Name) == method.Name() {
				names = []string{name, alias}
				break
			}
		}
	}
	checkReceiver := func(receiver gotypes.Type) error {
		for _, name := range names {
			obj, index, _ := gotypes.LookupFieldOrMethod(receiver, true, info.Pkg, name)
			if obj != nil || index != nil {
				return fmt.Errorf("cannot rename %s to %s: conflicts with an existing member of %s", methods[0].Name(), name, receiver)
			}
		}
		return nil
	}
	renamed := make(map[*gotypes.Func]bool, len(methods))
	for _, method := range methods {
		renamed[method] = true
		if err := checkReceiver(method.Signature().Recv().Type()); err != nil {
			return err
		}
	}
	for receiver := range projectReceiverTypes(info) {
		obj, _, _ := gotypes.LookupFieldOrMethod(receiver, true, info.Pkg, methods[0].Name())
		method, ok := obj.(*gotypes.Func)
		if ok && renamed[method.Origin()] {
			if err := checkReceiver(receiver); err != nil {
				return err
			}
		}
	}
	return nil
}

// renameTarget returns the shared declaration to rename. Embedded fields use
// the recorded type use that determines their name.
func renameTarget(info *types.Info, obj gotypes.Object) gotypes.Object {
	obj = info.ObjectDeclaration(obj)
	if field, ok := obj.(*gotypes.Var); ok && field.Embedded() {
		return info.Uses[info.ObjToDef[field.Origin()]]
	}
	return obj
}

// isRenameableObject includes local types and range variables whose declaration
// is recorded in the project without an object position from the compiler.
func isRenameableObject(proj *xgo.Project, obj gotypes.Object) bool {
	if xgoutil.IsRenameable(obj) {
		return true
	}
	switch obj.(type) {
	case *gotypes.Var, *gotypes.TypeName:
		file, _ := objectSource(proj, obj)
		return file != nil
	default:
		return false
	}
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
	if _, ok := obj.(*gotypes.TypeName); ok {
		for _, field := range typeInfo.Defs {
			if field != obj && renameTarget(typeInfo, field) == obj {
				refLocs = append(refLocs, s.findReferenceLocations(proj, field)...)
				kwargRefLocs = append(kwargRefLocs, s.kwargReferenceLocations(proj, field)...)
			}
		}
	}
	kwargNewName := kwargRenameText(obj, params.NewName)
	if len(kwargRefLocs) != 0 && !token.IsIdentifier(kwargNewName) {
		return nil, fmt.Errorf("%w: invalid keyword argument name %q", jsonrpc2.ErrInvalidParams, kwargNewName)
	}
	kwargRefSet := make(map[Location]struct{}, len(kwargRefLocs))
	for _, refLoc := range kwargRefLocs {
		kwargRefSet[refLoc] = struct{}{}
	}
	aliasRenames := s.functionAliasRenames(proj, typeInfo, obj, params.NewName)

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
		if aliasText, ok := aliasRenames[refLoc]; ok {
			newText = aliasText
		}
		if _, ok := kwargRefSet[refLoc]; ok {
			newText = kwargNewName
		}
		appendRefEdit(refLoc, newText)
	}
	for _, refLoc := range kwargRefLocs {
		appendRefEdit(refLoc, kwargNewName)
	}

	return &workspaceEdit, nil
}

// functionNameForAlias resolves the compiler's ASCII lowercase and underscore
// alias spellings to their declaration names.
func functionNameForAlias(name string) string {
	if strings.HasPrefix(name, "_") {
		return "XGo" + name
	}
	return upperFirstASCII(name)
}

// functionAliasName returns the source alias for a function declaration, or its
// unchanged name when the compiler does not supply an alias.
func functionAliasName(name string) string {
	if strings.HasPrefix(name, "XGo_") {
		return strings.TrimPrefix(name, "XGo")
	}
	return xgoutil.ToLowerCamelCase(name)
}

// functionAliasRenames preserves alias calls and automatic property reads.
// If newName has no usable alias, property reads become explicit calls.
func (s *Server) functionAliasRenames(proj *xgo.Project, info *types.Info, obj gotypes.Object, newName string) map[Location]string {
	if _, ok := obj.(*gotypes.Func); !ok {
		return nil
	}
	newAlias := functionAliasName(newName)
	useAlias := newAlias != newName && token.IsIdentifier(newAlias)
	renamed := make(map[Location]string)
	parentsByFile := make(map[*ast.File]map[ast.Node]ast.Node)
	for _, ident := range info.RefIdentsFor(obj) {
		if ident.Name == obj.Name() || functionNameForAlias(ident.Name) != obj.Name() {
			continue
		}
		file := sourceASTFile(proj, ident.Pos())
		if file == nil {
			continue
		}
		if useAlias {
			renamed[s.locationForNode(proj, ident)] = newAlias
			continue
		}

		parents := parentsByFile[file]
		if parents == nil {
			parents = nodeParents(file)
			parentsByFile[file] = parents
		}
		var expr ast.Node = ident
		if sel, ok := parents[expr].(*ast.SelectorExpr); ok && sel.Sel == ident {
			expr = sel
		}
		for {
			paren, ok := parents[expr].(*ast.ParenExpr)
			if !ok {
				break
			}
			expr = paren
		}
		text := newName
		if call, ok := parents[expr].(*ast.CallExpr); !ok || call.Fun != expr {
			text += "()"
		}
		renamed[s.locationForNode(proj, ident)] = text
	}
	return renamed
}
