package server

import (
	"cmp"
	"fmt"
	gotypes "go/types"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/scanner"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_completion
func (s *Server) textDocumentCompletion(params *CompletionParams) ([]CompletionItem, error) {
	result, spxFile, astFile, err := s.compileAndGetASTFileForDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if astFile == nil {
		return nil, nil
	}
	if !astFile.Pos().IsValid() {
		return nil, nil
	}

	pos := PosAt(result.proj, astFile, params.Position)
	if !pos.IsValid() {
		return nil, nil
	}
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}

	astPkg, _ := result.proj.ASTPackage()
	innermostScope := xgoutil.InnermostScopeAt(result.proj.Fset, typeInfo, astPkg, pos)
	if innermostScope == nil {
		return nil, nil
	}
	ctx := &completionContext{
		itemSet:        newCompletionItemSet(),
		proj:           result.proj,
		typeInfo:       typeInfo,
		result:         result,
		spxFile:        spxFile,
		astFile:        astFile,
		astFileScope:   typeInfo.Scopes[astFile],
		tokenFile:      xgoutil.NodeTokenFile(result.proj.Fset, astFile),
		pos:            pos,
		innermostScope: innermostScope,
	}
	ctx.analyze()
	if err := ctx.collect(); err != nil {
		return nil, fmt.Errorf("failed to collect completion items: %w", err)
	}
	return ctx.sortedItems(), nil
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
	completionKindSwitchCase
	completionKindSelect
)

