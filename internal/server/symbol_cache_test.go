package server

import (
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const symbolCacheSource = `type Reader interface { Read() int }
type Item struct{}
func (Item) Read() int { return 1 }
func use(reader Reader) { println reader.Read(), Item{}.read }
`

func symbolCacheMethod(t testing.TB, proj *xgo.Project) *gotypes.Func {
	t.Helper()
	info, _ := proj.TypeInfo()
	require.NotNil(t, info)
	obj := info.Pkg.Scope().Lookup("Reader")
	require.NotNil(t, obj)
	iface, ok := obj.Type().Underlying().(*gotypes.Interface)
	require.True(t, ok)
	return iface.Method(0)
}

func TestServerSymbolAnalysis(t *testing.T) {
	t.Run("SharedRequests", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(symbolCacheSource)})
		s.replier = newMockReplier()
		var sourceBuilds, methodBuilds atomic.Int32
		s.getProj().RegisterCacheBuilder(sourceInfoCacheKind{}, func(proj *xgo.Project) (any, error) {
			sourceBuilds.Add(1)
			return buildSourceInfoCache(proj)
		})
		s.getProj().RegisterCacheBuilder(methodInfoCacheKind{}, func(proj *xgo.Project) (any, error) {
			methodBuilds.Add(1)
			return buildMethodInfoCache(proj)
		})
		position := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: Position{Character: 24}}
		for range 3 {
			refs, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: position})
			require.NoError(t, err)
			assert.Len(t, refs, 2)
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: position.TextDocument, Position: position.Position, NewName: "Fetch"})
			require.NoError(t, err)
			require.NotNil(t, edit)
			assert.Equal(t, strings.NewReplacer("Read()", "Fetch()", "{}.read", "{}.fetch").Replace(symbolCacheSource), applyResourceRenameTestEdits(t, symbolCacheSource, edit.Changes[position.TextDocument.URI]))
			impl, err := s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: position})
			require.NoError(t, err)
			locations, ok := impl.([]Location)
			require.True(t, ok)
			assert.Len(t, locations, 1)
			highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: position})
			require.NoError(t, err)
			require.NotNil(t, highlights)
			assert.Len(t, *highlights, 2)
		}
		assert.EqualValues(t, 1, sourceBuilds.Load())
		assert.EqualValues(t, 1, methodBuilds.Load())
	})

	t.Run("ConcurrentColdRequests", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(symbolCacheSource)})
		const count = 24
		refs := make([][]Location, count)
		implementations := make([]any, count)
		errs := make([]error, count*2)
		position := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: Position{Character: 24}}
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range count {
			wg.Go(func() {
				<-start
				refs[i], errs[i] = s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: position})
			})
			wg.Go(func() {
				<-start
				implementations[i], errs[count+i] = s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: position})
			})
		}
		close(start)
		wg.Wait()
		for _, err := range errs {
			require.NoError(t, err)
		}
		for i := range count {
			assert.Len(t, refs[i], 2)
			locations, ok := implementations[i].([]Location)
			require.True(t, ok)
			assert.Len(t, locations, 1)
		}
	})

	t.Run("InstanceAndURIIsolation", func(t *testing.T) {
		first := newTestServer(t, map[string][]byte{"main.xgo": []byte(symbolCacheSource)})
		second := newTestServer(t, map[string][]byte{"main.xgo": []byte(symbolCacheSource)})
		firstProj, secondProj := first.requestProject(), second.requestProject()
		firstMethod, secondMethod := symbolCacheMethod(t, firstProj), symbolCacheMethod(t, secondProj)
		firstSource, err := sourceInfoForProject(firstProj)
		require.NoError(t, err)
		secondSource, err := sourceInfoForProject(secondProj)
		require.NoError(t, err)
		assert.NotSame(t, firstSource, secondSource)
		assert.Empty(t, firstSource.references[secondMethod])
		assert.Empty(t, secondSource.references[firstMethod])
		firstMethods, err := methodInfoForProject(firstProj)
		require.NoError(t, err)
		secondMethods, err := methodInfoForProject(secondProj)
		require.NoError(t, err)
		assert.NotSame(t, firstMethods, secondMethods)
		assert.Len(t, firstMethods.relatedMethods(firstMethod), 2)
		assert.Equal(t, []*gotypes.Func{secondMethod}, firstMethods.relatedMethods(secondMethod))
		first.workspaceRootURI = "file:///first/"
		second.workspaceRootURI = "file:///second/"
		for _, s := range []*Server{first, second} {
			refs, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: Position{Character: 24},
			}})
			require.NoError(t, err)
			require.Len(t, refs, 2)
			for _, ref := range refs {
				assert.Equal(t, s.toDocumentURI("main.xgo"), ref.URI)
			}
		}
	})

	t.Run("KwargDocumentIdentity", func(t *testing.T) {
		const source = "type Options struct { Count int }\nfunc use(opts Options?) {}\nuse count = 1\n"
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		for _, uri := range []DocumentURI{"file:///main.xgo", "file:///m%61in.xgo"} {
			highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 2, Character: 4},
			}})
			require.NoError(t, err)
			require.NotNil(t, highlights)
			assert.ElementsMatch(t, []DocumentHighlight{
				{Range: Range{Start: Position{Character: 22}, End: Position{Character: 27}}, Kind: Write},
				{Range: Range{Start: Position{Line: 2, Character: 4}, End: Position{Line: 2, Character: 9}}, Kind: Read},
			}, *highlights)
		}
	})
}

