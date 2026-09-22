package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerGenericMemberReferences(t *testing.T) {
	s := newClassfileTestServer(t, map[string][]byte{
		"main.xgo": []byte(`import f "example.com/first/internal/base"
type Other struct { Value int }
func configureInt(opts f.Box[int]?) {}
func configureText(opts f.Box[string]?) {}
func use(a f.Box[int], b f.Box[string], other Other) {
echo a.Value
echo b.Value
configureInt value = 1
configureText value = "text"
echo other.Value
}
`),
		"other.xgo": []byte("import f \"example.com/first/internal/base\"\nfunc extra(c f.Box[bool]) {\necho c.Value\n}\n"),
	})
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	want := []Location{
		{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 5, Character: 7}, End: Position{Line: 5, Character: 12}}},
		{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 6, Character: 7}, End: Position{Line: 6, Character: 12}}},
		{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 7, Character: 13}, End: Position{Line: 7, Character: 18}}},
		{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 8, Character: 14}, End: Position{Line: 8, Character: 19}}},
		{URI: "file:///other.xgo", Range: Range{Start: Position{Line: 2, Character: 7}, End: Position{Line: 2, Character: 12}}},
	}
	for _, location := range want {
		params := TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: location.URI},
			Position:     location.Range.Start,
		}
		refs, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: params})
		require.NoError(t, err)
		assert.ElementsMatch(t, want, refs)
		highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: params})
		require.NoError(t, err)
		require.NotNil(t, highlights)
		var wantHighlights []DocumentHighlight
		for _, ref := range want {
			if ref.URI == location.URI {
				wantHighlights = append(wantHighlights, DocumentHighlight{Range: ref.Range, Kind: Read})
			}
		}
		assert.ElementsMatch(t, wantHighlights, *highlights)
	}
}

