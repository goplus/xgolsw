package server

import (
	gotypes "go/types"
	"iter"
	"strings"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// frameworkAdapter supplies symbol semantics for a registered framework.
// It is resolved from the current project importer.
type frameworkAdapter interface {
	displayTypeName(gotypes.Object, string) string
	functionDocumentation(*gotypes.Func, *pkgdoc.PkgDoc) (string, bool)
	// A nil sequence leaves the receiver's properties to XGo semantics.
	properties(*gotypes.Named) iter.Seq[propertyObject]
	isEventHandler(*gotypes.Func) bool
}

// frameworkAnalysis holds immutable framework data for one project state.
// Operations receive the requesting project or context so cached data can also
// be used by snapshots without retaining a mutable project or server.
type frameworkAnalysis struct {
	adapter            frameworkAdapter
	resources          *resourceAnalysis
	configurePass      func(*xgo.Project) func(string, *protocol.Pass)
	collectCompletions func(*completionContext)
	inputType          func(gotypes.Type) XGoInputType
	adaptInputSlot     func(*inputSlotContext, ast.Expr, gotypes.Type, *XGoInputSlot) *XGoInputSlot
	renameResources    func(*Server, *xgo.Project, []XGoRenameResourceParams) (*WorkspaceEdit, error)
}

// frameworkAnalysisCacheKind identifies cached framework analysis.
type frameworkAnalysisCacheKind struct{}

// frameworkAdapterCacheKind identifies cached framework symbol semantics.
type frameworkAdapterCacheKind struct{}

// frameworkBaseTypes resolves registered framework base types from packages used by
// this project, including explicit imports and implicit class framework imports.
func frameworkBaseTypes(proj *xgo.Project) map[*gotypes.Named]struct{} {
	result := make(map[*gotypes.Named]struct{})
	frameworksByPackage := make(map[string][]*modfile.Project)
	for framework := range proj.Module().ClassFrameworks() {
		if len(framework.PkgPaths) != 0 {
			frameworksByPackage[framework.PkgPaths[0]] = append(frameworksByPackage[framework.PkgPaths[0]], framework)
		}
	}
	implicitPackages := make(map[string]struct{})
	if astPkg, _ := proj.ASTPackage(); astPkg != nil {
		for filename, file := range astPkg.Files {
			if file.IsClass {
				if framework, ok := proj.Module().LookupClass(modfile.ClassExt(filename)); ok && len(framework.PkgPaths) != 0 {
					implicitPackages[framework.PkgPaths[0]] = struct{}{}
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
		for _, framework := range frameworksByPackage[pkg.Path()] {
			add(framework.Class)
			if !framework.Flat {
				for _, work := range framework.Works {
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
		if pkg, err := proj.Import(pkgPath); err == nil {
			visitPackage(pkg)
		}
	}
	return result
}

// isFrameworkBaseType reports whether named is a registered framework base type
// available in this source snapshot. Type identity keeps unrelated same-named
// types distinct.
func (r *definitionContext) isFrameworkBaseType(named *gotypes.Named) bool {
	if r.frameworkBaseTypes == nil {
		r.frameworkBaseTypes = frameworkBaseTypes(r.proj)
	}
	_, ok := r.frameworkBaseTypes[named.Origin()]
	return ok
}

// resolveFrameworkAdapter retrieves optional symbol semantics without scanning resources.
func resolveFrameworkAdapter(proj *xgo.Project) frameworkAdapter {
	data, _ := proj.Cache(frameworkAdapterCacheKind{})
	adapter, _ := data.(frameworkAdapter)
	return adapter
}

// analyzeFramework retrieves optional framework analysis for the project state.
func analyzeFramework(proj *xgo.Project) (*frameworkAnalysis, error) {
	data, err := proj.Cache(frameworkAnalysisCacheKind{})
	if err != nil {
		return nil, err
	}
	return data.(*frameworkAnalysis), nil
}

// frameworkAdapter resolves optional symbol semantics once per request.
func (r *definitionContext) frameworkAdapter() frameworkAdapter {
	if !r.frameworkResolved {
		r.framework = resolveFrameworkAdapter(r.proj)
		r.frameworkResolved = true
	}
	return r.framework
}

// frameworkDisplayTypeName applies the framework's public type names.
func (r *definitionContext) frameworkDisplayTypeName(obj gotypes.Object, name string) string {
	if adapter := r.frameworkAdapter(); adapter != nil {
		return adapter.displayTypeName(obj, name)
	}
	return name
}

// frameworkFunctionDocumentation supplies documentation for adapted functions.
func (r *definitionContext) frameworkFunctionDocumentation(fun *gotypes.Func, doc *pkgdoc.PkgDoc) (string, bool) {
	if adapter := r.frameworkAdapter(); adapter != nil {
		return adapter.functionDocumentation(fun, doc)
	}
	return "", false
}

// isFrameworkEventHandler reports whether obj registers a framework event.
func (r *definitionContext) isFrameworkEventHandler(obj gotypes.Object) bool {
	fun, ok := obj.(*gotypes.Func)
	if !ok || fun == nil {
		return false
	}
	adapter := r.frameworkAdapter()
	return adapter != nil && adapter.isEventHandler(fun)
}

// isInFrameworkEventHandler checks if the given position is inside a framework event
// handler callback.
func (r *definitionContext) isInFrameworkEventHandler(pos token.Pos) bool {
	astPkg, _ := r.proj.ASTPackage()
	astFile := xgoutil.PosASTFile(r.proj.Fset, astPkg, pos)
	if astFile == nil {
		return false
	}
	typeInfo, _ := expressionTypeInfo(r.proj)
	if typeInfo == nil {
		return false
	}

	for node := range xgoutil.PathEnclosingIntervalNodes(astFile, pos-1, pos, false) {
		call := callExprFromNode(typeInfo, node)
		if call == nil || !r.isFrameworkEventHandler(xgoutil.FuncFromCallExpr(typeInfo, call)) {
			continue
		}
		for expr := range callArgValueTypes(typeInfo, call) {
			var body *ast.BlockStmt
			switch expr := astutil.Unparen(expr).(type) {
			case *ast.FuncLit:
				body = expr.Body
			case *ast.LambdaExpr:
				body = expr.Body
			case *ast.ArrowExpr:
				if expr.Rarrow < pos && pos <= expr.End() {
					return true
				}
			}
			if body != nil && body.Pos() < pos && pos <= body.End() {
				return true
			}
		}
	}
	return false
}
