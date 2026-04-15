package main

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"path"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/mod/module"
)

func TestBuildPkgResourceSchema(t *testing.T) {
	pkgPath, dir, pkgName, buildFiles, astFiles := loadSpxBuildInputs(t)

	t.Run("SerializedSchemaIncludesInterfaceBindings", func(t *testing.T) {
		schema, err := buildPkgResourceSchema(pkgPath, dir, pkgName, buildFiles, astFiles)
		if err != nil {
			t.Fatal(err)
		}
		if schema == nil {
			t.Fatal("serialized schema is nil")
		}
		for _, binding := range schema.APIScopeBindings {
			if binding.Callable == "(github.com/goplus/spx/v2.Sprite).SetCostume__0" {
				return
			}
		}
		t.Fatal("serialized schema does not include Sprite.SetCostume__0")
	})
}

func loadSpxBuildInputs(t *testing.T) (pkgPath string, dir string, pkgName string, buildFiles []string, astFiles map[string]*ast.File) {
	t.Helper()

	buildCtx := build.Default
	buildCtx.GOOS = "js"
	buildCtx.GOARCH = "wasm"
	buildCtx.CgoEnabled = false

	pkgPath = "github.com/goplus/spx/v2"
	buildPkg, err := buildCtx.Import(pkgPath, "", build.ImportComment)
	if err != nil {
		t.Fatal(err)
	}

	if prefix, _, ok := module.SplitPathVersion(pkgPath); ok {
		pkgName = path.Base(prefix)
	} else {
		pkgName = path.Base(buildPkg.ImportPath)
	}

	buildFiles = slices.Concat(buildPkg.GoFiles, buildPkg.CgoFiles)
	astFiles = make(map[string]*ast.File, len(buildFiles))
	fset := token.NewFileSet()
	for _, fileName := range buildFiles {
		fullPath := filepath.Join(buildPkg.Dir, fileName)
		astFile, err := parser.ParseFile(fset, fullPath, nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		if astFile.Name == nil || astFile.Name.Name != pkgName {
			continue
		}
		astFiles[fullPath] = astFile
	}

	return pkgPath, buildPkg.Dir, pkgName, buildFiles, astFiles
}
