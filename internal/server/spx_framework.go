package server

import (
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	"github.com/goplus/xgolsw/xgo"
)

// buildFrameworkAdapterCache selects symbol semantics from classfile registrations.
// The SDK package must resolve through the project's own importer.
func buildFrameworkAdapterCache(proj *xgo.Project) (any, error) {
	symbols := newSpxSymbols(proj)
	if symbols.pkg == nil {
		return nil, nil
	}
	return symbols, nil
}

// buildFrameworkAnalysisCache builds optional framework data without retaining
// the project or server. Operations bind their context when requested.
func buildFrameworkAnalysisCache(proj *xgo.Project) (any, error) {
	// Populate shared caches before taking a stable view of source and resources.
	proj.TypeInfo()
	resolveFrameworkAdapter(proj)
	result, err := analyzeSpx(proj.Snapshot())
	if err != nil || result == nil {
		return (*frameworkAnalysis)(nil), err
	}
	return &frameworkAnalysis{
		adapter:   result.spxSymbols,
		resources: result.resourceAnalysis,
		configurePass: func(proj *xgo.Project) func(string, *protocol.Pass) {
			return spxDiagnosticPass(proj, result)
		},
		collectCompletions: result.collectCompletions,
		inputType:          result.inferInputType,
		adaptInputSlot:     result.adaptInputSlot,
		renameResources: func(s *Server, proj *xgo.Project, params []XGoRenameResourceParams) (*WorkspaceEdit, error) {
			return s.renameSpxResources(proj, result, params)
		},
	}, nil
}
