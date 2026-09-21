package server

import (
	"cmp"
	"fmt"
	gotypes "go/types"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification/#textDocument_inlayHint
func (s *Server) textDocumentInlayHint(params *InlayHintParams) ([]InlayHint, error) {
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

	rangeStart := PosAt(proj, astFile, params.Range.Start)
	rangeEnd := PosAt(proj, astFile, params.Range.End)
	return collectInlayHints(proj, astFile, rangeStart, rangeEnd), nil
}

// collectInlayHints collects inlay hints from the given AST file. Valid bounds
// restrict hint positions to [rangeStart, rangeEnd). A zero bound leaves that
// end unrestricted.
func collectInlayHints(proj *xgo.Project, astFile *ast.File, rangeStart, rangeEnd token.Pos) []InlayHint {
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}

	var inlayHints []InlayHint
	// The compiler can reuse source expressions in generated statements.
	seenNodes := make(map[ast.Node]bool)
	ast.Inspect(astFile, func(node ast.Node) bool {
		if node == nil || seenNodes[node] {
			return false
		}
		seenNodes[node] = true
		if !node.Pos().IsValid() || !node.End().IsValid() {
			return true
		}

		if rangeStart.IsValid() && node.End() < rangeStart {
			return false
		}
		if rangeEnd.IsValid() && node.Pos() > rangeEnd {
			return false
		}

		if callExpr := callExprFromNode(typeInfo, node); callExpr != nil {
			hints := collectInlayHintsFromCallExpr(proj, callExpr)
			inlayHints = append(inlayHints, hints...)
		}
		return true
	})
	inlayHints = slices.DeleteFunc(inlayHints, func(hint InlayHint) bool {
		pos := PosAt(proj, astFile, hint.Position)
		return rangeStart.IsValid() && pos < rangeStart || rangeEnd.IsValid() && pos >= rangeEnd
	})
	sortInlayHints(inlayHints)
	return inlayHints
}

// collectInlayHintsFromCallExpr collects inlay hints from a call expression.
func collectInlayHintsFromCallExpr(proj *xgo.Project, callExpr *ast.CallExpr) []InlayHint {
	astPkg, _ := proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(proj.Fset, astPkg, callExpr)
	if astFile == nil {
		return nil
	}
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	_, sig, _ := xgoutil.ResolveCallExprSignature(typeInfo, callExpr)
	hasResolvedSignature := sig != nil

	var inlayHints []InlayHint
	labelsByPosition := make(map[Position]string)
	ambiguousPositions := make(map[Position]struct{})
	seenVariadicParams := make(map[*gotypes.Var]bool)
	for resolvedArg := range resolvedCallExprArgs(typeInfo, callExpr) {
		if resolvedArg.Kind != xgoutil.ResolvedCallExprArgPositional {
			continue
		}
		variadicArg := resolvedArg.Signature.Variadic() && resolvedArg.ParamIndex == resolvedArg.Params.Len()-1
		if variadicArg {
			if seenVariadicParams[resolvedArg.Param] {
				continue
			}
			seenVariadicParams[resolvedArg.Param] = true
		}

		switch resolvedArg.Arg.(type) {
		case *ast.ArrowExpr, *ast.LambdaExpr:
			// Skip lambda expressions.
			continue
		}
		if !hasResolvedSignature && !xgoutil.IsValidType(typeInfo.TypeOf(resolvedArg.Arg)) {
			continue
		}

		// Create an inlay hint with the parameter name before the argument.
		position := proj.Fset.PositionFor(resolvedArg.Arg.Pos(), false)
		label := xgoutil.SourceParamName(resolvedArg.Param)
		if label == "" || label == "_" {
			continue
		}
		if variadicArg {
			label += "..."
		}
		hint := InlayHint{
			Position: FromPosition(proj, astFile, position),
			Label:    label,
			Kind:     Parameter,
		}
		if _, ok := xgoutil.AutoclosureParamResultType(resolvedArg.Param); ok {
			hint.Tooltip = &InlayHintTooltip{Value: autoclosureParamDocumentation}
		}
		if existingLabel, ok := labelsByPosition[hint.Position]; ok {
			if existingLabel != hint.Label {
				ambiguousPositions[hint.Position] = struct{}{}
			}
			continue
		}
		labelsByPosition[hint.Position] = hint.Label
		inlayHints = append(inlayHints, hint)
	}
	return slices.DeleteFunc(inlayHints, func(hint InlayHint) bool {
		_, ok := ambiguousPositions[hint.Position]
		return ok
	})
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
