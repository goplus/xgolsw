package classfile

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResourceIndex(t *testing.T) {
	var idx ResourceIndex
	_, ok := idx.Get("missing")
	assert.False(t, ok)

	idx.Set("key", 42)
	value, ok := idx.Get("key")
	assert.True(t, ok)
	assert.Equal(t, 42, value)

	var nilIdx *ResourceIndex
	_, ok = nilIdx.Get("key")
	assert.False(t, ok)
	assert.NotPanics(t, func() { nilIdx.Set("key", 1) })
}

func TestSymbolIndex(t *testing.T) {
	var idx SymbolIndex
	_, ok := idx.Get("missing")
	assert.False(t, ok)

	idx.Set("key", "value")
	value, ok := idx.Get("key")
	assert.True(t, ok)
	assert.Equal(t, "value", value)

	var nilIdx *SymbolIndex
	_, ok = nilIdx.Get("key")
	assert.False(t, ok)
	assert.NotPanics(t, func() { nilIdx.Set("key", "value") })
}
