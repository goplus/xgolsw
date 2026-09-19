package server

import (
	"encoding/json"
	gotypes "go/types"
	"maps"
	"sync"
	"testing"
	"testing/synctest"
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

type diagnosticReplierFunc func(jsonrpc2.Message) error

func (f diagnosticReplierFunc) ReplyMessage(message jsonrpc2.Message) error {
	return f(message)
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
	t.Run("InvalidUTF8Recovery", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("// \xff\nvar value = 1\n")})
		require.NotEmpty(t, s.getDiagnostics("main.xgo"))
		replier := newMockReplier()
		s.replier = replier
		uri := DocumentURI("file:///main.xgo")
		require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
			TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: TextDocumentIdentifier{URI: uri}, Version: 1},
			ContentChanges: []protocol.TextDocumentContentChangeEvent{{
				Range: &Range{Start: Position{Character: 3}, End: Position{Character: 4}}, Text: "repaired",
			}},
		}))
		assert.Empty(t, requirePublishedDiagnostics(t, replier, uri))
		current, ok := s.getProjWithFile().File("main.xgo")
		require.True(t, ok)
		assert.Equal(t, "// repaired\nvar value = 1\n", string(current.Content))
		assert.Equal(t, 1, current.Version)
		hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 1, Character: 4},
		}})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, "var value int")
	})

	for _, tt := range []struct {
		name        string
		filename    string
		projectFile string
		newServer   testServerFactory
	}{
		{name: "PlainXGo", filename: "main.xgo", newServer: newTestServer},
		{name: "ProjectClass", filename: "main_fixture.gox", newServer: newFrameworkTestServer},
		{name: "WorkClass", filename: "Worker_fixture.gox", projectFile: "main_fixture.gox", newServer: newFrameworkTestServer},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{tt.filename: nil}
			if tt.projectFile != "" {
				files[tt.projectFile] = nil
			}
			s := tt.newServer(t, files)
			provider := s.fileMapGetter
			modTime := time.UnixMilli(100)
			s.fileMapGetter = func() map[string]*xgo.File {
				provided := provider()
				for _, file := range provided {
					file.ModTime = modTime
				}
				return provided
			}
			s.getProjWithFile()
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
			assert.Equal(t, modTime, current.ModTime)

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
			assert.Equal(t, modTime, current.ModTime)
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
			assert.Equal(t, current.Version, saved.Version)
			assert.Equal(t, modTime, saved.ModTime)

			require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: TextDocumentIdentifier{URI: uri}, Version: 3},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{{
					Range: &Range{Start: Position{Character: 14}, End: Position{Character: 21}}, Text: "2",
				}},
			}))
			assert.Empty(t, requirePublishedDiagnostics(t, replier, uri))
			changed, ok := s.getProj().File(tt.filename)
			require.True(t, ok)
			assert.Equal(t, "println \"\U0001f600\", 2\n", string(changed.Content))
			assert.Equal(t, 3, changed.Version)
			assert.Equal(t, modTime, changed.ModTime)
			report, err = s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: uri}})
			require.NoError(t, err)
			assert.Empty(t, requireRelatedFullDocumentDiagnosticReport(t, report).Items)

			require.NoError(t, s.didClose(&DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: uri}}))
			assert.Empty(t, requirePublishedDiagnostics(t, replier, uri))
			require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{URI: uri, LanguageID: "xgo", Version: 0, Text: content},
			}))
			assert.Equal(t, want, requirePublishedDiagnostics(t, replier, uri))
			reopened, ok := s.getProj().File(tt.filename)
			require.True(t, ok)
			assert.Equal(t, content, string(reopened.Content))
			assert.Zero(t, reopened.Version)
			assert.Equal(t, modTime, reopened.ModTime)

			snapshot := s.getProj().Snapshot()
			require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: TextDocumentIdentifier{URI: uri}, Version: 1},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{
					{Text: "println \"\U0001f600\", other\n"},
					{Range: &Range{Start: Position{Character: 14}, End: Position{Character: 19}}, Text: "3"},
				},
			}))
			assert.Empty(t, requirePublishedDiagnostics(t, replier, uri))
			changed, ok = s.getProj().File(tt.filename)
			require.True(t, ok)
			assert.Equal(t, "println \"\U0001f600\", 3\n", string(changed.Content))
			assert.Equal(t, 1, changed.Version)
			assert.Equal(t, modTime, changed.ModTime)
			report, err = s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: uri}})
			require.NoError(t, err)
			assert.Empty(t, requireRelatedFullDocumentDiagnosticReport(t, report).Items)
			original, ok := snapshot.File(tt.filename)
			require.True(t, ok)
			assert.Same(t, reopened, original)
			assert.Equal(t, content, string(original.Content))
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

