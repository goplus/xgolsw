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

package xgoutil

import (
	gotypes "go/types"
	"iter"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/types"
)

const (
	// xgoOptionalParamPrefix prefixes generated names for current XGo optional parameters.
	xgoOptionalParamPrefix = "__xgo_optional_"

	// gopOptionalParamPrefix prefixes generated names for legacy XGo optional parameters.
	gopOptionalParamPrefix = "__gop_optional_"
)

// ResolvedCallExprArgKind describes how an argument is spelled in source.
type ResolvedCallExprArgKind int

const (
	// ResolvedCallExprArgPositional identifies a positional call argument.
	ResolvedCallExprArgPositional ResolvedCallExprArgKind = iota
	// ResolvedCallExprArgKeyword identifies a keyword call argument value.
	ResolvedCallExprArgKeyword
)

// ResolvedCallExprKwargTargetKind describes how a keyword argument name maps to
// the target parameter container.
type ResolvedCallExprKwargTargetKind int

const (
	// ResolvedCallExprKwargTargetUnknown is the zero-value sentinel for a
	// target kind. Unresolved keyword arguments use a nil target.
	ResolvedCallExprKwargTargetUnknown ResolvedCallExprKwargTargetKind = iota
	// ResolvedCallExprKwargTargetMap identifies a string-keyed map target.
	ResolvedCallExprKwargTargetMap
	// ResolvedCallExprKwargTargetStructField identifies a struct field target.
	ResolvedCallExprKwargTargetStructField
	// ResolvedCallExprKwargTargetInterfaceMethod identifies a self-returning
	// interface method target.
	ResolvedCallExprKwargTargetInterfaceMethod
	// ResolvedCallExprKwargTargetInterfaceSet identifies a dynamic Set method
	// target on a self-returning interface.
	ResolvedCallExprKwargTargetInterfaceSet
)

// ResolvedCallExprArg describes a call argument after XGo-specific mapping.
type ResolvedCallExprArg struct {
	Fun        *gotypes.Func
	Params     *gotypes.Tuple
	Param      *gotypes.Var
	ParamIndex int
	Arg        ast.Expr
	// ArgIndex is the index in the resolved source argument stream. For
	// positional arguments, it is the index in expr.Args. For keyword
	// arguments, it is len(expr.Args) plus the index in expr.Kwargs.
	ArgIndex int
	Kind     ResolvedCallExprArgKind
	Kwarg    *ast.KwargExpr
	// ExpectedType is nil for a keyword argument whose name cannot be resolved
	// against its kwarg target parameter.
	ExpectedType gotypes.Type
	// KwargTarget is non-nil only for resolved keyword arguments.
	KwargTarget *ResolvedCallExprKwargTarget
}

// ResolvedCallExprKwarg describes the parameter slot that receives kwargs.
type ResolvedCallExprKwarg struct {
	Param                 *gotypes.Var
	ParamIndex            int
	AllowInterfaceTargets bool
}

// ResolvedCallExprKwargTarget describes a resolved keyword argument binding.
type ResolvedCallExprKwargTarget struct {
	Kind      ResolvedCallExprKwargTargetKind
	Name      string
	ValueType gotypes.Type
	Field     *gotypes.Var
	Method    *gotypes.Func
}

// IsTypeArg reports whether arg represents an XGox type-as-parameter argument.
func (arg ResolvedCallExprArg) IsTypeArg() bool {
	if arg.Kind != ResolvedCallExprArgPositional {
		return false
	}
	if !IsMarkedAsXGoPackage(arg.Fun.Pkg()) {
		return false
	}
	_, methodName, ok := SplitXGotMethodName(arg.Fun.Name(), false)
	if !ok {
		return false
	}
	if _, ok := SplitXGoxFuncName(methodName); !ok {
		return false
	}
	typeParams := arg.Fun.Signature().TypeParams()
	return typeParams != nil && arg.ParamIndex < typeParams.Len()
}

