package classfile

import (
	"errors"
	"fmt"
	"sync"

	"github.com/goplus/xgolsw/internal/analysis"
	"github.com/goplus/xgolsw/xgo"
)

var (
	errNilProject         = errors.New("nil project")
	errProviderNotFound   = errors.New("provider not found")
	errNoMatchingProvider = errors.New("no provider supports the specified path")
)

// Project wraps an [xgo.Project] with classfile-aware helpers.
type Project struct {
	base          *xgo.Project
	mu            sync.RWMutex
	analyzers     []*analysis.Analyzer
	cacheBuilders map[ProviderID]struct{}
}

// NewProject creates a new [Project].
func NewProject(base *xgo.Project) (*Project, error) {
	if base == nil {
		return nil, errNilProject
	}
	return &Project{
		base: base,
	}, nil
}

// Underlying returns the wrapped [xgo.Project].
func (p *Project) Underlying() *xgo.Project {
	if p == nil {
		return nil
	}
	return p.base
}

// SetAnalyzers records analyzer metadata for providers.
func (p *Project) SetAnalyzers(list []*analysis.Analyzer) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.analyzers = append([]*analysis.Analyzer(nil), list...)
}

// ensureCacheBuilder registers the provider's cache builder once per project.
func (p *Project) ensureCacheBuilder(provider Provider) {
	p.mu.Lock()
	if p.cacheBuilders == nil {
		p.cacheBuilders = make(map[ProviderID]struct{})
	}
	if _, ok := p.cacheBuilders[provider.ID()]; ok {
		p.mu.Unlock()
		return
	}
	builder := p.cacheBuilderFor(provider)
	p.base.RegisterCacheBuilder(snapshotCacheKind{provider: provider.ID()}, builder)
	p.cacheBuilders[provider.ID()] = struct{}{}
	p.mu.Unlock()
}

// cacheBuilderFor returns the cache builder used to construct provider snapshots.
func (p *Project) cacheBuilderFor(provider Provider) func(*xgo.Project) (any, error) {
	return func(proj *xgo.Project) (any, error) {
		buildCtx := p.buildContextFor(proj)
		snapshot, err := provider.Build(buildCtx)
		if err != nil {
			return nil, err
		}
		if snapshot == nil {
			return nil, fmt.Errorf("provider %s returned nil snapshot", provider.ID())
		}
		snapshot.Provider = provider.ID()
		return snapshot, nil
	}
}

// buildContextFor creates a build context using the latest project state and metadata.
func (p *Project) buildContextFor(proj *xgo.Project) *BuildContext {
	p.mu.RLock()
	analyzers := append([]*analysis.Analyzer(nil), p.analyzers...)
	p.mu.RUnlock()

	return &BuildContext{
		Project:   proj,
		Analyzers: analyzers,
	}
}

// Snapshot returns the snapshot for the provider ID, building it if needed.
func (p *Project) Snapshot(id ProviderID) (*Snapshot, error) {
	if p == nil {
		return nil, errNilProject
	}
	provider, ok := ProviderByID(id)
	if !ok {
		return nil, errProviderNotFound
	}

	p.ensureCacheBuilder(provider)
	cache, err := p.base.Cache(snapshotCacheKind{provider: id})
	if err != nil {
		return nil, err
	}
	snapshot, ok := cache.(*Snapshot)
	if !ok || snapshot == nil {
		return nil, fmt.Errorf("invalid snapshot for %s", id)
	}
	return snapshot, nil
}

// SnapshotForPath resolves the provider by path and returns its snapshot.
func (p *Project) SnapshotForPath(path string) (*Snapshot, ProviderID, error) {
	if p == nil {
		return nil, "", errNilProject
	}
	provider, ok := ProviderForPath(path)
	if !ok {
		return nil, "", errNoMatchingProvider
	}
	snapshot, err := p.Snapshot(provider.ID())
	return snapshot, provider.ID(), err
}

// BuildContext carries shared dependencies from the project into a provider build.
type BuildContext struct {
	Project   *xgo.Project
	Analyzers []*analysis.Analyzer
}
