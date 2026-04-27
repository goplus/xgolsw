package server

import (
	"bytes"
	"fmt"
	gotypes "go/types"
	"io/fs"
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
	var astFile *ast.File
	astFile, _ = snapshot.ASTFile(spxFile)
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

	// Get the start position of the shadow entry if it exists and not empty.
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
		varBlocks         []*ast.GenDecl
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
				varBlocks = append(varBlocks, decl)
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

	// Split var blocks into two groups: with initialization and without initialization.
	var (
		varBlocksWithInit    []*ast.GenDecl // Blocks with initialization
		varBlocksWithoutInit []*ast.GenDecl // Blocks without initialization
	)

	for _, decl := range varBlocks {
		// Check if the variable declaration has initialization expressions.
		hasInit := slices.ContainsFunc(decl.Specs, func(spec ast.Spec) bool {
			vs, ok := spec.(*ast.ValueSpec)
			return ok && len(vs.Values) > 0
		})

		if hasInit {
			varBlocksWithInit = append(varBlocksWithInit, decl)
		} else {
			varBlocksWithoutInit = append(varBlocksWithoutInit, decl)
		}
	}

	// Reorder declarations: imports -> types -> consts -> vars (without init) -> vars (with init) -> funcs -> others.
	sortedDecls := make([]ast.Decl, 0, len(astFile.Decls))
	sortedDecls = append(sortedDecls, importDecls...)
	sortedDecls = append(sortedDecls, typeDecls...)
	sortedDecls = append(sortedDecls, methodDecls...)
	sortedDecls = append(sortedDecls, constDecls...)
	if len(varBlocksWithoutInit) > 0 {
		// Add the first var block without initialization to reserve the correct position in declaration order.
		sortedDecls = append(sortedDecls, varBlocksWithoutInit[0])
	}
	if len(varBlocksWithInit) > 0 {
		// Add the first var block with initialization to reserve the correct position in declaration order.
		sortedDecls = append(sortedDecls, varBlocksWithInit[0])
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
			currentVarBlocks := varBlocksWithoutInit
			if len(currentVarBlocks) == 0 || currentVarBlocks[0] != genDecl {
				currentVarBlocks = varBlocksWithInit
			}

			// Process only the same type of var blocks (with or without initialization).
			for i, varBlock := range currentVarBlocks {
				ensureTrailingNewlines(2)

				var doc []byte
				if varBlock.Doc != nil && len(varBlock.Doc.List) > 0 {
					docStart := fset.Position(varBlock.Doc.Pos()).Offset
					docEnd := fset.Position(varBlock.Doc.End()).Offset
					doc = astFile.Code[docStart:docEnd]
				}

				if doc != nil && varBlock.Lparen.IsValid() && (i > 0 || len(currentVarBlocks) == 1) {
					formattedBuf.Write(doc)
					formattedBuf.WriteByte('\n')
					doc = nil
				}

				if i == 0 {
					formattedBuf.WriteString("var (")
				}

				if doc != nil {
					if !varBlock.Lparen.IsValid() || len(currentVarBlocks) > 1 {
						formattedBuf.WriteByte('\n')
					}
					formattedBuf.Write(doc)
					formattedBuf.WriteByte('\n')
				}

				var bodyStartPos token.Pos
				if varBlock.Lparen.IsValid() {
					if cg := findInlineComments(varBlock.Lparen); cg != nil {
						cgStart := fset.Position(cg.Pos()).Offset
						cgEnd := fset.Position(cg.End()).Offset
						formattedBuf.Write(astFile.Code[cgStart:cgEnd])
						formattedBuf.WriteByte('\n')
						if i > 0 {
							formattedBuf.WriteByte('\n')
						}

						bodyStartPos = cg.End() + 1
					} else {
						bodyStartPos = varBlock.Lparen + 1
					}
				} else {
					bodyStartPos = varBlock.Pos() + token.Pos(len(varBlock.Tok.String())) + 1
				}
				var bodyEndPos token.Pos
				if varBlock.Rparen.IsValid() {
					bodyEndPos = varBlock.Rparen - 1
				} else {
					bodyEndPos = varBlock.End()
					if cg := findInlineComments(bodyEndPos); cg != nil {
						bodyEndPos = cg.End()
					}
				}
				bodyStart := fset.Position(bodyStartPos).Offset
				bodyEnd := fset.Position(bodyEndPos).Offset
				formattedBuf.Write(astFile.Code[bodyStart:bodyEnd])
				formattedBuf.WriteByte('\n')

				var trailingComments []byte
				if varBlock.Rparen.IsValid() {
					if cg := findInlineComments(varBlock.Rparen); cg != nil {
						cgStart := fset.Position(cg.Pos()).Offset
						cgEnd := fset.Position(cg.End()).Offset
						trailingComments = astFile.Code[cgStart:cgEnd]
					}
				}

				if i == len(currentVarBlocks)-1 {
					if i > 0 {
						formattedBuf.WriteString("\n\t")
						formattedBuf.Write(trailingComments)
						formattedBuf.WriteByte('\n')
						trailingComments = nil
					}
					formattedBuf.WriteString(")")
					if trailingComments != nil {
						formattedBuf.WriteByte(' ')
						formattedBuf.Write(trailingComments)
					}
				} else if trailingComments != nil {
					formattedBuf.WriteByte('\n')
					formattedBuf.Write(trailingComments)
				}
				formattedBuf.WriteByte('\n')
			}

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

	// Add the shadow entry if it exists and not empty.
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
//  2. The lambda is passed to a function that has a overload which receives the lambda without the parameter.
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
		funIdent, ok := callExpr.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		funType, funTypeOverloads := getFuncAndOverloadsType(proj, funIdent)
		if funType == nil || funTypeOverloads == nil {
			return true
		}
		paramsType := funType.Signature().Params()
		for argIdx, argExpr := range callExpr.Args {
			lambdaExpr, ok := argExpr.(*ast.LambdaExpr2)
			if !ok {
				continue
			}
			if argIdx >= paramsType.Len() {
				break
			}
			lambdaSig, ok := paramsType.At(argIdx).Type().(*gotypes.Signature)
			if !ok {
				continue
			}
			if len(lambdaExpr.Lhs) == 0 {
				continue
			}
			// To simplify the implementation, we only check & process the last parameter,
			// which is enough to cover known cases.
			lastParamIdx := len(lambdaExpr.Lhs) - 1
			if used := isIdentUsed(typeInfo, lambdaExpr.Lhs[lastParamIdx]); used {
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
			for _, overloadType := range funTypeOverloads {
				if overloadType == funType {
					continue
				}
				overloadParamsType := overloadType.Signature().Params()
				if overloadParamsType.Len() != paramsType.Len() {
					continue
				}
				overloadLambdaSig, ok := overloadParamsType.At(argIdx).Type().(*gotypes.Signature)
				if !ok {
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

// getFuncAndOverloadsType returns the function type and all its overloads.
func getFuncAndOverloadsType(proj *xgo.Project, funIdent *ast.Ident) (fun *gotypes.Func, overloads []*gotypes.Func) {
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return
	}
	funTypeObj := typeInfo.ObjectOf(funIdent)
	if funTypeObj == nil {
		return
	}
	funType, ok := funTypeObj.(*gotypes.Func)
	if !ok {
		return
	}
	pkg := funType.Pkg()
	if pkg == nil {
		return
	}
	recvTypeName := SelectorTypeNameForIdent(proj, funIdent)
	if recvTypeName == "" {
		return
	}
	if IsInSpxPkg(funTypeObj) && recvTypeName == "Sprite" {
		recvTypeName = "SpriteImpl"
	}

	recvType := funType.Pkg().Scope().Lookup(recvTypeName).Type()
	if recvType == nil {
		return
	}
	recvNamed, ok := recvType.(*gotypes.Named)
	if !ok || !xgoutil.IsNamedStructType(recvNamed) {
		return
	}
	var underlineFunType *gotypes.Func
	xgoutil.WalkStruct(recvNamed, func(member gotypes.Object, selector *gotypes.Named) bool {
		method, ok := member.(*gotypes.Func)
		if !ok {
			return true
		}
		if pn, overloadID := xgoutil.ParseXGoFuncName(method.Name()); pn == funIdent.Name && overloadID == nil {
			underlineFunType = method
			return false
		}
		return true
	})
	if underlineFunType == nil {
		return
	}
	return funType, xgoutil.ExpandXGoOverloadableFunc(underlineFunType)
}

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
