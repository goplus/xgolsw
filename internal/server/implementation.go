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
	_, obj, _ := objectAtPosition(proj, typeInfo, astFile, position)
	if !xgoutil.IsInMainPkg(obj) {
		return nil, nil
	}

	if method, ok := obj.(*gotypes.Func); ok {
		if recv := method.Signature().Recv(); recv != nil && gotypes.IsInterface(recv.Type()) {
			locations := s.findImplementingMethodDefinitions(proj, typeInfo, recv.Type().(*gotypes.Interface), method.Name())
			return DedupeLocations(locations), nil
		}
	}

	if xgoutil.PosTokenFile(proj.Fset, obj.Pos()) == nil {
		return nil, nil
	}
	return s.locationForPos(proj, obj.Pos()), nil
}

// findImplementingMethodDefinitions finds the definition locations of project
// methods that implement the given interface method.
func (s *Server) findImplementingMethodDefinitions(proj *xgo.Project, typeInfo *types.Info, iface *gotypes.Interface, methodName string) []Location {
	var implementations []Location
	for _, obj := range typeInfo.Defs {
		if obj == nil {
			continue
		}
		named, ok := obj.Type().(*gotypes.Named)
		if !ok || named.Obj().Pkg() != typeInfo.Pkg || !gotypes.Implements(named, iface) {
			continue
		}

		for method := range named.Methods() {
			if method.Name() == methodName {
				implementations = append(implementations, s.locationForPos(proj, method.Pos()))
			}
		}
	}
	return implementations
}
