package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

func TestRangesOverlap(t *testing.T) {
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
