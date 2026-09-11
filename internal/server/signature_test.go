package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentSignatureHelp(t *testing.T) {
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
				"functions.xgo": []byte("func combine(count int, label string) {}\n"),
				tt.filename:     []byte("combine 1, \"value\"\n"),
			})
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
					Position:     Position{Line: 0, Character: 13},
				},
			})
			require.NoError(t, err)
			assert.Equal(t, &SignatureHelp{
				Signatures: []SignatureInformation{{
					Label:      "combine(count int, label string)",
					Parameters: []ParameterInformation{{Label: "count int"}, {Label: "label string"}},
				}},
				ActiveParameter: 1,
			}, help)
			_, err = s.workspaceRootFS.TypeInfo()
			assert.NoError(t, err)
		})
	}

	t.Run("FrameworkMethods", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			source   string
			position Position
			label    string
		}{
			{
				name: "ProjectOverload", filename: "main_fixture.gox",
				source: "onStart => {\n    measure 1\n}\n", position: Position{Line: 1, Character: 12},
				label: "measure(value int) int",
			},
			{
				name: "WorkMethod", filename: "Worker_fixture.gox",
				source: "onValue amount => {\n    apply amount\n}\n", position: Position{Line: 1, Character: 12},
				label: "apply(value int)",
			},
			{
				name: "BoundWorkMethod", filename: "main_fixture.gox",
				source: "onStart => {\n    Worker.apply 1\n}\n", position: Position{Line: 1, Character: 17},
				label: "apply(value int)",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"main_fixture.gox":   {},
					"Worker_fixture.gox": {},
				}
				files[tt.filename] = []byte(tt.source)
				s := newFrameworkTestServer(t, files)
				help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
						Position:     tt.position,
					},
				})
				require.NoError(t, err)
				assert.Equal(t, &SignatureHelp{
					Signatures: []SignatureInformation{{
						Label: tt.label, Parameters: []ParameterInformation{{Label: "value int"}},
					}},
				}, help)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("ImportedFunction", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte("import \"fmt\"\nfmt.Println \"value\"\n"),
		})
		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 1, Character: 5},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, &SignatureHelp{
			Signatures: []SignatureInformation{{
				Label: "println(a ...any) (n int, err error)", Parameters: []ParameterInformation{{Label: "a ...any"}},
			}},
		}, help)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("UTF16", func(t *testing.T) {
		for _, tt := range []struct {
			name            string
			character       uint32
			activeParameter uint32
		}{
			{name: "StringArgument", character: 11, activeParameter: 0},
			{name: "FollowingArgument", character: 14, activeParameter: 1},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte("func combine(text string, count int) {}\ncombine \"\U0001F600\", 1\n"),
				})
				help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
						Position:     Position{Line: 1, Character: tt.character},
					},
				})
				require.NoError(t, err)
				assert.Equal(t, &SignatureHelp{
					Signatures: []SignatureInformation{{
						Label:      "combine(text string, count int)",
						Parameters: []ParameterInformation{{Label: "text string"}, {Label: "count int"}},
					}},
					ActiveParameter: tt.activeParameter,
				}, help)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("FileUpdatesWithTypeErrors", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"functions.xgo": []byte("func combine(count int) {}\n"),
			"main.xgo":      []byte("combine 1\n"),
		})
		params := &SignatureHelpParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Position:     Position{Character: 8},
		}}
		help, err := s.textDocumentSignatureHelp(params)
		require.NoError(t, err)
		assert.Equal(t, &SignatureHelp{
			Signatures: []SignatureInformation{{
				Label: "combine(count int)", Parameters: []ParameterInformation{{Label: "count int"}},
			}},
		}, help)
		_, err = s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)

		s.ModifyFiles([]FileChange{
			{Path: "functions.xgo", Content: []byte("func combine(value int, label string) {}\nfunc broken() { missing() }\n"), Version: 1},
			{Path: "main.xgo", Content: []byte("\ncombine 1, \"x\"\n"), Version: 1},
		})
		params.Position = Position{Line: 1, Character: 12}
		help, err = s.textDocumentSignatureHelp(params)
		require.NoError(t, err)
		assert.Equal(t, &SignatureHelp{
			Signatures: []SignatureInformation{{
				Label:      "combine(value int, label string)",
				Parameters: []ParameterInformation{{Label: "value int"}, {Label: "label string"}},
			}},
			ActiveParameter: 1,
		}, help)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.ErrorContains(t, err, "undefined: missing")
	})

	for _, tt := range []struct {
		name         string
		uri          DocumentURI
		source       string
		position     Position
		wantError    bool
		wantASTError bool
		want         *SignatureHelp
	}{
		{name: "EmptyFile", uri: "file:///main.xgo"},
		{name: "MissingFile", uri: "file:///missing.xgo"},
		{name: "NonSourceFile", uri: "file:///notes.txt"},
		{name: "InvalidURI", uri: "https://example.com/main.xgo", wantError: true},
		{
			name: "PositionBeyondEOF", uri: "file:///main.xgo",
			source: "func use(value int) {}\nuse 1\n", position: Position{Line: 100, Character: 100},
			want: &SignatureHelp{Signatures: []SignatureInformation{{
				Label: "use(value int)", Parameters: []ParameterInformation{{Label: "value int"}},
			}}},
		},
		{name: "NonFunction", uri: "file:///main.xgo", source: "var value int\n_ = value\n", position: Position{Line: 1, Character: 4}},
		{name: "StartWithInvalidChar", uri: "file:///main.xgo", source: "\n\u201c\u201dvar (\n    maps []int\n)\n", wantASTError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"main.xgo":  []byte(tt.source),
				"notes.txt": []byte("func use(value int) {}\nuse 1\n"),
			})
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: tt.uri},
					Position:     tt.position,
				},
			})
			if tt.wantError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			assert.Equal(t, tt.want, help)
			_, err = s.workspaceRootFS.ASTPackage()
			if tt.wantASTError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			}
		})
	}

	t.Run("Autoclosure", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`
onStart => {
	runWhen true, => {}
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 2, Character: 12},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label: "runWhen(condition bool, callback func())",
			Parameters: []ParameterInformation{
				{
					Label:         "condition bool",
					Documentation: autoclosureParamDocumentation,
				},
				{
					Label: "callback func()",
				},
			},
		}, help.Signatures[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("FuncDecorator", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`func retry(times int, fn func()) {
	fn()
}

@retry(2)
func run() {
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 8},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, "retry(times int)", help.Signatures[0].Label)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("FuncDecoratorReturningError", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`func log(fn func() error) {
	_ = fn()
}

@log
func run() error {
	return nil
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 3},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, "log()", help.Signatures[0].Label)
		assert.Empty(t, help.Signatures[0].Parameters)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	for _, tt := range []struct {
		name   string
		result string
		want   string
	}{
		{name: "SingleResult", result: "int", want: "answer() int"},
		{name: "SingleNamedResult", result: "(n int)", want: "answer() (n int)"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"main.xgo": []byte("\nfunc answer() " + tt.result + " { return 42 }\n\nfunc main() {\n\tanswer\n}\n"),
			})
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 4, Character: 4},
				},
			})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, tt.want, help.Signatures[0].Label)
			assert.Empty(t, help.Signatures[0].Parameters)
			_, err = s.workspaceRootFS.TypeInfo()
			assert.NoError(t, err)
		})
	}

	t.Run("XGoxMethod", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`
onStart => {
	create Item, "sample"
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label: "create(T Type, name string) *T",
			Parameters: []ParameterInformation{
				{
					Label: "T Type",
				},
				{
					Label: "name string",
				},
			},
		}, help.Signatures[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("PartialXGoxFunction", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`import "example.com/typeargs"

func main() {
	println typeargs.convert(string, 100)
}
`),
		})
		s.workspaceRootFS.Importer = xgoxTestImporter{fallback: s.workspaceRootFS.Importer}

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 3, Character: 36},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(1), help.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label: "convert(To Type, src From) To",
			Parameters: []ParameterInformation{
				{
					Label: "To Type",
				},
				{
					Label: "src From",
				},
			},
		}, help.Signatures[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("KwargField", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
	Count int
}

func configure(opts Options?) {}

func main() {
	configure count = 1
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 8, Character: 13},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label: "configure(opts main.Options)",
			Parameters: []ParameterInformation{
				{
					Label: "opts main.Options",
				},
			},
		}, help.Signatures[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("OverloadKwargField", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
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
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 22, Character: 18},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 1)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label: "handle(opts main.CountOptions)",
			Parameters: []ParameterInformation{
				{
					Label: "opts main.CountOptions",
				},
			},
		}, help.Signatures[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("OverloadIncompleteKwargName", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
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
	worker.handle cou = 1
}
`),
		})

		help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 22, Character: 16},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, help)
		require.Len(t, help.Signatures, 2)
		assert.Equal(t, uint32(0), help.ActiveParameter)
		assert.Equal(t, "handle(opts main.CountOptions)", help.Signatures[0].Label)
		assert.Equal(t, "handle(opts main.NameOptions)", help.Signatures[1].Label)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.Error(t, err)
	})

	t.Run("VariadicKwargActiveParameter", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func process(opts map[string]string?, args ...int) {}

func main() {
    process 1, name = "x"
}
`),
		})

		positionalHelp, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 12},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, positionalHelp)
		require.Len(t, positionalHelp.Signatures, 1)
		assert.Equal(t, uint32(1), positionalHelp.ActiveParameter)
		assert.Equal(t, SignatureInformation{
			Label: "process(opts map[string]string, args ...int)",
			Parameters: []ParameterInformation{
				{
					Label: "opts map[string]string",
				},
				{
					Label: "args ...int",
				},
			},
		}, positionalHelp.Signatures[0])

		kwargHelp, err := s.textDocumentSignatureHelp(&SignatureHelpParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 23},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, kwargHelp)
		require.Len(t, kwargHelp.Signatures, 1)
		assert.Equal(t, uint32(0), kwargHelp.ActiveParameter)
		assert.Equal(t, positionalHelp.Signatures[0], kwargHelp.Signatures[0])
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})
}
