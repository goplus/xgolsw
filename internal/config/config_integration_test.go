//go:build !test_no_pkgdata

package config

import (
	"testing"

	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProjectEmbeddedPkgData(t *testing.T) {
	project, data, err := NewProject(map[string]*xgo.File{
		"main.xgo": {Content: []byte("import \"strings\"\nvar Text = strings.ToUpper(\"hello\")\n")},
	}, Options{})
	require.NoError(t, err)
	_, err = project.TypeInfo()
	require.NoError(t, err)
	doc, err := data.GetPkgDoc("strings")
	require.NoError(t, err)
	assert.Contains(t, doc.Funcs, "ToUpper")
	packages, err := data.ListPkgs()
	require.NoError(t, err)
	assert.Contains(t, packages, "fmt")
	assert.Contains(t, packages, "github.com/qiniu/x/xgo")
	assert.NotContains(t, packages, "github.com/goplus/spx/v3")
	assert.NotContains(t, packages, "github.com/goplus/spx/v3/pkg/spx/pkg/engine")
}
