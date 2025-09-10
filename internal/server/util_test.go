package server

import (
	"testing"

	xgotoken "github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUTF16Len(t *testing.T) {
	for _, tt := range []struct {
		name string
		s    string
		want int
	}{
		{
			name: "EmptyString",
			s:    "",
			want: 0,
		},
		{
			name: "ASCIIString",
			s:    "hello",
			want: 5,
		},
		{
			name: "ASCIIStringWithSpacesAndPunctuation",
			s:    "Hello, World!",
			want: 13,
		},
		{
			name: "CJKCharacters",
			s:    "世界",
			want: 2, // Each CJK character is 1 UTF-16 code unit.
		},
		{
			name: "MixedASCIIAndCJK",
			s:    "Hello 世界",
			want: 8, // "Hello " (6) + "世界" (2).
		},
		{
			name: "EmojiSingleCodePoint",
			s:    "😀",
			want: 2, // Basic emoji requires surrogate pair (2 UTF-16 code units).
		},
		{
			name: "MultipleEmojis",
			s:    "😀😁😂",
			want: 6, // Each emoji is 2 UTF-16 code units.
		},
		{
			name: "EmojiWithModifier",
			s:    "👨‍💻",
			want: 5, // Man (2) + ZWJ (1) + Computer (2) = 5 UTF-16 code units.
		},
		{
			name: "SkinToneEmoji",
			s:    "👋🏽",
			want: 4, // Waving hand (2) + skin tone modifier (2) = 4 UTF-16 code units.
		},
		{
			name: "SurrogatePairCharacter",
			s:    "𝒃",
			want: 2, // Mathematical script small b requires surrogate pair.
		},
		{
			name: "MixedContent",
			s:    "Hello, 世界! 😀",
			want: 13, // "Hello, " (7) + "世界" (2) + "! " (2) + emoji (2) = 13 UTF-16 code units.
		},
		{
			name: "StringWithNewlines",
			s:    "line1\nline2",
			want: 11, // Each character including newline is 1 UTF-16 code unit.
		},
		{
			name: "StringWithTabs",
			s:    "a\tb\tc",
			want: 5, // Each character including tabs is 1 UTF-16 code unit.
		},
		{
			name: "UnicodeAccents",
			s:    "café",
			want: 4, // c(1) + a(1) + f(1) + é(1) = 4 UTF-16 code units.
		},
		{
			name: "CombiningCharacters",
			s:    "e\u0301", // e + combining acute accent
			want: 2,         // Base character (1) + combining mark (1) = 2 UTF-16 code units.
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, UTF16Len(tt.s))
		})
	}
}

func TestUTF16PosToUTF8Offset(t *testing.T) {
	for _, tt := range []struct {
		name     string
		s        string
		utf16Pos int
		want     int
	}{
		{
			name:     "EmptyString",
			s:        "",
			utf16Pos: 0,
			want:     0,
		},
		{
			name:     "EmptyStringNonZeroOffset",
			s:        "",
			utf16Pos: 5,
			want:     0,
		},
		{
			name:     "NegativeOffset",
			s:        "abc",
			utf16Pos: -1,
			want:     0,
		},
		{
			name:     "ASCIIStringZeroOffset",
			s:        "abc",
			utf16Pos: 0,
			want:     0,
		},
		{
			name:     "ASCIIStringValidOffset",
			s:        "abc",
			utf16Pos: 2,
			want:     2,
		},
		{
			name:     "ASCIIStringOffsetAtEnd",
			s:        "abc",
			utf16Pos: 3,
			want:     3,
		},
		{
			name:     "ASCIIStringOffsetBeyondEnd",
			s:        "abc",
			utf16Pos: 5,
			want:     3,
		},
		{
			name:     "StringWithSurrogateCharBeforeChar",
			s:        "a𝒃c",
			utf16Pos: 1,
			want:     1, // Points to after 'a'.
		},
		{
			name:     "StringWithSurrogateCharMiddleOfChar",
			s:        "a𝒃c",
			utf16Pos: 2,
			want:     1, // Points to start of '𝒃'.
		},
		{
			name:     "StringWithSurrogateCharAfterChar",
			s:        "a𝒃c",
			utf16Pos: 3,
			want:     5, // Points to after '𝒃'.
		},
		{
			name:     "StringWithSurrogateCharAtEnd",
			s:        "a𝒃c",
			utf16Pos: 4,
			want:     6, // Points to end of string.
		},
		{
			name:     "EmojiStringZeroOffset",
			s:        "😀😁😂",
			utf16Pos: 0,
			want:     0,
		},
		{
			name:     "EmojiStringMiddleOfFirstEmoji",
			s:        "😀😁😂",
			utf16Pos: 1,
			want:     0, // Points to start of first emoji.
		},
		{
			name:     "EmojiStringAfterFirstEmoji",
			s:        "😀😁😂",
			utf16Pos: 2,
			want:     4, // Points to after first emoji.
		},
		{
			name:     "EmojiStringMiddleOfSecondEmoji",
			s:        "😀😁😂",
			utf16Pos: 3,
			want:     4, // Points to start of second emoji.
		},
		{
			name:     "EmojiStringAfterSecondEmoji",
			s:        "😀😁😂",
			utf16Pos: 4,
			want:     8, // Points to after second emoji.
		},
		{
			name:     "MixedContent",
			s:        "Hello, 世界! 😀",
			utf16Pos: 7,
			want:     7, // Points to after "Hello, ".
		},
		{
			name:     "MixedContentAfterCJK",
			s:        "Hello, 世界! 😀",
			utf16Pos: 9,
			want:     13, // Points to after "世界!" (CJK chars are 1 UTF-16 unit each).
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, UTF16PosToUTF8Offset(tt.s, tt.utf16Pos))
		})
	}
}

