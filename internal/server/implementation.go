package server

import (
	"fmt"
	gotypes "go/types"
	"iter"

	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_implementation
func (s *Server) textDocumentImplementation(params *ImplementationParams) (any, error) {
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
	_, obj, _ := objectAtPosition(proj, typeInfo, astFile, position)
	if obj == nil {
		return nil, nil
	}

	if method, ok := obj.(*gotypes.Func); ok {
		if recv := method.Signature().Recv(); recv != nil && gotypes.IsInterface(recv.Type()) {
			locations := s.findImplementingMethodDefinitions(proj, typeInfo, recv.Type().Underlying().(*gotypes.Interface), method)
			return DedupeLocations(locations), nil
		}
	}

	file, pos := objectSource(proj, obj)
	if file == nil {
		return nil, nil
	}
	return s.locationForPos(proj, pos), nil
}

// findImplementingMethodDefinitions finds the definition locations of project
// methods that implement the given interface method.
func (s *Server) findImplementingMethodDefinitions(proj *xgo.Project, typeInfo *types.Info, iface *gotypes.Interface, target *gotypes.Func) []Location {
	var locations []Location
	for method := range implementingMethods(typeInfo, iface, target) {
		if method.Pkg() == typeInfo.Pkg && xgoutil.PosTokenFile(proj.Fset, method.Pos()) != nil {
			locations = append(locations, s.locationForPos(proj, method.Pos()))
		}
	}
	return locations
}

// implementingMethods yields distinct methods that implement target on types
// used in the project, including methods promoted through embedded fields.
func implementingMethods(info *types.Info, iface *gotypes.Interface, target *gotypes.Func) iter.Seq[*gotypes.Func] {
	return func(yield func(*gotypes.Func) bool) {
		seen := make(map[*gotypes.Func]bool)
		for receiver := range projectReceiverTypes(info) {
			// The pointer method set includes value, pointer, and promoted methods.
			// Pointers to interfaces have no methods and are not implementations.
			pointer := gotypes.NewPointer(receiver)
			if !gotypes.Implements(receiver, iface) && !gotypes.Implements(pointer, iface) {
				continue
			}
			selection := gotypes.NewMethodSet(pointer).Lookup(target.Pkg(), target.Name())
			if selection == nil {
				// Implements tolerates invalid types to suppress follow-on errors.
				continue
			}
			method := selection.Obj().(*gotypes.Func)
			if seen[method] {
				continue
			}
			seen[method] = true
			if !yield(method) {
				return
			}
		}
	}
}
