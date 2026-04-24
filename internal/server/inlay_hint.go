package server

import (
	"cmp"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_inlayHint
func (s *Server) textDocumentInlayHint(params *InlayHintParams) ([]InlayHint, error) {
	result, _, astFile, err := s.compileAndGetASTFileForDocumentURI(params.TextDocument.URI)
	if err != nil {
		return nil, err
	}
	if astFile == nil {
		return nil, nil
	}
	if !astFile.Pos().IsValid() {
		return nil, nil
	}

	rangeStart := PosAt(result.proj, astFile, params.Range.Start)
	rangeEnd := PosAt(result.proj, astFile, params.Range.End)
	return collectInlayHints(result, astFile, rangeStart, rangeEnd), nil
}

// collectInlayHints collects inlay hints from the given AST file. If
// rangeStart and rangeEnd positions are provided (non-zero), only hints within
// the range are included.
func collectInlayHints(result *compileResult, astFile *ast.File, rangeStart, rangeEnd token.Pos) []InlayHint {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}

	var inlayHints []InlayHint
	ast.Inspect(astFile, func(node ast.Node) bool {
		if node == nil || !node.Pos().IsValid() || !node.End().IsValid() {
			return true
		}

		if rangeStart.IsValid() && node.End() < rangeStart {
			return false
		}
		if rangeEnd.IsValid() && node.Pos() > rangeEnd {
			return false
		}

		switch node := node.(type) {
		case *ast.BranchStmt:
			if callExpr := xgoutil.CreateCallExprFromBranchStmt(typeInfo, node); callExpr != nil {
				hints := collectInlayHintsFromCallExpr(result, callExpr)
				inlayHints = append(inlayHints, hints...)
			}
		case *ast.CallExpr:
			hints := collectInlayHintsFromCallExpr(result, node)
			inlayHints = append(inlayHints, hints...)
		}
		return true
	})
	sortInlayHints(inlayHints)
	return inlayHints
}

// collectInlayHintsFromCallExpr collects inlay hints from a call expression.
func collectInlayHintsFromCallExpr(result *compileResult, callExpr *ast.CallExpr) []InlayHint {
	astPkg, _ := result.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(result.proj.Fset, astPkg, callExpr)
	if astFile == nil {
		return nil
	}
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	_, _, resolvedParams := xgoutil.ResolveCallExprSignature(typeInfo, callExpr)
	hasResolvedSignature := resolvedParams != nil

	var inlayHints []InlayHint
	variadicParamSeen := false
	for resolvedArg := range resolvedCallExprArgs(result.proj, typeInfo, callExpr) {
		if resolvedArg.Kind != xgoutil.ResolvedCallExprArgPositional {
			continue
		}
		variadicArg := resolvedArg.Fun.Signature().Variadic() && resolvedArg.ParamIndex == resolvedArg.Params.Len()-1
		if variadicArg {
			if variadicParamSeen {
				break
			}
			variadicParamSeen = true
		}

		switch resolvedArg.Arg.(type) {
		case *ast.LambdaExpr, *ast.LambdaExpr2:
			// Skip lambda expressions.
			continue
		}
		if !hasResolvedSignature && !xgoutil.IsValidType(typeInfo.TypeOf(resolvedArg.Arg)) {
			continue
		}

		// Create an inlay hint with the parameter name before the argument.
		position := result.proj.Fset.Position(resolvedArg.Arg.Pos())
		label := xgoutil.SourceParamName(resolvedArg.Param)
		if variadicArg {
			label += "..."
		}
		hint := InlayHint{
			Position: FromPosition(result.proj, astFile, position),
			Label:    label,
			Kind:     Parameter,
		}
		inlayHints = append(inlayHints, hint)
	}
	return inlayHints
}

// sortInlayHints sorts the given inlay hints in a stable manner.
func sortInlayHints(hints []InlayHint) {
	slices.SortFunc(hints, func(a, b InlayHint) int {
		// First sort by line number.
		if a.Position.Line != b.Position.Line {
			return cmp.Compare(a.Position.Line, b.Position.Line)
		}
		// If same line, sort by character position.
		if a.Position.Character != b.Position.Character {
			return cmp.Compare(a.Position.Character, b.Position.Character)
		}
		// If same position (unlikely), sort by label for stability.
		return cmp.Compare(a.Label, b.Label)
	})
}
