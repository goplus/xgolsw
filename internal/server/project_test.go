package server

import (
	"context"
	"encoding/json"
	"fmt"
	gotypes "go/types"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/protocol"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerRequestProject(t *testing.T) {
	t.Run("SourcePositionLifetime", func(t *testing.T) {
		for _, kind := range []struct {
			name      string
			filename  string
			newServer testServerFactory
		}{
			{"PlainXGo", "main.xgo", newTestServer},
			{"NormalClass", "Record.gox", newTestServer},
			{"ProjectClass", "main_fixture.gox", newFrameworkTestServer},
			{"WorkClass", "Worker_fixture.gox", newFrameworkTestServer},
		} {
			t.Run(kind.name, func(t *testing.T) {
				prefix := strings.Repeat("// Source line.\n", 128)
				files := map[string][]byte{kind.filename: []byte(prefix + "var value = 0\n")}
				if kind.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := kind.newServer(t, files)
				original := s.requestProject()
				originalAST, err := original.ASTFile(kind.filename)
				require.NoError(t, err)
				originalPos := original.Fset.PositionFor(originalAST.End(), false)
				originalInfo, err := original.TypeInfo()
				require.NoError(t, err)
				sourceFiles := func(fset *token.FileSet) []*token.File {
					var files []*token.File
					fset.Iterate(func(file *token.File) bool {
						if file.Name() == kind.filename {
							files = append(files, file)
						}
						return true
					})
					return files
				}
				for version := 1; version <= 16; version++ {
					s.ModifyFiles([]FileChange{{
						Path: kind.filename, Version: version,
						Content: []byte(prefix + fmt.Sprintf("var value = %d\n", version)),
					}})
					current := s.requestProject()
					_, err := current.TypeInfo()
					require.NoError(t, err)
					require.Len(t, sourceFiles(current.Fset), 1, "the current revision must not retain obsolete source position records")
					assert.Same(t, current, s.requestProject())
				}
				assert.Empty(t, sourceFiles(s.getProj().Fset), "the workspace must not retain request position records")
				delete(files, kind.filename)
				assert.Empty(t, sourceFiles(s.requestProject().Fset))

				// In-flight requests still own their original source positions and analysis.
				assert.Len(t, sourceFiles(original.Fset), 1)
				assert.Equal(t, originalPos, original.Fset.PositionFor(originalAST.End(), false))
				info, err := original.TypeInfo()
				require.NoError(t, err)
				assert.Same(t, originalInfo, info)
				assert.Equal(t, prefix+"var value = 0\n", string(originalAST.Code))
			})
		}
	})

	t.Run("SharedAnalysis", func(t *testing.T) {
		s, builds := newAnalysisTestServer(t)
		const count = 16
		projects := make([]*xgo.Project, count)
		analyses := make([]*frameworkAnalysis, count)
		errs := make([]error, count)
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range count {
			wg.Go(func() {
				<-start
				projects[i] = s.requestProject()
				analyses[i], errs[i] = analyzeFramework(projects[i])
			})
		}
		close(start)
		wg.Wait()
		for i := range count {
			require.NoError(t, errs[i])
			assert.Same(t, projects[0], projects[i])
			assert.Same(t, analyses[0], analyses[i])
		}
		assert.EqualValues(t, 1, builds.Load())
		original := projects[0]
		info, err := original.TypeInfo()
		require.NoError(t, err)
		astFile, err := original.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(`func use(name string) {}
use "Other"
`), Version: 1}})
		current := s.requestProject()
		assert.NotSame(t, original, current)
		assert.Same(t, current, s.requestProject())
		analysis, err := analyzeFramework(current)
		require.NoError(t, err)
		assert.NotSame(t, analyses[0], analysis)
		assert.EqualValues(t, 2, builds.Load())
		originalInfo, err := original.TypeInfo()
		require.NoError(t, err)
		assert.Same(t, info, originalInfo)
		originalAST, err := original.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		assert.Same(t, astFile, originalAST)
		assert.Contains(t, string(originalAST.Code), "Known")
		assert.NotContains(t, string(originalAST.Code), "Other")
	})

	t.Run("ModuleChange", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": nil, "Worker_fixture.gox": nil})
		before := s.requestProject()
		mod := testframework.NewModule(t)
		mod.Opt.Projects[0].Works[0].Prefix = "Actor"
		s.getProj().SetModule(newTestModule(t, mod.Module))
		after := s.requestProject()
		assert.NotSame(t, before, after)
		assert.NotSame(t, before.Module(), after.Module())
		assert.Same(t, after, s.requestProject())
		beforeInfo, err := before.TypeInfo()
		require.NoError(t, err)
		assert.NotNil(t, beforeInfo.Pkg.Scope().Lookup("Worker"))
		assert.Nil(t, beforeInfo.Pkg.Scope().Lookup("ActorWorker"))
		afterInfo, err := after.TypeInfo()
		require.NoError(t, err)
		assert.Nil(t, afterInfo.Pkg.Scope().Lookup("Worker"))
		assert.NotNil(t, afterInfo.Pkg.Scope().Lookup("ActorWorker"))
	})
}

