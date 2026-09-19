package server

import (
	"errors"
	"fmt"
	gotypes "go/types"
	"slices"
	"sync"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireFrameworkAnalysis(t testing.TB, proj *xgo.Project) *frameworkAnalysis {
	t.Helper()

	result, err := analyzeFramework(proj)
	require.NoError(t, err)
	require.NotNil(t, result)
	return result
}

func TestBuildFrameworkAnalysisCache(t *testing.T) {
	t.Run("PreserveAnalyzedSyntax", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx": []byte(`func work() {
	var value int
	for value = range 0:limit+1 {
		play "Known"
	}
}
work
`),
			"values.xgo":                     []byte("const limit = 1\n"),
			"assets/index.json":              []byte(`{}`),
			"assets/sounds/Known/index.json": []byte(`{}`),
		})
		proj := s.getProj()
		result := requireFrameworkAnalysis(t, proj)
		require.Len(t, result.resources.resourceRefs, 1)
		original, err := proj.ASTFile("main.spx")
		require.NoError(t, err)
		var body *ast.BlockStmt
		ast.Inspect(original, func(node ast.Node) bool {
			if loop, ok := node.(*ast.RangeStmt); ok {
				body = loop.Body
			}
			return true
		})
		require.NotNil(t, body)
		statements := slices.Clone(body.List)
		proj.PutFile("values.xgo", &xgo.File{Content: []byte("const limit = 2\n")})
		_, err = proj.TypeInfo()
		require.NoError(t, err)
		assert.Equal(t, statements, body.List)
		current, err := proj.ASTFile("main.spx")
		require.NoError(t, err)
		assert.NotSame(t, original, current)
	})

	t.Run("ConcurrentEdits", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx": []byte(`func work() {
	var value int
	for value = range 0:limit+1 {
		play "Known"
	}
}
work
`),
			"values.xgo":                     []byte("const limit = 1\n"),
			"assets/index.json":              []byte(`{}`),
			"assets/sounds/Known/index.json": []byte(`{}`),
		})
		proj := s.getProj()
		_, err := proj.TypeInfo()
		require.NoError(t, err)
		var wg sync.WaitGroup
		start := make(chan struct{})
		var analysisErr, typeErr error
		var result any
		wg.Go(func() {
			<-start
			for range 100 {
				result, analysisErr = buildFrameworkAnalysisCache(proj)
				if analysisErr != nil {
					return
				}
			}
		})
		wg.Go(func() {
			<-start
			for i := range 40 {
				proj.PutFile("values.xgo", &xgo.File{Content: []byte(fmt.Sprintf("const limit = %d\n", i))})
				if _, err := proj.TypeInfo(); err != nil {
					typeErr = err
					return
				}
			}
		})
		close(start)
		wg.Wait()
		require.NoError(t, analysisErr)
		require.NoError(t, typeErr)
		analysis := requireValueAs[*frameworkAnalysis](t, result)
		require.NotNil(t, analysis)
		assert.Len(t, analysis.resources.resourceRefs, 1)
	})

	t.Run("ResourceChanges", func(t *testing.T) {
		for _, tt := range []struct {
			name            string
			change          func(*testing.T, *xgo.Project)
			wantLinks       int
			wantDiagnostics int
		}{
			{"Source", func(t *testing.T, proj *xgo.Project) {
				proj.PutFile("main.spx", &xgo.File{Content: []byte("play \"Other\"\n")})
			}, 0, 1},
			{"AddMetadata", func(t *testing.T, proj *xgo.Project) {
				proj.PutFile("assets/sounds/Missing/index.json", &xgo.File{Content: []byte(`{}`)})
			}, 2, 0},
			{"DeleteMetadata", func(t *testing.T, proj *xgo.Project) {
				require.NoError(t, proj.DeleteFile("assets/sounds/Known/index.json"))
			}, 0, 2},
			{"RenameMetadata", func(t *testing.T, proj *xgo.Project) {
				require.NoError(t, proj.RenameFile("assets/sounds/Known/index.json", "assets/sounds/Renamed/index.json"))
			}, 0, 2},
			{"MalformedMetadata", func(t *testing.T, proj *xgo.Project) {
				proj.PutFile("assets/index.json", &xgo.File{Content: []byte(`{`)})
			}, 0, 1},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newSpxTestServer(t, map[string][]byte{
					"main.spx":                       []byte("play \"Known\"\nplay \"Missing\"\n"),
					"assets/index.json":              []byte(`{}`),
					"assets/sounds/Known/index.json": []byte(`{}`),
				})
				proj := s.getProj()
				first := requireFrameworkAnalysis(t, proj)
				assert.Same(t, resolveFrameworkAdapter(proj), first.adapter)
				oldLinks := first.resources.resourceDocumentLinks(proj, "main.spx")
				require.Len(t, oldLinks, 1)
				oldDiagnostics := resourceDiagnostics(s, first.resources)
				require.Len(t, oldDiagnostics.diagnostics["file:///main.spx"], 1)
				snapshot := proj.Snapshot()
				tt.change(t, proj)
				current := requireFrameworkAnalysis(t, proj)
				assert.NotSame(t, first, current)
				assert.Len(t, current.resources.resourceDocumentLinks(proj, "main.spx"), tt.wantLinks)
				assert.Len(t, resourceDiagnostics(s, current.resources).diagnostics["file:///main.spx"], tt.wantDiagnostics)
				old := requireFrameworkAnalysis(t, snapshot)
				assert.Same(t, first, old)
				assert.Equal(t, oldLinks, old.resources.resourceDocumentLinks(snapshot, "main.spx"))
				assert.Equal(t, oldDiagnostics, resourceDiagnostics(s, old.resources))
				assert.Same(t, current, requireFrameworkAnalysis(t, proj))
				if tt.name == "MalformedMetadata" {
					proj.PutFile("assets/index.json", &xgo.File{Content: []byte(`{}`)})
					recovered := requireFrameworkAnalysis(t, proj)
					assert.Equal(t, oldLinks, recovered.resources.resourceDocumentLinks(proj, "main.spx"))
					assert.Equal(t, oldDiagnostics, resourceDiagnostics(s, recovered.resources))
				}
			})
		}
	})

	t.Run("MissingClassfileAndRegistrationRecovery", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx":                       []byte("play \"Known\"\n"),
			"assets/index.json":              []byte(`{}`),
			"assets/sounds/Known/index.json": []byte(`{}`),
		})
		proj := s.getProj()
		first := requireFrameworkAnalysis(t, proj)
		snapshot := proj.Snapshot()
		module := proj.Module()
		source, ok := proj.File("main.spx")
		require.True(t, ok)
		require.NoError(t, proj.DeleteFile("main.spx"))
		for range 2 {
			absent, err := analyzeFramework(proj)
			require.NoError(t, err)
			assert.Nil(t, absent)
		}
		proj.PutFile("main.spx", source)
		assert.NotSame(t, first, requireFrameworkAnalysis(t, proj))
		proj.SetModule(newTestServer(t, nil).getProj().Module())
		absent, err := analyzeFramework(proj)
		require.NoError(t, err)
		assert.Nil(t, absent)
		assert.Nil(t, resolveFrameworkAdapter(proj))
		proj.SetModule(module)
		recovered := requireFrameworkAnalysis(t, proj)
		assert.Len(t, recovered.resources.resourceDocumentLinks(proj, "main.spx"), 1)
		assert.Same(t, first, requireFrameworkAnalysis(t, snapshot))
	})

	t.Run("RequestContextAndImmutableResults", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx":                       []byte("play \"Known\"\nplay \"\"\n"),
			"assets/index.json":              []byte(`{}`),
			"assets/sounds/Known/index.json": []byte(`{}`),
		})
		proj := s.getProj()
		cached := requireFrameworkAnalysis(t, proj)
		for _, tt := range []struct {
			root   DocumentURI
			locale string
		}{
			{"file:///first/", "en"}, {"file:///second/", "zh-CN"}, {"file:///first/", "en"},
		} {
			s.workspaceRootURI = tt.root
			s.setLanguageFromLocale(tt.locale)
			uri := s.toDocumentURI("main.spx")
			report, err := s.diagnosticsAt(proj)
			require.NoError(t, err)
			require.Len(t, report.diagnostics[uri], 1)
			message := "sound resource name cannot be empty"
			assert.Equal(t, s.translate(message), report.diagnostics[uri][0].Message)
			report.diagnostics[uri][0].Message = "changed response"
			edit, err := cached.renameResources(s, proj, []XGoRenameResourceParams{{
				Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/Known"}, NewName: "Other",
			}})
			require.NoError(t, err)
			require.Len(t, edit.Changes, 1)
			require.Len(t, edit.Changes[uri], 1)
			assert.Equal(t, "Other", edit.Changes[uri][0].NewText)
			edit.Changes[uri][0].NewText = "changed response"
			assert.Same(t, cached, requireFrameworkAnalysis(t, proj))
		}
	})

	t.Run("SnapshotRenameContext", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx":                         []byte("const Sound = \"Known\"\nplay Sound\nRunner.setCostume \"idle\"\nvar target *Runner\n"),
			"Runner.spx":                       []byte("setCostume \"idle\"\n"),
			"assets/index.json":                []byte(`{}`),
			"assets/sounds/Known/index.json":   []byte(`{}`),
			"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"idle"}]}`),
		})
		params := []XGoRenameResourceParams{
			{Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/Known"}, NewName: "Other"},
			{Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Runner"}, NewName: "Renamed"},
			{Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Runner/costumes/idle"}, NewName: "walk"},
		}
		want, err := s.renameResources(params)
		require.NoError(t, err)
		require.Len(t, want.Changes["file:///main.spx"], 4)
		require.Len(t, want.Changes["file:///Runner.spx"], 1)
		proj := s.getProj()
		cached := requireFrameworkAnalysis(t, proj)
		snapshot := New(proj.Snapshot(), nil, s.fileMapGetter, &MockScheduler{}, s.listPkgs, s.lookupPkgDoc)
		snapshot.workspaceRootURI = "file:///snapshot/"
		proj.PutFile("main.spx", &xgo.File{Content: []byte("play \"Other\"\n")})
		require.NoError(t, proj.RenameFile("Runner.spx", "Renamed.spx"))
		assert.NotSame(t, cached, requireFrameworkAnalysis(t, proj))
		got, err := snapshot.renameResources(params)
		require.NoError(t, err)
		require.Len(t, got.Changes, len(want.Changes))
		for uri, edits := range want.Changes {
			filename, err := s.fromDocumentURI(uri)
			require.NoError(t, err)
			assert.ElementsMatch(t, edits, got.Changes[snapshot.toDocumentURI(filename)])
		}
		assert.Same(t, cached, requireFrameworkAnalysis(t, snapshot.getProj()))
	})

	t.Run("ConcurrentDiagnostics", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx":          []byte("func show(name PropertyName) {}\nshow \"Missing\"\n"),
			"assets/index.json": []byte(`{}`),
		})
		proj := s.getProj()
		cached := requireFrameworkAnalysis(t, proj)
		want, err := s.diagnosticsAt(proj)
		require.NoError(t, err)
		require.NotEmpty(t, want.diagnostics["file:///main.spx"])
		var wg sync.WaitGroup
		results := make([]*diagnosticResult, 12)
		errs := make([]error, len(results))
		for i := range results {
			wg.Go(func() { results[i], errs[i] = s.diagnosticsAt(proj) })
		}
		wg.Wait()
		for i := range results {
			require.NoError(t, errs[i])
			assert.Equal(t, want.diagnostics, results[i].diagnostics)
		}
		assert.Same(t, cached, requireFrameworkAnalysis(t, proj))
	})
}

