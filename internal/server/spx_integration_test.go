//go:build !test_no_pkgdata

package server

import (
	"testing"

	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
)

func newSpxIntegrationTestServer(t testing.TB, files map[string][]byte) *Server {
	t.Helper()

	proj := xgo.NewProject(nil, newFileMap(files), xgo.FeatAll)
	return New(proj, nil, fileMapGetter(files), &MockScheduler{})
}

func TestSpxIntegrationSymbols(t *testing.T) {
	s := newSpxIntegrationTestServer(t, nil)
	ctx := &definitionContext{proj: s.getProj()}
	for _, name := range []string{
		"Sprite", "SpriteImpl", "BackdropName", "SpriteName", "SpriteCostumeName",
		"SpriteAnimationName", "SoundName", "WidgetName", "Direction", "layerAction",
		"dirAction", "EffectKind", "Key", "Edge", "RotationStyle", "PropertyName", "Value", "List",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, name, ctx.spxTypeName(spxTestType(t, s, name)))
		})
	}
}