func TestServerTextDocumentReferences(t *testing.T) {
	t.Run("KwargFactory", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			method string
			kwarg  string
		}{
			{name: "DifferentNames", method: "Limit", kwarg: "limit"},
			{name: "SameNames", method: "Params", kwarg: "Params"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const template = `type Client struct{}
type Params interface { Limit(value int) Params }
var client Client
func (c Client) Params() Params { return nil }
func (c Client) complete(text string, params Params?) {}
client.complete "text", limit = 2
client.Params()
`
				source := strings.ReplaceAll(template, "Limit", tt.method)
				source = strings.Replace(source, "limit =", tt.kwarg+" =", 1)
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
				uri := DocumentURI("file:///main.xgo")
				declaration := Range{Start: Position{Line: 3, Character: 16}, End: Position{Line: 3, Character: 22}}
				use := Range{Start: Position{Line: 6, Character: 7}, End: Position{Line: 6, Character: 13}}
				for _, position := range []Position{declaration.Start, use.Start} {
					for _, includeDeclaration := range []bool{false, true} {
						refs, err := s.textDocumentReferences(&ReferenceParams{
							TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position},
							Context:                    ReferenceContext{IncludeDeclaration: includeDeclaration},
						})
						require.NoError(t, err)
						want := []Location{{URI: uri, Range: use}}
						if includeDeclaration {
							want = append(want, Location{URI: uri, Range: declaration})
						}
						assert.ElementsMatch(t, want, refs)
					}
					edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position, NewName: "Options"})
					require.NoError(t, err)
					assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{uri: {{Range: declaration, NewText: "Options"}, {Range: use, NewText: "Options"}}})
				}
				// The kwarg still refers to its interface method, independently of the factory.
				refs, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 1, Character: 24},
				}})
				require.NoError(t, err)
				assert.Equal(t, []Location{{URI: uri, Range: Range{Start: Position{Line: 5, Character: 24}, End: Position{Line: 5, Character: 24 + uint32(len(tt.kwarg))}}}}, refs)
			})
		}
	})

	t.Run("ImportedReceivers", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			parameters   string
			call         string
			extraMethods string
			implements   bool
		}{
			{name: "PointerVariable", parameters: "worker *f.App, ", call: "worker.|read(1)", implements: true},
			{name: "Expression", call: "new(f.App).|read(1)", implements: true},
			{name: "Alias", call: "new(f.AppAlias).|read(1)", implements: true},
			{name: "Instantiated", call: "new(f.Wrapper[int]).|read(1)", implements: true},
			{name: "PromotedMethod", call: "new(f.Item).|read(1)", extraMethods: "Main()", implements: true},
			{name: "MissingMethod", call: "new(f.App).|read(1)", extraMethods: "Main()"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, worker := typeDisplayTestSource(t, "import f \"example.com/first\"\n"+
					"type Reader interface { Read(n int) int; "+tt.extraMethods+" }\n"+
					"func use("+tt.parameters+"reader Reader) {\n"+tt.call+"\nreader.read(1)\n}\n")
				_, reader := typeDisplayTestSource(t, strings.Replace(source, "reader.read", "reader.|read", 1))
				s := newClassfileTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
				for _, position := range []Position{worker, reader} {
					for _, includeDeclaration := range []bool{false, true} {
						refs, err := s.textDocumentReferences(&ReferenceParams{
							TextDocumentPositionParams: TextDocumentPositionParams{
								TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
							},
							Context: ReferenceContext{IncludeDeclaration: includeDeclaration},
						})
						require.NoError(t, err)
						positions := []Position{position}
						if tt.implements {
							positions = []Position{worker, reader}
						}
						if includeDeclaration && position == reader {
							positions = append(positions, Position{Line: 1, Character: 24})
						}
						var want []Location
						for _, start := range positions {
							end := start
							end.Character += 4
							want = append(want, Location{URI: "file:///main.xgo", Range: Range{Start: start, End: end}})
						}
						assert.ElementsMatch(t, want, refs)
					}
				}
			})
		}
	})

	t.Run("UsedInterfaces", func(t *testing.T) {
		for _, tt := range []struct {
			name          string
			declarations  string
			interfaceType string
			method        string
			signature     string
			workerArgs    string
			runnerArgs    string
			implements    bool
		}{
			{name: "Anonymous", interfaceType: "interface { Run() }", method: "Run", signature: "()", implements: true},
			{name: "AnonymousAlias", declarations: "type Runner = interface { Run() }\n", interfaceType: "Runner", method: "Run", signature: "()", implements: true},
			{name: "Embedded", declarations: "type Runner interface { Run() }\n", interfaceType: "interface { Runner }", method: "Run", signature: "()", implements: true},
			{name: "ExtraMethod", interfaceType: "interface { Run(); Stop() }", method: "Run", signature: "()"},
			{name: "DifferentSignature", interfaceType: "interface { Run(int) }", method: "Run", signature: "()", runnerArgs: "1"},
			{name: "Imported", declarations: "import f \"example.com/first\"\n", interfaceType: "f.PublicReader", method: "Read", signature: "(n int) int", workerArgs: "1", runnerArgs: "1", implements: true},
			{name: "ImportedAlias", declarations: "import f \"example.com/first\"\n", interfaceType: "f.ReaderAlias", method: "Read", signature: "(n int) int", workerArgs: "1", runnerArgs: "1", implements: true},
			{name: "ImportedEmbedding", declarations: "import f \"example.com/first\"\n", interfaceType: "f.CombinedReader", method: "Read", signature: "(n int) int", workerArgs: "1", runnerArgs: "1", implements: true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				body := ""
				if tt.method == "Read" {
					body = "return n"
				}
				method := strings.ToLower(tt.method[:1]) + tt.method[1:]
				source := tt.declarations + "type Worker struct{}\nfunc (*Worker) " + tt.method + tt.signature + " { " + body + " }\n" +
					"func " + tt.method + "() {}\n" +
					"func use(worker *Worker, runner " + tt.interfaceType + ") {\n" +
					"worker." + method + "(" + tt.workerArgs + ")\n" +
					"runner." + method + "(" + tt.runnerArgs + ")\n" +
					"runner." + method + "(" + tt.runnerArgs + ")\n" + tt.method + "()\n}\n"
				s := newClassfileTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
				_, worker := typeDisplayTestSource(t, strings.Replace(source, "worker."+method, "worker.|"+method, 1))
				runner := worker
				runner.Line++
				again := runner
				again.Line++
				for _, position := range []Position{worker, runner, again} {
					refs, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
					}})
					require.NoError(t, err)
					positions := []Position{worker, runner, again}
					if !tt.implements {
						if position == worker {
							positions = []Position{worker}
						} else {
							positions = []Position{runner, again}
						}
					}
					var want []Location
					for _, start := range positions {
						end := start
						end.Character += uint32(len(tt.method))
						want = append(want, Location{URI: "file:///main.xgo", Range: Range{Start: start, End: end}})
					}
					assert.ElementsMatch(t, want, refs)
				}
			})
		}
	})

	t.Run("MethodSets", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			filename     string
			source       string
			receiver     string
			localTypes   string
			extraMethods string
			implements   bool
			newServer    testServerFactory
		}{
			{
				name: "PointerReceiver", filename: "worker.xgo", receiver: "*Worker", newServer: newTestServer, implements: true,
				source: "type Worker struct{}\nfunc (*Worker) Run() {}\n",
			},
			{
				name: "NormalClass", filename: "Worker.gox", receiver: "*Worker", newServer: newTestServer, implements: true,
				source: "func Run() {}\n",
			},
			{
				name: "ProjectClass", filename: "main_fixture.gox", receiver: "*App", newServer: newFrameworkTestServer, implements: true,
				source: "func Run() {}\n",
			},
			{
				name: "WorkClass", filename: "Worker_fixture.gox", receiver: "*Worker", newServer: newFrameworkTestServer, implements: true,
				source: "func Run() {}\n",
			},
			{
				name: "PromotedMethod", filename: "worker.xgo", receiver: "Base", newServer: newTestServer, implements: true,
				extraMethods: "Stop()",
				source:       "type Base struct{}\nfunc (Base) Run() {}\ntype Worker struct { Base }\nfunc (Worker) Stop() {}\n",
			},
			{
				name: "PromotedPointerMethod", filename: "worker.xgo", receiver: "*Base", newServer: newTestServer, implements: true,
				extraMethods: "Stop()",
				source:       "type Base struct{}\nfunc (*Base) Run() {}\ntype Worker struct { *Base }\nfunc (*Worker) Stop() {}\n",
			},
			{
				name: "LocalPromotedMethod", filename: "worker.xgo", receiver: "Base", newServer: newTestServer, implements: true,
				extraMethods: "Stop()", localTypes: "type Worker struct { Base; Stopper }\n",
				source: "type Base struct{}\nfunc (Base) Run() {}\ntype Stopper struct{}\nfunc (Stopper) Stop() {}\n",
			},
			{
				name: "ShadowedMethod", filename: "worker.xgo", receiver: "Base", newServer: newTestServer,
				extraMethods: "Stop()",
				source:       "type Base struct{}\nfunc (Base) Run() {}\ntype Worker struct { Base; Run int }\nfunc (Worker) Stop() {}\n",
			},
			{
				name: "AmbiguousMethod", filename: "worker.xgo", receiver: "Base", newServer: newTestServer,
				extraMethods: "Stop()",
				source:       "type Base struct{}\nfunc (Base) Run() {}\ntype Other struct{}\nfunc (Other) Run() {}\ntype Worker struct { Base; Other }\nfunc (Worker) Stop() {}\n",
			},
			{
				name: "LocalInterface", filename: "worker.xgo", receiver: "*Worker", newServer: newTestServer, implements: true,
				localTypes: "type Local interface { Run() }\nvar runner Local\n",
				source:     "type Worker struct{}\nfunc (*Worker) Run() {}\n",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := "func use(worker " + tt.receiver + ", runner Runner) {\n" + tt.localTypes + "worker.run()\nrunner.run()\n}\n"
				// Use a local interface in place of the package interface.
				if tt.name == "LocalInterface" {
					source = "func use(worker " + tt.receiver + ") {\n" + tt.localTypes + "worker.run()\nrunner.run()\n}\n"
				}
				files := map[string][]byte{
					tt.filename:     []byte(tt.source),
					"main.xgo":      []byte(source),
					"interface.xgo": []byte("type Runner interface { Run(); " + tt.extraMethods + " }\n"),
				}
				if tt.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := tt.newServer(t, files)
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
				_, worker := typeDisplayTestSource(t, strings.Replace(source, "worker.run()", "worker.|run()", 1))
				_, runner := typeDisplayTestSource(t, strings.Replace(source, "runner.run()", "runner.|run()", 1))
				for _, position := range []Position{worker, runner} {
					params := &ReferenceParams{TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
					}}
					refs, err := s.textDocumentReferences(params)
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
	})

	for _, tt := range []struct {
		name               string
		filename           string
		includeDeclaration bool
	}{
		{name: "XGo", filename: "main.xgo", includeDeclaration: true},
		{name: "Gop", filename: "main.gop", includeDeclaration: true},
		{name: "NormalClass", filename: "Record.gox", includeDeclaration: true},
		{name: "ExcludeDeclaration", filename: "main.xgo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				tt.filename: []byte("var value int\nfunc use() { println(value) }\n"),
			})
			uri := s.toDocumentURI(tt.filename)
			refs, err := s.textDocumentReferences(&ReferenceParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: uri},
					Position:     Position{Line: 1, Character: 21},
				},
				Context: ReferenceContext{IncludeDeclaration: tt.includeDeclaration},
			})
			require.NoError(t, err)
			want := []Location{{
				URI: uri,
				Range: Range{
					Start: Position{Line: 1, Character: 21},
					End:   Position{Line: 1, Character: 26},
				},
			}}
			if tt.includeDeclaration {
				want = append(want, Location{
					URI: uri,
					Range: Range{
						Start: Position{Line: 0, Character: 4},
						End:   Position{Line: 0, Character: 9},
					},
				})
			}
			assert.ElementsMatch(t, want, refs)
		})
	}

	for _, tt := range []struct {
		name    string
		uri     DocumentURI
		wantErr bool
	}{
		{name: "MissingFile", uri: "file:///missing.xgo"},
		{name: "NonSourceFile", uri: "file:///notes.txt"},
		{name: "InvalidURI", uri: "https://example.com/main.xgo", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"notes.txt": []byte("var value int")})
			refs, err := s.textDocumentReferences(&ReferenceParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: tt.uri},
				},
			})
			if tt.wantErr {
				assert.ErrorContains(t, err, "workspace root URI")
			} else {
				assert.NoError(t, err)
			}
			assert.Nil(t, refs)
		})
	}

	t.Run("FileUpdatesWithTypeErrors", func(t *testing.T) {
		files := map[string][]byte{
			"main.xgo": []byte("var value int\nfunc use() { println(value) }\n"),
		}
		s := newTestServer(t, files)
		params := &ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 0, Character: 4},
			},
			Context: ReferenceContext{IncludeDeclaration: true},
		}
		refs, err := s.textDocumentReferences(params)
		require.NoError(t, err)
		require.Len(t, refs, 2)

		s.ModifyFiles([]FileChange{{
			Path:    "main.xgo",
			Content: append(files["main.xgo"], []byte("func again() { println(value); missing() }\n")...),
			Version: 1,
		}})
		refs, err = s.textDocumentReferences(params)
		require.NoError(t, err)
		assert.ElementsMatch(t, []Location{
			{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}},
			{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 1, Character: 21}, End: Position{Line: 1, Character: 26}}},
			{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 2, Character: 23}, End: Position{Line: 2, Character: 28}}},
		}, refs)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.ErrorContains(t, err, "undefined: missing")
	})

	t.Run("FrameworkClasses", func(t *testing.T) {
		m := map[string][]byte{
			"main_fixture.gox": []byte(`
Worker.apply Low
Worker.apply High
`),
			"Worker_fixture.gox": []byte(`
onValue amount => {
	Worker.apply High
}
`),
		}
		s := newFrameworkTestServer(t, m)

		workerRefs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 1, Character: 0},
			},
			Context: ReferenceContext{
				IncludeDeclaration: true,
			},
		})
		require.NoError(t, err)
		require.Len(t, workerRefs, 3)
		assert.Contains(t, workerRefs, Location{
			URI: "file:///main_fixture.gox",
			Range: Range{
				Start: Position{Line: 1, Character: 0},
				End:   Position{Line: 1, Character: 6},
			},
		})
		assert.Contains(t, workerRefs, Location{
			URI: "file:///main_fixture.gox",
			Range: Range{
				Start: Position{Line: 2, Character: 0},
				End:   Position{Line: 2, Character: 6},
			},
		})
		assert.Contains(t, workerRefs, Location{
			URI: "file:///Worker_fixture.gox",
			Range: Range{
				Start: Position{Line: 2, Character: 1},
				End:   Position{Line: 2, Character: 7},
			},
		})

		applyRefs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 1, Character: 7},
			},
			Context: ReferenceContext{
				IncludeDeclaration: true,
			},
		})
		require.NoError(t, err)
		require.Len(t, applyRefs, 3)
		assert.Contains(t, applyRefs, Location{
			URI: "file:///main_fixture.gox",
			Range: Range{
				Start: Position{Line: 1, Character: 7},
				End:   Position{Line: 1, Character: 12},
			},
		})
		assert.Contains(t, applyRefs, Location{
			URI: "file:///main_fixture.gox",
			Range: Range{
				Start: Position{Line: 2, Character: 7},
				End:   Position{Line: 2, Character: 12},
			},
		})
		assert.Contains(t, applyRefs, Location{
			URI: "file:///Worker_fixture.gox",
			Range: Range{
				Start: Position{Line: 2, Character: 8},
				End:   Position{Line: 2, Character: 13},
			},
		})
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("InvalidPosition", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`var x int`),
		}
		s := newTestServer(t, m)

		refs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 99, Character: 99},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, refs)
	})

	t.Run("MethodsAcrossFiles", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			position Position
		}{
			{name: "ConcreteMethod", position: Position{Line: 1, Character: 11}},
			{name: "EmbeddedMethod", position: Position{Line: 4, Character: 12}},
			{name: "NestedEmbeddedMethod", position: Position{Line: 5, Character: 10}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"types.xgo": []byte(`type Runner interface { Run() }
type Extended interface { Runner }
type Unrelated interface { Stop() }
type Worker struct{}
func (Worker) Run() {}
type Wrapper struct { Worker }
type Outer struct { Wrapper }
type Different struct{}
func (Different) Run(int) {}
`),
					"main.xgo": []byte(`func use(worker Worker, runner Runner, extended Extended, wrapper Wrapper, outer Outer, different Different) {
    worker.run()
    runner.run()
    extended.run()
    wrapper.run()
    outer.run()
    different.run 1
}
`),
				})
				refs, err := s.textDocumentReferences(&ReferenceParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
						Position:     tt.position,
					},
					Context: ReferenceContext{IncludeDeclaration: true},
				})
				require.NoError(t, err)
				assert.ElementsMatch(t, []Location{
					{URI: "file:///types.xgo", Range: Range{Start: Position{Line: 4, Character: 14}, End: Position{Line: 4, Character: 17}}},
					{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 1, Character: 11}, End: Position{Line: 1, Character: 14}}},
					{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 2, Character: 11}, End: Position{Line: 2, Character: 14}}},
					{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 3, Character: 13}, End: Position{Line: 3, Character: 16}}},
					{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 4, Character: 12}, End: Position{Line: 4, Character: 15}}},
					{URI: "file:///main.xgo", Range: Range{Start: Position{Line: 5, Character: 10}, End: Position{Line: 5, Character: 13}}},
				}, refs)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("KwargField", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

func main() {
	configure count = 1
	configure count = 2
}
`),
			"other.xgo": []byte("func other() { configure count = 3 }\n"),
		}
		s := newTestServer(t, m)

		refs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 2, Character: 4},
			},
			Context: ReferenceContext{
				IncludeDeclaration: true,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, refs)
		require.Len(t, refs, 4)
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 2, Character: 1},
				End:   Position{Line: 2, Character: 6},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 8, Character: 11},
				End:   Position{Line: 8, Character: 16},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 9, Character: 11},
				End:   Position{Line: 9, Character: 16},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///other.xgo",
			Range: Range{
				Start: Position{Line: 0, Character: 25},
				End:   Position{Line: 0, Character: 30},
			},
		})
	})

	t.Run("OverloadKwargField", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Worker struct{}

type CountOptions struct {
    Count int
}

type NameOptions struct {
    Name string
}

var worker Worker

func (w *Worker) handleCount(opts CountOptions?) {}
func (w *Worker) handleName(opts NameOptions?) {}

func (Worker).handle = (
    (Worker).handleCount
    (Worker).handleName
)

func main() {
    worker.handle count = 1
    worker.handle count = 2
}
`),
		}
		s := newTestServer(t, m)

		refs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 5},
			},
			Context: ReferenceContext{
				IncludeDeclaration: true,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, refs)
		require.Len(t, refs, 3)
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 4, Character: 4},
				End:   Position{Line: 4, Character: 9},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 22, Character: 18},
				End:   Position{Line: 22, Character: 23},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 23, Character: 18},
				End:   Position{Line: 23, Character: 23},
			},
		})
	})

	t.Run("KwargInterfaceMethod", func(t *testing.T) {
		m := map[string][]byte{
			"main.xgo": []byte(`
type Client struct{}

type Params interface {
	MaxTokens(n int64) Params
}

var client Client

func (c Client) Params() Params { return nil }

func (c Client) complete(prompt string, params Params?) {}

func main() {
	client.complete "hi", maxTokens = 1
	client.complete "bye", maxTokens = 2
}
`),
		}
		s := newTestServer(t, m)

		refs, err := s.textDocumentReferences(&ReferenceParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 14, Character: 25},
			},
			Context: ReferenceContext{
				IncludeDeclaration: true,
			},
		})
		require.NoError(t, err)
		require.NotNil(t, refs)
		require.Len(t, refs, 3)
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 4, Character: 1},
				End:   Position{Line: 4, Character: 10},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 14, Character: 23},
				End:   Position{Line: 14, Character: 32},
			},
		})
		assert.Contains(t, refs, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 15, Character: 24},
				End:   Position{Line: 15, Character: 33},
			},
		})
	})
}

