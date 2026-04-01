// Package loopclosure defines an Analyzer that checks for references to
// enclosing loop variables from within nested functions.
//
// # Analyzer loopclosure
//
// loopclosure: check references to loop variables from within nested functions
//
// This analyzer reports places where a function literal references the
// iteration variable of an enclosing loop, and the loop calls the function
// in such a way (e.g. with go or defer) that it may outlive the loop
// iteration and possibly observe the wrong value of the variable.
package loopclosure
