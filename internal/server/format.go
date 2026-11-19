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
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	xerrors "github.com/qiniu/x/errors"
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
	return []TextEdit{
		{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   positionForOffset(original, xgoutil.LineOffsets(original), len(original)),
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
//  1. Classfile field grouping
//  2. XGo formatter
//  3. Lambda parameter elimination
//  4. Classfile declaration reordering
func formatSource(proj *xgo.Project, filename string, original []byte) ([]byte, error) {
	// Type checking can modify other classfiles too. Use fresh caches for all
	// files and a fresh file set so temporary source positions are not retained
	// by the project after formatting.
	snapshot := proj.Fork()
	snapshot.PutFile(filename, &xgo.File{Content: original})
	formatted := original
	for _, formatter := range []sourceFormatter{
		formatClassFields,
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

// formatClassFields groups a single classfile var declaration before printing.
func formatClassFields(snapshot *xgo.Project, filename string) ([]byte, error) {
	file, err := snapshot.ASTFile(filename)
	if err != nil || file == nil || !file.IsClass {
		return nil, nil
	}
	var fields *ast.GenDecl
	for _, decl := range file.Decls {
		if decl, ok := decl.(*ast.GenDecl); ok && decl.Tok == token.VAR {
			if fields != nil {
				return nil, nil
			}
			fields = decl
		}
	}
	if fields == nil || fields.Lparen.IsValid() {
		return nil, nil
	}
	sources, commentEnds := classVarSources(snapshot, file, []*ast.GenDecl{fields})
	grouped, err := formatClassVarDecls(snapshot, filename, file, sources, commentEnds)
	if err != nil {
		return nil, err
	}
	start, end := classVarEditOffsets(file.Code, sources[0].start, sources[0].end)
	var formatted bytes.Buffer
	formatted.Write(file.Code[:start])
	formatted.Write(grouped)
	formatted.Write(file.Code[end:])
	return formatted.Bytes(), nil
}

// formatXGo formats a source file with XGo formatter.
func formatXGo(snapshot *xgo.Project, filename string) ([]byte, error) {
	file, ok := snapshot.File(filename)
	if !ok {
		return nil, fs.ErrNotExist
	}
	var formatted []byte
	var err error
	if astFile, parseErr := snapshot.ASTFile(filename); parseErr == nil && astFile != nil && astFile.IsClass {
		formatted, err = formatFileWithClassTags(snapshot, filename, astFile)
	} else {
		formatted, err = format.Source(file.Content, snapshot.Module().ClassInfo, filename)
	}
	if err != nil {
		return nil, err
	}
	if len(formatted) == 0 || string(formatted) == "\n" {
		return []byte{}, nil
	}
	return formatted, nil
}

// formatLambda formats a source file until no further unused lambda parameters
// can be removed without changing checked types or introducing errors.
func formatLambda(snapshot *xgo.Project, filename string) ([]byte, error) {
	astFile, _ := snapshot.ASTFile(filename)
	if astFile == nil {
		return nil, nil
	}
	current := snapshot
	syntax := formattingSourceSyntax(astFile)
	before, beforeErr := snapshot.TypeInfo()
	var accepted []byte
	try := func(lambdas []*ast.LambdaExpr) (bool, error) {
		formatted, err := formatShortenedLambdas(current, filename, astFile, lambdas)
		if err != nil {
			return false, err
		}
		// Recheck the printed edit even when the project already contains errors.
		preview := current.Fork()
		preview.PutFile(filename, &xgo.File{Content: formatted})
		previewFile, _ := preview.ASTFile(filename)
		previewSyntax := formattingSourceSyntax(previewFile)
		after, afterErr := preview.TypeInfo()
		if hasNewFormattingErrors(beforeErr, afterErr, filename, astFile.Code, formatted) ||
			!preservesFormattingTypes(before, after, syntax, previewSyntax) {
			return false, nil
		}
		// Reuse the checked preview to find the next unused parameters.
		accepted = formatted
		current, astFile, syntax = preview, previewFile, previewSyntax
		before, beforeErr = after, afterErr
		return true, nil
	}
	for {
		lambdas := unusedLambdaParams(current, astFile)
		if len(lambdas) == 0 {
			if accepted != nil {
				return accepted, nil
			}
			formatted, err := formatFileWithClassTags(current, filename, astFile)
			if err == nil && string(formatted) == "\n" {
				return []byte{}, nil
			}
			return formatted, err
		}
		ok, err := try(lambdas)
		if err != nil {
			return nil, err
		}
		if ok {
			continue
		}
		if len(lambdas) == 1 {
			return accepted, nil
		}
		// Sweep once in source order, checking each edit against the latest
		// accepted preview. A later edit may enable an earlier candidate, so
		// revisit the list after the sweep when anything changed.
		eligible := make(map[*ast.LambdaExpr]bool, len(lambdas))
		for _, lambda := range lambdas {
			eligible[lambda] = true
		}
		changed := false
		for i := range syntax.lambdas {
			lambda, _ := syntax.lambdas[i].expr.(*ast.LambdaExpr)
			if !eligible[lambda] {
				continue
			}
			ok, err := try([]*ast.LambdaExpr{lambda})
			if err != nil {
				return nil, err
			}
			if ok {
				changed = true
				clear(eligible)
				for _, lambda := range unusedLambdaParams(current, astFile) {
					eligible[lambda] = true
				}
			}
		}
		if !changed {
			return accepted, nil
		}
	}
}

// formatShortenedLambdas prints a preview while keeping the checked AST's
// parameter lists available for later attempts.
func formatShortenedLambdas(proj *xgo.Project, filename string, file *ast.File, lambdas []*ast.LambdaExpr) ([]byte, error) {
	params := make([][]*ast.Ident, len(lambdas))
	for i, lambda := range lambdas {
		params[i] = lambda.Lhs
		lambda.Lhs = lambda.Lhs[:len(lambda.Lhs)-1]
		if len(lambda.Lhs) == 0 {
			// The printer expects nil for a lambda with no parameters.
			lambda.Lhs = nil
		}
	}
	defer func() {
		for i, lambda := range lambdas {
			lambda.Lhs = params[i]
		}
	}()
	return formatFileWithClassTags(proj, filename, file)
}

// formattingSyntax holds source calls and lambda parameter lists in traversal order.
type formattingSyntax struct {
	calls   []*ast.CallExpr
	lambdas []formattingLambda
}

// formattingLambda retains the parameter list before it is shortened.
type formattingLambda struct {
	expr   ast.Expr
	params []*ast.Ident
}

// formattingSourceSyntax captures the syntax needed to compare checked types
// before the checker modifies the AST and formatting drops lambda parameters.
func formattingSourceSyntax(file *ast.File) (syntax formattingSyntax) {
	if file == nil {
		return syntax
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch node := node.(type) {
		case *ast.CallExpr:
			syntax.calls = append(syntax.calls, node)
		case *ast.FuncDecorator:
			syntax.calls = append(syntax.calls, &node.CallExpr)
		case *ast.LambdaExpr:
			syntax.lambdas = append(syntax.lambdas, formattingLambda{expr: node, params: node.Lhs})
		case *ast.ArrowExpr:
			syntax.lambdas = append(syntax.lambdas, formattingLambda{expr: node, params: node.Lhs})
		}
		return true
	})
	return syntax
}

// preservesFormattingTypes checks call results and the types of parameters
// retained by lambdas, as well as the contextual results of lambda arguments.
// Removing trailing parameters preserves call and lambda traversal order.
func preservesFormattingTypes(before, after *types.Info, original, preview formattingSyntax) bool {
	if after == nil || len(original.calls) != len(preview.calls) || len(original.lambdas) != len(preview.lambdas) {
		return false
	}
	for i, call := range original.calls {
		if !sameFormattingType(before.TypeOf(call), after.TypeOf(preview.calls[i])) {
			return false
		}
	}
	beforeResults := formattingLambdaResults(before, original.calls)
	afterResults := formattingLambdaResults(after, preview.calls)
	for i, lambda := range original.lambdas {
		next := preview.lambdas[i]
		if len(next.params) > len(lambda.params) || !sameFormattingType(beforeResults[lambda.expr], afterResults[next.expr]) {
			return false
		}
		for j, param := range next.params {
			if !sameFormattingType(before.TypeOf(lambda.params[j]), after.TypeOf(param)) {
				return false
			}
		}
	}
	return true
}

// formattingLambdaResults records the result types supplied by checked calls.
func formattingLambdaResults(info *types.Info, calls []*ast.CallExpr) map[ast.Expr]gotypes.Type {
	results := make(map[ast.Expr]gotypes.Type)
	for _, call := range calls {
		if !xgoutil.IsValidType(info.TypeOf(call)) {
			continue
		}
		for arg := range xgoutil.ResolvedCallExprArgs(info, call) {
			switch expr := astutil.Unparen(arg.Arg).(type) {
			case *ast.LambdaExpr, *ast.ArrowExpr:
				if sig := signatureType(arg.ExpectedType); sig != nil {
					results[expr] = sig.Results()
				}
			}
		}
	}
	return results
}

// sameFormattingType compares known types across separately checked snapshots.
// Package paths distinguish the preview's recreated named and local types.
func sameFormattingType(before, after gotypes.Type) bool {
	if !xgoutil.IsValidType(before) || before == after {
		return true
	}
	if !xgoutil.IsValidType(after) {
		return false
	}
	if results, ok := before.(*gotypes.Tuple); ok {
		next, ok := after.(*gotypes.Tuple)
		if !ok || results.Len() != next.Len() {
			return false
		}
		// Result names do not affect function type identity.
		for i := range results.Len() {
			if !sameFormattingType(results.At(i).Type(), next.At(i).Type()) {
				return false
			}
		}
		return true
	}
	return gotypes.TypeString(before, (*gotypes.Package).Path) == gotypes.TypeString(after, (*gotypes.Package).Path)
}

// hasNewFormattingErrors detects diagnostics introduced by the printed edit.
// In the edited file, match the source line and byte column so unchanged
// diagnostics can move when formatting shifts their line numbers.
func hasNewFormattingErrors(before, after error, filename string, original, formatted []byte) bool {
	if after == nil {
		return false
	}
	if before == nil {
		return true
	}
	type errorKey struct {
		file, line, message string
		offset              int
		code                typesutil.Code
	}
	keyFor := func(err error, content []byte) errorKey {
		typeErr, ok := err.(typesutil.Error)
		if !ok {
			return errorKey{message: err.Error()}
		}
		pos := typeErr.Fset.PositionFor(typeErr.Pos, false)
		key := errorKey{file: pos.Filename, offset: pos.Offset, code: typeErr.Code, message: typeErr.Msg}
		if pos.Filename == filename && pos.Offset <= len(content) {
			start := bytes.LastIndexByte(content[:pos.Offset], '\n') + 1
			line, _, _ := bytes.Cut(content[start:], []byte("\n"))
			key.line = string(line)
			key.offset -= start
		}
		return key
	}
	errorList := func(err error) xerrors.List {
		if list, ok := err.(xerrors.List); ok {
			return list
		}
		return xerrors.List{err}
	}
	existing := make(map[errorKey]int)
	for _, err := range errorList(before) {
		existing[keyFor(err, original)]++
	}
	for _, err := range errorList(after) {
		key := keyFor(err, formatted)
		if existing[key] == 0 {
			return true
		}
		existing[key]--
	}
	return false
}

// formatClassDecls reorders classfile declarations after field grouping.
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

	// Class fields must precede all functions, including explicit receiver methods.
	sortedDecls := make([]ast.Decl, 0, len(astFile.Decls))
	sortedDecls = append(sortedDecls, importDecls...)
	sortedDecls = append(sortedDecls, typeDecls...)
	sortedDecls = append(sortedDecls, constDecls...)
	if varDecl != nil {
		sortedDecls = append(sortedDecls, varDecl)
	}
	sortedDecls = append(sortedDecls, methodDecls...)
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
	return formatClassSource(snapshot, filename, formatted)
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

// unusedLambdaParams finds block lambdas whose unused last parameter can be
// dropped by a candidate overload.
func unusedLambdaParams(proj *xgo.Project, astFile *ast.File) []*ast.LambdaExpr {
	typeInfo, _ := expressionTypeInfo(proj)
	if typeInfo == nil {
		return nil
	}
	var lambdas []*ast.LambdaExpr
	ast.Inspect(astFile, func(n ast.Node) bool {
		callExpr := callExprFromNode(typeInfo, n)
		if callExpr == nil {
			return true
		}
		funcOverloads := callExprOverloads(typeInfo, callExpr)
		if len(funcOverloads) == 0 {
			return true
		}
		for resolvedArg := range formatResolvedCallExprArgs(typeInfo, callExpr, funcOverloads) {
			lambdaExpr, ok := resolvedArg.Arg.(*ast.LambdaExpr)
			if !ok || len(lambdaExpr.Lhs) == 0 {
				continue
			}
			lambdaSig := resolvedLambdaSignature(typeInfo, callExpr, funcOverloads, resolvedArg, len(lambdaExpr.Lhs))
			if lambdaSig == nil {
				continue
			}

			lastParamIdx := len(lambdaExpr.Lhs) - 1
			if isLambdaParamUsed(typeInfo, lambdaExpr, lambdaExpr.Lhs[lastParamIdx]) {
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
				false, // Removing the final parameter also removes any variadic tail.
			)
			for _, overloadType := range funcOverloads {
				_, expectedType, matches := overloadType.matchArgs(typeInfo, callExpr, resolvedArg.Arg)
				if !matches {
					continue
				}
				overloadLambdaSig := signatureType(expectedType)
				if overloadLambdaSig == nil || !gotypes.AssignableTo(newLambdaSig, overloadLambdaSig) {
					continue
				}

				sig, params := overloadType.signature, overloadType.params
				args, _ := xgoutil.CallExprArgsForSignature(typeInfo, callExpr, sig, params)
				argCount := len(args)
				if len(callExpr.Kwargs) > 0 {
					argCount++ // Keyword arguments share one parameter slot.
				}
				typeArgCount := xgoutil.NormalizedCallExprTypeArgCount(overloadType.function(), params)
				if argCount < xgoutil.RequiredParamCount(typeInfo, sig, params, typeArgCount) {
					continue
				}
				lambdas = append(lambdas, lambdaExpr)
				break
			}
		}
		return true
	})
	return lambdas
}

// formatResolvedCallExprArgs returns call arguments with a fallback path for
// overload pseudo-functions that do not expose a normal callable signature.
func formatResolvedCallExprArgs(typeInfo *types.Info, callExpr *ast.CallExpr, overloads []callOverload) iter.Seq[xgoutil.ResolvedCallExprArg] {
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
			for arg := range overload.args(typeInfo, callExpr) {
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

// resolvedLambdaSignature returns a callback signature with the lambda's
// written parameter count. An incomplete call can resolve to another arity.
func resolvedLambdaSignature(typeInfo *types.Info, callExpr *ast.CallExpr, overloads []callOverload, resolvedArg xgoutil.ResolvedCallExprArg, paramCount int) *gotypes.Signature {
	if lambdaSig := signatureType(resolvedArg.ExpectedType); lambdaSig != nil && lambdaSig.Params().Len() == paramCount {
		return lambdaSig
	}

	for _, overloadType := range overloads {
		_, expectedType, matches := overloadType.matchArgs(typeInfo, callExpr, resolvedArg.Arg)
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
	return callExprParamType(sig, params, paramIndex, false)
}

// callExprParamType returns the parameter type for a source argument or for a
// value expanded from a multiple-result call.
func callExprParamType(sig *gotypes.Signature, params *gotypes.Tuple, paramIndex int, expanded bool) gotypes.Type {
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
	if expanded {
		return param.Type()
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

// isLambdaParamUsed reports whether param is used or its use cannot be ruled out.
func isLambdaParamUsed(typeInfo *types.Info, lambdaExpr *ast.LambdaExpr, param *ast.Ident) bool {
	if param.Name == "_" {
		return false
	}
	obj := typeInfo.ObjectOf(param)
	if obj == nil {
		return true
	}
	for _, usedObj := range typeInfo.Uses {
		if usedObj == obj {
			return true
		}
	}

	// Failed expression checking can leave source references out of Uses.
	var used bool
	var inspect func(ast.Node) bool
	inspect = func(node ast.Node) bool {
		if used {
			return false
		}
		switch node := node.(type) {
		case *ast.Ident:
			if node.Name == param.Name && typeInfo.ObjectOf(node) == nil {
				used = true
			}
		case *ast.SelectorExpr:
			// Member names are not parameter references.
			ast.Inspect(node.X, inspect)
			return false
		case *ast.KwargExpr:
			// Keyword names are not parameter references.
			ast.Inspect(node.Value, inspect)
			return false
		case *ast.LabeledStmt:
			// Label names are not parameter references.
			ast.Inspect(node.Stmt, inspect)
			return false
		case *ast.BranchStmt:
			// Branch labels are not parameter references.
			return false
		}
		return true
	}
	ast.Inspect(lambdaExpr.Body, inspect)
	return used
}
