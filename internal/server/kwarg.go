package server

import (
	gotypes "go/types"
	"iter"
	"slices"
	"unicode"
	"unicode/utf8"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// kwargNameTarget describes a source keyword and its declaration, when present.
type kwargNameTarget struct {
	ident    *ast.Ident
	obj      gotypes.Object
	receiver gotypes.Type
}

// callExprKwargTarget pairs a keyword's declaration with its call binding.
type callExprKwargTarget struct {
	target  *xgoutil.ResolvedCallExprKwargTarget
	binding *xgoutil.ResolvedCallExprKwarg
}

// objectAtPosition resolves the identifier, object, and kwarg target at
// position. Kwarg names take precedence over generated identifiers at the same
// source position.
func objectAtPosition(proj *xgo.Project, typeInfo *types.Info, astFile *ast.File, position token.Position) (ident *ast.Ident, obj gotypes.Object, kwargTarget *kwargNameTarget) {
	kwargTarget = kwargNameTargetAtPosition(proj, astFile, position)
	if kwargTarget != nil {
		return kwargTarget.ident, kwargTarget.obj, kwargTarget
	}

	ident = xgoutil.IdentAtPosition(proj.Fset, typeInfo, astFile, position)
	if ident != nil {
		obj = typeInfo.ObjectOf(ident)
		if obj != nil {
			return ident, obj, nil
		}
	}
	return
}

// sourceObjectAtPosition resolves the named source symbol, retaining an
// overload declaration instead of its selected implementation.
func sourceObjectAtPosition(proj *xgo.Project, info *types.Info, file *ast.File, position token.Position) (ident *ast.Ident, obj gotypes.Object, kwargTarget *kwargNameTarget) {
	ident, obj, kwargTarget = objectAtPosition(proj, info, file, position)
	if ident != nil && kwargTarget == nil {
		obj = info.SourceObjectOf(ident)
	}
	return
}

// kwargNameTargetAtPosition resolves the kwarg target under position if the
// cursor is on a kwarg name.
func kwargNameTargetAtPosition(proj *xgo.Project, astFile *ast.File, position token.Position) *kwargNameTarget {
	tokenFile := xgoutil.NodeTokenFile(proj.Fset, astFile)
	pos := tokenFile.Pos(position.Offset)

	path, _ := xgoutil.PathEnclosingInterval(astFile, pos, pos)
	for _, node := range path {
		kwargExpr, ok := node.(*ast.KwargExpr)
		if !ok {
			continue
		}
		if pos < kwargExpr.Name.Pos() || pos > kwargExpr.Name.End() {
			return nil
		}
		typeInfo, _ := expressionTypeInfo(proj)
		if typeInfo == nil {
			return nil
		}
		return kwargNameTargetForPath(typeInfo, path, kwargExpr)
	}
	return nil
}

// kwargNameTargetForPath resolves kwargExpr as a kwarg name target within path.
func kwargNameTargetForPath(typeInfo *types.Info, path []ast.Node, kwargExpr *ast.KwargExpr) *kwargNameTarget {
	result := &kwargNameTarget{ident: kwargExpr.Name}
	for _, node := range path {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			continue
		}
		if index := slices.Index(call.Kwargs, kwargExpr); index >= 0 {
			for _, targets := range callExprKwargTargets(typeInfo, call, index) {
				if len(targets) != 0 {
					result.obj = kwargTargetObject(targets[0].target)
					result.receiver = targets[0].binding.ContainerType()
				}
				break
			}
		}
		break
	}
	return result
}

// resolvedCallExprArgs returns call arguments resolved from the callable
// signature or from the matching overloads.
func resolvedCallExprArgs(typeInfo *types.Info, callExpr *ast.CallExpr) iter.Seq[xgoutil.ResolvedCallExprArg] {
	return func(yield func(xgoutil.ResolvedCallExprArg) bool) {
		hasResolvedArgs := false
		for resolvedArg := range xgoutil.ResolvedCallExprArgs(typeInfo, callExpr) {
			hasResolvedArgs = true
			if !yield(resolvedArg) {
				return
			}
		}
		if hasResolvedArgs {
			return
		}

		for _, overload := range callExprOverloads(typeInfo, callExpr) {
			bound, _, matches := overload.matchArgs(typeInfo, callExpr, nil)
			if !matches {
				continue
			}
			for resolvedArg := range bound.args(typeInfo, callExpr) {
				if !yield(resolvedArg) {
					return
				}
			}
		}
	}
}

