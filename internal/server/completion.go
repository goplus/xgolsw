package server

import (
	"cmp"
	"fmt"
	gotypes "go/types"
	"iter"
	"path"
	"slices"
	"unicode"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/scanner"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_completion
func (s *Server) textDocumentCompletion(params *CompletionParams) (any, error) {
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
	if !pos.IsValid() {
		return nil, nil
	}
	typeInfo, _ := expressionTypeInfo(proj)
	if typeInfo == nil {
		return nil, nil
	}

	sourcePos := pos
	pos = completionASTPosition(proj, astFile, pos)
	innermostScope := xgoutil.InnermostScopeAt(proj.Fset, typeInfo, astPkg, pos)
	if innermostScope == nil {
		return nil, nil
	}
	clientCapabilities, hasClientCapabilities := s.completionClientCapabilities()
	documentationKind := Markdown
	if hasClientCapabilities {
		documentationKind = preferredMarkupKind(clientCapabilities.CompletionItem.DocumentationFormat)
	}
	enums, err := enumInfoForProject(proj)
	if err != nil {
		return nil, err
	}
	display := newTypeDisplay(proj, astFile, pos)
	display.typeStrings = make(map[gotypes.Type]string)
	ctx := &completionContext{
		definitionContext: definitionContext{
			proj:         proj,
			typeDisplay:  display,
			enumInfo:     enums,
			lookupPkgDoc: s.lookupPkgDoc,
		},
		itemSet:        newCompletionItemSet(documentationKind),
		listPkgs:       s.listPkgs,
		typeInfo:       typeInfo,
		filename:       filename,
		astFile:        astFile,
		astFileScope:   typeInfo.Scopes[astFile],
		tokenFile:      xgoutil.NodeTokenFile(proj.Fset, astFile),
		pos:            pos,
		sourcePos:      sourcePos,
		innermostScope: innermostScope,
	}
	ctx.itemSet.typeInfo = typeInfo
	ctx.itemSet.notRecommended = newNotRecommendedDeclarations(proj, s.lookupPkgDoc).hasDefinition
	ctx.analyze()
	if err := ctx.collect(); err != nil {
		return nil, fmt.Errorf("failed to collect completion items: %w", err)
	}
	items := ctx.sortedItems()
	if hasClientCapabilities {
		adaptCompletionItemsForClient(clientCapabilities, items)
	}
	if ctx.isIncomplete {
		return CompletionList{
			IsIncomplete: true,
			Items:        items,
		}, nil
	}
	return items, nil
}

// completionKind represents different kinds of completion contexts.
type completionKind int

const (
	completionKindUnknown completionKind = iota
	completionKindDisabled
	completionKindGeneral
	completionKindComment
	completionKindStringLit
	completionKindImport
	completionKindDot
	completionKindCall
	completionKindAssignOrDefine
	completionKindDecl
	completionKindReturn
	completionKindStructLit
	completionKindSelect
)

// completionContext represents the context for completion operations.
type completionContext struct {
	definitionContext

	itemSet  *completionItemSet
	listPkgs func() ([]string, error)

	typeInfo       *types.Info
	filename       string
	astFile        *ast.File
	astFileScope   *gotypes.Scope
	tokenFile      *token.File
	pos            token.Pos // Position used for AST lookup.
	sourcePos      token.Pos // Physical cursor position used for edits.
	innermostScope *gotypes.Scope

	kind completionKind

	enclosingNode       ast.Node
	enclosingCallExpr   *ast.CallExpr
	calleeCall          *ast.CallExpr
	selectorExpr        *ast.SelectorExpr
	expectedTypes       []gotypes.Type
	unqualifiedTypeArgs []gotypes.Type
	writtenValues       map[ast.Expr]gotypes.TypeAndValue
	enumContext         enumIdentContext
	expectedStructType  *gotypes.Struct
	compositeLitType    gotypes.Type
	assignTargets       []gotypes.Object
	declValueSpec       *ast.ValueSpec
	returnIndex         int

	inStringLit             bool
	stringLit               *ast.BasicLit
	inCallKwargName         bool
	inFuncDecorator         bool
	inFrameworkEventHandler bool
	valueExpression         bool
	typeExpression          bool
	expectedFuncResultCount int

	// isIncomplete is set by collectors whose results depend on continued typing.
	isIncomplete bool
}

// getEnclosingCallExpr returns the closest call expression in the current
// completion context.
func (ctx *completionContext) getEnclosingCallExpr() *ast.CallExpr {
	if callExpr, ok := ctx.enclosingNode.(*ast.CallExpr); ok {
		return callExpr
	}
	return ctx.enclosingCallExpr
}

// analyze analyzes the completion context to determine the kind of completion needed.
func (ctx *completionContext) analyze() {
	path, _ := xgoutil.PathEnclosingInterval(ctx.astFile, ctx.pos-1, ctx.pos)
	if ctx.isInDisabledIdentifierContext(path) {
		ctx.kind = completionKindDisabled
		return
	}
	for i, node := range slices.Backward(path) {
		switch node := node.(type) {
		case *ast.ImportSpec:
			ctx.kind = completionKindImport
		case *ast.SelectorExpr:
			if node.Sel == nil || node.X.End() < ctx.pos && ctx.pos <= node.Sel.End() {
				ctx.kind = completionKindDot
				ctx.selectorExpr = node
			}
			if node.X.Pos() <= ctx.pos && ctx.pos <= node.X.End() {
				ctx.analyzeValueOperand(node.X, path[i:])
			}
		case *ast.KwargExpr:
			if ctx.pos <= node.Name.End() {
				ctx.inCallKwargName = true
			}
		case *ast.CallExpr:
			ctx.analyzeCallExpr(node, false)
		case *ast.FuncDecorator:
			ctx.analyzeCallExpr(&node.CallExpr, true)
		case *ast.BranchStmt:
			if call := callExprFromNode(ctx.typeInfo, node); call != nil {
				ctx.analyzeCallExpr(call, false)
			}
		case *ast.TupleLit, *ast.SliceLit, *ast.MatrixLit:
			ctx.analyzeLiteralElements(path[i:])
		case *ast.CompositeLit:
			literalTypes := literalTypes(ctx.typeInfo, path[i:])
			if len(literalTypes) == 0 {
				continue
			}
			typ := xgoutil.DerefType(literalTypes[0])
			st, isStruct := typ.Underlying().(*gotypes.Struct)
			inFieldName := isStruct && slices.ContainsFunc(node.Elts, func(expr ast.Expr) bool {
				ident, ok := expr.(*ast.Ident)
				return ok && ident.Pos() <= ctx.pos && ctx.pos <= ident.End() && !xgoutil.IsValidType(ctx.typeInfo.TypeOf(ident))
			})
			if !inFieldName && ctx.analyzeLiteralElements(path[i:]) {
				continue
			}
			if !isStruct {
				ctx.kind = completionKindGeneral
				ctx.valueExpression = true
				ctx.expectedTypes = nil
				ctx.expectedFuncResultCount = 0
				continue
			}
			ctx.kind = completionKindStructLit
			ctx.expectedStructType = st
			ctx.compositeLitType = typ
			ctx.enclosingNode = node
		case *ast.AssignStmt:
			if ctx.isAfterNumberLiteral() {
				continue
			}
			for expr, typ := range contextualValueTypes(ctx.typeInfo, path[i:]) {
				if expr.Pos() > ctx.pos || ctx.pos > expr.End() {
					continue
				}
				ctx.kind = completionKindAssignOrDefine
				ctx.valueExpression = true
				ctx.expectedTypes = validExpectedType(typ)
				index := slices.Index(node.Rhs, expr)
				if index < len(node.Lhs) {
					if ident, ok := node.Lhs[index].(*ast.Ident); ok {
						if obj := ctx.typeInfo.ObjectOf(ident); obj != nil {
							ctx.assignTargets = append(ctx.assignTargets, obj)
						}
					}
				}
				if len(node.Lhs) > 1 && len(node.Rhs) == 1 {
					ctx.expectedFuncResultCount = len(node.Lhs)
					// The user can still add another expression after this one.
					ctx.expectedTypes = append(ctx.expectedTypes, validExpectedType(ctx.typeInfo.TypeOf(node.Lhs[0]))...)
				}
				break
			}
		case *ast.ReturnStmt:
			sig := enclosingFunctionSignature(ctx.typeInfo, path[i+1:])
			if sig == nil || sig.Results().Len() == 0 {
				continue
			}
			ctx.kind = completionKindReturn
			ctx.valueExpression = true
			ctx.returnIndex = ctx.findReturnValueIndex(node)
			ctx.expectedTypes = validExpectedType(valueListType(len(node.Results), ctx.returnIndex, resultTypes(sig)))
		case *ast.GoStmt:
			if ctx.enclosingCallExpr == nil {
				ctx.enclosingCallExpr = node.Call
			}
			ctx.kind = completionKindCall
			ctx.enclosingNode = node.Call
			ctx.valueExpression = true
		case *ast.DeferStmt:
			if ctx.enclosingCallExpr == nil {
				ctx.enclosingCallExpr = node.Call
			}
			ctx.kind = completionKindCall
			ctx.enclosingNode = node.Call
			ctx.valueExpression = true
		case *ast.CaseClause:
			if ctx.pos <= node.Colon {
				ctx.kind = completionKindGeneral
				ctx.valueExpression = true
				ctx.expectedTypes = switchCaseTypes(ctx.typeInfo, path[i+1:])
				ctx.expectedFuncResultCount = 0
			}
		case *ast.SelectStmt:
			ctx.kind = completionKindSelect
		case *ast.ValueSpec:
			if len(node.Names) == 0 || ctx.isAfterNumberLiteral() {
				continue
			}
			ctx.kind = completionKindDecl
			ctx.expectedTypes = validExpectedType(ctx.typeInfo.TypeOf(node.Type))
			for expr, typ := range contextualValueTypes(ctx.typeInfo, path[i:]) {
				if expr.Pos() <= ctx.pos && ctx.pos <= expr.End() {
					ctx.expectedTypes = validExpectedType(typ)
					break
				}
			}
			for _, name := range node.Names {
				if obj := ctx.typeInfo.ObjectOf(name); obj != nil {
					ctx.assignTargets = append(ctx.assignTargets, obj)
				}
			}
			ctx.declValueSpec = node
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				if ctx.kind == completionKindUnknown {
					ctx.kind = completionKindStringLit
				}
				ctx.inStringLit = true
				ctx.stringLit = node
			}
		case *ast.BlockStmt:
			ctx.kind = completionKindUnknown
			ctx.expectedTypes = nil
			ctx.expectedFuncResultCount = 0
		default:
			for _, expr := range valueOperands(ctx.typeInfo, node) {
				ctx.analyzeValueOperand(expr, path[i:])
			}
		}
	}
	if ctx.isInTypeExpression(path) {
		ctx.typeExpression = true
		if ctx.kind != completionKindDot {
			ctx.kind = completionKindGeneral
		}
		ctx.expectedTypes = nil
		ctx.expectedFuncResultCount = 0
		ctx.itemSet.callResult = false
	}
	if ctx.kind != completionKindDot {
		ctx.unqualifiedTypeArgs = ctx.indexedCompletionTypeArguments(path)
	}
	if ctx.kind == completionKindUnknown {
		switch {
		case ctx.isInComment():
			ctx.kind = completionKindComment
		case ctx.isInImportStringLit():
			ctx.kind = completionKindImport
			ctx.inStringLit = true
		case ctx.isLineStart(), ctx.isInIdentifier():
			if !ctx.isAfterNumberLiteral() {
				ctx.kind = completionKindGeneral
			}
		}
	}
	if len(ctx.enumInfo.members) > 0 {
		if ident := xgoutil.EnclosingNode[*ast.Ident](path); ident != nil {
			ctx.enumContext = enumContextAtIdent(ctx.proj, ident)
		}
	}

	if ctx.frameworkAdapter() != nil {
		ctx.inFrameworkEventHandler = ctx.isInFrameworkEventHandler(ctx.pos)
	}
}

