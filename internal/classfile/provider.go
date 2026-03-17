package classfile

import (
	"path/filepath"
	"sync"
)

// ProviderID uniquely identifies a registered provider.
type ProviderID string

// Provider implements classfile-specific logic on top of an XGo project.
type Provider interface {
	ID() ProviderID
	Supports(path string) bool
	Build(buildCtx *BuildContext) (*Snapshot, error)
}

var (
	providerMu sync.RWMutex
	providers  = make(map[ProviderID]Provider)
	order      []Provider
)

// RegisterProvider registers a provider. It panics if the ID already exists.
func RegisterProvider(p Provider) {
	if p == nil {
		panic("cannot register nil provider")
	}
	id := p.ID()
	if id == "" {
		panic("provider id cannot be empty")
	}

	providerMu.Lock()
	defer providerMu.Unlock()
	if _, exists := providers[id]; exists {
		panic("duplicate provider id " + string(id))
	}
	providers[id] = p
	order = append(order, p)
}

// ProviderByID returns the provider with the given ID.
func ProviderByID(id ProviderID) (Provider, bool) {
	providerMu.RLock()
	defer providerMu.RUnlock()
	p, ok := providers[id]
	return p, ok
}

// ProviderForPath returns the first provider that supports the provided path.
func ProviderForPath(path string) (Provider, bool) {
	cleanPath := filepath.ToSlash(path)

	providerMu.RLock()
	defer providerMu.RUnlock()
	for _, p := range order {
		if p.Supports(cleanPath) {
			return p, true
		}
	}
	return nil, false
}

// RegisteredProviders returns all providers in registration order.
func RegisteredProviders() []Provider {
	providerMu.RLock()
	defer providerMu.RUnlock()
	result := make([]Provider, len(order))
	copy(result, order)
	return result
}
