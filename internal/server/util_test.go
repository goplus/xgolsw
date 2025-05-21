package server

import "testing"

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
			if got := rangesOverlap(tt.a, tt.b); got != tt.want {
				t.Errorf("got %t, want %t", got, tt.want)
			}
			if got := rangesOverlap(tt.b, tt.a); got != tt.want {
				t.Errorf("got %t, want %t", got, tt.want)
			}
		})
	}
}
