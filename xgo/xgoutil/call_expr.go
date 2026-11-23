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
	"cmp"
	goast "go/ast"
	gotypes "go/types"
	"iter"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/qiniu/x/stringutil"
)

const (
	// xgoAutoclosureParamPrefix prefixes generated names for XGo autoclosure parameters.
	xgoAutoclosureParamPrefix = "__xgo_autoclosure_"

	// xgoOptionalParamPrefix marks optional parameters in imported Go signatures.
	xgoOptionalParamPrefix = "__xgo_optional_"

	// gogenOptionalParamKind marks local optional parameters created by gogen on Go 1.25 and later.
	gogenOptionalParamKind gotypes.VarKind = 0xff
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
	// Fun is nil for calls through function values.
	Fun        *gotypes.Func
	Signature  *gotypes.Signature
	Params     *gotypes.Tuple
	Param      *gotypes.Var
	ParamIndex int
	Arg        ast.Expr
	// ArgIndex is the index in the resolved source argument stream. For
	// positional arguments, it is the index after tuple expansion. Keyword
	// arguments follow the positional arguments in source order.
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

	info             *types.Info
	interfaceBuilder gotypes.Type
	containerType    gotypes.Type
	spellingType     gotypes.Type
	lowering         types.KeywordLowering

	// argIndex is the container's operand position, which can fall within
	// the selected signature's variadic tail.
	argIndex int

	// appendKeywords reports whether lowering appends a map after the
	// positional arguments without using a concrete signature.
	appendKeywords bool

	// inferredMapValueType is the expected element of an inferred keyword map.
	inferredMapValueType gotypes.Type
}

// ContainerType returns the type used to lower the keyword arguments.
func (kwarg *ResolvedCallExprKwarg) ContainerType() gotypes.Type {
	return cmp.Or(kwarg.containerType, kwarg.Param.Type())
}

// StructSpellingType returns the type used to choose the field spelling.
func (kwarg *ResolvedCallExprKwarg) StructSpellingType() gotypes.Type {
	return cmp.Or(kwarg.spellingType, kwarg.ContainerType())
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
	return arg.ParamIndex < NormalizedCallExprTypeArgCount(arg.Fun, arg.Params)
}

// CreateCallExprFromBranchStmt returns the function call recorded by XGo for
// a goto statement whose target is a value rather than a label.
func CreateCallExprFromBranchStmt(typeInfo *types.Info, stmt *ast.BranchStmt) *ast.CallExpr {
	if typeInfo == nil || stmt == nil {
		return nil
	}
	if stmt.Tok != token.GOTO || stmt.Label == nil {
		return nil
	}

	// Skip if this is a real branch statement with an actual label object.
	if _, ok := typeInfo.ObjectOf(stmt.Label).(*gotypes.Label); ok {
		return nil
	}

	// The compiler records the callee separately from the source branch node.
	stmtTokEnd := stmt.TokPos + token.Pos(len(stmt.Tok.String()))
	for ident, obj := range typeInfo.Uses {
		if ident.Pos() != stmt.TokPos || ident.End() != stmtTokEnd {
			continue
		}
		if _, ok := obj.(*gotypes.Func); !ok {
			return nil
		}
		return &ast.CallExpr{
			Fun:        ident,
			Args:       []ast.Expr{stmt.Label},
			NoParenEnd: stmt.Label.End(),
		}
	}
	return nil
}

// CallExprFun returns the callee used during compilation. A command's error
// wrapper applies to the call result.
func CallExprFun(expr *ast.CallExpr) ast.Expr {
	if wrap, ok := expr.Fun.(*ast.ErrWrapExpr); ok && expr.IsCommand() {
		return wrap.X
	}
	return expr.Fun
}

// CallExprFunIdent returns the identifier at the base of the callee expression,
// looking through parentheses and index expressions.
func CallExprFunIdent(expr *ast.CallExpr) *ast.Ident {
	switch node := callExprCallee(expr).(type) {
	case *ast.Ident:
		return node
	case *ast.SelectorExpr:
		return node.Sel
	default:
		return nil
	}
}

// callExprCallableIdent returns the callee's base identifier for direct calls
// and type instantiations, including incomplete ones.
func callExprCallableIdent(info *types.Info, expr *ast.CallExpr) *ast.Ident {
	if index, ok := astutil.Unparen(CallExprFun(expr)).(*ast.IndexExpr); ok {
		if isType, known := IsTypeExpr(info, index.Index); known && !isType {
			return nil
		}
	}
	return CallExprFunIdent(expr)
}

// callExprCallee unwraps parentheses and index expressions from the callee.
func callExprCallee(expr *ast.CallExpr) ast.Expr {
	fun := CallExprFun(expr)
	for {
		switch node := fun.(type) {
		case *ast.ParenExpr:
			fun = node.X
		case *ast.IndexExpr:
			fun = node.X
		case *ast.IndexListExpr:
			fun = node.X
		default:
			return fun
		}
	}
}

// FuncFromCallExpr returns the recorded function object, or nil for
// function values and other expressions without a function declaration.
func FuncFromCallExpr(typeInfo *types.Info, expr *ast.CallExpr) *gotypes.Func {
	if typeInfo == nil || expr == nil {
		return nil
	}
	fun, _ := typeInfo.ObjectOf(callExprCallableIdent(typeInfo, expr)).(*gotypes.Func)
	return fun
}

// CallExprImplementation returns the declaration recorded for a direct call,
// including selections retained from nested overload resolution.
func CallExprImplementation(info *types.Info, expr *ast.CallExpr) gotypes.Object {
	implementation, _ := callExprImplementation(info, expr)
	return implementation
}

// CallExprOverloadImplementations yields the call's reconstructed candidates
// with their source bindings, including candidates of a static member.
func CallExprOverloadImplementations(info *types.Info, call *ast.CallExpr) iter.Seq2[gotypes.Object, *gotypes.Func] {
	ident := callExprCallableIdent(info, call)
	if ident == nil {
		return nil
	}
	fun, _ := info.Overloads[ident].(*gotypes.Func)
	if fun == nil {
		fun, _ = info.ObjectOf(ident).(*gotypes.Func)
	}
	if fun == nil {
		return nil
	}
	member := types.StaticMemberObject(fun)
	if member == nil {
		return OverloadImplementations(fun)
	}
	return func(yield func(gotypes.Object, *gotypes.Func) bool) {
		if group, ok := member.(*gotypes.Func); ok {
			if candidates := OverloadImplementations(group); candidates != nil {
				for obj, binding := range candidates {
					if binding == group {
						binding = fun
					}
					if !yield(obj, binding) {
						return
					}
				}
				return
			}
		}
		yield(member, fun)
	}
}

// ConcreteOverloadSelection resolves a recorded candidate to its implementation
// and any distinct type-argument binding.
func ConcreteOverloadSelection(obj gotypes.Object) types.OverloadSelection {
	implementation, binding := CallableImplementation(obj)
	if fun, ok := implementation.(*gotypes.Func); ok {
		if _, extended := gogen.CheckFuncEx(fun.Signature()); extended || IsOverloadPlaceholder(fun) {
			return types.OverloadSelection{}
		}
	}
	return types.OverloadSelection{Implementation: implementation, Binding: binding}
}

// callExprImplementation also returns a distinct type-argument binding when
// the compiler selected a type-as-parameters alias.
func callExprImplementation(info *types.Info, expr *ast.CallExpr) (gotypes.Object, *gotypes.Func) {
	if callExprCallee(expr) != astutil.Unparen(CallExprFun(expr)) {
		return nil, nil
	}
	ident := CallExprFunIdent(expr)
	if selected := info.SelectedOverloads[ident]; selected.Implementation != nil {
		return selected.Implementation, selected.Binding
	}
	return CallableImplementation(info.ObjectOf(ident))
}

// CallableImplementation unwraps static members and single-candidate
// overloads. It also returns the type-as-parameters wrapper when it binds the
// implementation and retains unresolved groups as is.
func CallableImplementation(obj gotypes.Object) (gotypes.Object, *gotypes.Func) {
	if fun, ok := obj.(*gotypes.Func); ok {
		obj = cmp.Or(types.StaticMemberObject(fun), obj)
	}
	fun, ok := obj.(*gotypes.Func)
	if !ok {
		return obj, nil
	}
	candidates := OverloadImplementations(fun)
	if candidates == nil {
		return obj, nil
	}
	var implementation gotypes.Object
	var binding *gotypes.Func
	for candidate, source := range candidates {
		if implementation != nil {
			return obj, nil
		}
		implementation, binding = candidate, source
	}
	if implementation == nil {
		return obj, nil
	}
	ext, _ := gogen.CheckFuncEx(binding.Signature())
	if _, typeAsParams := ext.(*gogen.TyTypeAsParams); !typeAsParams {
		binding = nil
	}
	return implementation, binding
}

// ResolveCallableSignatureForCall resolves a specific overload candidate,
// which can be a function declaration or a function-valued variable. The
// optional binding supplies the selected parameter wrapper for the candidate.
func ResolveCallableSignatureForCall(info *types.Info, expr *ast.CallExpr, obj gotypes.Object, binding *gotypes.Func) (*gotypes.Signature, *gotypes.Tuple) {
	var source *gotypes.Func
	if info != nil && expr != nil {
		source, _ = info.SourceObjectOf(CallExprFunIdent(expr)).(*gotypes.Func)
	}
	if fun, ok := obj.(*gotypes.Func); ok {
		return resolveFuncSignatureForCall(info, expr, fun, cmp.Or(source, binding), binding)
	}
	if sel, ok := callExprCallee(expr).(*ast.SelectorExpr); ok && info != nil {
		if isType, _ := IsTypeExpr(info, sel.X); isType {
			source, binding = nil, nil
		}
	}
	return ResolveSourceFuncSignature(obj, source, binding)
}

