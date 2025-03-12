package server

import (
	"encoding/json"
	"errors"
	"go/types"
	"testing"

	"github.com/goplus/gop/ast"
	"github.com/goplus/gop/x/typesutil"
	"github.com/goplus/goxlsw/gop"
	"github.com/goplus/goxlsw/jsonrpc2"
	"github.com/goplus/goxlsw/protocol"
)

// MockProject implements a mock Project interface for testing
type MockProject struct {
	files        map[string]gop.File
	astError     error
	typeError    error
	updatedPaths []string
}

// AST returns a mock AST file or an error if astError is set
func (m *MockProject) AST(path string) (*ast.File, error) {
	if m.astError != nil {
		return nil, m.astError
	}
	// Create a minimal ast.File instance
	return &ast.File{
		Name: &ast.Ident{Name: "main"},
	}, nil
}

// TypeInfo returns mock type information or an error if typeError is set
func (m *MockProject) TypeInfo() (*types.Package, *typesutil.Info, error, error) {
	if m.typeError != nil {
		return nil, nil, m.typeError, nil
	}

	// Create minimal type information instances
	pkg := types.NewPackage("main", "main")
	info := &typesutil.Info{
		Types: make(map[ast.Expr]types.TypeAndValue),
		Defs:  make(map[*ast.Ident]types.Object),
		Uses:  make(map[*ast.Ident]types.Object),
	}

	return pkg, info, nil, nil
}

// ModifyFiles tracks file modifications for testing
func (m *MockProject) ModifyFiles(changes []gop.FileChange) {
	for _, change := range changes {
		m.files[change.Path] = &gop.FileImpl{
			Content: change.Content,
		}
		m.updatedPaths = append(m.updatedPaths, change.Path)
	}
}

// File returns a file by path if it exists in the mock project
func (m *MockProject) File(path string) (gop.File, bool) {
	file, ok := m.files[path]
	return file, ok
}

// MockReplier implements a message replier for testing
type MockReplier struct {
	notifications []*jsonrpc2.Notification
}

// ReplyMessage records notifications for later verification
func (m *MockReplier) ReplyMessage(msg jsonrpc2.Message) error {
	if n, ok := msg.(*jsonrpc2.Notification); ok {
		m.notifications = append(m.notifications, n)
	}
	return nil
}

// TestServer implements a server for testing
type TestServer struct {
	proj         *MockProject
	replier      *MockReplier
	convertError error
}

// getProj returns the mock project
func (s *TestServer) getProj() *MockProject {
	return s.proj
}

// fromDocumentURI converts a URI to a filesystem path or returns an error
// if convertError is set
func (s *TestServer) fromDocumentURI(uri protocol.DocumentURI) (string, error) {
	if s.convertError != nil {
		return "", s.convertError
	}
	// Simply convert URI to path by removing file:// prefix
	path := string(uri)
	if len(path) > 7 && path[:7] == "file://" {
		path = path[7:]
	}
	return path, nil
}

// toDocumentURI converts a filesystem path to a DocumentURI
func (s *TestServer) toDocumentURI(path string) protocol.DocumentURI {
	return protocol.DocumentURI("file://" + path)
}

// publishDiagnostics creates and sends a publishDiagnostics notification
func (s *TestServer) publishDiagnostics(uri protocol.DocumentURI, diagnostics []protocol.Diagnostic) error {
	params := &protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diagnostics,
	}
	notification, _ := jsonrpc2.NewNotification("textDocument/publishDiagnostics", params)
	return s.replier.ReplyMessage(notification)
}

// didOpen implements the textDocument/didOpen handler for testing
func (s *TestServer) didOpen(params *protocol.DidOpenTextDocumentParams) error {
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

// didChange implements the textDocument/didChange handler for testing
func (s *TestServer) didChange(params *protocol.DidChangeTextDocumentParams) error {
	path, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return err
	}

	changes := []gop.FileChange{{
		Path:    path,
		Content: []byte(params.ContentChanges[len(params.ContentChanges)-1].Text),
		Version: int(params.TextDocument.Version),
	}}

	return s.didModifyFile(changes)
}

