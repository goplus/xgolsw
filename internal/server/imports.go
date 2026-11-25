package server

import (
	"cmp"
	gotypes "go/types"
	"maps"
	"slices"
	"sync"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// fileImports holds the effective package bindings for one source file. Named
// imports include class framework auto-imports. The member packages list
// contains framework lookups followed by explicit dot imports. The cached
// bindings are read-only.
type fileImports struct {
	named          map[string]*gotypes.PkgName
	names          []*gotypes.PkgName
	members        []*gotypes.Package
	dotImportStart int
	ambiguous      map[string]bool
}

// fileImportsCacheKind identifies import bindings for one project revision.
type fileImportsCacheKind struct{}

// buildFileImportsCache resolves imports on demand for files in a checked
// snapshot so explicit imports retain compiler-recorded identities.
func buildFileImportsCache(proj *xgo.Project) (any, error) {
	proj.TypeInfo()
	proj = proj.Snapshot()
	info, _ := proj.TypeInfo()
	astPkg, _ := proj.ASTPackage()
	if info == nil || astPkg == nil {
		return (map[string]func() *fileImports)(nil), nil
	}
	files := make(map[string]func() *fileImports, len(astPkg.Files))
	for filename, file := range astPkg.Files {
		files[filename] = sync.OnceValue(func() *fileImports {
			return resolveFileImports(proj, info, filename, file)
		})
	}
	return files, nil
}

// resolveFileImports follows the compiler's explicit-import precedence over
// auto-imports. Auto-import directives do not expose unqualified members.
func resolveFileImports(proj *xgo.Project, info *types.Info, filename string, file *ast.File) *fileImports {
	imports := &fileImports{named: make(map[string]*gotypes.PkgName)}
	if file.IsClass && !file.IsNormalGox {
		if framework, ok := proj.Module().LookupClass(xgoutil.ClassfileExt(filename)); ok {
			// Flat fragments receive named auto-imports, but the compiler
			// only installs framework lookup packages on the project file.
			if !framework.Flat || file.IsProj {
				for _, path := range framework.PkgPaths {
					if pkg, err := proj.Import(path); err == nil {
						imports.members = append(imports.members, pkg)
					}
				}
			}
			for _, imp := range framework.Import {
				pkg, err := proj.Import(imp.Path)
				if err != nil {
					continue
				}
				name := imp.Name
				if name == "" {
					name = pkg.Name()
				}
				if name != "." && name != "_" {
					imports.named[name] = gotypes.NewPkgName(token.NoPos, info.Pkg, name, pkg)
				}
			}
		}
	}
	imports.dotImportStart = len(imports.members)
	for _, spec := range file.Imports {
		name := importPackageName(info, spec)
		if name == nil {
			continue
		}
		switch name.Name() {
		case ".":
			imports.members = append(imports.members, name.Imported())
		case "_":
		default:
			imports.named[name.Name()] = name
		}
	}
	// Conflicting unqualified declarations cannot be selected by import order.
	// Keep repeated packages: XGo treats their unqualified names as ambiguous,
	// including a classfile lookup package also imported with a dot.
	imports.ambiguous = make(map[string]bool)
	seen := make(map[string]bool)
	add := func(name string) {
		if seen[name] {
			imports.ambiguous[name] = true
		}
		seen[name] = true
	}
	for _, pkg := range imports.members {
		for _, name := range pkg.Scope().Names() {
			obj := pkg.Scope().Lookup(name)
			if !obj.Exported() {
				continue
			}
			add(name)
			if gogen.IsFunc(obj.Type()) {
				if alias := xgoutil.FunctionAliasName(name); alias != name {
					add(alias)
				}
			}
		}
	}
	imports.names = slices.SortedFunc(maps.Values(imports.named), func(a, b *gotypes.PkgName) int {
		return cmp.Compare(a.Name(), b.Name())
	})
	return imports
}

// importPackageName returns the compiler's binding for a source import.
func importPackageName(info *types.Info, spec *ast.ImportSpec) *gotypes.PkgName {
	obj := info.Implicits[spec]
	if spec.Name != nil {
		obj = info.Defs[spec.Name]
	}
	name, _ := obj.(*gotypes.PkgName)
	return name
}

// importsForFile returns imports from the same snapshot as file. Files without
// syntax or type information have no resolved imports.
func importsForFile(proj *xgo.Project, file *ast.File) *fileImports {
	if file == nil {
		return &fileImports{}
	}
	data, _ := proj.Cache(fileImportsCacheKind{})
	files := data.(map[string]func() *fileImports)
	if load := files[proj.Fset.PositionFor(file.Pos(), false).Filename]; load != nil {
		return load()
	}
	return &fileImports{}
}
