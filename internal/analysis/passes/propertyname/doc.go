// Package propertyname defines an Analyzer that validates PropertyName
// arguments in function calls against the receiver type's properties.
//
// # Analyzer propertyname
//
// propertyname: check that PropertyName arguments refer to existing properties
//
// This checker reports calls where a PropertyName string-literal argument does
// not match any property of the effective receiver type (the type inferred from
// the enclosing file or an explicit selector expression).
//
// Such calls would silently do nothing at runtime and almost always indicate
// a typo or stale property name.
//
//	showVar("nonexistent")
package propertyname
