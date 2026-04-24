package server

import (
	gotypes "go/types"
	"iter"
	"unicode"
	"unicode/utf8"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// kwargNameTarget describes the symbol targeted by a kwarg name in source.
type kwargNameTarget struct {
	ident *ast.Ident
	obj   gotypes.Object
}

// objectAtPosition resolves the identifier, object, and kwarg target at
// position. Kwarg names take precedence over generated identifiers at the same
// source position.
func objectAtPosition(proj *xgo.Project, typeInfo *types.Info, astFile *ast.File, position token.Position) (ident *ast.Ident, obj gotypes.Object, kwargTarget *kwargNameTarget) {
	kwargTarget = kwargNameTargetAtPosition(proj, typeInfo, astFile, position)
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

// kwargNameTargetAtPosition resolves the kwarg target under position if the
// cursor is on a kwarg name.
func kwargNameTargetAtPosition(proj *xgo.Project, typeInfo *types.Info, astFile *ast.File, position token.Position) *kwargNameTarget {
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
		return kwargNameTargetForPath(proj, typeInfo, path, kwargExpr)
	}
	return nil
}

// kwargNameTargetForPath resolves kwargExpr as a kwarg name target within path.
func kwargNameTargetForPath(proj *xgo.Project, typeInfo *types.Info, path []ast.Node, kwargExpr *ast.KwargExpr) *kwargNameTarget {
	var callExpr *ast.CallExpr
	for _, node := range path {
		if node, ok := node.(*ast.CallExpr); ok {
			callExpr = node
			break
		}
	}
	if callExpr == nil {
		return nil
	}

	ident := kwargExpr.Name
	target := lookupCallExprKwargTarget(proj, typeInfo, callExpr, ident.Name)
	if target == nil {
		return nil
	}
	var obj gotypes.Object
	if target.Field != nil {
		obj = target.Field
	} else if target.Method != nil {
		obj = target.Method
	}
	if obj == nil {
		return nil
	}

	return &kwargNameTarget{
		ident: ident,
		obj:   obj,
	}
}

// resolvedCallExprArgs returns call arguments resolved from the callable
// signature or from the matching overloads.
func resolvedCallExprArgs(proj *xgo.Project, typeInfo *types.Info, callExpr *ast.CallExpr) iter.Seq[xgoutil.ResolvedCallExprArg] {
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

		for _, overload := range callExprFuncOverloads(proj, typeInfo, callExpr) {
			if !overloadMatchesCallExpr(typeInfo, callExpr, overload, -1) {
				continue
			}
			for resolvedArg := range resolvedOverloadCallExprArgs(typeInfo, callExpr, overload) {
				if !yield(resolvedArg) {
					return
				}
			}
		}
	}
}

