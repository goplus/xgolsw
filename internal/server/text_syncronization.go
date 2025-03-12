package server

import (
	"github.com/goplus/goxlsw/gop"
)

// didOpen handles the textDocument/didOpen notification from the LSP client.
// It updates the project with the new file content and publishes diagnostics.
// The document URI is converted to a filesystem path, and a file change is created
// with the document's content and version number.
func (s *Server) didOpen(params *DidOpenTextDocumentParams) error {
	path, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return err
	}

	return s.didModifyFile([]gop.FileChange{{
		Path:    path,
		Content: []byte(params.TextDocument.Text),
		Version: int(params.TextDocument.Version),
	}})
}

// didChange handles the textDocument/didChange notification from the LSP client.
// It applies document changes to the project and publishes updated diagnostics.
// For simplicity, this implementation only uses the latest content change
// rather than applying incremental changes.
func (s *Server) didChange(params *DidChangeTextDocumentParams) error {
	path, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return err
	}

	// Convert all changes to FileChange
	// Note: We currently take only the final state of the document
	// rather than applying incremental changes
	changes := []gop.FileChange{{
		Path:    path,
		Content: []byte(params.ContentChanges[len(params.ContentChanges)-1].Text),
		Version: int(params.TextDocument.Version),
	}}

	return s.didModifyFile(changes)
}

// didSave handles the textDocument/didSave notification from the LSP client.
// If the notification includes the document text, the project is updated.
// Otherwise, no change is made since the document content hasn't changed.
// Save notifications typically don't include version numbers, so 0 is used.
func (s *Server) didSave(params *DidSaveTextDocumentParams) error {
	// If text is included in save notification, update the file
	if params.Text != nil {
		path, err := s.fromDocumentURI(params.TextDocument.URI)
		if err != nil {
			return err
		}

		return s.didModifyFile([]gop.FileChange{{
			Path:    path,
			Content: []byte(*params.Text),
			Version: 0, // Save notifications don't include versions
		}})
	}
	return nil
}

// didClose handles the textDocument/didClose notification from the LSP client.
// When a document is closed, its diagnostics are cleared by sending an empty
// diagnostics array to the client.
func (s *Server) didClose(params *DidCloseTextDocumentParams) error {
	// Clear diagnostics when file is closed
	return s.publishDiagnostics(params.TextDocument.URI, nil)
}

// didModifyFile is a shared implementation for handling document modifications.
// It updates the project with file changes and asynchronously publishes diagnostics.
// The function:
// 1. Updates the project's files with the provided changes
// 2. Starts a goroutine to generate and publish diagnostics for each changed file
// 3. Returns immediately after updating files for better responsiveness
func (s *Server) didModifyFile(changes []gop.FileChange) error {
	// 1. Update files synchronously
	s.getProj().ModifyFiles(changes)

	// 2. Asynchronously generate and publish diagnostics
	// This allows for quick response while diagnostics computation happens in background
	go func() {
		for _, change := range changes {
			// Convert path to URI for diagnostics
			uri := s.toDocumentURI(change.Path)

			// Get diagnostics from AST and type checking
			diagnostics, err := s.getDiagnostics(change.Path)
			if err != nil {
				// Log error but continue processing other files
				continue
			}

			// Publish diagnostics
			if err := s.publishDiagnostics(uri, diagnostics); err != nil {
				// Log error but continue
				continue
			}
		}
	}()

	return nil
}

// getDiagnostics generates diagnostic information for a specific file.
// It performs two checks:
// 1. AST parsing - reports syntax errors
// 2. Type checking - reports type errors
//
// If AST parsing fails, only syntax errors are returned as diagnostics.
// If AST parsing succeeds but type checking fails, type errors are returned.
// Returns a slice of diagnostics and an error (if diagnostic generation failed).
func (s *Server) getDiagnostics(path string) ([]Diagnostic, error) {
	var diagnostics []Diagnostic

	proj := s.getProj()

	// 1. Get AST diagnostics
	// Parse the file and check for syntax errors
	_, err := proj.AST(path)
	if err != nil {
		// Convert syntax errors to diagnostics with position at the start of file
		return []Diagnostic{{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 0, Character: 0},
			},
			Severity: SeverityError,
			Source:   "goxlsw",
			Message:  err.Error(),
		}}, nil
	}

	// 2. Get type checking diagnostics
	// Perform type checking on the file
	_, _, err, _ = proj.TypeInfo()
	if err != nil {
		// Add type checking errors to diagnostics
		diagnostics = append(diagnostics, Diagnostic{
			Range: Range{
				Start: Position{Line: 0, Character: 0},
				End:   Position{Line: 0, Character: 0},
			},
			Severity: SeverityError,
			Source:   "goxlsw",
			Message:  err.Error(),
		})
	}

	return diagnostics, nil
}
