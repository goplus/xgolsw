package server

import (
	"fmt"
	gotypes "go/types"
	"maps"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
)

// resourceProvider supplies resource rules and data without retaining a project.
// Providers are ordered by precedence when resolving types and resource URIs.
type resourceProvider struct {
	analysis           *resourceAnalysis
	matchesType        func(gotypes.Type) bool
	resolve            func(*xgo.Project, resourceValue) (resourceID, bool)
	inspect            func(*xgo.Project, resourceRef)
	parseURI           func(XGoResourceURI) (resourceID, bool, error)
	validateRename     func(resourceID, string) error
	collectCompletions func(*completionContext)
}

// resourceExpression retains a value's resolved context even when its name is
// dynamic, empty, or invalid and cannot be exposed as a resource reference.
type resourceExpression struct {
	value resourceValue
	id    resourceID
}

// collectResourceReferences resolves every reference once across all providers.
// Recognized contexts stop intrinsic conversions from selecting another provider.
func collectResourceReferences(proj *xgo.Project, providers []*resourceProvider) {
	info, _ := expressionTypeInfo(proj)
	var selected *resourceProvider
	for ref := range resourceReferences(proj, func(value resourceValue) (resourceID, bool) {
		for _, provider := range providers {
			id, recognized := provider.resolve(proj, value)
			if !recognized {
				continue
			}
			selected = provider
			if provider.analysis.expressions == nil {
				provider.analysis.expressions = make(map[ast.Expr]resourceExpression)
			}
			for expr, complete := range resourceExpressionParts(value.Expr, info) {
				resolved := resourceExpression{}
				if complete {
					resolved = resourceExpression{value: value, id: id}
				}
				provider.analysis.expressions[expr] = resolved
			}
			return id, true
		}
		return nil, false
	}) {
		selected.inspect(proj, ref)
	}
}

// combineResourceAnalysis combines provider data and resolved expression contexts.
func combineResourceAnalysis(providers []*resourceProvider) *resourceAnalysis {
	if len(providers) == 1 {
		return providers[0].analysis
	}
	result := &resourceAnalysis{expressions: make(map[ast.Expr]resourceExpression)}
	for _, provider := range providers {
		maps.Copy(result.expressions, provider.analysis.expressions)
		result.diagnostics = append(result.diagnostics, provider.analysis.diagnostics...)
		result.resourceRefs = append(result.resourceRefs, provider.analysis.resourceRefs...)
	}
	result.contains = func(id resourceID) bool {
		for _, provider := range providers {
			if contains := provider.analysis.contains; contains != nil && contains(id) {
				return true
			}
		}
		return false
	}
	return result
}

// resourceProviderForURI selects the owner of a resource URI.
func resourceProviderForURI(providers []*resourceProvider, uri XGoResourceURI) (*resourceProvider, resourceID, error) {
	for _, provider := range providers {
		id, recognized, err := provider.parseURI(uri)
		if err != nil {
			return nil, nil, err
		}
		if recognized {
			return provider, id, nil
		}
	}
	return nil, nil, fmt.Errorf("unknown resource collection for %q", uri)
}

// prepareResourceRenames validates names and conflicts for the complete batch.
// All providers feed one edit plan so shared constants are handled together.
func prepareResourceRenames(providers []*resourceProvider, params []XGoRenameResourceParams) (map[resourceID]string, error) {
	renames := make(map[resourceID]string)
	type target struct {
		context XGoResourceContextURI
		name    string
	}
	targets := make(map[target]bool)
	for _, param := range params {
		provider, id, err := resourceProviderForURI(providers, param.Resource.URI)
		if err != nil {
			return nil, err
		}
		if !validResourceName(param.NewName) {
			return nil, fmt.Errorf("invalid resource name %q", param.NewName)
		}
		if previous, exists := renames[id]; exists {
			if previous != param.NewName {
				return nil, fmt.Errorf("conflicting renames for resource %q", id.URI())
			}
			continue
		}
		if err := provider.validateRename(id, param.NewName); err != nil {
			return nil, fmt.Errorf("failed to rename resource %q: %w", id.URI(), err)
		}
		destination := target{id.ContextURI(), param.NewName}
		if targets[destination] {
			return nil, fmt.Errorf("conflicting rename target %q in %q", param.NewName, id.ContextURI())
		}
		targets[destination] = true
		renames[id] = param.NewName
	}
	for id, name := range renames {
		if id.Name() == name {
			delete(renames, id)
		}
	}
	return renames, nil
}
