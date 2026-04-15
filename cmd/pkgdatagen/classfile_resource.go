package main

import (
	"fmt"
	goast "go/ast"
	"go/types"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/cl/outline"
	xgoclassfile "github.com/goplus/xgo/cl/outline/classfile"
	xgoparser "github.com/goplus/xgo/parser"
	xgotoken "github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"golang.org/x/tools/go/gcexportdata"
)

const classfileDirectivePrefix = "//xgo:class:"

// buildPkgResourceSchema builds one serialized discovery schema from one package.
func buildPkgResourceSchema(pkgPath string, dir string, pkgName string, buildFiles []string, astFiles map[string]*goast.File) (*pkgdata.PkgResourceSchema, error) {
	if !hasClassfileResourceDirectives(astFiles) {
		return nil, nil
	}

	allowedFiles := make(map[string]struct{}, len(buildFiles))
	for _, name := range buildFiles {
		allowedFiles[name] = struct{}{}
	}

	fset := xgotoken.NewFileSet()
	pkgs, err := xgoparser.ParseDirEx(fset, dir, xgoparser.Config{
		Filter: func(info fs.FileInfo) bool {
			_, ok := allowedFiles[info.Name()]
			return ok
		},
		Mode: xgoparser.ParseComments,
	})
	if err != nil {
		return nil, err
	}
	pkg := pkgs[pkgName]
	if pkg == nil {
		return nil, nil
	}

	schema, err := xgoclassfile.LoadResourceSchema(pkg, &modfile.Project{
		PkgPaths: []string{pkgPath},
	}, &outline.Config{
		Fset:     fset,
		Importer: newPkggenImporter(),
	})
	if err != nil {
		return nil, err
	}

	if len(schema.Kinds) == 0 {
		return nil, nil
	}

	kinds := make([]pkgdata.PkgResourceKind, 0, len(schema.Kinds))
	for _, kind := range schema.Kinds {
		canonicalType := ""
		if kind.CanonicalType != nil {
			canonicalType = kind.CanonicalType.Name()
		}
		kinds = append(kinds, pkgdata.PkgResourceKind{
			Name:               kind.Name,
			CanonicalType:      canonicalType,
			DiscoveryQuery:     kind.DiscoveryQuery,
			NameDiscoveryQuery: kind.NameDiscoveryQuery,
		})
	}
	return &pkgdata.PkgResourceSchema{
		Kinds:            kinds,
		APIScopeBindings: buildPkgResourceAPIScopeBindings(schema),
	}, nil
}

// pkggenImporter resolves imports from module-aware export data produced by `go list -export`.
type pkggenImporter struct {
	mu     sync.Mutex
	fset   *xgotoken.FileSet
	loaded map[string]*types.Package
}

// newPkggenImporter creates a new [pkggenImporter].
func newPkggenImporter() *pkggenImporter {
	loaded := make(map[string]*types.Package)
	loaded["unsafe"] = types.Unsafe
	return &pkggenImporter{
		fset:   xgotoken.NewFileSet(),
		loaded: loaded,
	}
}

// Import implements [types.Importer].
func (imp *pkggenImporter) Import(path string) (*types.Package, error) {
	imp.mu.Lock()
	defer imp.mu.Unlock()

	if pkg, ok := imp.loaded[path]; ok && pkg.Complete() {
		return pkg, nil
	}

	exportFile, err := execGo("list", "-trimpath", "-export", "-f", "{{.Export}}", path)
	if err != nil {
		return nil, fmt.Errorf("failed to list package export file: %w", err)
	}
	exportPath := strings.TrimSpace(string(exportFile))
	if exportPath == "" {
		return nil, fmt.Errorf("empty export file for package %q", path)
	}

	f, err := os.Open(exportPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open package export file: %w", err)
	}
	defer f.Close()

	r, err := gcexportdata.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("failed to create package export reader: %w", err)
	}

	pkg, err := gcexportdata.Read(r, imp.fset, imp.loaded, path)
	if err != nil {
		return nil, fmt.Errorf("failed to read package export data: %w", err)
	}
	return pkg, nil
}

// hasClassfileResourceDirectives reports whether the parsed files contain any
// classfile resource directives worth extracting.
func hasClassfileResourceDirectives(astFiles map[string]*goast.File) bool {
	for _, file := range astFiles {
		for _, group := range file.Comments {
			for _, comment := range group.List {
				if strings.HasPrefix(strings.TrimSpace(comment.Text), classfileDirectivePrefix) {
					return true
				}
			}
		}
	}
	return false
}

// buildPkgResourceAPIScopeBindings serializes the API-position scope bindings
// declared in schema.
func buildPkgResourceAPIScopeBindings(schema *xgoclassfile.ResourceSchema) []pkgdata.PkgResourceAPIScopeBinding {
	callables := make(map[string]*types.Func)
	scope := schema.Package.Scope()
	names := slices.Clone(scope.Names())
	slices.Sort(names)
	for _, name := range names {
		obj := scope.Lookup(name)
		fn, ok := obj.(*types.Func)
		if !ok {
			continue
		}
		callables[fn.FullName()] = fn
	}

	handleTypes := make([]*types.TypeName, 0)
	for _, kind := range schema.Kinds {
		handleTypes = append(handleTypes, kind.HandleTypes...)
	}
	slices.SortFunc(handleTypes, func(a, b *types.TypeName) int {
		return strings.Compare(a.Name(), b.Name())
	})
	for _, obj := range handleTypes {
		var methods []*types.Func
		keyOf := func(fn *types.Func) string {
			return pkgResourceCallableKey(fn, obj)
		}
		if iface, ok := types.Unalias(obj.Type()).Underlying().(*types.Interface); ok {
			methods = make([]*types.Func, 0, iface.NumExplicitMethods())
			for i := range iface.NumExplicitMethods() {
				methods = append(methods, iface.ExplicitMethod(i))
			}
		} else {
			named, ok := obj.Type().(*types.Named)
			if !ok {
				continue
			}
			methods = make([]*types.Func, 0, named.NumMethods())
			for i := range named.NumMethods() {
				methods = append(methods, named.Method(i))
			}
		}
		slices.SortFunc(methods, func(a, b *types.Func) int {
			return strings.Compare(keyOf(a), keyOf(b))
		})
		for _, fn := range methods {
			callables[keyOf(fn)] = fn
		}
	}

	keys := slices.Collect(maps.Keys(callables))
	slices.Sort(keys)
	bindings := make([]pkgdata.PkgResourceAPIScopeBinding, 0)
	for _, key := range keys {
		fn := callables[key]
		for _, binding := range schema.APIScopeBindings(fn) {
			bindings = append(bindings, pkgdata.PkgResourceAPIScopeBinding{
				Callable:       key,
				TargetParam:    binding.TargetParam,
				SourceReceiver: binding.Source.Receiver,
				SourceParam:    binding.Source.Param,
			})
		}
	}
	return bindings
}

// pkgResourceCallableKey reports the serialized callable identity used by
// pkgresource artifacts.
func pkgResourceCallableKey(fn *types.Func, owner *types.TypeName) string {
	if owner != nil {
		if _, ok := types.Unalias(owner.Type()).Underlying().(*types.Interface); ok {
			return fmt.Sprintf("(%s.%s).%s", owner.Pkg().Path(), owner.Name(), fn.Name())
		}
	}
	return fn.FullName()
}
