package server

import (
	"testing"

	"github.com/goplus/xgolsw/internal/testframework"
)

func newTestServer(t *testing.T, files map[string][]byte) *Server {
	t.Helper()

	proj := newProjectWithoutModTime(files)
	s := New(proj, nil, fileMapGetter(files), &MockScheduler{})
	proj.Mod = testframework.NewModule(t)
	proj.Importer = testframework.NewImporter(t, proj.Fset)
	return s
}