// ResolveCallExprSignature resolves the call signature and source-visible
// parameters for expr. The function identifies the resolved implementation,
// including selected overloads and reconstructed static members. It is nil
// for calls through function values.
func ResolveCallExprSignature(typeInfo *types.Info, expr *ast.CallExpr) (fun *gotypes.Func, sig *gotypes.Signature, params *gotypes.Tuple) {
	if typeInfo == nil || expr == nil {
		return nil, nil, nil
	}

	ident := callExprCallableIdent(typeInfo, expr)
	fun, _ = typeInfo.ObjectOf(ident).(*gotypes.Func)
	if fun != nil {
		sig, params = ResolveFuncSignatureForCall(typeInfo, expr, fun)
		if sig != nil {
			implementation, _ := CallableImplementation(fun)
			fun, _ = implementation.(*gotypes.Func)
			return fun, sig, params
		}
	}
	if typeInfo.Overloads[ident] != nil {
		if implementation, binding := callExprImplementation(typeInfo, expr); implementation != nil && implementation != fun {
			if sig, params := ResolveCallableSignatureForCall(typeInfo, expr, implementation, binding); sig != nil {
				fun, _ = implementation.(*gotypes.Func)
				return fun, sig, params
			}
		}
	}
	if fun == nil {
		if sig := callExprSignature(typeInfo, expr, nil); sig != nil {
			return nil, sig, sig.Params()
		}
	}
	return nil, nil, nil
}

// callExprSignature returns the callee's actual function type. A declaration
// signature can recover an instantiation that the checker did not record.
func callExprSignature(typeInfo *types.Info, expr *ast.CallExpr, decl *gotypes.Signature) *gotypes.Signature {
	callee := CallExprFun(expr)
	isType, _ := IsTypeExpr(typeInfo, callee)
	if isType || typeInfo.Types[callee].IsBuiltin() {
		return nil
	}
	typ := typeInfo.TypeOf(callee)
	if typ == nil {
		return instantiateCallExprSignature(typeInfo, callee, decl)
	}
	sig, _ := typ.Underlying().(*gotypes.Signature)
	if sig == nil {
		return nil
	}
	if _, ok := gogen.CheckFuncEx(sig); ok {
		return nil
	}
	return sig
}

// instantiateCallExprSignature recovers a function value with multiple explicit
// type arguments. XGo can omit the type of a standalone IndexListExpr.
func instantiateCallExprSignature(info *types.Info, callee ast.Expr, decl *gotypes.Signature) *gotypes.Signature {
	indexed, ok := astutil.Unparen(callee).(*ast.IndexListExpr)
	if !ok || decl == nil || len(indexed.Indices) < 2 || decl.TypeParams().Len() != len(indexed.Indices) {
		return nil
	}
	args := make([]gotypes.Type, 0, len(indexed.Indices))
	for _, index := range indexed.Indices {
		typ := info.TypeOf(index)
		if isType, _ := IsTypeExpr(info, index); !isType || !IsValidType(typ) {
			return nil
		}
		args = append(args, typ)
	}
	instantiated, err := gotypes.Instantiate(nil, decl, args, true)
	if err != nil {
		return nil
	}
	return instantiated.(*gotypes.Signature)
}

// ResolveFuncSignatureForCall resolves fun's signature and normalized
// parameter list for expr.
func ResolveFuncSignatureForCall(typeInfo *types.Info, expr *ast.CallExpr, fun *gotypes.Func) (sig *gotypes.Signature, params *gotypes.Tuple) {
	return ResolveCallableSignatureForCall(typeInfo, expr, fun, nil)
}

// resolveFuncSignatureForCall resolves a function with its parameter binding.
func resolveFuncSignatureForCall(typeInfo *types.Info, expr *ast.CallExpr, fun, source, binding *gotypes.Func) (sig *gotypes.Signature, params *gotypes.Tuple) {
	if expr == nil || fun == nil || IsOverloadPlaceholder(fun) {
		return nil, nil
	}

	sig = fun.Signature()
	_, extended := gogen.CheckFuncEx(sig)
	paramOffset, isXGox := xgoFuncParamLayout(fun, source, binding)
	var typeReceiver ast.Expr
	selector, _ := callExprCallee(expr).(*ast.SelectorExpr)
	if typeInfo != nil && selector != nil {
		if isType, _ := IsTypeExpr(typeInfo, selector.X); isType {
			typeReceiver = selector.X
		}
	}
	if typeReceiver != nil {
		implementation, _ := ReconstructedMethodImplementation(source)
		if implementation != nil && (source == fun || implementation == fun) &&
			typeInfo.ObjectOf(selector.Sel) == source {
			if actual := callExprSignature(typeInfo, expr, implementation.Signature()); actual != nil {
				return actual, actual.Params()
			}
			// The recorder can keep a synthetic wrapper or pending inference
			// type. Use the declaration when the callee has such a record.
			if source == fun && typeInfo.Types[CallExprFun(expr)].Type != nil {
				impl := implementation.Signature()
				if _, extended := gogen.CheckFuncEx(impl); !extended {
					return impl, impl.Params()
				}
			}
		}
	}
	if extended {
		return nil, nil
	}
	// Selecting a template through its type can expose the explicit receiver.
	encoded := paramOffset > 0 && !isXGox && typeReceiver != nil
	if !encoded && (paramOffset > 0 || isXGox) {
		if ident := CallExprFunIdent(expr); ident != nil {
			encoded = ReferencesEncodedFuncName(fun, ident.Name)
		}
	}
	// The callee type can include generic instantiation or a method expression's
	// receiver. Other overload candidates retain their declaration types.
	if typeInfo != nil && (encoded || paramOffset == 0 && !isXGox) && FuncFromCallExpr(typeInfo, expr) == fun {
		if actual := callExprSignature(typeInfo, expr, sig); actual != nil {
			sig = actual
		}
	}
	if encoded {
		return sig, sig.Params()
	}
	typeArgCount := 0
	if typeParams := sig.TypeParams(); isXGox && typeParams != nil {
		typeArgCount = typeParams.Len()
		if typeInfo != nil {
			typeArgCount = callExprXGoxTypeArgCount(typeInfo, expr, sig, typeArgCount)
		}
	}
	params, ok := normalizedCallExprParams(sig, paramOffset, isXGox, typeArgCount)
	if !ok {
		return nil, nil
	}
	// XGo's member recorder can retain the method declaration signature for
	// a method expression. Its explicit receiver has the selector's type,
	// which may differ from the declaring receiver for promoted methods.
	if recv := sig.Recv(); typeReceiver != nil && recv != nil {
		params = MethodExpressionParams(params, typeInfo.TypeOf(typeReceiver), recv.Type())
		if params == nil {
			return nil, nil
		}
	}
	return sig, params
}

// MethodExpressionParams prepends the explicit receiver for an ordinary
// method expression. It returns nil when the receiver cannot be used.
func MethodExpressionParams(params *gotypes.Tuple, selector, declared gotypes.Type) *gotypes.Tuple {
	receiver := MethodExpressionReceiver(selector, declared)
	if receiver == nil {
		return nil
	}
	recv := gotypes.NewParam(token.NoPos, nil, "", receiver)
	return gotypes.NewTuple(slices.AppendSeq([]*gotypes.Var{recv}, params.Variables())...)
}

// ResolveFuncSignature resolves the callable signature and normalized
// parameter list for fun.
func ResolveFuncSignature(fun *gotypes.Func) (sig *gotypes.Signature, params *gotypes.Tuple) {
	return ResolveSourceFuncSignature(fun, nil, nil)
}

// ResolveSourceFuncSignature resolves the declaration signature as exposed by
// the source callable, including bound template receivers and type arguments.
// An optional binding describes a distinct wrapper selected by the overload.
func ResolveSourceFuncSignature(callable gotypes.Object, source, binding *gotypes.Func) (sig *gotypes.Signature, params *gotypes.Tuple) {
	sig, _ = callable.Type().Underlying().(*gotypes.Signature)
	if sig == nil {
		return nil, nil
	}
	if _, ok := gogen.CheckFuncEx(sig); ok {
		return nil, nil
	}

	typeArgCount := 0
	if typeParams := sig.TypeParams(); typeParams != nil {
		typeArgCount = typeParams.Len()
	}
	paramOffset, isXGox := xgoFuncParamLayout(callable, source, binding)
	params, ok := normalizedCallExprParams(sig, paramOffset, isXGox, typeArgCount)
	if !ok {
		return nil, nil
	}
	return sig, params
}

// normalizedCallExprParams returns the source-visible parameters. An imported
// template can be reconstructed even when its receiver parameter is missing.
func normalizedCallExprParams(sig *gotypes.Signature, paramOffset int, isXGox bool, typeArgCount int) (*gotypes.Tuple, bool) {
	params := sig.Params()
	if paramOffset > params.Len() {
		return nil, false
	}
	if sig.Variadic() && paramOffset == params.Len() {
		// A bound receiver can use the first variadic argument. The remaining
		// arguments still share the same parameter.
		paramOffset--
	}
	if paramOffset == 0 && !isXGox {
		return params, true
	}

	var vars []*gotypes.Var
	if typeParams := sig.TypeParams(); isXGox && typeParams != nil {
		typeArgCount = min(typeArgCount, typeParams.Len())
		vars = slices.Grow(vars, typeArgCount)
		for i := range typeArgCount {
			typeParam := typeParams.At(i)
			param := gotypes.NewParam(token.NoPos, typeParam.Obj().Pkg(), typeParam.Obj().Name(), typeParam)
			vars = append(vars, param)
		}
	}

	vars = slices.Grow(vars, params.Len()-paramOffset)
	for i := paramOffset; i < params.Len(); i++ {
		vars = append(vars, params.At(i))
	}
	return gotypes.NewTuple(vars...), true
}

// NormalizedCallExprTypeArgCount returns the number of normalized XGox type
// arguments at the start of params.
func NormalizedCallExprTypeArgCount(fun *gotypes.Func, params *gotypes.Tuple) int {
	if fun == nil || params == nil {
		return 0
	}
	// Normalization inserts synthetic parameters named after the type
	// parameters and gives them the corresponding type parameter types.
	typeParams := fun.Signature().TypeParams()
	var count int
	for count < min(typeParams.Len(), params.Len()) &&
		params.At(count).Type() == typeParams.At(count) &&
		params.At(count).Name() == typeParams.At(count).Obj().Name() {
		count++
	}
	return count
}