// CreateCallExprFromBranchStmt attempts to create a call expression from a
// branch statement. This handles cases in spx where the `Sprite.Goto` method is
// intended to precede the goto statement.
func CreateCallExprFromBranchStmt(typeInfo *types.Info, stmt *ast.BranchStmt) *ast.CallExpr {
	if typeInfo == nil || stmt == nil {
		return nil
	}
	if stmt.Tok != token.GOTO {
		// Currently, we only need to handle goto statements.
		return nil
	}

	// Skip if this is a real branch statement with an actual label object.
	if obj := typeInfo.ObjectOf(stmt.Label); obj == nil {
		return nil
	} else if _, ok := obj.(*gotypes.Label); ok {
		return nil
	}

	// Performance note: This requires traversing the typeInfo.Uses map to locate
	// the function object, which is unavoidable since the AST still treats this
	// node as a branch statement rather than a call expression.
	stmtTokEnd := stmt.TokPos + token.Pos(len(stmt.Tok.String()))
	for ident, obj := range typeInfo.Uses {
		if ident.Pos() == stmt.TokPos && ident.End() == stmtTokEnd {
			if _, ok := obj.(*gotypes.Func); ok {
				return &ast.CallExpr{
					Fun:  ident,
					Args: []ast.Expr{stmt.Label},
				}
			}
			break
		}
	}
	return nil
}

// FuncFromCallExpr returns the function object from a call expression.
func FuncFromCallExpr(typeInfo *types.Info, expr *ast.CallExpr) *gotypes.Func {
	if typeInfo == nil || expr == nil {
		return nil
	}

	var ident *ast.Ident
	switch fun := expr.Fun.(type) {
	case *ast.Ident:
		ident = fun
	case *ast.SelectorExpr:
		ident = fun.Sel
	default:
		return nil
	}

	obj := typeInfo.ObjectOf(ident)
	if obj == nil {
		return nil
	}
	fun, _ := obj.(*gotypes.Func)
	return fun
}

// ResolveCallExprSignature resolves the callable function, its signature, and
// the normalized parameter list for expr.
func ResolveCallExprSignature(typeInfo *types.Info, expr *ast.CallExpr) (fun *gotypes.Func, sig *gotypes.Signature, params *gotypes.Tuple) {
	if typeInfo == nil || expr == nil {
		return nil, nil, nil
	}

	fun = FuncFromCallExpr(typeInfo, expr)
	if fun == nil {
		return nil, nil, nil
	}

	sig, params = ResolveFuncSignature(fun)
	if sig == nil {
		return nil, nil, nil
	}
	return fun, sig, params
}

// ResolveFuncSignature resolves the callable signature and normalized
// parameter list for fun.
func ResolveFuncSignature(fun *gotypes.Func) (sig *gotypes.Signature, params *gotypes.Tuple) {
	sig = fun.Signature()
	if _, ok := gogen.CheckFuncEx(sig); ok {
		return nil, nil
	}

	return sig, normalizedCallExprParams(fun, sig)
}

// normalizedCallExprParams returns the parameter list that should be exposed to
// callers after applying XGo-specific function normalization.
func normalizedCallExprParams(fun *gotypes.Func, sig *gotypes.Signature) *gotypes.Tuple {
	params := sig.Params()
	if !IsMarkedAsXGoPackage(fun.Pkg()) {
		return params
	}

	_, methodName, ok := SplitXGotMethodName(fun.Name(), false)
	if !ok {
		return params
	}

	var vars []*gotypes.Var
	if _, ok := SplitXGoxFuncName(methodName); ok {
		typeParams := sig.TypeParams()
		if typeParams != nil {
			vars = slices.Grow(vars, typeParams.Len())
			for typeParam := range typeParams.TypeParams() {
				param := gotypes.NewParam(token.NoPos, typeParam.Obj().Pkg(), typeParam.Obj().Name(), typeParam.Constraint().Underlying())
				vars = append(vars, param)
			}
		}
	}

	vars = slices.Grow(vars, params.Len()-1)
	for i := 1; i < params.Len(); i++ {
		vars = append(vars, params.At(i))
	}
	return gotypes.NewTuple(vars...)
}

