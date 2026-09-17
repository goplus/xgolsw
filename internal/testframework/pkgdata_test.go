package testframework

import (
	"testing"

	"github.com/goplus/xgolsw/internal"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPkgDataZip(t *testing.T) {
	data, err := pkgdata.New(NewPkgDataZip(t))
	require.NoError(t, err)
	packages, err := data.ListPkgs()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{PkgPath, "fmt"}, packages)
	importer := internal.NewImporter(data.OpenExport)
	pkg, err := importer.Import(PkgPath)
	require.NoError(t, err)
	assert.NotNil(t, pkg.Scope().Lookup("App"))
	assert.NotNil(t, pkg.Scope().Lookup("Item"))
	fmtPkg, err := importer.Import("fmt")
	require.NoError(t, err)
	assert.NotNil(t, fmtPkg.Scope().Lookup("Println"))
	doc, err := data.GetPkgDoc(PkgPath)
	require.NoError(t, err)
	assert.Equal(t, NewPkgDoc(t), doc)
}
