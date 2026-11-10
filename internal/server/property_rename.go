package server

import (
	"fmt"
	gotypes "go/types"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// notifyPropertiesRenamed reports properties that still resolve to the renamed
// declarations after applying the edit. The preview uses the same property
// lookup and framework policy as getProperties without changing project state.
func (s *Server) notifyPropertiesRenamed(proj *xgo.Project, params *RenameParams, preview *renamePreview, objects ...gotypes.Object) error {
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
	declarations := make(map[token.Pos]gotypes.Object, len(renamed))
	for obj := range renamed {
		declarations[preview.position(proj, info.ObjToDef[obj].Pos())] = obj
	}
	previewInfo, _ := preview.project.TypeInfo()
	if previewInfo == nil {
		return nil
	}
	ctx = &definitionContext{proj: preview.project}
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
				_, pos := objectSource(preview.project, previewInfo.ObjectDeclaration(property.Object))
				members[property.Name] = declarations[pos]
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