// resolveCallExprKwargsAtArgCount returns kwargs resolved as if only argCount
// positional arguments appeared before kwargs.
func resolveCallExprKwargsAtArgCount(typeInfo *types.Info, callExpr *ast.CallExpr, argCount, skipArgIndex int) []*xgoutil.ResolvedCallExprKwarg {
	// Editing the arguments can select a different overload.
	overloads := callExprOverloads(typeInfo, callExpr)
	if len(overloads) < 2 && argCount == len(callExpr.Args) {
		if kwarg := xgoutil.ResolveCallExprKwarg(typeInfo, callExpr); kwarg != nil {
			return []*xgoutil.ResolvedCallExprKwarg{kwarg}
		}
	}

	if len(overloads) == 0 {
		fun, sig, params := xgoutil.ResolveCallExprSignature(typeInfo, callExpr)
		if sig == nil {
			return nil
		}
		overloads = []callOverload{{object: fun, signature: sig, params: params}}
	}

	var kwargs []*xgoutil.ResolvedCallExprKwarg
	for _, overload := range overloads {
		bound, ok := overload.bindTypeArgs(typeInfo, callExpr, skipArgIndex)
		if !ok {
			continue
		}
		sig, params := bound.signature, bound.params
		kwarg := xgoutil.ResolveCallExprKwargAtArgCount(typeInfo, callExpr, sig, params, argCount)
		if kwarg != nil && bound.matchesKwargs(typeInfo, callExpr, kwarg, skipArgIndex) {
			kwargs = append(kwargs, kwarg)
		}
	}
	return kwargs
}

// matchesKwargs reports whether callExpr fits the candidate's parameters with
// a keyword parameter inserted and the edited argument ignored.
func (o callOverload) matchesKwargs(typeInfo *types.Info, callExpr *ast.CallExpr, kwarg *xgoutil.ResolvedCallExprKwarg, skipArgIndex int) bool {
	typeArgCount := xgoutil.NormalizedCallExprTypeArgCount(o.function(), o.params)
	if kwarg.ParamIndex < typeArgCount {
		return false
	}
	for i := typeArgCount; i < len(callExpr.Args); i++ {
		if i == skipArgIndex {
			continue
		}
		paramIndex := i
		if i >= kwarg.ParamIndex {
			paramIndex++
		}
		expectedType := callExprArgType(o.signature, o.params, paramIndex)
		if !callValueArgMatchesType(typeInfo, callExpr.Args[i], expectedType, nil) {
			return false
		}
	}

	binding, previous := kwarg, 0
	for i, kwargExpr := range callExpr.Kwargs {
		globalIndex := len(callExpr.Args) + i
		if globalIndex == skipArgIndex {
			continue
		}
		binding = binding.AfterKeywords(callExpr.Kwargs[previous:i])
		previous = i
		if binding == nil && skipArgIndex >= len(callExpr.Args) && skipArgIndex < globalIndex {
			// Changing an earlier keyword can change the receiver of this step.
			continue
		}
		target := binding.KeywordTarget(kwargExpr)
		if target == nil || !callValueArgMatchesType(typeInfo, kwargExpr.Value, target.ValueType, target) {
			return false
		}
	}
	return true
}

