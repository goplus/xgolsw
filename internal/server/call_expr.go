/*
 * Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
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

package server

import (
	gotypes "go/types"
	"iter"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// callableExprForIdent returns the callable spelling around ident, preserving
// its selector and explicit type arguments.
func callableExprForIdent(file *ast.File, ident *ast.Ident) (ast.Expr, *ast.SelectorExpr) {
	var callee ast.Expr = ident
	var selector *ast.SelectorExpr
	for node := range xgoutil.PathEnclosingIntervalNodes(file, ident.Pos(), ident.End(), false) {
		expr, ok := node.(ast.Expr)
		if !ok || xgoutil.CallExprFunIdent(&ast.CallExpr{Fun: expr}) != ident {
			break
		}
		callee = expr
		if sel, ok := expr.(*ast.SelectorExpr); ok {
			selector = sel
		}
	}
	return callee, selector
}

// callImplementationForIdent returns the implementation when ident names the
// callee of an explicit or implicit call.
func callImplementationForIdent(info *types.Info, file *ast.File, ident *ast.Ident) gotypes.Object {
	if selected := info.SelectedOverloads[ident]; selected.Implementation != nil {
		return selected.Implementation
	}
	for node := range xgoutil.PathEnclosingIntervalNodes(file, ident.Pos(), ident.End(), false) {
		call := callExprFromNode(info, node)
		if call == nil {
			continue
		}
		if xgoutil.CallExprFunIdent(call) == ident {
			return xgoutil.CallExprImplementation(info, call)
		}
		break
	}
	return nil
}

// callExprFromNode returns the call expression represented by node.
func callExprFromNode(typeInfo *types.Info, node ast.Node) *ast.CallExpr {
	switch node := node.(type) {
	case *ast.CallExpr:
		return node
	case *ast.FuncDecorator:
		return &node.CallExpr
	case *ast.BranchStmt:
		return xgoutil.CreateCallExprFromBranchStmt(typeInfo, node)
	default:
		return nil
	}
}

// funcDecoratorParams returns the source-visible parameters of a valid
// decorator function signature.
func funcDecoratorParams(sig *gotypes.Signature) (*gotypes.Tuple, bool) {
	params := sig.Params()
	if params.Len() == 0 {
		return nil, false
	}

	fn, ok := gotypes.Unalias(params.At(params.Len() - 1).Type()).(*gotypes.Signature)
	if !ok || fn.Params().Len() != 0 {
		return nil, false
	}
	switch fn.Results().Len() {
	case 0:
	case 1:
		errorType := gotypes.Universe.Lookup("error").Type()
		if gotypes.Unalias(fn.Results().At(0).Type()) != errorType {
			return nil, false
		}
	default:
		return nil, false
	}

	visible := make([]*gotypes.Var, params.Len()-1)
	for i := range visible {
		visible[i] = params.At(i)
	}
	return gotypes.NewTuple(visible...), true
}

// callArgValueTypes yields call argument values and their resolved target types,
// including elements of XGo collection and tuple literals.
func callArgValueTypes(typeInfo *types.Info, call *ast.CallExpr) iter.Seq2[ast.Expr, gotypes.Type] {
	return func(yield func(ast.Expr, gotypes.Type) bool) {
		for expr, typ := range builtinArgValueTypes(typeInfo, call) {
			if !yield(astutil.Unparen(expr), typ) {
				return
			}
		}
		for arg := range resolvedCallExprArgs(typeInfo, call) {
			for expr, typ := range valueElementTypes(typeInfo, arg.Arg, arg.ExpectedType) {
				if !yield(astutil.Unparen(expr), typ) {
					return
				}
			}
		}
	}
}

// callOverload is one implementation and its source-visible call signature.
type callOverload struct {
	object    gotypes.Object
	signature *gotypes.Signature
	params    *gotypes.Tuple
}

// function returns the implementation's function declaration, if it has one.
func (o callOverload) function() *gotypes.Func {
	fun, _ := o.object.(*gotypes.Func)
	return fun
}

// args maps the written arguments to the candidate's parameters.
func (o callOverload) args(info *types.Info, call *ast.CallExpr) iter.Seq[xgoutil.ResolvedCallExprArg] {
	return xgoutil.ResolvedCallExprArgsForSignature(info, call, o.function(), o.signature, o.params)
}

// bindTypeArgs resolves the candidate's value parameter types from the call.
func (o callOverload) bindTypeArgs(info *types.Info, call *ast.CallExpr, skipArgIndex int) (callOverload, bool) {
	params, ok := xgoutil.BindCallExprParams(info, call, o.function(), o.signature, o.params, skipArgIndex)
	o.params = params
	return o, ok
}

// expandsCallResults reports whether a single written argument can supply
// several values to this signature.
func (o callOverload) expandsCallResults(info *types.Info, call *ast.CallExpr) bool {
	// Inference runs on the written argument before gogen expands its results.
	if o.signature.TypeParams().Len() != 0 {
		return false
	}
	count := o.params.Len()
	variadic := o.signature.Variadic()
	if (count < 2 && !variadic) ||
		xgoutil.NormalizedCallExprTypeArgCount(o.function(), o.params) != 0 {
		return false
	}
	// Bound templates insert a receiver before the written argument.
	if _, bound := xgoutil.CallExprTemplateReceiver(info, call, o.function(), o.signature, o.params); bound {
		return false
	}
	if _, autoclosure := xgoutil.AutoclosureParamResultType(o.params.At(0)); autoclosure {
		return false
	}
	// Gogen adds missing optional arguments before checking for a sole tuple.
	fixed := count
	if variadic {
		fixed--
	}
	return fixed <= 1 || xgoutil.RequiredParamCount(info, o.signature, o.params, 0) > 1
}

// acceptsAnyCallResults reports whether a variadic signature imposes no
// constraints on the types or number of a nested call's results.
func (o callOverload) acceptsAnyCallResults() bool {
	if !o.signature.Variadic() || o.params.Len() != 1 {
		return false
	}
	slice, ok := o.params.At(0).Type().(*gotypes.Slice)
	return ok && gotypes.AssignableTo(gotypes.Universe.Lookup("any").Type(), slice.Elem())
}

// matchesCallResults checks the results of the sole argument against this
// candidate's parameter list. Expanded results use Go parameter types.
func (o callOverload) matchesCallResults(info *types.Info, call *ast.CallExpr, results *gotypes.Tuple) bool {
	count := results.Len()
	if count == 0 || o.params.Len() == 0 || xgoutil.NormalizedCallExprTypeArgCount(o.function(), o.params) != 0 {
		return false
	}
	expanded := count > 1
	if expanded {
		if !o.expandsCallResults(info, call) || (!o.signature.Variadic() && count != o.params.Len()) ||
			(o.signature.Variadic() && count < o.params.Len()-1) {
			return false
		}
	} else if xgoutil.RequiredParamCount(info, o.signature, o.params, 0) > 1 {
		return false
	}
	for i := range count {
		want := callExprParamType(o.signature, o.params, i, expanded)
		if !xgoutil.CallValueTypeMatches(info, gotypes.TypeAndValue{Type: results.At(i).Type()}, want) {
			return false
		}
	}
	return true
}

// callExprOverloads returns the resolved candidates of a reconstructed
// overload or static callable, in compiler resolution order.
func callExprOverloads(typeInfo *types.Info, callExpr *ast.CallExpr) []callOverload {
	implementations := xgoutil.CallExprOverloadImplementations(typeInfo, callExpr)
	if implementations == nil {
		return nil
	}
	var overloads []callOverload
	for obj, binding := range implementations {
		if sig, params := xgoutil.ResolveCallableSignatureForCall(typeInfo, callExpr, obj, binding); sig != nil {
			overloads = append(overloads, callOverload{object: obj, signature: sig, params: params})
		}
	}
	return overloads
}

// matchArgs checks the call against the candidate and returns the bound
// parameters and the expected type of the edited argument. A nil edited
// expression checks every argument. Each candidate resolves the argument position.
func (o callOverload) matchArgs(typeInfo *types.Info, call *ast.CallExpr, edited ast.Expr) (callOverload, gotypes.Type, bool) {
	args, ellipsis := xgoutil.CallExprArgsForSignature(typeInfo, call, o.signature, o.params)
	if ellipsis {
		count := len(args)
		if len(call.Kwargs) > 0 {
			count++ // Keyword arguments share one parameter slot.
		}
		if !o.signature.Variadic() || count != o.params.Len() {
			return o, nil, false
		}
	}
	skipArgIndex := -1
	if edited != nil {
		skipArgIndex = xgoutil.SourceExprIndex(args, edited)
		if skipArgIndex < 0 {
			if i := slices.IndexFunc(call.Kwargs, func(kwarg *ast.KwargExpr) bool { return kwarg.Value == edited }); i >= 0 {
				skipArgIndex = len(args) + i
			}
		}
	}
	if typeInfo != nil && skipArgIndex != 0 && len(args) == 1 && len(call.Kwargs) == 0 && !ellipsis {
		if results, ok := typeInfo.TypeOf(args[0]).(*gotypes.Tuple); ok && results.Len() > 1 {
			return o, nil, o.matchesCallResults(typeInfo, call, results)
		}
	}
	bound, ok := o.bindTypeArgs(typeInfo, call, skipArgIndex)
	if !ok {
		return o, nil, false
	}

	argCount := 0
	var expectedType gotypes.Type
	for arg := range bound.args(typeInfo, call) {
		argCount++
		if arg.ArgIndex == skipArgIndex {
			expectedType = arg.ExpectedType
			continue
		}
		if arg.IsTypeArg() {
			continue
		}
		if !callValueArgMatchesType(typeInfo, arg.Arg, arg.ExpectedType, arg.KwargTarget) {
			return bound, nil, false
		}
	}
	return bound, expectedType, argCount == len(args)+len(call.Kwargs)
}

// callValueArgMatchesType checks a written value argument during candidate filtering.
func callValueArgMatchesType(typeInfo *types.Info, arg ast.Expr, want gotypes.Type, keyword *xgoutil.ResolvedCallExprKwargTarget) bool {
	if want == nil {
		return false
	}
	// XGo lowers both lambda forms to non-variadic closures.
	switch arg := arg.(type) {
	case *ast.LambdaExpr:
		sig := signatureType(want)
		return sig != nil && !sig.Variadic() && len(arg.Lhs) == sig.Params().Len()
	case *ast.ArrowExpr:
		sig := signatureType(want)
		if sig == nil || sig.Variadic() || len(arg.Lhs) != sig.Params().Len() || len(arg.Rhs) != sig.Results().Len() {
			return false
		}
		if typeInfo == nil {
			return true
		}
		for i, result := range arg.Rhs {
			// A literal keeps its type across compiler attempts with different callback parameters.
			if lit, ok := astutil.Unparen(result).(*ast.BasicLit); ok &&
				!callValueArgMatchesType(typeInfo, lit, sig.Results().At(i).Type(), nil) {
				return false
			}
		}
		return true
	case *ast.NumberUnitLit:
		typ := xgoUnitArgumentType(want, keyword)
		if _, unbound := typ.(*gotypes.TypeParam); unbound {
			return true
		}
		_, found := xgoutil.UnitSpecForType(typ, arg.Unit)
		return found
	}
	if isType, _ := xgoutil.IsTypeExpr(typeInfo, arg); isType {
		// Gogen materializes a type expression as a reflect.Type argument.
		named, ok := want.(*gotypes.Named)
		return ok && named.Obj().Name() == "Type" && xgoutil.PkgPath(named.Obj().Pkg()) == "reflect"
	}

	actual := typeInfo.Types[arg]
	actual.Type = typeInfo.TypeOf(arg)
	if !xgoutil.IsValidType(actual.Type) {
		return true
	}
	return xgoutil.CallValueTypeMatches(typeInfo, actual, want)
}

// expectedTypesForCallArg returns expected types when target is a direct
// call argument.
func expectedTypesForCallArg(
	typeInfo *types.Info,
	call *ast.CallExpr,
	target ast.Expr,
) ([]gotypes.Type, bool) {
	expected := builtinArgTypes(typeInfo, call, xgoutil.SourceExprIndex(call.Args, target))

	if arg, ok := xgoutil.ResolveCallExprArg(typeInfo, call, target); ok {
		expected = append(expected, validExpectedType(arg.ExpectedType)...)
	} else if _, sig, _ := xgoutil.ResolveCallExprSignature(typeInfo, call); sig == nil {
		for _, overload := range callExprOverloads(typeInfo, call) {
			if _, typ, matches := overload.matchArgs(typeInfo, call, target); matches {
				expected = append(expected, validExpectedType(typ)...)
			}
		}
	}
	allowConversion := false
	if len(call.Args) == 1 && call.Args[0] == target {
		if tv := typeInfo.Types[xgoutil.CallExprFun(call)]; tv.IsType() && xgoutil.IsValidType(tv.Type) {
			expected = append(expected, gotypes.Unalias(tv.Type))
			allowConversion = true
		}
	}
	return deduplicateTypes(expected), allowConversion
}