// didSave implements the textDocument/didSave handler for testing
func (s *TestServer) didSave(params *protocol.DidSaveTextDocumentParams) error {
	if params.Text == nil {
		return nil
	}

	path, err := s.fromDocumentURI(params.TextDocument.URI)
	if err != nil {
		return err
	}

	return s.didModifyFile([]gop.FileChange{{
		Path:    path,
		Content: []byte(*params.Text),
		Version: 0,
	}})
}

// didClose implements the textDocument/didClose handler for testing
func (s *TestServer) didClose(params *protocol.DidCloseTextDocumentParams) error {
	return s.publishDiagnostics(params.TextDocument.URI, nil)
}

// didModifyFile performs file modifications and synchronously handles diagnostics for testing
func (s *TestServer) didModifyFile(changes []gop.FileChange) error {
	s.proj.ModifyFiles(changes)

	// Process diagnostics synchronously to simplify testing
	for _, change := range changes {
		uri := s.toDocumentURI(change.Path)
		diagnostics, _ := s.getDiagnostics(change.Path)
		s.publishDiagnostics(uri, diagnostics)
	}

	return nil
}

// getDiagnostics generates diagnostics for testing
func (s *TestServer) getDiagnostics(path string) ([]protocol.Diagnostic, error) {
	var diagnostics []protocol.Diagnostic

	// Check for AST errors
	if s.proj.astError != nil {
		return []protocol.Diagnostic{{
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 0},
			},
			Severity: protocol.SeverityError,
			Source:   "goxlsw",
			Message:  s.proj.astError.Error(),
		}}, nil
	}

	// Check for type errors
	if s.proj.typeError != nil {
		diagnostics = append(diagnostics, protocol.Diagnostic{
			Range: protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 0},
			},
			Severity: protocol.SeverityError,
			Source:   "goxlsw",
			Message:  s.proj.typeError.Error(),
		})
	}

	return diagnostics, nil
}

// Test functions below

// TestDidOpen tests the didOpen handler functionality
func TestDidOpen(t *testing.T) {
	tests := []struct {
		name            string
		params          *protocol.DidOpenTextDocumentParams
		convertError    error
		expectedPath    string
		expectedContent string
		wantErr         bool
	}{
		{
			name: "basic open",
			params: &protocol.DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{
					URI:     "file:///test.gop",
					Version: 1,
					Text:    "package main",
				},
			},
			expectedPath:    "/test.gop",
			expectedContent: "package main",
			wantErr:         false,
		},
		{
			name: "URI conversion error",
			params: &protocol.DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{
					URI:     "file:///invalid.gop",
					Version: 1,
					Text:    "package main",
				},
			},
			convertError: errors.New("URI conversion failed"),
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			mockProj := &MockProject{
				files: make(map[string]gop.File),
			}
			mockReplier := &MockReplier{}

			server := &TestServer{
				proj:         mockProj,
				replier:      mockReplier,
				convertError: tt.convertError,
			}

			// Execute test
			err := server.didOpen(tt.params)

			// Verify results
			if (err != nil) != tt.wantErr {
				t.Errorf("didOpen() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify file was correctly updated
				if len(mockProj.updatedPaths) == 0 {
					t.Errorf("No files were updated")
					return
				}

				if mockProj.updatedPaths[0] != tt.expectedPath {
					t.Errorf("Updated wrong path: got %s, want %s", mockProj.updatedPaths[0], tt.expectedPath)
				}

				file, ok := mockProj.files[tt.expectedPath]
				if !ok {
					t.Errorf("File not found in project: %s", tt.expectedPath)
					return
				}

				if string(file.Content) != tt.expectedContent {
					t.Errorf("File content = %q, want %q", string(file.Content), tt.expectedContent)
				}

				// Verify diagnostic notification was sent
				if len(mockReplier.notifications) == 0 {
					t.Errorf("No diagnostics notifications were sent")
				} else if mockReplier.notifications[0].Method() != "textDocument/publishDiagnostics" {
					t.Errorf("Wrong notification method: %s", mockReplier.notifications[0].Method())
				}
			}
		})
	}
}