func TestServerRequestsWithIncompletePackage(t *testing.T) {
	for _, kind := range []struct {
		name      string
		filename  string
		newServer testServerFactory
	}{
		{"PlainXGo", "main.xgo", newTestServer},
		{"NormalClass", "Record.gox", newTestServer},
		{"ProjectClass", "main_fixture.gox", newFrameworkTestServer},
		{"WorkClass", "Worker_fixture.gox", newFrameworkTestServer},
	} {
		t.Run(kind.name, func(t *testing.T) {
			for _, request := range []struct {
				name string
				read func(*Server, TextDocumentPositionParams) (any, error)
			}{
				{"Declaration", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentDeclaration(&DeclarationParams{TextDocumentPositionParams: p})
				}},
				{"Definition", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: p})
				}},
				{"TypeDefinition", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentTypeDefinition(&TypeDefinitionParams{TextDocumentPositionParams: p})
				}},
				{"Implementation", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: p})
				}},
				{"References", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: p})
				}},
				{"Highlight", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: p})
				}},
				{"PrepareRename", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: p})
				}},
				{"Rename", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentRename(&RenameParams{TextDocument: p.TextDocument, Position: p.Position, NewName: "renamed"})
				}},
			} {
				t.Run(request.name, func(t *testing.T) {
					const source = `type Value int
func run() {
var value Value
println value
}
`
					files := map[string][]byte{kind.filename: []byte(source)}
					if kind.name == "WorkClass" {
						files["main_fixture.gox"] = nil
					}
					s := kind.newServer(t, files)
					params := TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(kind.filename)},
						Position:     Position{Line: 3, Character: 9},
					}
					before, err := request.read(s, params)
					require.NoError(t, err)
					require.NotEmpty(t, before)

					s.ModifyFiles([]FileChange{{Path: kind.filename, Content: []byte("package"), Version: 1}})
					astFile, err := s.requestProject().ASTFile(kind.filename)
					require.Error(t, err)
					require.NotNil(t, astFile)
					require.False(t, astFile.Pos().IsValid())
					for _, pos := range []Position{{}, {Character: 7}} {
						incomplete := params
						incomplete.Position = pos
						result, err := request.read(s, incomplete)
						require.NoError(t, err)
						assert.Nil(t, result)
					}
					report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: params.TextDocument})
					require.NoError(t, err)
					assert.NotEmpty(t, requireRelatedFullDocumentDiagnosticReport(t, report).Items)

					s.ModifyFiles([]FileChange{{Path: kind.filename, Content: []byte(source), Version: 2}})
					after, err := request.read(s, params)
					require.NoError(t, err)
					assert.Equal(t, before, after)
				})
			}
		})
	}
}

