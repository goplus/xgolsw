package xgo

import (
	"testing"

	"github.com/goplus/mod/xgomod"
	"github.com/goplus/xgolsw/internal/testframework"
)

type testProjectFactory func(*testing.T, map[string]*File, uint) *Project

func newTestProject(t *testing.T, files map[string]*File, feats uint) *Project {
	t.Helper()

	proj := NewProject(nil, files, feats)
	proj.Mod = testframework.NewBaseModule(t)
	proj.Importer = testframework.NewBaseImporter(t, proj.Fset)
	return proj
}

func newFrameworkTestProject(t *testing.T, files map[string]*File, feats uint) *Project {
	t.Helper()

	return newFrameworkTestProjectWithModule(t, files, feats, testframework.NewModule(t))
}

func newFrameworkTestProjectWithModule(t *testing.T, files map[string]*File, feats uint, mod *xgomod.Module) *Project {
	t.Helper()

	proj := NewProject(nil, files, feats)
	proj.Mod = mod
	proj.Importer = testframework.NewImporter(t, proj.Fset)
	return proj
}
