package server

import (
	"bytes"
	"fmt"
	gotypes "go/types"
	"io/fs"
	"iter"
	"path"
	"slices"
	"time"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/format"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification#textDocument_formatting
func (s *Server) textDocumentFormatting(params *DocumentFormattingParams) ([]TextEdit, error) {
	spxFile, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document uri %q: %w", params.TextDocument.URI, err)
	}
	if path.Ext(spxFile) != ".spx" {
		return nil, nil // Not an spx source file.
	}

	snapshot := s.getProj().Snapshot()
	file, ok := snapshot.File(spxFile)
	if !ok {
		return nil, fmt.Errorf("failed to read spx source file: %w", fs.ErrNotExist)
	}
	original := file.Content
	// FIXME(wyvern): Remove this workaround when the server supports CRLF line endings.
	original = bytes.ReplaceAll(original, []byte("\r\n"), []byte("\n"))
	formatted, err := s.formatSpx(snapshot, spxFile, original)
	if err != nil {
		return nil, fmt.Errorf("failed to format spx source file: %w", err)
	}

	if bytes.Equal(formatted, original) {
		return nil, nil // No changes.
	}

	// Simply replace the entire document.
	lines := bytes.Count(original, []byte("\n"))
	lastNewLine := bytes.LastIndex(original, []byte("\n"))
	lastLineContent := original
	if lastNewLine >= 0 {
		lastLineContent = lastLineContent[lastNewLine+1:]
	}
	return []TextEdit{
		{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End: Position{
					Line:      uint32(lines),
					Character: uint32(UTF16Len(string(lastLineContent))),
				},
			},
			NewText: string(formatted),
		},
	}, nil
}

// spxFormatter defines a function that formats an spx source file in the given
// root file system snapshot.
type spxFormatter func(snapshot *xgo.Project, spxFile string) (formatted []byte, err error)

// formatSpx applies a series of formatters to an spx source file in order.
//
// The formatters are applied in the following order:
//  1. XGo formatter
//  2. Lambda parameter elimination
//  3. Declaration reordering
func (s *Server) formatSpx(snapshot *xgo.Project, spxFile string, original []byte) ([]byte, error) {
	formatted := original
	for _, formatter := range []spxFormatter{
		s.formatSpxXGo,
		s.formatSpxLambda,
		s.formatSpxDecls,
	} {
		subFormatted, err := formatter(snapshot, spxFile)
		if err != nil {
			return nil, err
		}
		if subFormatted != nil && !bytes.Equal(subFormatted, formatted) {
			snapshot = snapshot.SnapshotWithOverlay(map[string]*xgo.File{
				spxFile: {
					Content: subFormatted,
					ModTime: time.Now(),
				},
			})
			formatted = subFormatted
		}
	}
	return formatted, nil
}

// formatSpxXGo formats an spx source file with XGo formatter.
func (s *Server) formatSpxXGo(snapshot *xgo.Project, spxFile string) ([]byte, error) {
	file, ok := snapshot.File(spxFile)
	if !ok {
		return nil, fs.ErrNotExist
	}
	original := file.Content
	formatted, err := format.Source(original, true, spxFile)
	if err != nil {
		return nil, err
	}
	if len(formatted) == 0 || string(formatted) == "\n" {
		return []byte{}, nil
	}
	return formatted, err
}

// formatSpxLambda formats an spx source file by eliminating unused lambda parameters.
func (s *Server) formatSpxLambda(snapshot *xgo.Project, spxFile string) ([]byte, error) {
	snapshot.UpdateFiles(s.fileMapGetter())
	astFile, _ := snapshot.ASTFile(spxFile)
	if astFile == nil {
		return nil, nil
	}

	// Eliminate unused lambda parameters.
	eliminateUnusedLambdaParams(snapshot, astFile)

	// Format the modified AST.
	var formattedBuf bytes.Buffer
	if err := format.Node(&formattedBuf, snapshot.Fset, astFile); err != nil {
		return nil, err
	}

	formatted := formattedBuf.Bytes()
	if len(formatted) == 0 || string(formatted) == "\n" {
		return []byte{}, nil
	}
	return formatted, nil
}

