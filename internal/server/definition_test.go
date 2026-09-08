package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentDefinition(t *testing.T) {
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
				"values.xgo": []byte("var value int\n"),
				tt.filename:  []byte("println value\n"),
			})
			def, err := s.textDocumentDefinition(&DefinitionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
					Position:     Position{Line: 0, Character: 8},
				},
			})
			require.NoError(t, err)
			assert.Equal(t, Location{
				URI:   "file:///values.xgo",
				Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}},
			}, requireValueAs[Location](t, def))
			_, err = s.workspaceRootFS.TypeInfo()
			assert.NoError(t, err)
		})
	}

	t.Run("FileUpdatesWithTypeErrors", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"values.xgo": []byte("var value int\n"),
			"main.xgo":   []byte("println value\n"),
		})
		params := &DefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Position:     Position{Line: 0, Character: 8},
		}}
		def, err := s.textDocumentDefinition(params)
		require.NoError(t, err)
		assert.Equal(t, Location{
			URI:   "file:///values.xgo",
			Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}},
		}, requireValueAs[Location](t, def))

		s.ModifyFiles([]FileChange{{
			Path:    "values.xgo",
			Content: []byte("\nvar value int\nfunc broken() { missing() }\n"),
			Version: 1,
		}})
		def, err = s.textDocumentDefinition(params)
		require.NoError(t, err)
		assert.Equal(t, Location{
			URI:   "file:///values.xgo",
			Range: Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 9}},
		}, requireValueAs[Location](t, def))
		_, err = s.workspaceRootFS.TypeInfo()
		assert.ErrorContains(t, err, "undefined: missing")
	})

	t.Run("FrameworkClasses", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main_fixture.gox": []byte(`
Worker.apply Low
`),
			"Worker_fixture.gox": []byte(`
onValue amount => {
	Worker.apply High
}
`),
		})

		workerDef, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 1, Character: 0},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, workerDef)

		applyDef, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 1, Character: 7},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, applyDef)

		workerClassDef, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///Worker_fixture.gox"},
				Position:     Position{Line: 2, Character: 1},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, workerClassDef)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("FrameworkMembers", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			position Position
			want     Location
		}{
			{
				name:     "ProjectField",
				position: Position{Line: 1, Character: 4},
				want: Location{
					URI:   "file:///main_fixture.gox",
					Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}},
				},
			},
			{
				name:     "CallbackParameter",
				position: Position{Line: 1, Character: 12},
				want: Location{
					URI:   "file:///Worker_fixture.gox",
					Range: Range{Start: Position{Line: 0, Character: 8}, End: Position{Line: 0, Character: 14}},
				},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main_fixture.gox": []byte("var total int\n"),
					"Worker_fixture.gox": []byte(`onValue amount => {
    total = amount
}
`),
				})
				def, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///Worker_fixture.gox"},
					Position:     tt.position,
				}})
				require.NoError(t, err)
				assert.Equal(t, tt.want, requireValueAs[Location](t, def))
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("BuiltinType", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
var x int
`),
		})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 1, Character: 6},
			},
		})
		require.NoError(t, err)
		require.Nil(t, def)
	})

	t.Run("ThisPtr", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
		}{
			{name: "ProjectClass", filename: "main_fixture.gox"},
			{name: "WorkClass", filename: "Worker_fixture.gox"},
			{name: "NormalClass", filename: "Record.gox"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{"main_fixture.gox": nil}
				files[tt.filename] = []byte("println this\n")
				s := newTestServer(t, files)
				def, err := s.textDocumentDefinition(&DefinitionParams{
					TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
						Position:     Position{Line: 0, Character: 8},
					},
				})
				require.NoError(t, err)
				assert.Nil(t, def)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("BlankIdent", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
const _ = 1
`),
		})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 1, Character: 6},
			},
		})
		require.NoError(t, err)
		require.Nil(t, def)
	})

	t.Run("ExplicitThis", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("var this int\nprintln this\n")})
		def, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Position:     Position{Line: 1, Character: 8},
		}})
		require.NoError(t, err)
		assert.Equal(t, Location{
			URI:   "file:///main.xgo",
			Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 8}},
		}, requireValueAs[Location](t, def))
	})

	t.Run("InvalidPosition", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
var x int
`),
		})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 99, Character: 99},
			},
		})
		require.NoError(t, err)
		require.Nil(t, def)
	})

	t.Run("ImportedPackage", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
import "fmt"
fmt.println "Hello, XGo!"
`),
		})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 2, Character: 0},
			},
		})
		require.NoError(t, err)
		loc := requireValueAs[Location](t, def)
		assert.Equal(t, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 1, Character: 7},
				End:   Position{Line: 1, Character: 7},
			},
		}, loc)
	})

	t.Run("ImportedPackageWithAlias", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
import fmt2 "fmt"
fmt2.println "Hello, XGo!"
`),
		})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 2, Character: 0},
			},
		})
		require.NoError(t, err)
		loc := requireValueAs[Location](t, def)
		assert.Equal(t, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 1, Character: 7},
				End:   Position{Line: 1, Character: 11},
			},
		}, loc)
	})

	t.Run("InvalidTextDocument", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
var x int
`),
		})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "bucket:///main.xgo"},
				Position:     Position{Line: 99, Character: 99},
			},
		})
		require.ErrorContains(t, err, "failed to get file path from document URI")
		require.Nil(t, def)
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

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 8, Character: 14},
			},
		})
		require.NoError(t, err)
		loc := requireValueAs[Location](t, def)
		assert.Equal(t, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 2, Character: 4},
				End:   Position{Line: 2, Character: 9},
			},
		}, loc)
	})

	t.Run("MapKwargHasNoDefinition", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func configure(opts map[string]int?) {}

func main() {
    configure count = 1
}
`),
		})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 14},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, def)
	})

	t.Run("NestedKwargField", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type OuterOptions struct {
    Count int
}

type InnerOptions struct {
    Name string
}

func makeValue(opts InnerOptions?) int { return 0 }

func configure(value int, opts OuterOptions?) {}

func main() {
    configure makeValue(name = "x"), count = 1
}
`),
		})

		innerDef, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 14, Character: 25},
			},
		})
		require.NoError(t, err)
		innerLoc := requireValueAs[Location](t, innerDef)
		assert.Equal(t, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 6, Character: 4},
				End:   Position{Line: 6, Character: 8},
			},
		}, innerLoc)

		outerDef, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 14, Character: 38},
			},
		})
		require.NoError(t, err)
		outerLoc := requireValueAs[Location](t, outerDef)
		assert.Equal(t, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 2, Character: 4},
				End:   Position{Line: 2, Character: 9},
			},
		}, outerLoc)
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

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 22, Character: 19},
			},
		})
		require.NoError(t, err)
		loc := requireValueAs[Location](t, def)
		assert.Equal(t, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 4, Character: 4},
				End:   Position{Line: 4, Character: 9},
			},
		}, loc)
	})

	t.Run("OverloadKwargFieldDisambiguatesByValue", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Worker struct{}

type Options0 struct {
    Handler func()
}

type Options1 struct {
    Handler func(int)
}

var worker Worker

func (w *Worker) handle0(opts Options0?) {}
func (w *Worker) handle1(opts Options1?) {}

func (Worker).handle = (
    (Worker).handle0
    (Worker).handle1
)

func main() {
    worker.handle handler = (n) => {
        echo n
    }
}
`),
		})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 22, Character: 20},
			},
		})
		require.NoError(t, err)
		loc := requireValueAs[Location](t, def)
		assert.Equal(t, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 8, Character: 4},
				End:   Position{Line: 8, Character: 11},
			},
		}, loc)
	})

	t.Run("NonOptionalKwargField", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Options struct {
    Count int
}

func configure(opts Options) {}

func main() {
    configure count = 1
}
`),
		})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 8, Character: 14},
			},
		})
		require.NoError(t, err)
		loc := requireValueAs[Location](t, def)
		assert.Equal(t, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 2, Character: 4},
				End:   Position{Line: 2, Character: 9},
			},
		}, loc)
	})

	t.Run("KwargInterfaceMethod", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
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
}
`),
		})

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 14, Character: 25},
			},
		})
		require.NoError(t, err)
		loc := requireValueAs[Location](t, def)
		assert.Equal(t, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 4, Character: 1},
				End:   Position{Line: 4, Character: 10},
			},
		}, loc)
	})
}

