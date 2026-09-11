package server

import (
	"fmt"
	"io/fs"
	"testing"

	"github.com/goplus/mod/xgomod"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testServerFactory func(testing.TB, map[string][]byte) *Server

func newTestServer(t testing.TB, files map[string][]byte) *Server {
	t.Helper()

	proj := xgo.NewProject(nil, newFileMap(files), xgo.FeatAll)
	proj.PkgPath = "main"
	proj.Mod = testframework.NewBaseModule(t)
	proj.Importer = testframework.NewBaseImporter(t, proj.Fset)
	return newServer(proj, nil, fileMapGetter(files), &MockScheduler{},
		func() ([]string, error) { return nil, nil },
		func(pkgPath string) (*pkgdoc.PkgDoc, error) {
			t.Helper()

			if err := checkNonSpxDocumentation(t, pkgPath); err != nil {
				return nil, err
			}
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

		if err := checkNonSpxDocumentation(t, pkgPath); err != nil {
			return nil, err
		}
		if pkgPath == testframework.PkgPath {
			return frameworkDoc, nil
		}
		return nil, fs.ErrNotExist
	}
	return newServer(proj, nil, fileMapGetter(files), &MockScheduler{}, listPkgs, lookupPkgDoc)
}

func checkNonSpxDocumentation(t testing.TB, pkgPath string) error {
	t.Helper()

	if testframework.IsSpxPath(pkgPath) {
		err := fmt.Errorf("unexpected spx documentation lookup: %s", pkgPath)
		assert.Fail(t, err.Error())
		return err
	}
	return nil
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
