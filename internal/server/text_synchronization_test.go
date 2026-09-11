package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/protocol"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func file(text string) *xgo.File {
	return &xgo.File{Content: []byte(text)}
}

func requirePublishedDiagnostics(t *testing.T, replier *mockReplier, uri DocumentURI) []Diagnostic {
	t.Helper()

	messages := replier.waitForMessages(1, 5*time.Second)
	require.Len(t, messages, 1)
	notification := requireValueAs[*jsonrpc2.Notification](t, messages[0])
	require.Equal(t, "textDocument/publishDiagnostics", notification.Method())
	var params PublishDiagnosticsParams
	require.NoError(t, json.Unmarshal(notification.Params(), &params))
	assert.Equal(t, uri, params.URI)
	require.NotNil(t, params.Diagnostics)
	replier.clearMessages()
	return params.Diagnostics
}

func TestServerModifyFiles(t *testing.T) {
	for _, tt := range []struct {
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
					Path:    "new.xgo",
					Content: []byte("package main"),
					Version: 100,
				},
			},
			want: map[string]string{
				"new.xgo": "package main",
			},
		},
		{
			name: "UpdateExistingFileWithNewerVersion",
			initial: map[string]*xgo.File{
				"main.xgo": {
					Content: []byte("old content"),
					ModTime: time.UnixMilli(100),
				},
			},
			changes: []FileChange{
				{
					Path:    "main.xgo",
					Content: []byte("new content"),
					Version: 200,
				},
			},
			want: map[string]string{
				"main.xgo": "new content",
			},
		},
		{
			name: "IgnoreOlderVersionUpdate",
			initial: map[string]*xgo.File{
				"main.xgo": {
					Content: []byte("current content"),
					Version: 200,
				},
			},
			changes: []FileChange{
				{
					Path:    "main.xgo",
					Content: []byte("old content"),
					Version: 100,
				},
			},
			want: map[string]string{
				"main.xgo": "current content",
			},
		},
		{
			name: "MultipleFileChanges",
			initial: map[string]*xgo.File{
				"file1.xgo": {
					Content: []byte("content1"),
					ModTime: time.UnixMilli(100),
				},
				"file2.xgo": {
					Content: []byte("content2"),
					ModTime: time.UnixMilli(100),
				},
			},
			changes: []FileChange{
				{
					Path:    "file1.xgo",
					Content: []byte("new content1"),
					Version: 200,
				},
				{
					Path:    "file3.xgo",
					Content: []byte("content3"),
					Version: 200,
				},
			},
			want: map[string]string{
				"file1.xgo": "new content1",
				"file2.xgo": "content2",
				"file3.xgo": "content3",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			// Create new project with initial files
			proj := xgo.NewProject(nil, tt.initial, xgo.FeatAll)

			// Create a server with versioned files
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

func TestServerDocumentSynchronization(t *testing.T) {
	for _, tt := range []struct {
		name        string
		filename    string
		projectFile string
	}{
		{name: "PlainXGo", filename: "main.xgo"},
		{name: "ProjectClass", filename: "main_fixture.gox"},
		{name: "WorkClass", filename: "Worker_fixture.gox", projectFile: "main_fixture.gox"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{tt.filename: nil}
			if tt.projectFile != "" {
				files[tt.projectFile] = nil
			}
			s := newTestServer(t, files)
			replier := newMockReplier()
			s.replier = replier
			uri := s.toDocumentURI(tt.filename)
			content := "println \"\U0001f600\", missing\n"
			want := []Diagnostic{{
				Severity: SeverityError,
				Message:  "undefined: missing",
				Range:    Range{Start: Position{Character: 14}, End: Position{Character: 21}},
			}}

			require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{URI: uri, LanguageID: "xgo", Version: 1, Text: content},
			}))
			assert.Equal(t, want, requirePublishedDiagnostics(t, replier, uri))
			current, ok := s.getProj().File(tt.filename)
			require.True(t, ok)
			assert.Equal(t, content, string(current.Content))
			assert.Equal(t, 1, current.Version)

			report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: uri}})
			require.NoError(t, err)
			assert.Equal(t, want, requireRelatedFullDocumentDiagnosticReport(t, report).Items)

			require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: TextDocumentIdentifier{URI: uri}, Version: 2},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{{
					Range: &Range{Start: Position{Character: 14}, End: Position{Character: 21}}, Text: "1",
				}},
			}))
			assert.Empty(t, requirePublishedDiagnostics(t, replier, uri))
			current, ok = s.getProj().File(tt.filename)
			require.True(t, ok)
			assert.Equal(t, "println \"\U0001f600\", 1\n", string(current.Content))
			assert.Equal(t, 2, current.Version)
			report, err = s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: uri}})
			require.NoError(t, err)
			assert.Empty(t, requireRelatedFullDocumentDiagnosticReport(t, report).Items)

			require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
				TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: TextDocumentIdentifier{URI: uri}, Version: 1},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: content}},
			}))
			assert.Empty(t, requirePublishedDiagnostics(t, replier, uri))
			unchanged, ok := s.getProj().File(tt.filename)
			require.True(t, ok)
			assert.Same(t, current, unchanged)

			require.NoError(t, s.didSave(&DidSaveTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: uri}}))
			assert.Empty(t, replier.getMessages())
			unchanged, ok = s.getProj().File(tt.filename)
			require.True(t, ok)
			assert.Same(t, current, unchanged)

			require.NoError(t, s.didSave(&DidSaveTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: uri}, Text: &content}))
			assert.Equal(t, want, requirePublishedDiagnostics(t, replier, uri))
			saved, ok := s.getProj().File(tt.filename)
			require.True(t, ok)
			assert.Equal(t, content, string(saved.Content))
			assert.Greater(t, saved.Version, current.Version)

			require.NoError(t, s.didClose(&DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: uri}}))
			assert.Empty(t, requirePublishedDiagnostics(t, replier, uri))
		})
	}
}