func TestServerProviderReadOrdering(t *testing.T) {
	for _, state := range []string{"Updated", "Deleted"} {
		t.Run(state, func(t *testing.T) {
			for _, operation := range []string{"Request", "Open", "Close", "ReentrantRequest"} {
				t.Run(operation, func(t *testing.T) {
					const filename = "values.xgo"
					const uri DocumentURI = "file:///values.xgo"
					const original = "const oldValue = 1\n"
					const updated = "const newValue = 2\n"
					s := newTestServer(t, map[string][]byte{filename: []byte(original)})
					replier := newMockReplier()
					s.replier = replier
					if operation == "Close" {
						require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
							TextDocument: protocol.TextDocumentItem{URI: uri, Version: 1, Text: "const editorValue = 3\n"},
						}))
						requirePublishedDiagnostics(t, replier, uri)
					}
					entered, resume := make(chan struct{}), make(chan struct{})
					release := sync.OnceFunc(func() { close(resume) })
					t.Cleanup(release)
					var calls atomic.Int32
					s.fileMapGetter = func() map[string]*xgo.File {
						if calls.Add(1) == 1 {
							if operation == "ReentrantRequest" {
								s.syncProject()
							} else {
								close(entered)
								<-resume
							}
							return map[string]*xgo.File{filename: {Content: []byte(original), ModTime: time.Unix(1, 0)}}
						}
						if state == "Deleted" {
							return nil
						}
						return map[string]*xgo.File{filename: {Content: []byte(updated), ModTime: time.Unix(2, 0)}}
					}
					var err error
					if operation == "ReentrantRequest" {
						s.syncProject()
					} else {
						var wg sync.WaitGroup
						wg.Go(func() {
							switch operation {
							case "Request":
								s.syncProject()
							case "Open":
								err = s.didOpen(&DidOpenTextDocumentParams{
									TextDocument: protocol.TextDocumentItem{URI: "file:///editor.xgo", Version: 1, Text: "println 1\n"},
								})
							case "Close":
								err = s.didClose(&DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: uri}})
							}
						})
						<-entered
						s.syncProject()
						release()
						wg.Wait()
					}
					require.NoError(t, err)
					current, ok := s.getProj().File(filename)
					if state == "Deleted" {
						assert.False(t, ok, "a delayed provider result must not restore deleted files")
					} else {
						require.True(t, ok)
						assert.Equal(t, updated, string(current.Content), "a delayed provider result must not replace newer files")
					}
					if operation == "Open" {
						requirePublishedDiagnostics(t, replier, "file:///editor.xgo")
					} else if operation == "Close" {
						requirePublishedDiagnostics(t, replier, uri)
					}
				})
			}
		})
	}
}

func TestServerRequestConcurrentFrameworkImport(t *testing.T) {
	s := newSpxTestServer(t, map[string][]byte{"values.xgo": []byte("const value = 1\n")})
	var activeImports, concurrentImports atomic.Int32
	base := s.getProj().Importer
	s.getProj().Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
		if activeImports.Add(1) > 1 {
			concurrentImports.Add(1)
		}
		defer activeImports.Add(-1)
		runtime.Gosched()
		return base.Import(path)
	})
	old := s.requestProject()
	current := old.Fork()
	current.PutFile("main.spx", &xgo.File{Content: []byte("println 1\n")})
	var adapter frameworkAdapter
	var err error
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Go(func() {
		<-start
		adapter = resolveFrameworkAdapter(old)
	})
	wg.Go(func() {
		<-start
		_, err = current.TypeInfo()
	})
	close(start)
	wg.Wait()
	require.NoError(t, err)
	require.NotNil(t, adapter)
	assert.Zero(t, concurrentImports.Load(), "symbol imports and type checking must share the importer lock")
}