// completionContext represents the context for completion operations.
type completionContext struct {
	itemSet *completionItemSet

	proj           *xgo.Project
	typeInfo       *types.Info
	result         *compileResult
	spxFile        string
	astFile        *ast.File
	astFileScope   *gotypes.Scope
	tokenFile      *token.File
	pos            token.Pos
	innermostScope *gotypes.Scope

	kind completionKind

	enclosingNode      ast.Node
	enclosingCallExpr  *ast.CallExpr
	selectorExpr       *ast.SelectorExpr
	expectedTypes      []gotypes.Type
	expectedStructType *gotypes.Struct
	compositeLitType   *gotypes.Named
	assignTargets      []*ast.Ident
	declValueSpec      *ast.ValueSpec
	switchTag          ast.Expr
	returnIndex        int

	inStringLit             bool
	inSpxEventHandler       bool
	valueExpression         bool
	expectedFuncResultCount int
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
			if node.Sel == nil || node.Sel.End() >= ctx.pos {
				ctx.kind = completionKindDot
				ctx.selectorExpr = node
			}
		case *ast.CallExpr:
			if ctx.enclosingCallExpr == nil {
				ctx.enclosingCallExpr = node
			}
			if typ := ctx.typeInfo.TypeOf(node.Fun); !xgoutil.IsValidType(typ) {
				continue
			}

			// In XGo, map literals can be passed directly to funcs without
			// explicit type declaration, e.g., `println {"foo": value}`.
			// When the cursor is inside such a map literal, we should provide
			// general completions (including variables) rather than restricting
			// to the expected parameter type.
			shouldSetCallContext := ctx.kind == completionKindUnknown

			// Check if cursor is inside a composite literal or slice literal argument
			// where we want general completions.
			if shouldSetCallContext {
				for _, arg := range node.Args {
					// Check for SliceLit (XGo-style slice literals)
					if sl, ok := arg.(*ast.SliceLit); ok {
						if sl.Pos() <= ctx.pos && ctx.pos <= sl.End() {
							shouldSetCallContext = false
							break
						}
					}

					comp, ok := arg.(*ast.CompositeLit)
					if !ok {
						continue
					}
					if comp.Pos() <= ctx.pos && ctx.pos <= comp.End() {
						// Don't set call context for map literals.
						if ctx.isMapLiteral(comp) {
							shouldSetCallContext = false
							break
						}

						// Don't set call context for slice or array literals.
						if ctx.isSliceOrArrayLiteral(comp) {
							shouldSetCallContext = false
							break
						}

						// Also don't set call context if we're in a struct literal
						// field value position.
						for _, elt := range comp.Elts {
							if kv, ok := elt.(*ast.KeyValueExpr); ok {
								if kv.Colon < ctx.pos {
									shouldSetCallContext = false
									break
								}
							}
						}
						if !shouldSetCallContext {
							break
						}
					}
				}
			}

			if shouldSetCallContext {
				ctx.kind = completionKindCall
				ctx.enclosingNode = node
				ctx.valueExpression = true
			}
		case *ast.FuncLit:
			// Skip FuncLit, as we want general completion inside function literals
			// to allow access to all variables and identifiers.
			continue
		case *ast.SliceLit:
			// Skip SliceLit, as XGo-style slice literals should allow general completion
			// to access all variables and identifiers.
			continue
		case *ast.CompositeLit:
			typ := ctx.typeInfo.TypeOf(node)
			if !xgoutil.IsValidType(typ) {
				// Try to get type from the CompositeLit.Type field.
				if node.Type != nil {
					typ = ctx.typeInfo.TypeOf(node.Type)
				}
				if !xgoutil.IsValidType(typ) {
					continue
				}
			}
			typ = xgoutil.DerefType(typ)

			// Skip map literals, as they should use general completion to allow
			// variable suggestions inside the literal.
			if isMapType(typ) {
				if ctx.valueExprAtPos(node) != nil {
					ctx.valueExpression = true
				}
				continue
			}

			// Skip slice and array literals, as they should also use general completion
			// to allow variable suggestions inside the literal.
			if _, ok := typ.Underlying().(*gotypes.Slice); ok {
				if ctx.valueExprAtPos(node) != nil {
					ctx.valueExpression = true
				}
				continue
			}
			if _, ok := typ.Underlying().(*gotypes.Array); ok {
				if ctx.valueExprAtPos(node) != nil {
					ctx.valueExpression = true
				}
				continue
			}

			named := resolvedNamedType(typ)
			if named == nil {
				continue
			}
			typ = named
			st, ok := named.Underlying().(*gotypes.Struct)
			if !ok {
				continue
			}

			// Check if we're in a field value position (after the colon in `field: value`).
			// If so, we want general completion for the value, not struct field completion.
			inFieldValue := false
			for _, elt := range node.Elts {
				if kv, ok := elt.(*ast.KeyValueExpr); ok {
					// Check if cursor is in the value part of the key-value pair.
					if kv.Colon < ctx.pos && ctx.pos <= kv.Value.End() {
						inFieldValue = true
						break
					}
				}
			}

			if inFieldValue {
				// Don't set struct literal context for field values.
				ctx.valueExpression = true
				continue
			}

			// CompositeLit is more specific than other contexts, so override.
			ctx.kind = completionKindStructLit
			ctx.expectedStructType = st
			ctx.compositeLitType = named
			ctx.enclosingNode = node
		case *ast.AssignStmt:
			if node.Tok != token.ASSIGN && node.Tok != token.DEFINE {
				continue
			}
			for j, rhs := range node.Rhs {
				if rhs.Pos() > ctx.pos || ctx.pos > rhs.End() {
					continue
				}
				if j < len(node.Lhs) {
					if ctx.isAfterNumberLiteral() {
						continue
					}
					ctx.kind = completionKindAssignOrDefine
					ctx.valueExpression = true
					if typ := ctx.typeInfo.TypeOf(node.Lhs[j]); xgoutil.IsValidType(typ) {
						ctx.expectedTypes = []gotypes.Type{typ}
					}
					if ident, ok := node.Lhs[j].(*ast.Ident); ok {
						defIdent := ctx.typeInfo.ObjToDef[ctx.typeInfo.ObjectOf(ident)]
						if defIdent != nil {
							ctx.assignTargets = append(ctx.assignTargets, defIdent)
						}
					}

					if len(node.Lhs) > 1 && len(node.Rhs) == 1 {
						ctx.expectedFuncResultCount = len(node.Lhs)
						resultVars := make([]*gotypes.Var, 0, len(node.Lhs))
						hasAllTypes := true
						for _, lhsExpr := range node.Lhs {
							typ := ctx.typeInfo.TypeOf(lhsExpr)
							if !xgoutil.IsValidType(typ) {
								hasAllTypes = false
								break
							}
							resultVars = append(resultVars, gotypes.NewVar(lhsExpr.Pos(), ctx.typeInfo.Pkg, "", typ))
						}
						if hasAllTypes {
							sig := gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(resultVars...), false)
							ctx.expectedTypes = append(ctx.expectedTypes, sig)
						}
					}
					break
				}
			}
		case *ast.ReturnStmt:
			sig := ctx.enclosingFunction(path[i+1:])
			if sig == nil {
				continue
			}
			results := sig.Results()
			if results.Len() == 0 {
				continue
			}

			// Check if cursor is inside a composite literal (map or struct) in the
			// return statement. If the cursor is in a value position, we should allow
			// general completion instead of restricting to return type completion.
			shouldSetReturnContext := true
			var mapValueExpectedType gotypes.Type
			for j, result := range node.Results {
				// Check for CompositeLit directly or within UnaryExpr (e.g., &Struct{}).
				var comp *ast.CompositeLit
				var sliceLit *ast.SliceLit
				switch expr := result.(type) {
				case *ast.CompositeLit:
					comp = expr
				case *ast.SliceLit:
					// Handle XGo-style slice literal [...].
					sliceLit = expr
				case *ast.UnaryExpr:
					// Handle &Struct{...} case.
					if c, ok := expr.X.(*ast.CompositeLit); ok {
						comp = c
					}
				}

				// Handle XGo-style slice literal.
				if sliceLit != nil && sliceLit.Pos() <= ctx.pos && ctx.pos <= sliceLit.End() {
					// For XGo-style slice literals, allow general completions.
					ctx.itemSet.setDisallowVoidFuncs(true)
					shouldSetReturnContext = false
					ctx.valueExpression = true
					break
				}

				if comp != nil && comp.Pos() <= ctx.pos && ctx.pos <= comp.End() {
					// Check if we're in a value position inside a composite literal.
					// This applies to maps, slices, and arrays.
					if valueExpr := ctx.valueExprAtPos(comp); valueExpr != nil {
						var expected gotypes.Type
						if j < results.Len() {
							expected = results.At(j).Type()
						}
						if ctx.isMapLiteral(comp) {
							if elemType := ctx.expectedMapElementTypeAtPos(comp, expected); elemType != nil {
								mapValueExpectedType = elemType
							}
							ctx.itemSet.setDisallowVoidFuncs(true)
						}
						// Also handle slices and arrays in value position.
						if ctx.isSliceOrArrayLiteral(comp) {
							ctx.itemSet.setDisallowVoidFuncs(true)
						}
						shouldSetReturnContext = false
						ctx.valueExpression = true
						break
					}
					if ctx.isSliceOrArrayLiteral(comp) {
						shouldSetReturnContext = false
						ctx.valueExpression = true
						break
					}
				}
			}

			if mapValueExpectedType == nil {
				if idx := ctx.findReturnValueIndex(node); idx >= 0 && idx < results.Len() {
					if mapType, ok := xgoutil.DerefType(results.At(idx).Type()).Underlying().(*gotypes.Map); ok {
						mapValueExpectedType = mapType.Elem()
					}
				}
			}
			if mapValueExpectedType == nil {
				for result := range results.Variables() {
					if mapType, ok := xgoutil.DerefType(result.Type()).Underlying().(*gotypes.Map); ok {
						mapValueExpectedType = mapType.Elem()
						break
					}
				}
			}
			if mapValueExpectedType != nil {
				ctx.expectedTypes = []gotypes.Type{mapValueExpectedType}
				ctx.valueExpression = true
			}

			if shouldSetReturnContext {
				ctx.kind = completionKindReturn
				ctx.valueExpression = true
				ctx.returnIndex = ctx.findReturnValueIndex(node)
				if ctx.returnIndex >= 0 && ctx.returnIndex < results.Len() {
					ctx.expectedTypes = []gotypes.Type{results.At(ctx.returnIndex).Type()}
				}
			}
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
		case *ast.SwitchStmt:
			ctx.kind = completionKindSwitchCase
			ctx.switchTag = node.Tag
		case *ast.SelectStmt:
			ctx.kind = completionKindSelect
		case *ast.DeclStmt:
			if genDecl, ok := node.Decl.(*ast.GenDecl); ok && (genDecl.Tok == token.VAR || genDecl.Tok == token.CONST) {
				for _, spec := range genDecl.Specs {
					valueSpec, ok := spec.(*ast.ValueSpec)
					if !ok || len(valueSpec.Names) == 0 {
						continue
					}
					if ctx.isAfterNumberLiteral() {
						continue
					}
					ctx.kind = completionKindDecl
					if typ := ctx.typeInfo.TypeOf(valueSpec.Type); xgoutil.IsValidType(typ) {
						ctx.expectedTypes = []gotypes.Type{typ}
					}
					ctx.assignTargets = valueSpec.Names
					ctx.declValueSpec = valueSpec
					break
				}
			}
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				if ctx.kind == completionKindUnknown {
					ctx.kind = completionKindStringLit
				}
				ctx.inStringLit = true
			}
		case *ast.BlockStmt:
			ctx.kind = completionKindUnknown
		}
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

	ctx.inSpxEventHandler = ctx.result.isInSpxEventHandler(ctx.pos)
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

