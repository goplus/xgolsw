package server

import (
	"fmt"
	gotypes "go/types"

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
	info, err := methodInfoForProject(proj)
	if err != nil {
		return nil
	}
	var locations []Location
	for _, method := range info.implementations[iface]()[target.Id()] {
		if method.Pkg() == typeInfo.Pkg && xgoutil.PosTokenFile(proj.Fset, method.Pos()) != nil {
			locations = append(locations, s.locationForPos(proj, method.Pos()))
		}
	}
	return locations
}