func TestServerDocumentSourceOwnership(t *testing.T) {
	const diskSource = "const shared = 1\n"
	const editorSource = "const edited = 2\n"
	const changedSource = "const changed = 3\n"
	for _, kind := range []struct {
		name      string
		filename  string
		newServer testServerFactory
	}{
		{"PlainXGo", "values.xgo", newTestServer},
		{"ProjectClass", "main_fixture.gox", newFrameworkTestServer},
		{"WorkClass", "Worker_fixture.gox", newFrameworkTestServer},
	} {
		t.Run(kind.name, func(t *testing.T) {
			for _, tt := range []struct {
				name     string
				existing bool
				provided bool
				change   bool
				remove   bool
				save     bool
				close    bool
				want     string
			}{
				{name: "OpenNewProviderFile", provided: true, want: editorSource},
				{name: "OpenUnsavedFile", want: editorSource},
				{name: "ProviderChangedWhileOpen", existing: true, change: true, want: editorSource},
				{name: "ProviderDeletedWhileOpen", existing: true, remove: true, want: editorSource},
				{name: "CloseWithoutSaving", existing: true, close: true, want: diskSource},
				{name: "CloseAfterProviderChange", existing: true, change: true, close: true, want: changedSource},
				{name: "CloseAfterProviderDeletion", existing: true, remove: true, close: true},
				{name: "CloseUnsavedFile", close: true},
				{name: "SaveThenClose", existing: true, save: true, close: true, want: changedSource},
			} {
				t.Run(tt.name, func(t *testing.T) {
					initial := map[string][]byte{"helper.xgo": []byte("const helper = 1\n")}
					if kind.filename == "Worker_fixture.gox" {
						initial["main_fixture.gox"] = nil
					}
					if tt.existing {
						initial[kind.filename] = []byte(diskSource)
					}
					s := kind.newServer(t, initial)
					provided := newFileMap(initial)
					for _, file := range provided {
						file.ModTime = time.Unix(1, 0)
					}
					s.fileMapGetter = func() map[string]*xgo.File { return provided }
					s.requestProject()
					if tt.provided {
						provided[kind.filename] = &xgo.File{Content: []byte(diskSource), ModTime: time.Unix(1, 0)}
					}
					s.replier = newMockReplier()
					uri := s.toDocumentURI(kind.filename)
					synctest.Test(t, func(t *testing.T) {
						require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
							TextDocument: protocol.TextDocumentItem{
								URI: uri, LanguageID: "xgo", Version: 7, Text: editorSource,
							},
						}))
						synctest.Wait()
						opened := s.requestProject()
						if tt.change || tt.save {
							provided[kind.filename] = &xgo.File{Content: []byte(changedSource), ModTime: time.Unix(2, 0)}
						}
						if tt.remove {
							delete(provided, kind.filename)
						}
						assert.Same(t, opened, s.requestProject(), "provider changes cannot replace an open document")
						if tt.save {
							require.NoError(t, s.didSave(&DidSaveTextDocumentParams{
								TextDocument: TextDocumentIdentifier{URI: uri}, Text: new(changedSource),
							}))
							synctest.Wait()
							saved, ok := s.requestProject().File(kind.filename)
							require.True(t, ok)
							assert.Equal(t, 7, saved.Version)
						}
						if tt.close {
							require.NoError(t, s.didClose(&DidCloseTextDocumentParams{
								TextDocument: TextDocumentIdentifier{URI: uri},
							}))
							synctest.Wait()
						}
						current, ok := s.requestProject().File(kind.filename)
						if tt.want == "" {
							assert.False(t, ok)
							return
						}
						require.True(t, ok)
						assert.Equal(t, tt.want, string(current.Content))
						if tt.close {
							assert.Zero(t, current.Version)
						} else {
							assert.Equal(t, 7, current.Version)
						}
					})
				})
			}
		})
	}
}

func TestServerRequestProviderUpdates(t *testing.T) {
	t.Run("Formatting", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("var oldValue=1\n")})
		files := map[string]*xgo.File{
			"main.xgo": {Content: []byte("var oldValue=1\n"), ModTime: time.Unix(1, 0)},
		}
		s.fileMapGetter = func() map[string]*xgo.File { return files }
		s.syncProject()
		params := &DocumentFormattingParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}
		_, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		files["main.xgo"] = &xgo.File{Content: []byte("var newValue=2\n"), ModTime: time.Unix(2, 0)}
		beforeOtherRequest, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		_, err = s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: params.TextDocument})
		require.NoError(t, err)
		afterOtherRequest, err := s.textDocumentFormatting(params)
		require.NoError(t, err)
		require.Len(t, afterOtherRequest, 1)
		require.Equal(t, "var newValue = 2\n", afterOtherRequest[0].NewText)
		assert.Equal(t, afterOtherRequest, beforeOtherRequest, "formatting must observe provider changes without a preceding documentLink request")
	})

	t.Run("Properties", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("type Record struct { OldValue int }\n")})
		files := map[string]*xgo.File{
			"main.xgo": {Content: []byte("type Record struct { OldValue int }\n"), ModTime: time.Unix(1, 0)},
		}
		s.fileMapGetter = func() map[string]*xgo.File { return files }
		s.syncProject()
		params := XGoGetPropertiesParams{Target: "Record"}
		_, err := s.xgoGetProperties(params)
		require.NoError(t, err)
		files["main.xgo"] = &xgo.File{Content: []byte("type Record struct { NewValue string }\n"), ModTime: time.Unix(2, 0)}
		beforeOtherRequest, err := s.xgoGetProperties(params)
		require.NoError(t, err)
		_, err = s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}})
		require.NoError(t, err)
		afterOtherRequest, err := s.xgoGetProperties(params)
		require.NoError(t, err)
		require.Len(t, afterOtherRequest, 1)
		require.Equal(t, "NewValue", afterOtherRequest[0].Name)
		assert.Equal(t, afterOtherRequest, beforeOtherRequest, "properties must observe provider changes without a preceding documentLink request")
	})
}

