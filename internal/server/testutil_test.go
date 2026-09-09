package server

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/require"
)

func newTestServer(t *testing.T, files map[string][]byte) *Server {
	t.Helper()

	proj := newProjectWithoutModTime(files)
	s := New(proj, nil, fileMapGetter(files), &MockScheduler{})
	proj.Mod = testframework.NewModule(t)
	proj.Importer = testframework.NewImporter(t, proj.Fset)
	frameworkDoc := testframework.NewPkgDoc(t)
	s.lookupPkgDoc = func(pkgPath string) (*pkgdoc.PkgDoc, error) {
		t.Helper()

		require.False(t, pkgPath == "github.com/goplus/spx" || strings.HasPrefix(pkgPath, "github.com/goplus/spx/"),
			"unexpected spx documentation lookup: %s", pkgPath)
		if pkgPath == testframework.PkgPath {
			return frameworkDoc, nil
		}
		return nil, fs.ErrNotExist
	}
	return s
}