// formatSpxDecls formats an spx source file by reordering declarations.
func (s *Server) formatSpxDecls(snapshot *xgo.Project, spxFile string) ([]byte, error) {
	astFile, _ := snapshot.ASTFile(spxFile)
	if astFile == nil {
		return nil, nil
	}

	// Find the position of the first declaration that contains any syntax error.
	var errorPos token.Pos
	for _, decl := range astFile.Decls {
		ast.Inspect(decl, func(node ast.Node) bool {
			switch node.(type) {
			case *ast.BadExpr, *ast.BadStmt, *ast.BadDecl:
				if !errorPos.IsValid() || decl.Pos() < errorPos {
					errorPos = decl.Pos()
					return false
				}
			}
			return true
		})
	}

	// Get the start position of the shadow entry if it exists and is not empty.
	var shadowEntryPos token.Pos
	if astFile.ShadowEntry != nil &&
		astFile.ShadowEntry.Pos().IsValid() &&
		astFile.ShadowEntry.Pos() != errorPos &&
		len(astFile.ShadowEntry.Body.List) > 0 {
		shadowEntryPos = astFile.ShadowEntry.Pos()
		if astFile.ShadowEntry.Doc != nil {
			shadowEntryPos = astFile.ShadowEntry.Doc.Pos()
		}
	}

	// Collect all declarations.
	var (
		importDecls       []ast.Decl
		typeDecls         []ast.Decl
		methodDecls       []ast.Decl
		constDecls        []ast.Decl
		varDecl           *ast.GenDecl
		funcDecls         []ast.Decl
		otherDecls        []ast.Decl
		processedComments = make(map[*ast.CommentGroup]struct{})
	)
	fset := snapshot.Fset
	for _, decl := range astFile.Decls {
		// Skip the declaration if it appears after the error position.
		if errorPos.IsValid() && decl.Pos() >= errorPos {
			continue
		}

		switch decl := decl.(type) {
		case *ast.GenDecl:
			switch decl.Tok {
			case token.IMPORT:
				importDecls = append(importDecls, decl)
			case token.TYPE:
				typeDecls = append(typeDecls, decl)
			case token.CONST:
				constDecls = append(constDecls, decl)
			case token.VAR:
				if varDecl != nil {
					return nil, fmt.Errorf("multiple top-level var declarations in classfile")
				}
				varDecl = decl
			default:
				otherDecls = append(otherDecls, decl)
			}
		case *ast.FuncDecl:
			if decl.Shadow {
				continue
			}
			if decl.Recv != nil && !decl.IsClass {
				methodDecls = append(methodDecls, decl)
			} else {
				funcDecls = append(funcDecls, decl)
			}
		case *ast.OverloadFuncDecl:
			if decl.Recv != nil && !decl.IsClass {
				methodDecls = append(methodDecls, decl)
			} else {
				funcDecls = append(funcDecls, decl)
			}
		default:
			otherDecls = append(otherDecls, decl)
		}

		// Pre-process all comments within the declaration to exclude
		// them from floating comments.
		if doc := getDeclDoc(decl); doc != nil {
			processedComments[doc] = struct{}{}
		}
		startLine := fset.Position(decl.Pos()).Line
		endLine := fset.Position(decl.End()).Line
		for _, cg := range astFile.Comments {
			if _, ok := processedComments[cg]; ok {
				continue
			}
			cgStartLine := fset.Position(cg.Pos()).Line
			if cgStartLine >= startLine && cgStartLine <= endLine {
				processedComments[cg] = struct{}{}
			}
		}
	}

	// Reorder declarations: imports -> types -> methods -> consts -> vars -> funcs -> others.
	sortedDecls := make([]ast.Decl, 0, len(astFile.Decls))
	sortedDecls = append(sortedDecls, importDecls...)
	sortedDecls = append(sortedDecls, typeDecls...)
	sortedDecls = append(sortedDecls, methodDecls...)
	sortedDecls = append(sortedDecls, constDecls...)
	if varDecl != nil {
		sortedDecls = append(sortedDecls, varDecl)
	}
	sortedDecls = append(sortedDecls, funcDecls...)
	sortedDecls = append(sortedDecls, otherDecls...)

	// Format the sorted declarations.
	formattedBuf := bytes.NewBuffer(make([]byte, 0, len(astFile.Code)))
	ensureTrailingNewlines := func(count int) {
		if formattedBuf.Len() == 0 {
			return
		}
		for _, b := range slices.Backward(formattedBuf.Bytes()) {
			if b != '\n' {
				break
			}
			count--
		}
		for range count {
			formattedBuf.WriteByte('\n')
		}
	}

	// Find comments that appears on the same line after the given position.
	findInlineComments := func(pos token.Pos) *ast.CommentGroup {
		line := fset.Position(pos).Line
		for _, cg := range astFile.Comments {
			if fset.Position(cg.Pos()).Line != line {
				continue
			}
			if cg.Pos() > pos {
				return cg
			}
		}
		return nil
	}

	// Handle declarations and floating comments in order of their position.
	processDecl := func(decl ast.Decl) error {
		if genDecl, ok := decl.(*ast.GenDecl); ok && genDecl.Tok == token.VAR {
			ensureTrailingNewlines(2)

			var doc []byte
			if genDecl.Doc != nil && len(genDecl.Doc.List) > 0 {
				docStart := fset.Position(genDecl.Doc.Pos()).Offset
				docEnd := fset.Position(genDecl.Doc.End()).Offset
				doc = astFile.Code[docStart:docEnd]
			}

			if doc != nil && genDecl.Lparen.IsValid() {
				formattedBuf.Write(doc)
				formattedBuf.WriteByte('\n')
				doc = nil
			}

			formattedBuf.WriteString("var (")
			if doc != nil {
				formattedBuf.WriteByte('\n')
				formattedBuf.Write(doc)
				formattedBuf.WriteByte('\n')
			}

			var bodyStartPos token.Pos
			if genDecl.Lparen.IsValid() {
				if cg := findInlineComments(genDecl.Lparen); cg != nil {
					cgStart := fset.Position(cg.Pos()).Offset
					cgEnd := fset.Position(cg.End()).Offset
					formattedBuf.Write(astFile.Code[cgStart:cgEnd])
					formattedBuf.WriteByte('\n')
					bodyStartPos = cg.End() + 1
				} else {
					bodyStartPos = genDecl.Lparen + 1
				}
			} else {
				bodyStartPos = genDecl.Pos() + token.Pos(len(genDecl.Tok.String())) + 1
			}
			var bodyEndPos token.Pos
			if genDecl.Rparen.IsValid() {
				bodyEndPos = genDecl.Rparen - 1
			} else {
				bodyEndPos = genDecl.End()
				if cg := findInlineComments(bodyEndPos); cg != nil {
					bodyEndPos = cg.End()
				}
			}
			bodyStart := fset.Position(bodyStartPos).Offset
			bodyEnd := fset.Position(bodyEndPos).Offset
			formattedBuf.Write(astFile.Code[bodyStart:bodyEnd])
			formattedBuf.WriteByte('\n')

			var trailingComments []byte
			if genDecl.Rparen.IsValid() {
				if cg := findInlineComments(genDecl.Rparen); cg != nil {
					cgStart := fset.Position(cg.Pos()).Offset
					cgEnd := fset.Position(cg.End()).Offset
					trailingComments = astFile.Code[cgStart:cgEnd]
				}
			}
			formattedBuf.WriteString(")")
			if trailingComments != nil {
				formattedBuf.WriteByte(' ')
				formattedBuf.Write(trailingComments)
			}
			formattedBuf.WriteByte('\n')
		} else {
			startPos := decl.Pos()
			if doc := getDeclDoc(decl); doc != nil {
				startPos = doc.Pos()
			}

			endPos := decl.End()
			if cg := findInlineComments(endPos); cg != nil {
				endPos = cg.End()
			}

			ensureTrailingNewlines(2)
			start := fset.Position(startPos).Offset
			end := fset.Position(endPos).Offset
			formattedBuf.Write(astFile.Code[start:end])
			ensureTrailingNewlines(1)
		}
		return nil
	}

	// Process all comments and declarations before shadow entry.
	var declIndex int
	for _, cg := range astFile.Comments {
		if _, ok := processedComments[cg]; ok {
			continue
		}
		processedComments[cg] = struct{}{}

		// Skip the comment if it appears after the error position or
		// shadow entry position.
		if errorPos.IsValid() && cg.Pos() >= errorPos {
			continue
		}
		if shadowEntryPos.IsValid() && cg.Pos() >= shadowEntryPos {
			continue
		}

		// Process declarations that should come before this comment.
		for ; declIndex < len(sortedDecls); declIndex++ {
			decl := sortedDecls[declIndex]

			startPos := decl.Pos()
			if doc := getDeclDoc(decl); doc != nil {
				startPos = doc.Pos()
			}
			if startPos > cg.Pos() {
				break
			}

			if err := processDecl(decl); err != nil {
				return nil, err
			}
		}

		// Add the floating comment.
		ensureTrailingNewlines(2)
		start := fset.Position(cg.Pos()).Offset
		end := fset.Position(cg.End()).Offset
		formattedBuf.Write(astFile.Code[start:end])
		ensureTrailingNewlines(1)
	}

	// Process remaining declarations before shadow entry.
	for ; declIndex < len(sortedDecls); declIndex++ {
		if err := processDecl(sortedDecls[declIndex]); err != nil {
			return nil, err
		}
	}

	// Add the shadow entry if it exists and is not empty.
	if shadowEntryPos.IsValid() {
		ensureTrailingNewlines(2)
		start := fset.Position(shadowEntryPos).Offset
		formattedBuf.Write(astFile.Code[start:])
		ensureTrailingNewlines(1)
	}

	formatted := formattedBuf.Bytes()
	if len(formatted) == 0 || string(formatted) == "\n" {
		return []byte{}, nil
	}
	return format.Source(formatted, true, spxFile)
}

