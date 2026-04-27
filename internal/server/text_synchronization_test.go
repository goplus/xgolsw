package server

import (
	"errors"
	"testing"
	"time"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/protocol"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

func file(text string) *xgo.File {
	return &xgo.File{Content: []byte(text)}
}

// strPtr returns a pointer to the given string
func strPtr(s string) *string {
	return &s
}

func TestModifyFiles(t *testing.T) {
	tests := []struct {
		name    string
		initial map[string]*xgo.File
		changes []FileChange
		want    map[string]string // path -> want content
	}{
		{
			name:    "AddNewFiles",
			initial: map[string]*xgo.File{},
			changes: []FileChange{
				{
					Path:    "new.go",
					Content: []byte("package main"),
					Version: 100,
				},
			},
			want: map[string]string{
				"new.go": "package main",
			},
		},
		{
			name: "UpdateExistingFileWithNewerVersion",
			initial: map[string]*xgo.File{
				"main.go": {
					Content: []byte("old content"),
					ModTime: time.UnixMilli(100),
				},
			},
			changes: []FileChange{
				{
					Path:    "main.go",
					Content: []byte("new content"),
					Version: 200,
				},
			},
			want: map[string]string{
				"main.go": "new content",
			},
		},
		{
			name: "IgnoreOlderVersionUpdate",
			initial: map[string]*xgo.File{
				"main.go": {
					Content: []byte("current content"),
					Version: 200,
				},
			},
			changes: []FileChange{
				{
					Path:    "main.go",
					Content: []byte("old content"),
					Version: 100,
				},
			},
			want: map[string]string{
				"main.go": "current content",
			},
		},
		{
			name: "MultipleFileChanges",
			initial: map[string]*xgo.File{
				"file1.go": {
					Content: []byte("content1"),
					ModTime: time.UnixMilli(100),
				},
				"file2.go": {
					Content: []byte("content2"),
					ModTime: time.UnixMilli(100),
				},
			},
			changes: []FileChange{
				{
					Path:    "file1.go",
					Content: []byte("new content1"),
					Version: 200,
				},
				{
					Path:    "file3.go",
					Content: []byte("content3"),
					Version: 200,
				},
			},
			want: map[string]string{
				"file1.go": "new content1",
				"file2.go": "content2",
				"file3.go": "content3",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create new project with initial files
			proj := xgo.NewProject(token.NewFileSet(), tt.initial, xgo.FeatAll)

			// Create a TestServer that extends the real Server
			server := &Server{
				workspaceRootFS: proj,
			}

			// Apply changes
			server.ModifyFiles(tt.changes)

			// Verify results
			for path, wantContent := range tt.want {
				file, ok := proj.File(path)
				require.True(t, ok)
				assert.Equal(t, wantContent, string(file.Content))
			}

			// Verify no extra files exist
			count := 0
			for path := range proj.Files() {
				count++
				assert.Contains(t, tt.want, path)
			}
			assert.Len(t, tt.want, count)
		})
	}
}

// TestDidOpen tests the didOpen handler functionality
func TestDidOpen(t *testing.T) {
	tests := []struct {
		name        string
		params      *protocol.DidOpenTextDocumentParams
		wantPath    string
		wantContent string
		wantErr     bool
	}{
		{
			name: "BasicOpen",
			params: &protocol.DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{
					URI:     "file://workspace/echo.spx",
					Version: 1,
					Text:    "echo \"100\"",
				},
			},
			wantPath:    "echo.spx",
			wantContent: "echo \"100\"",
			wantErr:     false,
		},
		{
			name: "OpenFileWithFunction",
			params: &protocol.DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{
					URI:     "file://workspace/test_func.spx",
					Version: 2,
					Text:    "onStart {\n say \"Hello, World!\"\n}",
				},
			},
			wantPath:    "test_func.spx",
			wantContent: "onStart {\n say \"Hello, World!\"\n}",
			wantErr:     false,
		},
		{
			name: "OpenFileWithUnicodeContent",
			params: &protocol.DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{
					URI:     "file://workspace/i18n.spx",
					Version: 3,
					Text:    "onStart {\n say \"你好，世界!\"\n}",
				},
			},
			wantPath:    "i18n.spx",
			wantContent: "onStart {\n say \"你好，世界!\"\n}",
			wantErr:     false,
		},
		{
			name: "URIConversionError",
			params: &protocol.DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{
					URI:     "file://error_workspace/error.spx",
					Version: 1,
					Text:    "onStart {}",
				},
			},
			wantErr: true,
		},
		{
			name: "EmptyFileContent",
			params: &protocol.DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{
					URI:     "file://workspace/empty.spx",
					Version: 1,
					Text:    "",
				},
			},
			wantPath:    "empty.spx",
			wantContent: "",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment with real Project instead of MockProject
			proj := xgo.NewProject(token.NewFileSet(), make(map[string]*xgo.File), 0)
			proj.PutFile(tt.wantPath, file("mock content"))
			mockReplier := &MockReplier{}

			// Create a TestServer that extends the real Server
			server := &Server{
				workspaceRootFS:  proj,
				replier:          mockReplier,
				workspaceRootURI: "file://workspace/",
			}

			// Execute test
			err := server.didOpen(tt.params)

			// Verify results
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if !tt.wantErr {
				// Verify file was correctly added to the project
				file, ok := proj.File(tt.wantPath)
				require.True(t, ok)
				assert.Equal(t, tt.wantContent, string(file.Content))
				assert.Equal(t, int(tt.params.TextDocument.Version), file.Version)
			}
		})
	}
}