func TestServerRequestConcurrentDeletion(t *testing.T) {
	for _, tt := range []struct {
		name string
		read func(*Server, TextDocumentPositionParams) (any, error)
	}{
		{"Hover", func(s *Server, p TextDocumentPositionParams) (any, error) {
			return s.textDocumentHover(&HoverParams{TextDocumentPositionParams: p})
		}},
		{"Definition", func(s *Server, p TextDocumentPositionParams) (any, error) {
			return s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: p})
		}},
		{"Highlights", func(s *Server, p TextDocumentPositionParams) (any, error) {
			return s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: p})
		}},
		{"Rename", func(s *Server, p TextDocumentPositionParams) (any, error) {
			return s.textDocumentRename(&RenameParams{TextDocument: p.TextDocument, Position: p.Position, NewName: "renamed"})
		}},
		{"PropertyRename", func(s *Server, p TextDocumentPositionParams) (any, error) {
			return s.textDocumentRename(&RenameParams{
				TextDocument: p.TextDocument, Position: Position{Line: 4, Character: 18}, NewName: "Renamed",
			})
		}},
		{"Completion", func(s *Server, p TextDocumentPositionParams) (any, error) {
			return s.textDocumentCompletion(&CompletionParams{TextDocumentPositionParams: p})
		}},
		{"SemanticTokens", func(s *Server, p TextDocumentPositionParams) (any, error) {
			return s.textDocumentSemanticTokensFull(&SemanticTokensParams{TextDocument: p.TextDocument})
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{"main.xgo": []byte("println 1\n")}
			s := newTestServer(t, files)
			replier := newMockReplier()
			s.replier = replier
			proj := s.getProj()
			_, err := proj.TypeInfo()
			require.NoError(t, err)
			entered, resume := make(chan struct{}), make(chan struct{})
			release := sync.OnceFunc(func() { close(resume) })
			t.Cleanup(release)
			pause := sync.OnceFunc(func() { close(entered); <-resume })
			fallback := proj.Importer
			dependency := gotypes.NewPackage("example.com/paused", "paused")
			dependency.MarkComplete()
			proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
				if path == dependency.Path() {
					pause()
					return dependency, nil
				}
				return fallback.Import(path)
			})
			proj.PutFile("main.xgo", file(`import _ "example.com/paused"
type Record struct { Value int }
var value = 1
value = 2
println Record{}.Value
`))
			var wg sync.WaitGroup
			var recovered any
			var stack []byte
			var readErr error
			var result any
			wg.Go(func() {
				defer func() {
					recovered = recover()
					if recovered != nil {
						stack = debug.Stack()
					}
				}()
				result, readErr = tt.read(s, TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: Position{Line: 3, Character: 2}})
			})
			<-entered
			delete(files, "main.xgo")
			s.syncProject()
			release()
			wg.Wait()
			assert.NoError(t, readErr)
			assert.NotNil(t, result)
			assert.Nil(t, recovered)
			if recovered != nil {
				t.Logf("panic stack:\n%s", stack)
			}
			if tt.name == "PropertyRename" {
				messages := replier.getMessages()
				require.Len(t, messages, 1)
				notification := requireValueAs[*jsonrpc2.Notification](t, messages[0])
				assert.Equal(t, "textDocument/xgo.propertyRenamed", notification.Method())
				var params PropertyRenamedParams
				require.NoError(t, UnmarshalJSON(notification.Params(), &params))
				assert.Equal(t, PropertyRenamedParams{
					Target: "Record", OldName: "Value", NewName: "Renamed",
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				}, params)
			}
		})
	}
}

