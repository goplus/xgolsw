package server

import (
	"fmt"
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_references
func (s *Server) textDocumentReferences(params *ReferenceParams) ([]Location, error) {
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
	ident, obj, _ := sourceObjectAtPosition(proj, typeInfo, astFile, position)
	if obj == nil {
		return nil, nil
	}

	var locations []Location

	locations = append(locations, s.findReferenceLocations(proj, obj)...)
	locations = append(locations, s.kwargReferenceLocations(proj, obj)...)

	if fn, ok := obj.(*gotypes.Func); ok && fn.Signature().Recv() != nil {
		locations = append(locations, s.findRelatedMethodReferences(proj, fn)...)
	}

	if params.Context.IncludeDeclaration && !xgoutil.IsSyntheticThisIdent(proj.Fset, typeInfo, astPkg, ident) {
		if loc := s.objectDefinitionLocation(proj, typeInfo, obj); loc != nil {
			locations = append(locations, *loc)
		}
	}

	return DedupeLocations(locations), nil
}

// findReferenceLocations returns all locations where the given object is referenced.
func (s *Server) findReferenceLocations(proj *xgo.Project, obj gotypes.Object) []Location {
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	refIdents := typeInfo.RefIdentsFor(obj)
	if len(refIdents) == 0 {
		return nil
	}
	astPkg, _ := proj.ASTPackage()
	refs := make(map[*ast.Ident]bool, len(refIdents))
	for _, ident := range refIdents {
		refs[ident] = true
	}
	locations := make([]Location, 0, len(refIdents))
	for _, astFile := range astPkg.Files {
		file := xgoutil.NodeTokenFile(proj.Fset, astFile)
		if file == nil {
			continue
		}
		// Recorder-only identifiers, such as implicit kwarg factories, are not
		// source references even when their names match the text at that position.
		ast.Inspect(astFile, func(node ast.Node) bool {
			ident, ok := node.(*ast.Ident)
			if !ok || !refs[ident] {
				return true
			}
			delete(refs, ident)
			if xgoutil.IsSourceIdent(file, astFile.Code, ident) {
				locations = append(locations, s.locationForNode(proj, ident))
			}
			return false
		})
	}
	return locations
}

// findRelatedMethodReferences finds uses of other methods in the same interface
// implementation relationships, including keyword argument references.
func (s *Server) findRelatedMethodReferences(proj *xgo.Project, target *gotypes.Func) []Location {
	info, _ := proj.TypeInfo()
	if info == nil {
		return nil
	}
	var locations []Location
	for _, method := range relatedMethodDeclarations(info, target) {
		if method == target.Origin() {
			continue
		}
		locations = append(locations, s.findReferenceLocations(proj, method)...)
		locations = append(locations, s.kwargReferenceLocations(proj, method)...)
	}
	return locations
}
