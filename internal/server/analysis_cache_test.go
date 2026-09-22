package server

import (
	"errors"
	gotypes "go/types"
	"io/fs"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newAnalysisTestServer(t testing.TB) (*Server, *atomic.Int32) {
	t.Helper()

	s := newFrameworkTestServer(t, map[string][]byte{
		"main_fixture.gox": []byte("func use(name string) {}\nuse \"Known\"\n"),
		"resources.txt":    []byte("Known"),
	})
	builds := new(atomic.Int32)
	s.getProj().RegisterCacheBuilder(frameworkAnalysisCacheKind{}, func(proj *xgo.Project) (any, error) {
		builds.Add(1)
		metadata, ok := proj.File("resources.txt")
		if !ok {
			return nil, fs.ErrNotExist
		}
		id := testResourceID{"files", string(metadata.Content)}
		resources := newTestResourceAnalysis(id)
		for ref := range resourceReferences(proj, func(value resourceValue) (resourceID, bool) {
			return testResourceID{"files", value.Name}, true
		}) {
			resources.addResourceRef(ref)
			if !resources.contains(ref.ID) {
				addResourceDiagnostic(proj, resources, ref.Node, "resource not found")
			}
		}
		return &frameworkAnalysis{
			resources: resources,
			collectCompletions: func(ctx *completionContext) {
				if ctx.inStringLit {
					ctx.collectResourceNames([]resourceID{id})
				}
			},
			adaptInputSlot: func(ctx *inputSlotContext, expr ast.Expr, typ gotypes.Type, slot *XGoInputSlot) *XGoInputSlot {
				if lit, ok := expr.(*ast.BasicLit); ok {
					return resources.createResourceInputSlot(ctx, lit, typ, "test-resource-name")
				}
				return slot
			},
			renameResources: func(server *Server, project *xgo.Project, params []XGoRenameResourceParams) (*WorkspaceEdit, error) {
				renames := make(map[resourceID]string)
				for _, param := range params {
					for _, ref := range resources.resourceRefs {
						if param.Resource.URI == ref.ID.URI() {
							renames[ref.ID] = param.NewName
						}
					}
				}
				changes, err := server.renameResourcesAtRefs(project, resources, renames)
				return &WorkspaceEdit{Changes: changes}, err
			},
		}, nil
	})
	return s, builds
}

func TestAnalyzeFrameworkCache(t *testing.T) {
	t.Run("SharedAcrossRequests", func(t *testing.T) {
		s, builds := newAnalysisTestServer(t)
		proj := s.requestProject()
		cached, err := analyzeFramework(proj)
		require.NoError(t, err)
		require.NotNil(t, cached)
		uri := DocumentURI("file:///main_fixture.gox")
		position := Position{Line: 1, Character: 5}
		for range 3 {
			links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: uri}})
			require.NoError(t, err)
			assert.True(t, slices.ContainsFunc(links, func(link DocumentLink) bool {
				return link.Target != nil && *link.Target == "test://resources/files/Known"
			}))
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: uri}, Position: position,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, "test://resources/files/Known")
			items := completionItemsAt(t, s, "main_fixture.gox", position)
			require.NotNil(t, completionItemByLabel(items, "Known"))
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: uri}}})
			require.NoError(t, err)
			require.Len(t, slots, 1)
			assert.Equal(t, XGoResourceURI("test://resources/files/Known"), slots[0].Input.Value)
			diagnostics, err := s.diagnosticsAt(proj)
			require.NoError(t, err)
			assert.False(t, diagnostics.hasErrorSeverityDiagnostic)
			edit, err := s.renameResources([]XGoRenameResourceParams{{
				Resource: XGoResourceIdentifier{URI: "test://resources/files/Known"}, NewName: "Other",
			}})
			require.NoError(t, err)
			require.Len(t, edit.Changes[uri], 1)
			assert.Equal(t, "Other", edit.Changes[uri][0].NewText)
			current, err := analyzeFramework(proj)
			require.NoError(t, err)
			assert.Same(t, cached, current)
		}
		assert.EqualValues(t, 1, builds.Load())
	})

	t.Run("ProviderMetadataChanges", func(t *testing.T) {
		s, builds := newAnalysisTestServer(t)
		proj := s.getProj()
		files := maps.Collect(proj.Files())
		s.fileMapGetter = func() map[string]*xgo.File { return maps.Clone(files) }
		params := &DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}}
		for version, tt := range []struct {
			name      string
			wantLinks bool
		}{
			{"Known", true}, {"Other", false}, {"Known", true},
		} {
			files["resources.txt"] = &xgo.File{Content: []byte(tt.name), ModTime: time.Unix(int64(version+1), 0)}
			before := builds.Load()
			for range 2 {
				links, err := s.textDocumentDocumentLink(params)
				require.NoError(t, err)
				assert.Equal(t, tt.wantLinks, slices.ContainsFunc(links, func(link DocumentLink) bool {
					return link.Target != nil && *link.Target == "test://resources/files/Known"
				}))
			}
			assert.Equal(t, before+1, builds.Load())
		}
	})

	t.Run("ReferencedConstantChanges", func(t *testing.T) {
		s, _ := newAnalysisTestServer(t)
		proj := s.getProj()
		proj.PutFile("main_fixture.gox", &xgo.File{Content: []byte("func use(name string) {}\nuse Selected\n")})
		proj.PutFile("names.xgo", &xgo.File{Content: []byte("const Selected = \"Known\"\n")})
		files := maps.Collect(proj.Files())
		files["names.xgo"] = &xgo.File{Content: files["names.xgo"].Content, ModTime: time.Unix(1, 0)}
		proj.UpdateFiles(files)
		s.fileMapGetter = func() map[string]*xgo.File { return maps.Clone(files) }
		params := &HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}, Position: Position{Line: 1, Character: 6},
		}}
		before, err := s.textDocumentHover(params)
		require.NoError(t, err)
		require.NotNil(t, before)
		assert.Contains(t, before.Contents.Value, "test://resources/files/Known")
		astFile, err := proj.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		s.ModifyFiles([]FileChange{{Path: "names.xgo", Content: []byte("const Selected = \"Other\"\n"), Version: 1}})
		after, err := s.textDocumentHover(params)
		require.NoError(t, err)
		require.NotNil(t, after)
		assert.Contains(t, after.Contents.Value, "test://resources/files/Other")
		currentAST, err := proj.ASTFile("main_fixture.gox")
		require.NoError(t, err)
		assert.Same(t, astFile, currentAST)
		cached, err := analyzeFramework(s.requestProject())
		require.NoError(t, err)
		s.ModifyFiles([]FileChange{{Path: "names.xgo", Content: []byte("const Selected = \"Stale\"\n"), Version: 1}})
		unchanged, err := analyzeFramework(s.syncProject())
		require.NoError(t, err)
		assert.Same(t, cached, unchanged)
		current, ok := proj.File("names.xgo")
		require.True(t, ok)
		assert.Equal(t, 1, current.Version)
		assert.Equal(t, time.Unix(1, 0), current.ModTime)
		assert.Equal(t, "const Selected = \"Other\"\n", string(current.Content))

		files["names.xgo"] = &xgo.File{Content: []byte("const Selected = \"Provided\"\n"), ModTime: time.Unix(2, 0)}
		provided, err := s.textDocumentHover(params)
		require.NoError(t, err)
		require.NotNil(t, provided)
		assert.Contains(t, provided.Contents.Value, "test://resources/files/Provided")
		refreshed, err := analyzeFramework(s.requestProject())
		require.NoError(t, err)
		assert.NotSame(t, cached, refreshed)
		current, ok = proj.File("names.xgo")
		require.True(t, ok)
		assert.Equal(t, 1, current.Version)
		s.ModifyFiles([]FileChange{{Path: "names.xgo", Content: []byte("const Selected = \"Stale\"\n"), Version: 1}})
		unchanged, err = analyzeFramework(s.syncProject())
		require.NoError(t, err)
		assert.Same(t, refreshed, unchanged)
		s.ModifyFiles([]FileChange{{Path: "names.xgo", Content: []byte("const Selected = \"Known\"\n"), Version: 2}})
		updated, err := s.textDocumentHover(params)
		require.NoError(t, err)
		require.NotNil(t, updated)
		assert.Contains(t, updated.Contents.Value, "test://resources/files/Known")
	})

	t.Run("SnapshotDuringBuild", func(t *testing.T) {
		s, _ := newAnalysisTestServer(t)
		proj := s.getProj()
		entered, resume := make(chan struct{}), make(chan struct{})
		release := sync.OnceFunc(func() { close(resume) })
		t.Cleanup(release)
		var builds atomic.Int32
		proj.RegisterCacheBuilder(frameworkAnalysisCacheKind{}, func(p *xgo.Project) (any, error) {
			resources := newTestResourceAnalysis(testResourceID{"files", "Known"})
			for ref := range resourceReferences(p, func(value resourceValue) (resourceID, bool) {
				return testResourceID{"files", value.Name}, true
			}) {
				resources.addResourceRef(ref)
			}
			if builds.Add(1) == 1 {
				close(entered)
				<-resume
			}
			return &frameworkAnalysis{resources: resources}, nil
		})
		var wg sync.WaitGroup
		var original *frameworkAnalysis
		var originalErr error
		wg.Go(func() { original, originalErr = analyzeFramework(proj) })
		<-entered
		snapshot := proj.Snapshot()
		release()
		wg.Wait()
		require.NoError(t, originalErr)
		copied, err := analyzeFramework(snapshot)
		require.NoError(t, err)
		assert.NotSame(t, original, copied)
		links := original.resources.resourceDocumentLinks(proj, "main_fixture.gox")
		require.Len(t, links, 1)
		assert.Equal(t, links,
			copied.resources.resourceDocumentLinks(snapshot, "main_fixture.gox"))
		assert.EqualValues(t, 2, builds.Load())
	})

	t.Run("InvalidationAndSnapshots", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			change    func(*testing.T, *xgo.Project)
			wantRefs  int
			wantLinks int
			wantName  string
			wantFile  string
		}{
			{"Source", func(t *testing.T, p *xgo.Project) {
				p.PutFile("main_fixture.gox", &xgo.File{Content: []byte("func use(name string) {}\nuse \"Other\"\n")})
			}, 1, 0, "Other", "main_fixture.gox"},
			{"Metadata", func(t *testing.T, p *xgo.Project) {
				p.PutFile("resources.txt", &xgo.File{Content: []byte("Other")})
			}, 1, 0, "Known", "main_fixture.gox"},
			{"AddSource", func(t *testing.T, p *xgo.Project) {
				p.PutFile("other.xgo", &xgo.File{Content: []byte("echo \"Known\"\n")})
			}, 2, 1, "Known", "main_fixture.gox"},
			{"DeleteSource", func(t *testing.T, p *xgo.Project) {
				require.NoError(t, p.DeleteFile("main_fixture.gox"))
			}, 0, 0, "", "main_fixture.gox"},
			{"RenameSource", func(t *testing.T, p *xgo.Project) {
				require.NoError(t, p.RenameFile("main_fixture.gox", "sub/main_fixture.gox"))
			}, 1, 1, "Known", "sub/main_fixture.gox"},
			{"UpdateFiles", func(t *testing.T, p *xgo.Project) {
				p.UpdateFiles(map[string]*xgo.File{
					"main_fixture.gox": {Content: []byte("func use(name string) {}\nuse \"Other\"\n"), ModTime: time.Unix(1, 0)},
					"resources.txt":    {Content: []byte("Other"), ModTime: time.Unix(1, 0)},
				})
			}, 1, 1, "Other", "main_fixture.gox"},
			{"Registration", func(t *testing.T, p *xgo.Project) {
				p.SetModule(newTestServer(t, nil).getProj().Module())
			}, 1, 1, "Known", "main_fixture.gox"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s, builds := newAnalysisTestServer(t)
				proj := s.getProj()
				before, err := analyzeFramework(proj)
				require.NoError(t, err)
				wantOldLinks := before.resources.resourceDocumentLinks(proj, "main_fixture.gox")
				require.Len(t, wantOldLinks, 1)
				renames := []XGoRenameResourceParams{{
					Resource: XGoResourceIdentifier{URI: "test://resources/files/Known"}, NewName: "Renamed",
				}}
				wantOldEdit, err := before.renameResources(s, proj, renames)
				require.NoError(t, err)
				require.Len(t, wantOldEdit.Changes["file:///main_fixture.gox"], 1)
				snapshot := proj.Snapshot()
				tt.change(t, proj)
				after, err := analyzeFramework(proj)
				require.NoError(t, err)
				assert.NotSame(t, before, after)
				require.Len(t, after.resources.resourceRefs, tt.wantRefs)
				if tt.wantName != "" {
					assert.Equal(t, tt.wantName, after.resources.resourceRefs[0].ID.Name())
				}
				assert.Len(t, after.resources.resourceDocumentLinks(proj, tt.wantFile), tt.wantLinks)
				old, err := analyzeFramework(snapshot)
				require.NoError(t, err)
				assert.Same(t, before, old)
				assert.Equal(t, wantOldLinks, old.resources.resourceDocumentLinks(snapshot, "main_fixture.gox"))
				oldEdit, err := old.renameResources(s, snapshot, renames)
				require.NoError(t, err)
				assert.Equal(t, wantOldEdit, oldEdit)
				assert.EqualValues(t, 2, builds.Load())
			})
		}
	})

	t.Run("CachedErrorRecovery", func(t *testing.T) {
		s, builds := newAnalysisTestServer(t)
		proj := s.getProj()
		require.NoError(t, proj.DeleteFile("resources.txt"))
		for range 2 {
			result, err := analyzeFramework(proj)
			assert.Nil(t, result)
			assert.ErrorIs(t, err, fs.ErrNotExist)
		}
		assert.EqualValues(t, 1, builds.Load())
		snapshot := proj.Snapshot()
		proj.PutFile("resources.txt", &xgo.File{Content: []byte("Known")})
		result, err := analyzeFramework(proj)
		require.NoError(t, err)
		assert.Len(t, result.resources.resourceDocumentLinks(proj, "main_fixture.gox"), 1)
		_, err = analyzeFramework(snapshot)
		assert.ErrorIs(t, err, fs.ErrNotExist)
		assert.EqualValues(t, 2, builds.Load())
	})

	t.Run("ConcurrentReaders", func(t *testing.T) {
		s, builds := newAnalysisTestServer(t)
		proj := s.getProj()
		var wg sync.WaitGroup
		results := make([]*frameworkAnalysis, 24)
		errs := make([]error, len(results))
		for i := range results {
			wg.Go(func() { results[i], errs[i] = analyzeFramework(proj) })
		}
		wg.Wait()
		for i := range results {
			require.NoError(t, errs[i])
			require.NotNil(t, results[i])
			assert.Same(t, results[0], results[i])
		}
		assert.EqualValues(t, 1, builds.Load())
	})

	t.Run("SeparateProjects", func(t *testing.T) {
		first, firstBuilds := newAnalysisTestServer(t)
		second, secondBuilds := newAnalysisTestServer(t)
		a, err := analyzeFramework(first.getProj())
		require.NoError(t, err)
		b, err := analyzeFramework(second.getProj())
		require.NoError(t, err)
		assert.NotSame(t, a, b)
		require.Len(t, a.resources.resourceRefs, 1)
		require.Len(t, b.resources.resourceRefs, 1)
		assert.NotSame(t, a.resources.resourceRefs[0].Node, b.resources.resourceRefs[0].Node)
		assert.EqualValues(t, 1, firstBuilds.Load())
		assert.EqualValues(t, 1, secondBuilds.Load())
	})

	t.Run("NoFramework", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte("println 1\n")})
		var builds int
		s.getProj().RegisterCacheBuilder(frameworkAnalysisCacheKind{}, func(proj *xgo.Project) (any, error) {
			builds++
			return buildFrameworkAnalysisCache(proj)
		})
		for range 2 {
			result, err := analyzeFramework(s.getProj())
			require.NoError(t, err)
			assert.Nil(t, result)
			assert.Nil(t, resolveFrameworkAdapter(s.getProj()))
		}
		assert.Equal(t, 1, builds)
	})

	t.Run("BuilderError", func(t *testing.T) {
		s, _ := newAnalysisTestServer(t)
		failure := errors.New("analysis failed")
		s.getProj().RegisterCacheBuilder(frameworkAnalysisCacheKind{}, func(*xgo.Project) (any, error) {
			return nil, failure
		})
		_, err := s.renameResources(nil)
		assert.ErrorIs(t, err, failure)
	})
}

