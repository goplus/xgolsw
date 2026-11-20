// Package appends defines an Analyzer that detects append calls with no values to add.
//
// # Analyzer appends
//
// appends: check for missing values after append
//
// This checker reports calls to append that pass
// no values to be appended to the slice.
//
//	s := []string{"a", "b", "c"}
//	_ = append(s)
//
// Such calls are always no-ops and often indicate an
// underlying mistake.
package appends