// getDeclDoc returns the doc comment of a declaration if any.
func getDeclDoc(decl ast.Decl) *ast.CommentGroup {
	switch decl := decl.(type) {
	case *ast.GenDecl:
		return decl.Doc
	case *ast.FuncDecl:
		return decl.Doc
	case *ast.OverloadFuncDecl:
		return decl.Doc
	default:
		return nil
	}
}

// eliminateUnusedLambdaParams eliminates useless lambda parameter declarations.
// A lambda parameter is considered "useless" if:
//  1. The parameter is not used.
//  2. The lambda is passed to a function that has an overload which receives the lambda without the parameter.
//
// Then we can omit its declaration safely.
//
// NOTE: There are limitations with current implementation:
//  1. Only `LambdaExpr2` (not `LambdaExpr`) is supported.
//  2. Only the last parameter of the lambda is checked.
//
// We may complete it in the future, if needed.
func eliminateUnusedLambdaParams(proj *xgo.Project, astFile *ast.File) {
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return
	}
	ast.Inspect(astFile, func(n ast.Node) bool {
		callExpr, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		funIdent := callExprFunIdent(callExpr)
		if funIdent == nil {
			return true
		}
		funcOverloads := getFuncOverloads(proj, funIdent)
		if len(funcOverloads) == 0 {
			return true
		}
		for resolvedArg := range formatResolvedCallExprArgs(typeInfo, callExpr, funcOverloads) {
			lambdaExpr, ok := resolvedArg.Arg.(*ast.LambdaExpr2)
			if !ok {
				continue
			}
			if len(lambdaExpr.Lhs) == 0 {
				continue
			}
			lambdaSig := resolvedLambdaSignature(typeInfo, callExpr, funcOverloads, resolvedArg, len(lambdaExpr.Lhs))
			if lambdaSig == nil {
				continue
			}

			// To simplify the implementation, we only check & process the last parameter,
			// which is enough to cover known cases.
			lastParamIdx := len(lambdaExpr.Lhs) - 1
			if isIdentUsed(typeInfo, lambdaExpr.Lhs[lastParamIdx]) {
				continue
			}

			newParams := slices.Collect(lambdaSig.Params().Variables())
			newParams = newParams[:len(newParams)-1] // Remove the last parameter.
			newLambdaSig := gotypes.NewSignatureType(
				lambdaSig.Recv(),
				slices.Collect(lambdaSig.RecvTypeParams().TypeParams()),
				slices.Collect(lambdaSig.TypeParams().TypeParams()),
				gotypes.NewTuple(newParams...),
				lambdaSig.Results(),
				lambdaSig.Variadic(),
			)
			hasMatchedOverload := false
			for _, overloadType := range funcOverloads {
				if !overloadMatchesCallExpr(typeInfo, callExpr, overloadType, resolvedArg.ArgIndex) {
					continue
				}
				overloadLambdaSig := signatureType(overloadResolvedCallExprArgType(typeInfo, callExpr, overloadType, resolvedArg))
				if overloadLambdaSig == nil {
					continue
				}
				if gotypes.AssignableTo(newLambdaSig, overloadLambdaSig) {
					hasMatchedOverload = true
					break
				}
			}
			if hasMatchedOverload {
				lambdaExpr.Lhs = lambdaExpr.Lhs[:lastParamIdx]
				if len(lambdaExpr.Lhs) == 0 {
					// Avoid `index out of range [0] with length 0` when printing lambda expression.
					lambdaExpr.Lhs = nil
				}
			}
		}
		return true
	})
}