func TestProjectSymbolCaches(t *testing.T) {
	t.Run("EditsAndSnapshots", func(t *testing.T) {
		for _, state := range []struct {
			name string
			warm bool
		}{{"Cold", false}, {"Warm", true}} {
			t.Run(state.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(symbolCacheSource)})
				proj := s.getProj()
				method := symbolCacheMethod(t, proj)
				if state.warm {
					_, err := sourceInfoForProject(proj)
					require.NoError(t, err)
					_, err = methodInfoForProject(proj)
					require.NoError(t, err)
				}
				snapshot := proj.Snapshot()
				for version, edit := range []struct {
					source      string
					wantTypeErr bool
					wantMethods int
				}{
					{strings.ReplaceAll(symbolCacheSource, "func (Item) Read() int { return 1 }", "func (Item) Read() string { return missing }"), true, 1},
					{symbolCacheSource, false, 2},
				} {
					proj.PutFile("main.xgo", &xgo.File{Content: []byte(edit.source), Version: version + 1})
					_, typeErr := proj.TypeInfo()
					if edit.wantTypeErr {
						require.Error(t, typeErr)
					} else {
						require.NoError(t, typeErr)
					}
					currentSource, err := sourceInfoForProject(proj)
					require.NoError(t, err)
					assert.Empty(t, currentSource.references[method])
					currentMethod := symbolCacheMethod(t, proj)
					assert.Len(t, currentSource.references[currentMethod], 1)
					currentMethods, err := methodInfoForProject(proj)
					require.NoError(t, err)
					assert.Len(t, currentMethods.relatedMethods(currentMethod), edit.wantMethods)
					oldSource, err := sourceInfoForProject(snapshot)
					require.NoError(t, err)
					assert.Len(t, oldSource.references[method], 1)
					oldMethods, err := methodInfoForProject(snapshot)
					require.NoError(t, err)
					assert.Len(t, oldMethods.relatedMethods(method), 2)
				}
				require.NoError(t, proj.RenameFile("main.xgo", "other.xgo"))
				current, err := sourceInfoForProject(proj)
				require.NoError(t, err)
				refs := current.references[symbolCacheMethod(t, proj)]
				require.Len(t, refs, 1)
				assert.Equal(t, "other.xgo", proj.Fset.PositionFor(refs[0].ident.Pos(), false).Filename)
				require.NoError(t, proj.DeleteFile("other.xgo"))
				empty, err := sourceInfoForProject(proj)
				require.NoError(t, err)
				assert.Empty(t, empty.references)
				methods, err := methodInfoForProject(proj)
				require.NoError(t, err)
				assert.Empty(t, methods.receivers)
			})
		}
	})

	t.Run("EditDuringAnalysis", func(t *testing.T) {
		for _, kind := range []struct {
			name string
			load func(*xgo.Project) (any, error)
		}{
			{"Source", func(proj *xgo.Project) (any, error) { return sourceInfoForProject(proj) }},
			{"Methods", func(proj *xgo.Project) (any, error) { return methodInfoForProject(proj) }},
		} {
			t.Run(kind.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte("import _ \"example.com/paused\"\n" + symbolCacheSource),
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
				var result any
				var err error
				wg.Go(func() { result, err = kind.load(proj) })
				<-entered
				proj.PutFile("main.xgo", &xgo.File{Content: []byte("type Reader interface { Read() int }\nfunc use(reader Reader) { println reader.Read() }\n")})
				release()
				wg.Wait()
				require.NoError(t, err)
				current, err := kind.load(proj)
				require.NoError(t, err)
				// The edit invalidates the in-flight build. Its pinned result
				// remains usable, but must not replace the new revision's cache.
				assert.NotSame(t, result, current)
				switch result := result.(type) {
				case *sourceInfo:
					var readRefs []sourceIdent
					for obj, refs := range result.references {
						if obj.Name() == "Read" {
							readRefs = append(readRefs, refs...)
						}
					}
					require.Len(t, readRefs, 1)
					assert.NotContains(t, string(readRefs[0].file.Code), "Item")
				case *methodInfo:
					require.Len(t, result.relations["Read"](), 1)
				}
				cached, err := kind.load(proj)
				require.NoError(t, err)
				assert.Same(t, current, cached)
				method := symbolCacheMethod(t, proj)
				source, err := sourceInfoForProject(proj)
				require.NoError(t, err)
				require.Len(t, source.references[method], 1)
				assert.Equal(t, uint32(1), RangeForNode(proj, source.references[method][0].ident).Start.Line)
				methods, err := methodInfoForProject(proj)
				require.NoError(t, err)
				assert.Equal(t, []*gotypes.Func{method}, methods.relatedMethods(method))
			})
		}
	})

	t.Run("ModuleChange", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": nil, "Worker_fixture.gox": []byte("func Read() int { return 1 }\nfunc use() { println read }\n")})
		before := s.requestProject()
		oldSource, err := sourceInfoForProject(before)
		require.NoError(t, err)
		oldMethods, err := methodInfoForProject(before)
		require.NoError(t, err)
		mod := testframework.NewModule(t)
		mod.Opt.Projects[0].Works[0].Prefix = "Actor"
		s.getProj().SetModule(newTestModule(t, mod.Module))
		after := s.requestProject()
		currentSource, err := sourceInfoForProject(after)
		require.NoError(t, err)
		currentMethods, err := methodInfoForProject(after)
		require.NoError(t, err)
		assert.NotSame(t, oldSource, currentSource)
		assert.NotSame(t, oldMethods, currentMethods)
		var method *gotypes.Func
		for obj := range currentSource.references {
			if fn, ok := obj.(*gotypes.Func); ok && fn.Name() == "Read" {
				method = fn
				assert.NotContains(t, oldSource.references, fn)
			}
		}
		require.NotNil(t, method)
		assert.Contains(t, method.FullName(), "ActorWorker")
	})
}

