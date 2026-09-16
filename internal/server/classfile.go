package server

import (
	gotypes "go/types"
	"strings"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/cl"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// classBaseTypes resolves registered framework base types from packages used by
// this project, including explicit imports and classfile implicit imports.
func classBaseTypes(proj *xgo.Project) map[*gotypes.Named]struct{} {
	result := make(map[*gotypes.Named]struct{})
	classes := make(map[string][]*modfile.Project)
	for class := range proj.Module().ClassProjects() {
		if len(class.PkgPaths) != 0 {
			classes[class.PkgPaths[0]] = append(classes[class.PkgPaths[0]], class)
		}
	}
	implicitPackages := make(map[string]struct{})
	if astPkg, _ := proj.ASTPackage(); astPkg != nil {
		for filename, file := range astPkg.Files {
			if file.IsClass {
				if class, ok := proj.Module().LookupClass(modfile.ClassExt(filename)); ok && len(class.PkgPaths) != 0 {
					implicitPackages[class.PkgPaths[0]] = struct{}{}
				}
			}
		}
	}

	seenPackages := make(map[*gotypes.Package]struct{})
	var visitPackage func(*gotypes.Package)
	visitPackage = func(pkg *gotypes.Package) {
		if _, seen := seenPackages[pkg]; seen {
			return
		}
		seenPackages[pkg] = struct{}{}
		add := func(name string) {
			obj, ok := pkg.Scope().Lookup(strings.TrimPrefix(name, "*")).(*gotypes.TypeName)
			if !ok {
				return
			}
			if named := resolvedNamedType(obj.Type()); named != nil {
				result[named.Origin()] = struct{}{}
			}
		}
		for _, class := range classes[pkg.Path()] {
			add(class.Class)
			if !class.Flat {
				for _, work := range class.Works {
					add(work.Class)
				}
			}
		}
		for _, imported := range pkg.Imports() {
			visitPackage(imported)
		}
	}
	if info, _ := proj.TypeInfo(); info != nil {
		// XGo records explicit imports as package-name objects rather than
		// populating info.Pkg.Imports(). Dependencies may re-export base types.
		for _, obj := range info.Defs {
			if name, ok := obj.(*gotypes.PkgName); ok {
				visitPackage(name.Imported())
			}
		}
		for _, obj := range info.Implicits {
			if name, ok := obj.(*gotypes.PkgName); ok {
				visitPackage(name.Imported())
			}
		}
	}
	for pkgPath := range implicitPackages {
		if pkg, err := proj.Importer.Import(pkgPath); err == nil {
			visitPackage(pkg)
		}
	}
	return result
}

// isClassBaseType reports whether named is a registered framework base type
// available in this source snapshot. Type identity keeps unrelated same-named
// types distinct.
func (r *definitionContext) isClassBaseType(named *gotypes.Named) bool {
	if r.classTypes == nil {
		r.classTypes = classBaseTypes(r.proj)
	}
	_, ok := r.classTypes[named.Origin()]
	return ok
}

// classTypeForFile resolves the type generated for a classfile using the
// compiler's naming rules and the project's framework registrations.
func classTypeForFile(proj *xgo.Project, file *ast.File) *gotypes.Named {
	if file == nil || !file.IsClass {
		return nil
	}
	info, _ := proj.TypeInfo()
	if info == nil {
		return nil
	}
	name, _ := cl.GetFileClassType(file, xgoutil.NodeFilename(proj.Fset, file), proj.Module().LookupClass)
	obj, ok := info.Pkg.Scope().Lookup(name).(*gotypes.TypeName)
	if !ok {
		return nil
	}
	return resolvedNamedType(obj.Type())
}