func TestServerTextDocumentReferencesMethodRelations(t *testing.T) {
	for _, kind := range []struct {
		name     string
		filename string
	}{
		{name: "XGo", filename: "main.xgo"},
		{name: "NormalClass", filename: "Record.gox"},
		{name: "ProjectClass", filename: "main_fixture.gox"},
		{name: "WorkClass", filename: "Worker_fixture.gox"},
	} {
		t.Run(kind.name, func(t *testing.T) {
			for _, marked := range []string{
				"type Reader interface { Read() int }\ntype Other interface { Read() int }\nfunc use(reader Reader, other Other) { println reader.|Read(), other.|Read() }\n",
				"type Reader interface { Read() int }\ntype Other interface { Reader; Read() int }\nfunc use(reader Reader, other Other) { println reader.|Read(), other.|Read() }\n",
				"type Reader interface { Read() int }\ntype Closer interface { Read() int; Close() }\ntype Value struct{}\nfunc (Value) Read() int { return 1 }\nfunc (Value) Close() {}\ntype Other struct{}\nfunc (Other) Read() int { return 1 }\nfunc use(reader Reader, closer Closer) { println reader.|Read(), closer.|Read(), Value{}.|Read(), Other{}.|Read() }\n",
			} {
				parts := strings.Split(marked, "|")
				source := strings.Join(parts, "")
				files := map[string][]byte{kind.filename: []byte(source)}
				if kind.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := newFrameworkTestServer(t, files)
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
				var want []Location
				prefix := ""
				for _, part := range parts[:len(parts)-1] {
					prefix += part
					start := Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(UTF16Len(prefix[strings.LastIndexByte(prefix, '\n')+1:]))}
					end := start
					end.Character += 4
					want = append(want, Location{URI: s.toDocumentURI(kind.filename), Range: Range{Start: start, End: end}})
				}
				for _, target := range want {
					got, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: target.URI}, Position: target.Range.Start}})
					require.NoError(t, err)
					assert.ElementsMatch(t, want, got)
				}
			}
		})
	}
}
