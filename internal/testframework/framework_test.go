package testframework

import (
	"io/fs"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBaseImporter(t *testing.T) {
	base := NewBaseImporter(t, token.NewFileSet())
	_, err := base.Import(PkgPath)
	assert.ErrorIs(t, err, fs.ErrNotExist)

	framework, err := NewImporter(t, token.NewFileSet()).Import(PkgPath)
	require.NoError(t, err)
	require.NotNil(t, framework.Scope().Lookup("App"))
	_, err = base.Import(PkgPath)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	_, err = NewBaseImporter(t, token.NewFileSet()).Import(PkgPath)
	assert.ErrorIs(t, err, fs.ErrNotExist)

	for _, pkgPath := range []string{"strings", "github.com/qiniu/x/osx"} {
		pkg, err := base.Import(pkgPath)
		require.NoError(t, err)
		assert.Equal(t, pkgPath, pkg.Path())
		assert.True(t, pkg.Complete())
		again, err := base.Import(pkgPath)
		require.NoError(t, err)
		assert.Same(t, pkg, again)
	}
}
