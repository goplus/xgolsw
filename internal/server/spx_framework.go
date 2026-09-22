package server

import (
	gotypes "go/types"
	"strings"

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

// spxFrameworkAnalysis binds the SDK's resource and symbol operations.
func spxFrameworkAnalysis(result *spxAnalysis) *frameworkAnalysis {
	return &frameworkAnalysis{
		adapter: result.spxSymbols,
		configurePass: func(proj *xgo.Project) func(string, *protocol.Pass) {
			return spxDiagnosticPass(proj, result)
		},
		inputType:      result.inferInputType,
		adaptInputSlot: result.adaptInputSlot,
		appendResourceRenames: func(s *Server, proj *xgo.Project, renames map[resourceID]string, changes map[DocumentURI][]TextEdit) {
			s.appendSpxSpriteTypeRenames(proj, result, renames, changes)
		},
	}
}

// resourceProvider exposes the SDK's resource rules to common analysis and rename.
func (result *spxAnalysis) resourceProvider() *resourceProvider {
	return &resourceProvider{
		analysis:    result.resourceAnalysis,
		matchesType: func(typ gotypes.Type) bool { return result.spxResourceNameType(typ) != "" },
		resolve:     result.resolveResourceValue,
		inspect:     func(proj *xgo.Project, ref resourceRef) { inspectSpxResourceRef(proj, result, ref) },
		parseURI: func(uri XGoResourceURI) (resourceID, bool, error) {
			if !strings.HasPrefix(string(uri), "spx://resources/") {
				return nil, false, nil
			}
			id, err := ParseSpxResourceURI(uri)
			return id, true, err
		},
		validateRename:     result.validateResourceRename,
		collectCompletions: result.collectCompletions,
	}
}

// loadFrameworkAnalysis selects the registered adapter and its resource rules.
func loadFrameworkAnalysis(proj *xgo.Project) (*frameworkAnalysis, []*resourceProvider, error) {
	result, err := loadSpxAnalysis(proj)
	if err != nil || result == nil {
		return nil, nil, err
	}
	return spxFrameworkAnalysis(result), []*resourceProvider{result.resourceProvider()}, nil
}
