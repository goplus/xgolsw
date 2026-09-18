package server

import (
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
)

// sourceDiagnostic stores an untranslated diagnostic at a project-relative path.
// URI resolution and translation belong to the request that reports it.
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

// collectResourceDiagnostics renders cached diagnostics for the current server.
func (s *Server) collectResourceDiagnostics(result *diagnosticResult, analysis *resourceAnalysis) {
	for _, source := range analysis.diagnostics {
		diagnostic := source.diagnostic
		diagnostic.Message = s.translate(diagnostic.Message)
		result.addDiagnostics(s.toDocumentURI(source.filename), diagnostic)
	}
}
