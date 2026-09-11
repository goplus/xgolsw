package server

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/goplus/mod/xgomod"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/require"
)

type testServerFactory func(testing.TB, map[string][]byte) *Server

func newTestServer(t testing.TB, files map[string][]byte) *Server {
	t.Helper()

	proj := xgo.NewProject(nil, newFileMap(files), xgo.FeatAll)
	proj.PkgPath = "main"
	proj.Mod = xgomod.New(modload.Module{Opt: &modfile.File{}})
	require.NoError(t, proj.Mod.ImportClasses())
	proj.Importer = testframework.NewBaseImporter(t, proj.Fset)
	return newServer(proj, nil, fileMapGetter(files), &MockScheduler{},
		func() ([]string, error) { return nil, nil },
		func(pkgPath string) (*pkgdoc.PkgDoc, error) {
			t.Helper()

			requireNonSpxDocumentation(t, pkgPath)
			return nil, fs.ErrNotExist
		},
	)
}

func newFrameworkTestServer(t testing.TB, files map[string][]byte) *Server {
	t.Helper()

	return newFrameworkTestServerWithModule(t, files, testframework.NewModule(t))
}

func newFrameworkTestServerWithSpxExtension(t testing.TB, files map[string][]byte) *Server {
	t.Helper()

	mod := testframework.NewModule(t)
	class := mod.Opt.Projects[0]
	class.Ext = ".spx"
	class.FullExt = "main.spx"
	class.Works[0].Ext = ".spx"
	require.NoError(t, mod.ImportClasses())
	return newFrameworkTestServerWithModule(t, files, mod)
}

func newFrameworkTestServerWithModule(t testing.TB, files map[string][]byte, mod *xgomod.Module) *Server {
	t.Helper()

	proj := xgo.NewProject(nil, newFileMap(files), xgo.FeatAll)
	proj.PkgPath = "main"
	proj.Mod = mod
	proj.Importer = testframework.NewImporter(t, proj.Fset)
	listPkgs := func() ([]string, error) { return []string{testframework.PkgPath}, nil }
	frameworkDoc := testframework.NewPkgDoc(t)
	lookupPkgDoc := func(pkgPath string) (*pkgdoc.PkgDoc, error) {
		t.Helper()

		requireNonSpxDocumentation(t, pkgPath)
		if pkgPath == testframework.PkgPath {
			return frameworkDoc, nil
		}
		return nil, fs.ErrNotExist
	}
	return newServer(proj, nil, fileMapGetter(files), &MockScheduler{}, listPkgs, lookupPkgDoc)
}

func requireNonSpxDocumentation(t testing.TB, pkgPath string) {
	t.Helper()

	require.False(t, pkgPath == "github.com/goplus/spx" || strings.HasPrefix(pkgPath, "github.com/goplus/spx/"),
		"unexpected spx documentation lookup: %s", pkgPath)
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