func TestServerPublishFileDiagnostics(t *testing.T) {
	t.Run("DependencyChangedDuringAnalysis", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			change  func(*testing.T, *Server)
			reports int
		}{
			{"DocumentChange", func(t *testing.T, s *Server) {
				require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
					TextDocument: protocol.VersionedTextDocumentIdentifier{
						TextDocumentIdentifier: TextDocumentIdentifier{URI: "file:///values.xgo"}, Version: 1,
					},
					ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: "const replacement = 1\n"}},
				}))
			}, 2},
			{"ProviderChange", func(t *testing.T, s *Server) {
				files := maps.Collect(s.getProj().Files())
				files["values.xgo"] = &xgo.File{Content: []byte("const replacement = 1\n"), ModTime: time.Unix(1, 0)}
				s.fileMapGetter = func() map[string]*xgo.File { return files }
				s.getProjWithFile()
			}, 1},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte("println shared\n"), "values.xgo": []byte("const shared = 1\n"),
				})
				proj := s.getProj()
				_, err := proj.TypeInfo()
				require.NoError(t, err)
				dependency := gotypes.NewPackage("example.com/paused", "paused")
				dependency.Scope().Insert(gotypes.NewVar(0, dependency, "Value", gotypes.Typ[gotypes.Int]))
				dependency.MarkComplete()
				replier := newMockReplier()
				s.replier = replier
				synctest.Test(t, func(t *testing.T) {
					entered, resume := make(chan struct{}), make(chan struct{})
					release := sync.OnceFunc(func() { close(resume) })
					t.Cleanup(release)
					fallback := proj.Importer
					pause := sync.OnceFunc(func() {
						close(entered)
						<-resume
					})
					proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
						if path == dependency.Path() {
							pause()
							return dependency, nil
						}
						return fallback.Import(path)
					})
					require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
						TextDocument: protocol.TextDocumentItem{
							URI: "file:///main.xgo", LanguageID: "xgo", Version: 1,
							Text: "import \"example.com/paused\"\nprintln paused.Value, shared\n",
						},
					}))
					<-entered
					tt.change(t, s)
					release()
					synctest.Wait()
					messages := replier.getMessages()
					require.Len(t, messages, tt.reports)
					reports := make(map[DocumentURI][]Diagnostic)
					for _, message := range messages {
						notification := requireValueAs[*jsonrpc2.Notification](t, message)
						var params PublishDiagnosticsParams
						require.NoError(t, json.Unmarshal(notification.Params(), &params))
						reports[params.URI] = params.Diagnostics
					}
					want := s.getDiagnostics("main.xgo")
					require.Len(t, want, 1)
					assert.Equal(t, "undefined: shared", want[0].Message)
					assert.Equal(t, want, reports["file:///main.xgo"])
					assert.Empty(t, reports["file:///values.xgo"])
				})
			})
		}
	})

	t.Run("MultipleDocuments", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"first.xgo":  []byte("func checkFirst() { println first }\n"),
			"second.xgo": []byte("func checkSecond() { println second }\n"),
		})
		_, err := s.getProj().TypeInfo()
		require.Error(t, err)
		replier := newMockReplier()
		s.replier = replier
		synctest.Test(t, func(t *testing.T) {
			s.publishFileDiagnostics("first.xgo")
			s.publishFileDiagnostics("second.xgo")
			synctest.Wait()
			messages := replier.getMessages()
			require.Len(t, messages, 2)
			reports := make(map[DocumentURI]string)
			for _, message := range messages {
				notification := requireValueAs[*jsonrpc2.Notification](t, message)
				var params PublishDiagnosticsParams
				require.NoError(t, json.Unmarshal(notification.Params(), &params))
				require.Len(t, params.Diagnostics, 1)
				reports[params.URI] = params.Diagnostics[0].Message
			}
			assert.Equal(t, map[DocumentURI]string{
				"file:///first.xgo": "undefined: first", "file:///second.xgo": "undefined: second",
			}, reports)
		})
	})

	t.Run("ReentrantClose", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("println missing\n")})
		_, err := s.getProj().TypeInfo()
		require.Error(t, err)
		replier := newMockReplier()
		synctest.Test(t, func(t *testing.T) {
			var closed bool
			var closeErr error
			s.replier = diagnosticReplierFunc(func(message jsonrpc2.Message) error {
				if err := replier.ReplyMessage(message); err != nil {
					return err
				}
				if !closed {
					closed = true
					closeErr = s.didClose(&DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}})
				}
				return closeErr
			})
			s.publishFileDiagnostics("main.xgo")
			synctest.Wait()
			require.NoError(t, closeErr)
			messages := replier.getMessages()
			require.Len(t, messages, 2)
			for i, message := range messages {
				notification := requireValueAs[*jsonrpc2.Notification](t, message)
				var params PublishDiagnosticsParams
				require.NoError(t, json.Unmarshal(notification.Params(), &params))
				if i == 0 {
					require.Len(t, params.Diagnostics, 1)
					assert.Equal(t, "undefined: missing", params.Diagnostics[0].Message)
				} else {
					assert.Empty(t, params.Diagnostics)
				}
			}
		})
	})

	for _, tt := range []struct {
		name string
		next func(*Server, DocumentURI) error
		want []string
	}{
		{"Changed", func(s *Server, uri DocumentURI) error {
			return s.didChange(&DidChangeTextDocumentParams{
				TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: TextDocumentIdentifier{URI: uri}, Version: 2},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: "println after\n"}},
			})
		}, []string{"undefined: after"}},
		{"Closed", func(s *Server, uri DocumentURI) error {
			return s.didClose(&DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: uri}})
		}, nil},
		{"Deleted", func(s *Server, uri DocumentURI) error {
			return s.getProj().DeleteFile("main.xgo")
		}, nil},
		{"Renamed", func(s *Server, uri DocumentURI) error {
			return s.getProj().RenameFile("main.xgo", "renamed.xgo")
		}, nil},
		{"Reopened", func(s *Server, uri DocumentURI) error {
			if err := s.didClose(&DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: uri}}); err != nil {
				return err
			}
			return s.didOpen(&DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{URI: uri, LanguageID: "xgo", Version: 0, Text: "println reopened\n"},
			})
		}, []string{"undefined: reopened"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("println 1\n")})
			proj := s.getProj()
			_, err := proj.TypeInfo()
			require.NoError(t, err)
			dependency := gotypes.NewPackage("example.com/paused", "paused")
			dependency.Scope().Insert(gotypes.NewVar(0, dependency, "Value", gotypes.Typ[gotypes.Int]))
			dependency.MarkComplete()
			replier := newMockReplier()
			s.replier = replier
			synctest.Test(t, func(t *testing.T) {
				entered, resume := make(chan struct{}), make(chan struct{})
				release := sync.OnceFunc(func() { close(resume) })
				t.Cleanup(release)
				fallback := proj.Importer
				pause := sync.OnceFunc(func() {
					close(entered)
					<-resume
				})
				proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
					if path == dependency.Path() {
						pause()
						return dependency, nil
					}
					return fallback.Import(path)
				})
				uri := DocumentURI("file:///main.xgo")
				require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
					TextDocument: protocol.TextDocumentItem{
						URI: uri, LanguageID: "xgo", Version: 1,
						Text: "import \"example.com/paused\"\nprintln paused.Value, before\n",
					},
				}))
				<-entered
				require.NoError(t, tt.next(s, uri))
				release()
				synctest.Wait()
				messages := replier.getMessages()
				require.Len(t, messages, 1)
				notification := requireValueAs[*jsonrpc2.Notification](t, messages[0])
				var params PublishDiagnosticsParams
				require.NoError(t, json.Unmarshal(notification.Params(), &params))
				assert.Equal(t, uri, params.URI)
				var diagnostics []string
				for _, diagnostic := range params.Diagnostics {
					diagnostics = append(diagnostics, diagnostic.Message)
				}
				assert.Equal(t, tt.want, diagnostics)
			})
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
		{name: "InvalidBatch", run: func(s *Server) error {
			return s.didChange(&DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: TextDocumentIdentifier{URI: "file:///workspace/main.xgo"}, Version: 1},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{
					{Text: "println 2\n"},
					{Range: &Range{Start: Position{Character: 4}, End: Position{Character: 2}}},
				},
			})
		}, want: "invalid range"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("println 1")})
			s.workspaceRootURI = "file:///workspace/"
			replier := newMockReplier()
			s.replier = replier
			info, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			require.ErrorContains(t, tt.run(s), tt.want)
			assert.Empty(t, replier.getMessages())
			current, ok := s.getProj().File("main.xgo")
			require.True(t, ok)
			assert.Equal(t, "println 1", string(current.Content))
			assert.Zero(t, current.Version)
			unchanged, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			assert.Same(t, info, unchanged)
		})
	}
}

