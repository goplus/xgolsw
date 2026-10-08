package server

import (
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
)

// sourceDiagnostic stores a diagnostic at a project-relative path.
// The reporting request resolves the path to a document URI.
type sourceDiagnostic struct {
	filename   string
	diagnostic Diagnostic
}

// addResourceDiagnostic reports an adapter's resource error at its physical
// source range. Nodes from deleted or replaced source cannot produce a range.
func addResourceDiagnostic(proj *xgo.Project, result *resourceAnalysis, node ast.Node, message string) {
	astFile := sourceASTFile(proj, node.Pos())
	if astFile == nil {
		return
	}
	result.diagnostics = append(result.diagnostics, sourceDiagnostic{proj.Fset.File(node.Pos()).Name(), Diagnostic{
		Severity: SeverityError,
		Range:    resourceRange(proj, astFile, node),
		Message:  message,
	}})
}

// collectResourceDiagnostics resolves cached diagnostics against the current workspace root.
func (s *Server) collectResourceDiagnostics(result *diagnosticResult, analysis *resourceAnalysis) {
	for _, source := range analysis.diagnostics {
		result.addDiagnostics(s.toDocumentURI(source.filename), source.diagnostic)
	}
}