// TestDidChange tests the didChange handler functionality
func TestDidChange(t *testing.T) {
	tests := []struct {
		name            string
		params          *protocol.DidChangeTextDocumentParams
		convertError    error
		expectedPath    string
		expectedContent string
		wantErr         bool
	}{
		{
			name: "basic change",
			params: &protocol.DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{
					TextDocumentIdentifier: protocol.TextDocumentIdentifier{
						URI: "file:///test.gop",
					},
					Version: 2,
				},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{
					{Text: "package main\n\nfunc main() {}"},
				},
			},
			expectedPath:    "/test.gop",
			expectedContent: "package main\n\nfunc main() {}",
			wantErr:         false,
		},
		{
			name: "URI conversion error",
			params: &protocol.DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{
					TextDocumentIdentifier: protocol.TextDocumentIdentifier{
						URI: "file:///invalid.gop",
					},
					Version: 2,
				},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{
					{Text: "package main"},
				},
			},
			convertError: errors.New("URI conversion failed"),
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			mockProj := &MockProject{
				files: make(map[string]gop.File),
			}
			mockReplier := &MockReplier{}

			server := &TestServer{
				proj:         mockProj,
				replier:      mockReplier,
				convertError: tt.convertError,
			}

			// Execute test
			err := server.didChange(tt.params)

			// Verify results
			if (err != nil) != tt.wantErr {
				t.Errorf("didChange() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify file was correctly updated
				if len(mockProj.updatedPaths) == 0 {
					t.Errorf("No files were updated")
					return
				}

				if mockProj.updatedPaths[0] != tt.expectedPath {
					t.Errorf("Updated wrong path: got %s, want %s", mockProj.updatedPaths[0], tt.expectedPath)
				}

				file, ok := mockProj.files[tt.expectedPath]
				if !ok {
					t.Errorf("File not found in project: %s", tt.expectedPath)
					return
				}

				if string(file.Content) != tt.expectedContent {
					t.Errorf("File content = %q, want %q", string(file.Content), tt.expectedContent)
				}

				// Verify diagnostic notification was sent
				if len(mockReplier.notifications) == 0 {
					t.Errorf("No diagnostics notifications were sent")
				} else if mockReplier.notifications[0].Method() != "textDocument/publishDiagnostics" {
					t.Errorf("Wrong notification method: %s", mockReplier.notifications[0].Method())
				}
			}
		})
	}
}