func TestServerTextDocumentTypeDefinition(t *testing.T) {
	for _, tt := range []struct {
		name     string
		filename string
		typeExpr string
		wantLine uint32
	}{
		{name: "XGo", filename: "main.xgo", typeExpr: "Value"},
		{name: "Gop", filename: "main.gop", typeExpr: "Value"},
		{name: "NormalClass", filename: "Record.gox", typeExpr: "Value"},
		{name: "Pointer", filename: "main.xgo", typeExpr: "*Value"},
		{name: "AliasAcrossFiles", filename: "main.xgo", typeExpr: "Alias", wantLine: 1},
		{name: "PointerToAlias", filename: "main.xgo", typeExpr: "*Alias", wantLine: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"types.xgo": []byte("type Value struct{}\ntype Alias = Value\n"),
				tt.filename: []byte("var value " + tt.typeExpr + "\n_ = value\n"),
			})
			def, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
				Position:     Position{Line: 1, Character: 4},
			}})
			require.NoError(t, err)
			assert.Equal(t, Location{
				URI:   "file:///types.xgo",
				Range: Range{Start: Position{Line: tt.wantLine, Character: 5}, End: Position{Line: tt.wantLine, Character: 5}},
			}, requireValueAs[Location](t, def))
			_, err = s.workspaceRootFS.TypeInfo()
			assert.NoError(t, err)
		})
	}

	t.Run("AliasType", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type MyType struct{}
type MyAlias = MyType
var x MyAlias
`),
		})

		def, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 3, Character: 4},
			},
		})
		require.NoError(t, err)
		loc := requireValueAs[Location](t, def)
		assert.Equal(t, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 2, Character: 5},
				End:   Position{Line: 2, Character: 5},
			},
		}, loc)
	})

	t.Run("KwargField", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Handler func()

type Options struct {
    Handler Handler
}

func configure(opts Options?) {}

func main() {
    configure handler = () => {}
}
`),
		})

		def, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 10, Character: 14},
			},
		})
		require.NoError(t, err)
		loc := requireValueAs[Location](t, def)
		assert.Equal(t, Location{
			URI: "file:///main.xgo",
			Range: Range{
				Start: Position{Line: 1, Character: 5},
				End:   Position{Line: 1, Character: 5},
			},
		}, loc)
	})

	t.Run("FrameworkTypes", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			source   string
			position Position
			want     *Location
		}{
			{name: "ImportedType", source: "var item Item\n", position: Position{Line: 0, Character: 9}},
			{name: "ImportedValue", source: "var item Item\n", position: Position{Line: 0, Character: 4}},
			{name: "ImportedPointer", source: "var item *Item\n", position: Position{Line: 0, Character: 4}},
			{name: "GeneratedClass", source: "var worker *Worker\n", position: Position{Line: 0, Character: 4}},
			{
				name:     "LocalAlias",
				source:   "type Local = Item\nvar item Local\n",
				position: Position{Line: 1, Character: 4},
				want: &Location{
					URI:   "file:///main_fixture.gox",
					Range: Range{Start: Position{Line: 0, Character: 5}, End: Position{Line: 0, Character: 5}},
				},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main_fixture.gox":   []byte(tt.source),
					"Worker_fixture.gox": nil,
				})
				def, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
					Position:     tt.position,
				}})
				require.NoError(t, err)
				if tt.want == nil {
					assert.Nil(t, def)
				} else {
					assert.Equal(t, *tt.want, requireValueAs[Location](t, def))
				}
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("FileUpdatesWithTypeErrors", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"types.xgo": []byte("type First struct{}\ntype Second struct{}\n"),
			"main.xgo":  []byte("var value First\nprintln value\n"),
		})
		params := &TypeDefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Position:     Position{Line: 1, Character: 8},
		}}
		def, err := s.textDocumentTypeDefinition(params)
		require.NoError(t, err)
		assert.Equal(t, Location{
			URI:   "file:///types.xgo",
			Range: Range{Start: Position{Line: 0, Character: 5}, End: Position{Line: 0, Character: 5}},
		}, requireValueAs[Location](t, def))

		s.ModifyFiles([]FileChange{{
			Path:    "main.xgo",
			Content: []byte("var value Second\nprintln value\nmissing()\n"),
			Version: 1,
		}})
		def, err = s.textDocumentTypeDefinition(params)
		require.NoError(t, err)
		assert.Equal(t, Location{
			URI:   "file:///types.xgo",
			Range: Range{Start: Position{Line: 1, Character: 5}, End: Position{Line: 1, Character: 5}},
		}, requireValueAs[Location](t, def))
		_, err = s.workspaceRootFS.TypeInfo()
		assert.ErrorContains(t, err, "undefined: missing")
	})

	t.Run("BuiltinType", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
var x int
`),
		})

		def, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 1, Character: 6},
			},
		})
		require.NoError(t, err)
		require.Nil(t, def)
	})

	t.Run("InvalidPosition", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
var x int
`),
		})

		def, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 99, Character: 99},
			},
		})
		require.NoError(t, err)
		require.Nil(t, def)
	})
}
