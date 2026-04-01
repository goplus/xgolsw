// Package analysisutil defines various helper functions
// used by two or more packages beneath go/analysis.
package analysisutil

import (
	"fmt"
	"go/parser"
	"go/token"
	"strings"

	"github.com/goplus/xgo/ast"
	xgotoken "github.com/goplus/xgo/token"
)

// MustExtractDoc is like [ExtractDoc] but it panics on error.
//
// To use, define a doc.go file such as:
//
//	// Package halting defines an analyzer of program termination.
//	//
//	// # Analyzer halting
//	//
//	// halting: reports whether execution will halt.
//	//
//	// The halting analyzer reports a diagnostic for functions
//	// that run forever. To suppress the diagnostics, try inserting
//	// a 'break' statement into each loop.
//	package halting
//
//	import _ "embed"
//
//	//go:embed doc.go
//	var doc string
//
// And declare your analyzer as:
//
//	var Analyzer = &analysis.Analyzer{
//		Name:             "halting",
//		Doc:              analysisutil.MustExtractDoc(doc, "halting"),
//		...
//	}
func MustExtractDoc(content, name string) string {
	doc, err := ExtractDoc(content, name)
	if err != nil {
		panic(err)
	}
	return doc
}

// ExtractDoc extracts a section of a package doc comment from the
// provided contents of an analyzer package's doc.go file.
//
// A section is a portion of the comment between one heading and
// the next, using this form:
//
//	# Analyzer NAME
//
//	MARKER(DOC-FMT): NAME: SUMMARY format is required, where NAME matches
//	                 the analyzer name and SUMMARY describes its function
//
//	Full description...
//
// where NAME matches the name argument, and SUMMARY is a brief
// verb-phrase that describes the analyzer. The following lines, up
// until the next heading or the end of the comment, contain the full
// description. ExtractDoc returns the portion following the colon,
// which is the form expected by Analyzer.Doc.
//
// Example:
//
//	# Analyzer printf
//
//	printf: checks consistency of calls to printf
//
//	The printf analyzer checks consistency of calls to printf.
//	Here is the complete description...
//
// This notation allows a single doc comment to provide documentation
// for multiple analyzers, each in its own section.
// The HTML anchors generated for each heading are predictable.
//
// It returns an error if the content was not a valid Go source file
// containing a package doc comment with a heading of the required
// form.
//
// This machinery enables the package documentation (typically
// accessible via the web at https://pkg.go.dev/) and the command
// documentation (typically printed to a terminal) to be derived from
// the same source and formatted appropriately.
func ExtractDoc(content, name string) (string, error) {
	if content == "" {
		return "", fmt.Errorf("empty XGo source file")
	}
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", content, parser.ParseComments|parser.PackageClauseOnly)
	if err != nil {
		return "", fmt.Errorf("not an XGo source file")
	}
	if f.Doc == nil {
		return "", fmt.Errorf("an XGo source file has no package doc comment")
	}
	for _, section := range strings.Split(f.Doc.Text(), "\n# ") {
		if body := strings.TrimPrefix(section, "Analyzer "+name); body != section &&
			body != "" &&
			(body[0] == '\r' || body[0] == '\n') {
			body = strings.TrimSpace(body)
			rest := strings.TrimPrefix(body, name+":")
			if rest == body {
				return "", fmt.Errorf("'Analyzer %s' heading not followed by '%s: summary...' line", name, name)
			}
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("package doc comment contains no 'Analyzer %s' heading", name)
}

// NoSideEffects reports whether the expression e can be evaluated without
// observable side effects. It is conservative: returns false when unsure.
func NoSideEffects(e ast.Expr) bool {
	switch e := e.(type) {
	case *ast.BasicLit:
		return true
	case *ast.Ident:
		return true
	case *ast.ParenExpr:
		return NoSideEffects(e.X)
	case *ast.UnaryExpr:
		if e.Op == xgotoken.ARROW { // channel receive has side effects
			return false
		}
		return NoSideEffects(e.X)
	case *ast.BinaryExpr:
		return NoSideEffects(e.X) && NoSideEffects(e.Y)
	case *ast.StarExpr:
		// Dereference may panic (e.g., nil pointer); treat as having side effects.
		return false
	case *ast.SelectorExpr:
		return NoSideEffects(e.X)
	case *ast.IndexExpr:
		// Indexing may panic (nil or out-of-bounds); treat as having side effects.
		return false
	case *ast.SliceExpr:
		// Slicing may panic and may have side effects in bounds; treat as having side effects.
		return false
	case *ast.TypeAssertExpr:
		// Type assertion may panic on failure; treat as having side effects.
		return false
	}
	return false
}

// PrintExpr renders an expression as a human-readable string for diagnostic messages.
func PrintExpr(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.BasicLit:
		return e.Value
	case *ast.Ident:
		return e.Name
	case *ast.ParenExpr:
		return "(" + PrintExpr(e.X) + ")"
	case *ast.SelectorExpr:
		return PrintExpr(e.X) + "." + e.Sel.Name
	case *ast.IndexExpr:
		return PrintExpr(e.X) + "[" + PrintExpr(e.Index) + "]"
	case *ast.StarExpr:
		return "*" + PrintExpr(e.X)
	case *ast.UnaryExpr:
		return e.Op.String() + PrintExpr(e.X)
	case *ast.BinaryExpr:
		return PrintExpr(e.X) + " " + e.Op.String() + " " + PrintExpr(e.Y)
	case *ast.CallExpr:
		return PrintExpr(e.Fun) + "(...)"
	case *ast.CompositeLit:
		if e.Type != nil {
			return PrintExpr(e.Type) + "{...}"
		}
		return "{...}"
	case *ast.TypeAssertExpr:
		return PrintExpr(e.X) + ".(" + PrintExpr(e.Type) + ")"
	case *ast.SliceExpr:
		return PrintExpr(e.X) + "[:]"
	}
	return "..."
}
