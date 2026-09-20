package xgo

import (
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"io/fs"
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
					"main.xgo": file("import framework \"example.com/framework\"\necho framework.Value\n"),
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

		// Separate imports in one compilation share already loaded dependencies.
		const extraPath = "example.com/extra"
		packages[extraPath] = newImportTestPackage(t, extraPath, fmt.Sprintf("XGoPackage = %q", depPath))
		clear(calls)
		proj.PutFile("main.xgo", file("import root \"example.com/framework\"\nimport extra \"example.com/extra\"\necho root.Pick(1), extra.Pick(\"value\")\n"))
		_, err = proj.TypeInfo()
		require.NoError(t, err)
		for path := range packages {
			assert.Equal(t, 1, calls[path])
		}
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
