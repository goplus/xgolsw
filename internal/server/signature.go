package server

import (
	"cmp"
	gotypes "go/types"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_signatureHelp
func (s *Server) textDocumentSignatureHelp(params *SignatureHelpParams) (*SignatureHelp, error) {
	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if astFile == nil {
		return nil, nil
	}
	pos := PosAt(result.proj, astFile, params.Position)
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}

	callExpr := enclosingCallExprAtPosition(astFile, pos)
	if callExpr != nil && !callExprCoversSignaturePosition(callExpr, pos) {
		callExpr = nil
	}

	var (
		fun             *gotypes.Func
		sig             *gotypes.Signature
		resolvedParams  *gotypes.Tuple
		activeParameter int
	)
	if callExpr != nil {
		fun, sig, resolvedParams = xgoutil.ResolveCallExprSignature(typeInfo, callExpr)
		if fun == nil || sig == nil || resolvedParams == nil {
			return overloadSignatureHelp(result.proj, typeInfo, callExpr, pos), nil
		}
		activeParameter = signatureHelpActiveParameter(typeInfo, callExpr, pos, sig, resolvedParams)
	} else {
		ident := signatureHelpIdentAtPosition(typeInfo, astFile, pos)
		obj := typeInfo.ObjectOf(ident)
		if obj == nil {
			return nil, nil
		}
		var ok bool
		fun, ok = obj.(*gotypes.Func)
		if !ok {
			return nil, nil
		}
		sig = fun.Signature()
		resolvedParams = sig.Params()
		activeParameter = 0
	}

	displayedName := ""
	if callExpr != nil {
		displayedName = signatureHelpResolvedCallName(result.proj, typeInfo, callExpr, fun)
	}
	help := &SignatureHelp{
		Signatures: []SignatureInformation{signatureHelpInformation(fun, sig, resolvedParams, displayedName)},
	}
	if activeParameter >= 0 {
		help.ActiveParameter = uint32(activeParameter)
	}
	return help, nil
}

// signatureHelpIdentAtPosition returns the smallest non-implicit identifier at
// pos that has type information.
func signatureHelpIdentAtPosition(typeInfo *types.Info, astFile *ast.File, pos token.Pos) *ast.Ident {
	var best *ast.Ident
	ast.Inspect(astFile, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok || ident.Implicit() || typeInfo.ObjectOf(ident) == nil {
			return true
		}
		if pos < ident.Pos() || pos >= ident.End() {
			return true
		}
		if best == nil || ident.End()-ident.Pos() < best.End()-best.Pos() {
			best = ident
		}
		return true
	})
	return best
}

// overloadSignatureHelp returns signature help for an overload pseudo-function
// call.
func overloadSignatureHelp(proj *xgo.Project, typeInfo *types.Info, callExpr *ast.CallExpr, pos token.Pos) *SignatureHelp {
	overloads := callExprFuncOverloads(proj, typeInfo, callExpr)
	if len(overloads) == 0 {
		return nil
	}

	resolvedArg, hasResolvedArg := signatureHelpResolvedArgAtPosition(typeInfo, callExpr, overloads, pos)
	skipArgIndex := -1
	if hasResolvedArg {
		skipArgIndex = resolvedArg.ArgIndex
	}

	var signatures []SignatureInformation
	activeParameter := -1
	displayedName := signatureHelpCallName(callExpr)
	for _, overload := range overloads {
		if !overloadMatchesCallExpr(typeInfo, callExpr, overload, skipArgIndex) {
			continue
		}
		sig := overload.Signature()
		params := sig.Params()
		signature := signatureHelpInformation(overload, sig, params, displayedName)
		if activeParameter < 0 {
			activeParameter = overloadSignatureHelpActiveParameter(callExpr, pos, sig, resolvedArg, hasResolvedArg)
		}
		signatures = append(signatures, signature)
	}
	if len(signatures) == 0 {
		return nil
	}

	help := &SignatureHelp{Signatures: signatures}
	if activeParameter >= 0 {
		help.ActiveParameter = uint32(activeParameter)
	}
	return help
}

// signatureHelpInformation returns signature information for one function.
func signatureHelpInformation(fun *gotypes.Func, sig *gotypes.Signature, params *gotypes.Tuple, displayedName string) SignatureInformation {
	paramLabels := make([]string, 0, params.Len())
	paramInfos := make([]ParameterInformation, 0, params.Len())
	for i := range params.Len() {
		paramLabel := signatureHelpParameterLabel(fun, sig, params, i)
		paramLabels = append(paramLabels, paramLabel)
		paramInfos = append(paramInfos, ParameterInformation{
			Label: paramLabel,
			// TODO: Add documentation.
		})
	}

	label := signatureHelpLabel(fun, sig, paramLabels)
	if displayedName != "" {
		label = displayedName + "(" + strings.Join(paramLabels, ", ") + ")" + displayedFuncResults(sig.Results())
	}
	return SignatureInformation{
		Label:      label,
		Parameters: paramInfos,
	}
}

