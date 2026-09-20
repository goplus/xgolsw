package server

import (
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentImplementation(t *testing.T) {
	t.Run("ImportedInterfaces", func(t *testing.T) {
		for _, declaration := range []struct {
			name   string
			source string
			typ    string
		}{
			{name: "Named", typ: "f.PublicReader"},
			{name: "Alias", typ: "f.ReaderAlias"},
			{name: "ImportedEmbedding", typ: "f.CombinedReader"},
			{name: "LocalEmbedding", source: "type Reader interface { f.ReaderAlias }\n", typ: "Reader"},
		} {
			t.Run(declaration.name, func(t *testing.T) {
				for _, implementation := range []struct {
					name     string
					filename string
					source   string
					position Position
				}{
					{"PlainXGo", "worker.xgo", "type Worker struct{}\nfunc (*Worker) Read(n int) int { return n }\n", Position{Line: 1, Character: 15}},
					{"NormalClass", "Worker.gox", "func Read(n int) int { return n }\n", Position{Character: 5}},
					{"ProjectClass", "First.first", "func Read(n int) int { return n }\n", Position{Character: 5}},
					{"WorkClass", "Worker.firstwork", "func Read(n int) int { return n }\n", Position{Character: 5}},
				} {
					t.Run(implementation.name, func(t *testing.T) {
						source, position := typeDisplayTestSource(t, "import f \"example.com/first\"\n"+declaration.source+
							"func use(reader "+declaration.typ+") {\nreader.|read(1)\n}\n")
						files := map[string][]byte{"main.xgo": []byte(source), implementation.filename: []byte(implementation.source)}
						if implementation.name == "WorkClass" {
							files["First.first"] = nil
						}
						s := newClassfileTestServer(t, files)
						params := &ImplementationParams{TextDocumentPositionParams: TextDocumentPositionParams{
							TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
						}}
						want := Location{URI: s.toDocumentURI(implementation.filename), Range: Range{Start: implementation.position, End: implementation.position}}
						for version, state := range []struct {
							source  string
							matches bool
							wantErr string
						}{
							{source: implementation.source, matches: true},
							{source: strings.NewReplacer("n int", "n string", "return n", "return 1").Replace(implementation.source)},
							{source: strings.ReplaceAll(implementation.source, "return n", "missing(); return n"), matches: true, wantErr: "undefined: missing"},
						} {
							s.ModifyFiles([]FileChange{{Path: implementation.filename, Content: []byte(state.source), Version: version + 1}})
							_, err := s.requestProject().TypeInfo()
							if state.wantErr != "" {
								require.ErrorContains(t, err, state.wantErr)
							} else {
								require.NoError(t, err)
							}
							got, err := s.textDocumentImplementation(params)
							require.NoError(t, err)
							locations := requireValueAs[[]Location](t, got)
							if state.matches {
								assert.Equal(t, []Location{want}, locations)
							} else {
								assert.Empty(t, locations)
							}
						}
					})
				}
			})
		}
	})

	t.Run("MethodSets", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			filename     string
			source       string
			extraMethods string
			wantTypeErr  bool
			newServer    testServerFactory
			want         []Position
		}{
			{
				name: "PointerReceiver", filename: "worker.xgo", newServer: newTestServer,
				source: "type Worker struct{}\nfunc (*Worker) Run() {}\n",
				want:   []Position{{Line: 1, Character: 15}},
			},
			{
				name: "NormalClass", filename: "Worker.gox", newServer: newTestServer,
				source: "func Run() {}\n",
				want:   []Position{{Character: 5}},
			},
			{
				name: "ProjectClass", filename: "main_fixture.gox", newServer: newFrameworkTestServer,
				source: "func Run() {}\n",
				want:   []Position{{Character: 5}},
			},
			{
				name: "WorkClass", filename: "Worker_fixture.gox", newServer: newFrameworkTestServer,
				source: "func Run() {}\n",
				want:   []Position{{Character: 5}},
			},
			{
				name: "PromotedMethod", filename: "worker.xgo", newServer: newTestServer, extraMethods: "Stop()",
				source: "type Base struct{}\nfunc (Base) Run() {}\ntype Worker struct { Base }\nfunc (Worker) Stop() {}\n",
				want:   []Position{{Line: 1, Character: 12}},
			},
			{
				name: "PromotedPointerMethod", filename: "worker.xgo", newServer: newTestServer, extraMethods: "Stop()",
				source: "type Base struct{}\nfunc (*Base) Run() {}\ntype Worker struct { *Base }\nfunc (*Worker) Stop() {}\n",
				want:   []Position{{Line: 1, Character: 13}},
			},
			{
				name: "LocalPromotedMethod", filename: "worker.xgo", newServer: newTestServer, extraMethods: "Stop()",
				source: "type Base struct{}\nfunc (Base) Run() {}\ntype Stopper struct{}\nfunc (Stopper) Stop() {}\nfunc test() { type Worker struct { Base; Stopper } }\n",
				want:   []Position{{Line: 1, Character: 12}},
			},
			{
				name: "ShadowedMethod", filename: "worker.xgo", newServer: newTestServer, extraMethods: "Stop()",
				source: "type Base struct{}\nfunc (Base) Run() {}\ntype Worker struct { Base; Run int }\nfunc (Worker) Stop() {}\n",
			},
			{
				name: "AmbiguousMethod", filename: "worker.xgo", newServer: newTestServer, extraMethods: "Stop()",
				source: "type Base struct{}\nfunc (Base) Run() {}\ntype Other struct{}\nfunc (Other) Run() {}\ntype Worker struct { Base; Other }\nfunc (Worker) Stop() {}\n",
			},
			{
				name: "InterfaceEmbedding", filename: "worker.xgo", newServer: newTestServer,
				source: "type Other interface { Runner }\n",
			},
			{
				name: "InvalidType", filename: "worker.xgo", newServer: newTestServer,
				source: "type Broken Missing\n", wantTypeErr: true,
			},
			{
				name: "InvalidEmbeddedType", filename: "worker.xgo", newServer: newTestServer,
				source: "type Broken struct { Missing }\n", wantTypeErr: true,
			},
			{
				name: "RecursiveInterface", filename: "worker.xgo", newServer: newTestServer,
				source: "type Broken interface { Broken }\n", wantTypeErr: true,
			},
			{
				name: "RecursiveTypes", filename: "worker.xgo", newServer: newTestServer,
				source: "type First Second\ntype Second First\n", wantTypeErr: true,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				iface := "type Runner interface { Run(); " + tt.extraMethods + " }\n"
				files := map[string][]byte{"interface.xgo": []byte(iface), tt.filename: []byte(tt.source)}
				if tt.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := tt.newServer(t, files)
				_, err := s.requestProject().TypeInfo()
				if tt.wantTypeErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
				implementations, err := s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///interface.xgo"},
					Position:     Position{Character: 24},
				}})
				require.NoError(t, err)
				var want []Location
				for _, position := range tt.want {
					want = append(want, Location{URI: s.toDocumentURI(tt.filename), Range: Range{Start: position, End: position}})
				}
				assert.ElementsMatch(t, want, requireValueAs[[]Location](t, implementations))
			})
		}
	})

	t.Run("ImportedPromotedPosition", func(t *testing.T) {
		s := newClassfileTestServer(t, map[string][]byte{
			"interface.xgo": []byte("type Runner interface { Score() int }\n"),
			"worker.xgo":    []byte(strings.Repeat("// Padding.\n", 128) + "import \"example.com/first\"\ntype Worker struct { framework.App }\n"),
		})
		proj := s.requestProject()
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		worker := info.Pkg.Scope().Lookup("Worker")
		require.NotNil(t, worker)
		method, _, _ := gotypes.LookupFieldOrMethod(worker.Type(), true, info.Pkg, "Score")
		require.NotNil(t, method)
		require.NotSame(t, info.Pkg, method.Pkg())
		require.NotNil(t, proj.Fset.File(method.Pos()), "imported positions must overlap the source file set for this regression")
		implementations, err := s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///interface.xgo"},
			Position:     Position{Character: 24},
		}})
		require.NoError(t, err)
		assert.Empty(t, requireValueAs[[]Location](t, implementations))
	})

	t.Run("MixedFrameworkUpdates", func(t *testing.T) {
		s := newClassfileTestServer(t, map[string][]byte{
			"interface.xgo": []byte("type Runner interface { Run() }\n"),
			"First.first":   []byte("func Run() {}\n"),
			"Second.second": []byte("func Run() {}\n"),
		})
		params := &ImplementationParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///interface.xgo"},
			Position:     Position{Character: 24},
		}}
		for version, tt := range []struct {
			source     string
			wantSecond bool
			wantErr    string
		}{
			{source: "func Run() {}\n", wantSecond: true},
			{source: "func Run(value int) {}\n"},
			{source: "func Run() { missing() }\n", wantSecond: true, wantErr: "undefined: missing"},
		} {
			s.ModifyFiles([]FileChange{{Path: "Second.second", Content: []byte(tt.source), Version: version + 1}})
			implementations, err := s.textDocumentImplementation(params)
			require.NoError(t, err)
			want := []Location{{URI: "file:///First.first", Range: Range{Start: Position{Character: 5}, End: Position{Character: 5}}}}
			if tt.wantSecond {
				want = append(want, Location{URI: "file:///Second.second", Range: Range{Start: Position{Character: 5}, End: Position{Character: 5}}})
			}
			assert.ElementsMatch(t, want, requireValueAs[[]Location](t, implementations))
			_, err = s.requestProject().TypeInfo()
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		}
	})

	for _, tt := range []struct {
		name     string
		filename string
	}{
		{name: "XGo", filename: "main.xgo"},
		{name: "Gop", filename: "main.gop"},
		{name: "NormalClass", filename: "Record.gox"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"types.xgo": []byte("type Worker struct{}\nfunc (Worker) Run() {}\n"),
				tt.filename: []byte("var worker Worker\nworker.run()\n"),
			})
			implementation, err := s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
				Position:     Position{Line: 1, Character: 7},
			}})
			require.NoError(t, err)
			assert.Equal(t, Location{
				URI:   "file:///types.xgo",
				Range: Range{Start: Position{Line: 1, Character: 14}, End: Position{Line: 1, Character: 14}},
			}, requireValueAs[Location](t, implementation))
			_, err = s.workspaceRootFS.TypeInfo()
			assert.NoError(t, err)
		})
	}

	t.Run("FileUpdatesWithTypeErrors", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("type Runner interface { Run() }\n"),
			"types.xgo": []byte(`type Worker struct{}
func (Worker) Run() {}
var worker Worker
`),
			"other.xgo": nil,
		})
		params := &ImplementationParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Position:     Position{Line: 0, Character: 24},
		}}
		implementations, err := s.textDocumentImplementation(params)
		require.NoError(t, err)
		want := Location{
			URI:   "file:///types.xgo",
			Range: Range{Start: Position{Line: 1, Character: 14}, End: Position{Line: 1, Character: 14}},
		}
		assert.ElementsMatch(t, []Location{want}, requireValueAs[[]Location](t, implementations))

		s.ModifyFiles([]FileChange{{
			Path: "other.xgo",
			Content: []byte(`type Other struct{}
func (Other) Run() { missing() }
type Different struct{}
func (Different) Run(int) {}
`),
			Version: 1,
		}})
		implementations, err = s.textDocumentImplementation(params)
		require.NoError(t, err)
		assert.ElementsMatch(t, []Location{want, {
			URI:   "file:///other.xgo",
			Range: Range{Start: Position{Line: 1, Character: 13}, End: Position{Line: 1, Character: 13}},
		}}, requireValueAs[[]Location](t, implementations))
		_, err = s.workspaceRootFS.TypeInfo()
		assert.ErrorContains(t, err, "undefined: missing")
	})

	t.Run("ImportedImplementations", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`import "time"
type Stringer interface {
    String() string
}
var duration time.Duration
type Local struct{}
func (Local) String() string { return "local" }
`)})
		implementations, err := s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Position:     Position{Line: 2, Character: 4},
		}})
		require.NoError(t, err)
		assert.ElementsMatch(t, []Location{{
			URI:   "file:///main.xgo",
			Range: Range{Start: Position{Line: 6, Character: 13}, End: Position{Line: 6, Character: 13}},
		}}, requireValueAs[[]Location](t, implementations))
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("FrameworkClasses", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			position Position
		}{
			{name: "GeneratedClass", position: Position{Line: 0, Character: 0}},
			{name: "ImportedMethod", position: Position{Line: 0, Character: 7}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newFrameworkTestServer(t, map[string][]byte{
					"main_fixture.gox":   []byte("Worker.apply Low\n"),
					"Worker_fixture.gox": nil,
				})
				implementation, err := s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
					Position:     tt.position,
				}})
				require.NoError(t, err)
				assert.Nil(t, implementation)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("InterfaceMethod", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type MyInterface interface {
	myMethod()
}

type MyType struct{}

func (t MyType) myMethod() {}

type MyType2 struct{}

func (t MyType2) myMethod() {}

var x MyInterface
`),
		})

		implementations, err := s.textDocumentImplementation(&ImplementationParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		locations := requireValueAs[[]Location](t, implementations)
		require.Len(t, locations, 2)
		assert.Contains(t, locations, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 7, Character: 16},
				End:   Position{Line: 7, Character: 16},
			},
		})
		assert.Contains(t, locations, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 11, Character: 17},
				End:   Position{Line: 11, Character: 17},
			},
		})
	})

	t.Run("NonInterfaceMethod", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type MyType struct{}

func (t MyType) myMethod() {}
`),
		})

		implementation, err := s.textDocumentImplementation(&ImplementationParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 3, Character: 16},
			},
		})
		require.NoError(t, err)
		location := requireValueAs[Location](t, implementation)
		assert.Equal(t, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 3, Character: 16},
				End:   Position{Line: 3, Character: 16},
			},
		}, location)
	})

	t.Run("KwargInterfaceMethod", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Params interface {
    MaxTokens(n int64) Params
}

type Config struct{}

var client Client

func (c Config) MaxTokens(n int64) Params { return c }

type Client struct{}

func (c Client) Params() Params { return Config{} }

func (c Client) complete(prompt string, params Params?) {}

func main() {
    client.complete "hi", maxTokens = 1
}
`),
		})

		implementation, err := s.textDocumentImplementation(&ImplementationParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 18, Character: 26},
			},
		})
		require.NoError(t, err)
		locations := requireValueAs[[]Location](t, implementation)
		require.Len(t, locations, 1)
		assert.Equal(t, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 9, Character: 16},
				End:   Position{Line: 9, Character: 16},
			},
		}, locations[0])
	})

	for _, tt := range []struct {
		name     string
		uri      DocumentURI
		position Position
		wantErr  bool
	}{
		{name: "MissingFile", uri: "file:///missing.xgo"},
		{name: "NonSourceFile", uri: "file:///notes.txt"},
		{name: "InvalidURI", uri: "https://example.com/main.xgo", wantErr: true},
		{name: "InvalidPosition", uri: "file:///main.xgo", position: Position{Line: 99, Character: 99}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"main.xgo":  []byte("var value int\n"),
				"notes.txt": []byte("var value int\n"),
			})
			implementation, err := s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: tt.uri},
				Position:     tt.position,
			}})
			if tt.wantErr {
				assert.ErrorContains(t, err, "workspace root URI")
			} else {
				assert.NoError(t, err)
			}
			assert.Nil(t, implementation)
		})
	}
}

func TestServerAnonymousReceiverMethods(t *testing.T) {
	for _, tt := range []struct {
		name       string
		setup      string
		call       string
		implements bool
	}{
		{name: "Variable", setup: "var worker struct { Base; Stopper }", call: "worker.|run()", implements: true},
		{name: "Pointer", setup: "var worker *struct { *Base; *Stopper }", call: "worker.|run()", implements: true},
		{name: "Alias", setup: "type Group = struct { Base; Stopper }\nvar worker Group", call: "worker.|run()", implements: true},
		{name: "PointerAlias", setup: "type Group = *struct { Base; Stopper }\nvar worker Group", call: "worker.|run()", implements: true},
		{name: "Literal", setup: "worker := struct { *Base; *Stopper }{}", call: "worker.|run()", implements: true},
		{name: "Expression", call: "new(struct { Base; Stopper }).|run()", implements: true},
		{name: "Field", setup: "var outer struct { Child struct { Base; Stopper } }", call: "outer.Child.|run()", implements: true},
		{name: "MissingMethod", setup: "var worker struct { Base }", call: "worker.|run()"},
		{name: "ShadowedMethod", setup: "var decoy struct { Base; Stopper; Run int }\necho decoy\nvar worker Base", call: "worker.|run()"},
		{name: "AmbiguousMethod", setup: "var decoy struct { Base; Other; Stopper }\necho decoy\nvar worker Base", call: "worker.|run()"},
		{name: "Interface", setup: "var decoy interface { Runner }\necho decoy\nvar worker Base", call: "worker.|run()"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, worker := typeDisplayTestSource(t, "func use(runner Runner) {\n"+tt.setup+"\n"+tt.call+"\nrunner.run()\n}\n")
			_, runner := typeDisplayTestSource(t, strings.Replace(source, "runner.run", "runner.|run", 1))
			s := newTestServer(t, map[string][]byte{
				"main.xgo": []byte(source),
				"types.xgo": []byte("type Runner interface { Run(); Stop() }\ntype Base struct{}\nfunc (*Base) Run() {}\n" +
					"type Stopper struct{}\nfunc (*Stopper) Stop() {}\ntype Other struct{}\nfunc (*Other) Run() {}\n"),
			})
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: runner}
			implementations, err := s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			locations := requireValueAs[[]Location](t, implementations)
			if tt.implements {
				assert.Equal(t, []Location{{URI: "file:///types.xgo", Range: Range{Start: Position{Line: 2, Character: 13}, End: Position{Line: 2, Character: 13}}}}, locations)
			} else {
				assert.Empty(t, locations)
			}
			for _, position := range []Position{worker, runner} {
				params.Position = position
				refs, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: params})
				require.NoError(t, err)
				positions := []Position{position}
				if tt.implements {
					positions = []Position{worker, runner}
				}
				var want []Location
				for _, start := range positions {
					end := start
					end.Character += 3
					want = append(want, Location{URI: "file:///main.xgo", Range: Range{Start: start, End: end}})
				}
				assert.ElementsMatch(t, want, refs)
			}
		})
	}
}
