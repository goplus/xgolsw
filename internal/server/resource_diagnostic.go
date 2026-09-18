package server

import "github.com/goplus/xgo/ast"

// addResourceDiagnostic reports an adapter's resource error at its physical
// source range. Nodes from deleted or replaced source cannot produce a range.
func (s *Server) addResourceDiagnostic(result *resourceAnalysis, node ast.Node, message string) {
	astFile := sourceASTFile(result.proj, node.Pos())
	if astFile == nil {
		return
	}
	uri := s.toDocumentURI(result.proj.Fset.File(node.Pos()).Name())
	result.addDiagnostics(uri, Diagnostic{
		Severity: SeverityError,
		Range:    resourceRange(result.proj, astFile, node),
		Message:  s.translate(message),
	})
}
