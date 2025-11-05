package classfile

import (
	"sync"

	"github.com/goplus/xgo/x/typesutil"
)

// Snapshot is the immutable view a provider builds for a project state.
type Snapshot struct {
	Provider    ProviderID
	Diagnostics []typesutil.Error
	Resources   *ResourceIndex
	Symbols     *SymbolIndex
}

// snapshotCacheKind identifies provider snapshot caches inside [xgo.Project].
type snapshotCacheKind struct {
	provider ProviderID
}

// ResourceIndex exposes classfile resource lookup helpers.
type ResourceIndex struct {
	mu    sync.RWMutex
	items map[string]any
}

// Get returns the resource associated with the key.
func (idx *ResourceIndex) Get(key string) (any, bool) {
	if idx == nil {
		return nil, false
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	v, ok := idx.items[key]
	return v, ok
}

// Set stores the resource under the key.
func (idx *ResourceIndex) Set(key string, value any) {
	if idx == nil {
		return
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.items == nil {
		idx.items = make(map[string]any)
	}
	idx.items[key] = value
}

// SymbolIndex provides lookup helpers for definitions and references.
type SymbolIndex struct {
	mu      sync.RWMutex
	entries map[any]any
}

// Get returns the value associated with the key.
func (idx *SymbolIndex) Get(key any) (any, bool) {
	if idx == nil {
		return nil, false
	}
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	v, ok := idx.entries[key]
	return v, ok
}

// Set stores a value under the key.
func (idx *SymbolIndex) Set(key, value any) {
	if idx == nil {
		return
	}
	idx.mu.Lock()
	defer idx.mu.Unlock()
	if idx.entries == nil {
		idx.entries = make(map[any]any)
	}
	idx.entries[key] = value
}
