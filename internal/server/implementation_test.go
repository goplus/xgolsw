package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentImplementation(t *testing.T) {
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