// resolvedOverloadCallExprArgs returns call arguments resolved against one
// matching overload.
func resolvedOverloadCallExprArgs(typeInfo *types.Info, callExpr *ast.CallExpr, overload *gotypes.Func) iter.Seq[xgoutil.ResolvedCallExprArg] {
	return func(yield func(xgoutil.ResolvedCallExprArg) bool) {
		sig := overload.Signature()
		params := sig.Params()
		kwargParamIndex, hasKwargParam := 0, false
		var kwarg *xgoutil.ResolvedCallExprKwarg
		if len(callExpr.Kwargs) > 0 {
			var ok bool
			kwargParamIndex, ok = overloadKwargParamIndex(sig, len(callExpr.Args))
			if !ok {
				return
			}
			param := params.At(kwargParamIndex)
			kwarg = &xgoutil.ResolvedCallExprKwarg{
				Param:                 param,
				ParamIndex:            kwargParamIndex,
				AllowInterfaceTargets: xgoutil.CallExprSupportsInterfaceKwargs(typeInfo, callExpr, param.Type()),
			}
			hasKwargParam = true
		}

		for i, arg := range callExpr.Args {
			paramIndex := i
			if hasKwargParam && i >= kwargParamIndex {
				paramIndex++
			}
			param, paramIndex := overloadCallExprParam(sig, paramIndex)
			if param == nil {
				return
			}
			if !yield(xgoutil.ResolvedCallExprArg{
				Fun:          overload,
				Params:       params,
				Param:        param,
				ParamIndex:   paramIndex,
				Arg:          arg,
				ArgIndex:     i,
				Kind:         xgoutil.ResolvedCallExprArgPositional,
				ExpectedType: overloadCallExprArgType(sig, paramIndex),
			}) {
				return
			}
		}

		if kwarg == nil {
			return
		}
		for i, kwargExpr := range callExpr.Kwargs {
			target := xgoutil.LookupResolvedCallExprKwargTarget(kwarg, kwargExpr.Name.Name)
			var expectedType gotypes.Type
			if target != nil {
				expectedType = target.ValueType
			}
			if !yield(xgoutil.ResolvedCallExprArg{
				Fun:          overload,
				Params:       params,
				Param:        kwarg.Param,
				ParamIndex:   kwarg.ParamIndex,
				Arg:          kwargExpr.Value,
				ArgIndex:     len(callExpr.Args) + i,
				Kind:         xgoutil.ResolvedCallExprArgKeyword,
				Kwarg:        kwargExpr,
				ExpectedType: expectedType,
				KwargTarget:  target,
			}) {
				return
			}
		}
	}
}

// resolveCallExprKwargs returns kwargs resolved from the callable signature or
// its overload set after checking every argument except skipArgIndex.
func resolveCallExprKwargs(proj *xgo.Project, typeInfo *types.Info, callExpr *ast.CallExpr, skipArgIndex int) []*xgoutil.ResolvedCallExprKwarg {
	if kwarg := xgoutil.ResolveCallExprKwarg(typeInfo, callExpr); kwarg != nil {
		return []*xgoutil.ResolvedCallExprKwarg{kwarg}
	}
	var kwargs []*xgoutil.ResolvedCallExprKwarg
	for _, overload := range callExprFuncOverloads(proj, typeInfo, callExpr) {
		if !overloadMatchesCallExpr(typeInfo, callExpr, overload, skipArgIndex) {
			continue
		}
		kwarg := overloadResolvedCallExprKwargForFunc(typeInfo, callExpr, overload)
		if kwarg == nil {
			continue
		}
		kwargs = append(kwargs, kwarg)
	}
	return kwargs
}

// lookupCallExprKwargTarget returns the first resolved target for name at
// callExpr.
func lookupCallExprKwargTarget(proj *xgo.Project, typeInfo *types.Info, callExpr *ast.CallExpr, name string) *xgoutil.ResolvedCallExprKwargTarget {
	targets := lookupCallExprKwargTargets(proj, typeInfo, callExpr, name)
	if len(targets) == 0 {
		return nil
	}
	return targets[0]
}

// lookupCallExprKwargTargets returns every resolved target for name at
// callExpr.
func lookupCallExprKwargTargets(proj *xgo.Project, typeInfo *types.Info, callExpr *ast.CallExpr, name string) []*xgoutil.ResolvedCallExprKwargTarget {
	if kwarg := xgoutil.ResolveCallExprKwarg(typeInfo, callExpr); kwarg != nil {
		target := xgoutil.LookupResolvedCallExprKwargTarget(kwarg, name)
		if target == nil {
			return nil
		}
		return []*xgoutil.ResolvedCallExprKwargTarget{target}
	}
	return lookupOverloadCallExprKwargTargets(proj, typeInfo, callExpr, name)
}

// lookupOverloadCallExprKwargTargets returns kwarg targets from matching
// overloads.
func lookupOverloadCallExprKwargTargets(proj *xgo.Project, typeInfo *types.Info, callExpr *ast.CallExpr, name string) []*xgoutil.ResolvedCallExprKwargTarget {
	var targets []*xgoutil.ResolvedCallExprKwargTarget
	for _, overload := range callExprFuncOverloads(proj, typeInfo, callExpr) {
		kwarg := overloadResolvedCallExprKwargForFunc(typeInfo, callExpr, overload)
		if kwarg == nil {
			continue
		}
		target := xgoutil.LookupResolvedCallExprKwargTarget(kwarg, name)
		if target == nil || !overloadMatchesCallExpr(typeInfo, callExpr, overload, -1) {
			continue
		}
		targets = append(targets, target)
	}
	return targets
}

