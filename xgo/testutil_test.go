package xgo

import (
	_ "embed"
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/gogen/packages"
	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/goplus/mod/xgomod"
	"github.com/stretchr/testify/require"
)

const testFrameworkPkgPath = "example.com/framework"

//go:embed testdata/framework/framework.go
var testFrameworkSource string

func newTestProject(t *testing.T, files map[string]*File, feats uint) *Project {
	t.Helper()

	proj := NewProject(nil, files, feats)
	proj.Mod = xgomod.New(modload.Module{
		Opt: &modfile.File{Projects: []*modfile.Project{{
			Ext:      "_fixture.gox",
			FullExt:  "main_fixture.gox",
			Class:    "App",
			PkgPaths: []string{testFrameworkPkgPath},
			Works:    []*modfile.Class{{Ext: "_fixture.gox", Class: "Item", Embedded: true}},
		}}},
	})
	require.NoError(t, proj.Mod.ImportClasses())

	astFile, err := goparser.ParseFile(proj.Fset, "testdata/framework/framework.go", testFrameworkSource, 0)
	require.NoError(t, err)
	framework, err := new(gotypes.Config).Check(testFrameworkPkgPath, proj.Fset, []*goast.File{astFile}, nil)
	require.NoError(t, err)
	proj.Importer = &testImporter{
		t:         t,
		framework: framework,
		fallback:  packages.NewImporter(proj.Fset),
	}
	return proj
}

type testImporter struct {
	t         *testing.T
	framework *gotypes.Package
	fallback  gotypes.Importer
}

func (i *testImporter) Import(pkgPath string) (*gotypes.Package, error) {
	i.t.Helper()

	require.False(i.t, pkgPath == "github.com/goplus/spx" || strings.HasPrefix(pkgPath, "github.com/goplus/spx/"),
		"unexpected spx import: %s", pkgPath)
	if pkgPath == testFrameworkPkgPath {
		return i.framework, nil
	}
	return i.fallback.Import(pkgPath)
}