// isMapType reports whether the given type is a map type.
func isMapType(typ gotypes.Type) bool {
	if !xgoutil.IsValidType(typ) {
		return false
	}
	_, isMap := typ.Underlying().(*gotypes.Map)
	return isMap
}

// isSliceOrArrayLiteral reports whether the given [ast.CompositeLit]
// represents a slice or array literal.
//
// In XGo, slice literals can be written without explicit type declaration when
// passed as function arguments, e.g., `printSlice [1, 2, 3]`.
func (ctx *completionContext) isSliceOrArrayLiteral(comp *ast.CompositeLit) bool {
	// Check if we have type information.
	if typ := ctx.typeInfo.TypeOf(comp); xgoutil.IsValidType(typ) {
		underlying := typ.Underlying()
		_, isSlice := underlying.(*gotypes.Slice)
		_, isArray := underlying.(*gotypes.Array)
		return isSlice || isArray
	}

	// Try to get type information from the Type field if available.
	if comp.Type != nil {
		if typ := ctx.typeInfo.TypeOf(comp.Type); xgoutil.IsValidType(typ) {
			underlying := typ.Underlying()
			_, isSlice := underlying.(*gotypes.Slice)
			_, isArray := underlying.(*gotypes.Array)
			return isSlice || isArray
		}
	}

	// No type info available. In XGo, slice literals without key-value pairs
	// could be slice literals (e.g., [1, 2, 3]).
	// If all elements are NOT key-value pairs, it might be a slice.
	if len(comp.Elts) > 0 {
		for _, elt := range comp.Elts {
			if _, isKV := elt.(*ast.KeyValueExpr); isKV {
				// Has key-value pairs, so it's not a slice
				return false
			}
		}
		// No key-value pairs, could be a slice literal
		return true
	}

	return false
}

// isMapLiteral reports whether the given [ast.CompositeLit] represents a map
// literal.
//
// In XGo, map literals can be written without explicit type declaration when
// passed as function arguments, e.g., `println {"key": value}`.
func (ctx *completionContext) isMapLiteral(comp *ast.CompositeLit) bool {
	// Check if we have type information.
	if typ := ctx.typeInfo.TypeOf(comp); xgoutil.IsValidType(typ) {
		return isMapType(typ)
	}

	// Try to get type information from the Type field if available.
	if comp.Type != nil {
		if typ := ctx.typeInfo.TypeOf(comp.Type); xgoutil.IsValidType(typ) {
			return isMapType(typ)
		}

		// If we have an explicit type but no type info, it's likely not a map.
		// XGo-style map literals don't have an explicit type.
		return false
	}

	// No type info available, but could still be an XGo-style map literal.
	// Check if it contains key-value pairs (characteristic of map literals).
	//
	// Note: An empty composite literal {} is ambiguous and could be either
	// a map or struct, so we don't consider it a map without type info.
	for _, elt := range comp.Elts {
		if _, isKV := elt.(*ast.KeyValueExpr); isKV {
			return true
		}
	}
	return false
}

// mapLiteralElementType returns the element type for the given map literal.
func (ctx *completionContext) mapLiteralElementType(comp *ast.CompositeLit) gotypes.Type {
	if comp == nil {
		return nil
	}

	if typ := ctx.typeInfo.TypeOf(comp); xgoutil.IsValidType(typ) {
		if mapType, ok := xgoutil.DerefType(typ).Underlying().(*gotypes.Map); ok {
			return mapType.Elem()
		}
	}

	if comp.Type != nil {
		if typ := ctx.typeInfo.TypeOf(comp.Type); xgoutil.IsValidType(typ) {
			if mapType, ok := xgoutil.DerefType(typ).Underlying().(*gotypes.Map); ok {
				return mapType.Elem()
			}
		}
	}

	return nil
}