// callExprFunIdent returns the identifier that names the called function.
func callExprFunIdent(callExpr *ast.CallExpr) *ast.Ident {
	switch fun := callExpr.Fun.(type) {
	case *ast.Ident:
		return fun
	case *ast.SelectorExpr:
		return fun.Sel
	default:
		return nil
	}
}

// formatResolvedCallExprArgs returns call arguments with a fallback path for
// overload pseudo-functions that do not expose a normal callable signature.
func formatResolvedCallExprArgs(typeInfo *types.Info, callExpr *ast.CallExpr, overloads []*gotypes.Func) iter.Seq[xgoutil.ResolvedCallExprArg] {
	return func(yield func(xgoutil.ResolvedCallExprArg) bool) {
		hasResolvedArgs := false
		for resolvedArg := range xgoutil.ResolvedCallExprArgs(typeInfo, callExpr) {
			hasResolvedArgs = true
			if !yield(resolvedArg) {
				return
			}
		}
		if hasResolvedArgs || len(overloads) == 0 {
			return
		}

		for i, argExpr := range callExpr.Args {
			if !yield(xgoutil.ResolvedCallExprArg{
				Arg:      argExpr,
				ArgIndex: i,
				Kind:     xgoutil.ResolvedCallExprArgPositional,
			}) {
				return
			}
		}
		for i, kwarg := range callExpr.Kwargs {
			if !yield(xgoutil.ResolvedCallExprArg{
				Arg:      kwarg.Value,
				ArgIndex: len(callExpr.Args) + i,
				Kind:     xgoutil.ResolvedCallExprArgKeyword,
				Kwarg:    kwarg,
			}) {
				return
			}
		}
	}
}