func TestServerDidOpen(t *testing.T) {
	for _, tt := range []struct {
		name    string
		content string
	}{
		{name: "Basic", content: "println 100"},
		{name: "Function", content: "func hello() {\n    println \"hello\"\n}\nhello\n"},
		{name: "Unicode", content: "println \"\u4f60\u597d\U0001f600\""},
		{name: "Empty"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, nil)
			replier := newMockReplier()
			s.replier = replier
			uri := DocumentURI("file:///main.xgo")
			require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{URI: uri, LanguageID: "xgo", Version: 2, Text: tt.content},
			}))
			assert.Empty(t, requirePublishedDiagnostics(t, replier, uri))
			opened, ok := s.getProj().File("main.xgo")
			require.True(t, ok)
			assert.Equal(t, tt.content, string(opened.Content))
			assert.Equal(t, 2, opened.Version)
		})
	}
}

func TestServerDocumentSynchronizationErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		run  func(*Server) error
		want string
	}{
		{name: "OpenOutsideWorkspace", run: func(s *Server) error {
			return s.didOpen(&DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: "file:///outside/main.xgo"}})
		}, want: "outside"},
		{name: "ChangeOutsideWorkspace", run: func(s *Server) error {
			return s.didChange(&DidChangeTextDocumentParams{TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: TextDocumentIdentifier{URI: "file:///outside/main.xgo"}}})
		}, want: "outside"},
		{name: "SaveOutsideWorkspace", run: func(s *Server) error {
			return s.didSave(&DidSaveTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: "file:///outside/main.xgo"}, Text: ToPtr("println 1")})
		}, want: "outside"},
		{name: "EmptyChanges", run: func(s *Server) error {
			return s.didChange(&DidChangeTextDocumentParams{TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: TextDocumentIdentifier{URI: "file:///workspace/main.xgo"}}})
		}, want: "no content changes provided"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("println 1")})
			s.workspaceRootURI = "file:///workspace/"
			replier := newMockReplier()
			s.replier = replier
			require.ErrorContains(t, tt.run(s), tt.want)
			assert.Empty(t, replier.getMessages())
			current, ok := s.getProj().File("main.xgo")
			require.True(t, ok)
			assert.Equal(t, "println 1", string(current.Content))
		})
	}
}

func TestServerChangedText(t *testing.T) {
	for _, tt := range []struct {
		name    string
		changes []protocol.TextDocumentContentChangeEvent
		want    string
		wantErr string
	}{
		{name: "FullReplacement", changes: []protocol.TextDocumentContentChangeEvent{{Text: "println 2\n"}}, want: "println 2\n"},
		{name: "EmptyChanges", wantErr: "no content changes provided"},
		{name: "Incremental", changes: []protocol.TextDocumentContentChangeEvent{{Range: &Range{Start: Position{Character: 8}, End: Position{Character: 9}}, Text: "2"}}, want: "println 2\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("println 1\n")})
			got, err := s.changedText("main.xgo", tt.changes)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, string(got))
			}
		})
	}
}

func TestServerApplyIncrementalChanges(t *testing.T) {
	for _, tt := range []struct {
		name     string
		filename string
		changes  []protocol.TextDocumentContentChangeEvent
		want     string
		wantErr  string
	}{
		{name: "Beginning", changes: []protocol.TextDocumentContentChangeEvent{{Range: &Range{}, Text: "// hello\n"}}, want: "// hello\nprintln \"hello\"\n"},
		{name: "Middle", changes: []protocol.TextDocumentContentChangeEvent{{Range: &Range{Start: Position{Character: 14}, End: Position{Character: 14}}, Text: ", world"}}, want: "println \"hello, world\"\n"},
		{name: "Delete", changes: []protocol.TextDocumentContentChangeEvent{{Range: &Range{Start: Position{}, End: Position{Line: 1}}}}, want: ""},
		{name: "Replace", changes: []protocol.TextDocumentContentChangeEvent{{Range: &Range{Start: Position{Character: 9}, End: Position{Character: 14}}, Text: "world"}}, want: "println \"world\"\n"},
		{name: "SequentialUTF16", changes: []protocol.TextDocumentContentChangeEvent{
			{Range: &Range{Start: Position{Character: 9}, End: Position{Character: 14}}, Text: "\U0001f600"},
			{Range: &Range{Start: Position{Character: 11}, End: Position{Character: 11}}, Text: "!"},
		}, want: "println \"\U0001f600!\"\n"},
		{name: "NilRange", changes: []protocol.TextDocumentContentChangeEvent{{Text: "println 2"}}, wantErr: "unexpected nil range"},
		{name: "ReversedRange", changes: []protocol.TextDocumentContentChangeEvent{{Range: &Range{Start: Position{Character: 4}, End: Position{Character: 2}}}}, wantErr: "invalid range"},
		{name: "MissingFile", filename: "missing.xgo", wantErr: "file not found"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("println \"hello\"\n")})
			filename := tt.filename
			if filename == "" {
				filename = "main.xgo"
			}
			got, err := s.applyIncrementalChanges(filename, tt.changes)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, string(got))
			}
		})
	}
}
