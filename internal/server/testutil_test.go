package server

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/require"
)

func newTestServer(t testing.TB, files map[string][]byte) *Server {
	t.Helper()

	proj := xgo.NewProject(nil, newFileMap(files), xgo.FeatAll)
	s := New(proj, nil, fileMapGetter(files), &MockScheduler{})
	proj.Mod = testframework.NewModule(t)
	proj.Importer = testframework.NewImporter(t, proj.Fset)
	s.listPkgs = func() ([]string, error) { return []string{testframework.PkgPath}, nil }
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

func newFileMap(files map[string][]byte) map[string]*xgo.File {
	fileMap := make(map[string]*xgo.File)
	for k, v := range files {
		fileMap[k] = &xgo.File{Content: v}
	}
	return fileMap
}

func requireValueAs[T any](t *testing.T, value any) T {
	t.Helper()

	typed, ok := value.(T)
	require.True(t, ok)
	return typed
}

func fileMapGetter(files map[string][]byte) func() map[string]*xgo.File {
	return func() map[string]*xgo.File {
		return newFileMap(files)
	}
}
