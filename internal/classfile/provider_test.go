package classfile

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegisterProvider(t *testing.T) {
	withProviderRegistry(t, func() {
		t.Run("NilProvider", func(t *testing.T) {
			assert.PanicsWithValue(t, "cannot register nil provider", func() {
				RegisterProvider(nil)
			})
		})

		t.Run("DuplicateID", func(t *testing.T) {
			RegisterProvider(&recordingProvider{id: "dup"})
			assert.PanicsWithValue(t, "duplicate provider id dup", func() {
				RegisterProvider(&recordingProvider{id: "dup"})
			})
		})
	})
}

func TestProviderLookup(t *testing.T) {
	withProviderRegistry(t, func() {
		matchAll := &recordingProvider{id: "all", supports: func(string) bool { return true }}
		matchSPX := &recordingProvider{id: "spx", supports: func(path string) bool {
			return filepath.Ext(path) == ".spx"
		}}
		RegisterProvider(matchSPX)
		RegisterProvider(matchAll)

		t.Run("ByID", func(t *testing.T) {
			p, ok := ProviderByID(matchSPX.ID())
			assert.True(t, ok)
			assert.Equal(t, matchSPX, p)
		})

		t.Run("ByPathExactMatch", func(t *testing.T) {
			p, ok := ProviderForPath("test.spx")
			assert.True(t, ok)
			assert.Equal(t, matchSPX, p)
		})

		t.Run("ByPathFallback", func(t *testing.T) {
			p, ok := ProviderForPath("test.txt")
			assert.True(t, ok)
			assert.Equal(t, matchAll, p)
		})
	})
}