func TestServerSymbolAnalysisImportedOverloadContracts(t *testing.T) {
	for _, tt := range []struct {
		name     string
		contract string
		call     string
	}{
		{"Function", "func Accept__0(value interface { Read() int }) {}\nfunc Accept__1(value string) {}\n", "contract.accept missing"},
		{"Method", "type Client struct{}\nfunc (Client) Accept__0(value interface { Read() int }) {}\nfunc (Client) Accept__1(value string) {}\n", "var client contract.Client\nclient.accept missing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := "import contract \"example.com/contract\"\ntype Item struct{}\nfunc (Item) Read() int { return 1 }\n" + tt.call + "\n"
			s := newSymbolContractTestServer(t, source, tt.contract)
			_, err := s.requestProject().TypeInfo()
			require.ErrorContains(t, err, "undefined: missing")
			edit, err := s.textDocumentRename(&RenameParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 2, Character: 12}, NewName: "Fetch",
			})
			assert.ErrorContains(t, err, "no editable project declaration")
			assert.Nil(t, edit)
		})
	}

	t.Run("Kwarg", func(t *testing.T) {
		const contract = `type Options interface { Count(int) Options }
type Client struct{}
func (Client) Options() Options { return nil }
func (Client) Complete__0(prefix int, opts Options) {}
func (Client) Complete__1(prefix string, opts Options) {}
`
		const source = "import contract \"example.com/contract\"\nvar client contract.Client\nclient.complete missing, count = 1\n"
		s := newSymbolContractTestServer(t, source, contract)
		for version, prefix := range []string{"missing", "1"} {
			current := strings.Replace(source, "missing", prefix, 1)
			s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(current), Version: version + 1}})
			_, err := s.requestProject().TypeInfo()
			if prefix == "missing" {
				require.ErrorContains(t, err, "undefined: missing")
			} else {
				require.NoError(t, err)
			}
			position := Position{Line: 2, Character: uint32(len("client.complete ") + len(prefix) + len(", "))}
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position}
			for range 2 {
				result, err := s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: params})
				require.NoError(t, err)
				locations, ok := result.([]Location)
				require.True(t, ok)
				assert.Empty(t, locations)
				refs, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: params})
				require.NoError(t, err)
				assert.Equal(t, []Location{{URI: params.TextDocument.URI, Range: Range{
					Start: position, End: Position{Line: position.Line, Character: position.Character + 5},
				}}}, refs)
			}
		}
	})
}