// analyzeValueOperand prevents an enclosing result type from constraining a
// nested operand with its own type relationship.
func (ctx *completionContext) analyzeValueOperand(expr ast.Expr, outer []ast.Node) {
	if expr == nil || ctx.pos < expr.Pos() || ctx.pos > expr.End() {
		return
	}
	ctx.kind = completionKindGeneral
	ctx.valueExpression = true
	ctx.expectedFuncResultCount = 0
	ctx.expectedTypes = expectedExprTypes(ctx.typeInfo, append([]ast.Node{expr}, outer...))
}

// isInTypeExpression distinguishes annotations and generic type arguments from
// runtime indices and array lengths, including values nested inside a type.
func (ctx *completionContext) isInTypeExpression(path []ast.Node) bool {
	for _, node := range path {
		var base ast.Expr
		var lbrack, rbrack token.Pos
		switch node := node.(type) {
		case *ast.ValueSpec:
			return node.Type != nil && node.Type.Pos() <= ctx.pos && ctx.pos <= node.Type.End()
		case *ast.TypeSpec:
			return node.Type.Pos() <= ctx.pos && ctx.pos <= node.Type.End()
		case *ast.Field:
			return node.Type != nil && node.Type.Pos() <= ctx.pos && ctx.pos <= node.Type.End()
		case *ast.CompositeLit:
			return node.Type != nil && node.Type.Pos() <= ctx.pos && ctx.pos <= node.Type.End()
		case *ast.TypeAssertExpr:
			return node.Type != nil && node.Type.Pos() <= ctx.pos && ctx.pos <= node.Type.End()
		case *ast.FuncDecl:
			// PathEnclosingInterval flattens FuncType under declarations, so
			// the start of an unparenthesized result can have no type ancestor.
			results := node.Type.Results
			return results != nil && results.Pos() <= ctx.pos && ctx.pos <= results.End()
		case *ast.ArrayType:
			if node.Len != nil && node.Len.Pos() <= ctx.pos && ctx.pos <= node.Len.End() {
				return false
			}
			return true
		case *ast.MapType, *ast.ChanType, *ast.FuncType:
			return true
		case *ast.IndexExpr:
			base, lbrack, rbrack = node.X, node.Lbrack, node.Rbrack
		case *ast.IndexListExpr:
			base, lbrack, rbrack = node.X, node.Lbrack, node.Rbrack
		default:
			continue
		}
		if ctx.pos <= lbrack || rbrack.IsValid() && ctx.pos > rbrack {
			continue
		}
		// Use the declaration because the compiler may record an instantiated
		// type on the base expression. Container variables remain value indices.
		var obj gotypes.Object
		switch base := astutil.Unparen(base).(type) {
		case *ast.Ident:
			obj = ctx.typeInfo.ObjectOf(base)
		case *ast.SelectorExpr:
			obj = ctx.typeInfo.ObjectOf(base.Sel)
			if fun, ok := obj.(*gotypes.Func); ok && fun.Signature().TypeParams().Len() == 0 {
				if isType, _ := xgoutil.IsTypeExpr(ctx.typeInfo, base.X); isType {
					if impl, _ := xgoutil.ReconstructedMethodImplementation(fun); impl != nil {
						obj = impl
					}
				}
			}
		}
		switch obj.(type) {
		case *gotypes.Func, *gotypes.TypeName:
			if generic, ok := obj.Type().(interface{ TypeParams() *gotypes.TypeParamList }); ok && generic.TypeParams().Len() > 0 {
				return true
			}
		}
	}
	return false
}

// analyzeLiteralElements gives a literal element its own completion context.
func (ctx *completionContext) analyzeLiteralElements(path []ast.Node) bool {
	var expectedTypes []gotypes.Type
	var edited ast.Expr
	for expr, typ := range contextualValueTypes(ctx.typeInfo, path) {
		if expr.Pos() <= ctx.pos && ctx.pos <= expr.End() {
			edited = expr
			if xgoutil.IsValidType(typ) {
				expectedTypes = append(expectedTypes, typ)
			}
		}
	}
	if _, tuple := path[0].(*ast.TupleLit); tuple && edited != nil {
		expectedTypes = append(expectedTypes, expandedTupleArgumentTypes(ctx.typeInfo, path, edited)...)
	}
	found := edited != nil
	if !found {
		for _, typ := range literalTypes(ctx.typeInfo, path) {
			var elementType gotypes.Type
			switch literal := path[0].(type) {
			case *ast.SliceLit:
				elementType = collectionElementType(typ)
			case *ast.TupleLit:
				index := len(literal.Elts)
				for i, element := range literal.Elts {
					if ctx.pos <= element.End() {
						index = i
						break
					}
				}
				elementType = tupleElementType(typ, index)
			case *ast.MatrixLit:
				elementType = collectionElementType(collectionElementType(typ))
			case *ast.CompositeLit:
				switch container := xgoutil.DerefType(typ).Underlying().(type) {
				case *gotypes.Array, *gotypes.Slice:
					elementType = collectionElementType(container)
				case *gotypes.Map:
					elementType = container.Key()
				}
			}
			if xgoutil.IsValidType(elementType) {
				expectedTypes = append(expectedTypes, elementType)
				found = true
			}
		}
	}

	if found {
		ctx.kind = completionKindGeneral
		ctx.valueExpression = true
		ctx.expectedTypes = deduplicateTypes(expectedTypes)
		ctx.expectedFuncResultCount = 0
	}
	return found
}

