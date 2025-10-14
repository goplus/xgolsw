package server

import (
	"cmp"
	"fmt"
	"go/types"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"

	xgoast "github.com/goplus/xgo/ast"
	xgoscanner "github.com/goplus/xgo/scanner"
	xgotoken "github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
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
	typeInfo       *xgotypes.Info
	result         *compileResult
	spxFile        string
	astFile        *xgoast.File
	astFileScope   *types.Scope
	tokenFile      *xgotoken.File
	pos            xgotoken.Pos
	innermostScope *types.Scope

	kind completionKind

	enclosingNode      xgoast.Node
	selectorExpr       *xgoast.SelectorExpr
	expectedTypes      []types.Type
	expectedStructType *types.Struct
	compositeLitType   *types.Named
	assignTargets      []*xgoast.Ident
	declValueSpec      *xgoast.ValueSpec
	switchTag          xgoast.Expr
	returnIndex        int

	inStringLit             bool
	inSpxEventHandler       bool
	valueExpression         bool
	expectedFuncResultCount int
}

// analyze analyzes the completion context to determine the kind of completion needed.
func (ctx *completionContext) analyze() {
	path, _ := xgoutil.PathEnclosingInterval(ctx.astFile, ctx.pos-1, ctx.pos)
	for i, node := range slices.Backward(path) {
		switch node := node.(type) {
		case *xgoast.ImportSpec:
			ctx.kind = completionKindImport
		case *xgoast.SelectorExpr:
			if node.Sel == nil || node.Sel.End() >= ctx.pos {
				ctx.kind = completionKindDot
				ctx.selectorExpr = node
			}
		case *xgoast.CallExpr:
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
					if sl, ok := arg.(*xgoast.SliceLit); ok {
						if sl.Pos() <= ctx.pos && ctx.pos <= sl.End() {
							shouldSetCallContext = false
							break
						}
					}

					comp, ok := arg.(*xgoast.CompositeLit)
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
							if kv, ok := elt.(*xgoast.KeyValueExpr); ok {
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
		case *xgoast.FuncLit:
			// Skip FuncLit, as we want general completion inside function literals
			// to allow access to all variables and identifiers.
			continue
		case *xgoast.SliceLit:
			// Skip SliceLit, as XGo-style slice literals should allow general completion
			// to access all variables and identifiers.
			continue
		case *xgoast.CompositeLit:
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
			if _, ok := typ.Underlying().(*types.Slice); ok {
				if ctx.valueExprAtPos(node) != nil {
					ctx.valueExpression = true
				}
				continue
			}
			if _, ok := typ.Underlying().(*types.Array); ok {
				if ctx.valueExprAtPos(node) != nil {
					ctx.valueExpression = true
				}
				continue
			}

			named, ok := typ.(*types.Named)
			if !ok {
				continue
			}
			st, ok := named.Underlying().(*types.Struct)
			if !ok {
				continue
			}

			// Check if we're in a field value position (after the colon in `field: value`).
			// If so, we want general completion for the value, not struct field completion.
			inFieldValue := false
			for _, elt := range node.Elts {
				if kv, ok := elt.(*xgoast.KeyValueExpr); ok {
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
		case *xgoast.AssignStmt:
			if node.Tok != xgotoken.ASSIGN && node.Tok != xgotoken.DEFINE {
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
						ctx.expectedTypes = []types.Type{typ}
					}
					if ident, ok := node.Lhs[j].(*xgoast.Ident); ok {
						defIdent := ctx.typeInfo.ObjToDef[ctx.typeInfo.ObjectOf(ident)]
						if defIdent != nil {
							ctx.assignTargets = append(ctx.assignTargets, defIdent)
						}
					}

					if len(node.Lhs) > 1 && len(node.Rhs) == 1 {
						ctx.expectedFuncResultCount = len(node.Lhs)
						resultVars := make([]*types.Var, 0, len(node.Lhs))
						hasAllTypes := true
						for _, lhsExpr := range node.Lhs {
							typ := ctx.typeInfo.TypeOf(lhsExpr)
							if !xgoutil.IsValidType(typ) {
								hasAllTypes = false
								break
							}
							resultVars = append(resultVars, types.NewVar(lhsExpr.Pos(), ctx.typeInfo.Pkg, "", typ))
						}
						if hasAllTypes {
							sig := types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(resultVars...), false)
							ctx.expectedTypes = append(ctx.expectedTypes, sig)
						}
					}
					break
				}
			}
		case *xgoast.ReturnStmt:
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
			var mapValueExpectedType types.Type
			for j, result := range node.Results {
				// Check for CompositeLit directly or within UnaryExpr (e.g., &Struct{}).
				var comp *xgoast.CompositeLit
				var sliceLit *xgoast.SliceLit
				switch expr := result.(type) {
				case *xgoast.CompositeLit:
					comp = expr
				case *xgoast.SliceLit:
					// Handle XGo-style slice literal [...].
					sliceLit = expr
				case *xgoast.UnaryExpr:
					// Handle &Struct{...} case.
					if c, ok := expr.X.(*xgoast.CompositeLit); ok {
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
						var expected types.Type
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
					if mapType, ok := xgoutil.DerefType(results.At(idx).Type()).Underlying().(*types.Map); ok {
						mapValueExpectedType = mapType.Elem()
					}
				}
			}
			if mapValueExpectedType == nil {
				for result := range results.Variables() {
					if mapType, ok := xgoutil.DerefType(result.Type()).Underlying().(*types.Map); ok {
						mapValueExpectedType = mapType.Elem()
						break
					}
				}
			}
			if mapValueExpectedType != nil {
				ctx.expectedTypes = []types.Type{mapValueExpectedType}
				ctx.valueExpression = true
			}

			if shouldSetReturnContext {
				ctx.kind = completionKindReturn
				ctx.valueExpression = true
				ctx.returnIndex = ctx.findReturnValueIndex(node)
				if ctx.returnIndex >= 0 && ctx.returnIndex < results.Len() {
					ctx.expectedTypes = []types.Type{results.At(ctx.returnIndex).Type()}
				}
			}
		case *xgoast.GoStmt:
			ctx.kind = completionKindCall
			ctx.enclosingNode = node.Call
			ctx.valueExpression = true
		case *xgoast.DeferStmt:
			ctx.kind = completionKindCall
			ctx.enclosingNode = node.Call
			ctx.valueExpression = true
		case *xgoast.SwitchStmt:
			ctx.kind = completionKindSwitchCase
			ctx.switchTag = node.Tag
		case *xgoast.SelectStmt:
			ctx.kind = completionKindSelect
		case *xgoast.DeclStmt:
			if genDecl, ok := node.Decl.(*xgoast.GenDecl); ok && (genDecl.Tok == xgotoken.VAR || genDecl.Tok == xgotoken.CONST) {
				for _, spec := range genDecl.Specs {
					valueSpec, ok := spec.(*xgoast.ValueSpec)
					if !ok || len(valueSpec.Names) == 0 {
						continue
					}
					if ctx.isAfterNumberLiteral() {
						continue
					}
					ctx.kind = completionKindDecl
					if typ := ctx.typeInfo.TypeOf(valueSpec.Type); xgoutil.IsValidType(typ) {
						ctx.expectedTypes = []types.Type{typ}
					}
					ctx.assignTargets = valueSpec.Names
					ctx.declValueSpec = valueSpec
					break
				}
			}
		case *xgoast.BasicLit:
			if node.Kind == xgotoken.STRING {
				if ctx.kind == completionKindUnknown {
					ctx.kind = completionKindStringLit
				}
				ctx.inStringLit = true
			}
		case *xgoast.BlockStmt:
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
	var s xgoscanner.Scanner
	s.Init(ctx.tokenFile, ctx.astFile.Code, nil, 0)

	var (
		lastPos       xgotoken.Pos
		lastTok       xgotoken.Token
		inImportGroup bool
	)
	for {
		pos, tok, lit := s.Scan()
		if tok == xgotoken.EOF {
			break
		}

		// Track if we're inside an import group.
		if lastTok == xgotoken.IMPORT && tok == xgotoken.LPAREN {
			inImportGroup = true
		} else if tok == xgotoken.RPAREN {
			inImportGroup = false
		}

		// Check if we found `import` followed by `"` or we're in an import group.
		if (lastTok == xgotoken.IMPORT || inImportGroup) &&
			(tok == xgotoken.STRING || tok == xgotoken.ILLEGAL) {
			// Check if position is after `import` keyword or within an import
			// group, and inside a string literal (complete or incomplete).
			if lastPos <= ctx.pos && ctx.pos <= pos+xgotoken.Pos(len(lit)) {
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
	fileBase := xgotoken.Pos(ctx.tokenFile.Base())
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
	fileBase := xgotoken.Pos(ctx.tokenFile.Base())
	relPos := ctx.pos - fileBase
	if relPos < 0 || int(relPos) > len(ctx.astFile.Code) {
		return false
	}

	var s xgoscanner.Scanner
	s.Init(ctx.tokenFile, ctx.astFile.Code, nil, 0)

	for {
		pos, tok, lit := s.Scan()
		if tok == xgotoken.EOF {
			break
		}

		// Check if position is inside this token. For identifiers, we should
		// be either in the middle or at the end to trigger completion (not
		// at the beginning).
		if pos < ctx.pos && ctx.pos <= pos+xgotoken.Pos(len(lit)) {
			return tok == xgotoken.IDENT
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
	fileBase := xgotoken.Pos(ctx.tokenFile.Base())
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
func isMapType(typ types.Type) bool {
	if !xgoutil.IsValidType(typ) {
		return false
	}
	_, isMap := typ.Underlying().(*types.Map)
	return isMap
}

// isSliceOrArrayLiteral reports whether the given [xgoast.CompositeLit]
// represents a slice or array literal.
//
// In XGo, slice literals can be written without explicit type declaration when
// passed as function arguments, e.g., `printSlice [1, 2, 3]`.
func (ctx *completionContext) isSliceOrArrayLiteral(comp *xgoast.CompositeLit) bool {
	// Check if we have type information.
	if typ := ctx.typeInfo.TypeOf(comp); xgoutil.IsValidType(typ) {
		underlying := typ.Underlying()
		_, isSlice := underlying.(*types.Slice)
		_, isArray := underlying.(*types.Array)
		return isSlice || isArray
	}

	// Try to get type information from the Type field if available.
	if comp.Type != nil {
		if typ := ctx.typeInfo.TypeOf(comp.Type); xgoutil.IsValidType(typ) {
			underlying := typ.Underlying()
			_, isSlice := underlying.(*types.Slice)
			_, isArray := underlying.(*types.Array)
			return isSlice || isArray
		}
	}

	// No type info available. In XGo, slice literals without key-value pairs
	// could be slice literals (e.g., [1, 2, 3]).
	// If all elements are NOT key-value pairs, it might be a slice.
	if len(comp.Elts) > 0 {
		for _, elt := range comp.Elts {
			if _, isKV := elt.(*xgoast.KeyValueExpr); isKV {
				// Has key-value pairs, so it's not a slice
				return false
			}
		}
		// No key-value pairs, could be a slice literal
		return true
	}

	return false
}

// isMapLiteral reports whether the given [xgoast.CompositeLit] represents a map
// literal.
//
// In XGo, map literals can be written without explicit type declaration when
// passed as function arguments, e.g., `println {"key": value}`.
func (ctx *completionContext) isMapLiteral(comp *xgoast.CompositeLit) bool {
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
		if _, isKV := elt.(*xgoast.KeyValueExpr); isKV {
			return true
		}
	}
	return false
}

// mapLiteralElementType returns the element type for the given map literal.
func (ctx *completionContext) mapLiteralElementType(comp *xgoast.CompositeLit) types.Type {
	if comp == nil {
		return nil
	}

	if typ := ctx.typeInfo.TypeOf(comp); xgoutil.IsValidType(typ) {
		if mapType, ok := xgoutil.DerefType(typ).Underlying().(*types.Map); ok {
			return mapType.Elem()
		}
	}

	if comp.Type != nil {
		if typ := ctx.typeInfo.TypeOf(comp.Type); xgoutil.IsValidType(typ) {
			if mapType, ok := xgoutil.DerefType(typ).Underlying().(*types.Map); ok {
				return mapType.Elem()
			}
		}
	}

	return nil
}

// valueExprAtPos returns the expression for the value located at the current
// position within the given composite literal, handling nested literals.
func (ctx *completionContext) valueExprAtPos(comp *xgoast.CompositeLit) xgoast.Expr {
	for _, elt := range comp.Elts {
		// Handle KeyValueExpr for maps and structs.
		if kv, ok := elt.(*xgoast.KeyValueExpr); ok {
			if kv.Value == nil {
				continue
			}
			if ctx.pos < kv.Value.Pos() || ctx.pos > kv.Value.End()+1 {
				continue
			}

			if innerComp, ok := kv.Value.(*xgoast.CompositeLit); ok {
				if inner := ctx.valueExprAtPos(innerComp); inner != nil {
					return inner
				}
			}
			return kv.Value
		}

		// Handle direct expressions for slices and arrays.
		if ctx.pos >= elt.Pos() && ctx.pos <= elt.End()+1 {
			if innerComp, ok := elt.(*xgoast.CompositeLit); ok {
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
func (ctx *completionContext) expectedMapElementTypeAtPos(comp *xgoast.CompositeLit, expected types.Type) types.Type {
	if comp == nil || ctx.pos < comp.Pos() || ctx.pos > comp.End() {
		return nil
	}

	var mapType types.Type
	if expected != nil {
		mapType = expected
	} else if typ := ctx.typeInfo.TypeOf(comp); xgoutil.IsValidType(typ) {
		mapType = typ
	}

	for _, elt := range comp.Elts {
		kv, ok := elt.(*xgoast.KeyValueExpr)
		if !ok || kv.Value == nil {
			continue
		}
		if ctx.pos < kv.Value.Pos() || ctx.pos > kv.Value.End()+1 {
			continue
		}

		if typ := ctx.typeInfo.TypeOf(kv.Value); xgoutil.IsValidType(typ) {
			if mapTyp, ok := xgoutil.DerefType(typ).Underlying().(*types.Map); ok {
				return mapTyp.Elem()
			}
			return typ
		}

		if innerComp, ok := kv.Value.(*xgoast.CompositeLit); ok {
			var innerExpected types.Type
			if mapType != nil {
				if mapTyp, ok := xgoutil.DerefType(mapType).Underlying().(*types.Map); ok {
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
			if mapTyp, ok := xgoutil.DerefType(mapType).Underlying().(*types.Map); ok {
				return mapTyp.Elem()
			}
		}
		if ctx.isMapLiteral(comp) {
			return ctx.mapLiteralElementType(comp)
		}
		return nil
	}

	if mapType != nil {
		if mapTyp, ok := xgoutil.DerefType(mapType).Underlying().(*types.Map); ok {
			return mapTyp.Elem()
		}
	}
	if ctx.isMapLiteral(comp) && len(comp.Elts) == 0 {
		return ctx.mapLiteralElementType(comp)
	}
	return nil
}

// enclosingFunction gets the function signature containing the current position.
func (ctx *completionContext) enclosingFunction(path []xgoast.Node) *types.Signature {
	for _, node := range path {
		switch n := node.(type) {
		case *xgoast.FuncDecl:
			obj := ctx.typeInfo.ObjectOf(n.Name)
			if obj == nil {
				continue
			}
			fun, ok := obj.(*types.Func)
			if !ok {
				continue
			}
			return fun.Type().(*types.Signature)
		case *xgoast.FuncLit:
			// For function literals, get the type from the type info directly.
			if typ := ctx.typeInfo.TypeOf(n); xgoutil.IsValidType(typ) {
				if sig, ok := typ.(*types.Signature); ok {
					return sig
				}
			}
		}
	}
	return nil
}

// findReturnValueIndex finds the index of the return value at the current position.
func (ctx *completionContext) findReturnValueIndex(ret *xgoast.ReturnStmt) int {
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
				named, ok := xgoutil.DerefType(obj.Type()).(*types.Named)
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

	if ident, ok := ctx.selectorExpr.X.(*xgoast.Ident); ok {
		if obj := ctx.typeInfo.ObjectOf(ident); obj != nil {
			if pkgName, ok := obj.(*types.PkgName); ok {
				return ctx.collectPackageMembers(pkgName.Imported())
			}
		}
	}

	typ := ctx.typeInfo.TypeOf(ctx.selectorExpr.X)
	if !xgoutil.IsValidType(typ) {
		return nil
	}
	typ = xgoutil.DerefType(typ)
	named, ok := typ.(*types.Named)
	if ok && IsInSpxPkg(named.Obj()) && named.Obj().Name() == "Sprite" {
		typ = GetSpxSpriteImplType()
	}

	if iface, ok := typ.Underlying().(*types.Interface); ok {
		for method := range iface.Methods() {
			if !xgoutil.IsExportedOrInMainPkg(method) {
				continue
			}

			var recvTypeName string
			if named != nil && xgoutil.IsInMainPkg(named.Obj()) {
				recvTypeName = named.Obj().Name()
			}

			spxDef := ctx.result.spxDefinitionForMethod(method, recvTypeName)
			ctx.itemSet.addSpxDefs(spxDef)
		}
	} else if named, ok := typ.(*types.Named); ok && xgoutil.IsNamedStructType(named) {
		ctx.itemSet.addSpxDefs(ctx.result.spxDefinitionsForNamedStruct(named)...)
	}
	return nil
}

// collectPackageMembers collects members of a package.
func (ctx *completionContext) collectPackageMembers(pkg *types.Package) error {
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
	callExpr, ok := ctx.enclosingNode.(*xgoast.CallExpr)
	if !ok {
		return nil
	}
	typ := ctx.typeInfo.TypeOf(callExpr.Fun)
	if !xgoutil.IsValidType(typ) {
		return ctx.collectGeneral()
	}
	sig, ok := typ.(*types.Signature)
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
			expectedTypes := make([]types.Type, 0, len(funcOverloads))
			for _, funcOverload := range funcOverloads {
				sig := funcOverload.Type().(*types.Signature)
				if argIndex < sig.Params().Len() {
					expectedTypes = append(expectedTypes, sig.Params().At(argIndex).Type())
				} else if sig.Variadic() && argIndex >= sig.Params().Len()-1 {
					expectedTypes = append(expectedTypes, sig.Params().At(sig.Params().Len()-1).Type().(*types.Slice).Elem())
				}
			}
			ctx.expectedTypes = slices.Compact(expectedTypes)
			return ctx.collectGeneral()
		}
	}

	if argIndex < sig.Params().Len() {
		ctx.expectedTypes = []types.Type{sig.Params().At(argIndex).Type()}
	} else if sig.Variadic() && argIndex >= sig.Params().Len()-1 {
		ctx.expectedTypes = []types.Type{sig.Params().At(sig.Params().Len() - 1).Type().(*types.Slice).Elem()}
	}
	return ctx.collectGeneral()
}

// getCurrentArgIndex gets the current argument index in a function call.
func (ctx *completionContext) getCurrentArgIndex(callExpr *xgoast.CallExpr) int {
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
func (ctx *completionContext) collectTypeSpecific(typ types.Type) error {
	if !xgoutil.IsValidType(typ) {
		return nil
	}

	var spxResourceIDs []SpxResourceID
	switch typ {
	case GetSpxBackdropNameType():
		spxResourceIDs = slices.Grow(spxResourceIDs, len(ctx.result.spxResourceSet.backdrops))
		for spxBackdropName := range ctx.result.spxResourceSet.backdrops {
			spxResourceIDs = append(spxResourceIDs, SpxBackdropResourceID{spxBackdropName})
		}
	case GetSpxSpriteType(), GetSpxSpriteImplType():
		for spxSprite := range ctx.result.spxSpriteResourceAutoBindings {
			if spxSprite.Type() == typ {
				ctx.itemSet.addSpxDefs(ctx.result.spxDefinitionsFor(spxSprite, "Game")...)
			}
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
	for _, spxResourceID := range spxResourceIDs {
		name := spxResourceID.Name()
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
	if ctx.kind != completionKindCall {
		return nil
	}

	callExpr, ok := ctx.enclosingNode.(*xgoast.CallExpr)
	if !ok {
		return nil
	}
	sel, ok := callExpr.Fun.(*xgoast.SelectorExpr)
	if !ok {
		if ctx.spxFile == "main.spx" {
			return nil
		}
		return ctx.result.spxResourceSet.sprites[strings.TrimSuffix(ctx.spxFile, ".spx")]
	}

	ident, ok := sel.X.(*xgoast.Ident)
	if !ok {
		return nil
	}
	obj := ctx.typeInfo.ObjectOf(ident)
	if obj == nil {
		return nil
	}
	named, ok := xgoutil.DerefType(obj.Type()).(*types.Named)
	if !ok {
		return nil
	}

	if named == GetSpxSpriteType() {
		return ctx.result.spxResourceSet.sprites[ident.Name]
	}
	if ctx.result.hasSpxSpriteType(named) {
		return ctx.result.spxResourceSet.sprites[obj.Name()]
	}
	return nil
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
	if composite, ok := ctx.enclosingNode.(*xgoast.CompositeLit); ok {
		for _, elem := range composite.Elts {
			if kv, ok := elem.(*xgoast.KeyValueExpr); ok {
				if ident, ok := kv.Key.(*xgoast.Ident); ok {
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
			if obj := types.Universe.Lookup(name); obj != nil {
				ctx.itemSet.addSpxDefs(GetSpxDefinitionForBuiltinObj(obj))
			}
		}
		return nil
	}

	typ := ctx.typeInfo.TypeOf(ctx.switchTag)
	if !xgoutil.IsValidType(typ) {
		return nil
	}
	named, ok := typ.(*types.Named)
	if !ok {
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
		c, ok := obj.(*types.Const)
		if !ok {
			continue
		}

		if types.Identical(c.Type(), typ) {
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
	MethodCompletion:    3,
	FunctionCompletion:  4,
	ConstantCompletion:  5,
	ClassCompletion:     6,
	InterfaceCompletion: 7,
	ModuleCompletion:    8,
	KeywordCompletion:   9,
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
	isCompatibleWithExpectedTypes func(typ types.Type) bool
	disallowVoidFuncs             bool
	expectedFuncResultCount       int
	expectedTypes                 []types.Type
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
func (s *completionItemSet) setExpectedTypes(expectedTypes []types.Type) {
	if len(expectedTypes) == 0 {
		return
	}

	s.expectedTypes = expectedTypes
	s.isCompatibleWithExpectedTypes = func(typ types.Type) bool {
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
			if sig, ok := spxDef.TypeHint.(*types.Signature); ok {
				resultCount := sig.Results().Len()
				// Exclude multi-return functions with mismatched count.
				// Single-return functions are allowed to fall through for further type checks.
				if resultCount > 1 && resultCount != s.expectedFuncResultCount {
					continue
				}
			}
		}
		if s.disallowVoidFuncs && spxDef.CompletionItemKind == FunctionCompletion {
			if sig, ok := spxDef.TypeHint.(*types.Signature); ok && sig.Results().Len() == 0 {
				continue
			}
		}
		if s.isCompatibleWithExpectedTypes != nil {
			typeToCompare := spxDef.TypeHint
			if sig, ok := typeToCompare.(*types.Signature); ok {
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
