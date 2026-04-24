package server

import (
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_references
func (s *Server) textDocumentReferences(params *ReferenceParams) ([]Location, error) {
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
	_, obj, _ := objectAtPosition(result.proj, typeInfo, astFile, position)
	if obj == nil {
		return nil, nil
	}

	var locations []Location

	locations = append(locations, s.findReferenceLocations(result, obj)...)
	locations = append(locations, s.kwargReferenceLocations(result, obj)...)

	if fn, ok := obj.(*gotypes.Func); ok && fn.Signature().Recv() != nil {
		locations = append(locations, s.handleMethodReferences(result, fn)...)
		locations = append(locations, s.handleEmbeddedFieldReferences(result, obj)...)
	}

	if params.Context.IncludeDeclaration {
		if loc := s.objectDefinitionLocation(result.proj, typeInfo, obj); loc != nil {
			locations = append(locations, *loc)
		}
	}

	return DedupeLocations(locations), nil
}

// findReferenceLocations returns all locations where the given object is referenced.
func (s *Server) findReferenceLocations(result *compileResult, obj gotypes.Object) []Location {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	refIdents := typeInfo.RefIdentsFor(obj)
	if len(refIdents) == 0 {
		return nil
	}
	locations := make([]Location, 0, len(refIdents))
	for _, refIdent := range refIdents {
		if refIdent.Implicit() {
			continue
		}
		locations = append(locations, s.locationForNode(result.proj, refIdent))
	}
	return locations
}

// handleMethodReferences finds all references to a method, including interface
// implementations and interface method references.
func (s *Server) handleMethodReferences(result *compileResult, fn *gotypes.Func) []Location {
	var locations []Location
	recvType := fn.Signature().Recv().Type()
	if gotypes.IsInterface(recvType) {
		iface, ok := recvType.(*gotypes.Interface)
		if !ok {
			return nil
		}
		methodName := fn.Name()
		locations = append(locations, s.findEmbeddedInterfaceReferences(result, iface, methodName)...)
		locations = append(locations, s.findImplementingMethodReferences(result, iface, methodName)...)
	} else {
		locations = append(locations, s.findInterfaceMethodReferences(result, fn)...)
	}
	return locations
}

// findEmbeddedInterfaceReferences finds references to methods in interfaces
// that embed the given interface.
func (s *Server) findEmbeddedInterfaceReferences(result *compileResult, iface *gotypes.Interface, methodName string) []Location {
	var locations []Location
	seenIfaces := make(map[*gotypes.Interface]bool)
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return locations
	}

	astPkg, _ := result.proj.ASTPackage()
	var find func(*gotypes.Interface)
	find = func(current *gotypes.Interface) {
		if seenIfaces[current] {
			return
		}
		seenIfaces[current] = true

		for spec := range xgoutil.ASTSpecs(astPkg, token.TYPE) {
			typeSpec := spec.(*ast.TypeSpec)
			typeName := typeInfo.ObjectOf(typeSpec.Name)
			if typeName == nil {
				continue
			}
			embedIface, ok := typeName.Type().Underlying().(*gotypes.Interface)
			if !ok {
				continue
			}

			for typ := range embedIface.EmbeddedTypes() {
				if gotypes.Identical(typ, current) {
					selection, ok := gotypes.LookupSelection(embedIface, false, typeName.Pkg(), methodName)
					if ok {
						method, ok := selection.Obj().(*gotypes.Func)
						if ok {
							locations = append(locations, s.findReferenceLocations(result, method)...)
						}
					}
					find(embedIface)
				}
			}
		}
	}
	find(iface)
	return locations
}

// findImplementingMethodReferences finds references to all methods that
// implement the given interface method.
func (s *Server) findImplementingMethodReferences(result *compileResult, iface *gotypes.Interface, methodName string) []Location {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	var locations []Location
	astPkg, _ := result.proj.ASTPackage()
	for spec := range xgoutil.ASTSpecs(astPkg, token.TYPE) {
		typeSpec := spec.(*ast.TypeSpec)
		typeName := typeInfo.ObjectOf(typeSpec.Name)
		if typeName == nil {
			continue
		}
		named, ok := typeName.Type().(*gotypes.Named)
		if !ok || !gotypes.Implements(named, iface) {
			continue
		}

		selection, ok := gotypes.LookupSelection(named, false, named.Obj().Pkg(), methodName)
		if !ok {
			continue
		}
		method, ok := selection.Obj().(*gotypes.Func)
		if !ok {
			continue
		}
		locations = append(locations, s.findReferenceLocations(result, method)...)
	}
	return locations
}