// resolvedCallExprArgType returns the expected argument type at paramIndex.
// It unwraps variadic slices to their element type unless the source argument
// uses ellipsis.
func resolvedCallExprArgType(sig *gotypes.Signature, params *gotypes.Tuple, paramIndex int, ellipsis bool) gotypes.Type {
	param := params.At(paramIndex)
	if sig.Variadic() && paramIndex == params.Len()-1 && !ellipsis {
		return variadicValueType(param.Type())
	}
	return param.Type()
}

// variadicValueType returns the per-argument type for a variadic parameter.
func variadicValueType(typ gotypes.Type) gotypes.Type {
	if sliceType, ok := typ.(*gotypes.Slice); ok {
		return sliceType.Elem()
	}
	return typ
}

// SourceParamName returns the source-facing spelling of param.
func SourceParamName(param *gotypes.Var) string {
	name, _ := trimOptionalParamPrefix(param.Name())
	return name
}

// isOptionalParam reports whether param is an XGo optional parameter.
func isOptionalParam(typeInfo *types.Info, param *gotypes.Var) bool {
	if _, ok := trimOptionalParamPrefix(param.Name()); ok {
		return true
	}
	if typeInfo == nil {
		return false
	}

	defIdent := typeInfo.ObjToDef[param]
	if defIdent == nil || defIdent.Obj == nil {
		return false
	}
	field, ok := defIdent.Obj.Decl.(*ast.Field)
	return ok && field.Optional.IsValid()
}

// trimOptionalParamPrefix returns the source-facing parameter name after
// removing a generated XGo optional parameter prefix.
func trimOptionalParamPrefix(name string) (string, bool) {
	if name, ok := strings.CutPrefix(name, xgoOptionalParamPrefix); ok {
		return name, true
	}
	if name, ok := strings.CutPrefix(name, gopOptionalParamPrefix); ok {
		return name, true
	}
	return name, false
}

// anyType returns the predeclared `any` type.
func anyType() gotypes.Type {
	return gotypes.Universe.Lookup("any").Type()
}

// upperFirstRune uppercases the first rune in name.
func upperFirstRune(name string) string {
	if name == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(r)) + name[size:]
}

// lowerFirstRune lowercases the first rune in name.
func lowerFirstRune(name string) string {
	if name == "" {
		return ""
	}
	r, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToLower(r)) + name[size:]
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

// kwargSuggestedName returns the source-level keyword name for a target member.
func kwargSuggestedName(name string, exported bool) string {
	if !exported {
		return name
	}
	return lowerFirstRune(name)
}

// lookupStructKwargField resolves name to a struct field that can be addressed
// by kwargs.
func lookupStructKwargField(strct *gotypes.Struct, inMainPkg bool, name string) *gotypes.Var {
	capName := upperFirstRune(name)
	for field := range strct.Fields() {
		if !IsExportedOrInMainPkg(field) {
			continue
		}
		if inMainPkg && field.Name() == name {
			return field
		}
		if field.Exported() && field.Name() == capName {
			return field
		}
	}
	return nil
}

// structKwargType returns the struct and main-package matching mode for typ.
func structKwargType(typ gotypes.Type) (*gotypes.Struct, bool) {
	if ptr, ok := typ.Underlying().(*gotypes.Pointer); ok {
		typ = ptr.Elem()
	}
	strct, ok := typ.Underlying().(*gotypes.Struct)
	if !ok {
		return nil, false
	}
	named, _ := typ.(*gotypes.Named)
	return strct, named != nil && IsInMainPkg(named.Obj())
}

// structKwargTarget returns a target for field.
func structKwargTarget(field *gotypes.Var) ResolvedCallExprKwargTarget {
	return ResolvedCallExprKwargTarget{
		Kind:      ResolvedCallExprKwargTargetStructField,
		Name:      kwargSuggestedName(field.Name(), field.Exported()),
		ValueType: field.Type(),
		Field:     field,
	}
}