// callExprKwargTargets yields written keywords and their targets starting at
// first. Each candidate advances through a builder chain once per traversal.
func callExprKwargTargets(typeInfo *types.Info, callExpr *ast.CallExpr, first int) iter.Seq2[*ast.KwargExpr, []callExprKwargTarget] {
	return func(yield func(*ast.KwargExpr, []callExprKwargTarget) bool) {
		if first >= len(callExpr.Kwargs) {
			return
		}
		bindings := callExprKwargBindings(typeInfo, callExpr)
		previous := 0
		for index := first; index < len(callExpr.Kwargs); index++ {
			arg := callExpr.Kwargs[index]
			var targets []callExprKwargTarget
			for i, binding := range bindings {
				binding = binding.AfterKeywords(callExpr.Kwargs[previous:index])
				bindings[i] = binding
				if target := binding.KeywordTarget(arg); target != nil {
					targets = append(targets, callExprKwargTarget{target: target, binding: binding})
				}
			}
			if !yield(arg, targets) {
				return
			}
			previous = index
		}
	}
}

// callExprKwargBindings uses the checked keyword container when available.
// For an unresolved call, consider the overloads that fit the arguments.
func callExprKwargBindings(typeInfo *types.Info, callExpr *ast.CallExpr) []*xgoutil.ResolvedCallExprKwarg {
	if kwarg := xgoutil.ResolveCallExprKwarg(typeInfo, callExpr); kwarg != nil {
		return []*xgoutil.ResolvedCallExprKwarg{kwarg}
	}
	var bindings []*xgoutil.ResolvedCallExprKwarg
	for _, overload := range callExprOverloads(typeInfo, callExpr) {
		bound, _, matches := overload.matchArgs(typeInfo, callExpr, nil)
		if !matches {
			continue
		}
		if kwarg := xgoutil.ResolveCallExprKwargAtArgCount(typeInfo, callExpr, bound.signature, bound.params, len(callExpr.Args)); kwarg != nil {
			bindings = append(bindings, kwarg)
		}
	}
	return bindings
}

// objectDefinitionLocation returns the declaration location of obj when it is
// available in the current project.
func (s *Server) objectDefinitionLocation(proj *xgo.Project, typeInfo *types.Info, obj gotypes.Object) *Location {
	obj = typeInfo.ObjectDeclaration(obj)
	if obj.Pkg() != typeInfo.Pkg {
		return nil
	}
	if defIdent := typeInfo.ObjToDef[obj]; hasSourceSpelling(proj, defIdent) {
		loc := s.locationForNode(proj, defIdent)
		return &loc
	}

	if !obj.Pos().IsValid() || xgoutil.PosTokenFile(proj.Fset, obj.Pos()) == nil {
		return nil
	}
	loc := s.locationForPos(proj, obj.Pos())
	return &loc
}

// kwargReferenceLocations returns all kwarg-name locations that resolve to obj.
func (s *Server) kwargReferenceLocations(proj *xgo.Project, obj gotypes.Object) []Location {
	source, err := sourceInfoForProject(proj)
	if err != nil {
		return nil
	}
	refs := source.kwargs[types.ObjectOrigin(obj)]
	locations := make([]Location, 0, len(refs))
	for _, ref := range refs {
		locations = append(locations, s.locationForNode(proj, ref.ident))
	}
	return locations
}

// kwargTargetObject returns the field or method resolved by target.
func kwargTargetObject(target *xgoutil.ResolvedCallExprKwargTarget) gotypes.Object {
	if target == nil {
		return nil
	}
	if target.Field != nil {
		return target.Field
	}
	if target.Method != nil {
		return target.Method
	}
	return nil
}

// kwargRenameText returns the source-level keyword spelling for a renamed
// field or method.
func kwargRenameText(obj gotypes.Object, spellingType gotypes.Type, newName string) string {
	if field, ok := obj.(*gotypes.Var); ok {
		return xgoutil.RenamedStructKwargName(spellingType, field, newName)
	}
	return xgoutil.FunctionAliasName(newName)
}

// kwargDefinitionRenameText returns the declaration spelling for a rename that
// starts from a kwarg name.
func kwargDefinitionRenameText(obj gotypes.Object, newName string) string {
	if newName == "" {
		return ""
	}
	if _, ok := obj.(*gotypes.Func); ok {
		return xgoutil.UpperFirstASCII(newName)
	}
	if obj.Exported() {
		r, size := utf8.DecodeRuneInString(newName)
		return string(unicode.ToUpper(r)) + newName[size:]
	}
	return newName
}