// CallExprTemplateReceiver returns the implicit template receiver and whether
// the call binds it. In incomplete source, the receiver type may be unknown.
func CallExprTemplateReceiver(info *types.Info, call *ast.CallExpr, fun *gotypes.Func, sig *gotypes.Signature, params *gotypes.Tuple) (gotypes.TypeAndValue, bool) {
	valueCount := params.Len() - NormalizedCallExprTypeArgCount(fun, params)
	paramCount := sig.Params().Len()
	if paramCount <= valueCount && (!sig.Variadic() || paramCount != 1 || valueCount != 1) {
		return gotypes.TypeAndValue{}, false
	}
	selector, _ := callExprCallee(call).(*ast.SelectorExpr)
	if selector != nil {
		if isType, _ := IsTypeExpr(info, selector.X); isType {
			return gotypes.TypeAndValue{}, false
		}
	}
	ident := CallExprFunIdent(call)
	if paramCount == valueCount {
		// A variadic receiver shares its parameter with the explicit arguments.
		source, _ := info.SourceObjectOf(ident).(*gotypes.Func)
		var callable gotypes.Object
		if fun != nil {
			callable = fun
		}
		offset, _ := xgoFuncParamLayout(callable, source, nil)
		if offset == 0 || fun != nil && ident != nil && ReferencesEncodedFuncName(fun, ident.Name) {
			return gotypes.TypeAndValue{}, false
		}
	}
	if selector != nil {
		receiver := info.Types[selector.X]
		receiver.Type = info.TypeOf(selector.X)
		return receiver, true
	}
	return gotypes.TypeAndValue{Type: info.ImplicitReceivers[ident]}, true
}

// templateReceiverCandidates returns the receiver values in gogen's matching
// order, using the address only after the original value.
func templateReceiverCandidates(receiver gotypes.TypeAndValue) []gotypes.TypeAndValue {
	candidates := []gotypes.TypeAndValue{receiver}
	if _, pointer := gotypes.Unalias(receiver.Type).(*gotypes.Pointer); IsValidType(receiver.Type) && !pointer {
		candidates = append(candidates, gotypes.TypeAndValue{Type: gotypes.NewPointer(receiver.Type)})
	}
	return candidates
}

// BindCallExprParams binds known type arguments and the template receiver.
// The source tuple keeps its type argument slots. Known value types can also
// determine the remaining type arguments.
// skipArgIndex excludes the argument being edited from binding and inference.
// A known mismatch returns the original tuple and false.
func BindCallExprParams(info *types.Info, call *ast.CallExpr, fun *gotypes.Func, sig *gotypes.Signature, params *gotypes.Tuple, skipArgIndex int) (*gotypes.Tuple, bool) {
	if info == nil || sig == nil {
		return params, true
	}
	count := NormalizedCallExprTypeArgCount(fun, params)
	receiver, hasReceiver := CallExprTemplateReceiver(info, call, fun, sig, params)
	if count == 0 && !IsValidType(receiver.Type) {
		return params, true
	}
	typeParams := sig.TypeParams()
	receivers := templateReceiverCandidates(receiver)
	receiverMatches := func(sig *gotypes.Signature, receiver gotypes.TypeAndValue) bool {
		return !IsValidType(receiver.Type) || CallValueTypeMatches(info, receiver, firstCallValueParamType(sig))
	}
	if typeParams.Len() == 0 {
		return params, slices.ContainsFunc(receivers, func(receiver gotypes.TypeAndValue) bool {
			return receiverMatches(sig, receiver)
		})
	}

	args, ellipsis := CallExprArgsForSignature(info, call, sig, params)
	bindings := make([]gotypes.Type, typeParams.Len())
	bound := 0
	for i, arg := range args[:min(count, len(args))] {
		if i == skipArgIndex {
			continue
		}
		isType, known := IsTypeExpr(info, arg)
		if known && !isType {
			return params, false
		}
		if typ := info.TypeOf(arg); isType && IsValidType(typ) && typ.Underlying() != nil {
			bindings[i] = typ
			bound++
		}
	}
	valueCount := params.Len() - count
	paramOffset := sig.Params().Len() - valueCount
	var instance *gotypes.Signature
	if bound == count && bound < len(bindings) {
		for _, candidate := range receivers {
			operand := candidate
			if hasReceiver && !IsValidType(operand.Type) {
				operand.Type = gotypes.Typ[gotypes.UntypedNil]
			}
			inferred := inferCallExprSignature(info, call, sig, params, bindings[:bound], args[count:], operand, skipArgIndex-count, ellipsis)
			if inferred != nil && receiverMatches(inferred, candidate) {
				instance = inferred
				break
			}
		}
	}
	if instance == nil {
		if bound == 0 {
			return params, true
		}
		for i, typ := range bindings {
			if typ == nil {
				bindings[i] = typeParams.At(i)
			}
		}
		instantiated, err := gotypes.Instantiate(nil, sig, bindings, bound == len(bindings))
		if err != nil {
			return params, false
		}
		instance = instantiated.(*gotypes.Signature)
		if bound == len(bindings) && !slices.ContainsFunc(receivers, func(receiver gotypes.TypeAndValue) bool {
			return receiverMatches(instance, receiver)
		}) {
			return params, false
		}
	}
	values := instance.Params()
	vars := make([]*gotypes.Var, params.Len())
	for i := range count {
		vars[i] = params.At(i)
	}
	for i := range valueCount {
		vars[count+i] = values.At(paramOffset + i)
	}
	return gotypes.NewTuple(vars...), true
}

// inferCallExprSignature uses gogen to bind the remaining type arguments.
func inferCallExprSignature(info *types.Info, call *ast.CallExpr, sig *gotypes.Signature, params *gotypes.Tuple, typeArgs []gotypes.Type, args []ast.Expr, receiver gotypes.TypeAndValue, skipArgIndex int, ellipsis bool) *gotypes.Signature {
	receiverCount := 0
	if receiver.Type != nil {
		receiverCount = 1
	}
	operandCount := len(args) + receiverCount
	var kwarg *ResolvedCallExprKwarg
	if len(call.Kwargs) != 0 {
		kwarg = resolveCallExprKwarg(info, call, sig, params, false)
		if kwarg == nil || kwarg.ParamIndex < len(typeArgs) {
			return nil
		}
		// XGo inserts variadic kwargs before removing written type arguments.
		if !kwarg.appendKeywords && sig.Variadic() && kwarg.ParamIndex != sig.Params().Len()-2 {
			return nil
		}
		operandCount++
	}
	if ellipsis && (!sig.Variadic() || operandCount != sig.Params().Len()) {
		return nil
	}
	operands := make([]*gogen.Element, 0, operandCount)
	ident := goast.NewIdent("_")
	// Go inference does not learn type arguments from an untyped nil operand.
	unknown := &gogen.Element{Val: ident, Type: gotypes.Typ[gotypes.UntypedNil]}
	operandFor := func(arg ast.Expr) *gogen.Element {
		typ := info.TypeOf(arg)
		if !IsValidType(typ) || typ.Underlying() == nil {
			return unknown
		}
		return &gogen.Element{Val: ident, Type: typ, CVal: info.Types[arg].Value}
	}
	if receiverCount != 0 {
		operands = append(operands, &gogen.Element{Val: ident, Type: receiver.Type, CVal: receiver.Value})
	}
	for i, arg := range args {
		operand := unknown
		if i != skipArgIndex {
			operand = operandFor(arg)
		}
		operands = append(operands, operand)
	}
	pkg := gogen.NewPackage("", "", &gogen.Config{
		NewBuiltin: func(*gogen.Package, *gogen.Config) *gotypes.Package { return nil },
	})
	if kwarg != nil {
		operand := unknown
		// Editing a keyword value can change the type of the entire map.
		_, isMap := kwarg.Param.Type().Underlying().(*gotypes.Map)
		if (isMap || kwarg.inferredMapValueType != nil) && skipArgIndex < len(args) {
			typ := KeywordMapType(pkg.CB(), call.Kwargs, nil, func(expr ast.Expr) gotypes.Type {
				if isType, _ := IsTypeExpr(info, expr); isType {
					return nil
				}
				return info.TypeOf(expr)
			})
			if typ != nil {
				operand = &gogen.Element{Val: ident, Type: typ}
			}
		}
		if kwarg.appendKeywords {
			operands = append(operands, operand)
		} else {
			index := kwarg.ParamIndex - len(typeArgs) + receiverCount
			operands = slices.Insert(operands, index, operand)
		}
	}
	// A receiver can determine type arguments before a user has supplied
	// the remaining fixed arguments.
	if receiverCount != 0 && !ellipsis {
		fixed := sig.Params().Len()
		if sig.Variadic() {
			fixed--
		}
		for len(operands) < fixed {
			operands = append(operands, unknown)
		}
	}
	if sig.Variadic() && !ellipsis && len(operands) >= sig.Params().Len()-1 {
		last := sig.Params().Len() - 1
		tail := operands[last:]
		// Gogen infers the variadic slice from the first expanded operand.
		if i := slices.IndexFunc(tail, func(arg *gogen.Element) bool { return arg.Type != unknown.Type }); i >= 0 {
			tail[0] = tail[i]
		} else {
			operands = append(operands[:last], unknown)
			ellipsis = true
		}
	}
	var flags gogen.InstrFlags
	if ellipsis {
		flags = gogen.InstrFlagEllipsis
	}
	fn := &gogen.Element{Val: ident, Type: sig}
	instance, err := gogen.InferFunc(pkg, fn, sig, typeArgs, operands, flags)
	if err != nil {
		return nil
	}
	return instance.(*gotypes.Signature)
}

// KeywordMapType checks the map XGo builds for keyword arguments. A nil
// target lets the compiler infer its type from the values.
func KeywordMapType(cb *gogen.CodeBuilder, kwargs []*ast.KwargExpr, target gotypes.Type, typeOf func(ast.Expr) gotypes.Type) gotypes.Type {
	stack := cb.InternalStack()
	defer stack.SetLen(stack.Len())
	ident := goast.NewIdent("_")
	hasValueType := target != nil
	for _, arg := range kwargs {
		typ := typeOf(arg.Value)
		if !IsValidType(typ) || typ.Underlying() == nil {
			return nil
		}
		if _, tuple := typ.(*gotypes.Tuple); tuple {
			return nil
		}
		hasValueType = hasValueType || typ != gotypes.Typ[gotypes.UntypedNil]
		cb.Val(arg.Name.Name)
		stack.Push(&gogen.Element{Val: ident, Type: typ})
	}
	if !hasValueType || cb.MapLitEx(target, 2*len(kwargs)) != nil {
		return nil
	}
	return cb.Get(-1).Type
}