// lookupInterfaceKwargMethodTarget resolves name to a self-returning interface
// method target.
func lookupInterfaceKwargMethodTarget(iface *gotypes.Interface, self *gotypes.Named, name string) *ResolvedCallExprKwargTarget {
	methodName := upperFirstASCII(name)
	for method := range iface.Methods() {
		if method.Name() != methodName {
			continue
		}
		target, ok := interfaceKwargMethodTarget(method, self)
		if ok {
			return &target
		}
	}
	return nil
}

// interfaceKwargMethodTarget returns the kwarg target represented by method
// when it is a self-returning interface method.
func interfaceKwargMethodTarget(method *gotypes.Func, self *gotypes.Named) (ResolvedCallExprKwargTarget, bool) {
	sig := method.Signature()
	if sig.Params().Len() != 1 || sig.Results().Len() != 1 {
		return ResolvedCallExprKwargTarget{}, false
	}
	if !gotypes.Identical(sig.Results().At(0).Type(), self) {
		return ResolvedCallExprKwargTarget{}, false
	}
	valueType := sig.Params().At(0).Type()
	if sig.Variadic() {
		valueType = variadicValueType(valueType)
	}
	return ResolvedCallExprKwargTarget{
		Kind:      ResolvedCallExprKwargTargetInterfaceMethod,
		Name:      lowerFirstASCII(method.Name()),
		ValueType: valueType,
		Method:    method,
	}, true
}

// hasInterfaceKwargSet reports whether iface exposes a `Set(string, any) Self`
// fallback for kwargs.
func hasInterfaceKwargSet(iface *gotypes.Interface, self *gotypes.Named) bool {
	for method := range iface.Methods() {
		if method.Name() != "Set" {
			continue
		}
		sig := method.Signature()
		if sig.Params().Len() != 2 || sig.Results().Len() != 1 {
			continue
		}
		if !gotypes.Identical(sig.Results().At(0).Type(), self) {
			continue
		}

		keyType := sig.Params().At(0).Type()
		valType := sig.Params().At(1).Type()
		if isStringType(keyType) && isAnyType(valType) {
			return true
		}
	}
	return false
}

// isStringType reports whether typ is the string basic type.
func isStringType(typ gotypes.Type) bool {
	basic, ok := typ.(*gotypes.Basic)
	return ok && basic.Kind() == gotypes.String
}

// isAnyType reports whether typ is an empty interface.
func isAnyType(typ gotypes.Type) bool {
	iface, ok := typ.(*gotypes.Interface)
	return ok && iface.Empty()
}

// CallExprSupportsInterfaceKwargs reports whether expr can compile XGo
// interface-based kwargs for paramType.
func CallExprSupportsInterfaceKwargs(typeInfo *types.Info, expr *ast.CallExpr, paramType gotypes.Type) bool {
	if typeInfo == nil {
		return false
	}
	selector, ok := expr.Fun.(*ast.SelectorExpr)
	if !ok || !isAppendableKwargReceiver(selector.X) {
		return false
	}
	self, ok := paramType.(*gotypes.Named)
	if !ok {
		return false
	}
	if _, ok := self.Underlying().(*gotypes.Interface); !ok {
		return false
	}
	recvType := typeInfo.TypeOf(selector.X)
	if recvType == nil {
		return false
	}
	factory, _, _ := gotypes.LookupFieldOrMethod(recvType, true, self.Obj().Pkg(), self.Obj().Name())
	factoryFunc, ok := factory.(*gotypes.Func)
	if !ok {
		return false
	}
	sig := factoryFunc.Signature()
	return sig.Params().Len() == 0 && sig.Results().Len() == 1 &&
		gotypes.AssignableTo(sig.Results().At(0).Type(), self)
}

// isAppendableKwargReceiver reports whether receiver can be reused when XGo
// compiles interface kwargs into a method chain.
func isAppendableKwargReceiver(receiver ast.Expr) bool {
	switch receiver := receiver.(type) {
	case *ast.Ident:
		return true
	case *ast.SelectorExpr:
		_, ok := receiver.X.(*ast.Ident)
		return ok
	}
	return false
}