// analyzeCallExpr updates the completion context for a call expression.
func (ctx *completionContext) analyzeCallExpr(callExpr *ast.CallExpr, isFuncDecorator bool) {
	// A call used as a callee must return a function, independently of the
	// final call's result type. Direct callees keep the surrounding context.
	if callExpr.Fun.Pos() <= ctx.pos && ctx.pos <= callExpr.Fun.End() {
		callee := xgoutil.CallExprFun(callExpr)
		if _, ok := astutil.Unparen(callee).(*ast.CallExpr); ok {
			ctx.analyzeValueOperand(callee, []ast.Node{callExpr})
		}
		if ident := xgoutil.CallExprFunIdent(callExpr); ident != nil && ident.Pos() <= ctx.pos && ctx.pos <= ident.End() {
			ctx.itemSet.callResult = true
			ctx.calleeCall = callExpr
			if callee != callExpr.Fun {
				errorType := gotypes.Universe.Lookup("error").Type()
				ctx.itemSet.isCompatibleWithCallResults = func(results *gotypes.Tuple) bool {
					return results.Len() > 0 && gotypes.AssignableTo(results.At(results.Len()-1).Type(), errorType)
				}
			}
		}
		return
	}
	if ctx.enclosingCallExpr == nil {
		ctx.enclosingCallExpr = callExpr
		ctx.inFuncDecorator = isFuncDecorator
	}
	ctx.kind = completionKindCall
	ctx.enclosingNode = callExpr
	ctx.expectedTypes = nil
	ctx.expectedFuncResultCount = 0
	ctx.valueExpression = true
}

// isInDisabledIdentifierContext reports whether the completion position is
// inside an identifier context where completion should be suppressed.
func (ctx *completionContext) isInDisabledIdentifierContext(path []ast.Node) bool {
	ident := xgoutil.EnclosingNode[*ast.Ident](path)
	if ident == nil {
		return false
	}

	if ctx.astFile.HasPkgDecl() && ctx.astFile.Name == ident {
		return true
	}

	if funcDecl := xgoutil.EnclosingNode[*ast.FuncDecl](path); funcDecl != nil && funcDecl.Name == ident {
		return true
	}

	if overloadDecl := xgoutil.EnclosingNode[*ast.OverloadFuncDecl](path); overloadDecl != nil && overloadDecl.Name == ident {
		return true
	}

	if importSpec := xgoutil.EnclosingNode[*ast.ImportSpec](path); importSpec != nil && importSpec.Name == ident {
		return true
	}

	if typeSpec := xgoutil.EnclosingNode[*ast.TypeSpec](path); typeSpec != nil && typeSpec.Name == ident {
		return true
	}

	if valueSpec := xgoutil.EnclosingNode[*ast.ValueSpec](path); valueSpec != nil {
		if slices.Contains(valueSpec.Names, ident) {
			return true
		}
	}

	if field := xgoutil.EnclosingNode[*ast.Field](path); field != nil && slices.Contains(field.Names, ident) {
		return true
	}

	if labeledStmt := xgoutil.EnclosingNode[*ast.LabeledStmt](path); labeledStmt != nil && labeledStmt.Label == ident {
		return true
	}

	return false
}

// isInComment reports whether the position of the current completion context
// is inside a comment.
func (ctx *completionContext) isInComment() bool {
	for _, comment := range ctx.astFile.Comments {
		if comment.Pos() <= ctx.pos && ctx.pos <= comment.End() {
			return true
		}
	}
	return false
}

// isInImportStringLit reports whether the position of the current completion
// context is inside an import string literal.
func (ctx *completionContext) isInImportStringLit() bool {
	var s scanner.Scanner
	s.Init(ctx.tokenFile, ctx.astFile.Code, nil, 0)

	var (
		lastPos       token.Pos
		lastTok       token.Token
		inImportGroup bool
	)
	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}

		// Track if we're inside an import group.
		if lastTok == token.IMPORT && tok == token.LPAREN {
			inImportGroup = true
		} else if tok == token.RPAREN {
			inImportGroup = false
		}

		// Check if we found `import` followed by `"` or we're in an import group.
		if (lastTok == token.IMPORT || inImportGroup) &&
			(tok == token.STRING || tok == token.ILLEGAL) {
			// Check if position is after `import` keyword or within an import
			// group, and inside a string literal (complete or incomplete).
			if lastPos <= ctx.pos && ctx.pos <= pos+token.Pos(len(lit)) {
				return true
			}
		}

		lastPos = pos
		lastTok = tok
	}
	return false
}

// isLineStart reports whether the position is preceded by only whitespace, or
// by a continuous sequence of non-whitespace characters (like an identifier or
// a member access expression).
func (ctx *completionContext) isLineStart() bool {
	fileBase := token.Pos(ctx.tokenFile.Base())
	relPos := ctx.pos - fileBase
	if relPos < 0 || int(relPos) > len(ctx.astFile.Code) {
		return false
	}

	offsets := ctx.proj.ASTFileLineOffsets(ctx.astFile)
	line := lineForOffset(offsets, int(relPos))
	relLineStartPos := token.Pos(offsets[line])
	if int(relLineStartPos) >= len(ctx.astFile.Code) {
		return false
	}

	for pos := relLineStartPos; pos < relPos; pos++ {
		if !unicode.IsSpace(rune(ctx.astFile.Code[pos])) {
			text := string(ctx.astFile.Code[pos:relPos])
			return !slices.ContainsFunc([]rune(text), unicode.IsSpace)
		}
	}
	return true
}

// isInIdentifier reports whether the position is within an identifier.
func (ctx *completionContext) isInIdentifier() bool {
	fileBase := token.Pos(ctx.tokenFile.Base())
	relPos := ctx.pos - fileBase
	if relPos < 0 || int(relPos) > len(ctx.astFile.Code) {
		return false
	}

	var s scanner.Scanner
	s.Init(ctx.tokenFile, ctx.astFile.Code, nil, 0)

	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}

		// Check if position is inside this token. For identifiers, we should
		// be either in the middle or at the end to trigger completion (not
		// at the beginning).
		if pos < ctx.pos && ctx.pos <= pos+token.Pos(len(lit)) {
			return tok == token.IDENT
		}

		// If we've scanned past our position, we're not in an identifier.
		if pos > ctx.pos {
			break
		}
	}
	return false
}

// isAfterNumberLiteral reports whether the position is immediately after a
// number literal followed by a dot.
func (ctx *completionContext) isAfterNumberLiteral() bool {
	fileBase := token.Pos(ctx.tokenFile.Base())
	relPos := ctx.pos - fileBase
	if relPos < 1 || int(relPos) > len(ctx.astFile.Code) {
		return false
	}

	// Check if the previous character is a dot.
	if ctx.astFile.Code[relPos-1] != '.' {
		return false
	}

	// Check if before the dot is a number.
	if relPos < 2 {
		return false
	}

	// Scan backwards to find the start of the number.
	foundDigit := false
	for i := relPos - 2; i >= 0; i-- {
		ch := ctx.astFile.Code[i]
		if unicode.IsDigit(rune(ch)) {
			foundDigit = true
		} else if foundDigit {
			// Found a non-digit character after finding digits.
			// Check if it's a valid number terminator.
			if unicode.IsSpace(rune(ch)) {
				return true
			}
			switch ch {
			case '=', '(', '{', '\t', '\n':
				return true
			}
			// Found invalid character, not a number literal.
			return false
		} else {
			// Haven't found any digits yet and found a non-digit.
			return false
		}
	}

	// We scanned to the beginning and found only digits.
	return foundDigit
}

// findReturnValueIndex finds the index of the return value at the current position.
func (ctx *completionContext) findReturnValueIndex(ret *ast.ReturnStmt) int {
	if len(ret.Results) == 0 {
		return 0
	}
	for i, expr := range ret.Results {
		if ctx.pos >= expr.Pos() && ctx.pos <= expr.End() {
			return i
		}
	}
	if ctx.pos > ret.Results[len(ret.Results)-1].End() {
		return len(ret.Results)
	}
	return -1
}

// collect collects completion items based on the completion context kind.
func (ctx *completionContext) collect() error {
	if ctx.typeExpression {
		ctx.itemSet.setSupportedKinds(ClassCompletion, EnumCompletion, InterfaceCompletion, StructCompletion, ModuleCompletion)
	}
	switch ctx.kind {
	case completionKindDisabled:
		return nil
	case completionKindComment,
		completionKindStringLit:
		return nil
	case completionKindGeneral, completionKindAssignOrDefine, completionKindDecl, completionKindReturn:
		return ctx.collectGeneral()
	case completionKindImport:
		return ctx.collectImport()
	case completionKindDot:
		return ctx.collectDot()
	case completionKindCall:
		return ctx.collectCall()
	case completionKindStructLit:
		return ctx.collectStructLit()
	case completionKindSelect:
		return ctx.collectSelect()
	}
	return nil
}