func TestServerChangedText(t *testing.T) {
	t.Run("InvalidUTF8", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("// \xff")})
		got, err := s.changedText("main.xgo", []protocol.TextDocumentContentChangeEvent{{
			Range: &Range{Start: Position{Character: 3}, End: Position{Character: 4}}, Text: "repaired",
		}})
		require.NoError(t, err)
		assert.Equal(t, "// repaired", string(got))
		current, ok := s.getProj().File("main.xgo")
		require.True(t, ok)
		assert.Equal(t, "// \xff", string(current.Content))
	})

	for _, tt := range []struct {
		name     string
		filename string
		changes  []protocol.TextDocumentContentChangeEvent
		want     string
		wantErr  string
	}{
		{name: "FullReplacement", changes: []protocol.TextDocumentContentChangeEvent{{Text: "println 2\n"}}, want: "println 2\n"},
		{name: "EmptyChanges", wantErr: "no content changes provided"},
		{name: "FullThenIncremental", changes: []protocol.TextDocumentContentChangeEvent{
			{Text: "println \"\U0001f600\"\n"},
			{Range: &Range{Start: Position{Character: 11}, End: Position{Character: 11}}, Text: "!"},
		}, want: "println \"\U0001f600!\"\n"},
		{name: "IncrementalThenFull", changes: []protocol.TextDocumentContentChangeEvent{
			{Range: &Range{Start: Position{Character: 9}, End: Position{Character: 14}}, Text: "world"},
			{Text: "println 3\n"},
		}, want: "println 3\n"},
		{name: "MultipleFullReplacements", changes: []protocol.TextDocumentContentChangeEvent{
			{Text: "println 2\n"},
			{Text: "println 3\n"},
		}, want: "println 3\n"},
		{name: "EmptyThenIncremental", changes: []protocol.TextDocumentContentChangeEvent{
			{Text: ""},
			{Range: &Range{}, Text: "println 2\n"},
		}, want: "println 2\n"},
		{name: "Beginning", changes: []protocol.TextDocumentContentChangeEvent{{Range: &Range{}, Text: "// hello\n"}}, want: "// hello\nprintln \"hello\"\n"},
		{name: "Middle", changes: []protocol.TextDocumentContentChangeEvent{{Range: &Range{Start: Position{Character: 14}, End: Position{Character: 14}}, Text: ", world"}}, want: "println \"hello, world\"\n"},
		{name: "Delete", changes: []protocol.TextDocumentContentChangeEvent{{Range: &Range{Start: Position{}, End: Position{Line: 1}}}}, want: ""},
		{name: "Replace", changes: []protocol.TextDocumentContentChangeEvent{{Range: &Range{Start: Position{Character: 9}, End: Position{Character: 14}}, Text: "world"}}, want: "println \"world\"\n"},
		{name: "SequentialUTF16", changes: []protocol.TextDocumentContentChangeEvent{
			{Range: &Range{Start: Position{Character: 9}, End: Position{Character: 14}}, Text: "\U0001f600"},
			{Range: &Range{Start: Position{Character: 11}, End: Position{Character: 11}}, Text: "!"},
		}, want: "println \"\U0001f600!\"\n"},
		{name: "ReversedRange", changes: []protocol.TextDocumentContentChangeEvent{{Range: &Range{Start: Position{Character: 4}, End: Position{Character: 2}}}}, wantErr: "invalid range"},
		{name: "MissingFile", filename: "missing.xgo", changes: []protocol.TextDocumentContentChangeEvent{{Range: &Range{}, Text: "println 2\n"}}, wantErr: "file not found"},
		{name: "MissingFileFullReplacement", filename: "missing.xgo", changes: []protocol.TextDocumentContentChangeEvent{{Text: "println 2\n"}}, want: "println 2\n"},
		{name: "MissingFileFullThenIncremental", filename: "missing.xgo", changes: []protocol.TextDocumentContentChangeEvent{
			{Text: "println \"\U0001f600\"\n"},
			{Range: &Range{Start: Position{Character: 11}, End: Position{Character: 11}}, Text: "!"},
		}, want: "println \"\U0001f600!\"\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("println \"hello\"\n")})
			filename := tt.filename
			if filename == "" {
				filename = "main.xgo"
			}
			got, err := s.changedText(filename, tt.changes)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, string(got))
			}
			current, ok := s.getProj().File("main.xgo")
			require.True(t, ok)
			assert.Equal(t, "println \"hello\"\n", string(current.Content))
			_, ok = s.getProj().File("missing.xgo")
			assert.False(t, ok)
		})
	}
}
