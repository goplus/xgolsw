package server

import (
	"fmt"
	"iter"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/scanner"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/qiniu/x/errors"
)

// diagnosticResult contains diagnostic messages for project documents.
type diagnosticResult struct {
	// diagnostics stores diagnostic messages for each document.
	diagnostics map[DocumentURI][]Diagnostic

	// seenDiagnostics stores already reported diagnostics to avoid duplicates.
	seenDiagnostics map[DocumentURI]map[string]struct{}

	// hasErrorSeverityDiagnostic is true if the result has any
	// diagnostics with error severity.
	hasErrorSeverityDiagnostic bool
}

// newDiagnosticResult creates an empty diagnostic result.
func newDiagnosticResult() diagnosticResult {
	return diagnosticResult{diagnostics: make(map[DocumentURI][]Diagnostic)}
}

// addDiagnostics adds diagnostics to the diagnostic result.
func (r *diagnosticResult) addDiagnostics(documentURI DocumentURI, diags ...Diagnostic) {
	if r.seenDiagnostics == nil {
		r.seenDiagnostics = make(map[DocumentURI]map[string]struct{})
	}
	seenDiagnostics := r.seenDiagnostics[documentURI]
	if seenDiagnostics == nil {
		seenDiagnostics = make(map[string]struct{})
		r.seenDiagnostics[documentURI] = seenDiagnostics
	}

	r.diagnostics[documentURI] = slices.Grow(r.diagnostics[documentURI], len(diags))
	for _, diag := range diags {
		fingerprint := fmt.Sprintf("%d\n%v\n%s", diag.Severity, diag.Range, diag.Message)
		if _, ok := seenDiagnostics[fingerprint]; ok {
			continue
		}
		seenDiagnostics[fingerprint] = struct{}{}

		r.diagnostics[documentURI] = append(r.diagnostics[documentURI], diag)
		if diag.Severity == SeverityError {
			r.hasErrorSeverityDiagnostic = true
		}
	}
}

// diagnosticsAt collects pull diagnostics from project syntax, types, analyzers,
// and framework-specific checks.
func (s *Server) diagnosticsAt(proj *xgo.Project) (*diagnosticResult, error) {
	if result, err := s.diagnosticsForSpx(proj); result != nil || err != nil {
		return result, err
	}
	result := newDiagnosticResult()
	if _, err := s.collectPackageSyntaxDiagnostics(proj, &result); err != nil {
		return nil, err
	}
	s.collectTypeDiagnostics(proj, &result)
	s.inspectDiagnosticsAnalyzers(proj, &result, nil)
	return &result, nil
}

// collectPackageSyntaxDiagnostics collects parse errors, including those from
// files without an AST, and returns the possibly incomplete AST package.
func (s *Server) collectPackageSyntaxDiagnostics(proj *xgo.Project, result *diagnosticResult) (*ast.Package, error) {
	astPkg, err := proj.ASTPackage()
	if astPkg == nil {
		return nil, err
	}
	for filename := range astPkg.Files {
		s.collectSyntaxDiagnostics(proj, filename, result)
	}
	var errorList scanner.ErrorList
	if errors.As(err, &errorList) {
		for _, e := range errorList {
			if _, ok := result.diagnostics[s.toDocumentURI(e.Pos.Filename)]; !ok {
				s.collectSyntaxDiagnostics(proj, e.Pos.Filename, result)
			}
		}
	}
	return astPkg, nil
}

// collectSyntaxDiagnostics collects parse errors and returns the possibly incomplete AST.
func (s *Server) collectSyntaxDiagnostics(proj *xgo.Project, filename string, result *diagnosticResult) *ast.File {
	uri := s.toDocumentURI(filename)
	result.diagnostics[uri] = []Diagnostic{}
	astFile, err := proj.ASTFile(filename)
	if err == nil {
		return astFile
	}
	var errorList scanner.ErrorList
	if errors.As(err, &errorList) && astFile != nil && astFile.Pos().IsValid() {
		for _, e := range errorList {
			result.addDiagnostics(uri, Diagnostic{
				Severity: SeverityError,
				Range:    RangeForASTFilePosition(proj, astFile, e.Pos),
				Message:  s.translate(e.Msg),
			})
		}
	} else {
		result.addDiagnostics(uri, Diagnostic{
			Severity: SeverityError,
			Message:  s.translate(fmt.Sprintf("failed to parse source file: %v", err)),
		})
	}
	return astFile
}

