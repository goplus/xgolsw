package server

import (
	"github.com/goplus/xgolsw/xgo"
)

// resolveFrameworkAdapter selects symbol semantics from classfile registrations.
// The SDK package must resolve through the project's own importer.
func resolveFrameworkAdapter(proj *xgo.Project) frameworkAdapter {
	symbols := newSpxSymbols(proj)
	if symbols.pkg == nil {
		return nil
	}
	return symbols
}

// analyzeFramework selects optional framework analysis for the project snapshot.
func (s *Server) analyzeFramework(proj *xgo.Project) (*frameworkAnalysis, error) {
	result, err := s.analyzeSpx(proj)
	if err != nil || result == nil {
		return nil, err
	}
	return &frameworkAnalysis{
		adapter:            result.spxSymbols,
		resources:          result.resourceAnalysis,
		diagnosticResult:   result.diagnosticResult,
		configurePass:      spxDiagnosticPass(result),
		collectCompletions: result.collectCompletions,
		inputType:          result.inferInputType,
		adaptInputSlot:     result.adaptInputSlot,
		renameResources: func(params []XGoRenameResourceParams) (*WorkspaceEdit, error) {
			return s.renameSpxResources(result, params)
		},
	}, nil
}