// collectGeneral collects general completions.
func (ctx *completionContext) collectGeneral() error {
	if ctx.collectXGoUnitCompletions(xgoUnitExpectedTypesAtPosition(ctx.typeInfo, ctx.astFile, ctx.pos)) {
		return nil
	}

	enumContext := ctx.enumContext
	if enumContext.status == enumContextUnknown && len(ctx.expectedTypes) > 0 {
		enumContext.status = enumContextAllowed
		enumContext.expectedTypes = ctx.expectedTypes
	}
	ctx.addVisibleEnumMembers(enumContext.expectedTypes...)

	if ctx.stringLit != nil || len(ctx.expectedTypes) != 0 {
		framework, err := analyzeFramework(ctx.proj)
		if err != nil {
			return err
		}
		if framework != nil && framework.collectCompletions != nil {
			framework.collectCompletions(ctx)
		}
	}

	if ctx.inStringLit {
		return nil
	}

	switch ctx.kind {
	case completionKindDecl:
		if ctx.declValueSpec.Values == nil { // var x in|
			ctx.itemSet.setSupportedKinds(
				ClassCompletion,
				EnumCompletion,
				InterfaceCompletion,
				StructCompletion,
				ModuleCompletion,
			)
			break
		}
		fallthrough
	case completionKindAssignOrDefine:
		ctx.itemSet.setSupportedKinds(
			VariableCompletion,
			ConstantCompletion,
			EnumMemberCompletion,
			FunctionCompletion,
			FieldCompletion,
			MethodCompletion,
			ClassCompletion,
			InterfaceCompletion,
			StructCompletion,
			KeywordCompletion,
			TextCompletion,
			ModuleCompletion,
		)
	}
	if ctx.expectedFuncResultCount > 0 {
		ctx.itemSet.setExpectedFuncResultCount(ctx.expectedFuncResultCount)
	}
	ctx.itemSet.setExpectedTypes(ctx.expectedTypes)
	ctx.itemSet.setEnumContext(enumContext)

	// Add local definitions from innermost scope and its parents.
	pkg := ctx.typeInfo.Pkg
	seenNames := make(map[string]bool)
	seenTypes := make(map[string]bool)
	seenQualifiers := make(map[string]bool)
	memberNames := make(map[string]bool)
	autoProperty := ctx.unqualifiedTypeArgs == nil && !ctx.itemSet.callResult &&
		(ctx.valueExpression || ctx.kind == completionKindDecl)
	packageResolver := autoPropertyResolver{proj: ctx.proj}
	addDefinitions := func(seen map[string]bool, defs ...symbolDefinition) {
		if autoProperty {
			defs = ctx.appendPackagePropertyDefinitions(&packageResolver, defs)
		}
		for _, def := range defs {
			if ctx.unqualifiedTypeArgs != nil {
				if fun, ok := def.SourceObject.(*gotypes.Func); ok && (ctx.typeExpression || fun.Signature().Recv() != nil) {
					continue
				}
				var ok bool
				if def, ok = ctx.indexedPackageDefinition(&packageResolver, def, ctx.unqualifiedTypeArgs); !ok {
					continue
				}
			} else if source, ok := def.SourceObject.(*gotypes.Func); ok && source.Signature().Recv() == nil {
				var ok bool
				if def, ok = ctx.functionCompletionDefinition(&packageResolver, def, autoProperty); !ok {
					continue
				}
			}
			if !seen[ctx.itemSet.definitionName(def)] {
				ctx.itemSet.addDefinitions(def)
			}
		}
	}
	receiver := xgoutil.ClassReceiverAt(ctx.typeInfo, ctx.astFile, ctx.pos)
	classType := resolvedNamedType(receiver)
	if receiver == nil && ctx.innermostScope == ctx.astFileScope && ctx.kind == completionKindGeneral {
		// A new top-level statement may precede creation of the shadow entry.
		if classType = ctx.typeInfo.ClassTypes[ctx.astFile]; classType != nil {
			receiver = gotypes.NewPointer(classType)
		}
	}
	var parents map[ast.Node]ast.Node
	for scope := ctx.innermostScope; scope != nil && scope != gotypes.Universe; scope = scope.Parent() {
		// Locals take precedence over class members, which take precedence
		// over package declarations and imports.
		if scope == pkg.Scope() && classType != nil &&
			!ctx.typeExpression && !(ctx.kind == completionKindDecl && ctx.declValueSpec.Values == nil) {
			resolver := newClassAutoPropertyResolver(ctx.proj, receiver)
			for member := range xgoutil.StructMembers(classType, ctx.isFrameworkBaseType) {
				if ctx.inFrameworkEventHandler && ctx.isFrameworkEventHandler(member.Member) {
					continue
				}
				if name := member.Member.Name(); !seenNames[name] {
					memberNames[name] = true
					seenQualifiers[name] = true
				}
			}
			defs := ctx.definitionsForInstanceMembers(classType)
			if ctx.inFrameworkEventHandler {
				defs = slices.DeleteFunc(defs, func(def symbolDefinition) bool {
					return ctx.isFrameworkEventHandler(def.SourceObject)
				})
			}
			defs = ctx.appendAutoPropertyDefinitions(resolver, defs)
			for _, def := range defs {
				if def.Implementation == nil {
					continue
				}
				name := def.SourceObject.Name()
				if !seenNames[name] {
					memberNames[name] = true
					seenQualifiers[name] = true
				}
				// Bare aliases and selector qualifiers require an auto-property.
				// Explicit calls also resolve methods that require arguments.
				for _, alias := range []string{xgoutil.FunctionAliasName(name), def.CompletionItemLabel} {
					property := resolver.resolve(alias).exists
					if (!autoProperty || property) && !seenNames[alias] {
						memberNames[alias] = true
					}
					if property {
						seenQualifiers[alias] = true
					}
				}
			}
			for _, def := range defs {
				var ok bool
				if def, ok = ctx.instanceCompletionDefinition(resolver, def, autoProperty); !ok {
					continue
				}
				addDefinitions(seenNames, def)
			}
			// Reserve original member names even when overload expansion or
			// callback insertion uses a different spelling.
			for name := range memberNames {
				seenNames[name] = true
			}
		}
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if _, ok := obj.(*gotypes.PkgName); ok {
				continue
			}
			if variable, ok := obj.(*gotypes.Var); ok && name != "this" && isGeneratedVariable(ctx.proj, variable) {
				continue
			}
			if scope != pkg.Scope() && scope != ctx.astFileScope {
				if ident := ctx.typeInfo.ObjToDef[obj]; ident != nil {
					if parents == nil {
						parents = nodeParents(ctx.astFile)
					}
					start, end := objectUnavailableRange(ctx.typeInfo, ctx.astFile, obj, parents)
					if start <= ctx.pos && ctx.pos < end {
						continue
					}
				}
			}
			if !xgoutil.IsExportedOrInMainPkg(obj) {
				continue
			}
			seenQualifiers[name] = true
			// A visible inner declaration hides outer names even when the
			// inner object is incompatible with the expected completion type.
			if seenNames[name] {
				// Class members do not shadow types in type annotations. Keep
				// those types available alongside value candidates.
				if _, ok := obj.(*gotypes.TypeName); ok && memberNames[name] {
					ctx.itemSet.addDefinitions(ctx.definitionsFor(obj, "")...)
				}
				// A package alias can remain accessible when a local hides its
				// uppercase declaration. Filter by the final insertion name.
				if isAliasCallable(obj) && scope == pkg.Scope() {
					addDefinitions(seenNames, ctx.definitionsFor(obj, "")...)
				}
				continue
			}
			if _, ok := obj.(*gotypes.TypeName); ok {
				seenTypes[name] = true
			}
			if ctx.valueExpression || !slices.Contains(ctx.assignTargets, obj) {
				addDefinitions(seenNames, ctx.definitionsFor(obj, "")...)
			}
			seenNames[name] = true
		}
	}

	// Add package qualifiers without hiding bare names from lookup packages.
	imports := importsForFile(ctx.proj, ctx.astFile)
	for _, name := range imports.names {
		if ctx.typeExpression || !seenQualifiers[name.Name()] {
			// A package begins a selector rather than supplying a value or type
			// itself. Keep each alias independently of expected-type filtering.
			doc, _ := ctx.lookupPkgDoc(name.Imported().Path())
			ctx.itemSet.add(definitionForPkg(name, doc).completionItem(ctx.itemSet.documentationKind))
		}
	}
	// Package aliases resolve after named imports but before lookup packages.
	// They depend on package declarations even when a local hides the uppercase name.
	if !ctx.typeExpression {
		for _, name := range pkg.Scope().Names() {
			if !autoProperty || gogen.HasAutoProperty(pkg.Scope().Lookup(name).Type()) {
				seenNames[xgoutil.FunctionAliasName(name)] = true
			}
		}
	}
	importNames := seenNames
	if ctx.typeExpression {
		// Type lookup falls back to imported types after a lexical value.
		// Keep lexical shadowing intact for outer declarations and builtins.
		importNames = seenTypes
	}
	for name := range imports.ambiguous {
		importNames[name] = true
	}

	// Add other definitions.
	for _, pkg := range imports.members {
		pkgDoc, _ := ctx.lookupPkgDoc(pkg.Path())
		addDefinitions(importNames, ctx.definitionsForPkg(pkg, pkgDoc)...)
	}
	// Lookup packages take precedence over builtins. Reserve source bindings
	// independently of which spelling or signature was offered as a candidate.
	if !ctx.typeExpression {
		for _, pkg := range imports.members {
			for _, name := range pkg.Scope().Names() {
				obj := pkg.Scope().Lookup(name)
				if !obj.Exported() {
					continue
				}
				seenNames[name] = true
				if gogen.IsFunc(obj.Type()) && (!autoProperty || gogen.HasAutoProperty(obj.Type())) {
					seenNames[xgoutil.FunctionAliasName(name)] = true
				}
			}
		}
	}
	addDefinitions(seenNames, ctx.builtinDefinitions(ctx.proj, ctx.lookupPkgDoc)...)
	ctx.itemSet.addDefinitions(generalCompletionSnippets...)
	if ctx.innermostScope == ctx.astFileScope {
		ctx.itemSet.addDefinitions(fileScopeCompletionSnippets...)
	}

	return nil
}

