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
	ident            *ast.Ident
	obj              gotypes.Object
	selectorTypeName string
}

// callExprKwargTarget retains the selector type name for a resolved kwarg.
// A field object alone cannot distinguish types sharing an underlying struct.
type callExprKwargTarget struct {
	target           *xgoutil.ResolvedCallExprKwargTarget
	selectorTypeName string
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
		return kwargNameTargetForPath(typeInfo, path, kwargExpr)
	}
	return nil
}

// kwargNameTargetForPath resolves kwargExpr as a kwarg name target within path.
func kwargNameTargetForPath(typeInfo *types.Info, path []ast.Node, kwargExpr *ast.KwargExpr) *kwargNameTarget {
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
	targets := lookupCallExprKwargTargets(typeInfo, callExpr, ident.Name)
	if len(targets) == 0 {
		return nil
	}
	target := targets[0]
	obj := kwargTargetObject(target.target)
	if obj == nil {
		return nil
	}

	return &kwargNameTarget{
		ident:            ident,
		obj:              obj,
		selectorTypeName: target.selectorTypeName,
	}
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

		for _, overload := range callExprFuncOverloads(typeInfo, callExpr) {
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
		sig, params := xgoutil.ResolveFuncSignatureForCall(typeInfo, callExpr, overload)
		if sig == nil || params == nil {
			return
		}
		var kwarg *xgoutil.ResolvedCallExprKwarg
		if len(callExpr.Kwargs) > 0 {
			kwarg = resolvedCallExprKwargAtArgCount(typeInfo, callExpr, sig, params, len(callExpr.Args))
			if kwarg == nil {
				return
			}
		}

		for i, arg := range callExpr.Args {
			paramIndex := i
			if kwarg != nil && i >= kwarg.ParamIndex {
				paramIndex++
			}
			param, paramIndex := callExprParam(sig, params, paramIndex)
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
				ExpectedType: callExprArgType(sig, params, paramIndex),
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

// resolveCallExprKwargsAtArgCount returns kwargs resolved as if only argCount
// positional arguments appeared before kwargs.
func resolveCallExprKwargsAtArgCount(typeInfo *types.Info, callExpr *ast.CallExpr, argCount, skipArgIndex int) []*xgoutil.ResolvedCallExprKwarg {
	if argCount == len(callExpr.Args) {
		if kwarg := xgoutil.ResolveCallExprKwarg(typeInfo, callExpr); kwarg != nil {
			return []*xgoutil.ResolvedCallExprKwarg{kwarg}
		}
	}

	overloads := callExprFuncOverloads(typeInfo, callExpr)
	if len(overloads) == 0 {
		_, sig, params := xgoutil.ResolveCallExprSignature(typeInfo, callExpr)
		if sig == nil || params == nil {
			return nil
		}
		kwarg := resolvedCallExprKwargAtArgCount(typeInfo, callExpr, sig, params, argCount)
		if kwarg != nil && callExprMatchesKwargParams(typeInfo, callExpr, sig, params, kwarg, skipArgIndex) {
			return []*xgoutil.ResolvedCallExprKwarg{kwarg}
		}
		return nil
	}

	var kwargs []*xgoutil.ResolvedCallExprKwarg
	for _, overload := range overloads {
		sig, params := xgoutil.ResolveFuncSignatureForCall(typeInfo, callExpr, overload)
		if sig == nil || params == nil {
			continue
		}
		kwarg := resolvedCallExprKwargAtArgCount(typeInfo, callExpr, sig, params, argCount)
		if kwarg == nil {
			continue
		}
		if !callExprMatchesKwargParams(typeInfo, callExpr, sig, params, kwarg, skipArgIndex) {
			continue
		}
		kwargs = append(kwargs, kwarg)
	}
	return kwargs
}

// callExprMatchesKwargParams reports whether callExpr remains viable after
// checking every argument except skipArgIndex against params.
func callExprMatchesKwargParams(typeInfo *types.Info, callExpr *ast.CallExpr, sig *gotypes.Signature, params *gotypes.Tuple, kwarg *xgoutil.ResolvedCallExprKwarg, skipArgIndex int) bool {
	for i, argExpr := range callExpr.Args {
		if i == skipArgIndex {
			continue
		}
		paramIndex := i
		if i >= kwarg.ParamIndex {
			paramIndex++
		}
		expectedType := callExprArgType(sig, params, paramIndex)
		if expectedType == nil || !formatArgMatchesType(typeInfo, argExpr, expectedType) {
			return false
		}
	}

	for i, kwargExpr := range callExpr.Kwargs {
		globalIndex := len(callExpr.Args) + i
		if globalIndex == skipArgIndex {
			continue
		}
		target := xgoutil.LookupResolvedCallExprKwargTarget(kwarg, kwargExpr.Name.Name)
		if target == nil || !formatArgMatchesType(typeInfo, kwargExpr.Value, target.ValueType) {
			return false
		}
	}
	return true
}

// lookupCallExprKwargTargets returns every resolved target for name at
// callExpr.
func lookupCallExprKwargTargets(typeInfo *types.Info, callExpr *ast.CallExpr, name string) []callExprKwargTarget {
	if kwarg := xgoutil.ResolveCallExprKwarg(typeInfo, callExpr); kwarg != nil {
		target := xgoutil.LookupResolvedCallExprKwargTarget(kwarg, name)
		if target == nil {
			return nil
		}
		return []callExprKwargTarget{{target: target, selectorTypeName: kwargSelectorTypeName(kwarg)}}
	}
	return lookupOverloadCallExprKwargTargets(typeInfo, callExpr, name)
}

// lookupOverloadCallExprKwargTargets returns kwarg targets from matching
// overloads.
func lookupOverloadCallExprKwargTargets(typeInfo *types.Info, callExpr *ast.CallExpr, name string) []callExprKwargTarget {
	var targets []callExprKwargTarget
	for _, overload := range callExprFuncOverloads(typeInfo, callExpr) {
		sig, params := xgoutil.ResolveFuncSignatureForCall(typeInfo, callExpr, overload)
		if sig == nil || params == nil {
			continue
		}
		kwarg := resolvedCallExprKwargAtArgCount(typeInfo, callExpr, sig, params, len(callExpr.Args))
		if kwarg == nil {
			continue
		}
		target := xgoutil.LookupResolvedCallExprKwargTarget(kwarg, name)
		if target == nil || !overloadMatchesCallExpr(typeInfo, callExpr, overload, -1) {
			continue
		}
		targets = append(targets, callExprKwargTarget{target: target, selectorTypeName: kwargSelectorTypeName(kwarg)})
	}
	return targets
}

// callExprFuncOverloads returns overloads available at callExpr. The recorded
// declaration preserves all candidates after type checking selects one member.
func callExprFuncOverloads(typeInfo *types.Info, callExpr *ast.CallExpr) []*gotypes.Func {
	funIdent := callExprFunIdent(callExpr)
	if funIdent == nil {
		return nil
	}
	fun, _ := typeInfo.Overloads[funIdent].(*gotypes.Func)
	if fun == nil {
		fun = xgoutil.FuncFromCallExpr(typeInfo, callExpr)
	}
	if fun == nil {
		return nil
	}
	return xgoutil.ExpandXGoOverloadableFunc(fun)
}

// objectDefinitionLocation returns the declaration location of obj when it is
// available in the current project.
func (s *Server) objectDefinitionLocation(proj *xgo.Project, typeInfo *types.Info, obj gotypes.Object) *Location {
	if obj.Pkg() != typeInfo.Pkg {
		return nil
	}
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
func (s *Server) kwargReferenceLocations(proj *xgo.Project, obj gotypes.Object) []Location {
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	astPkg, _ := proj.ASTPackage()
	if astPkg == nil {
		return nil
	}

	var locations []Location
	for _, astFile := range astPkg.Files {
		ast.Inspect(astFile, func(node ast.Node) bool {
			callExpr, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}

			for _, kwarg := range callExpr.Kwargs {
				for _, target := range lookupCallExprKwargTargets(typeInfo, callExpr, kwarg.Name.Name) {
					if !kwargTargetMatchesObject(target.target, obj) {
						continue
					}
					locations = append(locations, s.locationForNode(proj, kwarg.Name))
				}
			}
			return true
		})
	}
	return locations
}

// kwargTargetMatchesObject reports whether target resolves to obj.
func kwargTargetMatchesObject(target *xgoutil.ResolvedCallExprKwargTarget, obj gotypes.Object) bool {
	targetObj := kwargTargetObject(target)
	return targetObj != nil && targetObj == obj
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
