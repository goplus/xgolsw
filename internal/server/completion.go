package server

import (
	"cmp"
	"fmt"
	gotypes "go/types"
	"iter"
	"path"
	"slices"
	"unicode"

	"github.com/goplus/mod/modfile"
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
	typeInfo, _ := proj.TypeInfo()
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
	ctx := &completionContext{
		definitionContext: definitionContext{
			proj:         proj,
			typeDisplay:  newTypeDisplay(proj, astFile, pos),
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
	ctx.frameworkResult, err = analyzeFramework(proj)
	if err != nil {
		return nil, err
	}
	if ctx.frameworkResult != nil {
		ctx.framework = ctx.frameworkResult.adapter
		ctx.frameworkResolved = true
	}
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

	typeInfo        *types.Info
	frameworkResult *frameworkAnalysis
	filename        string
	astFile         *ast.File
	astFileScope    *gotypes.Scope
	tokenFile       *token.File
	pos             token.Pos // Position used for AST lookup.
	sourcePos       token.Pos // Physical cursor position used for edits.
	innermostScope  *gotypes.Scope

	kind completionKind

	enclosingNode      ast.Node
	enclosingCallExpr  *ast.CallExpr
	selectorExpr       *ast.SelectorExpr
	expectedTypes      []gotypes.Type
	enumContext        enumIdentContext
	expectedStructType *gotypes.Struct
	compositeLitType   gotypes.Type
	assignTargets      []gotypes.Object
	declValueSpec      *ast.ValueSpec
	returnIndex        int

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
	if ctx.isInTypeArgument(path) {
		if ctx.kind != completionKindDot {
			ctx.kind = completionKindGeneral
		}
		ctx.typeExpression = true
		ctx.expectedTypes = nil
		ctx.expectedFuncResultCount = 0
		ctx.itemSet.callResult = false
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
			ctx.enumContext = enumContextAtIdent(ctx.proj, ctx.typeInfo, ident)
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

// isInTypeArgument distinguishes generic type syntax from runtime indices and
// array lengths, including values nested inside a type argument.
func (ctx *completionContext) isInTypeArgument(path []ast.Node) bool {
	for _, node := range path {
		var base ast.Expr
		var lbrack, rbrack token.Pos
		switch node := node.(type) {
		case *ast.ArrayType:
			if node.Len != nil && node.Len.Pos() <= ctx.pos && ctx.pos <= node.Len.End() {
				return false
			}
			continue
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
		var ident *ast.Ident
		switch base := astutil.Unparen(base).(type) {
		case *ast.Ident:
			ident = base
		case *ast.SelectorExpr:
			ident = base.Sel
		}
		// Use the declaration because the compiler may record an instantiated
		// type on the base expression. Container variables remain value indices.
		obj := ctx.typeInfo.ObjectOf(ident)
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
	found := false
	for expr, typ := range contextualValueTypes(ctx.typeInfo, path) {
		if expr.Pos() <= ctx.pos && ctx.pos <= expr.End() {
			found = true
			if xgoutil.IsValidType(typ) {
				expectedTypes = append(expectedTypes, typ)
			}
		}
	}
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
		if _, ok := astutil.Unparen(callExpr.Fun).(*ast.CallExpr); ok {
			ctx.analyzeValueOperand(callExpr.Fun, []ast.Node{callExpr})
		}
		if ident := xgoutil.CallExprFunIdent(callExpr); ident != nil && ident.Pos() <= ctx.pos && ctx.pos <= ident.End() {
			ctx.itemSet.callResult = true
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

	line := ctx.tokenFile.Line(ctx.pos)
	lineStartPos := ctx.tokenFile.LineStart(line)
	relLineStartPos := lineStartPos - fileBase
	if relLineStartPos < 0 || int(relLineStartPos) >= len(ctx.astFile.Code) {
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

	if ctx.frameworkResult != nil && ctx.frameworkResult.collectCompletions != nil {
		ctx.frameworkResult.collectCompletions(ctx)
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
	memberNames := make(map[string]bool)
	addDefinitions := func(defs ...symbolDefinition) {
		for _, def := range defs {
			if !seenNames[def.CompletionItemLabel] {
				ctx.itemSet.addDefinitions(def)
			}
		}
		// Reserve names even when incompatible, keeping overloads in one batch.
		for _, def := range defs {
			seenNames[def.CompletionItemLabel] = true
		}
	}
	var classType *gotypes.Named
	if ctx.innermostScope == ctx.astFileScope {
		classType = classTypeForFile(ctx.proj, ctx.astFile)
	}
	var parents map[ast.Node]ast.Node
	for scope := ctx.innermostScope; scope != nil && scope != gotypes.Universe; scope = scope.Parent() {
		// Locals take precedence over class members, which take precedence
		// over package declarations and imports.
		if scope == pkg.Scope() && xgoutil.IsNamedStructType(classType) &&
			!(ctx.kind == completionKindDecl && ctx.declValueSpec.Values == nil) {
			var defs []symbolDefinition
			for member := range xgoutil.StructMembers(classType, ctx.isClassBaseType) {
				if ctx.inFrameworkEventHandler && ctx.isFrameworkEventHandler(member.Member) {
					continue
				}
				defs = append(defs, ctx.definitionsForMember(member)...)
			}
			for _, def := range defs {
				if !seenNames[def.CompletionItemLabel] {
					memberNames[def.CompletionItemLabel] = true
				}
			}
			addDefinitions(defs...)
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
			// A visible inner declaration hides outer names even when the
			// inner object is incompatible with the expected completion type.
			if seenNames[name] {
				// Class members do not shadow types in type annotations. Keep
				// those types available alongside value candidates.
				if _, ok := obj.(*gotypes.TypeName); ok && memberNames[name] {
					ctx.itemSet.addDefinitions(ctx.definitionsFor(obj, "")...)
				}
				continue
			}
			if !xgoutil.IsExportedOrInMainPkg(obj) {
				continue
			}
			if ctx.valueExpression || !slices.Contains(ctx.assignTargets, obj) {
				addDefinitions(ctx.definitionsFor(obj, "")...)
			}
			seenNames[name] = true

			if name == "this" {
				classType, _ = xgoutil.DerefType(obj.Type()).(*gotypes.Named)
			}
		}
	}

	// Add imported package definitions.
	var importedMembers []*gotypes.Package
	for _, importSpec := range ctx.astFile.Imports {
		var obj gotypes.Object
		if importSpec.Name != nil {
			obj = ctx.typeInfo.Defs[importSpec.Name]
		} else {
			obj = ctx.typeInfo.Implicits[importSpec]
		}
		pkgName, ok := obj.(*gotypes.PkgName)
		if !ok {
			continue
		}
		switch pkgName.Name() {
		case ".":
			importedMembers = append(importedMembers, pkgName.Imported())
		case "_":
		default:
			addDefinitions(ctx.definitionsFor(pkgName, "")...)
		}
	}

	// Add other definitions.
	if ctx.astFile.IsClass {
		if class, ok := ctx.proj.Module().LookupClass(modfile.ClassExt(ctx.filename)); ok {
			for _, pkgPath := range class.PkgPaths {
				pkg, err := ctx.proj.Import(pkgPath)
				if err != nil {
					continue
				}
				importedMembers = append(importedMembers, pkg)
			}
		}
	}
	for _, pkg := range importedMembers {
		pkgDoc, _ := ctx.lookupPkgDoc(pkg.Path())
		addDefinitions(ctx.definitionsForPkg(pkg, pkgDoc)...)
	}
	addDefinitions(ctx.builtinDefinitions(ctx.proj, ctx.lookupPkgDoc)...)
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
		pkgDoc, err := ctx.lookupPkgDoc(pkgPath)
		if err != nil {
			continue
		}
		ctx.itemSet.addDefinitions(symbolDefinition{
			ID: XGoDefinitionIdentifier{
				Package: &pkgPath,
			},
			Overview: "package " + path.Base(pkgPath),
			Detail:   pkgDoc.Doc,

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
		for _, typ := range expectedExprTypes(ctx.typeInfo, path) {
			if signatureType(typ) != nil {
				ctx.itemSet.setExpectedTypes([]gotypes.Type{typ})
				break
			}
		}
	}

	if ident, ok := ctx.selectorExpr.X.(*ast.Ident); ok {
		if obj := ctx.typeInfo.ObjectOf(ident); obj != nil {
			if pkgName, ok := obj.(*gotypes.PkgName); ok {
				ctx.collectPackageMembers(pkgName.Imported())
				return nil
			}
		}
	}

	typ := ctx.typeInfo.TypeOf(ctx.selectorExpr.X)
	if ident, ok := ctx.selectorExpr.X.(*ast.Ident); ok {
		if propertyLikeType := ctx.resolvePropertyLikeExprType(ident, typ); xgoutil.IsValidType(propertyLikeType) {
			typ = propertyLikeType
		}
	}
	if !xgoutil.IsValidType(typ) {
		return nil
	}
	typ = gotypes.Unalias(xgoutil.DerefType(gotypes.Unalias(typ)))

	if iface, ok := typ.Underlying().(*gotypes.Interface); ok {
		ctx.collectInterfaceMethodCompletions(iface, typ)
	} else if _, ok := typ.Underlying().(*gotypes.Struct); ok {
		ctx.addMemberCompletions(ctx.definitionsForStruct(typ)...)
	}
	return nil
}

// addMemberCompletions uses the source selector's signature when completing a
// callback, including the explicit receiver of a method expression.
func (ctx *completionContext) addMemberCompletions(defs ...symbolDefinition) {
	for _, def := range defs {
		if def.Function != nil && ctx.itemSet.expectsFunctionValue {
			call := &ast.CallExpr{Fun: ctx.selectorExpr}
			if sig, params := xgoutil.ResolveFuncSignatureForCall(ctx.typeInfo, call, def.Function); sig != nil {
				def.TypeHint = gotypes.NewSignatureType(nil, nil, nil, params, sig.Results(), sig.Variadic())
			}
		}
		ctx.itemSet.addDefinitions(def)
	}
}

// resolvePropertyLikeExprType returns the result type of a property-like
// function reference. If type-checker information is unavailable, it falls back
// to [completionContext.resolvePropertyLikeFuncResultType].
func (ctx *completionContext) resolvePropertyLikeExprType(ident *ast.Ident, typ gotypes.Type) gotypes.Type {
	if ident == nil || ident.Name == "" {
		return nil
	}

	if sig, ok := typ.(*gotypes.Signature); ok && sig.Params().Len() == 0 && sig.Results().Len() == 1 {
		if obj := ctx.typeInfo.ObjectOf(ident); obj != nil {
			if fun, ok := obj.(*gotypes.Func); ok {
				if fun.Name() != ident.Name && xgoutil.ToLowerCamelCase(fun.Name()) == ident.Name {
					return sig.Results().At(0).Type()
				}
			}
		}
	}

	if xgoutil.IsValidType(typ) {
		return nil
	}

	return ctx.resolvePropertyLikeFuncResultType(ident)
}

// resolvePropertyLikeFuncResultType resolves the result type of a property-like
// function from the enclosing scopes.
func (ctx *completionContext) resolvePropertyLikeFuncResultType(ident *ast.Ident) gotypes.Type {
	if ident == nil || ident.Name == "" {
		return nil
	}

	for scope := ctx.innermostScope; scope != nil && scope != gotypes.Universe; scope = scope.Parent() {
		isInnermost := scope == ctx.innermostScope
		isPkgScope := ctx.typeInfo.Pkg != nil && scope == ctx.typeInfo.Pkg.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			fun, ok := obj.(*gotypes.Func)
			if !ok || fun.Name() == ident.Name || xgoutil.ToLowerCamelCase(fun.Name()) != ident.Name {
				continue
			}
			if isInnermost && !isPkgScope && fun.Pos().IsValid() && fun.Pos() >= ident.Pos() {
				continue
			}

			sig := fun.Signature()
			if sig.Params().Len() != 0 || sig.Results().Len() != 1 {
				continue
			}
			return sig.Results().At(0).Type()
		}
	}
	return nil
}

// collectInterfaceMethodCompletions collects accessible methods, including
// embedded methods, using each method's declaration for its description.
func (ctx *completionContext) collectInterfaceMethodCompletions(iface *gotypes.Interface, receiver gotypes.Type) {
	for method := range iface.Methods() {
		if xgoutil.IsExportedOrInMainPkg(method) {
			ctx.addMemberCompletions(ctx.definitionsForSelection(method, receiver)...)
		}
	}
}

// collectPackageMembers collects members of a package.
func (ctx *completionContext) collectPackageMembers(pkg *gotypes.Package) {
	if pkg == nil {
		return
	}

	var pkgDoc *pkgdoc.PkgDoc
	if xgoutil.IsMainPkg(pkg) {
		pkgDoc, _ = ctx.proj.PkgDoc()
	} else {
		pkgDoc, _ = ctx.lookupPkgDoc(xgoutil.PkgPath(pkg))
	}

	ctx.itemSet.addDefinitions(ctx.definitionsForPkg(pkg, pkgDoc)...)
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
		if ctx.collectCallKwargNames(callExpr, argIndex, argIndex) {
			return nil
		}
	}
	if expected := builtinArgTypes(ctx.typeInfo, callExpr, ctx.getCurrentArgIndex(callExpr.Args)); len(expected) > 0 {
		ctx.expectedTypes = expected
		return ctx.collectGeneral()
	}

	if tv := ctx.typeInfo.Types[callExpr.Fun]; tv.IsType() {
		ctx.expectedTypes = validExpectedType(tv.Type)
		return ctx.collectGeneral()
	}

	if ctx.itemSet.callResult && len(callExpr.Args) == 1 && len(callExpr.Kwargs) == 0 && !callExpr.Ellipsis.IsValid() {
		_, sig, params := xgoutil.ResolveCallExprSignature(ctx.typeInfo, callExpr)
		_, resultCall := astutil.Unparen(callExpr.Args[0]).(*ast.CallExpr)
		if resultCall && sig != nil && (sig.Variadic() || params.Len() > 1) {
			ctx.expectedTypes = nil
			ctx.itemSet.isCompatibleWithCallResults = func(results *gotypes.Tuple) bool {
				count := results.Len()
				if count == 0 || (!sig.Variadic() && count != params.Len()) || (sig.Variadic() && count < params.Len()-1) {
					return false
				}
				for index := range count {
					if !completionTypesCompatible(results.At(index).Type(), callExprArgType(sig, params, index)) {
						return false
					}
				}
				return true
			}
			return ctx.collectGeneral()
		}
	}
	if resolvedArg, ok := ctx.getCurrentResolvedCallArg(callExpr); ok {
		if xgoutil.IsValidType(resolvedArg.ExpectedType) {
			ctx.expectedTypes = []gotypes.Type{resolvedArg.ExpectedType}
		} else {
			ctx.expectedTypes = ctx.overloadExpectedTypes(callExpr, resolvedArg)
		}
		return ctx.collectGeneral()
	}
	if funcOverloads := callExprFuncOverloads(ctx.typeInfo, callExpr); len(funcOverloads) > 0 {
		expectedTypes := make([]gotypes.Type, 0, len(funcOverloads))
		for _, funcOverload := range funcOverloads {
			sig, params := xgoutil.ResolveFuncSignatureForCall(ctx.typeInfo, callExpr, funcOverload)
			if sig == nil {
				continue
			}
			args, _ := xgoutil.CallExprArgs(ctx.typeInfo, callExpr, params)
			if expectedType := callExprArgType(sig, params, ctx.getCurrentArgIndex(args)); expectedType != nil {
				expectedTypes = append(expectedTypes, expectedType)
			}
		}
		ctx.expectedTypes = deduplicateTypes(expectedTypes)
		return ctx.collectGeneral()
	}

	_, sig, params := xgoutil.ResolveCallExprSignature(ctx.typeInfo, callExpr)
	if sig != nil {
		args, _ := xgoutil.CallExprArgs(ctx.typeInfo, callExpr, params)
		if expectedType := callExprArgType(sig, params, ctx.getCurrentArgIndex(args)); expectedType != nil {
			ctx.expectedTypes = []gotypes.Type{expectedType}
		}
	}
	return ctx.collectGeneral()
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
	if arg, ok := ctx.currentResolvedCallArg(xgoutil.ResolvedCallExprArgs(ctx.typeInfo, callExpr)); ok {
		return arg, true
	}
	return ctx.currentResolvedCallArg(formatResolvedCallExprArgs(ctx.typeInfo, callExpr, callExprFuncOverloads(ctx.typeInfo, callExpr)))
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
func (ctx *completionContext) overloadExpectedTypes(callExpr *ast.CallExpr, resolvedArg xgoutil.ResolvedCallExprArg) []gotypes.Type {
	overloads := callExprFuncOverloads(ctx.typeInfo, callExpr)
	if len(overloads) == 0 {
		return nil
	}

	expectedTypes := make([]gotypes.Type, 0, len(overloads))
	for _, overload := range overloads {
		expectedType, matches := matchOverloadCallExprArg(ctx.typeInfo, callExpr, overload, resolvedArg.Arg, resolvedArg.ArgIndex)
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
func (ctx *completionContext) collectCallKwargNames(callExpr *ast.CallExpr, argCount, skipArgIndex int) bool {
	kwargs := resolveCallExprKwargsAtArgCount(ctx.typeInfo, callExpr, argCount, skipArgIndex)
	if len(kwargs) == 0 {
		return false
	}

	usedTargets := make(map[gotypes.Object]struct{})
	for _, kwarg := range kwargs {
		for _, kwargExpr := range callExpr.Kwargs {
			target := xgoutil.LookupResolvedCallExprKwargTarget(kwarg, kwargExpr.Name.Name)
			if obj := kwargTargetObject(target); obj != nil {
				usedTargets[obj] = struct{}{}
			}
		}
	}

	collected := false
	for _, kwarg := range kwargs {
		for _, target := range xgoutil.ListResolvedCallExprKwargTargets(kwarg) {
			obj := kwargTargetObject(&target)
			if _, ok := usedTargets[obj]; ok {
				continue
			}
			for _, def := range ctx.definitionsForSelection(obj, kwarg.Param.Type()) {
				def.CompletionItemLabel = target.Name
				def.CompletionItemInsertText = target.Name + " = ${1:}"
				def.CompletionItemInsertTextFormat = SnippetTextFormat
				ctx.itemSet.addDefinitions(def)
				collected = true
			}
		}
	}
	return collected
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
	completionRange, filterPrefix, ok := ctx.currentXGoUnitCompletionRange()
	if !ok {
		return false
	}

	seen := make(map[string]struct{})
	hasUnit := false
	for _, expectedType := range expectedTypes {
		for _, spec := range xgoUnitSpecsForType(expectedType) {
			if _, ok := seen[spec.Name]; ok {
				continue
			}
			seen[spec.Name] = struct{}{}
			hasUnit = true
			ctx.itemSet.add(CompletionItem{
				Label:      spec.Name,
				Kind:       UnitCompletion,
				Detail:     ctx.typeString(spec.SourceType),
				FilterText: filterPrefix + spec.Name,
				Documentation: completionDocumentation(markupContent(
					ctx.itemSet.documentationKind,
					"Multiplier: `"+spec.Factor+"`",
					"Multiplier: "+spec.Factor,
				)),
				InsertTextFormat: ToPtr(PlainTextTextFormat),
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

// currentXGoUnitCompletionRange returns the unit suffix replacement range at
// the completion position.
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
			InsertTextFormat: ToPtr(SnippetTextFormat),
		},
		CompletionItem{
			Label:            "default",
			Kind:             KeywordCompletion,
			InsertText:       "default:$0",
			InsertTextFormat: ToPtr(SnippetTextFormat),
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
			item.InsertTextFormat = ToPtr(PlainTextTextFormat)
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
	items                         []CompletionItem
	seenDefinitions               map[string]struct{}
	documentationKind             MarkupKind
	supportedKinds                map[CompletionItemKind]struct{}
	isCompatibleWithExpectedTypes func(typ gotypes.Type) bool
	isCompatibleWithCallResults   func(results *gotypes.Tuple) bool
	disallowVoidFuncs             bool
	expectedFuncResultCount       int
	callResult                    bool
	expectsFunctionValue          bool
	enumContext                   enumIdentContext
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
	s.expectsFunctionValue = slices.ContainsFunc(expectedTypes, func(typ gotypes.Type) bool {
		if !xgoutil.IsValidType(typ) {
			return false
		}
		_, ok := typ.Underlying().(*gotypes.Signature)
		return ok
	})

	s.isCompatibleWithExpectedTypes = func(typ gotypes.Type) bool {
		for _, expectedType := range expectedTypes {
			if xgoutil.IsValidType(expectedType) {
				// First check direct compatibility.
				if completionTypesCompatible(typ, expectedType) {
					return true
				}
				// Then check if convertible (allows showing more options).
				if xgoutil.IsTypesConvertible(typ, expectedType) {
					return true
				}
			}
		}
		return false
	}
}

// setEnumContext sets the context used to filter enum members.
func (s *completionItemSet) setEnumContext(context enumIdentContext) {
	s.enumContext = context
}

// isEnumMemberCompatibleWithContext reports whether typ satisfies the enum
// completion context.
func isEnumMemberCompatibleWithContext(typ gotypes.Type, context enumIdentContext) bool {
	if !xgoutil.IsValidType(typ) {
		return false
	}
	if len(context.basicTypeConstraints) > 0 {
		basic, ok := typ.Underlying().(*gotypes.Basic)
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
		if gotypes.AssignableTo(typ, expectedType) {
			return true
		}
		if context.allowConversion && xgoutil.IsTypesConvertible(typ, expectedType) {
			return true
		}
		if typeParam, ok := gotypes.Unalias(expectedType).(*gotypes.TypeParam); ok {
			constraint := typeParam.Constraint().Underlying().(*gotypes.Interface)
			if gotypes.Satisfies(typ, constraint) {
				return true
			}
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
		var sig *gotypes.Signature
		if xgoutil.IsValidType(def.TypeHint) {
			sig, _ = def.TypeHint.Underlying().(*gotypes.Signature)
		}
		if s.isCompatibleWithCallResults != nil && (sig == nil || !s.isCompatibleWithCallResults(sig.Results())) {
			continue
		}
		functionValue := sig != nil && !s.callResult && s.expectsFunctionValue &&
			s.isCompatibleWithExpectedTypes(def.TypeHint)
		if !functionValue && sig != nil {
			if s.expectedFuncResultCount > 0 && sig.Results().Len() > 1 && sig.Results().Len() != s.expectedFuncResultCount {
				continue
			}
			if s.disallowVoidFuncs && sig.Results().Len() == 0 {
				continue
			}
		}
		if def.CompletionItemKind == EnumMemberCompletion && s.enumContext.status != enumContextUnknown {
			if s.enumContext.status == enumContextDisallowed || !isEnumMemberCompatibleWithContext(def.TypeHint, s.enumContext) {
				continue
			}
		} else if !functionValue && s.isCompatibleWithExpectedTypes != nil {
			typeToCompare := def.TypeHint
			if sig != nil {
				switch sig.Results().Len() {
				case 0:
					continue
				case 1:
					typeToCompare = sig.Results().At(0).Type()
				default:
					typeToCompare = sig.Results()
				}
			}
			if !s.isCompatibleWithExpectedTypes(typeToCompare) {
				continue
			}
		}

		definitionKey := def.ID.String()
		if _, ok := s.seenDefinitions[definitionKey]; ok {
			continue
		}
		s.seenDefinitions[definitionKey] = struct{}{}

		if functionValue && def.Function != nil {
			def.CompletionItemLabel = functionValueName(def.Function)
			def.CompletionItemInsertText = def.CompletionItemLabel
		}
		s.add(def.completionItem(s.documentationKind))
	}
}

// completionTypesCompatible distinguishes function values from result lists.
func completionTypesCompatible(got, want gotypes.Type) bool {
	if !xgoutil.IsValidType(got) || !xgoutil.IsValidType(want) {
		return false
	}
	if results, ok := want.(*gotypes.Tuple); ok {
		actual, ok := got.(*gotypes.Tuple)
		if !ok || actual.Len() != results.Len() {
			return false
		}
		for index := range results.Len() {
			if !completionTypesCompatible(actual.At(index).Type(), results.At(index).Type()) {
				return false
			}
		}
		return true
	}
	_, gotFunction := got.Underlying().(*gotypes.Signature)
	_, wantFunction := want.Underlying().(*gotypes.Signature)
	if gotFunction || wantFunction {
		return gotypes.AssignableTo(got, want)
	}
	return xgoutil.IsTypesCompatible(got, want)
}
