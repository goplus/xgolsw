package server

import (
	"fmt"
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
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
	ident, obj, _ := objectAtPosition(proj, typeInfo, astFile, position)
	if obj == nil {
		return nil, nil
	}

	target := obj
	if used := typeInfo.Uses[ident]; used != nil {
		target = used
	}
	if name, ok := target.(*gotypes.TypeName); ok {
		typ := name.Type()
		for node := range xgoutil.PathEnclosingIntervalNodes(astFile, ident.Pos(), ident.End(), false) {
			if selector, ok := node.(*ast.SelectorExpr); ok && selector.Sel == ident {
				if valueType := typeInfo.TypeOf(selector); xgoutil.IsValidType(valueType) {
					typ = valueType
				}
				break
			}
		}
		if iface, ok := typ.Underlying().(*gotypes.Interface); ok {
			locations := s.findImplementingTypeDefinitions(proj, typeInfo, iface, name)
			return DedupeLocations(locations), nil
		}
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

// findImplementingTypeDefinitions finds project type declarations that implement
// the interface, including pointer receivers and generated classfile types.
func (s *Server) findImplementingTypeDefinitions(proj *xgo.Project, typeInfo *types.Info, iface *gotypes.Interface, target *gotypes.TypeName) []Location {
	if iface.NumMethods() == 0 {
		return nil
	}
	info, err := methodInfoForProject(proj)
	if err != nil {
		return nil
	}
	if named, ok := gotypes.Unalias(target.Type()).(*gotypes.Named); ok {
		target = named.Obj()
	}
	var locations []Location
	classes := make(map[*gotypes.Named]bool)
	for _, receiver := range info.receivers {
		named, ok := receiver.typ.(*gotypes.Named)
		if !ok || named.Obj() == target || named.Obj().Pkg() != typeInfo.Pkg {
			continue
		}
		if !receiver.implements(iface) {
			continue
		}
		if file, pos := objectSource(proj, named.Obj()); file != nil {
			locations = append(locations, s.locationForPos(proj, pos))
			continue
		}
		classes[named.Origin()] = true
	}
	if len(classes) != 0 {
		astPkg, _ := proj.ASTPackage()
		for filename, file := range astPkg.Files {
			if classes[classTypeForFile(proj, file)] {
				locations = append(locations, Location{URI: s.toDocumentURI(filename)})
			}
		}
	}
	return locations
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
