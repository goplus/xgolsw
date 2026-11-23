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
			Path: "values.xgo",
			Content: []byte(`
var value int
func broken() { missing() }
`),
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
		s := newFrameworkTestServer(t, map[string][]byte{
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
				s := newFrameworkTestServer(t, map[string][]byte{
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
			name        string
			filename    string
			projectFile string
			newServer   testServerFactory
		}{
			{name: "ProjectClass", filename: "main_fixture.gox", newServer: newFrameworkTestServer},
			{name: "WorkClass", filename: "Worker_fixture.gox", projectFile: "main_fixture.gox", newServer: newFrameworkTestServer},
			{name: "NormalClass", filename: "Record.gox", newServer: newTestServer},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{tt.filename: []byte("println this\n")}
				if tt.projectFile != "" {
					files[tt.projectFile] = nil
				}
				s := tt.newServer(t, files)
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
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(`var this int
println this
`)})
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

	t.Run("ShortUnicodeKwargHasNoDefinition", func(t *testing.T) {
		const filename = "main.xgo"
		source, pos := typeDisplayTestSource(t, "type Options struct { \u2c7e int }\n"+
			"func configure(opts Options?) {}\nconfigure |\u023f = 1\n")
		s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
		t.Cleanup(s.Close)
		_, err := s.requestProject().TypeInfo()
		require.Error(t, err)

		def, err := s.textDocumentDefinition(&DefinitionParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(filename)},
				Position:     pos,
			},
		})
		require.NoError(t, err)
		assert.Nil(t, def)
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

func TestServerOverloadDefinition(t *testing.T) {
	const declarations = `func supply(value int) int { return value }
func Choose = (
supply
)
`
	for _, tt := range []struct {
		name, source string
		definition   uint32
	}{
		{name: "ParenthesizedCall", source: "_ = (Ch|oose)(1)\n"},
		{name: "AliasReference", source: "func Other = (Ch|oose)\n", definition: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, declarations+tt.source)
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)

			uri := s.toDocumentURI("main.xgo")
			locationAt := func(line uint32) Location {
				return Location{URI: uri, Range: Range{Start: Position{Line: line, Character: 5}, End: Position{Line: line, Character: 11}}}
			}
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: position}
			definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			assert.Equal(t, locationAt(tt.definition), definition)
			declaration, err := s.textDocumentDeclaration(&DeclarationParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			assert.Equal(t, locationAt(1), declaration)
		})
	}
}

func TestServerForwardedOverloadDefinition(t *testing.T) {
	for _, tt := range []struct {
		name, source, implementation string
		definition, declaration      Position
	}{
		{
			name: "ParenthesizedAcrossGroups",
			source: `func pickFirst(value any) int { return 1 }
func pickText(value string) int { return len(value) }
func Pick = (
	pickFirst
	pickText
)
func Relay = (Pick)
func Run = (Relay)
_ = ((Ru|n))("text")
`,
			implementation: "pickFirst",
			definition:     Position{Character: 5},
			declaration:    Position{Line: 7, Character: 5},
		},
		{
			name: "RequiredArguments",
			source: `func pickLong(value string, level int) {}
func pickShort(value string) {}
func Pick = (
	pickLong
	pickShort
)
func Run = (Pick)
Ru|n("text")
`,
			implementation: "pickShort",
			definition:     Position{Line: 1, Character: 5},
			declaration:    Position{Line: 6, Character: 5},
		},
		{
			name: "FunctionVariable",
			source: `func pickNumber(value int) int { return value }
var pickText = func(value string) int { return len(value) }
func Pick = (
	pickNumber
	pickText
)
func Run = (Pick)
_ = Ru|n("text")
`,
			implementation: "pickText",
			definition:     Position{Line: 1, Character: 4},
			declaration:    Position{Line: 6, Character: 5},
		},
		{
			name: "PointerMethod",
			source: `type Record struct{}
func (*Record) pickNumber(value int) int { return value }
func (*Record) pickText(value string) int { return len(value) }
func (Record).Pick = (
	(*Record).pickNumber
	(*Record).pickText
)
func (Record).Run = ((Record).Pick)
var record Record
_ = record.r|un("text")
`,
			implementation: "pickText",
			definition:     Position{Line: 2, Character: 15},
			declaration:    Position{Line: 7, Character: 14},
		},
		{
			name: "ImplicitMethod",
			source: `type Record struct{}
func (Record) pickNumber(value int) int { return value }
func (Record) pickDefault() int { return 2 }
func (Record).Pick = (
	(Record).pickNumber
	(Record).pickDefault
)
func (Record).Run = ((Record).Pick)
var record Record
var result = record.r|un
`,
			implementation: "pickDefault",
			definition:     Position{Line: 2, Character: 14},
			declaration:    Position{Line: 7, Character: 14},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const filename = "main.xgo"
			source, pos := typeDisplayTestSource(t, tt.source)
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)

			uri := s.toDocumentURI(filename)
			location := func(start Position, name string) Location {
				end := start
				end.Character += uint32(len(name))
				return Location{URI: uri, Range: Range{Start: start, End: end}}
			}
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: pos}
			definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			assert.Equal(t, location(tt.definition, tt.implementation), definition)
			declaration, err := s.textDocumentDeclaration(&DeclarationParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			assert.Equal(t, location(tt.declaration, "Run"), declaration)
		})
	}
}