func TestServerDependencyDiagnostics(t *testing.T) {
	for _, tt := range []struct {
		name   string
		before string
		after  string
	}{
		{"ClearResolvedError", "func getValue() string { return \"text\" }\n", "func getValue() int { return 1 }\n"},
		{"PublishNewError", "func getValue() int { return 1 }\n", "func getValue() string { return \"text\" }\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, change := range []string{"Document", "Provider", "Close"} {
				t.Run(change, func(t *testing.T) {
					const mainSource = "var result int = getValue()\n"
					files := map[string][]byte{"main.xgo": []byte(mainSource), "helper.xgo": []byte(tt.before)}
					if change == "Close" {
						files["helper.xgo"] = []byte(tt.after)
					}
					s := newTestServer(t, files)
					provided := newFileMap(files)
					s.fileMapGetter = func() map[string]*xgo.File { return provided }
					replier := newMockReplier()
					s.replier = replier
					synctest.Test(t, func(t *testing.T) {
						open := map[string]string{"main.xgo": mainSource}
						if change != "Provider" {
							open["helper.xgo"] = tt.before
						}
						for path, source := range open {
							require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
								TextDocument: protocol.TextDocumentItem{
									URI: s.toDocumentURI(path), LanguageID: "xgo", Version: 1, Text: source,
								},
							}))
						}
						synctest.Wait()
						reports := requirePublishedDiagnosticReports(t, replier, len(open))
						mainURI := s.toDocumentURI("main.xgo")
						before := s.getDiagnostics(t, "main.xgo")
						assert.ElementsMatch(t, before, reports[mainURI])
						switch change {
						case "Document":
							require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
								TextDocument: protocol.VersionedTextDocumentIdentifier{
									TextDocumentIdentifier: TextDocumentIdentifier{URI: "file:///helper.xgo"}, Version: 2,
								},
								ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: tt.after}},
							}))
						case "Provider":
							provided["helper.xgo"] = &xgo.File{Content: []byte(tt.after), ModTime: time.Unix(1, 0)}
							s.syncProject()
						case "Close":
							require.NoError(t, s.didClose(&DidCloseTextDocumentParams{
								TextDocument: TextDocumentIdentifier{URI: "file:///helper.xgo"},
							}))
						}
						synctest.Wait()
						reports = requirePublishedDiagnosticReports(t, replier, len(open))
						after := s.getDiagnostics(t, "main.xgo")
						require.NotEqual(t, len(before), len(after))
						assert.ElementsMatch(t, after, reports[mainURI])
						assert.Empty(t, reports["file:///helper.xgo"])
						if change == "Close" {
							s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(mainSource), Version: 2}})
							synctest.Wait()
							reports = requirePublishedDiagnosticReports(t, replier, 1)
							assert.Contains(t, reports, mainURI)
						}
					})
				})
			}
		})
	}
}