// ResolveCallExprKwarg returns the parameter slot that receives keyword
// arguments, if any.
func ResolveCallExprKwarg(typeInfo *types.Info, expr *ast.CallExpr) *ResolvedCallExprKwarg {
	_, sig, params := ResolveCallExprSignature(typeInfo, expr)
	if sig == nil || params == nil {
		return nil
	}

	return resolveCallExprKwarg(typeInfo, expr, sig, params)
}

// resolveCallExprKwarg returns the parameter slot that receives keyword
// arguments after the call signature has already been resolved.
func resolveCallExprKwarg(typeInfo *types.Info, expr *ast.CallExpr, sig *gotypes.Signature, params *gotypes.Tuple) *ResolvedCallExprKwarg {
	paramIndex := len(expr.Args)
	if sig.Variadic() {
		paramIndex = params.Len() - 2
		if paramIndex < 0 || len(expr.Args) < paramIndex {
			return nil
		}
	} else if paramIndex >= params.Len() {
		return nil
	}

	param := params.At(paramIndex)
	if len(expr.Kwargs) == 0 && !isOptionalParam(typeInfo, param) {
		return nil
	}

	return &ResolvedCallExprKwarg{
		Param:                 param,
		ParamIndex:            paramIndex,
		AllowInterfaceTargets: CallExprSupportsInterfaceKwargs(typeInfo, expr, param.Type()),
	}
}

// LookupResolvedCallExprKwargTarget resolves a keyword name against the target
// parameter slot.
func LookupResolvedCallExprKwargTarget(kwarg *ResolvedCallExprKwarg, name string) *ResolvedCallExprKwargTarget {
	if kwarg == nil || name == "" {
		return nil
	}

	paramType := kwarg.Param.Type()
	if strct, inMainPkg := structKwargType(paramType); strct != nil {
		field := lookupStructKwargField(strct, inMainPkg, name)
		if field == nil {
			return nil
		}
		target := structKwargTarget(field)
		return &target
	}

	switch u := paramType.Underlying().(type) {
	case *gotypes.Interface:
		return lookupInterfaceKwargTarget(kwarg, u, name)
	case *gotypes.Map:
		if acceptsStringLiteral(u.Key()) {
			return mapKwargTarget(name, u.Elem())
		}
	}
	return nil
}

// lookupInterfaceKwargTarget resolves name against an interface kwarg parameter.
func lookupInterfaceKwargTarget(kwarg *ResolvedCallExprKwarg, iface *gotypes.Interface, name string) *ResolvedCallExprKwargTarget {
	named, ok := kwarg.Param.Type().(*gotypes.Named)
	if !ok {
		if iface.Empty() {
			return mapKwargTarget(name, anyType())
		}
		return nil
	}

	if !kwarg.AllowInterfaceTargets {
		return nil
	}
	if target := lookupInterfaceKwargMethodTarget(iface, named, name); target != nil {
		return target
	}
	if !hasInterfaceKwargSet(iface, named) {
		return nil
	}
	return &ResolvedCallExprKwargTarget{
		Kind:      ResolvedCallExprKwargTargetInterfaceSet,
		Name:      name,
		ValueType: anyType(),
	}
}

// mapKwargTarget returns a dynamic string-keyed map kwarg target.
func mapKwargTarget(name string, valueType gotypes.Type) *ResolvedCallExprKwargTarget {
	return &ResolvedCallExprKwargTarget{
		Kind:      ResolvedCallExprKwargTargetMap,
		Name:      name,
		ValueType: valueType,
	}
}

// acceptsStringLiteral reports whether typ accepts an untyped string literal.
func acceptsStringLiteral(typ gotypes.Type) bool {
	return gotypes.AssignableTo(gotypes.Typ[gotypes.UntypedString], typ)
}

