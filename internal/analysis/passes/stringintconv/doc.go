// Package stringintconv defines an Analyzer that flags type conversions
// from integers to strings.
//
// # Analyzer stringintconv
//
// stringintconv: check for string(int) conversions
//
// This checker flags conversions of the form string(x) where x is an integer
// (but not byte or rune) type. Such conversions return the UTF-8 encoding
// of the Unicode code point x, not a decimal string representation.
// Use strconv.Itoa or fmt.Sprint instead.
package stringintconv
