package server

import (
	gotypes "go/types"

	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_implementation
func (s *Server) textDocumentImplementation(params *ImplementationParams) (any, error) {
	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if astFile == nil {
		return nil, nil
	}
	position := ToPosition(result.proj, astFile, params.Position)
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}
	ident := xgoutil.IdentAtPosition(result.proj.Fset, typeInfo, astFile, position)

	obj := typeInfo.ObjectOf(ident)
	if !xgoutil.IsInMainPkg(obj) {
		return nil, nil
	}

	if method, ok := obj.(*gotypes.Func); ok {
		if recv := method.Signature().Recv(); recv != nil {
			if recvType := recv.Type(); gotypes.IsInterface(recvType) {
				locations := s.findImplementingMethodDefinitions(result, recvType.(*gotypes.Interface), method.Name())
				return DedupeLocations(locations), nil
			}
		}
	}

	return s.locationForPos(result.proj, obj.Pos()), nil
}

// findImplementingMethodDefinitions finds the definition locations of all
// methods that implement the given interface method.
func (s *Server) findImplementingMethodDefinitions(result *compileResult, iface *gotypes.Interface, methodName string) []Location {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}

	var implementations []Location
	for _, obj := range typeInfo.Defs {
		if obj == nil {
			continue
		}
		named, ok := obj.Type().(*gotypes.Named)
		if !ok {
			continue
		}
		if !gotypes.Implements(named, iface) {
			continue
		}

		for method := range named.Methods() {
			if method.Name() == methodName {
				implementations = append(implementations, s.locationForPos(result.proj, method.Pos()))
			}
		}
	}
	return implementations
}