// valueExprAtPos returns the expression for the value located at the current
// position within the given composite literal, handling nested literals.
func (ctx *completionContext) valueExprAtPos(comp *ast.CompositeLit) ast.Expr {
	for _, elt := range comp.Elts {
		// Handle KeyValueExpr for maps and structs.
		if kv, ok := elt.(*ast.KeyValueExpr); ok {
			if kv.Value == nil {
				continue
			}
			if ctx.pos < kv.Value.Pos() || ctx.pos > kv.Value.End()+1 {
				continue
			}

			if innerComp, ok := kv.Value.(*ast.CompositeLit); ok {
				if inner := ctx.valueExprAtPos(innerComp); inner != nil {
					return inner
				}
			}
			return kv.Value
		}

		// Handle direct expressions for slices and arrays.
		if ctx.pos >= elt.Pos() && ctx.pos <= elt.End()+1 {
			if innerComp, ok := elt.(*ast.CompositeLit); ok {
				if inner := ctx.valueExprAtPos(innerComp); inner != nil {
					return inner
				}
			}
			return elt
		}
	}
	return nil
}

// expectedMapElementTypeAtPos returns the map element type for the current
// position if it is within a map literal, handling nested map literals.
func (ctx *completionContext) expectedMapElementTypeAtPos(comp *ast.CompositeLit, expected gotypes.Type) gotypes.Type {
	if comp == nil || ctx.pos < comp.Pos() || ctx.pos > comp.End() {
		return nil
	}

	var mapType gotypes.Type
	if expected != nil {
		mapType = expected
	} else if typ := ctx.typeInfo.TypeOf(comp); xgoutil.IsValidType(typ) {
		mapType = typ
	}

	for _, elt := range comp.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok || kv.Value == nil {
			continue
		}
		if ctx.pos < kv.Value.Pos() || ctx.pos > kv.Value.End()+1 {
			continue
		}

		if typ := ctx.typeInfo.TypeOf(kv.Value); xgoutil.IsValidType(typ) {
			if mapTyp, ok := xgoutil.DerefType(typ).Underlying().(*gotypes.Map); ok {
				return mapTyp.Elem()
			}
			return typ
		}

		if innerComp, ok := kv.Value.(*ast.CompositeLit); ok {
			var innerExpected gotypes.Type
			if mapType != nil {
				if mapTyp, ok := xgoutil.DerefType(mapType).Underlying().(*gotypes.Map); ok {
					innerExpected = mapTyp.Elem()
				}
			}
			if innerExpected == nil {
				if typ := ctx.typeInfo.TypeOf(kv.Value); xgoutil.IsValidType(typ) {
					innerExpected = typ
				}
			}
			if innerType := ctx.expectedMapElementTypeAtPos(innerComp, innerExpected); innerType != nil {
				return innerType
			}
		}

		if mapType != nil {
			if mapTyp, ok := xgoutil.DerefType(mapType).Underlying().(*gotypes.Map); ok {
				return mapTyp.Elem()
			}
		}
		if ctx.isMapLiteral(comp) {
			return ctx.mapLiteralElementType(comp)
		}
		return nil
	}

	if mapType != nil {
		if mapTyp, ok := xgoutil.DerefType(mapType).Underlying().(*gotypes.Map); ok {
			return mapTyp.Elem()
		}
	}
	if ctx.isMapLiteral(comp) && len(comp.Elts) == 0 {
		return ctx.mapLiteralElementType(comp)
	}
	return nil
}

// enclosingFunction gets the function signature containing the current position.
func (ctx *completionContext) enclosingFunction(path []ast.Node) *gotypes.Signature {
	for _, node := range path {
		switch n := node.(type) {
		case *ast.FuncDecl:
			obj := ctx.typeInfo.ObjectOf(n.Name)
			if obj == nil {
				continue
			}
			fun, ok := obj.(*gotypes.Func)
			if !ok {
				continue
			}
			return fun.Type().(*gotypes.Signature)
		case *ast.FuncLit:
			// For function literals, get the type from the type info directly.
			if typ := ctx.typeInfo.TypeOf(n); xgoutil.IsValidType(typ) {
				if sig, ok := typ.(*gotypes.Signature); ok {
					return sig
				}
			}
		}
	}
	return nil
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
	switch ctx.kind {
	case completionKindDisabled:
		return nil
	case completionKindComment,
		completionKindStringLit:
		return nil
	case completionKindGeneral:
		return ctx.collectGeneral()
	case completionKindImport:
		return ctx.collectImport()
	case completionKindDot:
		return ctx.collectDot()
	case completionKindCall:
		return ctx.collectCall()
	case completionKindAssignOrDefine:
		return ctx.collectAssignOrDefine()
	case completionKindDecl:
		return ctx.collectDecl()
	case completionKindReturn:
		return ctx.collectReturn()
	case completionKindStructLit:
		return ctx.collectStructLit()
	case completionKindSwitchCase:
		return ctx.collectSwitchCase()
	case completionKindSelect:
		return ctx.collectSelect()
	}
	return nil
}

