package server

import (
	gotypes "go/types"
	"slices"
	"sort"
	"strings"

	"github.com/goplus/xgo/ast"
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

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_semanticTokens
func (s *Server) textDocumentSemanticTokensFull(params *SemanticTokensParams) (*SemanticTokens, error) {
	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if astFile == nil {
		return nil, nil
	}
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}

	fset := result.proj.Fset
	var tokenInfos []semanticTokenInfo
	addToken := func(startPos, endPos token.Pos, tokenType SemanticTokenTypes, tokenModifiers []SemanticTokenModifiers) {
		if !startPos.IsValid() || !endPos.IsValid() {
			return
		}

		start := fset.Position(startPos)
		end := fset.Position(endPos)
		if start.Line <= 0 || start.Column <= 0 || end.Offset <= start.Offset {
			return
		}

		tokenInfos = append(tokenInfos, semanticTokenInfo{
			startPos:       startPos,
			endPos:         endPos,
			tokenType:      tokenType,
			tokenModifiers: tokenModifiers,
		})
	}

	ast.Inspect(astFile, func(node ast.Node) bool {
		if node == nil || !node.Pos().IsValid() {
			return true
		}

		switch node := node.(type) {
		case *ast.Comment:
			addToken(node.Pos(), node.End(), CommentType, nil)
		case *ast.BadExpr:
			addToken(node.From, node.To, OperatorType, nil)
		case *ast.BadStmt:
			addToken(node.From, node.To, OperatorType, nil)
		case *ast.EmptyStmt:
			if !node.Implicit {
				addToken(node.Semicolon, node.Semicolon+1, OperatorType, nil)
			}
		case *ast.Ident:
			obj := typeInfo.ObjectOf(node)
			if obj == nil {
				if token.Lookup(node.Name).IsKeyword() {
					addToken(node.Pos(), node.End(), KeywordType, nil)
				}
				return true
			}

			var (
				tokenType SemanticTokenTypes
				modifiers []SemanticTokenModifiers
			)
			switch obj := obj.(type) {
			case *gotypes.Builtin:
				tokenType = KeywordType
				modifiers = append(modifiers, ModDefaultLibrary)
			case *gotypes.TypeName:
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
				switch obj.Kind() {
				case gotypes.FieldVar:
					typeInfo, _ := result.proj.TypeInfo()
					astPkg, _ := result.proj.ASTPackage()
					if xgoutil.IsInMainPkg(obj) && xgoutil.IsDefinedInClassFieldsDecl(result.proj.Fset, typeInfo, astPkg, obj) {
						tokenType = VariableType
					} else {
						tokenType = PropertyType
					}
				case gotypes.PackageVar:
					defIdent := typeInfo.ObjToDef[obj]
					if defIdent == node {
						tokenType = ParameterType
					} else {
						tokenType = VariableType
					}
				default:
					tokenType = VariableType
				}
			case *gotypes.Const:
				tokenType = VariableType
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
			if typeInfo.ObjToDef[obj] == node {
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
			addToken(node.ValuePos, node.ValuePos+token.Pos(len(node.Value)), tokenType, nil)

			if node.Extra != nil && len(node.Extra.Parts) > 0 {
				pos := node.ValuePos
				for _, part := range node.Extra.Parts {
					switch v := part.(type) {
					case string:
						nextPos := ast.NextPartPos(pos, v)
						addToken(pos, nextPos, StringType, nil)
						pos = nextPos
					case ast.Expr:
						pos = v.End()
					}
				}
			}
		case *ast.CompositeLit:
			addToken(node.Lbrace, node.Lbrace+1, OperatorType, nil)
			addToken(node.Rbrace, node.Rbrace+1, OperatorType, nil)
		case *ast.FuncLit:
			addToken(node.Type.Func, node.Type.Func+token.Pos(len("func")), KeywordType, nil)
		case *ast.SliceLit:
			addToken(node.Lbrack, node.Lbrack+1, OperatorType, nil)
			addToken(node.Rbrack, node.Rbrack+1, OperatorType, nil)
		case *ast.MatrixLit:
			addToken(node.Lbrack, node.Lbrack+1, OperatorType, nil)
			addToken(node.Rbrack, node.Rbrack+1, OperatorType, nil)
		case *ast.StarExpr:
			addToken(node.Star, node.Star+1, OperatorType, nil)
		case *ast.UnaryExpr:
			opLen := len(node.Op.String())
			addToken(node.OpPos, node.OpPos+token.Pos(opLen), OperatorType, nil)
		case *ast.BinaryExpr:
			opLen := len(node.Op.String())
			addToken(node.OpPos, node.OpPos+token.Pos(opLen), OperatorType, nil)
		case *ast.ParenExpr:
			addToken(node.Lparen, node.Lparen+1, OperatorType, nil)
			addToken(node.Rparen, node.Rparen+1, OperatorType, nil)
		case *ast.SelectorExpr:
			addToken(node.Sel.Pos()-1, node.Sel.Pos(), OperatorType, nil)
		case *ast.IndexExpr:
			addToken(node.Lbrack, node.Lbrack+1, OperatorType, nil)
			addToken(node.Rbrack, node.Rbrack+1, OperatorType, nil)
		case *ast.IndexListExpr:
			addToken(node.Lbrack, node.Lbrack+1, OperatorType, nil)
			addToken(node.Rbrack, node.Rbrack+1, OperatorType, nil)
		case *ast.SliceExpr:
			addToken(node.Lbrack, node.Lbrack+1, OperatorType, nil)
			addToken(node.Rbrack, node.Rbrack+1, OperatorType, nil)
		case *ast.TypeAssertExpr:
			addToken(node.Lparen-1, node.Lparen, OperatorType, nil)
			addToken(node.Lparen, node.Lparen+1, OperatorType, nil)
			if node.Type == nil {
				addToken(node.Lparen+1, node.Lparen+1+token.Pos(len("type")), KeywordType, nil)
			}
			addToken(node.Rparen, node.Rparen+1, OperatorType, nil)
		case *ast.CallExpr:
			addToken(node.Lparen, node.Lparen+1, OperatorType, nil)
			addToken(node.Rparen, node.Rparen+1, OperatorType, nil)
			if node.Ellipsis.IsValid() {
				addToken(node.Ellipsis, node.Ellipsis+3, OperatorType, nil)
			}
		case *ast.KeyValueExpr:
			addToken(node.Colon, node.Colon+1, OperatorType, nil)
		case *ast.ErrWrapExpr:
			addToken(node.TokPos, node.TokPos+1, OperatorType, nil)
			if node.Default != nil {
				addToken(node.TokPos+1, node.TokPos+2, OperatorType, nil)
			}
		case *ast.EnvExpr:
			addToken(node.TokPos, node.TokPos+1, OperatorType, nil)
			if node.HasBrace() {
				addToken(node.Lbrace, node.Lbrace+1, OperatorType, nil)
				addToken(node.Rbrace, node.Rbrace+1, OperatorType, nil)
			}
		case *ast.RangeExpr:
			addToken(node.To, node.To+1, OperatorType, nil)
			if node.Colon2.IsValid() {
				addToken(node.Colon2, node.Colon2+1, OperatorType, nil)
			}
		case *ast.ArrayType:
			addToken(node.Lbrack, node.Lbrack+1, OperatorType, nil)
			if node.Len == nil {
				addToken(node.Lbrack+1, node.Lbrack+2, OperatorType, nil)
			}
		case *ast.StructType:
			addToken(node.Struct, node.Struct+token.Pos(len("struct")), KeywordType, nil)
		case *ast.InterfaceType:
			addToken(node.Interface, node.Interface+token.Pos(len("interface")), KeywordType, nil)
		case *ast.FuncType:
			if node.Func.IsValid() {
				addToken(node.Func, node.Func+token.Pos(len("func")), KeywordType, nil)
			}
			if node.TypeParams != nil {
				addToken(node.TypeParams.Opening, node.TypeParams.Opening+1, OperatorType, nil)
				addToken(node.TypeParams.Closing, node.TypeParams.Closing+1, OperatorType, nil)
			}
		case *ast.MapType:
			addToken(node.Map, node.Map+token.Pos(len("map")), KeywordType, nil)
		case *ast.ChanType:
			addToken(node.Begin, node.Begin+token.Pos(len("chan")), KeywordType, nil)
			if node.Arrow.IsValid() {
				addToken(node.Arrow, node.Arrow+2, OperatorType, nil)
			}
		case *ast.GenDecl:
			switch node.Tok {
			case token.IMPORT:
				addToken(node.TokPos, node.TokPos+token.Pos(len("import")), KeywordType, nil)
			case token.CONST:
				addToken(node.TokPos, node.TokPos+token.Pos(len("const")), KeywordType, nil)
			case token.TYPE:
				addToken(node.TokPos, node.TokPos+token.Pos(len("type")), KeywordType, nil)
			case token.VAR:
				addToken(node.TokPos, node.TokPos+token.Pos(len("var")), KeywordType, nil)
			}
			if node.Lparen.IsValid() {
				addToken(node.Lparen, node.Lparen+1, OperatorType, nil)
			}
			if node.Rparen.IsValid() {
				addToken(node.Rparen, node.Rparen+1, OperatorType, nil)
			}
		case *ast.FuncDecl:
			if node.Shadow {
				return true
			}

			addToken(node.Type.Func, node.Type.Func+token.Pos(len("func")), KeywordType, nil)
			if node.Recv != nil {
				addToken(node.Recv.Opening, node.Recv.Opening+1, OperatorType, nil)
				addToken(node.Recv.Closing, node.Recv.Closing+1, OperatorType, nil)
			}
			if node.Operator {
				addToken(node.Name.Pos(), node.Name.End(), OperatorType, []SemanticTokenModifiers{ModDeclaration})
			}
		case *ast.OverloadFuncDecl:
			addToken(node.Func, node.Func+token.Pos(len("func")), KeywordType, nil)
			if node.Recv != nil {
				addToken(node.Recv.Opening, node.Recv.Opening+1, OperatorType, nil)
				addToken(node.Recv.Closing, node.Recv.Closing+1, OperatorType, nil)
			}
			if node.Operator {
				addToken(node.Name.Pos(), node.Name.End(), OperatorType, []SemanticTokenModifiers{ModDeclaration})
			} else {
				var tokenType SemanticTokenTypes
				if node.Recv != nil {
					tokenType = MethodType
				} else {
					tokenType = FunctionType
				}
				addToken(node.Name.Pos(), node.Name.End(), tokenType, []SemanticTokenModifiers{ModDeclaration})
			}
			addToken(node.Assign, node.Assign+1, OperatorType, nil)
			addToken(node.Lparen, node.Lparen+1, OperatorType, nil)
			addToken(node.Rparen, node.Rparen+1, OperatorType, nil)
		case *ast.ImportSpec:
			if node.Path != nil {
				addToken(node.Path.Pos(), node.Path.End(), StringType, nil)
			}
		case *ast.ValueSpec:
			if node.Type != nil {
				addToken(node.Type.Pos(), node.Type.End(), TypeType, nil)
			}
		case *ast.FieldList:
			if node.Opening.IsValid() {
				addToken(node.Opening, node.Opening+1, OperatorType, nil)
			}
			if node.Closing.IsValid() {
				addToken(node.Closing, node.Closing+1, OperatorType, nil)
			}
		case *ast.LabeledStmt:
			addToken(node.Label.Pos(), node.Label.End(), LabelType, nil)
			addToken(node.Colon, node.Colon+1, OperatorType, nil)
		case *ast.SendStmt:
			addToken(node.Arrow, node.Arrow+2, OperatorType, nil)
		case *ast.IncDecStmt:
			addToken(node.TokPos, node.TokPos+2, OperatorType, nil)
		case *ast.AssignStmt:
			opLen := len(node.Tok.String())
			addToken(node.TokPos, node.TokPos+token.Pos(opLen), OperatorType, nil)
		case *ast.GoStmt:
			addToken(node.Go, node.Go+token.Pos(len("go")), KeywordType, nil)
		case *ast.DeferStmt:
			addToken(node.Defer, node.Defer+token.Pos(len("defer")), KeywordType, nil)
		case *ast.ReturnStmt:
			addToken(node.Return, node.Return+token.Pos(len("return")), KeywordType, nil)
		case *ast.BranchStmt:
			opLen := len(node.Tok.String())
			addToken(node.TokPos, node.TokPos+token.Pos(opLen), KeywordType, nil)
		case *ast.BlockStmt:
			addToken(node.Lbrace, node.Lbrace+1, OperatorType, nil)
			addToken(node.Rbrace, node.Rbrace+1, OperatorType, nil)
		case *ast.IfStmt:
			addToken(node.If, node.If+token.Pos(len("if")), KeywordType, nil)
		case *ast.CaseClause:
			if node.List == nil {
				addToken(node.Case, node.Case+token.Pos(len("default")), KeywordType, nil)
			} else {
				addToken(node.Case, node.Case+token.Pos(len("case")), KeywordType, nil)
			}
		case *ast.SwitchStmt:
			addToken(node.Switch, node.Switch+token.Pos(len("switch")), KeywordType, nil)
		case *ast.TypeSwitchStmt:
			addToken(node.Switch, node.Switch+token.Pos(len("switch")), KeywordType, nil)
		case *ast.CommClause:
			if node.Comm == nil {
				addToken(node.Case, node.Case+token.Pos(len("default")), KeywordType, nil)
			} else {
				addToken(node.Case, node.Case+token.Pos(len("case")), KeywordType, nil)
			}
			addToken(node.Colon, node.Colon+1, OperatorType, nil)
		case *ast.SelectStmt:
			addToken(node.Select, node.Select+token.Pos(len("select")), KeywordType, nil)
		case *ast.ForStmt:
			addToken(node.For, node.For+token.Pos(len("for")), KeywordType, nil)
		case *ast.RangeStmt:
			addToken(node.For, node.For+token.Pos(len("for")), KeywordType, nil)
			if !node.NoRangeOp {
				addToken(node.For+token.Pos(len("for")+1), node.For+token.Pos(len("for range")), KeywordType, nil)
			}
			if node.Tok != token.ILLEGAL {
				addToken(node.TokPos, node.TokPos+token.Pos(len(node.Tok.String())), OperatorType, nil)
			}
		case *ast.LambdaExpr:
			addToken(node.Rarrow, node.Rarrow+2, OperatorType, nil)
			if node.LhsHasParen {
				addToken(node.First, node.First+1, OperatorType, nil)
				addToken(node.Rarrow-1, node.Rarrow, OperatorType, nil)
			}
			if node.RhsHasParen {
				addToken(node.Rarrow+2, node.Rarrow+3, OperatorType, nil)
				addToken(node.Last-1, node.Last, OperatorType, nil)
			}
		case *ast.LambdaExpr2:
			addToken(node.Rarrow, node.Rarrow+2, OperatorType, nil)
			if node.LhsHasParen {
				addToken(node.First, node.First+1, OperatorType, nil)
				addToken(node.Rarrow-1, node.Rarrow, OperatorType, nil)
			}
		case *ast.ForPhrase:
			addToken(node.For, node.For+token.Pos(len("for")), KeywordType, nil)
			addToken(node.TokPos, node.TokPos+2, OperatorType, nil)
			if node.IfPos.IsValid() {
				addToken(node.IfPos, node.IfPos+token.Pos(len("if")), KeywordType, nil)
			}
		case *ast.ForPhraseStmt:
			addToken(node.For, node.For+token.Pos(len("for")), KeywordType, nil)
			addToken(node.TokPos, node.TokPos+2, OperatorType, nil)
			if node.IfPos.IsValid() {
				addToken(node.IfPos, node.IfPos+token.Pos(len("if")), KeywordType, nil)
			}
			if node.Body != nil {
				addToken(node.Body.Lbrace, node.Body.Lbrace+1, OperatorType, nil)
				addToken(node.Body.Rbrace, node.Body.Rbrace+1, OperatorType, nil)
			}
		case *ast.ComprehensionExpr:
			addToken(node.Lpos, node.Lpos+1, OperatorType, nil)
			addToken(node.Rpos, node.Rpos+1, OperatorType, nil)
			if kvExpr, ok := node.Elt.(*ast.KeyValueExpr); ok {
				addToken(kvExpr.Colon, kvExpr.Colon+1, OperatorType, nil)
			}
		case *ast.Ellipsis:
			addToken(node.Ellipsis, node.Ellipsis+3, OperatorType, nil)
		case *ast.ElemEllipsis:
			addToken(node.Ellipsis, node.Ellipsis+3, OperatorType, nil)
		}
		return true
	})

	sort.Slice(tokenInfos, func(i, j int) bool {
		if tokenInfos[i].startPos != tokenInfos[j].startPos {
			return tokenInfos[i].startPos < tokenInfos[j].startPos
		}
		return tokenInfos[i].endPos < tokenInfos[j].endPos
	})

	var (
		tokensData         = make([]uint32, 0, len(tokenInfos))
		prevLine, prevChar uint32
	)
	for _, info := range tokenInfos {
		start := fset.Position(info.startPos)
		end := fset.Position(info.endPos)

		line := uint32(start.Line - 1)
		char := uint32(start.Column - 1)
		length := uint32(end.Offset - start.Offset)
		if line < prevLine || (line == prevLine && char < prevChar) {
			continue
		}

		typeIndex := getSemanticTokenTypeIndex(info.tokenType)
		modifiersMask := getSemanticTokenModifiersMask(info.tokenModifiers)

		if line == prevLine {
			tokensData = append(tokensData, 0, char-prevChar, length, typeIndex, modifiersMask)
		} else {
			tokensData = append(tokensData, line-prevLine, char, length, typeIndex, modifiersMask)
		}

		prevLine = line
		prevChar = char
	}
	return &SemanticTokens{
		Data: tokensData,
	}, nil
}
