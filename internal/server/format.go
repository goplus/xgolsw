package server

import (
	"bytes"
	"fmt"
	gotypes "go/types"
	"io/fs"
	"iter"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/format"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification#textDocument_formatting
func (s *Server) textDocumentFormatting(params *DocumentFormattingParams) ([]TextEdit, error) {
	filename, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, fmt.Errorf("failed to get file path from document uri %q: %w", params.TextDocument.URI, err)
	}
	proj := s.syncProject()
	if !proj.IsSourceFile(filename) {
		return nil, nil
	}
	file, ok := proj.File(filename)
	if !ok {
		return nil, fmt.Errorf("failed to read source file: %w", fs.ErrNotExist)
	}
	original := file.Content
	formatted, err := formatSource(proj, filename, original)
	if err != nil {
		return nil, fmt.Errorf("failed to format source file: %w", err)
	}

	if bytes.Equal(formatted, original) {
		return nil, nil // No changes.
	}

	// Simply replace the entire document.
	lines := bytes.Count(original, []byte("\n"))
	lastLineContent := original[bytes.LastIndexByte(original, '\n')+1:]
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

// sourceFormatter formats a source file in the given project snapshot.
type sourceFormatter func(snapshot *xgo.Project, filename string) (formatted []byte, err error)

// formatSource applies a series of formatters to a source file in order.
//
// The formatters are applied in the following order:
//  1. XGo formatter
//  2. Lambda parameter elimination
//  3. Classfile declaration reordering
func formatSource(proj *xgo.Project, filename string, original []byte) ([]byte, error) {
	// Type checking can modify other classfiles too. Use fresh caches for all
	// files and a fresh file set so temporary source positions are not retained
	// by the project after formatting.
	snapshot := proj.Fork()
	snapshot.PutFile(filename, &xgo.File{Content: original})
	formatted := original
	for _, formatter := range []sourceFormatter{
		formatXGo,
		formatLambda,
		formatClassDecls,
	} {
		next, err := formatter(snapshot, filename)
		if err != nil {
			return nil, err
		}
		if next != nil && !bytes.Equal(next, formatted) {
			snapshot.PutFile(filename, &xgo.File{Content: next})
			formatted = next
		}
	}
	return formatted, nil
}

// formatXGo formats a source file with XGo formatter.
func formatXGo(snapshot *xgo.Project, filename string) ([]byte, error) {
	file, ok := snapshot.File(filename)
	if !ok {
		return nil, fs.ErrNotExist
	}
	formatted, err := format.Source(file.Content, snapshot.Module().ClassInfo, filename)
	if err != nil {
		return nil, err
	}
	if len(formatted) == 0 || string(formatted) == "\n" {
		return []byte{}, nil
	}
	return formatted, nil
}

// formatLambda formats a source file by eliminating unused lambda parameters.
func formatLambda(snapshot *xgo.Project, filename string) ([]byte, error) {
	astFile, _ := snapshot.ASTFile(filename)
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

// formatClassDecls reorders classfile declarations and groups class fields.
func formatClassDecls(snapshot *xgo.Project, filename string) ([]byte, error) {
	astFile, _ := snapshot.ASTFile(filename)
	if astFile == nil || !astFile.IsClass {
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
		startLine := fset.PositionFor(decl.Pos(), false).Line
		endLine := fset.PositionFor(decl.End(), false).Line
		for _, cg := range astFile.Comments {
			if _, ok := processedComments[cg]; ok {
				continue
			}
			cgStartLine := fset.PositionFor(cg.Pos(), false).Line
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
		line := fset.PositionFor(pos, false).Line
		for _, cg := range astFile.Comments {
			if fset.PositionFor(cg.Pos(), false).Line != line {
				continue
			}
			if cg.Pos() > pos {
				return cg
			}
		}
		return nil
	}

	// Handle declarations and floating comments in order of their position.
	processDecl := func(decl ast.Decl) {
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
			if bodyStart < bodyEnd {
				formattedBuf.Write(astFile.Code[bodyStart:bodyEnd])
			}
			formattedBuf.WriteByte('\n')

			formattedBuf.WriteString(")")
			if genDecl.Rparen.IsValid() {
				if cg := findInlineComments(genDecl.Rparen); cg != nil {
					cgStart := fset.Position(cg.Pos()).Offset
					cgEnd := fset.Position(cg.End()).Offset
					formattedBuf.WriteByte(' ')
					formattedBuf.Write(astFile.Code[cgStart:cgEnd])
				}
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

			processDecl(decl)
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
		processDecl(sortedDecls[declIndex])
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
	return format.Source(formatted, snapshot.Module().ClassInfo, filename)
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
//  1. Only `LambdaExpr` (not `ArrowExpr`) is supported.
//  2. Only the last parameter of the lambda is checked.
//
// We may complete it in the future, if needed.
func eliminateUnusedLambdaParams(proj *xgo.Project, astFile *ast.File) {
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return
	}
	ast.Inspect(astFile, func(n ast.Node) bool {
		callExpr := callExprFromNode(typeInfo, n)
		if callExpr == nil {
			return true
		}
		funcOverloads := callExprFuncOverloads(typeInfo, callExpr)
		if len(funcOverloads) == 0 {
			return true
		}
		for resolvedArg := range formatResolvedCallExprArgs(typeInfo, callExpr, funcOverloads) {
			lambdaExpr, ok := resolvedArg.Arg.(*ast.LambdaExpr)
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
				expectedType, matches := matchOverloadCallExprArg(typeInfo, callExpr, overloadType, resolvedArg.Arg, resolvedArg.ArgIndex)
				if !matches {
					continue
				}
				overloadLambdaSig := signatureType(expectedType)
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

		seen := make(map[ast.Expr]bool)
		for _, overload := range overloads {
			for arg := range xgoutil.ResolvedCallExprArgsForFunc(typeInfo, callExpr, overload) {
				if seen[arg.Arg] {
					continue
				}
				seen[arg.Arg] = true
				if !yield(xgoutil.ResolvedCallExprArg{Arg: arg.Arg, ArgIndex: arg.ArgIndex, Kind: arg.Kind, Kwarg: arg.Kwarg}) {
					return
				}
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
	_, matches := matchOverloadCallExprArg(typeInfo, callExpr, overloadType, nil, skipArgIndex)
	return matches
}

// matchOverloadCallExprArg checks an overload and returns target's expected type
// using the same signature and argument mapping. A nil target only checks the
// match. skipArgIndex excludes an argument from compatibility checks.
func matchOverloadCallExprArg(typeInfo *types.Info, callExpr *ast.CallExpr, overloadType *gotypes.Func, target ast.Expr, skipArgIndex int) (gotypes.Type, bool) {
	sig, params := xgoutil.ResolveFuncSignatureForCall(typeInfo, callExpr, overloadType)
	if sig == nil {
		return nil, false
	}
	args, _ := xgoutil.CallExprArgs(typeInfo, callExpr, params)
	argCount := 0
	var expectedType gotypes.Type
	for arg := range xgoutil.ResolvedCallExprArgsForSignature(typeInfo, callExpr, overloadType, sig, params) {
		argCount++
		if arg.Arg == target {
			expectedType = arg.ExpectedType
		}
		if arg.ArgIndex == skipArgIndex {
			continue
		}
		if arg.ExpectedType == nil || !formatArgMatchesType(typeInfo, arg.Arg, arg.ExpectedType) {
			return nil, false
		}
	}
	return expectedType, argCount == len(args)+len(callExpr.Kwargs)
}

// formatArgMatchesType reports whether argExpr is compatible with expectedType
// for formatter-side overload filtering.
func formatArgMatchesType(typeInfo *types.Info, argExpr ast.Expr, expectedType gotypes.Type) bool {
	if lambdaExpr, ok := argExpr.(*ast.LambdaExpr); ok {
		lambdaSig := signatureType(expectedType)
		return lambdaSig != nil && len(lambdaExpr.Lhs) == lambdaSig.Params().Len()
	}

	actualType := typeInfo.TypeOf(argExpr)
	if actualType == nil {
		return true
	}
	if _, ok := expectedType.(*gotypes.TypeParam); ok {
		return xgoutil.IsTypesCompatible(actualType, expectedType)
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
		expectedType, matches := matchOverloadCallExprArg(typeInfo, callExpr, overloadType, resolvedArg.Arg, resolvedArg.ArgIndex)
		if !matches {
			continue
		}
		lambdaSig := signatureType(expectedType)
		if lambdaSig == nil || lambdaSig.Params().Len() != paramCount {
			continue
		}
		return lambdaSig
	}
	return nil
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
	return xgoutil.SourceParamType(param)
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

// signatureType returns the underlying function signature of typ.
func signatureType(typ gotypes.Type) *gotypes.Signature {
	if typ == nil {
		return nil
	}
	sig, _ := typ.Underlying().(*gotypes.Signature)
	return sig
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