// ListResolvedCallExprKwargTargets lists the finite named keyword targets
// exposed by the target parameter slot. Map-backed and dynamic `Set`-style
// interface kwargs are not enumerated.
func ListResolvedCallExprKwargTargets(kwarg *ResolvedCallExprKwarg) []ResolvedCallExprKwargTarget {
	if kwarg == nil {
		return nil
	}

	var targets []ResolvedCallExprKwargTarget
	appendTarget := func(target ResolvedCallExprKwargTarget) {
		if !sameResolvedCallExprKwargTarget(LookupResolvedCallExprKwargTarget(kwarg, target.Name), target) {
			return
		}
		targets = append(targets, target)
	}
	paramType := kwarg.Param.Type()
	if strct, _ := structKwargType(paramType); strct != nil {
		for field := range strct.Fields() {
			if !IsExportedOrInMainPkg(field) {
				continue
			}
			appendTarget(structKwargTarget(field))
		}
		return targets
	}

	switch u := paramType.Underlying().(type) {
	case *gotypes.Interface:
		named, ok := paramType.(*gotypes.Named)
		if !ok || !kwarg.AllowInterfaceTargets {
			return targets
		}
		for method := range u.Methods() {
			target, ok := interfaceKwargMethodTarget(method, named)
			if !ok {
				continue
			}
			appendTarget(target)
		}
	}
	return targets
}

// sameResolvedCallExprKwargTarget reports whether resolved and target identify
// the same concrete kwarg target.
func sameResolvedCallExprKwargTarget(resolved *ResolvedCallExprKwargTarget, target ResolvedCallExprKwargTarget) bool {
	if resolved == nil {
		return false
	}
	if target.Field != nil {
		return resolved.Field == target.Field
	}
	if target.Method != nil {
		return resolved.Method == target.Method
	}
	return resolved.Kind == target.Kind && resolved.Name == target.Name
}

// ResolvedCallExprArgs returns an iterator over both positional arguments and
// keyword argument values for the given call expression.
func ResolvedCallExprArgs(typeInfo *types.Info, expr *ast.CallExpr) iter.Seq[ResolvedCallExprArg] {
	return func(yield func(ResolvedCallExprArg) bool) {
		fun, sig, params := ResolveCallExprSignature(typeInfo, expr)
		if fun == nil || sig == nil || params == nil {
			return
		}

		var kwarg *ResolvedCallExprKwarg
		if len(expr.Kwargs) > 0 {
			kwarg = resolveCallExprKwarg(typeInfo, expr, sig, params)
		}
		totalParams := params.Len()
		for i, arg := range expr.Args {
			ellipsis := expr.Ellipsis.IsValid() && i == len(expr.Args)-1
			paramIndex := i
			if kwarg != nil && i >= kwarg.ParamIndex {
				paramIndex++
			}
			if paramIndex >= totalParams {
				if !sig.Variadic() || totalParams == 0 {
					break
				}
				paramIndex = totalParams - 1
			}

			if !yield(ResolvedCallExprArg{
				Fun:          fun,
				Params:       params,
				Param:        params.At(paramIndex),
				ParamIndex:   paramIndex,
				Arg:          arg,
				ArgIndex:     i,
				Kind:         ResolvedCallExprArgPositional,
				ExpectedType: resolvedCallExprArgType(sig, params, paramIndex, ellipsis),
			}) {
				return
			}
		}

		if kwarg == nil {
			return
		}

		for i, arg := range expr.Kwargs {
			target := LookupResolvedCallExprKwargTarget(kwarg, arg.Name.Name)
			var expectedType gotypes.Type
			if target != nil {
				expectedType = target.ValueType
			}
			if !yield(ResolvedCallExprArg{
				Fun:          fun,
				Params:       params,
				Param:        kwarg.Param,
				ParamIndex:   kwarg.ParamIndex,
				Arg:          arg.Value,
				ArgIndex:     len(expr.Args) + i,
				Kind:         ResolvedCallExprArgKeyword,
				Kwarg:        arg,
				ExpectedType: expectedType,
				KwargTarget:  target,
			}) {
				return
			}
		}
	}
}
