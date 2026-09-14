package server

import (
	"bytes"
	"strconv"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
)

// setCompletionStringValue configures a literal string candidate, replacing the
// enclosing literal when present so escapes and suffixes are replaced together.
// It reports whether the replacement can use non-overlapping LSP edits.
func (ctx *completionContext) setCompletionStringValue(item *CompletionItem, value string) bool {
	quoted := strconv.Quote(value)
	item.Label = quoted
	// Dollar signs introduce interpolation and dollar escapes in XGo strings.
	quoted = strings.ReplaceAll(quoted, "$", `\x24`)
	item.InsertText = quoted
	item.InsertTextFormat = ToPtr(PlainTextTextFormat)
	if !ctx.inStringLit {
		return true
	}
	item.Label = value
	item.InsertText = quoted[1 : len(quoted)-1]
	lit := ctx.stringLit
	// Match the source prefix, including the delimiter covered by the edit.
	delimiter := string(lit.Value[0])
	item.FilterText = delimiter + value + delimiter
	if delimiter == `"` {
		typed := string(ctx.astFile.Code[ctx.tokenFile.Offset(lit.Pos()):ctx.tokenFile.Offset(ctx.sourcePos)])
		if prefix, err := strconv.Unquote(typed + delimiter); err == nil && strings.HasPrefix(value, prefix) {
			item.FilterText = typed + value[len(prefix):] + delimiter
		}
	}
	if delimiter == "`" && strconv.CanBackquote(value) && !strings.ContainsRune(value, '$') {
		quoted = "`" + value + "`"
		item.InsertText = value
	}
	end := basicLitEnd(ctx.proj.Fset, ctx.astFile, lit)
	startPosition := FromPosition(ctx.proj, ctx.astFile, ctx.tokenFile.PositionFor(lit.Pos(), false))
	endPosition := FromPosition(ctx.proj, ctx.astFile, ctx.tokenFile.PositionFor(end, false))
	edit := TextEdit{Range: Range{Start: startPosition, End: endPosition}, NewText: quoted}
	// Completion text edits must stay on the cursor line. Delete the remaining
	// portions of a multiline literal with non-overlapping additional edits.
	position := FromPosition(ctx.proj, ctx.astFile, ctx.tokenFile.PositionFor(ctx.sourcePos, false))
	if startPosition.Line < position.Line {
		edit.Range.Start = Position{Line: position.Line}
		item.FilterText = value
		item.AdditionalTextEdits = append(item.AdditionalTextEdits, TextEdit{Range: Range{Start: startPosition, End: edit.Range.Start}})
	}
	if endPosition.Line > position.Line {
		lineStart := ctx.tokenFile.Offset(ctx.tokenFile.LineStart(int(position.Line) + 1))
		lineEnd := ctx.tokenFile.Offset(ctx.tokenFile.LineStart(int(position.Line) + 2))
		lineContent := trimLineEnding(ctx.astFile.Code[lineStart:lineEnd])
		edit.Range.End = Position{Line: position.Line, Character: uint32(UTF16Len(string(lineContent)))}
		item.AdditionalTextEdits = append(item.AdditionalTextEdits, TextEdit{Range: Range{Start: edit.Range.End, End: endPosition}})
	}
	// On an empty interior line, the insertion would overlap the trailing
	// deletion at the same position, which completion edits do not permit.
	if edit.Range.Start == edit.Range.End && len(item.AdditionalTextEdits) > 0 {
		return false
	}
	item.TextEdit = &Or_CompletionItem_textEdit{Value: edit}
	return true
}

// completionASTPosition keeps lookup inside raw literals whose AST end omits
// carriage returns. The original cursor position is retained for source edits.
func completionASTPosition(proj *xgo.Project, astFile *ast.File, pos token.Pos) token.Pos {
	if !bytes.ContainsRune(astFile.Code, '\r') {
		return pos
	}
	astPos := pos
	ast.Inspect(astFile, func(node ast.Node) bool {
		lit, ok := node.(*ast.BasicLit)
		if ok && lit.Kind == token.STRING && lit.Value[0] == '`' &&
			lit.End() < pos && pos <= basicLitEnd(proj.Fset, astFile, lit) {
			astPos = lit.End()
		}
		return true
	})
	return astPos
}