func TestServerInlineOverloadLocations(t *testing.T) {
	const filename = "main.xgo"
	const declarations = `func Choose = (
	func(value int) int { return value }
	func(value string) int { return len(value) }
)
`
	for _, tt := range []struct {
		name, usage    string
		implementation uint32
		group          bool
	}{
		{name: "GroupCall", usage: "_ = Ch|oose(\"text\")\n", implementation: 2, group: true},
		{name: "EncodedValue", usage: "_ = Choose__|0\n", implementation: 1},
		{name: "EncodedAlias", usage: "_ = choose__|0(1)\n", implementation: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, declarations+tt.usage)
			s := newImportTestServer(t, map[string][]byte{filename: []byte(source)})
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)
			uri := s.toDocumentURI(filename)
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: pos}

			start := Position{Line: tt.implementation, Character: 1}
			implementation := Location{URI: uri, Range: Range{Start: start, End: start}}
			definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			assert.Equal(t, implementation, definition)

			wantDeclaration := implementation
			if tt.group {
				wantDeclaration.Range = Range{Start: Position{Character: 5}, End: Position{Character: 11}}
			}
			declaration, err := s.textDocumentDeclaration(&DeclarationParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			assert.Equal(t, wantDeclaration, declaration)

			if !tt.group {
				prepared, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: params})
				require.NoError(t, err)
				assert.Nil(t, prepared)
				edit, err := s.textDocumentRename(&RenameParams{TextDocument: params.TextDocument, Position: pos, NewName: "Fetch"})
				require.NoError(t, err)
				assert.Nil(t, edit)
			}
		})
	}
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
		{name: "MultiplePointers", filename: "main.xgo", typeExpr: "***Value"},
		{name: "Slice", filename: "main.xgo", typeExpr: "[]Value"},
		{name: "Array", filename: "main.xgo", typeExpr: "[2]Value"},
		{name: "Map", filename: "main.xgo", typeExpr: "map[Alias]Value"},
		{name: "Channel", filename: "main.xgo", typeExpr: "chan Value"},
		{name: "SendChannel", filename: "main.xgo", typeExpr: "chan<- Value"},
		{name: "ReceiveChannel", filename: "main.xgo", typeExpr: "<-chan Value"},
		{name: "NestedContainers", filename: "main.xgo", typeExpr: "*[][2]map[string]chan **Value"},
		{name: "AliasAcrossFiles", filename: "main.xgo", typeExpr: "Alias", wantLine: 1},
		{name: "PointerToAlias", filename: "main.xgo", typeExpr: "*Alias", wantLine: 1},
		{name: "NestedAlias", filename: "main.xgo", typeExpr: "chan []**Alias", wantLine: 1},
		{name: "NamedContainer", filename: "main.xgo", typeExpr: "[]Collection", wantLine: 2},
		{name: "AliasToContainer", filename: "main.xgo", typeExpr: "**CollectionAlias", wantLine: 3},
		{name: "RecursiveContainer", filename: "main.xgo", typeExpr: "[]Recursive", wantLine: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"types.xgo": []byte(`type Value struct{}
type Alias = Value
type Collection []*Value
type CollectionAlias = []*Value
type Recursive []Recursive
`),
				tt.filename: []byte("var value " + tt.typeExpr + "\n_ = value\n"),
			})
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)
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

	t.Run("UnnamedTypes", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			typeExpr string
		}{
			{name: "Builtin", typeExpr: "int"},
			{name: "PointerToBuiltin", typeExpr: "**int"},
			{name: "SliceOfBuiltin", typeExpr: "[]int"},
			{name: "NamedMapKey", typeExpr: "map[Value]int"},
			{name: "AnonymousStruct", typeExpr: "*struct { Item Value }"},
			{name: "Function", typeExpr: "func() Value"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"types.xgo": []byte("type Value struct{}\n"),
					"main.xgo":  []byte("var value " + tt.typeExpr + "\n_ = value\n"),
				})
				t.Cleanup(s.Close)
				requireNoDiagnostics(t, s)
				def, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 1, Character: 4},
				}})
				require.NoError(t, err)
				assert.Nil(t, def)
			})
		}
	})

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
				s := newFrameworkTestServer(t, map[string][]byte{
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
			"types.xgo": []byte(`type First struct{}
type Second struct{}
`),
			"main.xgo": []byte(`var value First
println value
`),
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
			Path: "main.xgo",
			Content: []byte(`var value Second
println value
missing()
`),
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
