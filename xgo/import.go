package xgo

import (
	"fmt"
	"go/constant"
	gotypes "go/types"
	"maps"
	"strings"

	"github.com/goplus/gogen"
)

// Import loads a package and initializes its XGo declarations before exposing
// them to readers. Initialization is serialized with type checking in this
// project and its snapshots. The project also implements [go/types.Importer].
func (p *Project) Import(path string) (pkg *gotypes.Package, err error) {
	p.astMu.Lock()
	defer p.astMu.Unlock()
	defer func() {
		switch recovered := recover().(type) {
		case nil:
		case *gogen.ImportError:
			err = recovered
		default:
			panic(recovered)
		}
	}()
	loader := gogen.NewPackage(p.PkgPath, "", &gogen.Config{
		Fset:     p.Fset,
		Importer: newDependencyImporter(p.Importer),
		// Import initialization does not need code generation builtins.
		NewBuiltin: func(*gogen.Package, *gogen.Config) *gotypes.Package { return nil },
	})
	return loader.Import(path).Types, nil
}

// dependencyImporter loads XGo dependencies before gogen initializes a package.
// Gogen ignores dependency import errors and marks the package initialized, so
// all dependencies must be available before returning it to the compiler.
type dependencyImporter struct {
	base     gotypes.Importer
	packages map[string]*gotypes.Package
}

// newDependencyImporter wraps an explicit importer for one analysis operation.
// A nil importer preserves the compiler's default importer selection.
func newDependencyImporter(base gotypes.Importer) gotypes.Importer {
	if base == nil {
		return nil
	}
	return &dependencyImporter{base: base, packages: make(map[string]*gotypes.Package)}
}

// Import loads a package and its declared XGo dependencies without mutating them.
func (imp *dependencyImporter) Import(path string) (*gotypes.Package, error) {
	if pkg := imp.packages[path]; pkg != nil {
		return pkg, nil
	}
	pending := make(map[string]*gotypes.Package)
	pkg, err := imp.load(path, pending)
	if err != nil {
		return nil, err
	}
	maps.Copy(imp.packages, pending)
	return pkg, nil
}

// load resolves the dependency graph, tracking cycles and publishing no packages
// to the importer cache until every dependency has been loaded successfully.
func (imp *dependencyImporter) load(path string, pending map[string]*gotypes.Package) (*gotypes.Package, error) {
	if pkg := imp.packages[path]; pkg != nil {
		return pkg, nil
	}
	if pkg := pending[path]; pkg != nil {
		return pkg, nil
	}
	pkg, err := imp.base.Import(path)
	if err != nil {
		return nil, fmt.Errorf("failed to import package %q: %w", path, err)
	}
	pending[path] = pkg
	marker := pkg.Scope().Lookup("XGoPackage")
	if marker == nil {
		marker = pkg.Scope().Lookup("GopPackage")
	}
	deps, ok := marker.(*gotypes.Const)
	if !ok || deps.Val().Kind() != constant.String {
		return pkg, nil
	}
	for dep := range strings.SplitSeq(constant.StringVal(deps.Val()), ",") {
		if _, err := imp.load(dep, pending); err != nil {
			return nil, fmt.Errorf("failed to import XGo dependency %q of %q: %w", dep, path, err)
		}
	}
	return pkg, nil
}