func TestResolveFrameworkAdapterCache(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		name := "Available"
		if unavailable {
			name = "Unavailable"
		}
		t.Run(name, func(t *testing.T) {
			s := newSpxTestServer(t, map[string][]byte{"main.spx": []byte("println 1\n")})
			proj := s.getProj()
			module := proj.Module()
			fallback := proj.Importer
			var imports int
			proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
				if path == SpxPkgPath {
					imports++
					if unavailable {
						return nil, errors.New("SDK unavailable")
					}
				}
				return fallback.Import(path)
			})
			first := resolveFrameworkAdapter(proj)
			if unavailable {
				assert.Nil(t, first)
				assert.Nil(t, resolveFrameworkAdapter(proj))
			} else {
				require.NotNil(t, first)
				assert.Same(t, first, resolveFrameworkAdapter(proj))
				assert.Same(t, first, newSpxAnalysis(proj).spxSymbols)
			}
			assert.Equal(t, 1, imports)
			snapshot := proj.Snapshot()
			proj.SetModule(newTestServer(t, nil).getProj().Module())
			assert.Nil(t, resolveFrameworkAdapter(proj))
			assert.Equal(t, 1, imports)
			unavailable = false
			proj.SetModule(module)
			recovered := resolveFrameworkAdapter(proj)
			require.NotNil(t, recovered)
			assert.Equal(t, 2, imports)
			if first == nil {
				assert.Nil(t, resolveFrameworkAdapter(snapshot))
			} else {
				assert.Same(t, first, resolveFrameworkAdapter(snapshot))
				assert.NotSame(t, first, recovered)
			}
			other := newSpxTestServer(t, nil)
			otherAdapter := resolveFrameworkAdapter(other.getProj())
			require.NotNil(t, otherAdapter)
			assert.NotSame(t, recovered, otherAdapter)
			assert.NotSame(t, requireValueAs[*spxSymbols](t, recovered).pkg, requireValueAs[*spxSymbols](t, otherAdapter).pkg)
		})
	}
}