// TestDidSave tests the didSave handler functionality
func TestDidSave(t *testing.T) {
	content := "package main\n\nfunc main() {}"
	tests := []struct {
		name            string
		params          *protocol.DidSaveTextDocumentParams
		convertError    error
		expectedPath    string
		expectedContent string
		wantUpdate      bool
		wantErr         bool
	}{
		{
			name: "save with content",
			params: &protocol.DidSaveTextDocumentParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: "file:///test.gop",
				},
				Text: &content,
			},
			expectedPath:    "/test.gop",
			expectedContent: content,
			wantUpdate:      true,
			wantErr:         false,
		},
		{
			name: "save without content",
			params: &protocol.DidSaveTextDocumentParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: "file:///test.gop",
				},
			},
			wantUpdate: false,
			wantErr:    false,
		},
		{
			name: "URI conversion error",
			params: &protocol.DidSaveTextDocumentParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: "file:///invalid.gop",
				},
				Text: &content,
			},
			convertError: errors.New("URI conversion failed"),
			wantUpdate:   false,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			mockProj := &MockProject{
				files: make(map[string]gop.File),
			}
			mockReplier := &MockReplier{}

			server := &TestServer{
				proj:         mockProj,
				replier:      mockReplier,
				convertError: tt.convertError,
			}

			// Execute test
			err := server.didSave(tt.params)

			// Verify results
			if (err != nil) != tt.wantErr {
				t.Errorf("didSave() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.wantUpdate {
				// Verify file was correctly updated
				if len(mockProj.updatedPaths) == 0 {
					t.Errorf("No files were updated")
					return
				}

				if mockProj.updatedPaths[0] != tt.expectedPath {
					t.Errorf("Updated wrong path: got %s, want %s", mockProj.updatedPaths[0], tt.expectedPath)
				}

				file, ok := mockProj.files[tt.expectedPath]
				if !ok {
					t.Errorf("File not found in project: %s", tt.expectedPath)
					return
				}

				if string(file.Content) != tt.expectedContent {
					t.Errorf("File content = %q, want %q", string(file.Content), tt.expectedContent)
				}

				// Verify diagnostic notification was sent
				if len(mockReplier.notifications) == 0 {
					t.Errorf("No diagnostics notifications were sent")
				}
			}

			if !tt.wantErr && !tt.wantUpdate {
				if len(mockProj.updatedPaths) > 0 {
					t.Errorf("File was updated but shouldn't be")
				}
			}
		})
	}
}

// TestDidClose tests the didClose handler functionality
func TestDidClose(t *testing.T) {
	tests := []struct {
		name    string
		params  *protocol.DidCloseTextDocumentParams
		wantErr bool
	}{
		{
			name: "basic close",
			params: &protocol.DidCloseTextDocumentParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: "file:///test.gop",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			mockReplier := &MockReplier{}

			server := &TestServer{
				replier: mockReplier,
			}

			// Execute test
			err := server.didClose(tt.params)

			// Verify results
			if (err != nil) != tt.wantErr {
				t.Errorf("didClose() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify empty diagnostics notification was sent
			if len(mockReplier.notifications) == 0 {
				t.Errorf("No diagnostics notifications were sent")
				return
			}

			if mockReplier.notifications[0].Method() != "textDocument/publishDiagnostics" {
				t.Errorf("Wrong notification method: %s", mockReplier.notifications[0].Method())
			}

			// Verify diagnostics are empty
			var params protocol.PublishDiagnosticsParams
			if err := json.Unmarshal(mockReplier.notifications[0].Params(), &params); err != nil {
				t.Errorf("Failed to unmarshal notification params: %v", err)
				return
			}

			if len(params.Diagnostics) != 0 {
				t.Errorf("Diagnostics not cleared, got %d items", len(params.Diagnostics))
			}
		})
	}
}

// TestGetDiagnostics tests the getDiagnostics functionality
func TestGetDiagnostics(t *testing.T) {
	tests := []struct {
		name      string
		path      string
		astError  error
		typeError error
		wantCount int
	}{
		{
			name:      "no errors",
			path:      "/test.gop",
			wantCount: 0,
		},
		{
			name:      "AST error",
			path:      "/test.gop",
			astError:  errors.New("syntax error"),
			wantCount: 1,
		},
		{
			name:      "Type error",
			path:      "/test.gop",
			typeError: errors.New("type error"),
			wantCount: 1,
		},
		{
			name:      "Both errors",
			path:      "/test.gop",
			astError:  errors.New("syntax error"),
			typeError: errors.New("type error"),
			wantCount: 1, // AST errors take precedence
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			mockProj := &MockProject{
				files:     make(map[string]gop.File),
				astError:  tt.astError,
				typeError: tt.typeError,
			}

			server := &TestServer{
				proj: mockProj,
			}

			// Execute test
			diagnostics, err := server.getDiagnostics(tt.path)

			// Verify results
			if err != nil {
				t.Errorf("getDiagnostics() error = %v", err)
				return
			}

			if len(diagnostics) != tt.wantCount {
				t.Errorf("getDiagnostics() returned %d diagnostics, want %d", len(diagnostics), tt.wantCount)
			}

			if tt.astError != nil && len(diagnostics) > 0 {
				if diagnostics[0].Message != tt.astError.Error() {
					t.Errorf("Diagnostic message = %q, want %q", diagnostics[0].Message, tt.astError.Error())
				}
			} else if tt.typeError != nil && len(diagnostics) > 0 {
				if diagnostics[0].Message != tt.typeError.Error() {
					t.Errorf("Diagnostic message = %q, want %q", diagnostics[0].Message, tt.typeError.Error())
				}
			}
		})
	}
}

