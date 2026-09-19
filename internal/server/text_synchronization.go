package server

import (
	"bytes"
	"fmt"

	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/protocol"
	"github.com/goplus/xgolsw/xgo"
)

// didOpen handles the textDocument/didOpen notification from the LSP client.
// It starts a document session with the client's content and version, including
// when a reopened document has a lower version than its previous session.
func (s *Server) didOpen(params *DidOpenTextDocumentParams) error {
	path, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return err
	}

	s.putDocumentFile(path, &xgo.File{
		Content: []byte(params.TextDocument.Text),
		Version: int(params.TextDocument.Version),
	})
	s.publishFileDiagnostics(path)
	return nil
}

// didChange handles the textDocument/didChange notification from the LSP client.
// It applies document changes to the project and publishes updated diagnostics.
func (s *Server) didChange(params *DidChangeTextDocumentParams) error {
	path, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return err
	}

	content, err := s.changedText(path, params.ContentChanges)
	if err != nil {
		return err
	}

	s.ModifyFiles([]FileChange{{
		Path:    path,
		Content: content,
		Version: int(params.TextDocument.Version),
	}})
	s.publishFileDiagnostics(path)
	return nil
}

// didSave handles the textDocument/didSave notification from the LSP client.
// Included text replaces the document content without changing its version.
func (s *Server) didSave(params *DidSaveTextDocumentParams) error {
	if params.Text == nil {
		return nil
	}
	path, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return err
	}

	file := &xgo.File{Content: []byte(*params.Text)}
	if old, ok := s.getProj().File(path); ok {
		file.Version = old.Version
	}
	s.putDocumentFile(path, file)
	s.publishFileDiagnostics(path)
	return nil
}

// didClose handles the textDocument/didClose notification from the LSP client.
// When a document is closed, its diagnostics are cleared by sending an empty
// diagnostics array to the client.
func (s *Server) didClose(params *DidCloseTextDocumentParams) error {
	s.queueDiagnostics(params.TextDocument.URI, &diagnosticRequest{clear: true})
	return nil
}

// diagnosticRequest records the latest pending diagnostic action for a document.
type diagnosticRequest struct {
	path  string
	clear bool
}

// publishFileDiagnostics schedules analysis without blocking document changes.
func (s *Server) publishFileDiagnostics(path string) {
	s.queueDiagnostics(s.toDocumentURI(path), &diagnosticRequest{path: path})
}

// queueDiagnostics replaces superseded work and starts a publisher when needed.
func (s *Server) queueDiagnostics(uri DocumentURI, request *diagnosticRequest) {
	s.diagnosticsMu.Lock()
	defer s.diagnosticsMu.Unlock()
	if s.pendingDiagnostics == nil {
		s.pendingDiagnostics = make(map[DocumentURI]*diagnosticRequest)
	}
	s.pendingDiagnostics[uri] = request
	if !s.diagnosticsRunning {
		s.diagnosticsRunning = true
		go s.publishPendingDiagnostics()
	}
}

// publishPendingDiagnostics serializes publication so an older result cannot
// arrive after a newer result or a close notification's empty report. Client
// callbacks run without the queue lock and may schedule more work.
func (s *Server) publishPendingDiagnostics() {
	for {
		s.diagnosticsMu.Lock()
		if len(s.pendingDiagnostics) == 0 {
			s.diagnosticsRunning = false
			s.diagnosticsMu.Unlock()
			return
		}
		var uri DocumentURI
		var request *diagnosticRequest
		for uri, request = range s.pendingDiagnostics {
			break
		}
		s.diagnosticsMu.Unlock()

		revision := s.getProj().Revision()
		var diagnostics []Diagnostic
		if !request.clear {
			diagnostics = s.getDiagnostics(request.path)
		}
		s.diagnosticsMu.Lock()
		// Discard superseded requests and results invalidated by edits to any source file.
		if s.pendingDiagnostics[uri] != request || (!request.clear && s.getProj().Revision() != revision) {
			s.diagnosticsMu.Unlock()
			continue
		}
		delete(s.pendingDiagnostics, uri)
		s.diagnosticsMu.Unlock()
		s.publishDiagnostics(uri, diagnostics)
	}
}

// changedText applies full and incremental changes in notification order without
// modifying the project. Each range refers to the preceding change's result.
func (s *Server) changedText(path string, changes []protocol.TextDocumentContentChangeEvent) ([]byte, error) {
	if len(changes) == 0 {
		return nil, fmt.Errorf("%w: no content changes provided", jsonrpc2.ErrInternal)
	}

	var content []byte
	if changes[0].Range != nil {
		file, ok := s.getProj().File(path)
		if !ok {
			return nil, fmt.Errorf("%w: file not found", jsonrpc2.ErrInternal)
		}
		content = file.Content
	}

	for _, change := range changes {
		if change.Range == nil {
			content = []byte(change.Text)
			continue
		}

		start := PositionOffset(content, change.Range.Start)
		end := PositionOffset(content, change.Range.End)
		if end < start {
			return nil, fmt.Errorf("%w: invalid range for content change", jsonrpc2.ErrInternal)
		}

		var buf bytes.Buffer
		buf.Write(content[:start])
		buf.WriteString(change.Text)
		buf.Write(content[end:])
		content = buf.Bytes()
	}

	return content, nil
}

// getDiagnostics collects syntax and type errors for a modified document.
// Analyzers and framework-specific checks run through pull diagnostics.
func (s *Server) getDiagnostics(path string) []Diagnostic {
	proj := s.getProj()
	if !proj.IsSourceFile(path) {
		return nil
	}
	result := newDiagnosticResult()
	if astFile := s.collectSyntaxDiagnostics(proj, path, &result); astFile != nil {
		s.collectTypeDiagnostics(proj, &result)
	}
	return result.diagnostics[s.toDocumentURI(path)]
}

// FileChange represents a file change.
type FileChange struct {
	Path    string
	Content []byte
	Version int // Client document version.
}

// ModifyFiles modifies files in the project.
func (s *Server) ModifyFiles(changes []FileChange) {
	for _, change := range changes {
		if old, ok := s.getProj().File(change.Path); ok && change.Version <= old.Version {
			continue
		}
		s.putDocumentFile(change.Path, &xgo.File{
			Content: change.Content,
			Version: change.Version,
		})
	}
}

// putDocumentFile replaces document content while preserving the provider
// timestamp so an unchanged file map does not overwrite the update.
func (s *Server) putDocumentFile(path string, file *xgo.File) {
	proj := s.getProj()
	if old, ok := proj.File(path); ok {
		file.ModTime = old.ModTime
	}
	proj.PutFile(path, file)
}
