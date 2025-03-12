package server

import (
	"github.com/goplus/goxlsw/gop"
)

// didOpen updates the file when a open notification is received.
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

// didChange updates the file when a change notification is received.
func (s *Server) didChange(params *DidChangeTextDocumentParams) error {
	path, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return err
	}

	// Convert all changes to FileChange
	changes := []gop.FileChange{{
		Path:    path,
		Content: []byte(params.ContentChanges[len(params.ContentChanges)-1].Text), // Use latest content
		Version: int(params.TextDocument.Version),
	}}

	return s.didModifyFile(changes)
}

// didSave updates the file when a save notification is received.
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

// didClose clears diagnostics when a file is closed.
func (s *Server) didClose(params *DidCloseTextDocumentParams) error {
	// Clear diagnostics when file is closed
	return s.publishDiagnostics(params.TextDocument.URI, nil)
}

// didModifyFile updates the project with the changes and publishes diagnostics.
func (s *Server) didModifyFile(changes []gop.FileChange) error {
	// 1. Update files
	s.getProj().ModifyFiles(changes)

	// 2. Asynchronously generate and publish diagnostics
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

func (s *Server) getDiagnostics(path string) ([]Diagnostic, error) {
	var diagnostics []Diagnostic

	proj := s.getProj()
	// Get AST diagnostics
	_, err := proj.AST(path)
	if err != nil {
		// Convert syntax errors to diagnostics
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

	// Get type checking diagnostics
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
