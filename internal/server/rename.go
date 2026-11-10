package server

import (
	"bytes"
	"fmt"
	gotypes "go/types"
	"iter"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	xerrors "github.com/qiniu/x/errors"
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
		return new(RangeForNode(proj, kwargTarget.ident)), nil
	}
	defIdent := typeInfo.ObjToDef[obj]
	if defIdent == nil || defIdent.Implicit() || xgoutil.NodeTokenFile(proj.Fset, defIdent) == nil {
		return nil, nil
	}

	return new(RangeForNode(proj, ident)), nil
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
	} else if isAliasCallable(obj) && ident.Name != obj.Name() && functionNameForAlias(ident.Name) == obj.Name() {
		aliasParams := *params
		aliasParams.NewName = functionNameForAlias(params.NewName)
		params = &aliasParams
	}
	if method, ok := obj.(*gotypes.Func); ok && method.Signature().Recv() != nil {
		return s.renameMethod(proj, params, typeInfo, method)
	}
	if err := checkFieldRenameConflicts(typeInfo, obj, params.NewName); err != nil {
		return nil, err
	}
	edit, err := s.renameObject(proj, params, typeInfo, obj)
	if err != nil {
		return nil, err
	}
	preview, err := s.checkRenameConflicts(proj, edit)
	if err != nil {
		return nil, err
	}
	// Property notifications are best effort. Delivery failure must not
	// invalidate the rename edit, especially after partial delivery.
	s.notifyPropertiesRenamed(proj, params, preview, obj)
	return edit, nil
}