// resolvedCallExprKwargAtArgCount returns the parameter slot that receives
// kwargs when only argCount positional arguments should be considered before
// kwargs.
func resolvedCallExprKwargAtArgCount(typeInfo *types.Info, callExpr *ast.CallExpr, sig *gotypes.Signature, params *gotypes.Tuple, argCount int) *xgoutil.ResolvedCallExprKwarg {
	paramIndex, ok := callKwargParamIndex(sig, params, argCount)
	if !ok {
		return nil
	}
	param := params.At(paramIndex)
	return &xgoutil.ResolvedCallExprKwarg{
		Param:                 param,
		ParamIndex:            paramIndex,
		AllowInterfaceTargets: xgoutil.CallExprSupportsInterfaceKwargs(typeInfo, callExpr, param.Type()),
	}
}

// callKwargParamIndex returns the parameter slot that receives kwargs after
// argCount positional arguments.
func callKwargParamIndex(sig *gotypes.Signature, params *gotypes.Tuple, argCount int) (int, bool) {
	if params.Len() == 0 {
		return 0, false
	}
	if sig.Variadic() {
		paramIndex := params.Len() - 2
		if paramIndex < 0 || argCount < paramIndex {
			return 0, false
		}
		return paramIndex, true
	}

	paramIndex := argCount
	if paramIndex >= params.Len() {
		return 0, false
	}
	return paramIndex, true
}

