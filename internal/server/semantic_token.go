package server

import (
	"bytes"
	"cmp"
	"fmt"
	gotypes "go/types"
	"slices"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/scanner"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

var (
	// semanticTokenTypesLegend defines the semantic token types we support
	// and their indexes.
	semanticTokenTypesLegend = []SemanticTokenTypes{
		NamespaceType,
		TypeType,
		InterfaceType,
		StructType,
		ParameterType,
		VariableType,
		PropertyType,
		FunctionType,
		MethodType,
		KeywordType,
		CommentType,
		StringType,
		NumberType,
		OperatorType,
		LabelType,
		EnumType,
		EnumMemberType,
	}

	// semanticTokenModifiersLegend defines the semantic token modifiers we
	// support and their bit positions.
	semanticTokenModifiersLegend = []SemanticTokenModifiers{
		ModDeclaration,
		ModReadonly,
		ModStatic,
		ModDefaultLibrary,
	}
)

// getSemanticTokenTypeIndex returns the index of the given token type in the legend.
func getSemanticTokenTypeIndex(tokenType SemanticTokenTypes) uint32 {
	idx := slices.Index(semanticTokenTypesLegend, tokenType)
	if idx == -1 {
		return 0 // Fallback to first type.
	}
	return uint32(idx)
}

// getSemanticTokenModifiersMask returns the bit mask for the given modifiers.
func getSemanticTokenModifiersMask(modifiers []SemanticTokenModifiers) uint32 {
	var mask uint32
	for _, mod := range modifiers {
		if i := slices.Index(semanticTokenModifiersLegend, mod); i >= 0 {
			mask |= 1 << uint32(i)
		}
	}
	return mask
}

// semanticTokenInfo represents the information of a semantic token.
type semanticTokenInfo struct {
	startPos       token.Pos
	endPos         token.Pos
	tokenType      SemanticTokenTypes
	tokenModifiers []SemanticTokenModifiers
}

// semanticTokenLegendStrings converts a semantic token legend to protocol strings.
func semanticTokenLegendStrings[T ~string](legend []T) []string {
	values := make([]string, len(legend))
	for i, value := range legend {
		values[i] = string(value)
	}
	return values
}

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_semanticTokens
func (s *Server) textDocumentSemanticTokensFull(params *SemanticTokensParams) (*SemanticTokens, error) {
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
	typeInfo, _ := expressionTypeInfo(proj)
	if typeInfo == nil {
		return nil, nil
	}
	enums, err := enumInfoForProject(proj)
	if err != nil {
		return nil, err
	}

	fset := proj.Fset
	file := xgoutil.NodeTokenFile(fset, astFile)
	var tokenInfos []semanticTokenInfo
	addToken := func(startPos, endPos token.Pos, tokenType SemanticTokenTypes, tokenModifiers []SemanticTokenModifiers) {
		if startPos < file.Pos(0) || endPos <= startPos || endPos > file.Pos(file.Size()) {
			return
		}

		tokenInfos = append(tokenInfos, semanticTokenInfo{
			startPos:       startPos,
			endPos:         endPos,
			tokenType:      tokenType,
			tokenModifiers: tokenModifiers,
		})
	}
	var syntaxExpressions []ast.Expr
	addStringToken := func(node *ast.BasicLit) {
		endPos := basicLitEnd(fset, astFile, node)
		if node.Extra == nil || len(node.Extra.Parts) == 0 {
			addToken(node.Pos(), endPos, StringType, nil)
			return
		}

		stringStart := node.ValuePos
		for _, part := range node.Extra.Parts {
			if v, ok := part.(ast.Expr); ok {
				syntaxExpressions = append(syntaxExpressions, v)
				if stringStart < v.Pos() {
					addToken(stringStart, v.Pos(), StringType, nil)
				}
				if v.End() < endPos {
					addToken(v.End(), v.End()+1, StringType, nil)
				}
				stringStart = v.End() + 1
			}
		}
		if stringStart < endPos {
			addToken(stringStart, endPos, StringType, nil)
		}
	}
	implicitReceivers := make(map[*ast.FieldList]bool)
	operatorDeclarations := make(map[token.Pos]bool)
	declarationTypes := make(map[*ast.Ident]SemanticTokenTypes)
	seenNodes := make(map[ast.Node]bool)
	ast.Inspect(astFile, func(node ast.Node) bool {
		// Lowering can reuse whole source expressions, including their literals.
		if node == nil || seenNodes[node] {
			return false
		}
		seenNodes[node] = true
		if !node.Pos().IsValid() {
			return true
		}

		switch node := node.(type) {
		case *ast.Ident:
			if !xgoutil.IsSourceIdent(file, astFile.Code, node) {
				return false
			}
			if tokenType, ok := declarationTypes[node]; ok {
				if tokenType == OperatorType {
					operatorDeclarations[node.Pos()] = true
				} else {
					addToken(node.Pos(), node.End(), tokenType, []SemanticTokenModifiers{ModDeclaration})
				}
				return true
			}
			if enums.declarationType(node) != nil {
				addToken(node.Pos(), node.End(), EnumType, []SemanticTokenModifiers{ModDeclaration})
				return true
			}
			if enums.declarationMember(node) != nil {
				addToken(
					node.Pos(),
					node.End(),
					EnumMemberType,
					[]SemanticTokenModifiers{ModDeclaration, ModStatic, ModReadonly},
				)
				return true
			}
			obj := enums.objectForIdent(typeInfo, node)
			if obj == nil {
				return true
			}

			var (
				tokenType SemanticTokenTypes
				modifiers []SemanticTokenModifiers
				isDef     = typeInfo.ObjToDef[obj] == node || enums.isRegularConstDeclaration(node)
			)
			switch obj := obj.(type) {
			case *gotypes.Builtin:
				tokenType = KeywordType
				modifiers = append(modifiers, ModDefaultLibrary)
			case *gotypes.TypeName:
				if enums.typeFor(obj.Type()) != nil {
					tokenType = EnumType
					break
				}
				if named := resolvedNamedType(obj.Type()); named != nil {
					switch named.Underlying().(type) {
					case *gotypes.Struct:
						tokenType = StructType
					case *gotypes.Interface:
						tokenType = InterfaceType
					default:
						tokenType = TypeType
					}
				} else {
					tokenType = TypeType
				}
			case *gotypes.Var:
				tokenType = VariableType
				switch obj.Kind() {
				case gotypes.FieldVar:
					if xgoutil.IsInMainPkg(obj) &&
						xgoutil.IsDefinedInClassFieldsDecl(fset, typeInfo, astPkg, obj) {
						break
					}
					tokenType = PropertyType
				case gotypes.ParamVar:
					if isDef {
						tokenType = ParameterType
					}
				}
			case *gotypes.Const:
				if len(enums.membersForIdent(proj, typeInfo, node)) > 0 {
					tokenType = EnumMemberType
				} else {
					tokenType = VariableType
				}
				modifiers = append(modifiers, ModStatic, ModReadonly)
			case *gotypes.Func:
				if obj.Signature().Recv() != nil {
					tokenType = MethodType
				} else {
					tokenType = FunctionType
				}
			case *gotypes.PkgName:
				tokenType = NamespaceType
			case *gotypes.Label:
				tokenType = LabelType
			}
			if isDef {
				modifiers = append(modifiers, ModDeclaration)
			}
			if obj.Pkg() != nil && !xgoutil.IsInMainPkg(obj) && !strings.Contains(xgoutil.PkgPath(obj.Pkg()), ".") {
				modifiers = append(modifiers, ModDefaultLibrary)
			}
			addToken(node.Pos(), node.End(), tokenType, modifiers)
		case *ast.BasicLit:
			var tokenType SemanticTokenTypes
			switch node.Kind {
			case token.STRING, token.CHAR, token.CSTRING:
				tokenType = StringType
			case token.INT, token.FLOAT, token.IMAG, token.RAT:
				tokenType = NumberType
			}
			if tokenType == StringType {
				addStringToken(node)
			} else {
				addToken(node.Pos(), node.End(), tokenType, nil)
			}
		case *ast.NumberUnitLit:
			if isXGoUnitNumberKind(node.Kind) {
				addToken(node.ValuePos, node.ValuePos+token.Pos(len(node.Value)), NumberType, nil)
			}
		case *ast.CallExpr:
			for _, kwarg := range node.Kwargs {
				if len(lookupCallExprKwargTargets(typeInfo, node, kwarg.Name.Name)) > 0 {
					addToken(kwarg.Name.Pos(), kwarg.Name.End(), PropertyType, nil)
				}
			}
		case *ast.FuncDecl:
			if node.IsClass {
				implicitReceivers[node.Recv] = true
			}
			if node.Operator {
				declarationTypes[node.Name] = OperatorType
			}
		case *ast.OverloadFuncDecl:
			if node.IsClass {
				implicitReceivers[node.Recv] = true
			}
			if node.Operator {
				declarationTypes[node.Name] = OperatorType
			} else if node.Recv != nil {
				declarationTypes[node.Name] = MethodType
			} else {
				declarationTypes[node.Name] = FunctionType
			}
		case *ast.FieldList:
			if implicitReceivers[node] {
				return false
			}
		case *ast.LabeledStmt:
			declarationTypes[node.Label] = LabelType
		}
		return true
	})

	// Scan physical syntax separately from compiler-mutated ASTs. Interpolated
	// expressions need their own scan because the lexer sees their outer string
	// as a single literal. Each scan owns its line table.
	addSyntaxTokens := func(start, end token.Pos) {
		end = min(end, file.Pos(file.Size()))
		if start < file.Pos(0) || end <= start {
			return
		}
		source := astFile.Code[file.Offset(start):file.Offset(end)]
		var scan scanner.Scanner
		scan.Init(token.NewFileSet().AddFile("", int(start), len(source)), source, nil, scanner.ScanComments)
		for {
			pos, tok, lit := scan.Scan()
			if tok == token.EOF {
				break
			}
			switch {
			case tok == token.COMMENT:
				// The scanner strips carriage returns from comment text.
				// Recover its physical end directly from the source delimiters.
				comment := source[int(pos-start):]
				length := len(comment)
				if bytes.HasPrefix(comment, []byte("/*")) {
					if closing := bytes.Index(comment[2:], []byte("*/")); closing >= 0 {
						length = closing + 4
					}
				} else if newline := bytes.IndexByte(comment, '\n'); newline >= 0 {
					length = newline
				}
				addToken(pos, pos+token.Pos(length), CommentType, nil)
			case tok.IsKeyword():
				addToken(pos, pos+token.Pos(len(tok.String())), KeywordType, nil)
			case tok.IsOperator() && lit != "\n":
				var modifiers []SemanticTokenModifiers
				if operatorDeclarations[pos] {
					modifiers = []SemanticTokenModifiers{ModDeclaration}
				}
				addToken(pos, pos+token.Pos(len(tok.String())), OperatorType, modifiers)
			}
		}
	}
	addSyntaxTokens(file.Pos(0), file.Pos(file.Size()))
	for _, expr := range syntaxExpressions {
		addSyntaxTokens(expr.Pos(), expr.End())
	}

	type semanticTokenDataSegment struct {
		semanticTokenSegment
		typeIndex     uint32
		modifiersMask uint32
	}

	var (
		lineLengths = semanticTokenLineLengths(astFile.Code)
		segments    = make([]semanticTokenDataSegment, 0, len(tokenInfos))
	)
	for _, info := range tokenInfos {
		start := fset.PositionFor(info.startPos, false)
		end := fset.PositionFor(info.endPos, false)

		typeIndex := getSemanticTokenTypeIndex(info.tokenType)
		modifiersMask := getSemanticTokenModifiersMask(info.tokenModifiers)
		for _, segment := range semanticTokenSegments(
			FromPosition(proj, astFile, start),
			FromPosition(proj, astFile, end),
			lineLengths,
		) {
			segments = append(segments, semanticTokenDataSegment{
				semanticTokenSegment: segment,
				typeIndex:            typeIndex,
				modifiersMask:        modifiersMask,
			})
		}
	}

	slices.SortStableFunc(segments, func(a, b semanticTokenDataSegment) int {
		if a.line != b.line {
			return cmp.Compare(a.line, b.line)
		}
		return cmp.Compare(a.char, b.char)
	})

	var (
		tokensData         = make([]uint32, 0, len(segments)*5)
		prevLine, prevChar uint32
	)
	for _, segment := range segments {
		lineDelta := segment.line - prevLine
		charDelta := segment.char
		if lineDelta == 0 {
			charDelta -= prevChar
		}
		tokensData = append(tokensData, lineDelta, charDelta, segment.length, segment.typeIndex, segment.modifiersMask)

		prevLine = segment.line
		prevChar = segment.char
	}
	return &SemanticTokens{
		Data: tokensData,
	}, nil
}

// semanticTokenSegment represents a single-line semantic token span encoded
// with LSP UTF-16 positions.
type semanticTokenSegment struct {
	line   uint32
	char   uint32
	length uint32
}

// semanticTokenLineLengths returns the UTF-16 lengths of all source lines.
func semanticTokenLineLengths(content []byte) []uint32 {
	lines := bytes.Split(content, []byte("\n"))
	lengths := make([]uint32, len(lines))
	for i, line := range lines {
		line = trimLineEnding(line)
		lengths[i] = uint32(UTF16Len(string(line)))
	}
	return lengths
}

// semanticTokenSegments splits a semantic token range into single-line
// segments encoded with LSP UTF-16 positions. Empty ranges emit no token.
func semanticTokenSegments(start, end Position, lineLengths []uint32) []semanticTokenSegment {
	if comparePositions(start, end) >= 0 {
		return nil
	}
	if start.Line == end.Line {
		return []semanticTokenSegment{{
			line:   start.Line,
			char:   start.Character,
			length: end.Character - start.Character,
		}}
	}

	segments := make([]semanticTokenSegment, 0, end.Line-start.Line+1)
	if startLineLength := semanticTokenLineLength(lineLengths, start.Line); start.Character < startLineLength {
		segments = append(segments, semanticTokenSegment{
			line:   start.Line,
			char:   start.Character,
			length: startLineLength - start.Character,
		})
	}
	for line := start.Line + 1; line < end.Line; line++ {
		length := semanticTokenLineLength(lineLengths, line)
		if length == 0 {
			continue
		}
		segments = append(segments, semanticTokenSegment{
			line:   line,
			length: length,
		})
	}
	if end.Character > 0 {
		segments = append(segments, semanticTokenSegment{
			line:   end.Line,
			length: end.Character,
		})
	}
	return segments
}

// semanticTokenLineLength returns the UTF-16 length of the given line.
func semanticTokenLineLength(lineLengths []uint32, line uint32) uint32 {
	if int(line) >= len(lineLengths) {
		return 0
	}
	return lineLengths[line]
}
