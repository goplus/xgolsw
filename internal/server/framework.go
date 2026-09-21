package server

import (
	gotypes "go/types"

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
	isPropertyType(*gotypes.Named) bool
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

// isFrameworkPropertyType reports whether a framework exposes named as a property.
func (r *definitionContext) isFrameworkPropertyType(named *gotypes.Named) bool {
	adapter := r.frameworkAdapter()
	return adapter != nil && adapter.isPropertyType(named)
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
	typeInfo, _ := r.proj.TypeInfo()
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