// overloadMatchesCallExpr reports whether overloadType is still viable for
// callExpr after checking every argument except the one at skipArgIndex.
func overloadMatchesCallExpr(typeInfo *types.Info, callExpr *ast.CallExpr, overloadType *gotypes.Func, skipArgIndex int) bool {
	sig := overloadType.Signature()
	params := sig.Params()
	kwargParamIndex, hasKwargParam := 0, false
	if len(callExpr.Kwargs) > 0 {
		var ok bool
		kwargParamIndex, ok = callKwargParamIndex(sig, params, len(callExpr.Args))
		if !ok {
			return false
		}
		hasKwargParam = true
	}

	for i, argExpr := range callExpr.Args {
		paramIndex := i
		if hasKwargParam && i >= kwargParamIndex {
			paramIndex++
		}
		expectedType := callExprArgType(sig, params, paramIndex)
		if expectedType == nil {
			return false
		}
		if i == skipArgIndex {
			continue
		}
		if !formatArgMatchesType(typeInfo, argExpr, expectedType) {
			return false
		}
	}

	for i, kwarg := range callExpr.Kwargs {
		globalIndex := len(callExpr.Args) + i
		if globalIndex == skipArgIndex {
			continue
		}
		expectedType := overloadResolvedCallExprArgType(typeInfo, callExpr, overloadType, xgoutil.ResolvedCallExprArg{
			Kind:       xgoutil.ResolvedCallExprArgKeyword,
			Kwarg:      kwarg,
			ParamIndex: kwargParamIndex,
		})
		if expectedType == nil {
			return false
		}
		if !formatArgMatchesType(typeInfo, kwarg.Value, expectedType) {
			return false
		}
	}
	return true
}

// formatArgMatchesType reports whether argExpr is compatible with expectedType
// for formatter-side overload filtering.
func formatArgMatchesType(typeInfo *types.Info, argExpr ast.Expr, expectedType gotypes.Type) bool {
	if lambdaExpr, ok := argExpr.(*ast.LambdaExpr2); ok {
		lambdaSig := signatureType(expectedType)
		return lambdaSig != nil && len(lambdaExpr.Lhs) == lambdaSig.Params().Len()
	}

	actualType := typeInfo.TypeOf(argExpr)
	if actualType == nil {
		return true
	}
	return gotypes.AssignableTo(actualType, expectedType)
}

// resolvedLambdaSignature returns the current expected lambda signature for
// resolvedArg. It falls back to matching overload signatures when the resolved
// argument type is unavailable on the current call object.
func resolvedLambdaSignature(typeInfo *types.Info, callExpr *ast.CallExpr, overloads []*gotypes.Func, resolvedArg xgoutil.ResolvedCallExprArg, paramCount int) *gotypes.Signature {
	if lambdaSig := signatureType(resolvedArg.ExpectedType); lambdaSig != nil {
		return lambdaSig
	}

	for _, overloadType := range overloads {
		if !overloadMatchesCallExpr(typeInfo, callExpr, overloadType, resolvedArg.ArgIndex) {
			continue
		}
		lambdaSig := signatureType(overloadResolvedCallExprArgType(typeInfo, callExpr, overloadType, resolvedArg))
		if lambdaSig == nil || lambdaSig.Params().Len() != paramCount {
			continue
		}
		return lambdaSig
	}
	return nil
}

