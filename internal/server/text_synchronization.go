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

	files := s.readProviderFiles()
	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	s.syncFilesLocked(files)
	if s.openFiles == nil {
		s.openFiles = make(map[string]*xgo.File)
	}
	file := &xgo.File{
		Content: []byte(params.TextDocument.Text),
		Version: int(params.TextDocument.Version),
	}
	s.putDocumentFileLocked(path, file)
	s.openFiles[path] = file
	s.queueOpenDiagnosticsLocked()
	s.queueDiagnosticsLocked(s.toDocumentURI(path), &diagnosticRequest{path: path})
	return nil
}

// didChange handles the textDocument/didChange notification from the LSP client.
// It applies document changes to the project and publishes updated diagnostics.
func (s *Server) didChange(params *DidChangeTextDocumentParams) error {
	path, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return err
	}

	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	content, err := s.changedText(path, params.ContentChanges)
	if err != nil {
		return err
	}
	s.modifyFilesLocked([]FileChange{{
		Path:    path,
		Content: content,
		Version: int(params.TextDocument.Version),
	}})
	s.queueDiagnosticsLocked(s.toDocumentURI(path), &diagnosticRequest{path: path})
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

	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	file := &xgo.File{Content: []byte(*params.Text)}
	if old, ok := s.getProj().File(path); ok {
		file.Version = old.Version
	}
	s.putDocumentFileLocked(path, file)
	s.queueOpenDiagnosticsLocked()
	s.queueDiagnosticsLocked(s.toDocumentURI(path), &diagnosticRequest{path: path})
	return nil
}

// didClose handles the textDocument/didClose notification from the LSP client.
// It restores provider ownership of the file and clears its diagnostics.
func (s *Server) didClose(params *DidCloseTextDocumentParams) error {
	path, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return err
	}
	files := s.readProviderFiles()
	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	delete(s.openFiles, path)
	// A preserved provider timestamp cannot identify discarded editor content.
	// Remove it before synchronization so even unchanged provider files reload.
	s.getProj().DeleteFile(path)
	s.syncFilesLocked(files)
	s.queueOpenDiagnosticsLocked()
	s.queueDiagnosticsLocked(s.toDocumentURI(path), &diagnosticRequest{clear: true})
	return nil
}

// diagnosticRequest records the latest pending diagnostic action for a document.
type diagnosticRequest struct {
	path  string
	clear bool
}

// queueOpenDiagnosticsLocked refreshes all open documents because source files
// share a package. The caller holds projectMu.
func (s *Server) queueOpenDiagnosticsLocked() {
	for path := range s.openFiles {
		if s.getProj().IsSourceFile(path) {
			s.queueDiagnosticsLocked(s.toDocumentURI(path), &diagnosticRequest{path: path})
		}
	}
}

// queueDiagnosticsLocked replaces superseded work and starts a publisher when
// needed. The caller holds projectMu.
func (s *Server) queueDiagnosticsLocked(uri DocumentURI, request *diagnosticRequest) {
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
		s.projectMu.Lock()
		if len(s.pendingDiagnostics) == 0 {
			s.diagnosticsRunning = false
			s.projectMu.Unlock()
			return
		}
		var uri DocumentURI
		var request *diagnosticRequest
		for uri, request = range s.pendingDiagnostics {
			break
		}
		revision := s.getProj().Revision()
		proj := s.projectSnapshotLocked()
		s.projectMu.Unlock()

		var diagnostics []Diagnostic
		if !request.clear {
			diagnostics = s.diagnosticsForFile(proj, request.path)
		}
		s.projectMu.Lock()
		// Discard superseded requests and results invalidated by edits to any source file.
		if s.pendingDiagnostics[uri] != request || (!request.clear && s.getProj().Revision() != revision) {
			s.projectMu.Unlock()
			continue
		}
		delete(s.pendingDiagnostics, uri)
		s.projectMu.Unlock()
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

// diagnosticsForFile collects syntax and type errors from a stable project.
func (s *Server) diagnosticsForFile(proj *xgo.Project, path string) []Diagnostic {
	if !proj.IsSourceFile(path) {
		return nil
	}
	proj.TypeInfo()
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
	s.projectMu.Lock()
	defer s.projectMu.Unlock()
	s.modifyFilesLocked(changes)
}

// modifyFilesLocked applies a batch of versioned edits and refreshes diagnostics
// if any file changed. The caller holds projectMu.
func (s *Server) modifyFilesLocked(changes []FileChange) {
	changed := false
	for _, change := range changes {
		if old, ok := s.getProj().File(change.Path); ok && change.Version <= old.Version {
			continue
		}
		s.putDocumentFileLocked(change.Path, &xgo.File{
			Content: change.Content,
			Version: change.Version,
		})
		changed = true
	}
	if changed {
		s.queueOpenDiagnosticsLocked()
	}
}

// putDocumentFileLocked updates editor content, preserving the provider timestamp
// for changes outside an open session. The caller holds projectMu.
func (s *Server) putDocumentFileLocked(path string, file *xgo.File) {
	proj := s.getProj()
	if old, ok := proj.File(path); ok {
		file.ModTime = old.ModTime
	}
	proj.PutFile(path, file)
	if _, open := s.openFiles[path]; open {
		s.openFiles[path] = file
	}
}