// renameMethod renames related interface and implementation declarations
// together so the resulting edits preserve their implementation relationships.
func (s *Server) renameMethod(proj *xgo.Project, params *RenameParams, info *types.Info, target *gotypes.Func) (*WorkspaceEdit, error) {
	methodInfo, err := methodInfoForProject(proj)
	if err != nil {
		return nil, err
	}
	source, err := sourceInfoForProject(proj)
	if err != nil {
		return nil, err
	}
	methods := methodInfo.relatedMethods(target)
	for _, method := range methods {
		ident := info.ObjToDef[method]
		if method.Pkg() != info.Pkg || ident == nil || ident.Implicit() {
			return nil, fmt.Errorf("cannot rename %s: related method %s has no editable project declaration", target.Name(), method.FullName())
		}
	}
	if err := checkMethodRenameConflicts(info, source, methodInfo, methods, params.NewName); err != nil {
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
	objects := make([]gotypes.Object, len(methods))
	for i, method := range methods {
		objects[i] = method
	}
	preview, err := s.checkRenameConflicts(proj, result)
	if err != nil {
		return nil, err
	}
	// Keep the complete method edit even if property notifications fail.
	s.notifyPropertiesRenamed(proj, params, preview, objects...)
	return result, nil
}

// checkRenameConflicts verifies the complete edit before sending notifications
// or returning it to the client. Existing project errors do not prevent checks
// of declarations and references that already have type information.
func (s *Server) checkRenameConflicts(proj *xgo.Project, edit *WorkspaceEdit) (*renamePreview, error) {
	preview, err := s.newRenamePreview(proj, edit)
	if err != nil {
		return nil, err
	}
	_, beforeErr := proj.TypeInfo()
	_, afterErr := preview.project.TypeInfo()
	if beforeErr == nil && afterErr != nil {
		return nil, fmt.Errorf("rename conflicts with existing code: %w", afterErr)
	}
	if afterErr != nil {
		type errorKey struct {
			position token.Pos
			code     typesutil.Code
		}
		// Error messages can include the old name. Match codes and translated
		// positions instead, preserving the number of errors at each location.
		existing := make(map[errorKey]int)
		for err := range renameTypeErrors(proj) {
			pos := err.Pos
			if pos.IsValid() {
				pos = preview.position(proj, pos)
			}
			key := errorKey{
				position: pos,
				code:     err.Code,
			}
			existing[key]++
		}
		for err := range renameTypeErrors(preview.project) {
			key := errorKey{
				position: err.Pos,
				code:     err.Code,
			}
			if existing[key] == 0 {
				return nil, fmt.Errorf("rename conflicts with existing code: %w", err)
			}
			existing[key]--
		}
	}
	before, err := sourceInfoForProject(proj)
	if err != nil {
		return nil, err
	}
	after, err := sourceInfoForProject(preview.project)
	if err != nil {
		return nil, err
	}
	if err := preview.checkBindings(proj, before.references, after.references); err != nil {
		return nil, err
	}
	if err := preview.checkBindings(proj, before.kwargs, after.kwargs); err != nil {
		return nil, err
	}
	if err := preview.checkBindings(proj, before.highlights, after.highlights); err != nil {
		return nil, err
	}
	conversions := renameConversions(preview.project)
	for pos, convertible := range renameConversions(proj) {
		if after, ok := conversions[preview.position(proj, pos)]; !ok || after != convertible {
			return nil, fmt.Errorf("rename conflicts with a type conversion at %s", proj.Fset.PositionFor(pos, false))
		}
	}
	return preview, nil
}

// renameConversions records whether source conversions use Go's conversion rules.
// XGo permits custom casts and can omit errors for invalid explicit conversions.
func renameConversions(proj *xgo.Project) map[token.Pos]bool {
	info, _ := expressionTypeInfo(proj)
	conversions := make(map[token.Pos]bool)
	for expr := range info.Types {
		call, ok := expr.(*ast.CallExpr)
		if !ok || len(call.Args) != 1 || sourceASTFile(proj, call.Pos()) == nil {
			continue
		}
		target := info.TypeOf(call.Fun)
		argument := info.TypeOf(call.Args[0])
		if isType, _ := xgoutil.IsTypeExpr(info, call.Fun); isType && xgoutil.IsValidType(target) && xgoutil.IsValidType(argument) {
			conversions[call.Pos()] = gotypes.ConvertibleTo(argument, target)
		}
	}
	return conversions
}

// renameTypeErrors yields checker errors in project source or without a position.
// Generated positions outside the source snapshot are excluded, as in diagnostics.
func renameTypeErrors(proj *xgo.Project) iter.Seq[typesutil.Error] {
	return func(yield func(typesutil.Error) bool) {
		_, err := proj.TypeInfo()
		errs, ok := err.(xerrors.List)
		if !ok {
			errs = xerrors.List{err}
		}
		for _, err := range errs {
			typeErr, ok := err.(typesutil.Error)
			if !ok || typeErr.Pos.IsValid() && sourceASTFile(proj, typeErr.Pos) == nil {
				continue
			}
			if !yield(typeErr) {
				return
			}
		}
	}
}

// checkFieldRenameConflicts rejects direct field and method collisions, which
// the XGo checker can otherwise accept until the conflicting member is used.
func checkFieldRenameConflicts(info *types.Info, target gotypes.Object, name string) error {
	if target.Name() == name {
		return nil
	}
	for receiver := range projectReceiverTypes(info) {
		structure, ok := receiver.Underlying().(*gotypes.Struct)
		if !ok {
			continue
		}
		for field := range structure.Fields() {
			if renameTarget(info, field) != target {
				continue
			}
			if obj, index, _ := gotypes.LookupFieldOrMethod(receiver, true, info.Pkg, name); obj != nil && len(index) == 1 {
				return fmt.Errorf("rename conflicts with member %s of %s", name, receiver)
			}
		}
	}
	return nil
}

// checkMethodRenameConflicts rejects names that collide with existing members,
// including members that would hide a renamed method on an embedding receiver.
func checkMethodRenameConflicts(info *types.Info, source *sourceInfo, methodInfo *methodInfo, methods []*gotypes.Func, name string) error {
	if name == methods[0].Name() {
		return nil
	}
	names := []string{name}
	alias := functionAliasName(name)
	for _, method := range methods {
		if alias == name || !token.IsIdentifier(alias) {
			break
		}
		for _, ref := range source.references[info.ObjectDeclaration(method)] {
			ident := ref.ident
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
	for _, candidate := range methodInfo.receivers {
		receiver := candidate.typ
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
	if !isAliasCallable(obj) {
		return nil
	}
	newAlias := functionAliasName(newName)
	useAlias := newAlias != newName && token.IsIdentifier(newAlias)
	renamed := make(map[Location]string)
	source, err := sourceInfoForProject(proj)
	if err != nil {
		return nil
	}
	for _, ref := range source.references[info.ObjectDeclaration(obj)] {
		ident := ref.ident
		if ident.Name == obj.Name() || functionNameForAlias(ident.Name) != obj.Name() {
			continue
		}
		text := newName
		if useAlias {
			text = newAlias
		} else if !ref.called {
			text += "()"
		}
		renamed[s.locationForNode(proj, ident)] = text
	}
	return renamed
}

// renameOffset records the cumulative byte displacement after a replacement.
type renameOffset struct {
	start int
	end   int
	delta int
}

// renamePreview checks edits without changing the source project or file set.
type renamePreview struct {
	project *xgo.Project
	offsets map[string][]renameOffset
}

// newRenamePreview applies edits to an isolated project and records byte
// displacements for checking declarations and references.
func (s *Server) newRenamePreview(proj *xgo.Project, edit *WorkspaceEdit) (*renamePreview, error) {
	preview := &renamePreview{
		project: proj.Fork(),
		offsets: make(map[string][]renameOffset),
	}
	for uri, edits := range edit.Changes {
		filename, err := s.fromDocumentURI(uri)
		if err != nil {
			return nil, err
		}
		file, _ := proj.File(filename)
		astFile, _ := proj.ASTFile(filename)
		updated := *file
		edits = slices.Clone(edits)
		slices.SortFunc(edits, func(a, b TextEdit) int { return comparePositions(a.Range.Start, b.Range.Start) })
		var content bytes.Buffer
		var offset int
		for _, edit := range edits {
			start := ToPosition(proj, astFile, edit.Range.Start).Offset
			end := ToPosition(proj, astFile, edit.Range.End).Offset
			content.Write(file.Content[offset:start])
			content.WriteString(edit.NewText)
			preview.offsets[filename] = append(preview.offsets[filename], renameOffset{
				start: start,
				end:   end,
				delta: content.Len() - end,
			})
			offset = end
		}
		content.Write(file.Content[offset:])
		updated.Content = content.Bytes()
		preview.project.PutFile(filename, &updated)
	}
	return preview, nil
}

// position translates an original source position into the preview's file set.
func (p *renamePreview) position(proj *xgo.Project, pos token.Pos) token.Pos {
	file := proj.Fset.File(pos)
	offset := file.Offset(pos)
	edits := p.offsets[file.Name()]
	i := sort.Search(len(edits), func(i int) bool { return edits[i].end > offset })
	if i < len(edits) && offset >= edits[i].start {
		offset = edits[i].start
	}
	if i > 0 {
		offset += edits[i-1].delta
	}
	astFile, _ := p.project.ASTFile(file.Name())
	return p.project.Fset.File(astFile.Pos()).Pos(offset)
}

// sameObject compares source declarations across file sets. Imported objects
// are shared, while compiler-generated objects retain their kind and name.
func (p *renamePreview) sameObject(proj *xgo.Project, before, after gotypes.Object) bool {
	info, _ := proj.TypeInfo()
	previewInfo, _ := p.project.TypeInfo()
	before, after = info.ObjectDeclaration(before), previewInfo.ObjectDeclaration(after)
	beforeFile, beforePos := objectSource(proj, before)
	afterFile, afterPos := objectSource(p.project, after)
	if beforeFile != nil || afterFile != nil {
		return beforeFile != nil && afterFile != nil && p.position(proj, beforePos) == afterPos
	}
	if before.Pkg() != nil && before.Pkg() != info.Pkg && before.Pkg().Path() != "" ||
		after.Pkg() != nil && after.Pkg() != previewInfo.Pkg && after.Pkg().Path() != "" {
		return before == after
	}
	return reflect.TypeOf(before) == reflect.TypeOf(after) && before.Id() == after.Id()
}

// checkBindings rejects changes to existing declarations and references, including
// references to other objects that would be captured by the renamed declaration.
func (p *renamePreview) checkBindings(proj *xgo.Project, before, after map[gotypes.Object][]sourceIdent) error {
	bindings := make(map[token.Pos][]gotypes.Object)
	for obj, refs := range after {
		for _, ref := range refs {
			bindings[ref.ident.Pos()] = append(bindings[ref.ident.Pos()], obj)
		}
	}
	for obj, refs := range before {
		for _, ref := range refs {
			if !slices.ContainsFunc(bindings[p.position(proj, ref.ident.Pos())], func(after gotypes.Object) bool {
				return p.sameObject(proj, obj, after)
			}) {
				return fmt.Errorf("rename conflicts with %q at %s: identifier binding would change", obj.Name(), proj.Fset.PositionFor(ref.ident.Pos(), false))
			}
		}
	}
	return nil
}