// signatureHelpResolvedArgAtPosition returns the resolved argument at pos.
func signatureHelpResolvedArgAtPosition(typeInfo *types.Info, callExpr *ast.CallExpr, overloads []*gotypes.Func, pos token.Pos) (xgoutil.ResolvedCallExprArg, bool) {
	for resolvedArg := range formatResolvedCallExprArgs(typeInfo, callExpr, overloads) {
		if pos >= resolvedArg.Arg.Pos() && pos <= resolvedArg.Arg.End() {
			return resolvedArg, true
		}
		if resolvedArg.Kind != xgoutil.ResolvedCallExprArgKeyword {
			continue
		}
		if pos >= resolvedArg.Kwarg.Name.Pos() && pos <= resolvedArg.Kwarg.End() {
			return resolvedArg, true
		}
	}
	return xgoutil.ResolvedCallExprArg{}, false
}

// signatureHelpCallName returns the source-facing call name.
func signatureHelpCallName(callExpr *ast.CallExpr) string {
	funIdent := callExprFunIdent(callExpr)
	if funIdent == nil {
		return ""
	}
	return funIdent.Name
}

// signatureHelpResolvedCallName returns the source call name for resolved
// overload functions.
func signatureHelpResolvedCallName(proj *xgo.Project, typeInfo *types.Info, callExpr *ast.CallExpr, fun *gotypes.Func) string {
	for _, overload := range callExprFuncOverloads(proj, typeInfo, callExpr) {
		if overload == fun {
			return signatureHelpCallName(callExpr)
		}
	}
	return ""
}

// enclosingCallExprAtPosition returns the innermost call expression at pos.
func enclosingCallExprAtPosition(astFile *ast.File, pos token.Pos) *ast.CallExpr {
	var best *ast.CallExpr
	ast.Inspect(astFile, func(node ast.Node) bool {
		callExpr, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}

		end := callExpr.End()
		if pos < callExpr.Pos() || pos > end {
			if !callExpr.IsCommand() || pos != end+1 {
				return true
			}
		}
		if best == nil || callExpr.End()-callExpr.Pos() <= best.End()-best.Pos() {
			best = callExpr
		}
		return true
	})
	return best
}

// callExprCoversSignaturePosition reports whether pos is on the callable or a
// non-lambda argument of callExpr.
func callExprCoversSignaturePosition(callExpr *ast.CallExpr, pos token.Pos) bool {
	if pos >= callExpr.Fun.Pos() && pos <= callExpr.Fun.End() {
		return true
	}
	if callExpr.Lparen.IsValid() && pos >= callExpr.Lparen && pos <= callExpr.Rparen {
		return true
	}
	if callExpr.IsCommand() && pos == callExpr.End()+1 {
		return true
	}
	for _, arg := range callExpr.Args {
		switch arg.(type) {
		case *ast.LambdaExpr, *ast.LambdaExpr2:
			continue
		}
		if pos >= arg.Pos() && pos <= arg.End() {
			return true
		}
	}
	for _, kwarg := range callExpr.Kwargs {
		if pos >= kwarg.Name.Pos() && pos <= kwarg.Name.End() {
			return true
		}
		switch kwarg.Value.(type) {
		case *ast.LambdaExpr, *ast.LambdaExpr2:
			continue
		}
		if pos >= kwarg.Value.Pos() && pos <= kwarg.Value.End() {
			return true
		}
	}
	return false
}

// signatureHelpLabel formats the signature label shown by signature help.
func signatureHelpLabel(fun *gotypes.Func, sig *gotypes.Signature, paramLabels []string) string {
	_, displayedName, _, _ := displayedFuncName(fun)
	return displayedName + "(" + strings.Join(paramLabels, ", ") + ")" + displayedFuncResults(sig.Results())
}

// signatureHelpParameterLabel formats a single parameter for signature help.
func signatureHelpParameterLabel(fun *gotypes.Func, sig *gotypes.Signature, params *gotypes.Tuple, paramIndex int) string {
	param := params.At(paramIndex)
	if signatureHelpParamIsTypeArg(fun, paramIndex) {
		return xgoutil.SourceParamName(param) + " Type"
	}
	paramType := param.Type()
	typeLabel := GetSimplifiedTypeString(paramType)
	if sig.Variadic() && paramIndex == params.Len()-1 {
		if slice, ok := paramType.(*gotypes.Slice); ok {
			typeLabel = "..." + GetSimplifiedTypeString(slice.Elem())
		}
	}
	return xgoutil.SourceParamName(param) + " " + typeLabel
}

