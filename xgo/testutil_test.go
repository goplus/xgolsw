package xgo

import (
	"testing"

	"github.com/goplus/xgolsw/internal/testframework"
)

func newTestProject(t *testing.T, files map[string]*File, feats uint) *Project {
	t.Helper()

	proj := NewProject(nil, files, feats)
	proj.Mod = testframework.NewModule(t)
	proj.Importer = testframework.NewImporter(t, proj.Fset)
	return proj
}
