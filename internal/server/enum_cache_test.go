package server

import (
	"fmt"
	gotypes "go/types"
	"slices"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func enumMemberNames(info *enumInfo) []string {
	names := make([]string, len(info.members))
	for i, member := range info.members {
		names[i] = member.ident.Name
	}
	slices.Sort(names)
	return names
}

func TestEnumInfoForProject(t *testing.T) {
	t.Run("PreserveAnalyzedSyntax", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`func work() {
	var value int
	for value = range 0:limit+1 {
		println value
	}
}
work
`),
			"values.xgo": []byte("const limit = 1\n"),
		})
		proj := s.getProj()
		_, err := enumInfoForProject(proj)
		require.NoError(t, err)
		original, err := proj.ASTFile("main.xgo")
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
		current, err := proj.ASTFile("main.xgo")
		require.NoError(t, err)
		assert.NotSame(t, original, current)
	})

	t.Run("EditDuringTypeChecking", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo":  []byte("import _ \"example.com/paused\"\n"),
			"enums.xgo": []byte("type Color const (\nRed = iota\n)\n"),
		})
		proj := s.getProj()
		dependency := gotypes.NewPackage("example.com/paused", "paused")
		dependency.MarkComplete()
		entered, resume := make(chan struct{}), make(chan struct{})
		release := sync.OnceFunc(func() { close(resume) })
		t.Cleanup(release)
		var calls atomic.Int32
		fallback := proj.Importer
		proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
			if path == dependency.Path() {
				if calls.Add(1) == 1 {
					close(entered)
					<-resume
				}
				return dependency, nil
			}
			return fallback.Import(path)
		})
		var wg sync.WaitGroup
		var info *enumInfo
		var err error
		wg.Go(func() { info, err = enumInfoForProject(proj) })
		<-entered
		proj.PutFile("enums.xgo", &xgo.File{Content: []byte("type Color const (\nBlue = iota\n)\n")})
		release()
		wg.Wait()
		require.NoError(t, err)
		assert.Equal(t, []string{"Blue"}, enumMemberNames(info))
		require.Len(t, info.members, 1)
		require.NotNil(t, info.members[0].object)
		assert.Equal(t, "Blue", info.members[0].object.Name())
	})

	t.Run("ConcurrentEdits", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`func work() {
	var value int
	for value = range 0:limit+1 {
		println value
	}
}
work
`),
			"values.xgo": []byte("const limit = 1\n"),
		})
		proj := s.getProj()
		_, err := proj.TypeInfo()
		require.NoError(t, err)
		var wg sync.WaitGroup
		start := make(chan struct{})
		wg.Go(func() {
			<-start
			for range 200 {
				buildEnumInfoCache(proj)
			}
		})
		var typeErr error
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
		require.NoError(t, typeErr)
	})

	t.Run("BuildOrder", func(t *testing.T) {
		for _, tt := range []struct {
			name string
			warm func(*xgo.Project) (any, error)
		}{
			{"Syntax", func(proj *xgo.Project) (any, error) { return proj.ASTPackage() }},
			{"Documentation", func(proj *xgo.Project) (any, error) { return proj.PkgDoc() }},
			{"Types", func(proj *xgo.Project) (any, error) { return proj.TypeInfo() }},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newFrameworkTestServer(t, map[string][]byte{
					"main_fixture.gox": []byte("type Color const (\n// Red documentation.\nRed = iota\n)\nvar color Color = Red\n"),
				})
				proj := s.syncProject()
				_, err := tt.warm(proj)
				require.NoError(t, err)
				enums, err := enumInfoForProject(proj)
				require.NoError(t, err)
				require.Len(t, enums.members, 1)
				assert.Equal(t, "Red documentation.\n", enums.members[0].doc)
				items := completionItemsAt(t, s, "main_fixture.gox", Position{Line: 4, Character: 19})
				item := completionItemByLabel(items, "Red")
				require.NotNil(t, item)
				assert.Equal(t, EnumMemberCompletion, item.Kind)
				cached, err := enumInfoForProject(proj)
				require.NoError(t, err)
				assert.Same(t, enums, cached)
			})
		}
	})

	t.Run("SharedAcrossRequests", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"enums.xgo": []byte("type Color const (\nRed = iota\nBlue\n)\n"),
			"main.xgo":  []byte("var color Color = Red\n"),
		})
		var builds int
		s.getProj().RegisterCacheBuilder(enumInfoCacheKind{}, func(proj *xgo.Project) (any, error) {
			builds++
			return buildEnumInfoCache(proj)
		})
		proj := s.requestProject()
		info, err := enumInfoForProject(proj)
		require.NoError(t, err)
		assert.Equal(t, []string{"Blue", "Red"}, enumMemberNames(info))
		uri := DocumentURI("file:///main.xgo")
		for range 2 {
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Character: 19},
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, "Red")
			links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: uri}})
			require.NoError(t, err)
			assert.NotEmpty(t, links)
			tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{TextDocument: TextDocumentIdentifier{URI: uri}})
			require.NoError(t, err)
			require.NotNil(t, tokens)
			assert.Contains(t, decodeSemanticTokens(tokens.Data), decodedSemanticToken{
				line: 0, character: 18, length: 3, tokenType: EnumMemberType,
			})
			items := completionItemsAt(t, s, "main.xgo", Position{Character: 19})
			require.NotNil(t, completionItemByLabel(items, "Red"))
			current, err := enumInfoForProject(proj)
			require.NoError(t, err)
			assert.Same(t, info, current)
		}
		assert.Equal(t, 1, builds)
	})

	t.Run("DeclarationInAnotherFile", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"enums.xgo": []byte("type Color const (\nRed = iota\n)\n"),
			"main.xgo":  []byte("var color Color = Red\n"),
		})
		position := Position{Character: 19}
		require.NotNil(t, completionItemByLabel(completionItemsAt(t, s, "main.xgo", position), "Red"))
		proj := s.getProj()
		original, err := enumInfoForProject(proj)
		require.NoError(t, err)
		snapshot := New(proj.Snapshot(), nil, s.fileMapGetter, &MockScheduler{}, s.listPkgs, s.lookupPkgDoc, nil)
		s.ModifyFiles([]FileChange{{
			Path: "enums.xgo", Content: []byte("type Color const (\nRuby = iota\n)\n"), Version: 1,
		}})
		items := completionItemsAt(t, s, "main.xgo", position)
		assert.NotNil(t, completionItemByLabel(items, "Ruby"))
		assert.Nil(t, completionItemByLabel(items, "Red"))
		current, err := enumInfoForProject(proj)
		require.NoError(t, err)
		assert.NotSame(t, original, current)
		items = completionItemsAt(t, snapshot, "main.xgo", position)
		assert.NotNil(t, completionItemByLabel(items, "Red"))
		assert.Nil(t, completionItemByLabel(items, "Ruby"))
		old, err := enumInfoForProject(snapshot.getProj())
		require.NoError(t, err)
		assert.Same(t, original, old)
	})

	t.Run("EditsAndPartialTypes", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("type Color const (\nRed = iota\n)\n"),
		})
		proj := s.getProj()
		original, err := enumInfoForProject(proj)
		require.NoError(t, err)
		snapshot := proj.Snapshot()
		for version, source := range []string{
			"type Color const (\nBlue = iota\n)\nvar broken = missing\n",
			"type Color const (\nBlue = iota\n)\n",
		} {
			s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(source), Version: version + 1}})
			_, typeErr := proj.TypeInfo()
			if version == 0 {
				require.Error(t, typeErr)
			} else {
				require.NoError(t, typeErr)
			}
			current, err := enumInfoForProject(proj)
			require.NoError(t, err)
			assert.NotSame(t, original, current)
			assert.Equal(t, []string{"Blue"}, enumMemberNames(current))
			old, err := enumInfoForProject(snapshot)
			require.NoError(t, err)
			assert.Same(t, original, old)
			assert.Equal(t, []string{"Red"}, enumMemberNames(old))
		}
		require.NoError(t, proj.RenameFile("main.xgo", "colors.xgo"))
		renamed, err := enumInfoForProject(proj)
		require.NoError(t, err)
		require.Len(t, renamed.members, 1)
		assert.Equal(t, "colors.xgo", proj.Fset.PositionFor(renamed.members[0].ident.Pos(), false).Filename)
		require.NoError(t, proj.DeleteFile("colors.xgo"))
		empty, err := enumInfoForProject(proj)
		require.NoError(t, err)
		assert.Empty(t, empty.members)
	})

	t.Run("ConcurrentAndIndependentProjects", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("type Color const (\nRed = iota\n)\n")})
		proj := s.getProj()
		var builds atomic.Int32
		proj.RegisterCacheBuilder(enumInfoCacheKind{}, func(proj *xgo.Project) (any, error) {
			builds.Add(1)
			return buildEnumInfoCache(proj)
		})
		var wg sync.WaitGroup
		results := make([]*enumInfo, 24)
		errs := make([]error, len(results))
		for i := range results {
			wg.Go(func() { results[i], errs[i] = enumInfoForProject(proj) })
		}
		wg.Wait()
		for i := range results {
			require.NoError(t, errs[i])
			assert.Same(t, results[0], results[i])
		}
		assert.EqualValues(t, 1, builds.Load())
		other := newTestServer(t, map[string][]byte{"main.xgo": []byte("type Color const (\nRed = iota\n)\n")})
		independent, err := enumInfoForProject(other.getProj())
		require.NoError(t, err)
		require.Len(t, independent.members, 1)
		require.Len(t, results[0].members, 1)
		assert.NotSame(t, results[0].members[0].object, independent.members[0].object)
	})

	t.Run("UnavailableCache", func(t *testing.T) {
		info, err := enumInfoForProject(xgo.NewProject(nil, nil, xgo.FeatAll))
		assert.Nil(t, info)
		assert.ErrorIs(t, err, xgo.ErrUnknownCacheKind)
	})
}
