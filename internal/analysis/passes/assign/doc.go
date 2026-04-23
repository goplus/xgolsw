// Package assign defines an Analyzer that detects useless assignments.
//
// # Analyzer assign
//
// assign: check for useless assignments
//
// This checker reports assignments of the form x = x or a[i] = a[i].
// These are almost always useless, and even when they are not they are
// usually a mistake.
package assign