// collectImport collects import completions.
func (ctx *completionContext) collectImport() error {
	pkgs, err := ctx.listPkgs()
	if err != nil {
		return fmt.Errorf("failed to list packages: %w", err)
	}
	for _, pkgPath := range pkgs {
		var detail string
		if pkgDoc, _ := ctx.lookupPkgDoc(pkgPath); pkgDoc != nil {
			detail = pkgDoc.Doc
		}
		ctx.itemSet.addDefinitions(symbolDefinition{
			ID: XGoDefinitionIdentifier{
				Package: &pkgPath,
			},
			Overview: "package " + path.Base(pkgPath),
			Detail:   detail,

			CompletionItemLabel:            pkgPath,
			CompletionItemKind:             ModuleCompletion,
			CompletionItemInsertText:       pkgPath,
			CompletionItemInsertTextFormat: PlainTextTextFormat,
		})
	}
	return nil
}

// collectDot collects dot completions for member access.
func (ctx *completionContext) collectDot() error {
	if ctx.selectorExpr == nil {
		return nil
	}
	path, _ := xgoutil.PathEnclosingInterval(ctx.astFile, ctx.selectorExpr.Pos(), ctx.selectorExpr.End())
	if !ctx.itemSet.callResult {
		ctx.itemSet.setExpectedTypes(expectedExprTypes(ctx.typeInfo, path))
	} else if call, ok := ctx.enclosingNode.(*ast.CallExpr); ok {
		ctx.setCallResultContext(call)
	}

	if ident, ok := ctx.selectorExpr.X.(*ast.Ident); ok {
		if pkgName := ctx.selectorPackage(ident, ctx.typeExpression); pkgName != nil {
			ctx.collectPackageMembers(pkgName.Imported(), ctx.indexedCompletionTypeArguments(path))
			return nil
		}
	}

	typ := ctx.typeInfo.TypeOf(ctx.selectorExpr.X)
	if !xgoutil.IsValidType(typ) {
		return nil
	}
	receiver := typ
	typ = gotypes.Unalias(xgoutil.DerefType(gotypes.Unalias(typ)))

	var defs []symbolDefinition
	_, isInterface := typ.Underlying().(*gotypes.Interface)
	isType, _ := xgoutil.IsTypeExpr(ctx.typeInfo, ctx.selectorExpr.X)
	_, isAlias := receiver.(*gotypes.Alias)
	// XGo unwraps a direct type alias for static value lookup only.
	aliasSelector := isType && isAlias
	if !aliasSelector {
		if isInterface || isType {
			defs = ctx.definitionsForSelectorMethods(receiver, isType)
		} else {
			defs = ctx.definitionsForInstanceMembers(receiver)
		}
	}
	// Static values belong to the declaring type, including through aliases.
	if named, ok := typ.(*gotypes.Named); ok && isType {
		for method := range named.Origin().Methods() {
			obj := types.StaticMemberObject(method)
			if _, callable := obj.(*gotypes.Func); obj != nil && !callable && xgoutil.IsExportedOrInMainPkg(obj) {
				defs = append(defs, ctx.staticMemberDefinitions(method, obj, nil)...)
			}
		}
	}
	if len(defs) > 0 {
		var typeArgs []gotypes.Type
		if isType {
			typeArgs = ctx.indexedCompletionTypeArguments(path)
		}
		ctx.addMemberCompletions(receiver, isType, typeArgs, defs...)
	}
	return nil
}

// addMemberCompletions uses the source selector's signature when completing a
// callback, including the explicit receiver of a method expression.
func (ctx *completionContext) addMemberCompletions(receiver gotypes.Type, methodExpression bool, typeArgs []gotypes.Type, defs ...symbolDefinition) {
	resolver := autoPropertyResolver{proj: ctx.proj, receiver: receiver}
	if !methodExpression {
		defs = ctx.appendAutoPropertyDefinitions(&resolver, defs)
	}
	autoProperty := !ctx.itemSet.callResult && !methodExpression &&
		(ctx.valueExpression || len(ctx.itemSet.expectedTypes) > 0)
	for _, def := range defs {
		if def.Implementation != nil && ctx.itemSet.expectsFunctionValue && typeArgs == nil {
			call := &ast.CallExpr{Fun: ctx.selectorExpr}
			source, _ := def.SourceObject.(*gotypes.Func)
			binding := cmp.Or(def.parameterBinding, source)
			if sig, params := xgoutil.ResolveCallableSignatureForCall(ctx.typeInfo, call, def.Implementation, binding); sig != nil {
				def.TypeHint = gotypes.NewSignatureType(nil, nil, nil, params, sig.Results(), sig.Variadic())
			}
		}
		var ok bool
		if methodExpression {
			def, ok = ctx.methodExpressionCompletionDefinition(&resolver, def, typeArgs)
		} else {
			def, ok = ctx.instanceCompletionDefinition(&resolver, def, autoProperty)
		}
		if !ok {
			continue
		}
		ctx.itemSet.addDefinitions(def)
	}
}

// appendAutoPropertyDefinitions includes aliases hidden by a field or absent
// from expanded overload definitions. Existing methods keep their display spelling.
func (ctx *completionContext) appendAutoPropertyDefinitions(resolver *autoPropertyResolver, defs []symbolDefinition) []symbolDefinition {
	seen := make(map[string]bool)
	for _, def := range defs {
		if def.Implementation == nil {
			continue
		}
		name := def.CompletionItemLabel
		if def.SourceObject != def.Implementation {
			name = xgoutil.MemberAliasName(def.SourceObject.Name())
		}
		property := resolver.resolve(name)
		if property.implementation != nil && types.ObjectOrigin(property.implementation) == types.ObjectOrigin(def.Implementation) {
			seen[xgoutil.MemberAliasName(name)] = true
		}
	}
	for name, property := range resolver.candidates() {
		if !property.exists || property.implementation == nil || !xgoutil.IsValidType(property.typ) || seen[name] {
			continue
		}
		if ctx.inFrameworkEventHandler && ctx.isFrameworkEventHandler(property.object) {
			continue
		}
		for _, def := range ctx.definitionsForSelection(property.implementation, resolver.receiver) {
			def.SourceObject = property.object
			def.Implementation = property.implementation
			def.CompletionItemLabel = name
			def.CompletionItemInsertText = name
			def.AutoPropertyType = property.typ
			defs = append(defs, def)
		}
	}
	return defs
}

// collectPackageMembers collects members of a package.
func (ctx *completionContext) collectPackageMembers(pkg *gotypes.Package, typeArgs []gotypes.Type) {
	if pkg == nil {
		return
	}

	var pkgDoc *pkgdoc.PkgDoc
	if xgoutil.IsMainPkg(pkg) {
		pkgDoc, _ = ctx.proj.PkgDoc()
	} else {
		pkgDoc, _ = ctx.lookupPkgDoc(xgoutil.PkgPath(pkg))
	}

	resolver := autoPropertyResolver{proj: ctx.proj}
	autoProperty := typeArgs == nil && !ctx.itemSet.callResult &&
		(ctx.valueExpression || len(ctx.itemSet.expectedTypes) > 0)
	defs := ctx.definitionsForPkg(pkg, pkgDoc)
	if autoProperty {
		defs = ctx.appendPackagePropertyDefinitions(&resolver, defs)
	}
	for _, def := range defs {
		var ok bool
		if typeArgs != nil {
			def, ok = ctx.indexedPackageDefinition(&resolver, def, typeArgs)
		} else {
			def, ok = ctx.functionCompletionDefinition(&resolver, def, autoProperty)
		}
		if ok {
			ctx.itemSet.addDefinitions(def)
		}
	}
}