// findInterfaceMethodReferences finds references to interface methods that this
// method implements, including methods from embedded interfaces.
func (s *Server) findInterfaceMethodReferences(result *compileResult, fn *gotypes.Func) []Location {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	var locations []Location
	recvType := fn.Signature().Recv().Type()
	seenIfaces := make(map[*gotypes.Interface]bool)
	astPkg, _ := result.proj.ASTPackage()

	for spec := range xgoutil.ASTSpecs(astPkg, token.TYPE) {
		typeSpec := spec.(*ast.TypeSpec)
		typeName := typeInfo.ObjectOf(typeSpec.Name)
		if typeName == nil {
			continue
		}
		ifaceType, ok := typeName.Type().Underlying().(*gotypes.Interface)
		if !ok || !gotypes.Implements(recvType, ifaceType) || seenIfaces[ifaceType] {
			continue
		}
		seenIfaces[ifaceType] = true

		selection, ok := gotypes.LookupSelection(ifaceType, false, typeName.Pkg(), fn.Name())
		if !ok {
			continue
		}
		method, ok := selection.Obj().(*gotypes.Func)
		if !ok {
			continue
		}
		locations = append(locations, s.findReferenceLocations(result, method)...)
		locations = append(locations, s.findEmbeddedInterfaceReferences(result, ifaceType, fn.Name())...)
	}
	return locations
}

// handleEmbeddedFieldReferences finds all references through embedded fields.
func (s *Server) handleEmbeddedFieldReferences(result *compileResult, obj gotypes.Object) []Location {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	var locations []Location
	if fn, ok := obj.(*gotypes.Func); ok {
		recv := fn.Signature().Recv()
		if recv == nil {
			return nil
		}

		seenTypes := make(map[gotypes.Type]bool)
		astPkg, _ := result.proj.ASTPackage()
		for spec := range xgoutil.ASTSpecs(astPkg, token.TYPE) {
			typeSpec := spec.(*ast.TypeSpec)
			typeName := typeInfo.ObjectOf(typeSpec.Name)
			if typeName == nil {
				continue
			}
			named, ok := typeName.Type().(*gotypes.Named)
			if !ok {
				continue
			}

			locations = append(locations, s.findEmbeddedMethodReferences(result, fn, named, recv.Type(), seenTypes)...)
		}
	}
	return locations
}

// findEmbeddedMethodReferences recursively finds all references to a method
// through embedded fields.
func (s *Server) findEmbeddedMethodReferences(result *compileResult, fn *gotypes.Func, named *gotypes.Named, targetType gotypes.Type, seenTypes map[gotypes.Type]bool) []Location {
	if seenTypes[named] {
		return nil
	}
	seenTypes[named] = true

	st, ok := named.Underlying().(*gotypes.Struct)
	if !ok {
		return nil
	}

	var locations []Location
	hasEmbed := false
	for field := range st.Fields() {
		if !field.Embedded() {
			continue
		}

		if gotypes.Identical(field.Type(), targetType) {
			hasEmbed = true

			selection, ok := gotypes.LookupSelection(named, false, fn.Pkg(), fn.Name())
			if !ok {
				continue
			}
			method, ok := selection.Obj().(*gotypes.Func)
			if !ok {
				continue
			}
			locations = append(locations, s.findReferenceLocations(result, method)...)
		}

		if fieldNamed, ok := field.Type().(*gotypes.Named); ok {
			locations = append(locations, s.findEmbeddedMethodReferences(result, fn, fieldNamed, targetType, seenTypes)...)
		}
	}
	if hasEmbed {
		typeInfo, _ := result.proj.TypeInfo()
		if typeInfo == nil {
			return nil
		}
		astPkg, _ := result.proj.ASTPackage()
		for spec := range xgoutil.ASTSpecs(astPkg, token.TYPE) {
			typeSpec := spec.(*ast.TypeSpec)
			typeName := typeInfo.ObjectOf(typeSpec.Name)
			if typeName == nil {
				continue
			}
			named, ok := typeName.Type().(*gotypes.Named)
			if !ok {
				continue
			}

			locations = append(locations, s.findEmbeddedMethodReferences(result, fn, named, named, seenTypes)...)
		}
	}
	return locations
}