// signatureHelpParamIsTypeArg reports whether paramIndex is a normalized XGox
// type argument parameter.
func signatureHelpParamIsTypeArg(fun *gotypes.Func, paramIndex int) bool {
	if !xgoutil.IsMarkedAsXGoPackage(fun.Pkg()) {
		return false
	}
	_, methodName, ok := xgoutil.SplitXGotMethodName(fun.Name(), false)
	if !ok {
		return false
	}
	if _, ok := xgoutil.SplitXGoxFuncName(methodName); !ok {
		return false
	}
	typeParams := fun.Signature().TypeParams()
	return typeParams != nil && paramIndex < typeParams.Len()
}

// overloadSignatureHelpActiveParameter resolves the active parameter for one
// overload signature.
func overloadSignatureHelpActiveParameter(callExpr *ast.CallExpr, pos token.Pos, sig *gotypes.Signature, resolvedArg xgoutil.ResolvedCallExprArg, hasResolvedArg bool) int {
	params := sig.Params()
	if params.Len() == 0 {
		return -1
	}

	if hasResolvedArg {
		if resolvedArg.Kind == xgoutil.ResolvedCallExprArgKeyword {
			paramIndex, ok := overloadKwargParamIndex(sig, len(callExpr.Args))
			if ok {
				return paramIndex
			}
		}

		paramIndex := resolvedArg.ArgIndex
		if len(callExpr.Kwargs) > 0 {
			if kwargParamIndex, ok := overloadKwargParamIndex(sig, len(callExpr.Args)); ok && paramIndex >= kwargParamIndex {
				paramIndex++
			}
		}
		if param, paramIndex := overloadCallExprParam(sig, paramIndex); param != nil {
			return paramIndex
		}
	}

	if len(callExpr.Kwargs) > 0 && pos >= callExpr.Kwargs[0].Pos() {
		if paramIndex, ok := overloadKwargParamIndex(sig, len(callExpr.Args)); ok {
			return paramIndex
		}
	}
	return signatureHelpPositionalActiveParameter(callExpr, pos, sig)
}

// signatureHelpActiveParameter resolves the active top-level parameter for pos.
func signatureHelpActiveParameter(typeInfo *types.Info, callExpr *ast.CallExpr, pos token.Pos, sig *gotypes.Signature, params *gotypes.Tuple) int {
	if params.Len() == 0 {
		return -1
	}

	if kwarg := xgoutil.ResolveCallExprKwarg(typeInfo, callExpr); kwarg != nil {
		for _, kwargExpr := range callExpr.Kwargs {
			if pos >= kwargExpr.Pos() && pos <= kwargExpr.End() {
				return kwarg.ParamIndex
			}
		}
		if len(callExpr.Kwargs) > 0 && pos >= callExpr.Kwargs[0].Pos() {
			return kwarg.ParamIndex
		}
	}

	lastParamIndex := -1
	lastArgEnd := cmp.Or(callExpr.Lparen, callExpr.Fun.End())
	for resolvedArg := range xgoutil.ResolvedCallExprArgs(typeInfo, callExpr) {
		if resolvedArg.Kind != xgoutil.ResolvedCallExprArgPositional {
			continue
		}
		lastParamIndex = resolvedArg.ParamIndex
		lastArgEnd = resolvedArg.Arg.End()
		if pos >= resolvedArg.Arg.Pos() && pos <= resolvedArg.Arg.End() {
			return resolvedArg.ParamIndex
		}
	}

	if pos <= lastArgEnd || len(callExpr.Args) == 0 {
		return 0
	}

	return signatureHelpNextParameter(sig, lastParamIndex)
}

// signatureHelpPositionalActiveParameter resolves the active parameter for
// positional arguments.
func signatureHelpPositionalActiveParameter(callExpr *ast.CallExpr, pos token.Pos, sig *gotypes.Signature) int {
	params := sig.Params()
	lastParamIndex := -1
	lastArgEnd := cmp.Or(callExpr.Lparen, callExpr.Fun.End())
	for i, arg := range callExpr.Args {
		paramIndex := i
		if sig.Variadic() && paramIndex >= params.Len()-1 {
			paramIndex = params.Len() - 1
		}
		lastParamIndex = paramIndex
		lastArgEnd = arg.End()
		if pos >= arg.Pos() && pos <= arg.End() {
			return paramIndex
		}
	}

	if pos <= lastArgEnd || len(callExpr.Args) == 0 {
		return 0
	}

	return signatureHelpNextParameter(sig, lastParamIndex)
}

// signatureHelpNextParameter returns the parameter after lastParamIndex,
// clamped to the final or variadic parameter.
func signatureHelpNextParameter(sig *gotypes.Signature, lastParamIndex int) int {
	params := sig.Params()
	nextParamIndex := lastParamIndex + 1
	if sig.Variadic() && nextParamIndex >= params.Len()-1 {
		return params.Len() - 1
	}
	if nextParamIndex < params.Len() {
		return nextParamIndex
	}
	return params.Len() - 1
}
