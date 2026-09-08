// Package testframework provides an isolated classfile framework for tests.
package testframework

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
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/require"
)

// PkgPath is the import path of the test framework.
const PkgPath = "example.com/framework"

// source defines the framework's base classes and methods.
//
//go:embed testdata/framework.go
var source string

// NewModule returns a fresh module with the test framework registered.
func NewModule(t *testing.T) *xgomod.Module {
	t.Helper()

	mod := xgomod.New(modload.Module{
		Opt: &modfile.File{Projects: []*modfile.Project{{
			Ext:      "_fixture.gox",
			FullExt:  "main_fixture.gox",
			Class:    "App",
			PkgPaths: []string{PkgPath},
			Works:    []*modfile.Class{{Ext: "_fixture.gox", Class: "Item", Embedded: true}},
		}}},
	})
	require.NoError(t, mod.ImportClasses())
	return mod
}

// NewImporter type checks the framework and returns an importer that rejects spx.
func NewImporter(t *testing.T, fset *token.FileSet) gotypes.Importer {
	t.Helper()

	astFile, err := goparser.ParseFile(fset, "testdata/framework.go", source, 0)
	require.NoError(t, err)
	framework, err := new(gotypes.Config).Check(PkgPath, fset, []*goast.File{astFile}, nil)
	require.NoError(t, err)
	return &importer{
		t:         t,
		framework: framework,
		fallback:  packages.NewImporter(fset),
	}
}

// importer supplies the test framework and delegates other imports.
type importer struct {
	t         *testing.T
	framework *gotypes.Package
	fallback  gotypes.Importer
}

// Import implements [go/types.Importer].
func (i *importer) Import(pkgPath string) (*gotypes.Package, error) {
	i.t.Helper()

	require.False(i.t, pkgPath == "github.com/goplus/spx" || strings.HasPrefix(pkgPath, "github.com/goplus/spx/"),
		"unexpected spx import: %s", pkgPath)
	if pkgPath == PkgPath {
		return i.framework, nil
	}
	return i.fallback.Import(pkgPath)
}
