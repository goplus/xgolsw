package server

import (
	gotypes "go/types"
	"path"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	"github.com/goplus/xgolsw/xgo"
)

// diagnosticsForSpx collects diagnostics only when the project contains spx classfiles.
func (s *Server) diagnosticsForSpx(proj *xgo.Project) (*diagnosticResult, error) {
	class, ok := proj.Mod.LookupClass(".spx")
	if !ok || !slices.Contains(class.PkgPaths, SpxPkgPath) {
		return nil, nil
	}
	for filename := range proj.Files() {
		if path.Ext(filename) != ".spx" {
			continue
		}
		result, err := s.compileAt(proj)
		if err != nil {
			return nil, err
		}
		return &result.diagnosticResult, nil
	}
	return nil, nil
}

// spxDiagnosticPass supplies property information to analyzers for an spx project.
func spxDiagnosticPass(result *compileResult) func(string, *protocol.Pass) {
	typeInfo, _ := result.proj.TypeInfo()
	propertyNamesCache := make(map[*gotypes.Named]map[string]struct{})
	return func(filename string, pass *protocol.Pass) {
		pass.IsPropertyNameType = IsSpxPropertyNameType
		pass.GetPropertyNamesForCall = func(call *ast.CallExpr) map[string]struct{} {
			named := PropertyTargetNamedTypeForCall(typeInfo, call, filename, result.mainSpxFile)
			if named == nil {
				return nil
			}
			if names, ok := propertyNamesCache[named]; ok {
				return names
			}
			names := make(map[string]struct{})
			for property := range propertyObjects(named) {
				names[property.Name] = struct{}{}
			}
			propertyNamesCache[named] = names
			return names
		}
	}
}
