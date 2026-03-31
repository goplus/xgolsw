package classfile

import (
	"maps"
	"sync"
	"testing"
)

func withProviderRegistry(t *testing.T, fn func()) {
	t.Helper()

	providerMu.Lock()
	savedProviders := maps.Clone(providers)
	savedOrder := append([]Provider(nil), order...)
	providers = make(map[ProviderID]Provider)
	order = nil
	providerMu.Unlock()

	t.Cleanup(func() {
		providerMu.Lock()
		providers = savedProviders
		order = savedOrder
		providerMu.Unlock()
	})

	fn()
}

type recordingProvider struct {
	id       ProviderID
	supports func(string) bool
	build    func(*BuildContext) (*Snapshot, error)
	mu       sync.Mutex
	calls    int
}

func (p *recordingProvider) ID() ProviderID { return p.id }

func (p *recordingProvider) Supports(path string) bool {
	if p.supports != nil {
		return p.supports(path)
	}
	return false
}

func (p *recordingProvider) Build(ctx *BuildContext) (*Snapshot, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	if p.build != nil {
		return p.build(ctx)
	}
	return &Snapshot{}, nil
}

func (p *recordingProvider) CallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}