// TestDidChange tests the didChange handler functionality
func TestDidChange(t *testing.T) {
	tests := []struct {
		name           string
		initialContent string
		params         *protocol.DidChangeTextDocumentParams
		convertError   error
		wantContent    string
		wantErr        bool
	}{
		{
			name:           "FullDocumentReplacement",
			initialContent: "package main",
			params: &protocol.DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{
					TextDocumentIdentifier: protocol.TextDocumentIdentifier{
						URI: "file://workspace/test.xgo",
					},
					Version: 2,
				},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{
					{Text: "package main\n\nfunc main() {}"},
				},
			},
			wantContent: "package main\n\nfunc main() {}",
			wantErr:     false,
		},
		{
			name:           "IncrementalChange",
			initialContent: "package main\n\nfunc main() {\n\t\n}",
			params: &protocol.DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{
					TextDocumentIdentifier: protocol.TextDocumentIdentifier{
						URI: "file://workspace/test.xgo",
					},
					Version: 2,
				},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{
					{
						Range: &protocol.Range{
							Start: protocol.Position{Line: 3, Character: 1},
							End:   protocol.Position{Line: 3, Character: 1},
						},
						Text: "fmt.Println(\"Hello, World!\")",
					},
				},
			},
			wantContent: "package main\n\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}",
			wantErr:     false,
		},
		{
			name:           "MultipleIncrementalChanges",
			initialContent: "package main\n\nfunc main() {\n\t\n}",
			params: &protocol.DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{
					TextDocumentIdentifier: protocol.TextDocumentIdentifier{
						URI: "file://workspace/test.xgo",
					},
					Version: 3,
				},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{
					{
						Range: &protocol.Range{
							Start: protocol.Position{Line: 3, Character: 1},
							End:   protocol.Position{Line: 3, Character: 1},
						},
						Text: "fmt.Print(\"Hello",
					},
					{
						Range: &protocol.Range{
							Start: protocol.Position{Line: 3, Character: 17},
							End:   protocol.Position{Line: 3, Character: 17},
						},
						Text: ", World!\")",
					},
				},
			},
			wantContent: "package main\n\nfunc main() {\n\tfmt.Print(\"Hello, World!\")\n}",
			wantErr:     false,
		},
		{
			name:           "URIConversionError",
			initialContent: "package main",
			params: &protocol.DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{
					TextDocumentIdentifier: protocol.TextDocumentIdentifier{
						URI: "file://error/test.xgo",
					},
					Version: 2,
				},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{
					{Text: "package main\n\nfunc main() {}"},
				},
			},
			convertError: errors.New("URI conversion failed"),
			wantErr:      true,
		},
		{
			name:           "EmptyChangesArray",
			initialContent: "package main",
			params: &protocol.DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{
					TextDocumentIdentifier: protocol.TextDocumentIdentifier{
						URI: "file://workspace/test.xgo",
					},
					Version: 2,
				},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment with initial file content
			files := make(map[string]*xgo.File)
			path := "test.xgo"

			files[path] = &xgo.File{
				Content: []byte(tt.initialContent),
				ModTime: time.Time{},
			}

			proj := xgo.NewProject(token.NewFileSet(), files, xgo.FeatAll)
			mockReplier := &MockReplier{}

			// Create a TestServer that extends the real Server
			server := &Server{
				workspaceRootFS:  proj,
				replier:          mockReplier,
				workspaceRootURI: "file://workspace/",
			}

			// Execute test
			err := server.didChange(tt.params)

			// Verify results
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if !tt.wantErr {
				// Verify file content was updated
				file, ok := proj.File(path)
				require.True(t, ok)
				assert.Equal(t, tt.wantContent, string(file.Content))
				assert.Equal(t, int(tt.params.TextDocument.Version), file.Version)
			}
		})
	}
}