func TestServerSymbolAnalysisGenericContracts(t *testing.T) {
	const contract = `type Reader[T any] interface { Read() T }
type Derived[T any] interface { Reader[T] }
type Alias[T any] = Reader[T]
`
	for _, typ := range []string{"Reader", "Derived", "Alias"} {
		t.Run(typ, func(t *testing.T) {
			source := `import contract "example.com/contract"
type Item struct{}
func (Item) Read() int { return 1 }
type Text struct{}
func (Text) Read() string { return "" }
func use(reader contract.` + typ + `[int], other contract.` + typ + `[string]) {
println reader.read, other.read, Item{}.read, Text{}.read
}
`
			s := newSymbolContractTestServer(t, source, contract)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			const uri DocumentURI = "file:///main.xgo"
			var wantRefs []Location
			for _, expression := range []string{"reader.read", "other.read", "Item{}.read", "Text{}.read"} {
				marked := strings.Replace(source, expression, strings.Replace(expression, ".", ".|", 1), 1)
				_, position := typeDisplayTestSource(t, marked)
				wantRefs = append(wantRefs, Location{URI: uri, Range: Range{Start: position, End: Position{Line: position.Line, Character: position.Character + 4}}})
			}
			for range 2 {
				for i, line := range []uint32{2, 4} {
					params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: wantRefs[i].Range.Start}
					impl, err := s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: params})
					require.NoError(t, err)
					declaration := Position{Line: line, Character: 12}
					assert.Equal(t, []Location{{URI: uri, Range: Range{Start: declaration, End: declaration}}}, impl)
					refs, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: params})
					require.NoError(t, err)
					assert.ElementsMatch(t, wantRefs, refs)
					edit, err := s.textDocumentRename(&RenameParams{TextDocument: params.TextDocument, Position: declaration, NewName: "Fetch"})
					assert.ErrorContains(t, err, "no editable project declaration")
					assert.Nil(t, edit)
				}
			}
		})
	}
}

func newSymbolContractTestServer(t *testing.T, source, contract string) *Server {
	t.Helper()
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
	s.replier = newMockReplier()
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "contract.go", "package contract\nconst XGoPackage = true\n"+contract, 0)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check("example.com/contract", fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	fallback := s.getProj().Importer
	s.getProj().Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
		if path == pkg.Path() {
			return pkg, nil
		}
		return fallback.Import(path)
	})
	return s
}
