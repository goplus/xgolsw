package server

import (
	"cmp"
	"fmt"
	gotypes "go/types"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/protocol"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

const autoclosureParamDocumentation = "Deferred expression. The callee controls when and how often it is evaluated."

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_signatureHelp
func (s *Server) textDocumentSignatureHelp(params *SignatureHelpParams) (*SignatureHelp, error) {
	filename, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document URI %q: %w", params.TextDocument.URI, err)
	}
	proj := s.requestProject()
	astPkg, _ := proj.ASTPackage()
	if astPkg == nil {
		return nil, nil
	}
	astFile := astPkg.Files[filename]
	if astFile == nil || !astFile.Pos().IsValid() {
		return nil, nil
	}
	pos := PosAt(proj, astFile, params.Position)
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}

	ctx := &definitionContext{
		typeDisplay:  newTypeDisplay(proj, astFile, pos),
		proj:         proj,
		lookupPkgDoc: s.lookupPkgDoc,
	}
	documentationKind := PlainText
	if capabilities, ok := s.clientCapabilitiesAfterInitialize(); ok {
		if help := capabilities.TextDocument.SignatureHelp; help != nil && help.SignatureInformation != nil {
			documentationKind = preferredMarkupKind(help.SignatureInformation.DocumentationFormat)
		}
	}

	callExpr, funcDecorator := enclosingCallExprAtPosition(astFile, pos)
	if callExpr != nil && !callExprCoversSignaturePosition(callExpr, pos) {
		callExpr = nil
		funcDecorator = false
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
			return ctx.overloadSignatureHelp(typeInfo, callExpr, pos, documentationKind), nil
		}
		activeParameter = signatureHelpActiveParameter(typeInfo, callExpr, pos, sig, resolvedParams)
		if funcDecorator {
			visibleParams, ok := funcDecoratorParams(sig)
			if !ok {
				return nil, nil
			}
			resolvedParams = visibleParams
			if activeParameter >= resolvedParams.Len() {
				activeParameter = -1
			}
		}
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
		displayedName = signatureHelpResolvedCallName(typeInfo, callExpr, fun)
	}
	help := &SignatureHelp{
		Signatures: []SignatureInformation{ctx.signatureHelpInformation(fun, sig, resolvedParams, displayedName, documentationKind)},
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
func (r *definitionContext) overloadSignatureHelp(typeInfo *types.Info, callExpr *ast.CallExpr, pos token.Pos, documentationKind MarkupKind) *SignatureHelp {
	overloads := callExprFuncOverloads(typeInfo, callExpr)
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
		sig, params := xgoutil.ResolveFuncSignatureForCall(typeInfo, callExpr, overload)
		if sig == nil || params == nil {
			continue
		}
		signature := r.signatureHelpInformation(overload, sig, params, displayedName, documentationKind)
		if activeParameter < 0 {
			activeParameter = overloadSignatureHelpActiveParameter(callExpr, pos, sig, params, resolvedArg, hasResolvedArg)
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
func (r *definitionContext) signatureHelpInformation(fun *gotypes.Func, sig *gotypes.Signature, params *gotypes.Tuple, displayedName string, documentationKind MarkupKind) SignatureInformation {
	paramLabels := make([]string, 0, params.Len())
	paramInfos := make([]ParameterInformation, 0, params.Len())
	for i := range params.Len() {
		paramLabel := r.signatureHelpParameterLabel(fun, sig, params, i)
		paramLabels = append(paramLabels, paramLabel)
		paramInfo := ParameterInformation{Label: paramLabel}
		if _, ok := xgoutil.AutoclosureParamResultType(params.At(i)); ok {
			paramInfo.Documentation = autoclosureParamDocumentation
		}
		paramInfos = append(paramInfos, paramInfo)
	}

	labelName := displayedName
	if labelName == "" {
		_, labelName, _, _ = displayedFuncName(fun)
	}
	info := SignatureInformation{
		Label:      labelName + "(" + strings.Join(paramLabels, ", ") + ")" + r.displayedFuncResults(sig.Results()),
		Parameters: paramInfos,
	}
	if doc := strings.TrimSpace(r.functionDocumentation(fun)); doc != "" {
		info.Documentation = &protocol.Or_SignatureInformation_documentation{Value: doc}
		if documentationKind == Markdown {
			info.Documentation.Value = MarkupContent{Kind: Markdown, Value: doc}
		}
	}
	return info
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
func signatureHelpResolvedCallName(typeInfo *types.Info, callExpr *ast.CallExpr, fun *gotypes.Func) string {
	for _, overload := range callExprFuncOverloads(typeInfo, callExpr) {
		if overload == fun {
			return signatureHelpCallName(callExpr)
		}
	}
	return ""
}

// enclosingCallExprAtPosition returns the innermost call expression at pos and
// reports whether it represents a function decorator.
func enclosingCallExprAtPosition(astFile *ast.File, pos token.Pos) (*ast.CallExpr, bool) {
	var best *ast.CallExpr
	var bestIsFuncDecorator bool
	ast.Inspect(astFile, func(node ast.Node) bool {
		callExpr := callExprFromNode(node)
		if callExpr == nil {
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
			_, bestIsFuncDecorator = node.(*ast.FuncDecorator)
		}
		return true
	})
	return best, bestIsFuncDecorator
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
		case *ast.ArrowExpr, *ast.LambdaExpr:
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
		case *ast.ArrowExpr, *ast.LambdaExpr:
			continue
		}
		if pos >= kwarg.Value.Pos() && pos <= kwarg.Value.End() {
			return true
		}
	}
	return false
}

// signatureHelpParameterLabel formats a single parameter for signature help.
func (d typeDisplay) signatureHelpParameterLabel(fun *gotypes.Func, sig *gotypes.Signature, params *gotypes.Tuple, paramIndex int) string {
	param := params.At(paramIndex)
	if paramIndex < xgoutil.NormalizedCallExprTypeArgCount(fun, params) {
		return xgoutil.SourceParamName(param) + " Type"
	}
	return d.sourceParamLabel(sig, params, paramIndex)
}

// overloadSignatureHelpActiveParameter resolves the active parameter for one
// overload signature.
func overloadSignatureHelpActiveParameter(callExpr *ast.CallExpr, pos token.Pos, sig *gotypes.Signature, params *gotypes.Tuple, resolvedArg xgoutil.ResolvedCallExprArg, hasResolvedArg bool) int {
	if params.Len() == 0 {
		return -1
	}

	if hasResolvedArg {
		if resolvedArg.Kind == xgoutil.ResolvedCallExprArgKeyword {
			paramIndex, ok := callKwargParamIndex(sig, params, len(callExpr.Args))
			if ok {
				return paramIndex
			}
		}

		paramIndex := resolvedArg.ArgIndex
		if len(callExpr.Kwargs) > 0 {
			if kwargParamIndex, ok := callKwargParamIndex(sig, params, len(callExpr.Args)); ok && paramIndex >= kwargParamIndex {
				paramIndex++
			}
		}
		if param, paramIndex := callExprParam(sig, params, paramIndex); param != nil {
			return paramIndex
		}
	}

	if len(callExpr.Kwargs) > 0 && pos >= callExpr.Kwargs[0].Pos() {
		if paramIndex, ok := callKwargParamIndex(sig, params, len(callExpr.Args)); ok {
			return paramIndex
		}
	}
	return signatureHelpPositionalActiveParameter(callExpr, pos, sig, params)
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

	return signatureHelpNextParameter(sig, params, lastParamIndex)
}

// signatureHelpPositionalActiveParameter resolves the active parameter for
// positional arguments.
func signatureHelpPositionalActiveParameter(callExpr *ast.CallExpr, pos token.Pos, sig *gotypes.Signature, params *gotypes.Tuple) int {
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

	return signatureHelpNextParameter(sig, params, lastParamIndex)
}

// signatureHelpNextParameter returns the parameter after lastParamIndex,
// clamped to the final or variadic parameter.
func signatureHelpNextParameter(sig *gotypes.Signature, params *gotypes.Tuple, lastParamIndex int) int {
	nextParamIndex := lastParamIndex + 1
	if sig.Variadic() && nextParamIndex >= params.Len()-1 {
		return params.Len() - 1
	}
	if nextParamIndex < params.Len() {
		return nextParamIndex
	}
	return params.Len() - 1
}