// callExprFuncOverloads returns overloads available at callExpr.
func callExprFuncOverloads(proj *xgo.Project, typeInfo *types.Info, callExpr *ast.CallExpr) []*gotypes.Func {
	if fun := xgoutil.FuncFromCallExpr(typeInfo, callExpr); fun != nil {
		if overloads := xgoutil.ExpandXGoOverloadableFunc(fun); len(overloads) > 0 {
			return overloads
		}
	}
	funIdent := callExprFunIdent(callExpr)
	if funIdent == nil {
		return nil
	}
	return getFuncOverloads(proj, funIdent)
}

// objectDefinitionLocation returns the declaration location of obj when it is
// available in the current project.
func (s *Server) objectDefinitionLocation(proj *xgo.Project, typeInfo *types.Info, obj gotypes.Object) *Location {
	defIdent := typeInfo.ObjToDef[obj]
	if defIdent != nil {
		if xgoutil.NodeTokenFile(proj.Fset, defIdent) == nil {
			return nil
		}
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
func (s *Server) kwargReferenceLocations(result *compileResult, obj gotypes.Object) []Location {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	astPkg, _ := result.proj.ASTPackage()
	if astPkg == nil {
		return nil
	}

	var locations []Location
	for _, astFile := range astPkg.Files {
		ast.Inspect(astFile, func(node ast.Node) bool {
			callExpr, ok := node.(*ast.CallExpr)
			if !ok || len(callExpr.Kwargs) == 0 {
				return true
			}

			for _, kwarg := range callExpr.Kwargs {
				for _, target := range lookupCallExprKwargTargets(result.proj, typeInfo, callExpr, kwarg.Name.Name) {
					if !kwargTargetMatchesObject(target, obj) {
						continue
					}
					locations = append(locations, s.locationForNode(result.proj, kwarg.Name))
				}
			}
			return true
		})
	}
	return locations
}

// kwargTargetMatchesObject reports whether target resolves to obj.
func kwargTargetMatchesObject(target *xgoutil.ResolvedCallExprKwargTarget, obj gotypes.Object) bool {
	if target.Field != nil {
		return target.Field == obj
	}
	if target.Method != nil {
		return target.Method == obj
	}
	return false
}

// kwargRenameText returns the canonical kwarg spelling for renaming obj.
func kwargRenameText(obj gotypes.Object, newName string) string {
	if newName == "" {
		return ""
	}
	if _, ok := obj.(*gotypes.Func); ok {
		return lowerFirstASCII(newName)
	}
	r, size := utf8.DecodeRuneInString(newName)
	return string(unicode.ToLower(r)) + newName[size:]
}

// kwargDefinitionRenameText returns the declaration spelling for a rename that
// starts from a kwarg name.
func kwargDefinitionRenameText(obj gotypes.Object, newName string) string {
	if newName == "" {
		return ""
	}
	if _, ok := obj.(*gotypes.Func); ok {
		return upperFirstASCII(newName)
	}
	if obj.Exported() {
		r, size := utf8.DecodeRuneInString(newName)
		return string(unicode.ToUpper(r)) + newName[size:]
	}
	return newName
}

// upperFirstASCII uppercases the first ASCII letter in name.
func upperFirstASCII(name string) string {
	if name == "" {
		return ""
	}
	first := name[0]
	if first < 'a' || first > 'z' {
		return name
	}
	return string(first-('a'-'A')) + name[1:]
}

// lowerFirstASCII lowercases the first ASCII letter in name.
func lowerFirstASCII(name string) string {
	if name == "" {
		return ""
	}
	first := name[0]
	if first < 'A' || first > 'Z' {
		return name
	}
	return string(first+('a'-'A')) + name[1:]
}
