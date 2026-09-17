//go:build !test_no_pkgdata

package server

import (
	_ "embed"
	"testing"

	"github.com/goplus/xgolsw/internal/config"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:generate sh -c "GOTOOLCHAIN=\"go$(go list -m -f '{{.GoVersion}}')\" go tool pkgdatagen -no-defaults -o testdata/spx-pkgdata.zip github.com/goplus/spx/v3 github.com/goplus/spx/v3/pkg/spx/pkg/engine"

//go:embed testdata/spx-pkgdata.zip
var spxIntegrationPkgDataZip []byte

func newSpxIntegrationTestServer(t testing.TB, files map[string][]byte) *Server {
	t.Helper()

	data, err := pkgdata.NewWithEmbedded(spxIntegrationPkgDataZip)
	require.NoError(t, err)
	proj, data, err := config.NewProject(newFileMap(files), config.Options{
		ClassfileConfig: "project main.spx Game github.com/goplus/spx/v3 math\nclass -embed *.spx SpriteImpl\n",
		PkgData:         data,
	})
	require.NoError(t, err)
	return New(proj, nil, fileMapGetter(files), &MockScheduler{}, data.ListPkgs, data.GetPkgDoc)
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
