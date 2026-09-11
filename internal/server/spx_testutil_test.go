//go:build !test_no_pkgdata

package server

import (
	"testing"

	"github.com/goplus/xgolsw/xgo"
)

func newSpxTestServer(t testing.TB, files map[string][]byte) *Server {
	t.Helper()

	proj := xgo.NewProject(nil, newFileMap(files), xgo.FeatAll)
	return New(proj, nil, fileMapGetter(files), &MockScheduler{})
}