// TestDidModifyFile tests the didModifyFile functionality
func TestDidModifyFile(t *testing.T) {
	tests := []struct {
		name      string
		changes   []gop.FileChange
		astError  error
		typeError error
		wantDiags bool
	}{
		{
			name: "single file no errors",
			changes: []gop.FileChange{
				{
					Path:    "/test.gop",
					Content: []byte("package main"),
					Version: 1,
				},
			},
			wantDiags: false,
		},
		{
			name: "single file with AST error",
			changes: []gop.FileChange{
				{
					Path:    "/test.gop",
					Content: []byte("package main"),
					Version: 1,
				},
			},
			astError:  errors.New("syntax error"),
			wantDiags: true,
		},
		{
			name: "multiple files",
			changes: []gop.FileChange{
				{
					Path:    "/test1.gop",
					Content: []byte("package main"),
					Version: 1,
				},
				{
					Path:    "/test2.gop",
					Content: []byte("package main"),
					Version: 1,
				},
			},
			wantDiags: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			mockProj := &MockProject{
				files:     make(map[string]gop.File),
				astError:  tt.astError,
				typeError: tt.typeError,
			}
			mockReplier := &MockReplier{}

			server := &TestServer{
				proj:    mockProj,
				replier: mockReplier,
			}

			// Execute test
			err := server.didModifyFile(tt.changes)

			// Verify results
			if err != nil {
				t.Errorf("didModifyFile() error = %v", err)
				return
			}

			// Verify files were updated
			if len(mockProj.updatedPaths) != len(tt.changes) {
				t.Errorf("Updated %d files, want %d", len(mockProj.updatedPaths), len(tt.changes))
			}

			for i, change := range tt.changes {
				if i < len(mockProj.updatedPaths) && mockProj.updatedPaths[i] != change.Path {
					t.Errorf("Updated wrong path at index %d: got %s, want %s",
						i, mockProj.updatedPaths[i], change.Path)
				}

				file, ok := mockProj.files[change.Path]
				if !ok {
					t.Errorf("File not found in project: %s", change.Path)
					continue
				}

				if string(file.Content) != string(change.Content) {
					t.Errorf("File content = %q, want %q", string(file.Content), string(change.Content))
				}
			}

			// Verify diagnostic notifications were sent
			if len(mockReplier.notifications) != len(tt.changes) {
				t.Errorf("Sent %d notifications, want %d", len(mockReplier.notifications), len(tt.changes))
			}

			for _, notification := range mockReplier.notifications {
				if notification.Method() != "textDocument/publishDiagnostics" {
					t.Errorf("Wrong notification method: %s", notification.Method())
				}

				// Check diagnostic content
				var params protocol.PublishDiagnosticsParams
				if err := json.Unmarshal(notification.Params(), &params); err != nil {
					t.Errorf("Failed to unmarshal notification params: %v", err)
					continue
				}

				hasDiags := len(params.Diagnostics) > 0
				if hasDiags != tt.wantDiags {
					t.Errorf("Diagnostics present = %v, want %v", hasDiags, tt.wantDiags)
				}
			}
		})
	}
}