// collectCall collects function call completions.
func (ctx *completionContext) collectCall() error {
	callExpr, ok := ctx.enclosingNode.(*ast.CallExpr)
	if !ok {
		return nil
	}
	if ctx.inFuncDecorator && callExpr == ctx.enclosingCallExpr {
		return ctx.collectFuncDecoratorCall(callExpr)
	}
	if ctx.inCallKwargName {
		ctx.collectCallKwargNames(callExpr, len(callExpr.Args), ctx.currentCallKwargArgIndex(callExpr))
		return nil
	}
	if argIndex, ok := ctx.currentCallKwargNameCandidateArgIndex(callExpr); ok {
		// A bare identifier can also be a positional argument.
		ctx.collectCallKwargNames(callExpr, argIndex, argIndex)
	}
	if expected := builtinArgTypes(ctx.typeInfo, callExpr, ctx.getCurrentArgIndex(callExpr.Args)); len(expected) > 0 {
		ctx.expectedTypes = expected
		return ctx.collectGeneral()
	}

	if tv := ctx.typeInfo.Types[xgoutil.CallExprFun(callExpr)]; tv.IsType() {
		ctx.expectedTypes = validExpectedType(tv.Type)
		return ctx.collectGeneral()
	}

	if ctx.setCallResultContext(callExpr) {
		return ctx.collectGeneral()
	}
	if resolvedArg, ok := ctx.getCurrentResolvedCallArg(callExpr); ok {
		if xgoutil.IsValidType(resolvedArg.ExpectedType) {
			ctx.expectedTypes = []gotypes.Type{resolvedArg.ExpectedType}
		} else {
			ctx.expectedTypes = ctx.overloadExpectedTypes(callExpr, resolvedArg.Arg)
		}
		return ctx.collectGeneral()
	}
	if overloads := callExprOverloads(ctx.typeInfo, callExpr); len(overloads) > 0 {
		expectedTypes := make([]gotypes.Type, 0, len(overloads))
		for _, overload := range overloads {
			bound, _, matches := overload.matchArgs(ctx.typeInfo, callExpr, nil)
			if !matches {
				continue
			}
			sig, params := bound.signature, bound.params
			args, _ := xgoutil.CallExprArgsForSignature(ctx.typeInfo, callExpr, sig, params)
			if expectedType := callExprArgType(sig, params, ctx.getCurrentArgIndex(args)); expectedType != nil {
				expectedTypes = append(expectedTypes, expectedType)
			}
		}
		ctx.expectedTypes = deduplicateTypes(expectedTypes)
		return ctx.collectGeneral()
	}

	fun, sig, params := xgoutil.ResolveCallExprSignature(ctx.typeInfo, callExpr)
	if sig != nil {
		params, _ = xgoutil.BindCallExprParams(ctx.typeInfo, callExpr, fun, sig, params, -1)
		args, _ := xgoutil.CallExprArgsForSignature(ctx.typeInfo, callExpr, sig, params)
		if expectedType := callExprArgType(sig, params, ctx.getCurrentArgIndex(args)); expectedType != nil {
			ctx.expectedTypes = []gotypes.Type{expectedType}
		}
	}
	return ctx.collectGeneral()
}

// setCallResultContext constrains the inner callable by the signatures that
// can consume its results as the sole argument of call.
func (ctx *completionContext) setCallResultContext(call *ast.CallExpr) bool {
	if !ctx.itemSet.callResult || len(call.Args) != 1 || len(call.Kwargs) != 0 || call.Ellipsis.IsValid() ||
		ctx.inFuncDecorator && call == ctx.enclosingCallExpr {
		return false
	}
	inner, ok := astutil.Unparen(call.Args[0]).(*ast.CallExpr)
	if !ok || ctx.pos < inner.Fun.Pos() || ctx.pos > inner.Fun.End() {
		return false
	}

	// Replacing the inner call can select a different outer overload. Prefer
	// the checked signature when there is only one possible callable.
	candidates := callExprOverloads(ctx.typeInfo, call)
	if len(candidates) < 2 {
		if fun, sig, params := xgoutil.ResolveCallExprSignature(ctx.typeInfo, call); sig != nil {
			candidates = []callOverload{{object: fun, signature: sig, params: params}}
		}
	}
	expands := false
	for _, candidate := range candidates {
		if !candidate.expandsCallResults(ctx.typeInfo, call) {
			continue
		}
		// An unconstrained outer call keeps the full selector member list.
		if ctx.kind == completionKindDot && candidate.acceptsAnyCallResults() {
			return false
		}
		expands = true
	}
	if !expands {
		return false
	}

	ctx.expectedTypes = nil
	ctx.itemSet.isCompatibleWithCallResults = func(results *gotypes.Tuple) bool {
		return slices.ContainsFunc(candidates, func(candidate callOverload) bool {
			return candidate.matchesCallResults(ctx.typeInfo, call, results)
		})
	}
	return true
}

// collectFuncDecoratorCall collects completions for a decorator's explicit
// arguments.
func (ctx *completionContext) collectFuncDecoratorCall(callExpr *ast.CallExpr) error {
	typ := ctx.typeInfo.TypeOf(callExpr.Fun)
	sig, ok := typ.(*gotypes.Signature)
	if !ok {
		return ctx.collectGeneral()
	}
	params, ok := funcDecoratorParams(sig)
	if !ok {
		return ctx.collectGeneral()
	}
	argIndex := ctx.getCurrentArgIndex(callExpr.Args)
	if argIndex >= 0 && argIndex < params.Len() {
		ctx.expectedTypes = []gotypes.Type{xgoutil.SourceParamType(params.At(argIndex))}
	}
	return ctx.collectGeneral()
}

// deduplicateTypes removes duplicate expected types while preserving order.
func deduplicateTypes(expectedTypes []gotypes.Type) []gotypes.Type {
	if len(expectedTypes) <= 1 {
		return expectedTypes
	}

	deduplicated := make([]gotypes.Type, 0, len(expectedTypes))
	for _, expectedType := range expectedTypes {
		if slices.ContainsFunc(deduplicated, func(existing gotypes.Type) bool {
			return gotypes.Identical(existing, expectedType)
		}) {
			continue
		}
		deduplicated = append(deduplicated, expectedType)
	}
	return deduplicated
}

// getCurrentArgIndex gets the current argument index in a function call.
func (ctx *completionContext) getCurrentArgIndex(args []ast.Expr) int {
	if len(args) == 0 {
		return 0
	}
	for i, arg := range args {
		if ctx.pos >= arg.Pos() && ctx.pos <= arg.End() {
			return i
		}
	}
	if ctx.pos > args[len(args)-1].End() {
		return len(args)
	}
	return -1
}

// getCurrentResolvedCallArg returns the resolved call argument that contains
// the current cursor position.
func (ctx *completionContext) getCurrentResolvedCallArg(callExpr *ast.CallExpr) (xgoutil.ResolvedCallExprArg, bool) {
	fun, sig, params := xgoutil.ResolveCallExprSignature(ctx.typeInfo, callExpr)
	resolved := callOverload{object: fun, signature: sig, params: params}
	if arg, ok := ctx.currentResolvedCallArg(resolved.args(ctx.typeInfo, callExpr)); ok {
		return xgoutil.ResolveCallExprArg(ctx.typeInfo, callExpr, arg.Arg)
	}
	return ctx.currentResolvedCallArg(formatResolvedCallExprArgs(ctx.typeInfo, callExpr, callExprOverloads(ctx.typeInfo, callExpr)))
}

// currentResolvedCallArg returns the call argument in args that contains the
// current cursor position.
func (ctx *completionContext) currentResolvedCallArg(args iter.Seq[xgoutil.ResolvedCallExprArg]) (xgoutil.ResolvedCallExprArg, bool) {
	for arg := range args {
		if ctx.pos >= arg.Arg.Pos() && ctx.pos <= arg.Arg.End() {
			return arg, true
		}
		if arg.Kind != xgoutil.ResolvedCallExprArgKeyword {
			continue
		}
		if ctx.pos > arg.Kwarg.Name.End() && ctx.pos <= arg.Kwarg.End() {
			return arg, true
		}
	}
	return xgoutil.ResolvedCallExprArg{}, false
}

// overloadExpectedTypes returns expected argument types from overloads that
// still match callExpr.
func (ctx *completionContext) overloadExpectedTypes(callExpr *ast.CallExpr, edited ast.Expr) []gotypes.Type {
	overloads := callExprOverloads(ctx.typeInfo, callExpr)
	if len(overloads) == 0 {
		return nil
	}

	expectedTypes := make([]gotypes.Type, 0, len(overloads))
	for _, overload := range overloads {
		_, expectedType, matches := overload.matchArgs(ctx.typeInfo, callExpr, edited)
		if matches && xgoutil.IsValidType(expectedType) {
			expectedTypes = append(expectedTypes, expectedType)
		}
	}
	return deduplicateTypes(expectedTypes)
}

// currentCallKwargNameCandidateArgIndex returns the positional argument index
// that should be treated as an incomplete kwarg name.
func (ctx *completionContext) currentCallKwargNameCandidateArgIndex(callExpr *ast.CallExpr) (int, bool) {
	for i, arg := range callExpr.Args {
		ident, ok := arg.(*ast.Ident)
		if !ok || ctx.pos < ident.Pos() || ctx.pos > ident.End() {
			continue
		}
		return i, true
	}
	return -1, false
}