// collectGeneral collects general completions.
func (ctx *completionContext) collectGeneral() error {
	for _, expectedType := range ctx.expectedTypes {
		if err := ctx.collectTypeSpecific(expectedType); err != nil {
			return err
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
			FunctionCompletion, // TODO: Add return type compatibility check for FunctionCompletion.
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

	// Add local definitions from innermost scope and its parents.
	pkg := ctx.typeInfo.Pkg
	for scope := ctx.innermostScope; scope != nil; scope = scope.Parent() {
		isInMainScope := ctx.innermostScope == ctx.astFileScope && scope == pkg.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			if !xgoutil.IsExportedOrInMainPkg(obj) {
				continue
			}
			if !ctx.valueExpression {
				if defIdent := ctx.typeInfo.ObjToDef[obj]; defIdent != nil && slices.Contains(ctx.assignTargets, defIdent) {
					continue
				}
			}

			ctx.itemSet.addSpxDefs(ctx.result.spxDefinitionsFor(obj, "")...)

			isThis := name == "this"
			isSpxFileMatch := ctx.spxFile == name+".spx" || (ctx.spxFile == ctx.result.mainSpxFile && name == "Game")
			isMainScopeObj := isInMainScope && isSpxFileMatch
			if isThis || isMainScopeObj {
				named, ok := xgoutil.DerefType(obj.Type()).(*gotypes.Named)
				if ok && xgoutil.IsNamedStructType(named) {
					for _, def := range ctx.result.spxDefinitionsForNamedStruct(named) {
						if ctx.inSpxEventHandler && def.ID.Name != nil {
							name := *def.ID.Name
							if idx := strings.LastIndex(name, "."); idx >= 0 {
								name = name[idx+1:]
							}
							if IsSpxEventHandlerFuncName(name) {
								continue
							}
						}
						ctx.itemSet.addSpxDefs(def)
					}
				}
			}
		}
	}

	// Add imported package definitions.
	for _, importSpec := range ctx.astFile.Imports {
		if importSpec.Path == nil {
			continue
		}
		pkgPath, err := strconv.Unquote(importSpec.Path.Value)
		if err != nil {
			continue
		}
		pkgDoc, err := pkgdata.GetPkgDoc(pkgPath)
		if err != nil {
			continue
		}

		pkgPathBase := path.Base(pkgPath)
		pkgName := pkgPathBase
		if importSpec.Name != nil {
			pkgName = importSpec.Name.Name
		}

		ctx.itemSet.addSpxDefs(SpxDefinition{
			ID: SpxDefinitionIdentifier{
				Package: &pkgPath,
			},
			Overview: "package " + pkgPathBase,
			Detail:   pkgDoc.Doc,

			CompletionItemLabel:            pkgName,
			CompletionItemKind:             ModuleCompletion,
			CompletionItemInsertText:       pkgName,
			CompletionItemInsertTextFormat: PlainTextTextFormat,
		})
	}

	// Add other definitions.
	ctx.itemSet.addSpxDefs(GetSpxPkgDefinitions()...)
	ctx.itemSet.addSpxDefs(GetMathPkgSpxDefinitions()...)
	ctx.itemSet.addSpxDefs(GetBuiltinSpxDefinitions()...)
	ctx.itemSet.addSpxDefs(GeneralSpxDefinitions...)
	if ctx.innermostScope == ctx.astFileScope {
		ctx.itemSet.addSpxDefs(FileScopeSpxDefinitions...)
	}

	return nil
}

// collectImport collects import completions.
func (ctx *completionContext) collectImport() error {
	pkgs, err := pkgdata.ListPkgs()
	if err != nil {
		return fmt.Errorf("failed to list packages: %w", err)
	}
	for _, pkgPath := range pkgs {
		pkgDoc, err := pkgdata.GetPkgDoc(pkgPath)
		if err != nil {
			continue
		}
		ctx.itemSet.addSpxDefs(SpxDefinition{
			ID: SpxDefinitionIdentifier{
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

	if ident, ok := ctx.selectorExpr.X.(*ast.Ident); ok {
		if obj := ctx.typeInfo.ObjectOf(ident); obj != nil {
			if pkgName, ok := obj.(*gotypes.PkgName); ok {
				return ctx.collectPackageMembers(pkgName.Imported())
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
	typ = xgoutil.DerefType(typ)
	named := resolvedNamedType(typ)
	if named != nil {
		if IsInSpxPkg(named.Obj()) && named.Obj().Name() == "Sprite" {
			named = GetSpxSpriteImplType()
		}
		typ = named
	}

	if iface, ok := typ.Underlying().(*gotypes.Interface); ok {
		ctx.collectInterfaceMethodCompletions(iface, named, nil)
	} else if named != nil && xgoutil.IsNamedStructType(named) {
		ctx.itemSet.addSpxDefs(ctx.result.spxDefinitionsForNamedStruct(named)...)
	}
	return nil
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

			sig, ok := fun.Type().(*gotypes.Signature)
			if !ok || sig.Params().Len() != 0 || sig.Results().Len() != 1 {
				continue
			}
			return sig.Results().At(0).Type()
		}
	}
	return nil
}

// collectInterfaceMethodCompletions collects completion items for the provided
// interface type and all of its embedded interfaces. The selectorNamed tracks
// the named interface whose methods should determine the completion definition
// name. The visited prevents infinite recursion for cyclic embeddings.
func (ctx *completionContext) collectInterfaceMethodCompletions(iface *gotypes.Interface, selectorNamed *gotypes.Named, visited map[*gotypes.Interface]struct{}) {
	if iface == nil {
		return
	}
	if visited == nil {
		visited = make(map[*gotypes.Interface]struct{})
	}
	if _, ok := visited[iface]; ok {
		return
	}
	visited[iface] = struct{}{}

	for method := range iface.ExplicitMethods() {
		if !xgoutil.IsExportedOrInMainPkg(method) {
			continue
		}

		recvTypeName := ""
		if selectorNamed != nil {
			selectorObj := selectorNamed.Obj()
			if !xgoutil.IsInMainPkg(selectorObj) || xgoutil.IsInMainPkg(method) {
				recvTypeName = selectorObj.Name()
			}
		}

		spxDef := ctx.result.spxDefinitionForMethod(method, recvTypeName)
		ctx.itemSet.addSpxDefs(spxDef)
	}

	for embedded := range iface.EmbeddedTypes() {
		embedded = gotypes.Unalias(embedded)

		var (
			named          *gotypes.Named
			ifaceToRecurse *gotypes.Interface
		)

		switch t := embedded.(type) {
		case *gotypes.Named:
			named = t
			ifaceToRecurse, _ = t.Underlying().(*gotypes.Interface)
		case *gotypes.Interface:
			ctx.collectInterfaceMethodCompletions(t, selectorNamed, visited)
			continue
		case *gotypes.Pointer:
			elem := gotypes.Unalias(t.Elem())
			if n, ok := elem.(*gotypes.Named); ok {
				named = n
				ifaceToRecurse, _ = n.Underlying().(*gotypes.Interface)
			} else if iface, ok := elem.(*gotypes.Interface); ok {
				ctx.collectInterfaceMethodCompletions(iface, selectorNamed, visited)
				continue
			}
		}

		if ifaceToRecurse != nil {
			nextSelector := selectorNamed
			if named != nil && (nextSelector == nil || (xgoutil.IsInMainPkg(nextSelector.Obj()) && !xgoutil.IsInMainPkg(named.Obj()))) {
				nextSelector = named
			}
			ctx.collectInterfaceMethodCompletions(ifaceToRecurse, nextSelector, visited)
		}
	}
}

// collectPackageMembers collects members of a package.
func (ctx *completionContext) collectPackageMembers(pkg *gotypes.Package) error {
	if pkg == nil {
		return nil
	}

	var pkgDoc *pkgdoc.PkgDoc
	if xgoutil.IsMainPkg(pkg) {
		pkgDoc, _ = ctx.proj.PkgDoc()
	} else {
		pkgPath := xgoutil.PkgPath(pkg)
		var err error
		pkgDoc, err = pkgdata.GetPkgDoc(pkgPath)
		if err != nil {
			return nil
		}
	}

	ctx.itemSet.addSpxDefs(GetSpxDefinitionsForPkg(pkg, pkgDoc)...)
	return nil
}

// collectCall collects function call completions.
func (ctx *completionContext) collectCall() error {
	callExpr, ok := ctx.enclosingNode.(*ast.CallExpr)
	if !ok {
		return nil
	}
	typ := ctx.typeInfo.TypeOf(callExpr.Fun)
	if !xgoutil.IsValidType(typ) {
		return ctx.collectGeneral()
	}
	sig, ok := typ.(*gotypes.Signature)
	if !ok {
		return ctx.collectGeneral()
	}
	argIndex := ctx.getCurrentArgIndex(callExpr)
	if argIndex < 0 {
		return nil
	}

	if fun := xgoutil.FuncFromCallExpr(ctx.typeInfo, callExpr); fun != nil {
		funcOverloads := xgoutil.ExpandXGoOverloadableFunc(fun)
		if len(funcOverloads) > 0 {
			expectedTypes := make([]gotypes.Type, 0, len(funcOverloads))
			for _, funcOverload := range funcOverloads {
				sig := funcOverload.Type().(*gotypes.Signature)
				if argIndex < sig.Params().Len() {
					expectedTypes = append(expectedTypes, sig.Params().At(argIndex).Type())
				} else if sig.Variadic() && argIndex >= sig.Params().Len()-1 {
					expectedTypes = append(expectedTypes, sig.Params().At(sig.Params().Len()-1).Type().(*gotypes.Slice).Elem())
				}
			}
			ctx.expectedTypes = deduplicateTypes(expectedTypes)
			return ctx.collectGeneral()
		}
	}

	if argIndex < sig.Params().Len() {
		ctx.expectedTypes = []gotypes.Type{sig.Params().At(argIndex).Type()}
	} else if sig.Variadic() && argIndex >= sig.Params().Len()-1 {
		ctx.expectedTypes = []gotypes.Type{sig.Params().At(sig.Params().Len() - 1).Type().(*gotypes.Slice).Elem()}
	}
	return ctx.collectGeneral()
}

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
func (ctx *completionContext) getCurrentArgIndex(callExpr *ast.CallExpr) int {
	if len(callExpr.Args) == 0 {
		return 0
	}
	for i, arg := range callExpr.Args {
		if ctx.pos >= arg.Pos() && ctx.pos <= arg.End() {
			return i
		}
	}
	if ctx.pos > callExpr.Args[len(callExpr.Args)-1].End() {
		return len(callExpr.Args)
	}
	return -1
}

// collectAssignOrDefine collects completions for assignments and definitions.
func (ctx *completionContext) collectAssignOrDefine() error {
	return ctx.collectGeneral()
}

// collectDecl collects declaration completions.
func (ctx *completionContext) collectDecl() error {
	return ctx.collectGeneral()
}

// collectReturn collects return value completions.
func (ctx *completionContext) collectReturn() error {
	return ctx.collectGeneral()
}

// collectTypeSpecific collects type-specific completions.
func (ctx *completionContext) collectTypeSpecific(typ gotypes.Type) error {
	if !xgoutil.IsValidType(typ) {
		return nil
	}

	if named := resolvedNamedType(typ); named != nil {
		switch named {
		case GetSpxSpriteType(), GetSpxSpriteImplType():
			for spxSprite := range ctx.result.spxSpriteResourceAutoBindings {
				if spxSprite.Type() == named {
					ctx.itemSet.addSpxDefs(ctx.result.spxDefinitionsFor(spxSprite, "Game")...)
				}
			}
		}
	}

	// Handle spx.PropertyName type - provide property name completions.
	if inferSpxInputTypeFromType(typ) == SpxInputTypePropertyName {
		if target := ctx.getPropertyTarget(); target != "" {
			ctx.collectPropertyNames(target)
		}
		return nil
	}

	var spxResourceIDs []SpxResourceID
	switch canonicalSpxResourceNameType(typ) {
	case GetSpxBackdropNameType():
		spxResourceIDs = slices.Grow(spxResourceIDs, len(ctx.result.spxResourceSet.backdrops))
		for spxBackdropName := range ctx.result.spxResourceSet.backdrops {
			spxResourceIDs = append(spxResourceIDs, SpxBackdropResourceID{spxBackdropName})
		}
	case GetSpxSpriteNameType():
		spxResourceIDs = slices.Grow(spxResourceIDs, len(ctx.result.spxResourceSet.sprites))
		for spxSpriteName := range ctx.result.spxResourceSet.sprites {
			spxResourceIDs = append(spxResourceIDs, SpxSpriteResourceID{spxSpriteName})
		}
	case GetSpxSpriteCostumeNameType():
		expectedSpxSprite := ctx.getSpxSpriteResource()
		for _, spxSprite := range ctx.result.spxResourceSet.sprites {
			if expectedSpxSprite == nil || spxSprite == expectedSpxSprite {
				spxResourceIDs = slices.Grow(spxResourceIDs, len(spxSprite.NormalCostumes))
				for _, spxSpriteCostume := range spxSprite.NormalCostumes {
					spxResourceIDs = append(spxResourceIDs, SpxSpriteCostumeResourceID{spxSprite.Name, spxSpriteCostume.Name})
				}
			}
		}
	case GetSpxSpriteAnimationNameType():
		expectedSpxSprite := ctx.getSpxSpriteResource()
		for _, spxSprite := range ctx.result.spxResourceSet.sprites {
			if expectedSpxSprite == nil || spxSprite == expectedSpxSprite {
				spxResourceIDs = slices.Grow(spxResourceIDs, len(spxSprite.Animations))
				for _, spxSpriteAnimation := range spxSprite.Animations {
					spxResourceIDs = append(spxResourceIDs, SpxSpriteAnimationResourceID{spxSprite.Name, spxSpriteAnimation.Name})
				}
			}
		}
	case GetSpxSoundNameType():
		spxResourceIDs = slices.Grow(spxResourceIDs, len(ctx.result.spxResourceSet.sounds))
		for spxSoundName := range ctx.result.spxResourceSet.sounds {
			spxResourceIDs = append(spxResourceIDs, SpxSoundResourceID{spxSoundName})
		}
	case GetSpxWidgetNameType():
		spxResourceIDs = slices.Grow(spxResourceIDs, len(ctx.result.spxResourceSet.widgets))
		for spxWidgetName := range ctx.result.spxResourceSet.widgets {
			spxResourceIDs = append(spxResourceIDs, SpxWidgetResourceID{spxWidgetName})
		}
	}
	seenResourceNames := make(map[string]struct{}, len(spxResourceIDs))
	for _, spxResourceID := range spxResourceIDs {
		name := spxResourceID.Name()
		if _, ok := seenResourceNames[name]; ok {
			continue
		}
		seenResourceNames[name] = struct{}{}
		if !ctx.inStringLit {
			name = strconv.Quote(name)
		}
		ctx.itemSet.add(CompletionItem{
			Label:            name,
			Kind:             TextCompletion,
			Documentation:    &Or_CompletionItem_documentation{Value: MarkupContent{Kind: Markdown, Value: spxResourceID.URI().HTML()}},
			InsertText:       name,
			InsertTextFormat: ToPtr(PlainTextTextFormat),
		})
	}
	return nil
}

// getSpxSpriteResource returns a [SpxSpriteResource] for the current context.
// It returns nil if no [SpxSpriteResource] can be inferred.
func (ctx *completionContext) getSpxSpriteResource() *SpxSpriteResource {
	callExpr := ctx.getEnclosingCallExpr()
	if callExpr != nil {
		return inferSpxSpriteResourceEnclosingNode(ctx.result, callExpr)
	}
	return ctx.getCurrentFileSpxSpriteResource()
}

func (ctx *completionContext) getEnclosingCallExpr() *ast.CallExpr {
	if callExpr, ok := ctx.enclosingNode.(*ast.CallExpr); ok {
		return callExpr
	}
	return ctx.enclosingCallExpr
}

func (ctx *completionContext) getCurrentFileSpxSpriteResource() *SpxSpriteResource {
	if ctx.spxFile == "" || path.Base(ctx.spxFile) == path.Base(ctx.result.mainSpxFile) {
		return nil
	}
	return ctx.result.spxResourceSet.sprites[strings.TrimSuffix(path.Base(ctx.spxFile), ".spx")]
}

// getPropertyTarget returns the target type name for property name completions.
// It looks at the enclosing call expression's receiver type (if any) and falls
// back to the current file's type.
func (ctx *completionContext) getPropertyTarget() string {
	if ctx.kind == completionKindCall {
		if callExpr, ok := ctx.enclosingNode.(*ast.CallExpr); ok {
			named := PropertyTargetNamedTypeForCall(ctx.typeInfo, callExpr, ctx.spxFile, ctx.result.mainSpxFile)
			if named != nil {
				// For explicit-receiver calls, only consider main-package types.
				if _, hasSel := callExpr.Fun.(*ast.SelectorExpr); hasSel && !xgoutil.IsInMainPkg(named.Obj()) {
					return ""
				}
				return named.Obj().Name()
			}
			return ""
		}
	}
	// For implicit receiver calls, derive target from the current file's type.
	if ctx.spxFile == "" {
		return ""
	}
	if ctx.spxFile == ctx.result.mainSpxFile {
		return "Game"
	}
	return strings.TrimSuffix(path.Base(ctx.spxFile), ".spx")
}

// collectPropertyNames collects property name completion items for the given target type.
func (ctx *completionContext) collectPropertyNames(target string) {
	pkgScope := ctx.typeInfo.Pkg.Scope()
	obj := pkgScope.Lookup(target)
	if obj == nil {
		return
	}
	typeName, ok := obj.(*gotypes.TypeName)
	if !ok {
		return
	}
	typ := gotypes.Unalias(typeName.Type())
	typ = xgoutil.DerefType(typ)
	namedType, ok := typ.(*gotypes.Named)
	if !ok {
		return
	}

	mainPkgDoc, _ := ctx.proj.PkgDoc()
	ctx.collectPropertyNamesFromNamedType(namedType, mainPkgDoc, make(map[*gotypes.Named]bool), make(map[string]bool))
}

// collectPropertyNamesFromNamedType collects property name completion items
// from the given named type (including embedded types) using walkPropertyMembers.
func (ctx *completionContext) collectPropertyNamesFromNamedType(namedType *gotypes.Named, mainPkgDoc *pkgdoc.PkgDoc, visited map[*gotypes.Named]bool, seenNames map[string]bool) {
	walkPropertyMembers(namedType, makePkgDocFor(mainPkgDoc), visited, seenNames, func(m propertyMember) {
		insertText := m.Name
		if !ctx.inStringLit {
			insertText = strconv.Quote(m.Name)
		}
		def := m.SpxDef
		// TypeHint must be nil so addSpxDefs does not filter property-name
		// items by expected type compatibility.
		def.TypeHint = nil
		// Regardless of whether the property is backed by a field or a method,
		// it is presented as a property to the user.
		def.CompletionItemKind = PropertyCompletion
		def.CompletionItemLabel = insertText
		def.CompletionItemInsertText = insertText
		def.CompletionItemInsertTextFormat = PlainTextTextFormat
		ctx.itemSet.addSpxDefs(def)
	})
}

// collectStructLit collects struct literal completions.
func (ctx *completionContext) collectStructLit() error {
	if ctx.expectedStructType == nil || ctx.compositeLitType == nil {
		return nil
	}

	selectorTypeName := ctx.compositeLitType.Obj().Name()
	if IsInSpxPkg(ctx.compositeLitType.Obj()) && selectorTypeName == "SpriteImpl" {
		selectorTypeName = "Sprite"
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

		spxDef := ctx.result.spxDefinitionForField(field, selectorTypeName)
		spxDef.CompletionItemInsertText = field.Name() + ": ${1:}"
		spxDef.CompletionItemInsertTextFormat = SnippetTextFormat
		ctx.itemSet.addSpxDefs(spxDef)
	}

	return nil
}

// collectSwitchCase collects switch/case completions.
func (ctx *completionContext) collectSwitchCase() error {
	if ctx.switchTag == nil {
		for _, name := range []string{"int", "string", "bool", "error"} {
			if obj := gotypes.Universe.Lookup(name); obj != nil {
				ctx.itemSet.addSpxDefs(GetSpxDefinitionForBuiltinObj(obj))
			}
		}
		return nil
	}

	typ := ctx.typeInfo.TypeOf(ctx.switchTag)
	if !xgoutil.IsValidType(typ) {
		return nil
	}
	named := resolvedNamedType(typ)
	if named == nil {
		return nil
	}
	pkg := named.Obj().Pkg()
	if pkg == nil {
		return nil
	}

	var pkgDoc *pkgdoc.PkgDoc
	if xgoutil.IsMainPkg(pkg) {
		pkgDoc, _ = ctx.proj.PkgDoc()
	} else {
		pkgPath := xgoutil.PkgPath(pkg)
		pkgDoc, _ = pkgdata.GetPkgDoc(pkgPath)
	}

	scope := pkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		c, ok := obj.(*gotypes.Const)
		if !ok {
			continue
		}

		if gotypes.Identical(c.Type(), typ) {
			ctx.itemSet.addSpxDefs(GetSpxDefinitionForConst(c, pkgDoc))
		}
	}
	return nil
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
	VariableCompletion:  1,
	FieldCompletion:     2,
	PropertyCompletion:  3,
	MethodCompletion:    4,
	FunctionCompletion:  5,
	ConstantCompletion:  6,
	ClassCompletion:     7,
	InterfaceCompletion: 8,
	ModuleCompletion:    9,
	KeywordCompletion:   10,
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

// completionItemSet is a set of completion items.
type completionItemSet struct {
	items                         []CompletionItem
	seenSpxDefs                   map[string]struct{}
	supportedKinds                map[CompletionItemKind]struct{}
	isCompatibleWithExpectedTypes func(typ gotypes.Type) bool
	disallowVoidFuncs             bool
	expectedFuncResultCount       int
	expectedTypes                 []gotypes.Type
}

// newCompletionItemSet creates a new [completionItemSet].
func newCompletionItemSet() *completionItemSet {
	return &completionItemSet{
		items:       []CompletionItem{},
		seenSpxDefs: make(map[string]struct{}),
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
	s.isCompatibleWithExpectedTypes = func(typ gotypes.Type) bool {
		for _, expectedType := range expectedTypes {
			if xgoutil.IsValidType(expectedType) {
				// First check direct compatibility.
				if xgoutil.IsTypesCompatible(typ, expectedType) {
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

// addSpxDefs adds spx definitions to the set.
func (s *completionItemSet) addSpxDefs(spxDefs ...SpxDefinition) {
	for _, spxDef := range spxDefs {
		if s.expectedFuncResultCount > 0 {
			if sig, ok := spxDef.TypeHint.(*gotypes.Signature); ok {
				resultCount := sig.Results().Len()
				// Exclude multi-return functions with mismatched count.
				// Single-return functions are allowed to fall through for further type checks.
				if resultCount > 1 && resultCount != s.expectedFuncResultCount {
					continue
				}
			}
		}
		if s.disallowVoidFuncs && spxDef.CompletionItemKind == FunctionCompletion {
			if sig, ok := spxDef.TypeHint.(*gotypes.Signature); ok && sig.Results().Len() == 0 {
				continue
			}
		}
		if s.isCompatibleWithExpectedTypes != nil {
			typeToCompare := spxDef.TypeHint
			if sig, ok := typeToCompare.(*gotypes.Signature); ok {
				switch sig.Results().Len() {
				case 0:
					// Void functions are not compatible with any expected type.
					continue
				case 1:
					// For single-return functions, check the return type's compatibility.
					typeToCompare = sig.Results().At(0).Type()
				}
			}

			if !s.isCompatibleWithExpectedTypes(typeToCompare) {
				continue
			}
		}

		spxDefIDKey := spxDef.ID.String()
		if _, ok := s.seenSpxDefs[spxDefIDKey]; ok {
			continue
		}
		s.seenSpxDefs[spxDefIDKey] = struct{}{}

		s.add(spxDef.CompletionItem())
	}
}