func TestServerDocumentProviderDiagnostics(t *testing.T) {
	const source = "var result int = getValue()\nprintln result\n"
	const good = "func getValue() int { return 1 }\n"
	const bad = "func getValue() string { return \"text\" }\n"
	for _, kind := range []struct{ name, filename string }{
		{"XGo", "main.xgo"},
		{"NormalClass", "Record.gox"},
		{"ProjectClass", "main_fixture.gox"},
		{"WorkClass", "Worker_fixture.gox"},
	} {
		t.Run(kind.name, func(t *testing.T) {
			for _, change := range []struct{ name, before, after string }{
				{"ClearTypeError", bad, good},
				{"IntroduceTypeError", good, bad},
				{"CreateDependency", "", good},
				{"DeleteDependency", good, ""},
			} {
				t.Run(change.name, func(t *testing.T) {
					for _, operation := range []string{"Change", "SaveWithText", "SaveWithoutText"} {
						t.Run(operation, func(t *testing.T) {
							files := map[string][]byte{kind.filename: []byte(source)}
							if change.before != "" {
								files["helper.xgo"] = []byte(change.before)
							}
							if kind.name == "WorkClass" {
								files["main_fixture.gox"] = nil
							}
							s := newImportTestServer(t, files)
							t.Cleanup(s.Close)
							provided := newFileMap(files)
							s.fileMapGetter = func() map[string]*xgo.File { return provided }
							replier := newMockReplier()
							s.replier = replier
							uri := s.toDocumentURI(kind.filename)
							synctest.Test(t, func(t *testing.T) {
								require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
									TextDocument: protocol.TextDocumentItem{URI: uri, Version: 7, Text: source},
								}))
								synctest.Wait()
								before := requirePublishedDiagnostics(t, replier, uri)
								if change.after == "" {
									delete(provided, "helper.xgo")
								} else {
									provided["helper.xgo"] = &xgo.File{Content: []byte(change.after), ModTime: time.Unix(1, 0)}
								}
								// Changed provider content must not replace the open document.
								provided[kind.filename] = &xgo.File{Content: []byte("println missing\n"), ModTime: time.Unix(1, 0)}
								edited := source + "\n"
								version := 7
								switch operation {
								case "Change":
									version++
									require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
										TextDocument: protocol.VersionedTextDocumentIdentifier{
											TextDocumentIdentifier: TextDocumentIdentifier{URI: uri}, Version: int32(version),
										},
										ContentChanges: []protocol.TextDocumentContentChangeEvent{{
											Range: &Range{Start: Position{Line: 2}, End: Position{Line: 2}}, Text: "\n",
										}},
									}))
								case "SaveWithText":
									require.NoError(t, s.didSave(&DidSaveTextDocumentParams{
										TextDocument: TextDocumentIdentifier{URI: uri}, Text: &edited,
									}))
								case "SaveWithoutText":
									edited = source
									require.NoError(t, s.didSave(&DidSaveTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: uri}}))
								}
								synctest.Wait()
								pushed := requirePublishedDiagnostics(t, replier, uri)
								effective := make(map[string][]byte, len(provided))
								for name, file := range provided {
									effective[name] = file.Content
								}
								effective[kind.filename] = []byte(edited)
								fresh := newImportTestServer(t, effective)
								t.Cleanup(fresh.Close)
								want := fresh.getDiagnostics(t, kind.filename)
								require.NotEqual(t, before, want)
								assert.ElementsMatch(t, want, pushed)
								current, ok := s.getProj().File(kind.filename)
								require.True(t, ok)
								assert.Equal(t, edited, string(current.Content))
								assert.Equal(t, version, current.Version)
								pull, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: uri}})
								require.NoError(t, err)
								assert.ElementsMatch(t, want, requireRelatedFullDocumentDiagnosticReport(t, pull).Items)
								synctest.Wait()
							})
						})
					}
				})
			}
		})
	}

	t.Run("UnchangedSave", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte("println 1\n")})
		t.Cleanup(s.Close)
		replier := newMockReplier()
		s.replier = replier
		synctest.Test(t, func(t *testing.T) {
			require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{URI: "file:///main.xgo", Version: 1, Text: "println 1\n"},
			}))
			synctest.Wait()
			assert.Empty(t, requirePublishedDiagnostics(t, replier, "file:///main.xgo"))
			before := s.requestProject()
			require.NoError(t, s.didSave(&DidSaveTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}))
			synctest.Wait()
			assert.Same(t, before, s.analysisSnapshot)
			assert.Empty(t, replier.getMessages())
		})
	})

	t.Run("SingleProviderRead", func(t *testing.T) {
		const source = "println value\n"
		files := map[string][]byte{
			"main.xgo":   []byte(source),
			"other.xgo":  []byte("func check() { println value }\n"),
			"helper.xgo": []byte("const value = 1\n"),
		}
		s := newImportTestServer(t, files)
		t.Cleanup(s.Close)
		replier := newMockReplier()
		s.replier = replier
		synctest.Test(t, func(t *testing.T) {
			for _, name := range []string{"main.xgo", "other.xgo"} {
				require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
					TextDocument: protocol.TextDocumentItem{URI: s.toDocumentURI(name), Version: 1, Text: string(files[name])},
				}))
			}
			synctest.Wait()
			requirePublishedDiagnosticReports(t, replier, 2)
			var reads atomic.Int32
			s.fileMapGetter = func() map[string]*xgo.File {
				provided := newFileMap(files)
				provided["helper.xgo"].ModTime = time.Unix(int64(reads.Add(1)), 0)
				return provided
			}
			require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{
					TextDocumentIdentifier: TextDocumentIdentifier{URI: "file:///main.xgo"}, Version: 2,
				},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: source + "\n"}},
			}))
			synctest.Wait()
			for _, diagnostics := range requirePublishedDiagnosticReports(t, replier, 2) {
				assert.Empty(t, diagnostics)
			}
			assert.EqualValues(t, 1, reads.Load())
		})
	})
}

