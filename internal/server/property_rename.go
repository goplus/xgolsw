package server

import (
	"bytes"
	"fmt"
	gotypes "go/types"
	"slices"

	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// notifyPropertiesRenamed reports properties that still resolve to the renamed
// declarations after applying the edit. The preview uses the same property
// lookup and framework policy as getProperties without changing project state.
func (s *Server) notifyPropertiesRenamed(proj *xgo.Project, params *RenameParams, edit *WorkspaceEdit, objects ...gotypes.Object) error {
	info, _ := proj.TypeInfo()
	renamed := make(map[gotypes.Object]bool, len(objects))
	for _, obj := range objects {
		switch obj := obj.(type) {
		case *gotypes.Var:
			if !obj.IsField() {
				continue
			}
		case *gotypes.Func:
			if obj.Signature().Recv() == nil {
				continue
			}
		default:
			continue
		}
		if obj.Name() != params.NewName {
			renamed[info.ObjectDeclaration(obj)] = true
		}
	}
	if len(renamed) == 0 {
		return nil
	}
	type candidate struct {
		params PropertyRenamedParams
		object gotypes.Object
	}
	var candidates []candidate
	ctx := &definitionContext{proj: proj}
	for _, name := range info.Pkg.Scope().Names() {
		typeName, ok := info.Pkg.Scope().Lookup(name).(*gotypes.TypeName)
		if !ok {
			continue
		}
		typ := gotypes.Unalias(xgoutil.DerefType(gotypes.Unalias(typeName.Type())))
		named, ok := typ.(*gotypes.Named)
		if !ok {
			continue
		}
		if _, ok := named.Underlying().(*gotypes.Struct); !ok {
			continue
		}
		for property := range ctx.propertyObjects(named) {
			obj := info.ObjectDeclaration(property.Object)
			if !renamed[obj] {
				continue
			}
			newName := params.NewName
			if _, method := obj.(*gotypes.Func); method {
				newName = functionAliasName(newName)
			}
			candidates = append(candidates, candidate{PropertyRenamedParams{
				Target:       name,
				OldName:      property.Name,
				NewName:      newName,
				TextDocument: TextDocumentIdentifier{URI: s.posDocumentURI(proj, obj.Pos())},
			}, obj})
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	preview, declarations, err := s.propertyRenamePreview(proj, edit, renamed)
	if err != nil {
		return err
	}
	previewInfo, _ := preview.TypeInfo()
	if previewInfo == nil {
		return nil
	}
	ctx = &definitionContext{proj: preview}
	properties := make(map[string]map[string]gotypes.Object)
	for _, candidate := range candidates {
		params := candidate.params
		members, checked := properties[params.Target]
		if !checked {
			members = make(map[string]gotypes.Object)
			properties[params.Target] = members
			obj, ok := previewInfo.Pkg.Scope().Lookup(params.Target).(*gotypes.TypeName)
			if !ok {
				continue
			}
			typ := gotypes.Unalias(xgoutil.DerefType(gotypes.Unalias(obj.Type())))
			named, ok := typ.(*gotypes.Named)
			if !ok {
				continue
			}
			for property := range ctx.propertyObjects(named) {
				_, pos := objectSource(preview, previewInfo.ObjectDeclaration(property.Object))
				position := preview.Fset.PositionFor(pos, false)
				members[property.Name] = declarations[position.Filename][position.Offset]
			}
		}
		if members[params.NewName] != candidate.object {
			continue
		}
		notification, err := jsonrpc2.NewNotification("textDocument/xgo.propertyRenamed", params)
		if err != nil {
			return fmt.Errorf("failed to create property renamed notification: %w", err)
		}
		if err := s.replier.ReplyMessage(notification); err != nil {
			return fmt.Errorf("failed to send property renamed notification: %w", err)
		}
	}
	return nil
}

// propertyRenamePreview applies the generated rename edits to an isolated
// project and tracks declaration offsets for checking property identity. A
// fresh file set keeps temporary source positions out of the current project.
func (s *Server) propertyRenamePreview(proj *xgo.Project, edit *WorkspaceEdit, renamed map[gotypes.Object]bool) (*xgo.Project, map[string]map[int]gotypes.Object, error) {
	info, _ := proj.TypeInfo()
	locations := make(map[Location]gotypes.Object, len(renamed))
	for obj := range renamed {
		locations[s.locationForNode(proj, info.ObjToDef[obj])] = obj
	}
	preview := proj.Fork()
	declarations := make(map[string]map[int]gotypes.Object)
	for uri, edits := range edit.Changes {
		filename, err := s.fromDocumentURI(uri)
		if err != nil {
			return nil, nil, err
		}
		file, _ := proj.File(filename)
		astFile, _ := proj.ASTFile(filename)
		updated := *file
		edits = slices.Clone(edits)
		slices.SortFunc(edits, func(a, b TextEdit) int { return comparePositions(a.Range.Start, b.Range.Start) })
		var content bytes.Buffer
		var offset int
		declarations[filename] = make(map[int]gotypes.Object)
		for _, edit := range edits {
			start := ToPosition(proj, astFile, edit.Range.Start).Offset
			content.Write(file.Content[offset:start])
			if obj := locations[Location{URI: uri, Range: edit.Range}]; obj != nil {
				declarations[filename][content.Len()] = obj
			}
			content.WriteString(edit.NewText)
			offset = ToPosition(proj, astFile, edit.Range.End).Offset
		}
		content.Write(file.Content[offset:])
		updated.Content = content.Bytes()
		preview.PutFile(filename, &updated)
	}
	return preview, declarations, nil
}