// collectTypeDiagnostics collects type errors and returns the project's type information.
func (s *Server) collectTypeDiagnostics(proj *xgo.Project, result *diagnosticResult) *types.Info {
	handleErr := func(err error) {
		if typeErr, ok := err.(typesutil.Error); ok {
			if !typeErr.Pos.IsValid() {
				// Implicit import failures may have no source position to report.
				return
			}
			position := typeErr.Fset.Position(typeErr.Pos)
			result.addDiagnostics(s.toDocumentURI(position.Filename), Diagnostic{
				Severity: SeverityError,
				Range:    RangeForPosEnd(proj, typeErr.Pos, typeErr.End),
				Message:  typeErr.Msg,
			})
		}
	}
	typeInfo, err := proj.TypeInfo()
	if err != nil {
		switch err := err.(type) {
		case errors.List:
			for _, e := range err {
				handleErr(e)
			}
		default:
			handleErr(err)
		}
	}
	return typeInfo
}

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification#textDocument_diagnostic
func (s *Server) textDocumentDiagnostic(params *DocumentDiagnosticParams) (*DocumentDiagnosticReport, error) {
	result, err := s.diagnosticsAt(s.getProjWithFile())
	if err != nil {
		return nil, err
	}

	return &DocumentDiagnosticReport{Value: RelatedFullDocumentDiagnosticReport{
		FullDocumentDiagnosticReport: FullDocumentDiagnosticReport{
			Kind:  string(DiagnosticFull),
			Items: append([]Diagnostic{}, result.diagnostics[params.TextDocument.URI]...),
		},
	}}, nil
}

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.18/specification#workspace_diagnostic
func (s *Server) workspaceDiagnostic(params *WorkspaceDiagnosticParams) (*WorkspaceDiagnosticReport, error) {
	result, err := s.diagnosticsAt(s.getProjWithFile())
	if err != nil {
		return nil, err
	}

	items := make([]WorkspaceDocumentDiagnosticReport, 0, len(result.diagnostics))
	for uri, diagnostics := range result.diagnostics {
		items = append(items, WorkspaceDocumentDiagnosticReport{
			Value: WorkspaceFullDocumentDiagnosticReport{
				URI: uri,
				FullDocumentDiagnosticReport: FullDocumentDiagnosticReport{
					Kind:  string(DiagnosticFull),
					Items: diagnostics,
				},
			},
		})
	}
	return &WorkspaceDiagnosticReport{Items: items}, nil
}

// inspectDiagnosticsAnalyzers runs registered analyzers on project source files.
func (s *Server) inspectDiagnosticsAnalyzers(proj *xgo.Project, result *diagnosticResult, configurePass func(string, *protocol.Pass)) {
	fset := proj.Fset
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return
	}
	astPkg, _ := proj.ASTPackage()
	if astPkg == nil {
		return
	}
	for filename, astFile := range astPkg.Files {
		var diagnostics []Diagnostic
		pass := &protocol.Pass{
			Fset:      fset,
			Files:     []*ast.File{astFile},
			Pkg:       typeInfo.Pkg,
			TypesInfo: typeInfo,
			Report: func(d protocol.Diagnostic) {
				diagnostics = append(diagnostics, Diagnostic{
					Range:    RangeForPosEnd(proj, d.Pos, d.End),
					Severity: SeverityError,
					Message:  s.translate(d.Message),
				})
			},
			ResultOf: map[*protocol.Analyzer]any{
				inspect.Analyzer: inspector.New([]*ast.File{astFile}),
			},
			ResolvedCallExprArgs: func(call *ast.CallExpr) iter.Seq[xgoutil.ResolvedCallExprArg] {
				return resolvedCallExprArgs(proj, typeInfo, call)
			},
		}

		if configurePass != nil {
			configurePass(filename, pass)
		}

		for _, analyzer := range s.analyzers {
			an := analyzer.Analyzer()
			if _, err := an.Run(pass); err != nil {
				diagnostics = append(diagnostics, Diagnostic{
					Severity: SeverityError,
					Message:  s.translate(fmt.Sprintf("analyzer %q failed: %v", an.Name, err)),
				})
			}
		}

		if len(diagnostics) > 0 {
			documentURI := s.toDocumentURI(filename)
			result.addDiagnostics(documentURI, diagnostics...)
		}
	}
}