func TestServerDocumentProviderCallbacks(t *testing.T) {
	const source = "var result int = getValue()\n"
	const original = "func getValue() string { return \"text\" }\n"
	const updated = "func getValue() int { return 1 }\n"
	for _, operation := range []string{"Change", "SaveWithText", "SaveWithoutText"} {
		t.Run(operation, func(t *testing.T) {
			for _, callback := range []string{"Request", "Edit", "EditDocument", "ReopenDocument", "Close"} {
				t.Run(callback, func(t *testing.T) {
					files := map[string][]byte{"main.xgo": []byte(source), "helper.xgo": []byte(original)}
					s := newImportTestServer(t, files)
					t.Cleanup(s.Close)
					replier := newMockReplier()
					s.replier = replier
					synctest.Test(t, func(t *testing.T) {
						require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
							TextDocument: protocol.TextDocumentItem{URI: "file:///main.xgo", Version: 1, Text: source},
						}))
						synctest.Wait()
						require.NotEmpty(t, requirePublishedDiagnostics(t, replier, "file:///main.xgo"))
						older := newFileMap(files)
						newer := newFileMap(files)
						newer["helper.xgo"] = &xgo.File{Content: []byte(updated), ModTime: time.Unix(1, 0)}
						var calls atomic.Int32
						s.fileMapGetter = func() map[string]*xgo.File {
							if calls.Add(1) == 1 {
								switch callback {
								case "Request":
									s.requestProject()
								case "Edit":
									s.ModifyFiles([]FileChange{{Path: "helper.xgo", Content: []byte(updated), Version: 1}})
								case "EditDocument":
									s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte("const latest = 1\n"), Version: 3}})
								case "ReopenDocument":
									require.NoError(t, s.didClose(&DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}))
									require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
										TextDocument: protocol.TextDocumentItem{URI: "file:///main.xgo", Version: 1, Text: "const latest = 1\n"},
									}))
								case "Close":
									s.Close()
								}
								return older
							}
							return newer
						}
						switch operation {
						case "Change":
							require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
								TextDocument: protocol.VersionedTextDocumentIdentifier{
									TextDocumentIdentifier: TextDocumentIdentifier{URI: "file:///main.xgo"}, Version: 2,
								},
								ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: source}},
							}))
						case "SaveWithText":
							require.NoError(t, s.didSave(&DidSaveTextDocumentParams{
								TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Text: new(source),
							}))
						case "SaveWithoutText":
							require.NoError(t, s.didSave(&DidSaveTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}))
						}
						synctest.Wait()
						messages := replier.getMessages()
						if callback == "Close" {
							assert.ErrorIs(t, context.Cause(s.ctx), ErrClosed)
							assert.Empty(t, messages)
							return
						}
						if callback == "EditDocument" || callback == "ReopenDocument" {
							current, ok := s.getProj().File("main.xgo")
							require.True(t, ok)
							assert.Equal(t, "const latest = 1\n", string(current.Content))
						}
						require.NotEmpty(t, messages)
						for _, message := range messages {
							notification := requireValueAs[*jsonrpc2.Notification](t, message)
							require.Equal(t, "textDocument/publishDiagnostics", notification.Method())
							var report PublishDiagnosticsParams
							require.NoError(t, json.Unmarshal(notification.Params(), &report))
							assert.Equal(t, DocumentURI("file:///main.xgo"), report.URI)
							assert.Empty(t, report.Diagnostics)
						}
					})
				})
			}
		})
	}
}
