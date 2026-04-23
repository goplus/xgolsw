// Package unreachable defines an Analyzer that checks for unreachable code.
//
// # Analyzer unreachable
//
// unreachable: check for unreachable code
//
// The unreachable analyzer finds statements that execution can never reach
// because they are preceded by a return statement, a call to panic, an
// infinite loop, or similar constructs.
package unreachable
