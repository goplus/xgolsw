package server

import (
	gotypes "go/types"

	"github.com/goplus/xgolsw/internal/config"
	"github.com/goplus/xgolsw/xgo"
)

// buildFrameworkAnalysis combines adapter rules and instance configuration.
// References are scanned once and every rename batch uses one edit plan.
func buildFrameworkAnalysis(proj *xgo.Project, resources *config.ResourceConfig) (any, error) {
	proj.TypeInfo()
	resolveFrameworkAdapter(proj)
	proj = proj.Snapshot()
	analysis, providers, err := loadFrameworkAnalysis(proj)
	if err != nil {
		return (*frameworkAnalysis)(nil), err
	}
	if resources != nil {
		if configured := newConfiguredResources(proj, resources, providers); configured != nil {
			if analysis == nil {
				analysis = new(frameworkAnalysis)
			}
			providers = append(providers, configured.resourceProvider())
			baseInputType := analysis.inputType
			if baseInputType == nil {
				baseInputType = inferBasicInputType
			}
			analysis.inputType = func(typ gotypes.Type) XGoInputType {
				if configured.contextForType(typ) != "" {
					return XGoInputTypeResourceName
				}
				return baseInputType(typ)
			}
		}
	}
	if analysis == nil {
		return (*frameworkAnalysis)(nil), nil
	}
	collectResourceReferences(proj, providers)
	analysis.resources = combineResourceAnalysis(providers)
	analysis.collectCompletions = func(ctx *completionContext) {
		for _, provider := range providers {
			if _, recognized := provider.analysis.expressions[ctx.stringLit]; recognized {
				provider.collectCompletions(ctx)
				return
			}
		}
		for _, provider := range providers {
			provider.collectCompletions(ctx)
		}
	}
	analysis.renameResources = func(s *Server, proj *xgo.Project, params []XGoRenameResourceParams) (*WorkspaceEdit, error) {
		renames, err := prepareResourceRenames(providers, params)
		if err != nil {
			return nil, err
		}
		changes, err := s.renameResourcesAtRefs(proj, analysis.resources, renames)
		if err != nil {
			return nil, err
		}
		if analysis.appendResourceRenames != nil {
			analysis.appendResourceRenames(s, proj, renames, changes)
		}
		return &WorkspaceEdit{Changes: changes}, nil
	}
	return analysis, nil
}