// xgoFuncParamLayout resolves parameter layout from the source callable and
// the selected overload's wrapper.
// Declaration spelling also supplies the layout when describing an implementation
// on its own or when the recorder has no reconstructed callable.
func xgoFuncParamLayout(callable gotypes.Object, source, binding *gotypes.Func) (paramOffset int, isXGox bool) {
	if wrapper := cmp.Or(binding, source); wrapper != nil {
		ext, _ := gogen.CheckFuncEx(wrapper.Signature())
		switch ext := ext.(type) {
		case *gogen.TyTemplateRecvMethod:
			return 1, false
		case *gogen.TyTypeAsParams:
			wrapped, _ := ext.Obj().(*gotypes.Func)
			_, template := ReconstructedMethodImplementation(wrapped)
			_, sourceTemplate := ReconstructedMethodImplementation(source)
			if template || sourceTemplate {
				return 1, true
			}
			return 0, true
		case *gogen.TyOverloadFunc, *gogen.TyOverloadMethod, *gogen.TyStaticMember:
			return 0, false
		}
	}
	fun, ok := callable.(*gotypes.Func)
	if !ok || !IsMarkedAsXGoPackage(fun.Pkg()) {
		return 0, false
	}
	if _, methodName, ok := TemplateMethodName(fun, false); ok {
		_, isXGox = splitXGoxMethodName(methodName)
		return 1, isXGox
	}
	_, isXGox = SplitXGoxFuncName(fun.Name())
	return 0, isXGox
}

// callExprXGoxTypeArgCount returns the type-argument prefix that remains
// explicit in a call. Once a value argument starts, XGo infers the remaining
// type parameters.
func callExprXGoxTypeArgCount(typeInfo *types.Info, expr *ast.CallExpr, sig *gotypes.Signature, typeArgCount int) int {
	args, _ := CallExprArgsForSignature(typeInfo, expr, sig, sig.Params())
	for i := 0; i < min(typeArgCount, len(args)); i++ {
		isType, known := IsTypeExpr(typeInfo, args[i])
		if !known {
			return typeArgCount
		}
		if !isType {
			if i > 0 {
				return i
			}
			return typeArgCount
		}
	}
	return typeArgCount
}

// resolvedCallExprArgType returns the expected argument type at paramIndex.
// It unwraps variadic slices to their element type unless the source argument
// uses ellipsis.
func resolvedCallExprArgType(sig *gotypes.Signature, params *gotypes.Tuple, paramIndex int, ellipsis bool) gotypes.Type {
	param := params.At(paramIndex)
	if sig.Variadic() && paramIndex == params.Len()-1 && !ellipsis {
		return variadicValueType(param.Type())
	}
	return SourceParamType(param)
}

// variadicValueType returns the per-argument type for a variadic parameter.
func variadicValueType(typ gotypes.Type) gotypes.Type {
	if sliceType, ok := typ.(*gotypes.Slice); ok {
		return sliceType.Elem()
	}
	return typ
}

// RequiredParamCount returns the minimum argument count for the normalized params
// returned by [ResolveFuncSignatureForCall]. typeArgCount is the number of
// explicit XGox type-argument slots at the start of params.
func RequiredParamCount(typeInfo *types.Info, sig *gotypes.Signature, params *gotypes.Tuple, typeArgCount int) int {
	count := params.Len()
	if sig.Variadic() {
		count--
	}

	// XGo checks inference arity before filling optional arguments.
	if sig.TypeParams().Len() > typeArgCount {
		if sig.Variadic() {
			// A type parameter tail also needs a value during inference.
			if _, needsValue := variadicValueType(params.At(count).Type()).(*gotypes.TypeParam); needsValue {
				count++
			}
		}
		return count
	}

	for count > 0 && isOptionalParam(typeInfo, params.At(count-1)) {
		count--
	}
	return count
}

// SourceParamName returns the parameter name without compiler metadata prefixes.
func SourceParamName(param *gotypes.Var) string {
	name := param.Name()
	for _, prefix := range [...]string{xgoAutoclosureParamPrefix, xgoOptionalParamPrefix, "__xgo_loop_", "__xgo_cond_"} {
		if sourceName, ok := strings.CutPrefix(name, prefix); ok {
			return sourceName
		}
	}
	return name
}

// SourceParamType returns the source-facing type of param.
func SourceParamType(param *gotypes.Var) gotypes.Type {
	if typeParam, ok := param.Type().(*gotypes.TypeParam); ok && param.Name() == typeParam.Obj().Name() {
		return typeParam.Constraint().Underlying()
	}
	if resultType, ok := AutoclosureParamResultType(param); ok {
		return resultType
	}
	return param.Type()
}

// AutoclosureParamResultType returns the result type of param and reports
// whether param is an XGo autoclosure parameter with an underlying `func() T`
// type.
func AutoclosureParamResultType(param *gotypes.Var) (gotypes.Type, bool) {
	if !strings.HasPrefix(param.Name(), xgoAutoclosureParamPrefix) {
		return nil, false
	}
	sig, ok := param.Type().Underlying().(*gotypes.Signature)
	if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
		return nil, false
	}
	return sig.Results().At(0).Type(), true
}

// isOptionalParam reads local compiler metadata or an imported parameter name.
func isOptionalParam(typeInfo *types.Info, param *gotypes.Var) bool {
	if typeInfo != nil && typeInfo.Pkg != nil && param.Pkg() == typeInfo.Pkg {
		return param.Kind() == gogenOptionalParamKind
	}
	return strings.HasPrefix(param.Name(), xgoOptionalParamPrefix)
}

// anyType returns the predeclared `any` type.
func anyType() gotypes.Type {
	return gotypes.Universe.Lookup("any").Type()
}

// capitalizeStructKeyword applies the compiler's field spelling. A short
// keyword can have too few bytes for its uppercase first rune.
func capitalizeStructKeyword(name string) string {
	first, _ := utf8.DecodeRuneInString(name)
	if utf8.RuneLen(unicode.ToUpper(first)) > len(name) {
		return ""
	}
	return stringutil.Capitalize(name)
}

// kwargSuggestedName prefers a lowercase spelling that can recover the field name.
func kwargSuggestedName(name string) string {
	first, size := utf8.DecodeRuneInString(name)
	if !unicode.IsUpper(first) {
		return name
	}
	lower := unicode.ToLower(first)
	if lower == first || unicode.ToUpper(lower) != first {
		return name
	}
	alias := string(lower) + name[size:]
	// XGo capitalizes the first rune in a buffer sized to the written name.
	if len(alias) < size || token.IsKeyword(alias) {
		return name
	}
	return alias
}

// RenamedStructKwargName returns the keyword spelling for field after its
// declaration is renamed to name. Spelling reflects the struct used to
// normalize names, which can differ from the struct that owns the field.
func RenamedStructKwargName(spelling gotypes.Type, field *gotypes.Var, name string) string {
	alias := kwargSuggestedName(name)
	if alias == name {
		return name
	}
	strct, localPkg := structKwargType(spelling, field.Pkg())
	if localPkg == nil {
		return alias
	}
	origin := field.Origin()
	for other := range strct.Fields() {
		if other.Origin() == origin {
			return alias
		}
		if other.Name() == alias {
			return name
		}
	}
	return name
}

// StructKeywordField resolves a keyword using the struct literal's field
// spelling and the current package's access to its fields.
func StructKeywordField(typ gotypes.Type, currentPkg *gotypes.Package, name string) *gotypes.Var {
	strct, localPkg := structKwargType(typ, currentPkg)
	if strct == nil {
		return nil
	}
	capitalized := capitalizeStructKeyword(name)
	if capitalized == "" {
		return nil
	}
	return lookupStructKwargField(strct, localPkg, name, capitalized)
}

// lookupStructKwargField resolves name to a struct field that can be addressed
// by kwargs.
func lookupStructKwargField(strct *gotypes.Struct, localPkg *gotypes.Package, name, capitalized string) *gotypes.Var {
	for field := range strct.Fields() {
		if localPkg != nil && field.Name() == name {
			if field.Exported() || field.Pkg() == localPkg {
				return field
			}
			return nil
		}
		if field.Exported() && field.Name() == capitalized {
			return field
		}
	}
	return nil
}

// structKwargType returns the struct and the package in which lowering can
// use unexported field names, if any.
func structKwargType(typ gotypes.Type, currentPkg *gotypes.Package) (*gotypes.Struct, *gotypes.Package) {
	if ptr, ok := typ.Underlying().(*gotypes.Pointer); ok {
		typ = ptr.Elem()
	}
	strct, ok := typ.Underlying().(*gotypes.Struct)
	if !ok {
		return nil, nil
	}
	named, ok := typ.(*gotypes.Named)
	if !ok || named.Obj().Pkg() != currentPkg {
		return strct, nil
	}
	return strct, currentPkg
}

// structKwargTarget returns a target for field.
func structKwargTarget(field *gotypes.Var) ResolvedCallExprKwargTarget {
	return ResolvedCallExprKwargTarget{
		Kind:      ResolvedCallExprKwargTargetStructField,
		Name:      kwargSuggestedName(field.Name()),
		ValueType: field.Type(),
		Field:     field,
	}
}