func TestServerCollectResourceDiagnostics(t *testing.T) {
	s := newTestServer(t, nil)
	message := "undefined: resource"
	analysis := &resourceAnalysis{diagnostics: []sourceDiagnostic{{
		filename:   "main.xgo",
		diagnostic: Diagnostic{Severity: SeverityError, Message: message, Range: Range{End: Position{Character: 8}}},
	}}}
	for _, tt := range []struct {
		root   DocumentURI
		locale string
	}{
		{"file:///first/", "en"}, {"file:///second/", "zh-CN"}, {"file:///first/", "en"},
	} {
		s.workspaceRootURI = tt.root
		s.setLanguageFromLocale(tt.locale)
		result := newDiagnosticResult()
		s.collectResourceDiagnostics(&result, analysis)
		s.collectResourceDiagnostics(&result, analysis)
		uri := s.toDocumentURI("main.xgo")
		require.Len(t, result.diagnostics, 1)
		require.Len(t, result.diagnostics[uri], 1)
		assert.Equal(t, s.translate(message), result.diagnostics[uri][0].Message)
		if tt.locale == "zh-CN" {
			assert.NotEqual(t, message, result.diagnostics[uri][0].Message)
		}
		result.diagnostics[uri][0].Message = "changed response"
		assert.Equal(t, message, analysis.diagnostics[0].diagnostic.Message)
	}
}