// TestDidSave tests the didSave handler functionality
func TestDidSave(t *testing.T) {
	tests := []struct {
		name           string
		initialContent string
		params         *protocol.DidSaveTextDocumentParams
		wantContent    string
		contentChanged bool
		wantErr        bool
	}{
		{
			name:           "SaveWithText",
			initialContent: "package main",
			params: &protocol.DidSaveTextDocumentParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: "file://workspace/test.xgo",
				},
				Text: strPtr("package main\n\nfunc main() {}"),
			},
			wantContent:    "package main\n\nfunc main() {}",
			contentChanged: true,
			wantErr:        false,
		},
		{
			name:           "SaveWithoutText",
			initialContent: "package main",
			params: &protocol.DidSaveTextDocumentParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: "file://workspace/test.xgo",
				},
				Text: nil,
			},
			wantContent:    "package main", // Content should not change
			contentChanged: false,
			wantErr:        false,
		},
		{
			name:           "URIConversionError",
			initialContent: "package main",
			params: &protocol.DidSaveTextDocumentParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: "file://error/test.xgo",
				},
				Text: strPtr("package main\n\nfunc main() {}"),
			},
			contentChanged: false,
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			fset := token.NewFileSet()
			files := make(map[string]*xgo.File)
			path := "test.xgo"

			files[path] = &xgo.File{
				Content: []byte(tt.initialContent),
				ModTime: time.Time{},
			}

			proj := xgo.NewProject(fset, files, xgo.FeatASTCache)
			mockReplier := &MockReplier{}

			// Create a TestServer
			server := &Server{
				workspaceRootFS:  proj,
				replier:          mockReplier,
				workspaceRootURI: "file://workspace/",
			}

			// Execute test
			err := server.didSave(tt.params)

			// Verify results
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if !tt.wantErr {
				// Verify file content
				file, ok := proj.File(path)
				require.True(t, ok)
				assert.Equal(t, tt.wantContent, string(file.Content))
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
			name: "CloseDocument",
			params: &protocol.DidCloseTextDocumentParams{
				TextDocument: protocol.TextDocumentIdentifier{
					URI: "file://workspace/test.xgo",
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			fset := token.NewFileSet()
			files := make(map[string]*xgo.File)
			path := "/test.xgo"

			files[path] = &xgo.File{
				Content: []byte("package main"),
				ModTime: time.Time{},
			}

			proj := xgo.NewProject(fset, files, xgo.FeatASTCache)
			mockReplier := &MockReplier{}

			// Create a TestServer
			server := &Server{
				workspaceRootFS:  proj,
				replier:          mockReplier,
				workspaceRootURI: "file://workspace/",
			}

			// Execute test
			err := server.didClose(tt.params)

			// Verify results
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestChangedText tests the changedText function for processing document content changes
func TestChangedText(t *testing.T) {
	tests := []struct {
		name           string
		initialContent string
		changes        []protocol.TextDocumentContentChangeEvent
		want           string
		wantErr        bool
	}{
		{
			name:           "FullDocumentReplacement",
			initialContent: "package main\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}",
			changes: []protocol.TextDocumentContentChangeEvent{
				{
					Text: "package main\n\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}",
				},
			},
			want:    "package main\n\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}",
			wantErr: false,
		},
		{
			name:           "IncrementalChangeAddComma",
			initialContent: "package main\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}",
			changes: []protocol.TextDocumentContentChangeEvent{
				{
					Range: &protocol.Range{
						Start: protocol.Position{Line: 3, Character: 19},
						End:   protocol.Position{Line: 3, Character: 19},
					},
					Text: ",",
				},
			},
			want:    "package main\n\nfunc main() {\n\tfmt.Println(\"Hello,\")\n}",
			wantErr: false,
		},
		{
			name:           "IncrementalChangeReplaceWord",
			initialContent: "package main\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}",
			changes: []protocol.TextDocumentContentChangeEvent{
				{
					Range: &protocol.Range{
						Start: protocol.Position{Line: 3, Character: 14},
						End:   protocol.Position{Line: 3, Character: 19},
					},
					Text: "World",
				},
			},
			want:    "package main\n\nfunc main() {\n\tfmt.Println(\"World\")\n}",
			wantErr: false,
		},
		{
			name:           "MultipleIncrementalChanges",
			initialContent: "package main\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}",
			changes: []protocol.TextDocumentContentChangeEvent{
				{
					Range: &protocol.Range{
						Start: protocol.Position{Line: 3, Character: 14},
						End:   protocol.Position{Line: 3, Character: 19},
					},
					Text: "World",
				},
				{
					Range: &protocol.Range{
						Start: protocol.Position{Line: 3, Character: 19},
						End:   protocol.Position{Line: 3, Character: 19},
					},
					Text: "!",
				},
			},
			want:    "package main\n\nfunc main() {\n\tfmt.Println(\"World!\")\n}",
			wantErr: false,
		},
		{
			name:           "EmptyChangesArray",
			initialContent: "package main",
			changes:        []protocol.TextDocumentContentChangeEvent{},
			want:           "",
			wantErr:        true,
		},
		{
			name:           "InvalidRangeEndBeforeStart",
			initialContent: "package main",
			changes: []protocol.TextDocumentContentChangeEvent{
				{
					Range: &protocol.Range{
						Start: protocol.Position{Line: 0, Character: 5},
						End:   protocol.Position{Line: 0, Character: 3},
					},
					Text: "invalid",
				},
			},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			fset := token.NewFileSet()
			files := make(map[string]*xgo.File)
			path := "/test.xgo"

			// Create initial file
			files[path] = &xgo.File{
				Content: []byte(tt.initialContent),
				ModTime: time.Now(),
			}

			proj := xgo.NewProject(fset, files, xgo.FeatASTCache)

			// For AST parsing to work, we need a real file with content
			// parsed into the AST before we can apply changes
			_, err := proj.ASTFile(path)
			require.NoError(t, err)

			server := &Server{
				workspaceRootFS: proj,
			}

			// Execute test
			got, err := server.changedText(path, tt.changes)

			// Verify results
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if !tt.wantErr {
				assert.Equal(t, tt.want, string(got))
			}
		})
	}
}

// TestApplyIncrementalChanges tests the applyIncrementalChanges function
func TestApplyIncrementalChanges(t *testing.T) {
	tests := []struct {
		name           string
		initialContent string
		changes        []protocol.TextDocumentContentChangeEvent
		want           string
		wantErr        bool
	}{
		{
			name:           "AddTextAtBeginning",
			initialContent: "func main() {}",
			changes: []protocol.TextDocumentContentChangeEvent{
				{
					Range: &protocol.Range{
						Start: protocol.Position{Line: 0, Character: 0},
						End:   protocol.Position{Line: 0, Character: 0},
					},
					Text: "package main\n\n",
				},
			},
			want:    "package main\n\nfunc main() {}",
			wantErr: false,
		},
		{
			name:           "AddTextInMiddle",
			initialContent: "func main() {}",
			changes: []protocol.TextDocumentContentChangeEvent{
				{
					Range: &protocol.Range{
						Start: protocol.Position{Line: 0, Character: 13},
						End:   protocol.Position{Line: 0, Character: 13},
					},
					Text: "\n\tfmt.Println(\"Hello\")\n",
				},
			},
			want:    "func main() {\n\tfmt.Println(\"Hello\")\n}",
			wantErr: false,
		},
		{
			name:           "DeleteText",
			initialContent: "package main\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}",
			changes: []protocol.TextDocumentContentChangeEvent{
				{
					Range: &protocol.Range{
						Start: protocol.Position{Line: 2, Character: 0},
						End:   protocol.Position{Line: 4, Character: 1},
					},
					Text: "",
				},
			},
			want:    "package main\n\n",
			wantErr: false,
		},
		{
			name:           "ReplaceEntireLine",
			initialContent: "package main\n\nfunc main() {\n\tfmt.Println(\"Hello\")\n}",
			changes: []protocol.TextDocumentContentChangeEvent{
				{
					Range: &protocol.Range{
						Start: protocol.Position{Line: 3, Character: 0},
						End:   protocol.Position{Line: 3, Character: 21},
					},
					Text: "\tfmt.Println(\"Hello, World!\")",
				},
			},
			want:    "package main\n\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}",
			wantErr: false,
		},
		{
			name:           "NilRange",
			initialContent: "package main",
			changes: []protocol.TextDocumentContentChangeEvent{
				{
					Range: nil,
					Text:  "new content",
				},
			},
			want:    "",
			wantErr: true,
		},
		{
			name:           "NonExistentFile",
			initialContent: "",
			changes: []protocol.TextDocumentContentChangeEvent{
				{
					Range: &protocol.Range{
						Start: protocol.Position{Line: 0, Character: 0},
						End:   protocol.Position{Line: 0, Character: 0},
					},
					Text: "package main",
				},
			},
			want:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			fset := token.NewFileSet()
			files := make(map[string]*xgo.File)
			path := "/test.xgo"

			if tt.initialContent != "" {
				files[path] = &xgo.File{
					Content: []byte(tt.initialContent),
					ModTime: time.Now(),
				}
			}

			proj := xgo.NewProject(fset, files, xgo.FeatASTCache)

			// For tests with content, ensure we have AST
			if tt.initialContent != "" {
				_, err := proj.ASTFile(path)
				require.NoError(t, err)
			}

			server := &Server{
				workspaceRootFS: proj,
			}

			// Execute test
			got, err := server.applyIncrementalChanges(path, tt.changes)

			// Verify results
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			if !tt.wantErr {
				assert.Equal(t, tt.want, string(got))
			}
		})
	}
}

// TestGetDiagnostics tests the getDiagnostics function for generating diagnostic information
func TestGetDiagnostics(t *testing.T) {
	tests := []struct {
		name           string
		content        string
		path           string
		wantDiagCount  int
		wantSeverities []protocol.DiagnosticSeverity
		wantErr        bool
	}{
		{
			name:           "ImportErrors",
			content:        "package main\n\nfunc main() {\n\tfmt.Println(\"Hello, World!\")\n}",
			path:           "/test.xgo",
			wantDiagCount:  1,
			wantSeverities: []protocol.DiagnosticSeverity{SeverityError},
			wantErr:        false,
		},
		{
			name:           "SyntaxError",
			content:        "package main\n\nfunc main() {\n\tfmt.Println(\"Hello, World!\"\n}", // Missing closing parenthesis
			path:           "/syntax_error.xgo",
			wantDiagCount:  8,
			wantSeverities: []protocol.DiagnosticSeverity{SeverityError},
			wantErr:        false,
		},
		{
			name:           "TypeError",
			content:        "package main\n\nfunc main() {\n\tvar x int = \"string\"\n}", // Type mismatch
			path:           "/type_error.xgo",
			wantDiagCount:  1,
			wantSeverities: []protocol.DiagnosticSeverity{SeverityError},
			wantErr:        false,
		},
		{
			name:           "NoError",
			content:        "package main\n\nfunc main() {\n\t}",
			path:           "/code_error.xgo",
			wantDiagCount:  0,
			wantSeverities: []protocol.DiagnosticSeverity{},
			wantErr:        false,
		},
		{
			name:           "MultipleTypeErrors",
			content:        "package main\n\nfunc main() {\n\tvar x int = \"string\"\n\tvar y bool = 42\n}",
			path:           "/multiple_errors.xgo",
			wantDiagCount:  2,
			wantSeverities: []protocol.DiagnosticSeverity{SeverityError},
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test environment
			fset := token.NewFileSet()
			files := make(map[string]*xgo.File)

			// Create the test file
			files[tt.path] = &xgo.File{
				Content: []byte(tt.content),
				ModTime: time.Now(),
			}

			// Create a mock Project that returns our predefined errors
			server := &Server{
				workspaceRootFS: xgo.NewProject(fset, files, xgo.FeatAll),
			}

			// Execute test
			diagnostics, err := server.getDiagnostics(tt.path)

			// Verify results
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Len(t, diagnostics, tt.wantDiagCount)

			// Check diagnostic severities
			for i, diag := range diagnostics {
				if i >= len(tt.wantSeverities) {
					break
				}
				assert.Equal(t, tt.wantSeverities[i], diag.Severity)
			}
		})
	}
}