// lookupInterfaceKwargMethodTarget resolves name to a self-returning interface
// method target.
func lookupInterfaceKwargMethodTarget(iface *gotypes.Interface, self *gotypes.Named, name string) *ResolvedCallExprKwargTarget {
	methodName := UpperFirstASCII(name)
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
	return ResolvedCallExprKwargTarget{
		Kind:      ResolvedCallExprKwargTargetInterfaceMethod,
		Name:      FunctionAliasName(method.Name()),
		ValueType: resolvedCallExprArgType(sig, sig.Params(), 0, false),
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

// CallExprSupportsInterfaceKwargs reports whether the call qualifier provides
// the factory used to lower keyword arguments for the named interface paramType.
func CallExprSupportsInterfaceKwargs(typeInfo *types.Info, expr *ast.CallExpr, paramType gotypes.Type) bool {
	_, ok := resolveInterfaceKwargBuilder(typeInfo, expr, paramType)
	return ok
}

// resolveInterfaceKwargBuilder returns the factory result when its methods
// can differ from the keyword interface. A nil result with true uses the
// interface itself.
func resolveInterfaceKwargBuilder(info *types.Info, expr *ast.CallExpr, paramType gotypes.Type) (gotypes.Type, bool) {
	self, ok := paramType.(*gotypes.Named)
	if !ok || info == nil {
		return nil, false
	}
	iface, ok := self.Underlying().(*gotypes.Interface)
	if !ok {
		return nil, false
	}
	selector, ok := CallExprFun(expr).(*ast.SelectorExpr)
	if !ok || !isAppendableKwargReceiver(selector.X) {
		return nil, false
	}
	factory, receiver := interfaceKwargFactory(info, selector.X, self.Obj().Name())
	if factory == nil {
		return nil, false
	}
	sig := resolveInterfaceKwargFactoryCall(info, factory, receiver)
	if sig == nil || sig.Results().Len() != 1 {
		return nil, false
	}
	builder := sig.Results().At(0).Type()
	if gotypes.Identical(builder, self) {
		return nil, true
	}
	if gotypes.AssignableTo(builder, self) {
		return builder, true
	}
	for method := range iface.Methods() {
		if target, ok := interfaceKwargMethodTarget(method, self); ok {
			if resolved, _ := resolveInterfaceBuilderTarget(info, builder, &target, nil); resolved != nil {
				return builder, true
			}
		}
	}
	if hasInterfaceKwargSet(iface, self) {
		target := &ResolvedCallExprKwargTarget{Kind: ResolvedCallExprKwargTargetInterfaceSet}
		if resolved, _ := resolveInterfaceBuilderTarget(info, builder, target, nil); resolved != nil {
			return builder, true
		}
	}
	return nil, false
}

// resolveInterfaceBuilderTarget describes the actual argument type at this
// step of a lowered keyword chain. The interface method retains the keyword's
// declaration identity.
func resolveInterfaceBuilderTarget(info *types.Info, builder gotypes.Type, target *ResolvedCallExprKwargTarget, value ast.Expr) (*ResolvedCallExprKwargTarget, gotypes.Type) {
	if target == nil {
		return nil, nil
	}
	name, valueIndex := "Set", 1
	if target.Method != nil {
		name, valueIndex = target.Method.Name(), 0
	}
	obj := lookupXGoMember(builder, name, info.Pkg, false)
	if obj == nil {
		return nil, nil
	}
	fun, _ := obj.(*gotypes.Func)
	if fun != nil && types.StaticMemberObject(fun) != nil {
		// A builder step calls a method on the previous result value.
		return nil, nil
	}
	var candidates iter.Seq2[gotypes.Object, *gotypes.Func]
	if fun != nil {
		candidates = OverloadImplementations(fun)
	}
	var valueType, result gotypes.Type
	if candidates != nil {
		receivers := interfaceKwargReceivers(fun, builder)
		valueType, result = interfaceBuilderOverloadTypes(info, candidates, receivers, valueIndex, value)
	} else {
		valueType, result = interfaceBuilderCallTypes(info, obj, gotypes.TypeAndValue{}, valueIndex, gotypes.TypeAndValue{})
	}
	if valueType == nil {
		return nil, nil
	}
	resolved := *target
	resolved.ValueType = valueType
	return &resolved, result
}

// interfaceBuilderOverloadTypes uses the first matching builder overload for
// a typed value. If the argument is unchecked, the result type must agree
// across the candidates before it can be used for the rest of the chain.
func interfaceBuilderOverloadTypes(info *types.Info, candidates iter.Seq2[gotypes.Object, *gotypes.Func], receivers []gotypes.TypeAndValue, valueIndex int, value ast.Expr) (gotypes.Type, gotypes.Type) {
	unit, _ := value.(*ast.NumberUnitLit)
	var actual gotypes.TypeAndValue
	if isType, _ := IsTypeExpr(info, value); !isType {
		actual = info.Types[value]
		actual.Type = info.TypeOf(value)
	}
	var want, result gotypes.Type
	for _, receiver := range receivers {
		for implementation, binding := range candidates {
			offset, typeAsParams := xgoFuncParamLayout(implementation, nil, binding)
			if typeAsParams && implementation.Type().Underlying().(*gotypes.Signature).TypeParams().Len() != 0 {
				continue
			}
			var bound gotypes.TypeAndValue
			if offset != 0 {
				bound = receiver
			}
			param, next := interfaceBuilderCallTypes(info, implementation, bound, valueIndex, actual)
			if param == nil {
				continue
			}
			if unit != nil {
				// The compiler lowers the unit separately for each candidate.
				if _, found := UnitSpecForType(param, unit.Unit); found {
					return param, next
				}
			} else if IsValidType(actual.Type) && CallValueTypeMatches(info, actual, param) {
				return param, next
			}
			if want == nil {
				want, result = param, next
				continue
			}
			if !gotypes.Identical(want, param) {
				want = anyType()
			}
			if result != nil && !gotypes.Identical(result, next) {
				result = nil
			}
		}
	}
	return want, result
}

// interfaceBuilderCallTypes returns the source value and result types for a
// builder method called with either a value or a string key and value.
func interfaceBuilderCallTypes(info *types.Info, obj gotypes.Object, receiver gotypes.TypeAndValue, valueIndex int, value gotypes.TypeAndValue) (gotypes.Type, gotypes.Type) {
	sig, ok := obj.Type().Underlying().(*gotypes.Signature)
	if !ok || sig.Results().Len() != 1 {
		return nil, nil
	}
	offset := 0
	if receiver.Type != nil {
		offset = 1
	}
	index := offset + valueIndex
	count := index + 1
	params := sig.Params()
	if params.Len() == 0 || RequiredParamCount(info, sig, params, 0) > count || !sig.Variadic() && params.Len() < count {
		return nil, nil
	}
	key := gotypes.TypeAndValue{Type: gotypes.Typ[gotypes.UntypedString]}
	if sig.TypeParams().Len() != 0 {
		var operands []gotypes.TypeAndValue
		if offset != 0 {
			operands = append(operands, receiver)
		}
		if valueIndex != 0 {
			operands = append(operands, key)
		}
		operands = append(operands, value)
		if instance := inferValueCallSignature(info, sig, operands...); instance != nil {
			sig, params = instance, instance.Params()
		}
	}
	if offset != 0 && !CallValueTypeMatches(info, receiver, firstCallValueParamType(sig)) {
		return nil, nil
	}
	if valueIndex != 0 && !CallValueTypeMatches(info, key, resolvedCallExprArgType(sig, params, min(offset, params.Len()-1), false)) {
		return nil, nil
	}
	return resolvedCallExprArgType(sig, params, min(index, params.Len()-1), false), sig.Results().At(0).Type()
}

// AfterKeywords returns the binding available after lowering the preceding
// keywords. A distinct factory builder can change the receiver at each step.
func (kwarg *ResolvedCallExprKwarg) AfterKeywords(preceding []*ast.KwargExpr) *ResolvedCallExprKwarg {
	if kwarg == nil || kwarg.interfaceBuilder == nil || len(preceding) == 0 {
		return kwarg
	}
	self := kwarg.ContainerType().(*gotypes.Named)
	iface := self.Underlying().(*gotypes.Interface)
	resolved := *kwarg
	for _, arg := range preceding {
		var result gotypes.Type
		if step := resolved.lowering.BuilderSteps[arg.Name.Pos()]; step != nil {
			result = resolved.info.TypeOf(step)
		} else {
			target := interfaceKwargTarget(iface, self, arg.Name.Name)
			_, result = resolveInterfaceBuilderTarget(resolved.info, resolved.interfaceBuilder, target, arg.Value)
		}
		if result == nil {
			return nil
		}
		if gotypes.Identical(result, self) {
			resolved.interfaceBuilder = nil
			break
		}
		resolved.interfaceBuilder = result
	}
	return &resolved
}

// resolveInterfaceKwargFactoryCall finds the first applicable zero-argument
// factory. Template method overloads are tried with the receiver before its address.
func resolveInterfaceKwargFactoryCall(info *types.Info, factory gotypes.Object, receiver gotypes.Type) *gotypes.Signature {
	if fun, ok := factory.(*gotypes.Func); ok {
		if candidates := OverloadImplementations(fun); candidates != nil {
			for _, recv := range interfaceKwargReceivers(fun, receiver) {
				for implementation, binding := range candidates {
					if sig := interfaceKwargFactorySignature(info, implementation, binding, recv); sig != nil {
						return sig
					}
				}
			}
			return nil
		}
	}
	return interfaceKwargFactorySignature(info, factory, nil, gotypes.TypeAndValue{})
}

// interfaceKwargReceivers follows the receiver order for reconstructed
// template methods in a lowered keyword chain.
func interfaceKwargReceivers(callable *gotypes.Func, receiver gotypes.Type) []gotypes.TypeAndValue {
	value := gotypes.TypeAndValue{Type: receiver}
	if offset, _ := xgoFuncParamLayout(callable, callable, nil); offset != 0 {
		return templateReceiverCandidates(value)
	}
	return []gotypes.TypeAndValue{value}
}

// interfaceKwargFactorySignature matches a factory candidate with no explicit
// arguments. A template method still passes its bound receiver to the function.
func interfaceKwargFactorySignature(info *types.Info, obj gotypes.Object, binding *gotypes.Func, receiver gotypes.TypeAndValue) *gotypes.Signature {
	sig, ok := obj.Type().Underlying().(*gotypes.Signature)
	if !ok {
		return nil
	}
	if _, extended := gogen.CheckFuncEx(sig); extended {
		return nil
	}
	if receiver.Type != nil {
		offset, typeAsParams := xgoFuncParamLayout(obj, nil, binding)
		if offset != 0 {
			if typeAsParams && sig.TypeParams().Len() != 0 {
				return nil
			}
			return singleValueCallSignature(info, sig, receiver, nil)
		}
	}
	if sig.TypeParams().Len() != 0 || RequiredParamCount(info, sig, sig.Params(), 0) != 0 {
		return nil
	}
	return sig
}

// interfaceKwargFactory looks up the factory on a package, type, or value.
// A value receiver can supply type arguments to a bound template method.
func interfaceKwargFactory(info *types.Info, qualifier ast.Expr, name string) (gotypes.Object, gotypes.Type) {
	if ident, ok := qualifier.(*ast.Ident); ok {
		if pkg, ok := info.ObjectOf(ident).(*gotypes.PkgName); ok {
			lookupName := UpperFirstASCII(name)
			switch factory := pkg.Imported().Scope().Lookup(lookupName).(type) {
			case *gotypes.Func:
				return factory, nil
			case *gotypes.Var:
				if lookupName == name || gogen.IsFunc(factory.Type()) {
					return factory, nil
				}
			}
			return nil, nil
		}
	}
	receiver := info.TypeOf(qualifier)
	if !IsValidType(receiver) {
		return nil, nil
	}
	if isType, _ := IsTypeExpr(info, qualifier); isType {
		return typeSelectorKeywordFactory(receiver, name, info.Pkg), nil
	}
	return lookupXGoMember(receiver, name, info.Pkg, false), receiver
}

// typeSelectorKeywordFactory resolves a factory selected from a type.
// Static values are returned directly. Static functions and template methods
// expose the underlying callable without binding a receiver.
func typeSelectorKeywordFactory(receiver gotypes.Type, name string, currentPkg *gotypes.Package) gotypes.Object {
	obj := lookupXGoMember(receiver, name, currentPkg, true)
	if method, ok := obj.(*gotypes.Func); ok {
		ext, _ := gogen.CheckFuncEx(method.Signature())
		switch member := ext.(type) {
		case *gogen.TyStaticMember:
			return member.Obj()
		case *gogen.TyTemplateRecvMethod:
			return member.Obj()
		}
		return nil
	}
	return obj
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
	fun, sig, params := ResolveCallExprSignature(typeInfo, expr)
	if sig == nil {
		return nil
	}

	params, _ = BindCallExprParams(typeInfo, expr, fun, sig, params, -1)
	return resolveCallExprKwarg(typeInfo, expr, sig, params, true)
}

// ResolveCallExprKwargAtArgCount finds the keyword parameter for a candidate
// signature and the given number of written positional arguments.
func ResolveCallExprKwargAtArgCount(info *types.Info, call *ast.CallExpr, sig *gotypes.Signature, params *gotypes.Tuple, argCount int) *ResolvedCallExprKwarg {
	return resolveCallExprKwargForSignature(info, call, sig, params, argCount, true, false)
}

// resolveCallExprKwargForSignature finds the slot before interpreting the
// keyword container. A call without written keywords requires an optional slot.
func resolveCallExprKwargForSignature(info *types.Info, call *ast.CallExpr, sig *gotypes.Signature, params *gotypes.Tuple, argCount int, allowRequired, useCheckedKwargs bool) *ResolvedCallExprKwarg {
	usesSignature := callExprUsesSignatureForLowering(info, call)
	argIndex := argCount
	if usesSignature {
		argIndex = keywordArgIndex(info, call, sig, params, argCount)
	}
	nominal := argIndex
	var lowering types.KeywordLowering
	var container, operand gotypes.Type
	if useCheckedKwargs && len(call.Kwargs) != 0 {
		lowering = info.KeywordArgs[call.Kwargs[len(call.Kwargs)-1]]
		operand = info.Types[lowering.Arg].Type
		container = operand
		if lowering.Factory != nil {
			container = keywordBuilderType(info, call, sig, params, lowering)
		}
	}
	recorded := lowering.Arg != nil && (lowering.Factory == nil || container != nil)
	if recorded && usesSignature {
		argIndex = checkedKeywordArgIndex(info, call, sig, params, container, operand, argIndex)
	}
	index := resolvedCallExprParamIndex(sig, params, argIndex)
	if index < 0 || argIndex > argCount {
		return nil
	}

	param := params.At(index)
	if !allowRequired && !isOptionalParam(info, param) {
		return nil
	}
	kwarg := &ResolvedCallExprKwarg{
		Param:          param,
		ParamIndex:     index,
		info:           info,
		argIndex:       argIndex,
		appendKeywords: !usesSignature,
	}
	typ := param.Type()
	if sig.Variadic() && index == params.Len()-1 && !call.Ellipsis.IsValid() {
		typ = variadicValueType(typ)
	}
	// A partial builder record applies only to the parameter that created it.
	if recorded || container != nil && gotypes.Identical(container, typ) {
		kwarg.lowering = lowering
	}
	switch {
	case recorded && container != nil:
		typ = container
	case recorded || !usesSignature:
		kwarg.inferredMapValueType = cmp.Or(inferredKeywordMapValueType(info, typ), anyType())
		return kwarg
	}
	kwarg.setContainerType(call, typ)
	if usesSignature && sig.Variadic() && index == params.Len()-1 && argIndex == nominal {
		// A bound template places the keyword at the raw signature's last
		// fixed parameter, then checks it against the visible variadic tail.
		if spelling := keywordLoweringParamType(info, call, sig, params, argIndex); spelling != nil && !matchesLoweredKeywordType(spelling, typ) {
			if shape, _ := structKwargType(spelling, kwarg.currentPackage()); shape != nil {
				kwarg.spellingType = spelling
			}
		}
	}
	return kwarg
}

// keywordArgIndex returns the operand position used to lower keywords before
// binding a template receiver or type arguments.
func keywordArgIndex(info *types.Info, call *ast.CallExpr, sig *gotypes.Signature, params *gotypes.Tuple, argCount int) int {
	if sig.Variadic() {
		return callExprLoweringParams(info, call, sig, params).Len() - 2
	}
	return argCount
}

// keywordBuilderType identifies the protocol that supplied the recorded
// factory. Its result can be a concrete builder used through an initializer.
func keywordBuilderType(info *types.Info, call *ast.CallExpr, sig *gotypes.Signature, params *gotypes.Tuple, lowering types.KeywordLowering) gotypes.Type {
	selector, ok := lowering.Factory.Fun.(*ast.SelectorExpr)
	if !ok {
		return nil
	}
	factory := selector.Sel.Name
	paramType := func(sig *gotypes.Signature, params *gotypes.Tuple) *gotypes.Named {
		if sig == nil {
			return nil
		}
		slot := keywordArgIndex(info, call, sig, params, len(call.Args))
		return keywordBuilderInterface(keywordLoweringParamType(info, call, sig, params, slot), factory, call.Kwargs, lowering.BuilderSteps)
	}
	if candidates := CallExprOverloadImplementations(info, call); candidates != nil {
		var found *gotypes.Named
		for implementation, binding := range candidates {
			if typ := paramType(ResolveCallableSignatureForCall(info, call, implementation, binding)); typ != nil {
				if found != nil && !gotypes.Identical(found, typ) {
					return nil
				}
				found = typ
			}
		}
		if found != nil {
			return found
		}
	}
	if named := paramType(sig, params); named != nil {
		return named
	}
	for _, expr := range [...]ast.Expr{lowering.Factory, lowering.Arg} {
		typ := gotypes.Unalias(info.Types[expr].Type)
		if named := keywordBuilderInterface(typ, factory, call.Kwargs, lowering.BuilderSteps); named != nil {
			return named
		}
	}
	return nil
}

// keywordBuilderInterface matches a factory name and the compiler's choice of
// a direct method or Set for each checked keyword.
func keywordBuilderInterface(typ gotypes.Type, factory string, args []*ast.KwargExpr, steps map[token.Pos]*ast.CallExpr) *gotypes.Named {
	named, ok := typ.(*gotypes.Named)
	if !ok || named.Obj().Name() != factory {
		return nil
	}
	iface, ok := named.Underlying().(*gotypes.Interface)
	if !ok {
		return nil
	}
	for _, arg := range args {
		if step := steps[arg.Name.Pos()]; step != nil {
			target := interfaceKwargTarget(iface, named, arg.Name.Name)
			direct := len(step.Args) == 1
			if target == nil || (target.Method != nil) != direct {
				return nil
			}
		}
	}
	return named
}

// keywordLoweringParamType returns the type used to choose keyword syntax.
// For a variadic template it can differ from the type receiving the result.
func keywordLoweringParamType(info *types.Info, call *ast.CallExpr, sig *gotypes.Signature, params *gotypes.Tuple, slot int) gotypes.Type {
	if sig.Variadic() {
		params = callExprLoweringParams(info, call, sig, params)
	}
	if slot < 0 || slot > len(call.Args) || slot >= params.Len() {
		return nil
	}
	return params.At(slot).Type()
}

// checkedKeywordArgIndex finds the placement shared by the candidates that
// could have supplied the compiler-recorded container. The invoked overload
// can have a different parameter layout.
func checkedKeywordArgIndex(info *types.Info, call *ast.CallExpr, selected *gotypes.Signature, selectedParams *gotypes.Tuple, lowered, operand gotypes.Type, nominal int) int {
	argCount := len(call.Args)
	if argCount == 0 {
		return 0
	}
	candidates := CallExprOverloadImplementations(info, call)
	if candidates == nil {
		return nominal
	}
	index := -1
	ellipsis := call.Ellipsis.IsValid()
	for implementation, binding := range candidates {
		sig, params := ResolveCallableSignatureForCall(info, call, implementation, binding)
		if sig == nil {
			continue
		}
		slot := keywordArgIndex(info, call, sig, params, argCount)
		candidateParam := resolvedCallExprParamIndex(sig, params, slot)
		actualParam := resolvedCallExprParamIndex(selected, selectedParams, slot)
		if slot > argCount || candidateParam < 0 || actualParam < 0 {
			continue
		}
		candidateType := resolvedCallExprArgType(sig, params, candidateParam, ellipsis)
		if lowered != nil && gotypes.IsInterface(lowered) {
			candidateType = keywordLoweringParamType(info, call, sig, params, slot)
		}
		if candidateType == nil || !matchesLoweredKeywordType(candidateType, lowered) {
			continue
		}
		if operand != nil {
			want := resolvedCallExprArgType(selected, selectedParams, actualParam, ellipsis)
			if !CallValueTypeMatches(info, gotypes.TypeAndValue{Type: operand}, want) {
				continue
			}
		}
		if index >= 0 && index != slot {
			return nominal
		}
		index = slot
	}
	if index < 0 {
		return nominal
	}
	return index
}

// resolvedCallExprParamIndex maps an operand position to a parameter, sharing
// the last parameter across variadic operands.
func resolvedCallExprParamIndex(sig *gotypes.Signature, params *gotypes.Tuple, index int) int {
	if sig.Variadic() {
		return min(index, params.Len()-1)
	}
	if index >= params.Len() {
		return -1
	}
	return index
}

// matchesLoweredKeywordType preserves alias identity because the compiler uses
// it to decide whether unexported struct fields are available.
func matchesLoweredKeywordType(param, lowered gotypes.Type) bool {
	if lowered == nil {
		return usesInferredKeywordMap(param)
	}
	if param == lowered {
		return true
	}
	_, paramAlias := param.(*gotypes.Alias)
	_, loweredAlias := lowered.(*gotypes.Alias)
	return !paramAlias && !loweredAlias && gotypes.Identical(param, lowered)
}

// setContainerType configures the keyword interpretation for the container type.
func (kwarg *ResolvedCallExprKwarg) setContainerType(call *ast.CallExpr, typ gotypes.Type) {
	kwarg.containerType = typ
	if factory := kwarg.lowering.Factory; factory != nil {
		if builder := kwarg.info.TypeOf(factory); IsValidType(builder) {
			kwarg.AllowInterfaceTargets = true
			if !gotypes.Identical(builder, typ) {
				kwarg.interfaceBuilder = builder
			}
			return
		}
	}
	kwarg.interfaceBuilder, kwarg.AllowInterfaceTargets = resolveInterfaceKwargBuilder(kwarg.info, call, typ)
	if usesInferredKeywordMap(typ) {
		kwarg.inferredMapValueType = inferredKeywordMapValueType(kwarg.info, typ)
	}
}

// usesInferredKeywordMap reports whether the compiler builds a string-keyed
// map without a container type, even when it knows the parameter.
func usesInferredKeywordMap(typ gotypes.Type) bool {
	switch u := typ.Underlying().(type) {
	case *gotypes.Map, *gotypes.Struct:
		return false
	case *gotypes.Pointer:
		_, isStruct := u.Elem().Underlying().(*gotypes.Struct)
		return !isStruct
	case *gotypes.Interface:
		_, named := typ.(*gotypes.Named)
		return !named
	default:
		return true
	}
}

// inferredKeywordMapValueType describes the value expected by a parameter
// or its initializer when the compiler first infers a string-keyed map.
func inferredKeywordMapValueType(info *types.Info, typ gotypes.Type) gotypes.Type {
	elem := stringKeyedMapElement(typ)
	init := TypeInitializer(typ)
	if init == nil {
		return elem
	}

	for sig := range callInitializerSignatures(init) {
		candidate := initializerKeywordMapElement(info, sig)
		if candidate == nil {
			continue
		}
		if elem != nil && !gotypes.Identical(elem, candidate) {
			return anyType()
		}
		elem = candidate
	}
	return elem
}

// initializerKeywordMapElement describes an initializer that could receive
// an inferred keyword map. Generic or unconstrained inputs keep the element
// open when the signature supplies no fixed type.
func initializerKeywordMapElement(info *types.Info, sig *gotypes.Signature) gotypes.Type {
	if _, extended := gogen.CheckFuncEx(sig); extended || sig.Params().Len() == 0 ||
		RequiredParamCount(info, sig, sig.Params(), 0) > 1 {
		return nil
	}
	param := firstCallValueParamType(sig)
	if TypeInitializer(param) != nil {
		return anyType()
	}
	if elem := stringKeyedMapElement(param); elem != nil {
		if sig.TypeParams().Len() == 0 {
			return elem
		}
		return anyType()
	}
	if _, typeParam := param.(*gotypes.TypeParam); typeParam {
		return anyType()
	}
	switch underlying := param.Underlying().(type) {
	case *gotypes.Interface:
		if underlying.Empty() {
			return anyType()
		}
	case *gotypes.Map:
		if _, typeParam := underlying.Key().(*gotypes.TypeParam); typeParam {
			return anyType()
		}
	}
	return nil
}

// stringKeyedMapElement recognizes a map whose key is the string type used
// by an inferred keyword map.
func stringKeyedMapElement(typ gotypes.Type) gotypes.Type {
	if m, ok := typ.Underlying().(*gotypes.Map); ok && gotypes.Identical(m.Key(), gotypes.Typ[gotypes.String]) {
		return m.Elem()
	}
	return nil
}

// resolveCallExprKwarg returns the parameter slot that receives keyword
// arguments after the call signature has already been resolved.
func resolveCallExprKwarg(typeInfo *types.Info, expr *ast.CallExpr, sig *gotypes.Signature, params *gotypes.Tuple, useCheckedKwargs bool) *ResolvedCallExprKwarg {
	return resolveCallExprKwargForSignature(typeInfo, expr, sig, params, len(expr.Args), len(expr.Kwargs) != 0, useCheckedKwargs)
}

// LookupResolvedCallExprKwargTarget resolves a keyword name against the target
// parameter slot.
func LookupResolvedCallExprKwargTarget(kwarg *ResolvedCallExprKwarg, name string) *ResolvedCallExprKwargTarget {
	return kwarg.lookupTarget(name, nil, nil)
}

// KeywordTarget resolves a written keyword, including builder overloads
// that use its value to select a signature.
func (kwarg *ResolvedCallExprKwarg) KeywordTarget(arg *ast.KwargExpr) *ResolvedCallExprKwargTarget {
	if kwarg == nil {
		return nil
	}
	if lit, ok := kwarg.lowering.Arg.(*ast.CompositeLit); ok {
		if field := loweredKeywordField(kwarg.info, lit, arg); field != nil {
			target := structKwargTarget(field)
			return &target
		}
	}
	return kwarg.lookupTarget(arg.Name.Name, arg.Value, kwarg.lowering.BuilderSteps[arg.Name.Pos()])
}

// loweredKeywordField returns the field recorded for the synthesized key.
func loweredKeywordField(info *types.Info, lit *ast.CompositeLit, arg *ast.KwargExpr) *gotypes.Var {
	i, ok := slices.BinarySearchFunc(lit.Elts, arg.Name.Pos(), func(elt ast.Expr, pos token.Pos) int {
		return cmp.Compare(elt.Pos(), pos)
	})
	if !ok {
		return nil
	}
	key := lit.Elts[i].(*ast.KeyValueExpr).Key
	if ident, ok := key.(*ast.Ident); ok {
		if field, ok := info.Uses[ident].(*gotypes.Var); ok && field.IsField() {
			return field
		}
	}
	return nil
}

// lookupTarget resolves a keyword against the current receiver.
func (kwarg *ResolvedCallExprKwarg) lookupTarget(name string, value ast.Expr, step *ast.CallExpr) *ResolvedCallExprKwargTarget {
	if kwarg == nil || name == "" {
		return nil
	}
	if kwarg.inferredMapValueType != nil {
		return mapKwargTarget(name, kwarg.inferredMapValueType)
	}

	paramType := kwarg.ContainerType()
	if strct, localPkg := structKwargType(paramType, kwarg.currentPackage()); strct != nil {
		field := kwarg.lookupStructField(strct, localPkg, name)
		if field == nil {
			return nil
		}
		target := structKwargTarget(field)
		return &target
	}

	switch u := paramType.Underlying().(type) {
	case *gotypes.Interface:
		return lookupInterfaceKwargTarget(kwarg, u, name, value, step)
	case *gotypes.Map:
		if acceptsStringLiteral(u.Key()) {
			return mapKwargTarget(name, u.Elem())
		}
	}
	return nil
}

// lookupStructField applies the lowering's name rules before looking up the
// field on the struct that receives the literal.
func (kwarg *ResolvedCallExprKwarg) lookupStructField(strct *gotypes.Struct, localPkg *gotypes.Package, name string) *gotypes.Var {
	capitalized := capitalizeStructKeyword(name)
	if capitalized == "" {
		return nil
	}
	if kwarg.spellingType == nil {
		return lookupStructKwargField(strct, localPkg, name, capitalized)
	}
	shape, shapePkg := structKwargType(kwarg.spellingType, kwarg.currentPackage())
	if shapePkg == nil {
		name = capitalized
	} else if field := lookupStructKwargField(shape, shapePkg, name, capitalized); field != nil {
		name = field.Name()
	}
	for field := range strct.Fields() {
		if field.Name() == name && (field.Exported() || field.Pkg() == kwarg.currentPackage()) {
			return field
		}
	}
	return nil
}

// lookupInterfaceKwargTarget resolves name against an interface kwarg parameter.
func lookupInterfaceKwargTarget(kwarg *ResolvedCallExprKwarg, iface *gotypes.Interface, name string, value ast.Expr, step *ast.CallExpr) *ResolvedCallExprKwargTarget {
	named, ok := kwarg.ContainerType().(*gotypes.Named)
	if !ok {
		if iface.Empty() {
			return mapKwargTarget(name, anyType())
		}
		return nil
	}

	if !kwarg.AllowInterfaceTargets {
		return nil
	}
	target := interfaceKwargTarget(iface, named, name)
	if target != nil && step != nil {
		for arg := range ResolvedCallExprArgs(kwarg.info, step) {
			if arg.Arg == value {
				target.ValueType = arg.ExpectedType
				return target
			}
		}
	}
	if kwarg.interfaceBuilder != nil {
		target, _ = resolveInterfaceBuilderTarget(kwarg.info, kwarg.interfaceBuilder, target, value)
	}
	return target
}

// interfaceKwargTarget maps a keyword name to its method in the interface
// protocol or to its dynamic Set fallback.
func interfaceKwargTarget(iface *gotypes.Interface, named *gotypes.Named, name string) *ResolvedCallExprKwargTarget {
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

// currentPackage returns the package in which the keyword argument is written.
func (kwarg *ResolvedCallExprKwarg) currentPackage() *gotypes.Package {
	if kwarg.info == nil {
		return nil
	}
	return kwarg.info.Pkg
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
	if kwarg == nil || kwarg.inferredMapValueType != nil {
		return nil
	}

	var targets []ResolvedCallExprKwargTarget
	appendTarget := func(target ResolvedCallExprKwargTarget) bool {
		resolved := LookupResolvedCallExprKwargTarget(kwarg, target.Name)
		if !sameResolvedCallExprKwargTarget(resolved, target) {
			return false
		}
		resolved.Name = target.Name
		targets = append(targets, *resolved)
		return true
	}
	paramType := kwarg.ContainerType()
	if strct, localPkg := structKwargType(paramType, kwarg.currentPackage()); strct != nil {
		for field := range strct.Fields() {
			if !field.Exported() && (localPkg == nil || field.Pkg() != localPkg) {
				continue
			}
			target := structKwargTarget(field)
			if !appendTarget(target) && target.Name != field.Name() {
				// A local field can claim the lowercase spelling first.
				target.Name = field.Name()
				appendTarget(target)
			}
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

// CallExprArgs returns positional arguments after tuple expansion using the
// supplied fixed parameters and reports whether the final argument uses
// ellipsis. Use [CallExprArgsForSignature] for a resolved call.
func CallExprArgs(typeInfo *types.Info, expr *ast.CallExpr, params *gotypes.Tuple) ([]ast.Expr, bool) {
	return CallExprArgsForSignature(typeInfo, expr, nil, params)
}

// CallExprArgsForSignature returns positional arguments after XGo tuple
// expansion and reports whether the final argument uses ellipsis.
// params holds the source-visible parameters for sig.
func CallExprArgsForSignature(typeInfo *types.Info, expr *ast.CallExpr, sig *gotypes.Signature, params *gotypes.Tuple) ([]ast.Expr, bool) {
	ellipsis := expr.Ellipsis.IsValid()
	if typeInfo != nil && typeInfo.FuncDecorators[expr] {
		return expr.Args, ellipsis
	}
	if len(expr.Args) != 1 || len(expr.Kwargs) != 0 || ellipsis {
		return expr.Args, ellipsis
	}
	tuple, ok := expr.Args[0].(*ast.TupleLit)
	if !ok {
		return expr.Args, ellipsis
	}
	if tuple.Ellipsis.IsValid() || !callExprKeepsTupleArgument(typeInfo, expr, sig, params) {
		return tuple.Elts, tuple.Ellipsis.IsValid()
	}
	return expr.Args, ellipsis
}

// callExprKeepsTupleArgument mirrors the compiler's check for a single fixed
// tuple parameter before receiver binding and type argument inference.
func callExprKeepsTupleArgument(info *types.Info, call *ast.CallExpr, sig *gotypes.Signature, params *gotypes.Tuple) bool {
	if sig != nil {
		if !callExprUsesSignatureForLowering(info, call) {
			return false
		}
		params = callExprLoweringParams(info, call, sig, params)
	}
	fixed := params.Len()
	if sig != nil && sig.Variadic() {
		fixed--
	}
	return fixed == 1 && new(gogen.CodeBuilder).IsTupleType(params.At(0).Type())
}

// callExprLoweringParams returns the compiler's parameters before receiver
// binding and type argument inference.
func callExprLoweringParams(info *types.Info, call *ast.CallExpr, sig *gotypes.Signature, params *gotypes.Tuple) *gotypes.Tuple {
	if sig.Recv() == nil || params.Len() <= sig.Params().Len() || callExprUsesOverloadedMethod(info, call) {
		return sig.Params()
	}
	return params
}

// callExprUsesSignatureForLowering reports whether the compiler uses parameter
// types before lowering source arguments. Named function types and pending
// inference types provide no parameter types at that point.
func callExprUsesSignatureForLowering(info *types.Info, call *ast.CallExpr) bool {
	if info == nil {
		return true
	}
	typ := info.Types[CallExprFun(call)].Type
	_, concrete := typ.(*gotypes.Signature)
	return concrete || !IsValidType(typ)
}

// callExprUsesOverloadedMethod reports whether the compiler sees an overload
// group. It counts the explicit receiver only for ordinary method expressions.
func callExprUsesOverloadedMethod(info *types.Info, call *ast.CallExpr) bool {
	if info == nil {
		return false
	}
	fun, ok := info.SourceObjectOf(CallExprFunIdent(call)).(*gotypes.Func)
	if !ok {
		return false
	}
	ext, _ := gogen.CheckFuncEx(fun.Signature())
	_, ok = ext.(*gogen.TyOverloadMethod)
	return ok
}

// ResolvedCallExprArgs returns positional arguments and keyword argument values
// mapped to the selected call signature.
func ResolvedCallExprArgs(typeInfo *types.Info, expr *ast.CallExpr) iter.Seq[ResolvedCallExprArg] {
	fun, sig, params := ResolveCallExprSignature(typeInfo, expr)
	params, _ = BindCallExprParams(typeInfo, expr, fun, sig, params, -1)
	return resolvedCallExprArgs(typeInfo, expr, fun, sig, params, nil, true)
}

// ResolvedCallExprArgsForFunc maps arguments to a specific overload candidate.
func ResolvedCallExprArgsForFunc(typeInfo *types.Info, expr *ast.CallExpr, fun *gotypes.Func) iter.Seq[ResolvedCallExprArg] {
	sig, params := ResolveFuncSignatureForCall(typeInfo, expr, fun)
	params, _ = BindCallExprParams(typeInfo, expr, fun, sig, params, -1)
	return resolvedCallExprArgs(typeInfo, expr, fun, sig, params, nil, false)
}

// ResolvedCallExprArgsForSignature maps arguments using an already resolved
// signature and its source-visible parameters. Use [BindCallExprParams] to
// apply explicit type arguments before mapping.
func ResolvedCallExprArgsForSignature(typeInfo *types.Info, expr *ast.CallExpr, fun *gotypes.Func, sig *gotypes.Signature, params *gotypes.Tuple) iter.Seq[ResolvedCallExprArg] {
	return resolvedCallExprArgs(typeInfo, expr, fun, sig, params, nil, false)
}

// SourceExprIndex finds target in expressions ordered by source position.
// Nodes sharing a position are distinguished by identity.
func SourceExprIndex(exprs []ast.Expr, target ast.Expr) int {
	start, _ := slices.BinarySearchFunc(exprs, target.Pos(), func(expr ast.Expr, pos token.Pos) int {
		return cmp.Compare(expr.Pos(), pos)
	})
	for i := start; i < len(exprs) && exprs[i].Pos() == target.Pos(); i++ {
		if exprs[i] == target {
			return i
		}
	}
	return -1
}

// ResolveCallExprArg resolves the expected type for target. Its surrounding
// arguments can supply inference evidence for that type.
func ResolveCallExprArg(typeInfo *types.Info, expr *ast.CallExpr, target ast.Expr) (ResolvedCallExprArg, bool) {
	fun, sig, params := ResolveCallExprSignature(typeInfo, expr)
	for arg := range resolvedCallExprArgs(typeInfo, expr, fun, sig, params, target, true) {
		bound, _ := BindCallExprParams(typeInfo, expr, fun, sig, params, arg.ArgIndex)
		if bound == params {
			return arg, true
		}
		for contextual := range resolvedCallExprArgs(typeInfo, expr, fun, sig, bound, target, true) {
			return contextual, true
		}
	}
	return ResolvedCallExprArg{}, false
}

// resolvedCallExprArgs maps source arguments to a resolved signature.
func resolvedCallExprArgs(typeInfo *types.Info, expr *ast.CallExpr, fun *gotypes.Func, sig *gotypes.Signature, params *gotypes.Tuple, target ast.Expr, useCheckedKwargs bool) iter.Seq[ResolvedCallExprArg] {
	return func(yield func(ResolvedCallExprArg) bool) {
		if sig == nil {
			return
		}

		var kwarg *ResolvedCallExprKwarg
		if len(expr.Kwargs) > 0 {
			kwarg = resolveCallExprKwarg(typeInfo, expr, sig, params, useCheckedKwargs)
		}
		args, ellipsis := CallExprArgsForSignature(typeInfo, expr, sig, params)
		start, end := 0, len(args)
		if target != nil {
			start = SourceExprIndex(args, target)
			end = start
			if start >= 0 {
				end++
			}
		}
		for i := start; i < end; i++ {
			arg := args[i]
			argEllipsis := ellipsis && i == len(args)-1
			paramIndex := i
			if kwarg != nil && i >= kwarg.argIndex {
				paramIndex++
			}
			paramIndex = resolvedCallExprParamIndex(sig, params, paramIndex)
			if paramIndex < 0 {
				break
			}

			if !yield(ResolvedCallExprArg{
				Fun:          fun,
				Signature:    sig,
				Params:       params,
				Param:        params.At(paramIndex),
				ParamIndex:   paramIndex,
				Arg:          arg,
				ArgIndex:     i,
				Kind:         ResolvedCallExprArgPositional,
				ExpectedType: resolvedCallExprArgType(sig, params, paramIndex, argEllipsis),
			}) {
				return
			}
		}

		if kwarg == nil {
			return
		}

		binding, previous := kwarg, 0
		for i, arg := range expr.Kwargs {
			if target != nil && arg.Value != target {
				continue
			}
			binding = binding.AfterKeywords(expr.Kwargs[previous:i])
			previous = i
			kwargTarget := binding.KeywordTarget(arg)
			var expectedType gotypes.Type
			if kwargTarget != nil {
				expectedType = kwargTarget.ValueType
			}
			if !yield(ResolvedCallExprArg{
				Fun:          fun,
				Signature:    sig,
				Params:       params,
				Param:        kwarg.Param,
				ParamIndex:   kwarg.ParamIndex,
				Arg:          arg.Value,
				ArgIndex:     len(args) + i,
				Kind:         ResolvedCallExprArgKeyword,
				Kwarg:        arg,
				ExpectedType: expectedType,
				KwargTarget:  kwargTarget,
			}) {
				return
			}
		}
	}
}
