package server

import (
	"bytes"
	"cmp"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// FromPtr returns the value from a pointer. It returns the zero value of type T
// if the pointer is nil.
func FromPtr[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// DedupeLocations deduplicates locations.
func DedupeLocations(locations []Location) []Location {
	result := make([]Location, 0, len(locations))
	seen := make(map[Location]struct{}, len(locations))
	for _, loc := range locations {
		if _, ok := seen[loc]; !ok {
			seen[loc] = struct{}{}
			result = append(result, loc)
		}
	}
	return result
}

// UTF16Len calculates the UTF-16 length of the given string.
func UTF16Len(s string) int {
	var length int
	for _, r := range s {
		length += utf16.RuneLen(r)
	}
	return length
}

// UTF16PosToUTF8Offset converts a UTF-16 code unit position to a UTF-8 byte
// offset in the given string.
func UTF16PosToUTF8Offset(s string, utf16Pos int) int {
	if utf16Pos <= 0 {
		return 0
	}

	var utf16Units int
	for offset, r := range s {
		nextUTF16Units := utf16Units + utf16.RuneLen(r)
		if nextUTF16Units > utf16Pos {
			return offset
		}
		utf16Units = nextUTF16Units
	}
	return len(s)
}

// trimLineEnding returns content without a trailing line ending.
func trimLineEnding(content []byte) []byte {
	if len(content) > 0 && content[len(content)-1] == '\n' {
		content = content[:len(content)-1]
	}
	if len(content) > 0 && content[len(content)-1] == '\r' {
		content = content[:len(content)-1]
	}
	return content
}

// sourceLine returns a line without its line ending. The line must exist.
func sourceLine(content []byte, offsets []int, line int) []byte {
	end := len(content)
	if line+1 < len(offsets) {
		end = offsets[line+1]
	}
	return trimLineEnding(content[offsets[line]:end])
}

// offsetForPosition converts an LSP UTF-16 position using source line offsets.
// Positions beyond a line or the document are clamped to its end.
func offsetForPosition(content []byte, offsets []int, position Position) int {
	if uint64(position.Line) >= uint64(len(offsets)) {
		return len(content)
	}
	line := int(position.Line)
	return offsets[line] + UTF16PosToUTF8Offset(string(sourceLine(content, offsets, line)), int(position.Character))
}

// lineForOffset finds the source line containing a valid byte offset.
func lineForOffset(offsets []int, offset int) int {
	line, found := slices.BinarySearch(offsets, offset)
	if !found {
		line--
	}
	return line
}

// positionForOffset converts a byte offset to an LSP UTF-16 position.
// Offsets inside a line ending map to the end of that line.
func positionForOffset(content []byte, offsets []int, offset int) Position {
	offset = min(max(offset, 0), len(content))
	line := lineForOffset(offsets, offset)
	prefix := trimLineEnding(content[offsets[line]:offset])
	return Position{Line: uint32(line), Character: uint32(UTF16Len(string(prefix)))}
}

// PositionOffset converts an LSP UTF-16 position to a document byte offset.
func PositionOffset(content []byte, position Position) int {
	return offsetForPosition(content, xgoutil.LineOffsets(content), position)
}

// FromPosition converts a physical compiler position to an LSP position.
func FromPosition(proj *xgo.Project, astFile *ast.File, position token.Position) Position {
	file := xgoutil.NodeTokenFile(proj.Fset, astFile)
	offsets := proj.ASTFileLineOffsets(astFile)

	// The compiler's line table omits an empty final line.
	if position.Offset == len(astFile.Code) {
		return positionForOffset(astFile.Code, offsets, position.Offset)
	}

	line := min(max(position.Line, 1), file.LineCount())
	start := file.Offset(file.LineStart(line))
	end := len(astFile.Code)
	if line < file.LineCount() {
		end = file.Offset(file.LineStart(line + 1))
	}
	content := trimLineEnding(astFile.Code[start:end])
	offset := start + min(max(position.Column-1, 0), len(content))
	return positionForOffset(astFile.Code, offsets, offset)
}

// ToPosition converts an LSP position to the compiler's physical position.
func ToPosition(proj *xgo.Project, astFile *ast.File, position Position) token.Position {
	file := xgoutil.NodeTokenFile(proj.Fset, astFile)
	offsets := proj.ASTFileLineOffsets(astFile)
	offset := offsetForPosition(astFile.Code, offsets, position)
	return file.PositionFor(file.Pos(offset), false)
}

// PosAt returns the [token.Pos] of the given LSP position in astFile.
func PosAt(proj *xgo.Project, astFile *ast.File, position Position) token.Pos {
	file := xgoutil.NodeTokenFile(proj.Fset, astFile)
	return file.Pos(ToPosition(proj, astFile, position).Offset)
}

// RangeForASTFilePosition returns a [Range] for the given [token.Position]
// in the given AST file.
func RangeForASTFilePosition(proj *xgo.Project, astFile *ast.File, position token.Position) Range {
	p := FromPosition(proj, astFile, position)
	return Range{Start: p, End: p}
}

// RangeForASTFileNode returns the physical source [Range] for the given node,
// ignoring line directives.
func RangeForASTFileNode(proj *xgo.Project, astFile *ast.File, node ast.Node) Range {
	fset := proj.Fset
	return Range{
		Start: FromPosition(proj, astFile, fset.PositionFor(node.Pos(), false)),
		End:   FromPosition(proj, astFile, fset.PositionFor(node.End(), false)),
	}
}

// RangeForPos returns the physical source [Range] for pos, ignoring line directives.
func RangeForPos(proj *xgo.Project, pos token.Pos) Range {
	astPkg, _ := proj.ASTPackage()
	return RangeForASTFilePosition(proj, xgoutil.PosASTFile(proj.Fset, astPkg, pos), proj.Fset.PositionFor(pos, false))
}

// RangeForPosEnd returns the physical source [Range] between pos and end,
// ignoring line directives.
func RangeForPosEnd(proj *xgo.Project, pos, end token.Pos) Range {
	astPkg, _ := proj.ASTPackage()
	astFile := xgoutil.PosASTFile(proj.Fset, astPkg, pos)
	return Range{
		Start: FromPosition(proj, astFile, proj.Fset.PositionFor(pos, false)),
		End:   FromPosition(proj, astFile, proj.Fset.PositionFor(end, false)),
	}
}

// RangeForNode returns the [Range] for the given node.
func RangeForNode(proj *xgo.Project, node ast.Node) Range {
	astPkg, _ := proj.ASTPackage()
	return RangeForASTFileNode(proj, xgoutil.NodeASTFile(proj.Fset, astPkg, node), node)
}

// sourceASTFile returns the physical source file containing pos, or nil if
// that source version is no longer in the project.
func sourceASTFile(proj *xgo.Project, pos token.Pos) *ast.File {
	file := proj.Fset.File(pos)
	if file == nil {
		return nil
	}
	astFile, _ := proj.ASTFile(file.Name())
	if astFile == nil || proj.Fset.File(astFile.Pos()) != file {
		return nil
	}
	return astFile
}

// basicLitEnd returns the source end of a literal in astFile,
// including carriage returns omitted from raw string values by the parser.
func basicLitEnd(fset *token.FileSet, astFile *ast.File, lit *ast.BasicLit) token.Pos {
	if lit.Kind != token.STRING || !strings.HasPrefix(lit.Value, "`") {
		return lit.End()
	}
	file := fset.File(lit.Pos())
	offset := file.Offset(lit.Pos())
	// An unterminated raw string extends to EOF.
	length := bytes.IndexByte(astFile.Code[offset+1:], '`')
	if length < 0 {
		return file.Pos(len(astFile.Code))
	}
	return lit.Pos() + token.Pos(length+2)
}

// comparePositions compares positions in source order.
func comparePositions(a, b Position) int {
	if line := cmp.Compare(a.Line, b.Line); line != 0 {
		return line
	}
	return cmp.Compare(a.Character, b.Character)
}

// IsRangesOverlap reports whether two half-open ranges overlap. Empty or
// reversed ranges do not overlap any range.
func IsRangesOverlap(a, b Range) bool {
	return comparePositions(a.Start, a.End) < 0 &&
		comparePositions(b.Start, b.End) < 0 &&
		comparePositions(a.Start, b.End) < 0 &&
		comparePositions(b.Start, a.End) < 0
}