// overloadResolvedCallExprArgType returns the expected argument type for
// resolvedArg when the same call is matched against overloadType.
func overloadResolvedCallExprArgType(typeInfo *types.Info, callExpr *ast.CallExpr, overloadType *gotypes.Func, resolvedArg xgoutil.ResolvedCallExprArg) gotypes.Type {
	overloadSig := overloadType.Signature()
	params := overloadSig.Params()
	if resolvedArg.Kind == xgoutil.ResolvedCallExprArgKeyword {
		kwarg := resolvedCallExprKwargAtArgCount(typeInfo, callExpr, overloadSig, params, len(callExpr.Args))
		if kwarg == nil {
			return nil
		}

		target := xgoutil.LookupResolvedCallExprKwargTarget(kwarg, resolvedArg.Kwarg.Name.Name)
		if target == nil {
			return nil
		}
		return target.ValueType
	}

	paramIndex := resolvedArg.ArgIndex
	if len(callExpr.Kwargs) > 0 {
		kwargParamIndex, ok := callKwargParamIndex(overloadSig, params, len(callExpr.Args))
		if !ok {
			return nil
		}
		if paramIndex >= kwargParamIndex {
			paramIndex++
		}
	}
	return callExprArgType(overloadSig, params, paramIndex)
}

// callExprArgType returns the positional expected argument type at paramIndex
// and unwraps variadic slices to their element type.
func callExprArgType(sig *gotypes.Signature, params *gotypes.Tuple, paramIndex int) gotypes.Type {
	param, paramIndex := callExprParam(sig, params, paramIndex)
	if param == nil {
		return nil
	}

	if sig.Variadic() && paramIndex == params.Len()-1 {
		if sliceType, ok := param.Type().(*gotypes.Slice); ok {
			return sliceType.Elem()
		}
		return nil
	}
	return param.Type()
}

// callExprParam returns the positional parameter at paramIndex and normalizes
// variadic overflow to the variadic parameter.
func callExprParam(sig *gotypes.Signature, params *gotypes.Tuple, paramIndex int) (*gotypes.Var, int) {
	if paramIndex < 0 || params.Len() == 0 {
		return nil, 0
	}
	if sig.Variadic() && paramIndex >= params.Len()-1 {
		return params.At(params.Len() - 1), params.Len() - 1
	}
	if paramIndex >= params.Len() {
		return nil, 0
	}
	return params.At(paramIndex), paramIndex
}

// signatureType returns typ as a function signature after unaliasing.
func signatureType(typ gotypes.Type) *gotypes.Signature {
	typ = gotypes.Unalias(typ)
	sig, _ := typ.(*gotypes.Signature)
	return sig
}

// getFuncOverloads returns all overloads for the function named by funIdent.
func getFuncOverloads(proj *xgo.Project, funIdent *ast.Ident) []*gotypes.Func {
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	funType, ok := typeInfo.ObjectOf(funIdent).(*gotypes.Func)
	if !ok {
		return nil
	}
	pkg := funType.Pkg()
	if pkg == nil {
		return nil
	}
	recvTypeName := SelectorTypeNameForIdent(proj, funIdent)
	if recvTypeName == "" {
		return nil
	}
	if IsInSpxPkg(funType) && recvTypeName == "Sprite" {
		recvTypeName = "SpriteImpl"
	}

	recvObj := pkg.Scope().Lookup(recvTypeName)
	if recvObj == nil {
		return nil
	}
	recvType := recvObj.Type()
	recvNamed, ok := recvType.(*gotypes.Named)
	if !ok || !xgoutil.IsNamedStructType(recvNamed) {
		return nil
	}
	var baseFunc *gotypes.Func
	for structMember := range xgoutil.StructMembers(recvNamed) {
		method, ok := structMember.Member.(*gotypes.Func)
		if !ok {
			continue
		}
		if pn, overloadID := xgoutil.ParseXGoFuncName(method.Name()); pn == funIdent.Name && overloadID == nil {
			baseFunc = method
			break
		}
	}
	if baseFunc == nil {
		return nil
	}
	return xgoutil.ExpandXGoOverloadableFunc(baseFunc)
}

// isIdentUsed reports whether ident is referenced by any use in typeInfo.
func isIdentUsed(typeInfo *types.Info, ident *ast.Ident) bool {
	obj := typeInfo.ObjectOf(ident)
	if obj == nil {
		return false
	}
	for _, usedObj := range typeInfo.Uses {
		if usedObj == obj {
			return true
		}
	}
	return false
}
