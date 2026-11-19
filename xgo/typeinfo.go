/*
 * Copyright (c) 2025 The XGo Authors (xgo.dev). All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package xgo

import (
	"cmp"
	"fmt"
	gotypes "go/types"
	"iter"
	"runtime"
	"slices"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/cl"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/qiniu/x/errors"
)

// typeInfoCacheKind is a cache kind type for [types.Info].
type typeInfoCacheKind struct{}

// typeInfoCache is a cache for [types.Info].
type typeInfoCache struct {
	astPkg     *ast.Package
	parserErr  error
	module     *Module
	typeInfo   *types.Info
	checkerErr error
}

// buildTypeInfoCache implements [CacheBuilder] to build a [typeInfoCache] for
// the provided XGo project.
func buildTypeInfoCache(proj *Project) (any, error) {
	astPkg, astErr := proj.ASTPackage()
	if astPkg == nil {
		return nil, fmt.Errorf("failed to retrieve AST package: %w", astErr)
	}
	proj.astMu.Lock()
	defer proj.astMu.Unlock()

	typeInfo := types.NewInfo(gotypes.NewPackage(proj.PkgPath, astPkg.Name))

	module := proj.Module()
	recorder := &callTypeRecorder{Recorder: typesutil.NewRecorder(&typeInfo.Info), info: typeInfo}
	checkerErrs := checkProjectTypes(proj, astPkg, module, recorder)

	// Build reverse mapping for O(1) object-to-identifier lookup. For
	// identifiers that do not denote objects, the object is nil and they
	// are excluded from this mapping.
	typeInfo.ObjToDef = make(map[gotypes.Object]*ast.Ident, len(typeInfo.Defs))
	for ident, obj := range typeInfo.Defs {
		if obj != nil {
			typeInfo.ObjToDef[obj] = ident
		}
	}
	// Record decorator syntax and recover declarations omitted by the compiler
	// so all consumers share the same source and argument mappings.
	var qualifiedOverloads map[*ast.Ident]bool
	memberOverloads := xgoutil.NewOverloadSourceLookup(typeInfo.Pkg)
	for _, file := range astPkg.Files {
		ast.Inspect(file, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok && len(recorder.keywordArgs) != 0 {
				recorder.recordPartialKeywordBuilder(call)
			}
			if selector, ok := node.(*ast.SelectorExpr); ok && typeInfo.Overloads[selector.Sel] != nil {
				if qualifiedOverloads == nil {
					qualifiedOverloads = make(map[*ast.Ident]bool)
				}
				qualifiedOverloads[selector.Sel] = true
				if receiver := typeInfo.TypeOf(selector.X); xgoutil.IsValidType(receiver) {
					recordOverloadSource(typeInfo, memberOverloads, selector.Sel, receiver)
				}
				return true
			}
			if decorator, ok := node.(*ast.FuncDecorator); ok {
				typeInfo.FuncDecorators[&decorator.CallExpr] = true
				return true
			}
			if stmt, ok := node.(*ast.TypeSwitchStmt); ok {
				recordTypeSwitchDeclaration(typeInfo, stmt)
				return true
			}
			if decl, ok := node.(*ast.FuncDecl); ok && decl.Static {
				recordStaticMethodReceiver(typeInfo, decl)
				return true
			}
			spec, ok := node.(*ast.TypeSpec)
			if !ok || typeInfo.Defs[spec.Name] != nil {
				return true
			}
			scope := xgoutil.InnermostScopeAt(proj.Fset, typeInfo, astPkg, spec.Name.Pos())
			if scope == nil || scope == typeInfo.Pkg.Scope() {
				return true
			}
			obj, ok := scope.Lookup(spec.Name.Name).(*gotypes.TypeName)
			if ok && typeInfo.ObjToDef[obj] == nil {
				typeInfo.Defs[spec.Name] = obj
				typeInfo.ObjToDef[obj] = spec.Name
			}
			return true
		})
	}

	recordClassOverloadImplementations(typeInfo, astPkg)
	recordClassTypes(typeInfo, astPkg, module)
	for ident := range typeInfo.Overloads {
		var receiver gotypes.Type
		if !qualifiedOverloads[ident] {
			file := xgoutil.PosASTFile(proj.Fset, astPkg, ident.Pos())
			receiver = xgoutil.ClassReceiverAt(typeInfo, file, ident.Pos())
		}
		if receiver == nil || !recordOverloadSource(typeInfo, memberOverloads, ident, receiver) {
			recordOverloadSource(typeInfo, memberOverloads, ident, nil)
		}
		if fun, ok := typeInfo.Overloads[ident].(*gotypes.Func); receiver != nil && ok && fun.Signature().Recv() != nil {
			if typeInfo.ImplicitReceivers == nil {
				typeInfo.ImplicitReceivers = make(map[*ast.Ident]gotypes.Type)
			}
			typeInfo.ImplicitReceivers[ident] = receiver
		}
	}

	// XGo completes interfaces before setting their named underlying types.
	// Go 1.26 no longer rejects cycles during that early step. Check after
	// recovering local declarations so unused local interfaces are covered too.
	interfaceCycles := make(map[*gotypes.Interface]bool)
	var cycleErrors []typesutil.Error
	for obj, ident := range typeInfo.ObjToDef {
		if name, ok := obj.(*gotypes.TypeName); ok && hasInterfaceEmbeddingCycle(name.Type(), interfaceCycles) {
			cycleErrors = append(cycleErrors, typesutil.Error{
				Fset: proj.Fset, Pos: ident.Pos(), End: ident.End(),
				Msg: "invalid recursive interface " + name.Name(),
			})
		}
	}
	slices.SortFunc(cycleErrors, func(a, b typesutil.Error) int {
		return cmp.Compare(a.Pos, b.Pos)
	})
	for _, err := range cycleErrors {
		checkerErrs.Add(err)
	}
	return &typeInfoCache{
		astPkg:     astPkg,
		parserErr:  astErr,
		module:     module,
		typeInfo:   typeInfo,
		checkerErr: checkerErrs.ToError(),
	}, nil
}

// checkProjectTypes uses the compiler recorder to keep source call information.
func checkProjectTypes(proj *Project, astPkg *ast.Package, module *Module, recorder *callTypeRecorder) errors.List {
	pkg := recorder.info.Pkg
	if len(astPkg.Files) == 0 {
		var errs errors.List
		checker := gotypes.NewChecker(&gotypes.Config{}, proj.Fset, pkg, nil)
		errs.Add(checker.Files(nil))
		return errs
	}
	files := make(map[string]*ast.File, len(astPkg.Files))
	for _, file := range astPkg.Files {
		if physical := proj.Fset.File(file.Pos()); physical != nil {
			files[physical.Name()] = file
		}
	}
	compiled, err := cl.NewPackage(pkg.Path(), &ast.Package{Name: astPkg.Name, Files: files}, &cl.Config{
		Types:          pkg,
		Fset:           proj.Fset,
		LookupClass:    module.LookupClass,
		Importer:       proj.newDependencyImporter(),
		Recorder:       recorder,
		NoFileLine:     true,
		NoAutoGenMain:  true,
		NoSkipConstant: true,
	})
	if compiled != nil {
		recorder.info.Builtins = compiled.Builtin().Types
	}
	return typeCheckDiagnostics(proj.Fset, recorder.info, err)
}

// typeCheckDiagnostics normalizes checker errors and retains failed import paths.
func typeCheckDiagnostics(fset *token.FileSet, info *types.Info, err error) errors.List {
	if err == nil {
		return nil
	}
	errs, listed := err.(errors.List)
	if !listed {
		errs = errors.List{err}
	}
	var diagnostics errors.List
	for _, err := range errs {
		diagnostic := typesutil.Error{Fset: fset}
		switch err := err.(type) {
		case *gogen.CodeError:
			diagnostic.Pos, diagnostic.End, diagnostic.Msg = err.Pos, err.End, err.Msg
		case *gogen.MatchError:
			if err.Src != nil {
				diagnostic.Pos, diagnostic.End = err.Src.Pos(), err.Src.End()
			}
			diagnostic.Msg = matchErrorMessage(err)
		case *gogen.ImportError:
			diagnostic.Pos, diagnostic.End, diagnostic.Msg = err.Pos, err.End, err.Err.Error()
			if err.Pos.IsValid() {
				if info.FailedImportPaths == nil {
					info.FailedImportPaths = make(map[token.Pos]string)
				}
				info.FailedImportPaths[err.Pos] = err.Path
			}
		default:
			continue
		}
		diagnostics.Add(diagnostic)
	}
	if len(diagnostics) == 0 {
		diagnostics.Add(err)
	}
	return diagnostics
}

// matchErrorMessage preserves the type mismatch if gogen cannot slice the
// source for a synthetic argument or callee.
func matchErrorMessage(err *gogen.MatchError) (message string) {
	defer func() {
		if failure := recover(); failure != nil {
			if _, ok := failure.(runtime.Error); !ok {
				panic(failure)
			}
			message = fmt.Sprintf("cannot use %v value as type %v", err.Arg, err.Param)
		}
	}()
	return err.Message("")
}

// callTypeRecorder connects overload selections and lowered keyword arguments
// to their source calls.
type callTypeRecorder struct {
	cl.Recorder

	info           *types.Info
	parenOverloads map[*ast.ParenExpr]types.OverloadSelection
	keywordArgs    map[keywordArgumentKey]ast.Expr
}

// keywordArgumentKey identifies the last keyword in a lowered argument list.
type keywordArgumentKey struct {
	pos   token.Pos
	value ast.Expr
}

// Use records the ordinary binding and retains any nested overload selection.
func (r *callTypeRecorder) Use(ident *ast.Ident, obj gotypes.Object) {
	previous := r.info.Uses[ident]
	source := r.info.Overloads[ident]
	r.Recorder.Use(ident, obj)
	if source == nil || previous == obj && obj != source {
		return
	}
	selected := r.info.SelectedOverloads[ident]
	delete(r.info.SelectedOverloads, ident)
	if source == obj {
		return
	}
	for candidate := range overloadObjects(source) {
		if candidate != obj {
			continue
		}
		// Preserve the written group across callbacks so another lookup of
		// it starts a fresh selection when the compiler retries an argument.
		if r.info.Overloads[ident] == obj {
			r.info.Overloads[ident] = source
			if group, ok := obj.(*gotypes.Func); ok {
				r.selectOverload(ident, enclosingOverloadSelection(group, previous, selected))
			}
		}
		return
	}
	// A new receiver can resolve the same spelling to an ordinary method.
	if r.info.Overloads[ident] == source {
		delete(r.info.Overloads, ident)
	}
}

// Type retains lowered keyword types and parenthesized overload selections
// when the compiler records a checked call.
func (r *callTypeRecorder) Type(expr ast.Expr, tv gotypes.TypeAndValue) {
	switch expr := expr.(type) {
	case *ast.CompositeLit:
		r.recordKeywordLiteral(expr)
	case *ast.ParenExpr:
		r.recordParenthesizedOverload(expr, r.info.Types[expr].Type, tv.Type)
	case *ast.CallExpr:
		r.recordKeywordCall(expr)
		if paren, ok := xgoutil.CallExprFun(expr).(*ast.ParenExpr); ok {
			if selected := r.parenOverloads[paren]; selected.Implementation != nil {
				r.selectOverload(parenthesizedCallIdent(paren), selected)
				delete(r.parenOverloads, paren)
			}
		}
	}
	r.Recorder.Type(expr, tv)
}

// recordKeywordLiteral tracks successful synthetic literals created by
// keyword lowering. They have no source colon or literal type expression.
func (r *callTypeRecorder) recordKeywordLiteral(lit *ast.CompositeLit) {
	if lit.Type != nil || len(lit.Elts) == 0 {
		return
	}
	last, ok := lit.Elts[len(lit.Elts)-1].(*ast.KeyValueExpr)
	if !ok || last.Colon.IsValid() || lit.Rbrace != last.Value.End() {
		return
	}
	r.recordKeywordArg(keywordArgumentKey{pos: last.Key.Pos(), value: last.Value}, lit)
}

// recordKeywordCall associates the last successful lowering with the checked
// source call. Synthetic builder calls have no written argument delimiters.
func (r *callTypeRecorder) recordKeywordCall(call *ast.CallExpr) {
	if len(call.Kwargs) == 0 {
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && !call.Lparen.IsValid() && !call.NoParenEnd.IsValid() && len(call.Args) != 0 {
			r.recordKeywordArg(keywordArgumentKey{pos: selector.Sel.Pos(), value: call.Args[len(call.Args)-1]}, call)
		}
		return
	}
	last := call.Kwargs[len(call.Kwargs)-1]
	key := keywordArgumentKey{pos: last.Name.Pos(), value: last.Value}
	arg := r.keywordArgs[key]
	delete(r.keywordArgs, key)
	if arg == nil {
		delete(r.info.KeywordArgs, last)
		return
	}
	var lowering types.KeywordLowering
	if builder, ok := arg.(*ast.CallExpr); ok {
		lowering = r.keywordBuilderLowering(builder)
	}
	lowering.Arg = arg
	r.setKeywordLowering(last, lowering)
}

// recordPartialKeywordBuilder saves a checked prefix when a later step or
// the outer call failed. Overload attempts can build different prefixes.
func (r *callTypeRecorder) recordPartialKeywordBuilder(call *ast.CallExpr) {
	if len(call.Kwargs) == 0 {
		return
	}
	last := call.Kwargs[len(call.Kwargs)-1]
	if _, recorded := r.info.KeywordArgs[last]; recorded || xgoutil.CallExprOverloadImplementations(r.info, call) != nil {
		return
	}
	for _, arg := range slices.Backward(call.Kwargs) {
		key := keywordArgumentKey{pos: arg.Name.Pos(), value: arg.Value}
		if step, ok := r.keywordArgs[key].(*ast.CallExpr); ok {
			if lowering := r.keywordBuilderLowering(step); lowering.Factory != nil {
				r.setKeywordLowering(last, lowering)
				return
			}
		}
	}
}

// setKeywordLowering records the lowering under the last source keyword.
func (r *callTypeRecorder) setKeywordLowering(last *ast.KwargExpr, lowering types.KeywordLowering) {
	if r.info.KeywordArgs == nil {
		r.info.KeywordArgs = make(map[*ast.KwargExpr]types.KeywordLowering)
	}
	r.info.KeywordArgs[last] = lowering
}

// recordKeywordArg retains a lowering until the source call is checked.
func (r *callTypeRecorder) recordKeywordArg(key keywordArgumentKey, arg ast.Expr) {
	if r.keywordArgs == nil {
		r.keywordArgs = make(map[keywordArgumentKey]ast.Expr)
	}
	r.keywordArgs[key] = arg
}

// keywordBuilderLowering collects the checked builder steps and their factory
// and removes their pending records. The caller records the synthesized
// argument when the full keyword list was checked.
func (r *callTypeRecorder) keywordBuilderLowering(call *ast.CallExpr) types.KeywordLowering {
	steps := make(map[token.Pos]*ast.CallExpr)
	for {
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return types.KeywordLowering{}
		}
		if len(call.Args) == 0 {
			for pos, step := range steps {
				delete(r.keywordArgs, keywordArgumentKey{pos: pos, value: step.Args[len(step.Args)-1]})
			}
			return types.KeywordLowering{Factory: call, BuilderSteps: steps}
		}
		previous, ok := selector.X.(*ast.CallExpr)
		if !ok {
			return types.KeywordLowering{}
		}
		steps[selector.Sel.Pos()] = call
		call = previous
	}
}

// recordParenthesizedOverload recovers a candidate only from the compiler's
// exact type records. Distinct declarations can share a function value type.
func (r *callTypeRecorder) recordParenthesizedOverload(expr *ast.ParenExpr, previous, current gotypes.Type) {
	if previous == nil || previous == current {
		return
	}
	ident := parenthesizedCallIdent(expr)
	source := r.info.Overloads[ident]
	if source == nil || current == source.Type() {
		delete(r.parenOverloads, expr)
		return
	}
	obj := overloadObjectWithType(source, current)
	selected := types.OverloadSelection{}
	if group, ok := obj.(*gotypes.Func); ok {
		// An enclosing group can disambiguate a type shared by multiple
		// candidates of the original group.
		prior := overloadObjectWithType(group, previous)
		selected = enclosingOverloadSelection(group, prior, r.parenOverloads[expr])
	}
	if selected.Implementation == nil {
		selected = xgoutil.ConcreteOverloadSelection(obj)
	}
	if selected.Implementation == nil {
		delete(r.parenOverloads, expr)
		return
	}
	if r.parenOverloads == nil {
		r.parenOverloads = make(map[*ast.ParenExpr]types.OverloadSelection)
	}
	r.parenOverloads[expr] = selected
}

// parenthesizedCallIdent identifies the direct callee inside parentheses.
func parenthesizedCallIdent(expr *ast.ParenExpr) *ast.Ident {
	switch callee := astutil.Unparen(expr).(type) {
	case *ast.Ident:
		return callee
	case *ast.SelectorExpr:
		return callee.Sel
	default:
		return nil
	}
}

// overloadObjectWithType finds the unique object with the recorded type.
func overloadObjectWithType(root gotypes.Object, typ gotypes.Type) gotypes.Object {
	var match gotypes.Object
	for obj := range overloadObjects(root) {
		if obj.Type() != typ {
			continue
		}
		if match != nil && match != obj {
			return nil
		}
		match = obj
	}
	return match
}

// overloadObjects visits the group, its wrappers, and its implementations.
func overloadObjects(root gotypes.Object) iter.Seq[gotypes.Object] {
	return func(yield func(gotypes.Object) bool) {
		yieldOverloadObjects(root, yield)
	}
}

// yieldOverloadObjects follows the compiler's objects without flattening wrappers.
func yieldOverloadObjects(obj gotypes.Object, yield func(gotypes.Object) bool) bool {
	switch obj := obj.(type) {
	case *gotypes.Func:
		if obj == nil {
			return true
		}
		if !yield(obj) {
			return false
		}
		var candidates []gotypes.Object
		extension, _ := gogen.CheckFuncEx(obj.Signature())
		switch extension := extension.(type) {
		case *gogen.TyOverloadFunc:
			candidates = extension.Funcs
		case *gogen.TyOverloadMethod:
			candidates = extension.Methods
		case interface{ Obj() gotypes.Object }:
			return yieldOverloadObjects(extension.Obj(), yield)
		}
		for _, candidate := range candidates {
			if !yieldOverloadObjects(candidate, yield) {
				return false
			}
		}
	case *gotypes.Var:
		return yield(obj)
	}
	return true
}

// enclosingOverloadSelection retains a selected implementation through an
// enclosing callback. Compare objects because multiple wrappers can share a leaf.
func enclosingOverloadSelection(group *gotypes.Func, previous gotypes.Object, selected types.OverloadSelection) types.OverloadSelection {
	extension, candidates := gogen.CheckSigFuncExObjects(group.Signature())
	if previous == nil || !slices.Contains(candidates, previous) {
		return types.OverloadSelection{}
	}
	if selected.Implementation == nil {
		selected = xgoutil.ConcreteOverloadSelection(previous)
	}
	if _, typeAsParams := extension.(*gogen.TyTypeAsParams); typeAsParams {
		selected.Binding = group
	}
	return selected
}

// selectOverload publishes a concrete selection in the project type information.
func (r *callTypeRecorder) selectOverload(ident *ast.Ident, selected types.OverloadSelection) {
	if selected.Implementation == nil {
		return
	}
	if r.info.SelectedOverloads == nil {
		r.info.SelectedOverloads = make(map[*ast.Ident]types.OverloadSelection)
	}
	r.info.SelectedOverloads[ident] = selected
}

// recordOverloadSource restores the written group when the recorder retained
// a reconstructed candidate.
func recordOverloadSource(info *types.Info, lookup *xgoutil.OverloadSourceLookup, ident *ast.Ident, receiver gotypes.Type) bool {
	if fun, ok := info.Overloads[ident].(*gotypes.Func); ok {
		if source := lookup.Source(fun, ident.Name, receiver); source != nil {
			info.Overloads[ident] = source
			return true
		}
	}
	return false
}

// recordStaticMethodReceiver records the type use omitted by static method lowering.
func recordStaticMethodReceiver(info *types.Info, decl *ast.FuncDecl) {
	if decl.IsClass || len(decl.Recv.List) == 0 || info.Defs[decl.Name] == nil {
		return
	}
	ident, ok := decl.Recv.List[0].Type.(*ast.Ident)
	if !ok || info.Uses[ident] != nil {
		return
	}
	if owner, ok := info.Pkg.Scope().Lookup(ident.Name).(*gotypes.TypeName); ok {
		info.Uses[ident] = owner
	}
}

// hasInterfaceEmbeddingCycle reports whether typ embeds an interface cycle.
// The memo tracks interfaces being visited as cyclic until all their embedded
// types have been checked. Method signatures are not embedding edges.
func hasInterfaceEmbeddingCycle(typ gotypes.Type, memo map[*gotypes.Interface]bool) bool {
	if typ == nil {
		return false
	}
	iface, ok := typ.Underlying().(*gotypes.Interface)
	if !ok {
		return false
	}
	if cyclic, seen := memo[iface]; seen {
		return cyclic
	}
	memo[iface] = true
	for embedded := range iface.EmbeddedTypes() {
		if hasInterfaceEmbeddingCycle(embedded, memo) {
			return true
		}
	}
	memo[iface] = false
	return false
}

// recordTypeSwitchDeclaration links the variables in each case scope to their
// shared declaration without changing their branch-specific types.
func recordTypeSwitchDeclaration(info *types.Info, stmt *ast.TypeSwitchStmt) {
	assign, ok := stmt.Assign.(*ast.AssignStmt)
	if !ok || assign.Tok != token.DEFINE {
		return
	}
	ident, ok := assign.Lhs[0].(*ast.Ident)
	if !ok || ident.Name == "_" {
		return
	}
	// The parser identifies type switch guards by their single type assertion.
	assertion := assign.Rhs[0].(*ast.TypeAssertExpr)
	typ := info.TypeOf(assertion.X)
	if typ == nil || !gotypes.IsInterface(typ) {
		return
	}
	obj := gotypes.NewVar(ident.Pos(), info.Pkg, ident.Name, typ)
	info.Defs[ident] = obj
	info.ObjToDef[obj] = ident
	for _, clause := range stmt.Body.List {
		if scope := info.Scopes[clause]; scope != nil {
			if variable, ok := scope.Lookup(ident.Name).(*gotypes.Var); ok {
				info.ObjToDef[variable] = ident
			}
		}
	}
}

// TypeInfo retrieves the [types.Info] from the project. The returned [types.Info]
// is nil only if building failed.
//
// NOTE: Both the returned [types.Info] and error can be non-nil, which
// indicates that only part of the project was type checked successfully.
func (p *Project) TypeInfo() (*types.Info, error) {
	cacheIface, err := p.Cache(typeInfoCacheKind{})
	if err != nil {
		return nil, err
	}
	cache := cacheIface.(*typeInfoCache)
	return cache.typeInfo, cache.checkerErr
}