func TestFromPosition(t *testing.T) {
	for _, tt := range []struct {
		name     string
		code     string
		position xgotoken.Position
		want     Position
	}{
		{
			name: "FirstCharacterOfFile",
			code: "package main",
			position: xgotoken.Position{
				Line:   1,
				Column: 1,
			},
			want: Position{
				Line:      0,
				Character: 0,
			},
		},
		{
			name: "MiddleOfFirstLine",
			code: "package main",
			position: xgotoken.Position{
				Line:   1,
				Column: 8,
			},
			want: Position{
				Line:      0,
				Character: 7,
			},
		},
		{
			name: "EndOfFirstLine",
			code: "package main",
			position: xgotoken.Position{
				Line:   1,
				Column: 13,
			},
			want: Position{
				Line:      0,
				Character: 12,
			},
		},
		{
			name: "SecondLineStart",
			code: "package main\nimport \"fmt\"",
			position: xgotoken.Position{
				Line:   2,
				Column: 1,
			},
			want: Position{
				Line:      1,
				Character: 0,
			},
		},
		{
			name: "SecondLineMiddle",
			code: "package main\nimport \"fmt\"",
			position: xgotoken.Position{
				Line:   2,
				Column: 7,
			},
			want: Position{
				Line:      1,
				Character: 6,
			},
		},
		{
			name: "WithCJKCharacters",
			code: "// 世界\npackage main",
			position: xgotoken.Position{
				Line:   1,
				Column: 4, // After "// "
			},
			want: Position{
				Line:      0,
				Character: 3, // "// " is 3 UTF-16 units
			},
		},
		{
			name: "AfterCJKCharacters",
			code: "// 世界\npackage main",
			position: xgotoken.Position{
				Line:   1,
				Column: 8, // After "// 世界" (UTF-8: 3 + 3 + 3 = 9 bytes, but column is 8)
			},
			want: Position{
				Line:      0,
				Character: 5, // "// " (3) + "世界" (2) = 5 UTF-16 units
			},
		},
		{
			name: "WithEmoji",
			code: "// 😀\npackage main",
			position: xgotoken.Position{
				Line:   1,
				Column: 4, // After "// "
			},
			want: Position{
				Line:      0,
				Character: 3, // "// " is 3 UTF-16 units
			},
		},
		{
			name: "AfterEmoji",
			code: "// 😀\npackage main",
			position: xgotoken.Position{
				Line:   1,
				Column: 8, // After "// 😀" (UTF-8: 3 + 4 = 7 bytes, column would be 8)
			},
			want: Position{
				Line:      0,
				Character: 5, // "// " (3) + emoji (2) = 5 UTF-16 units
			},
		},
		{
			name: "EmptyLine",
			code: "package main\n\nfunc main() {}",
			position: xgotoken.Position{
				Line:   2,
				Column: 1,
			},
			want: Position{
				Line:      1,
				Character: 0,
			},
		},
		{
			name: "WithTabs",
			code: "\tpackage main",
			position: xgotoken.Position{
				Line:   1,
				Column: 2,
			},
			want: Position{
				Line:      0,
				Character: 1, // Tab is 1 UTF-16 unit
			},
		},
		{
			name: "ColumnExceedsLineLength",
			code: "abc",
			position: xgotoken.Position{
				Line:   1,
				Column: 10, // Beyond the line length
			},
			want: Position{
				Line:      0,
				Character: 3, // Should be clamped to the line length
			},
		},
		{
			name: "ZeroLineAndColumn",
			code: "package main",
			position: xgotoken.Position{
				Line:   0, // Invalid line (will be corrected to 1)
				Column: 0, // Invalid column (will be corrected to 1)
			},
			want: Position{
				Line:      0,
				Character: 0,
			},
		},
		{
			name: "NegativeLineAndColumn",
			code: "package main",
			position: xgotoken.Position{
				Line:   -1, // Invalid line (will be corrected to 1)
				Column: -1, // Invalid column (will be corrected to 1)
			},
			want: Position{
				Line:      0,
				Character: 0,
			},
		},
		{
			name: "ColumnExceedsLineLengthWithNewline",
			code: "abc\ndef",
			position: xgotoken.Position{
				Line:   1,
				Column: 10, // Beyond the first line length
			},
			want: Position{
				Line:      0,
				Character: 3, // Should be clamped to the first line length (3 chars in "abc")
			},
		},
		{
			name: "ColumnExceedsLineLengthMultiLine",
			code: "first\nsecond line\nthird",
			position: xgotoken.Position{
				Line:   1,
				Column: 20, // Way beyond the first line
			},
			want: Position{
				Line:      0,
				Character: 5, // Should be "first" which is 5 chars
			},
		},
		{
			name: "ColumnExactlyAtNewline",
			code: "test\nnext",
			position: xgotoken.Position{
				Line:   1,
				Column: 5, // Points to the newline character position
			},
			want: Position{
				Line:      0,
				Character: 4, // Should be "test" which is 4 chars
			},
		},
		{
			name: "ColumnAfterNewline",
			code: "line1\nline2",
			position: xgotoken.Position{
				Line:   1,
				Column: 6, // Points after the newline character
			},
			want: Position{
				Line:      0,
				Character: 5, // Should be clamped to "line1" length (5 chars)
			},
		},
		{
			name: "LastLineWithoutNewline",
			code: "first\nlast",
			position: xgotoken.Position{
				Line:   2,
				Column: 5, // Points to end of last line (no newline after it)
			},
			want: Position{
				Line:      1,
				Character: 4, // Should be "last" which is 4 chars
			},
		},
		{
			name: "LineExceedsFileLineCount",
			code: "line1\nline2",
			position: xgotoken.Position{
				Line:   5, // File only has 2 lines
				Column: 1,
			},
			want: Position{
				Line:      1, // Should be clamped to last line (line 2, 0-indexed as 1)
				Character: 0,
			},
		},
		{
			name: "MultiByteCharacterMidLine",
			code: "var café = 1",
			position: xgotoken.Position{
				Line:   1,
				Column: 5, // After "var "
			},
			want: Position{
				Line:      0,
				Character: 4, // "var " is 4 UTF-16 units
			},
		},
		{
			name: "AfterAccentedCharacter",
			code: "var café = 1",
			position: xgotoken.Position{
				Line:   1,
				Column: 9, // After "var café"
			},
			want: Position{
				Line:      0,
				Character: 8, // "var café" is 8 UTF-16 units
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fset := xgotoken.NewFileSet()
			files := map[string]*xgo.File{
				"test.gop": {Content: []byte(tt.code)},
			}
			proj := xgo.NewProject(fset, files, xgo.FeatAll)

			astPkg, err := proj.ASTPackage()
			require.NoError(t, err)
			require.NotNil(t, astPkg)

			astFile, ok := astPkg.Files["test.gop"]
			require.True(t, ok)
			require.NotNil(t, astFile)

			got := FromPosition(proj, astFile, tt.position)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsRangesOverlap(t *testing.T) {
	for _, tt := range []struct {
		name string
		a    Range
		b    Range
		want bool
	}{
		{
			name: "SameRange",
			a:    Range{Start: Position{Line: 1, Character: 2}, End: Position{Line: 3, Character: 4}},
			b:    Range{Start: Position{Line: 1, Character: 2}, End: Position{Line: 3, Character: 4}},
			want: true,
		},
		{
			name: "CompletelyDisjointRanges",
			a:    Range{Start: Position{Line: 1, Character: 1}, End: Position{Line: 2, Character: 1}},
			b:    Range{Start: Position{Line: 3, Character: 1}, End: Position{Line: 4, Character: 1}},
			want: false,
		},
		{
			name: "OverlappingWithDifferentStartAndEnd",
			a:    Range{Start: Position{Line: 1, Character: 1}, End: Position{Line: 3, Character: 1}},
			b:    Range{Start: Position{Line: 2, Character: 1}, End: Position{Line: 4, Character: 1}},
			want: true,
		},
		{
			name: "RangeAContainsRangeB",
			a:    Range{Start: Position{Line: 1, Character: 1}, End: Position{Line: 5, Character: 1}},
			b:    Range{Start: Position{Line: 2, Character: 1}, End: Position{Line: 4, Character: 1}},
			want: true,
		},
		{
			name: "RangeBContainsRangeA",
			a:    Range{Start: Position{Line: 2, Character: 1}, End: Position{Line: 4, Character: 1}},
			b:    Range{Start: Position{Line: 1, Character: 1}, End: Position{Line: 5, Character: 1}},
			want: true,
		},
		{
			name: "RangesTouchAtEndpointExactlyEndOfAEqualsStartOfB",
			a:    Range{Start: Position{Line: 1, Character: 1}, End: Position{Line: 2, Character: 5}},
			b:    Range{Start: Position{Line: 2, Character: 5}, End: Position{Line: 3, Character: 1}},
			want: true,
		},
		{
			name: "RangesTouchAtEndpointExactlyEndOfBEqualsStartOfA",
			a:    Range{Start: Position{Line: 2, Character: 5}, End: Position{Line: 3, Character: 1}},
			b:    Range{Start: Position{Line: 1, Character: 1}, End: Position{Line: 2, Character: 5}},
			want: true,
		},
		{
			name: "SameLineOverlappingCharacters",
			a:    Range{Start: Position{Line: 1, Character: 1}, End: Position{Line: 1, Character: 5}},
			b:    Range{Start: Position{Line: 1, Character: 3}, End: Position{Line: 1, Character: 7}},
			want: true,
		},
		{
			name: "SameLineNonOverlappingCharacters",
			a:    Range{Start: Position{Line: 1, Character: 1}, End: Position{Line: 1, Character: 3}},
			b:    Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 6}},
			want: false,
		},
		{
			name: "ZeroWidthRangeAtSamePosition",
			a:    Range{Start: Position{Line: 2, Character: 2}, End: Position{Line: 2, Character: 2}},
			b:    Range{Start: Position{Line: 2, Character: 2}, End: Position{Line: 2, Character: 2}},
			want: true,
		},
		{
			name: "ZeroWidthRangeInsideLargerRange",
			a:    Range{Start: Position{Line: 2, Character: 2}, End: Position{Line: 2, Character: 2}},
			b:    Range{Start: Position{Line: 1, Character: 1}, End: Position{Line: 3, Character: 3}},
			want: true,
		},
		{
			name: "OverlapOnlyOnCharacterPosition",
			a:    Range{Start: Position{Line: 1, Character: 1}, End: Position{Line: 1, Character: 5}},
			b:    Range{Start: Position{Line: 1, Character: 5}, End: Position{Line: 1, Character: 15}},
			want: true,
		},
		{
			name: "StartOfAEqualsEndOfBOnDifferentLinesNoOverlap",
			a:    Range{Start: Position{Line: 3, Character: 0}, End: Position{Line: 4, Character: 0}},
			b:    Range{Start: Position{Line: 1, Character: 0}, End: Position{Line: 3, Character: 0}},
			want: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsRangesOverlap(tt.a, tt.b))
			assert.Equal(t, tt.want, IsRangesOverlap(tt.b, tt.a))
		})
	}
}