// collectCallKwargNames collects completion items for available keyword
// argument names at the current call site.
func (ctx *completionContext) collectCallKwargNames(callExpr *ast.CallExpr, argCount, skipArgIndex int) {
	kwargs := resolveCallExprKwargsAtArgCount(ctx.typeInfo, callExpr, argCount, skipArgIndex)
	if len(kwargs) == 0 {
		return
	}

	usedTargets := make(map[gotypes.Object]struct{})
	for _, kwarg := range kwargs {
		binding := kwarg
		for i, kwargExpr := range callExpr.Kwargs {
			target := binding.KeywordTarget(kwargExpr)
			if obj := kwargTargetObject(target); obj != nil {
				usedTargets[obj] = struct{}{}
			}
			if i+1 < len(callExpr.Kwargs) {
				binding = binding.AfterKeywords(callExpr.Kwargs[i : i+1])
			}
		}
	}

	keywordIndex := len(callExpr.Kwargs)
	if skipArgIndex >= 0 {
		keywordIndex = min(keywordIndex, max(0, skipArgIndex-len(callExpr.Args)))
	}
	for _, kwarg := range kwargs {
		binding := kwarg.AfterKeywords(callExpr.Kwargs[:keywordIndex])
		for _, target := range xgoutil.ListResolvedCallExprKwargTargets(binding) {
			obj := kwargTargetObject(&target)
			if _, ok := usedTargets[obj]; ok {
				continue
			}
			for _, def := range ctx.definitionsForSelection(obj, kwarg.ContainerType()) {
				def.CompletionItemLabel = target.Name
				def.CompletionItemInsertText = target.Name + " = ${1:}"
				def.CompletionItemInsertTextFormat = SnippetTextFormat
				ctx.itemSet.addDefinitions(def)
			}
		}
	}
}

// currentCallKwargArgIndex returns the argument index for the kwarg name under
// the current cursor position.
func (ctx *completionContext) currentCallKwargArgIndex(callExpr *ast.CallExpr) int {
	for i, kwarg := range callExpr.Kwargs {
		if ctx.pos >= kwarg.Name.Pos() && ctx.pos <= kwarg.Name.End() {
			return len(callExpr.Args) + i
		}
	}
	return -1
}

// collectXGoUnitCompletions collects unit suffix completions for number literals.
func (ctx *completionContext) collectXGoUnitCompletions(expectedTypes []gotypes.Type) bool {
	completionRange, number, ok := ctx.currentXGoUnitCompletionRange()
	if !ok {
		return false
	}

	seen := make(map[string]struct{})
	hasUnit := false
	for _, expectedType := range expectedTypes {
		for _, spec := range xgoutil.UnitSpecsForType(expectedType) {
			if _, ok := seen[spec.Name]; ok {
				continue
			}
			seen[spec.Name] = struct{}{}
			if !xgoUnitSuffixFitsNumber(number, spec.Name) {
				continue
			}
			hasUnit = true
			ctx.itemSet.add(CompletionItem{
				Label:      spec.Name,
				Kind:       UnitCompletion,
				Detail:     ctx.typeString(spec.SourceType),
				FilterText: number + spec.Name,
				Documentation: completionDocumentation(markupContent(
					ctx.itemSet.documentationKind,
					"Multiplier: `"+spec.Factor+"`",
					"Multiplier: "+spec.Factor,
				)),
				InsertTextFormat: new(PlainTextTextFormat),
				TextEdit: &Or_CompletionItem_textEdit{Value: TextEdit{
					Range:   completionRange,
					NewText: spec.Name,
				}},
			})
		}
	}
	if hasUnit {
		ctx.isIncomplete = true
	}
	return hasUnit
}

// currentXGoUnitCompletionRange returns the suffix replacement range and the
// numeric part of the literal at the completion position.
func (ctx *completionContext) currentXGoUnitCompletionRange() (Range, string, bool) {
	path, _ := xgoutil.PathEnclosingInterval(ctx.astFile, ctx.pos-1, ctx.pos)
	for _, node := range path {
		switch lit := node.(type) {
		case *ast.NumberUnitLit:
			if !isXGoUnitNumberKind(lit.Kind) {
				return Range{}, "", false
			}
			unitStart := xgoUnitStart(lit)
			if ctx.pos >= unitStart && ctx.pos <= lit.End() {
				return RangeForPosEnd(ctx.proj, unitStart, lit.End()), lit.Value, true
			}
			return Range{}, "", false
		case *ast.BasicLit:
			if !isXGoUnitNumberKind(lit.Kind) || ctx.pos != lit.End() {
				continue
			}
			return RangeForPosEnd(ctx.proj, lit.End(), lit.End()), lit.Value, true
		}
	}
	return Range{}, "", false
}

// collectStructLit collects struct literal completions.
func (ctx *completionContext) collectStructLit() error {
	if ctx.expectedStructType == nil {
		return nil
	}

	seenFields := make(map[string]struct{})

	// Collect already used fields.
	if composite, ok := ctx.enclosingNode.(*ast.CompositeLit); ok {
		for _, elem := range composite.Elts {
			if kv, ok := elem.(*ast.KeyValueExpr); ok {
				if ident, ok := kv.Key.(*ast.Ident); ok {
					seenFields[ident.Name] = struct{}{}
				}
			}
		}
	}

	// Add unused fields.
	for field := range ctx.expectedStructType.Fields() {
		if !xgoutil.IsExportedOrInMainPkg(field) {
			continue
		}
		if _, ok := seenFields[field.Name()]; ok {
			continue
		}

		for _, def := range ctx.definitionsForSelection(field, ctx.compositeLitType) {
			def.CompletionItemInsertText = field.Name() + ": ${1:}"
			def.CompletionItemInsertTextFormat = SnippetTextFormat
			ctx.itemSet.addDefinitions(def)
		}
	}

	return nil
}

// addVisibleEnumMembers adds members of the given types that are not shadowed
// at the completion position.
func (ctx *completionContext) addVisibleEnumMembers(expectedTypes ...gotypes.Type) {
	for _, def := range ctx.definitionsForEnumTypes(expectedTypes...) {
		_, obj := ctx.innermostScope.LookupParent(def.CompletionItemLabel, ctx.pos)
		if len(ctx.enumInfo.membersForObject(obj)) > 0 {
			ctx.itemSet.addDefinitions(def)
		}
	}
}

// collectSelect collects select statement completions.
func (ctx *completionContext) collectSelect() error {
	ctx.itemSet.add(
		CompletionItem{
			Label:            "case",
			Kind:             KeywordCompletion,
			InsertText:       "case ${1:ch} <- ${2:value}:$0",
			InsertTextFormat: new(SnippetTextFormat),
		},
		CompletionItem{
			Label:            "default",
			Kind:             KeywordCompletion,
			InsertText:       "default:$0",
			InsertTextFormat: new(SnippetTextFormat),
		},
	)
	return nil
}

// completionItemKindPriority is the priority order for different completion
// item kinds.
var completionItemKindPriority = map[CompletionItemKind]int{
	VariableCompletion:   1,
	FieldCompletion:      2,
	PropertyCompletion:   3,
	MethodCompletion:     4,
	FunctionCompletion:   5,
	ConstantCompletion:   6,
	EnumMemberCompletion: 6,
	UnitCompletion:       7,
	ClassCompletion:      8,
	EnumCompletion:       8,
	InterfaceCompletion:  9,
	ModuleCompletion:     10,
	KeywordCompletion:    11,
}

// sortedItems returns the sorted items.
func (ctx *completionContext) sortedItems() []CompletionItem {
	slices.SortStableFunc(ctx.itemSet.items, func(a, b CompletionItem) int {
		if p1, p2 := completionItemKindPriority[a.Kind], completionItemKindPriority[b.Kind]; p1 != p2 {
			return p1 - p2
		}
		return cmp.Compare(a.Label, b.Label)
	})
	return ctx.itemSet.items
}

// adaptCompletionItemsForClient removes completion features unsupported by the
// client. It adapts items in place.
func adaptCompletionItemsForClient(capabilities CompletionClientCapabilities, items []CompletionItem) {
	for i := range items {
		item := &items[i]
		if item.InsertTextFormat != nil && *item.InsertTextFormat == SnippetTextFormat &&
			!capabilities.CompletionItem.SnippetSupport {
			item.InsertText = item.Label
			item.TextEdit = plainTextCompletionTextEdit(item.Label, item.TextEdit)
			item.InsertTextFormat = new(PlainTextTextFormat)
		}
		if !completionItemKindSupportedByClient(capabilities, item.Kind) {
			item.Kind = TextCompletion
		}
	}
}

// plainTextCompletionTextEdit returns a text edit with snippet text replaced by label.
func plainTextCompletionTextEdit(label string, textEdit *Or_CompletionItem_textEdit) *Or_CompletionItem_textEdit {
	if textEdit == nil {
		return nil
	}
	result := *textEdit
	switch edit := result.Value.(type) {
	case TextEdit:
		edit.NewText = label
		result.Value = edit
	case InsertReplaceEdit:
		edit.NewText = label
		result.Value = edit
	default:
		return textEdit
	}
	return &result
}

// completionItemKindSupportedByClient reports whether kind is safe to send.
func completionItemKindSupportedByClient(capabilities CompletionClientCapabilities, kind CompletionItemKind) bool {
	if kind == 0 {
		return true
	}
	if capabilities.CompletionItemKind != nil && capabilities.CompletionItemKind.ValueSet != nil {
		return true
	}
	return kind <= ReferenceCompletion
}

// completionItemSet is a set of completion items.
type completionItemSet struct {
	items                       []CompletionItem
	seenDefinitions             map[string]struct{}
	documentationKind           MarkupKind
	supportedKinds              map[CompletionItemKind]struct{}
	isCompatibleWithCallResults func(results *gotypes.Tuple) bool
	notRecommended              func(symbolDefinition) bool
	disallowVoidFuncs           bool
	expectedFuncResultCount     int
	callResult                  bool
	expectsFunctionValue        bool
	expectedTypes               []gotypes.Type
	typeInfo                    *types.Info
	enumContext                 enumIdentContext
}

