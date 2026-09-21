package server

import (
	"cmp"
	gotypes "go/types"
	"maps"
	"slices"

	"github.com/goplus/gogen"
	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// fileImports holds the effective package bindings for one source file. Named
// imports include classfile auto-imports. Members come from classfile lookup
// packages and explicit dot imports. Cached bindings must not be modified.
type fileImports struct {
	named     map[string]*gotypes.PkgName
	names     []*gotypes.PkgName
	members   []*gotypes.Package
	ambiguous map[string]bool
}

// fileImportsCacheKind identifies import bindings for one project revision.
type fileImportsCacheKind struct{}

// buildFileImportsCache resolves imports after type checking so explicit imports
// retain compiler-recorded identities. Snapshots keep their own registrations.
func buildFileImportsCache(proj *xgo.Project) (any, error) {
	proj.TypeInfo()
	proj = proj.Snapshot()
	info, _ := proj.TypeInfo()
	astPkg, _ := proj.ASTPackage()
	files := make(map[string]*fileImports)
	if info == nil || astPkg == nil {
		return files, nil
	}
	for filename, file := range astPkg.Files {
		files[filename] = resolveFileImports(proj, info, filename, file)
	}
	return files, nil
}

// resolveFileImports follows the compiler's explicit-import precedence over
// auto-imports. Auto-import directives do not expose unqualified members.
func resolveFileImports(proj *xgo.Project, info *types.Info, filename string, file *ast.File) *fileImports {
	imports := &fileImports{named: make(map[string]*gotypes.PkgName)}
	if file.IsClass && !file.IsNormalGox {
		if class, ok := proj.Module().LookupClass(modfile.ClassExt(filename)); ok {
			for _, path := range class.PkgPaths {
				if pkg, err := proj.Import(path); err == nil {
					imports.members = append(imports.members, pkg)
				}
			}
			for _, imp := range class.Import {
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
	for _, spec := range file.Imports {
		var obj gotypes.Object
		if spec.Name != nil {
			obj = info.Defs[spec.Name]
		} else {
			obj = info.Implicits[spec]
		}
		name, ok := obj.(*gotypes.PkgName)
		if !ok {
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
				if alias := xgoutil.ToLowerCamelCase(name); alias != name {
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

// importsForFile returns imports from the same snapshot as file. Files without
// syntax or type information have no resolved imports.
func importsForFile(proj *xgo.Project, file *ast.File) *fileImports {
	if file == nil {
		return &fileImports{}
	}
	data, _ := proj.Cache(fileImportsCacheKind{})
	files := data.(map[string]*fileImports)
	if imports := files[proj.Fset.PositionFor(file.Pos(), false).Filename]; imports != nil {
		return imports
	}
	return &fileImports{}
}
