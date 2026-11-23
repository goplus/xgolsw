package xgo

import (
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"io/fs"
	"os"
	"os/exec"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectImport(t *testing.T) {
	t.Run("DefaultImporter", func(t *testing.T) {
		proj := NewProject(nil, nil, FeatAll)
		pkg, err := proj.Import("unsafe")
		require.NoError(t, err)
		assert.Same(t, gotypes.Unsafe, pkg)
	})

	t.Run("DefaultImporterMissingDependency", func(t *testing.T) {
		if _, err := exec.LookPath("go"); err != nil {
			t.Skip("requires the Go toolchain")
		}
		t.Chdir(t.TempDir())
		t.Setenv("GOWORK", "off")
		t.Setenv("GOPROXY", "off")
		const root = "example.com/importfixture"
		const dependency = root + "/dep"
		require.NoError(t, os.WriteFile("go.mod", []byte("module "+root+"\n\ngo 1.26\n"), 0o600))
		require.NoError(t, os.WriteFile("root.go", []byte(`package fixture
const XGoPackage = "example.com/importfixture/dep"
func Pick__0(value int) int { return value }
func Pick__1(value string) int { return len(value) }
`), 0o600))

		proj := NewProject(nil, map[string]*File{"main.xgo": file(`import fixture "example.com/importfixture"
_ = fixture.Pick(1)
`)}, FeatAll)
		pkg, err := proj.Import(root)
		require.ErrorContains(t, err, "failed to import XGo dependency")
		assert.ErrorContains(t, err, dependency)
		assert.Nil(t, pkg)
		_, err = proj.TypeInfo()
		require.ErrorContains(t, err, "failed to import XGo dependency")
		assert.ErrorContains(t, err, dependency)

		require.NoError(t, os.Mkdir("dep", 0o700))
		require.NoError(t, os.WriteFile("dep/dep.go", []byte("package dep\nconst XGoPackage = true\n"), 0o600))
		pkg, err = proj.Import(root)
		require.NoError(t, err)
		require.NotNil(t, pkg)
		assert.NotNil(t, pkg.Scope().Lookup("Pick"))
		_, err = proj.Fork().TypeInfo()
		require.NoError(t, err)
	})

	t.Run("InitializesOverloads", func(t *testing.T) {
		proj := newFrameworkTestProject(t, map[string]*File{
			"main_fixture.gox": file("measure 1\n"),
		}, FeatAll)
		pkg, err := proj.Import(testframework.PkgPath)
		require.NoError(t, err)
		require.NotNil(t, pkg)
		app := pkg.Scope().Lookup("App")
		require.NotNil(t, app)
		method, _, _ := gotypes.LookupFieldOrMethod(app.Type(), true, pkg, "Measure")
		require.NotNil(t, method)
		for _, project := range []*Project{proj, proj.Snapshot(), proj.Fork()} {
			_, err := project.TypeInfo()
			require.NoError(t, err)
			imported, err := project.Import(testframework.PkgPath)
			require.NoError(t, err)
			assert.Same(t, pkg, imported)
			current, _, _ := gotypes.LookupFieldOrMethod(app.Type(), true, pkg, "Measure")
			assert.Same(t, method, current, "type checking must preserve initialized overloads")
		}
	})

	t.Run("MissingPackage", func(t *testing.T) {
		proj := newTestProject(t, nil, FeatAll)
		pkg, err := proj.Import(testframework.PkgPath)
		assert.ErrorIs(t, err, fs.ErrNotExist)
		assert.Nil(t, pkg)
	})

	t.Run("ImporterPanic", func(t *testing.T) {
		proj := newTestProject(t, nil, FeatAll)
		proj.Importer = pkgDocTestImporter(func(string) (*gotypes.Package, error) { panic("import failed") })
		assert.PanicsWithValue(t, "import failed", func() { proj.Import(testframework.PkgPath) })
	})

	t.Run("MissingDependency", func(t *testing.T) {
		for _, first := range []string{"Import", "TypeChecking"} {
			t.Run(first, func(t *testing.T) {
				const midPath = "example.com/middle"
				const depPath = "example.com/dependency"
				proj := newTestProject(t, map[string]*File{
					"main.xgo": file(`import framework "example.com/framework"
echo framework.Value
`),
				}, FeatAll)
				framework := newImportTestPackage(t, testframework.PkgPath, fmt.Sprintf("XGoPackage = %q", midPath))
				middle := newImportTestPackage(t, midPath, fmt.Sprintf("GopPackage = %q", depPath))
				dependency := newImportTestPackage(t, depPath, "XGoPackage = true")
				available := false
				base := proj.Importer
				proj.Importer = pkgDocTestImporter(func(path string) (*gotypes.Package, error) {
					switch path {
					case testframework.PkgPath:
						return framework, nil
					case midPath:
						return middle, nil
					case depPath:
						if available {
							return dependency, nil
						}
						return nil, fs.ErrNotExist
					default:
						return base.Import(path)
					}
				})
				if first == "TypeChecking" {
					_, err := proj.TypeInfo()
					require.ErrorContains(t, err, depPath)
				}
				for range 2 {
					pkg, err := proj.Import(testframework.PkgPath)
					require.ErrorIs(t, err, fs.ErrNotExist)
					assert.ErrorContains(t, err, depPath)
					assert.Nil(t, pkg)
				}
				assert.Nil(t, framework.Scope().Lookup("Pick"), "failed imports must leave package initialization untouched")
				assert.Nil(t, middle.Scope().Lookup("Pick"))
				available = true
				pkg, err := proj.Import(testframework.PkgPath)
				require.NoError(t, err)
				assert.Same(t, framework, pkg)
				for _, imported := range []*gotypes.Package{framework, middle, dependency} {
					assert.NotNil(t, imported.Scope().Lookup("Pick"))
				}
				_, err = proj.Fork().TypeInfo()
				require.NoError(t, err)

				available = false
				pkg, err = proj.Import(testframework.PkgPath)
				require.NoError(t, err)
				assert.Same(t, framework, pkg)
			})
		}
	})

	t.Run("DependencyGraph", func(t *testing.T) {
		const depPath = "example.com/dependency"
		const siblingPath = "example.com/sibling"
		proj := newTestProject(t, nil, FeatAll)
		packages := map[string]*gotypes.Package{
			testframework.PkgPath: newImportTestPackage(t, testframework.PkgPath, fmt.Sprintf("XGoPackage = %q", depPath+","+siblingPath)),
			depPath:               newImportTestPackage(t, depPath, fmt.Sprintf("XGoPackage = %q", testframework.PkgPath)),
			siblingPath:           newImportTestPackage(t, siblingPath, fmt.Sprintf("XGoPackage = %q", depPath)),
		}
		calls := make(map[string]int)
		base := proj.Importer
		proj.Importer = pkgDocTestImporter(func(path string) (*gotypes.Package, error) {
			pkg, ok := packages[path]
			if !ok {
				return base.Import(path)
			}
			calls[path]++
			return pkg, nil
		})
		pkg, err := proj.Import(testframework.PkgPath)
		require.NoError(t, err)
		assert.Same(t, packages[testframework.PkgPath], pkg)
		for path, imported := range packages {
			assert.Equal(t, 1, calls[path], "dependencies should be loaded once per import operation")
			assert.NotNil(t, imported.Scope().Lookup("Pick"))
		}

		// An initialized package needs no dependency lookup, and direct and
		// indirect imports still share its package object.
		const extraPath = "example.com/extra"
		packages[extraPath] = newImportTestPackage(t, extraPath, fmt.Sprintf("XGoPackage = %q", testframework.PkgPath+","+depPath))
		clear(calls)
		proj.PutFile("main.xgo", file(`import root "example.com/framework"
import extra "example.com/extra"
echo root.Pick(1), extra.Pick("value")
`))
		_, err = proj.TypeInfo()
		require.NoError(t, err)
		assert.Equal(t, map[string]int{testframework.PkgPath: 1, extraPath: 1, depPath: 1}, calls)
	})
}

func newImportTestPackage(t *testing.T, path, marker string) *gotypes.Package {
	t.Helper()

	fset := token.NewFileSet()
	source := "package fixture\nconst " + marker + `
const Value = 1
func Pick__0(value int) int { return value }
func Pick__1(value string) int { return len(value) }
`
	file, err := goparser.ParseFile(fset, "fixture.go", source, 0)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check(path, fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	return pkg
}