// newCompletionItemSet creates a new [completionItemSet].
func newCompletionItemSet(documentationKind MarkupKind) *completionItemSet {
	return &completionItemSet{
		items:             []CompletionItem{},
		seenDefinitions:   make(map[string]struct{}),
		documentationKind: documentationKind,
	}
}

// setDisallowVoidFuncs toggles whether zero-result funcs are filtered out.
func (s *completionItemSet) setDisallowVoidFuncs(disallow bool) {
	s.disallowVoidFuncs = disallow
}

// setSupportedKinds sets the supported kinds for the completion items.
func (s *completionItemSet) setSupportedKinds(kinds ...CompletionItemKind) {
	if len(kinds) == 0 {
		return
	}

	s.supportedKinds = make(map[CompletionItemKind]struct{})
	for _, kind := range kinds {
		s.supportedKinds[kind] = struct{}{}
	}
}

// setExpectedFuncResultCount limits function-like items to signatures with the given result count.
func (s *completionItemSet) setExpectedFuncResultCount(count int) {
	if count <= 0 {
		return
	}
	s.expectedFuncResultCount = count
}

// setExpectedTypes sets the expected types for the completion items.
func (s *completionItemSet) setExpectedTypes(expectedTypes []gotypes.Type) {
	if len(expectedTypes) == 0 {
		return
	}
	s.expectedTypes = expectedTypes
	s.expectsFunctionValue = slices.ContainsFunc(expectedTypes, func(typ gotypes.Type) bool {
		if !xgoutil.IsValidType(typ) {
			return false
		}
		_, ok := typ.Underlying().(*gotypes.Signature)
		return ok
	})
}

// matchesExpectedValue checks the types and implicit conversions available at
// the completion site.
func (s *completionItemSet) matchesExpectedValue(value gotypes.TypeAndValue) bool {
	// A result list cannot satisfy a single value, even an empty interface.
	_, resultList := value.Type.(*gotypes.Tuple)
	for _, expected := range s.expectedTypes {
		_, expectedList := expected.(*gotypes.Tuple)
		if resultList != expectedList || !xgoutil.IsValidType(expected) {
			continue
		}
		// Assignability of an untyped constant's type does not check its value.
		if value.Value != nil && isUntypedType(value.Type) && gotypes.AssignableTo(value.Type, expected) &&
			!xgoutil.CallValueTypeMatches(s.typeInfo, value, expected) {
			continue
		}
		if completionValuesCompatible(s.typeInfo, value, expected) || xgoutil.IsTypesConvertible(value.Type, expected) {
			return true
		}
	}
	return false
}

// setEnumContext sets the context used to filter enum members.
func (s *completionItemSet) setEnumContext(context enumIdentContext) {
	s.enumContext = context
}

// isEnumMemberCompatibleWithContext reports whether value satisfies the enum
// completion context.
func isEnumMemberCompatibleWithContext(info *types.Info, value gotypes.TypeAndValue, context enumIdentContext) bool {
	if !xgoutil.IsValidType(value.Type) {
		return false
	}
	if len(context.basicTypeConstraints) > 0 {
		basic, ok := value.Type.Underlying().(*gotypes.Basic)
		if !ok {
			return false
		}
		for _, required := range context.basicTypeConstraints {
			if basic.Info()&required == 0 {
				return false
			}
		}
	}
	if len(context.expectedTypes) == 0 {
		return true
	}
	for _, expectedType := range context.expectedTypes {
		if !xgoutil.IsValidType(expectedType) {
			continue
		}
		if xgoutil.CallValueTypeMatches(info, value, gotypes.Unalias(expectedType)) {
			return true
		}
		if context.allowConversion && xgoutil.IsTypesConvertible(value.Type, expectedType) {
			return true
		}
	}
	return false
}

// add adds items to the set.
func (s *completionItemSet) add(items ...CompletionItem) {
	for _, item := range items {
		if s.supportedKinds != nil {
			if _, ok := s.supportedKinds[item.Kind]; !ok {
				continue
			}
		}
		s.items = append(s.items, item)
	}
}

// addDefinitions adds symbol definitions to the set.
func (s *completionItemSet) addDefinitions(defs ...symbolDefinition) {
	for _, def := range defs {
		if def.CompletionItemKind == ConstantCompletion && xgoutil.IsXGoInternalName(def.CompletionItemLabel) {
			continue
		}
		var sig *gotypes.Signature
		// Function variables keep their value type unless they are the callee
		// of an explicit call or the source of an implicit property call.
		if (def.Implementation != nil || def.AutoPropertyType != nil || s.callResult) && xgoutil.IsValidType(def.TypeHint) {
			sig, _ = def.TypeHint.Underlying().(*gotypes.Signature)
		}
		if s.isCompatibleWithCallResults != nil && (sig == nil || !s.isCompatibleWithCallResults(sig.Results())) {
			continue
		}
		functionValue := def.AutoPropertyType == nil && s.isFunctionValue(def.TypeHint)
		if !functionValue && sig != nil {
			resultCount := sig.Results().Len()
			switch typ := def.AutoPropertyType.(type) {
			case nil:
			case *gotypes.Tuple:
				resultCount = typ.Len()
			default:
				resultCount = 1
			}
			if s.expectedFuncResultCount > 0 && resultCount > 1 && resultCount != s.expectedFuncResultCount {
				continue
			}
			if s.disallowVoidFuncs && resultCount == 0 {
				continue
			}
		}
		value := gotypes.TypeAndValue{Type: def.TypeHint}
		if c, ok := def.SourceObject.(*gotypes.Const); ok {
			value.Value = c.Val()
		}
		if def.CompletionItemKind == EnumMemberCompletion && s.enumContext.status != enumContextUnknown {
			if s.enumContext.status == enumContextDisallowed || !isEnumMemberCompatibleWithContext(s.typeInfo, value, s.enumContext) {
				continue
			}
		} else if !functionValue && len(s.expectedTypes) > 0 {
			if def.AutoPropertyType != nil {
				value.Type = def.AutoPropertyType
			} else if sig != nil {
				switch sig.Results().Len() {
				case 0:
					continue
				case 1:
					value.Type = sig.Results().At(0).Type()
				default:
					value.Type = sig.Results()
				}
			}
			if !s.matchesExpectedValue(value) {
				continue
			}
		}

		if functionValue && def.Implementation != nil {
			name := def.functionValueName()
			def.useIndexedImplementationName(name)
			def.CompletionItemLabel = name
			def.CompletionItemInsertText = name
		}
		// A declaration can supply both a positional value and a keyword name.
		definitionKey := def.ID.String() + "\x00" + def.CompletionItemLabel + "\x00" + def.CompletionItemInsertText
		if _, ok := s.seenDefinitions[definitionKey]; ok {
			continue
		}
		if s.notRecommended != nil && s.notRecommended(def) {
			continue
		}
		s.seenDefinitions[definitionKey] = struct{}{}

		s.add(def.completionItem(s.documentationKind))
	}
}

// isFunctionValue reports whether typ can supply the expected callback without
// being called. This decision also determines a function's source spelling.
func (s *completionItemSet) isFunctionValue(typ gotypes.Type) bool {
	if s.callResult || !s.expectsFunctionValue || !xgoutil.IsValidType(typ) {
		return false
	}
	_, ok := typ.Underlying().(*gotypes.Signature)
	return ok && s.matchesExpectedValue(gotypes.TypeAndValue{Type: typ})
}

// definitionName returns the actual insertion name before visibility filtering.
func (s *completionItemSet) definitionName(def symbolDefinition) string {
	if def.Implementation != nil && def.AutoPropertyType == nil && s.isFunctionValue(def.TypeHint) {
		return def.functionValueName()
	}
	return def.CompletionItemLabel
}

// completionValuesCompatible distinguishes function values from result lists.
func completionValuesCompatible(info *types.Info, value gotypes.TypeAndValue, want gotypes.Type) bool {
	if !xgoutil.IsValidType(value.Type) || !xgoutil.IsValidType(want) {
		return false
	}
	if results, ok := want.(*gotypes.Tuple); ok {
		actual, ok := value.Type.(*gotypes.Tuple)
		if !ok || actual.Len() != results.Len() {
			return false
		}
		for index := range results.Len() {
			if !completionValuesCompatible(info, gotypes.TypeAndValue{Type: actual.At(index).Type()}, results.At(index).Type()) {
				return false
			}
		}
		return true
	}
	gotUnderlying := value.Type.Underlying()
	wantUnderlying := want.Underlying()
	_, gotFunction := gotUnderlying.(*gotypes.Signature)
	_, wantFunction := wantUnderlying.(*gotypes.Signature)
	if gotFunction || wantFunction {
		return xgoutil.CallValueTypeMatches(info, value, want)
	}
	return xgoutil.IsTypesCompatible(value.Type, want) || xgoutil.CallValueTypeMatches(info, value, want)
}
