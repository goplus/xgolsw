package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentPrepareRename(t *testing.T) {
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
				tt.filename:  []byte("_ = value\n"),
			})
			rng, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)},
				Position:     Position{Line: 0, Character: 4},
			}})
			require.NoError(t, err)
			assert.Equal(t, &Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}, rng)
			_, err = s.workspaceRootFS.TypeInfo()
			assert.NoError(t, err)
		})
	}

	t.Run("FrameworkMembers", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			position Position
			want     Range
		}{
			{
				name:     "ProjectField",
				position: Position{Line: 1, Character: 4},
				want:     Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 9}},
			},
			{
				name:     "CallbackParameter",
				position: Position{Line: 1, Character: 12},
				want:     Range{Start: Position{Line: 1, Character: 12}, End: Position{Line: 1, Character: 18}},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newFrameworkTestServer(t, map[string][]byte{
					"main_fixture.gox":   []byte("var total int\n"),
					"Worker_fixture.gox": []byte("onValue amount => {\n    total = amount\n}\n"),
				})
				rng, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///Worker_fixture.gox"},
					Position:     tt.position,
				}})
				require.NoError(t, err)
				assert.Equal(t, &tt.want, rng)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
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
				files := map[string][]byte{tt.filename: []byte("_ = this\n")}
				if tt.projectFile != "" {
					files[tt.projectFile] = nil
				}
				s := tt.newServer(t, files)
				uri := s.toDocumentURI(tt.filename)
				position := Position{Line: 0, Character: 4}
				rng, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: uri},
					Position:     position,
				}})
				require.NoError(t, err)
				assert.Nil(t, rng)
				edit, err := s.textDocumentRename(&RenameParams{
					TextDocument: TextDocumentIdentifier{URI: uri},
					Position:     position,
					NewName:      "that",
				})
				require.NoError(t, err)
				assert.Nil(t, edit)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("NotRenameable", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			filename    string
			source      string
			position    Position
			needsWorker bool
			newServer   testServerFactory
		}{
			{name: "BlankIdent", filename: "main.xgo", source: "const _ = 1\n", position: Position{Line: 0, Character: 6}, newServer: newTestServer},
			{name: "BuiltinType", filename: "main.xgo", source: "var value int\n", position: Position{Line: 0, Character: 10}, newServer: newTestServer},
			{name: "BuiltinEmbeddedField", filename: "main.xgo", source: "type Holder struct { int }\nvar holder Holder\necho holder.int\n", position: Position{Line: 2, Character: 12}, newServer: newTestServer},
			{name: "ImportedEmbeddedField", filename: "main.xgo", source: "import f \"example.com/first\"\ntype Holder struct { f.App }\nvar holder Holder\necho holder.App\n", position: Position{Line: 3, Character: 12}, newServer: newClassfileTestServer},
			{name: "ImportedPromotedEmbeddedField", filename: "main.xgo", source: "import f \"example.com/first\"\nvar holder f.Wrapper[int]\necho holder.App\n", position: Position{Line: 2, Character: 12}, newServer: newClassfileTestServer},
			{name: "GeneratedEmbeddedField", filename: "main_fixture.gox", source: "type Holder struct { Worker }\nvar holder Holder\necho holder.Worker\n", position: Position{Line: 2, Character: 12}, needsWorker: true, newServer: newFrameworkTestServer},
			{name: "BuiltinFunc", filename: "main.xgo", source: "println 1\n", newServer: newTestServer},
			{name: "ImportedPackage", filename: "main.xgo", source: "import \"fmt\"\nfmt.println 1\n", position: Position{Line: 1}, newServer: newTestServer},
			{name: "ImportedMember", filename: "main.xgo", source: "import \"fmt\"\nfmt.println 1\n", position: Position{Line: 1, Character: 4}, newServer: newTestServer},
			{name: "StringLiteral", filename: "main.xgo", source: "_ = \"value\"\n", position: Position{Line: 0, Character: 5}, newServer: newTestServer},
			{name: "GeneratedClass", filename: "main_fixture.gox", source: "Worker.apply Low\n", needsWorker: true, newServer: newFrameworkTestServer},
			{name: "ImportedFrameworkMember", filename: "main_fixture.gox", source: "Worker.apply Low\n", position: Position{Line: 0, Character: 7}, needsWorker: true, newServer: newFrameworkTestServer},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{tt.filename: []byte(tt.source)}
				if tt.needsWorker {
					files["Worker_fixture.gox"] = nil
				}
				s := tt.newServer(t, files)
				uri := s.toDocumentURI(tt.filename)
				rng, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: uri},
					Position:     tt.position,
				}})
				require.NoError(t, err)
				assert.Nil(t, rng)
				edit, err := s.textDocumentRename(&RenameParams{
					TextDocument: TextDocumentIdentifier{URI: uri},
					Position:     tt.position,
					NewName:      "renamed",
				})
				require.NoError(t, err)
				assert.Nil(t, edit)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
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

		rng, err := s.textDocumentPrepareRename(&PrepareRenameParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 8, Character: 12},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, &Range{
			Start: Position{Line: 8, Character: 11},
			End:   Position{Line: 8, Character: 16},
		}, rng)
	})

	t.Run("MapKwargHasNoPrepareRename", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func configure(opts map[string]int?) {}

func main() {
	configure count = 1
}
`),
		})

		rng, err := s.textDocumentPrepareRename(&PrepareRenameParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 4, Character: 12},
			},
		})
		require.NoError(t, err)
		assert.Nil(t, rng)
		edit, err := s.textDocumentRename(&RenameParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Position:     Position{Line: 4, Character: 12},
			NewName:      "total",
		})
		require.NoError(t, err)
		assert.Nil(t, edit)
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
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

		rng, err := s.textDocumentPrepareRename(&PrepareRenameParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 14, Character: 25},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, &Range{
			Start: Position{Line: 14, Character: 23},
			End:   Position{Line: 14, Character: 32},
		}, rng)
	})
}

func TestServerTextDocumentRename(t *testing.T) {
	t.Run("RangeVariables", func(t *testing.T) {
		for _, kind := range []struct {
			name      string
			filename  string
			newServer testServerFactory
		}{
			{name: "XGo", filename: "main.xgo", newServer: newTestServer},
			{name: "NormalClass", filename: "Record.gox", newServer: newTestServer},
			{name: "ProjectClass", filename: "main_fixture.gox", newServer: newFrameworkTestServer},
			{name: "WorkClass", filename: "Worker_fixture.gox", newServer: newFrameworkTestServer},
		} {
			t.Run(kind.name, func(t *testing.T) {
				for _, tt := range []struct {
					name        string
					source      string
					declaration int
				}{
					{name: "RangeValue", source: "for _, |value := range [1, 2] {\necho |value\n}"},
					{name: "RangeKey", source: "for |value := range [1, 2] {\necho |value\n}"},
					{name: "ForPhraseValue", source: "for |value <- [1, 2] {\necho |value\n}"},
					{name: "ForPhraseKey", source: "for |value, item <- [1, 2] {\necho |value, item\n}"},
					{name: "RangeExpression", source: "for |value <- 1:3 {\necho |value\n}"},
					{name: "ListComprehension", source: "echo [|value * 2 for |value <- [1, 2]]", declaration: 1},
					{name: "MapComprehension", source: "echo {|value: |value * 2 for |value <- [1, 2]}", declaration: 2},
					{name: "FilteredComprehension", source: "echo [|value for |value <- [1, 2] if |value > 1]", declaration: 1},
					{name: "NestedScope", source: "for |value <- [1, 2] {\nfor value <- [3, 4] { echo value }\necho |value\n}"},
				} {
					t.Run(tt.name, func(t *testing.T) {
						marked := "func run() {\nvalue := \"outer\"\necho value\n{\n" + tt.source + "\n}\necho value\n}\n"
						parts := strings.Split(marked, "|")
						source := strings.Join(parts, "")
						var want []TextEdit
						prefix := ""
						for _, part := range parts[:len(parts)-1] {
							prefix += part
							start := Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(UTF16Len(prefix[strings.LastIndex(prefix, "\n")+1:]))}
							end := start
							end.Character += 5
							want = append(want, TextEdit{Range: Range{Start: start, End: end}, NewText: "entry"})
						}
						files := map[string][]byte{kind.filename: []byte(source)}
						if kind.name == "WorkClass" {
							files["main_fixture.gox"] = nil
						}
						s := kind.newServer(t, files)
						_, err := s.requestProject().TypeInfo()
						require.NoError(t, err)
						uri := s.toDocumentURI(kind.filename)
						var edit *WorkspaceEdit
						for _, target := range want {
							params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: target.Range.Start}
							for _, includeDeclaration := range []bool{false, true} {
								var wantLocations []Location
								for i, edit := range want {
									if includeDeclaration || i != tt.declaration {
										wantLocations = append(wantLocations, Location{URI: uri, Range: edit.Range})
									}
								}
								locations, err := s.textDocumentReferences(&ReferenceParams{
									TextDocumentPositionParams: params,
									Context:                    ReferenceContext{IncludeDeclaration: includeDeclaration},
								})
								require.NoError(t, err)
								assert.ElementsMatch(t, wantLocations, locations)
							}
							prepared, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: params})
							require.NoError(t, err)
							assert.Equal(t, &target.Range, prepared)
							edit, err = s.textDocumentRename(&RenameParams{TextDocument: params.TextDocument, Position: params.Position, NewName: "entry"})
							require.NoError(t, err)
							assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{uri: want})
						}
						updated := applyResourceRenameTestEdits(t, source, edit.Changes[uri])
						s.ModifyFiles([]FileChange{{Path: kind.filename, Content: []byte(updated), Version: 1}})
						_, err = s.requestProject().TypeInfo()
						assert.NoError(t, err)
					})
				}
			})
		}
	})

	t.Run("EmbeddedTypes", func(t *testing.T) {
		for _, kind := range []struct {
			name     string
			filename string
		}{
			{name: "XGo", filename: "main.xgo"},
			{name: "NormalClass", filename: "Record.gox"},
		} {
			t.Run(kind.name, func(t *testing.T) {
				filename := kind.filename
				for _, target := range []struct {
					name     string
					inTypes  bool
					position Position
					kwarg    bool
				}{
					{name: "TypeDeclaration", inTypes: true, position: Position{Line: 0, Character: 5}},
					{name: "EmbeddedDeclaration", inTypes: true, position: Position{Line: 1, Character: 21}},
					{name: "PointerDeclaration", inTypes: true, position: Position{Line: 2, Character: 29}},
					{name: "Field", position: Position{Line: 5, Character: 11}},
					{name: "PointerField", position: Position{Line: 5, Character: 24}},
					{name: "PromotedField", position: Position{Line: 5, Character: 39}},
					{name: "Kwarg", position: Position{Line: 6, Character: 10}, kwarg: true},
				} {
					t.Run(target.name, func(t *testing.T) {
						files := map[string][]byte{
							"types.xgo": []byte("type Item struct{}\ntype Holder struct { Item }\ntype PointerHolder struct { *Item }\n" +
								"type Wrapper struct { Holder }\ntype Other struct { Item int }\nfunc configure(opts Holder?) {}\n"),
							filename: []byte("func use() {\nvar first Holder\nvar second PointerHolder\nvar promoted Wrapper\nvar other Other\n" +
								"echo first.Item, second.Item, promoted.Item, other.Item\nconfigure item = Item{}\n}\n"),
						}
						s := newTestServer(t, files)
						_, err := s.requestProject().TypeInfo()
						require.NoError(t, err)
						uri := s.toDocumentURI(filename)
						if target.inTypes {
							uri = "file:///types.xgo"
						}
						rng, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
							TextDocument: TextDocumentIdentifier{URI: uri}, Position: target.position,
						}})
						require.NoError(t, err)
						end := target.position
						end.Character += 4
						assert.Equal(t, &Range{Start: target.position, End: end}, rng)
						newName := "Node"
						if target.kwarg {
							newName = "node"
						}
						edit, err := s.textDocumentRename(&RenameParams{
							TextDocument: TextDocumentIdentifier{URI: uri}, Position: target.position, NewName: newName,
						})
						require.NoError(t, err)
						assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
							"file:///types.xgo": {
								{Range: Range{Start: Position{Line: 0, Character: 5}, End: Position{Line: 0, Character: 9}}, NewText: "Node"},
								{Range: Range{Start: Position{Line: 1, Character: 21}, End: Position{Line: 1, Character: 25}}, NewText: "Node"},
								{Range: Range{Start: Position{Line: 2, Character: 29}, End: Position{Line: 2, Character: 33}}, NewText: "Node"},
							},
							s.toDocumentURI(filename): {
								{Range: Range{Start: Position{Line: 5, Character: 11}, End: Position{Line: 5, Character: 15}}, NewText: "Node"},
								{Range: Range{Start: Position{Line: 5, Character: 24}, End: Position{Line: 5, Character: 28}}, NewText: "Node"},
								{Range: Range{Start: Position{Line: 5, Character: 39}, End: Position{Line: 5, Character: 43}}, NewText: "Node"},
								{Range: Range{Start: Position{Line: 6, Character: 10}, End: Position{Line: 6, Character: 14}}, NewText: "node"},
								{Range: Range{Start: Position{Line: 6, Character: 17}, End: Position{Line: 6, Character: 21}}, NewText: "Node"},
							},
						})
						var changes []FileChange
						for filename, source := range files {
							updated := applyResourceRenameTestEdits(t, string(source), edit.Changes[s.toDocumentURI(filename)])
							changes = append(changes, FileChange{Path: filename, Content: []byte(updated), Version: 1})
						}
						s.ModifyFiles(changes)
						_, err = s.requestProject().TypeInfo()
						assert.NoError(t, err)
					})
				}
			})
		}
		t.Run("LocalTypes", func(t *testing.T) {
			for _, position := range []Position{{Line: 1, Character: 5}, {Line: 2, Character: 21}, {Line: 4, Character: 12}} {
				source := "func use() {\ntype Item struct{}\ntype Holder struct { Item }\nvar holder Holder\necho holder.Item\n}\n"
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
				edit, err := s.textDocumentRename(&RenameParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position, NewName: "Node",
				})
				require.NoError(t, err)
				assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
					"file:///main.xgo": {
						{Range: Range{Start: Position{Line: 1, Character: 5}, End: Position{Line: 1, Character: 9}}, NewText: "Node"},
						{Range: Range{Start: Position{Line: 2, Character: 21}, End: Position{Line: 2, Character: 25}}, NewText: "Node"},
						{Range: Range{Start: Position{Line: 4, Character: 12}, End: Position{Line: 4, Character: 16}}, NewText: "Node"},
					},
				})
				updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
				s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
				_, err = s.requestProject().TypeInfo()
				assert.NoError(t, err)
			}
		})
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
			for _, target := range []struct {
				name     string
				filename string
				position Position
			}{
				{name: "Definition", filename: "values.xgo", position: Position{Line: 0, Character: 4}},
				{name: "Reference", filename: tt.filename, position: Position{Line: 0, Character: 4}},
			} {
				t.Run(target.name, func(t *testing.T) {
					s := newTestServer(t, map[string][]byte{
						"values.xgo": []byte("var value int\n"),
						tt.filename:  []byte("_ = value\n"),
					})
					edit, err := s.textDocumentRename(&RenameParams{
						TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(target.filename)},
						Position:     target.position,
						NewName:      "renamed",
					})
					require.NoError(t, err)
					assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
						"file:///values.xgo":         {{Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}, NewText: "renamed"}},
						s.toDocumentURI(tt.filename): {{Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}, NewText: "renamed"}},
					})
					_, err = s.workspaceRootFS.TypeInfo()
					assert.NoError(t, err)
				})
			}
		})
	}

	t.Run("SameNameDifferentScopes", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			position Position
			want     map[DocumentURI][]TextEdit
		}{
			{
				name: "PackageVariable", position: Position{Line: 0, Character: 4},
				want: map[DocumentURI][]TextEdit{
					"file:///values.xgo": {{Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}, NewText: "renamed"}},
					"file:///main.xgo":   {{Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}, NewText: "renamed"}},
				},
			},
			{
				name: "LocalVariable", position: Position{Line: 3, Character: 8},
				want: map[DocumentURI][]TextEdit{
					"file:///values.xgo": {
						{Range: Range{Start: Position{Line: 2, Character: 8}, End: Position{Line: 2, Character: 13}}, NewText: "renamed"},
						{Range: Range{Start: Position{Line: 3, Character: 8}, End: Position{Line: 3, Character: 13}}, NewText: "renamed"},
					},
				},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"values.xgo": []byte("var value int\nfunc useLocal() {\n    var value int\n    _ = value\n}\n"),
					"main.xgo":   []byte("_ = value\n"),
				})
				edit, err := s.textDocumentRename(&RenameParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///values.xgo"},
					Position:     tt.position,
					NewName:      "renamed",
				})
				require.NoError(t, err)
				assertRenameChanges(t, edit, tt.want)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("ExplicitThis", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("var this int\n_ = this\n")})
		position := Position{Line: 1, Character: 4}
		rng, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Position:     position,
		}})
		require.NoError(t, err)
		assert.Equal(t, &Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 8}}, rng)
		edit, err := s.textDocumentRename(&RenameParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Position:     position,
			NewName:      "value",
		})
		require.NoError(t, err)
		assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
			"file:///main.xgo": {
				{Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 8}}, NewText: "value"},
				{Range: Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 8}}, NewText: "value"},
			},
		})
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})

	t.Run("FrameworkConstant", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			uri      DocumentURI
			position Position
			want     Range
		}{
			{name: "Definition", uri: "file:///main_fixture.gox", position: Position{Line: 0, Character: 6}, want: Range{Start: Position{Line: 0, Character: 6}, End: Position{Line: 0, Character: 11}}},
			{name: "Reference", uri: "file:///Worker_fixture.gox", position: Position{Line: 0, Character: 4}, want: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newFrameworkTestServer(t, map[string][]byte{
					"main_fixture.gox":   []byte("const title = \"Example\"\n"),
					"Worker_fixture.gox": []byte("_ = title\n"),
				})
				rng, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: tt.uri}, Position: tt.position,
				}})
				require.NoError(t, err)
				assert.Equal(t, &tt.want, rng)
				edit, err := s.textDocumentRename(&RenameParams{
					TextDocument: TextDocumentIdentifier{URI: tt.uri}, Position: tt.position, NewName: "name",
				})
				require.NoError(t, err)
				assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
					"file:///main_fixture.gox":   {{Range: Range{Start: Position{Line: 0, Character: 6}, End: Position{Line: 0, Character: 11}}, NewText: "name"}},
					"file:///Worker_fixture.gox": {{Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}, NewText: "name"}},
				})
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("FrameworkMembers", func(t *testing.T) {
		for _, tt := range []struct {
			name             string
			position         Position
			want             map[DocumentURI][]TextEdit
			wantNotification *PropertyRenamedParams
		}{
			{
				name: "ProjectField", position: Position{Line: 2, Character: 4},
				want: map[DocumentURI][]TextEdit{
					"file:///main_fixture.gox":   {{Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}, NewText: "count"}},
					"file:///Worker_fixture.gox": {{Range: Range{Start: Position{Line: 2, Character: 4}, End: Position{Line: 2, Character: 9}}, NewText: "count"}},
				},
				wantNotification: &PropertyRenamedParams{
					Target: "App", OldName: "total", NewName: "count",
					TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				},
			},
			{
				name: "WorkField", position: Position{Line: 3, Character: 4},
				want: map[DocumentURI][]TextEdit{
					"file:///Worker_fixture.gox": {
						{Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}, NewText: "count"},
						{Range: Range{Start: Position{Line: 3, Character: 4}, End: Position{Line: 3, Character: 9}}, NewText: "count"},
					},
				},
				wantNotification: &PropertyRenamedParams{
					Target: "Worker", OldName: "value", NewName: "count",
					TextDocument: TextDocumentIdentifier{URI: "file:///Worker_fixture.gox"},
				},
			},
			{
				name: "CallbackParameter", position: Position{Line: 2, Character: 12},
				want: map[DocumentURI][]TextEdit{
					"file:///Worker_fixture.gox": {
						{Range: Range{Start: Position{Line: 1, Character: 8}, End: Position{Line: 1, Character: 14}}, NewText: "count"},
						{Range: Range{Start: Position{Line: 2, Character: 12}, End: Position{Line: 2, Character: 18}}, NewText: "count"},
						{Range: Range{Start: Position{Line: 3, Character: 12}, End: Position{Line: 3, Character: 18}}, NewText: "count"},
					},
				},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newFrameworkTestServer(t, map[string][]byte{
					"main_fixture.gox":   []byte("var total int\n"),
					"Worker_fixture.gox": []byte("var value int\nonValue amount => {\n    total = amount\n    value = amount\n}\n"),
				})
				replier := newMockReplier()
				s.replier = replier
				edit, err := s.textDocumentRename(&RenameParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///Worker_fixture.gox"},
					Position:     tt.position,
					NewName:      "count",
				})
				require.NoError(t, err)
				assertRenameChanges(t, edit, tt.want)
				msgs := replier.getMessages()
				if tt.wantNotification == nil {
					assert.Empty(t, msgs)
				} else {
					require.Len(t, msgs, 1)
					notif := requireValueAs[*jsonrpc2.Notification](t, msgs[0])
					assert.Equal(t, "textDocument/xgo.propertyRenamed", notif.Method())
					var params PropertyRenamedParams
					require.NoError(t, json.Unmarshal(notif.Params(), &params))
					assert.Equal(t, *tt.wantNotification, params)
				}
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("FieldOwners", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			filename  string
			source    string
			extra     map[string][]byte
			owner     string
			newServer testServerFactory
		}{
			{name: "DefinedSibling", filename: "main.xgo", owner: "Record", newServer: newTestServer,
				source: "type Record struct { |Count int }\ntype Adapter Record\nvar item Record\nitem.|Count = 1\n"},
			{name: "DefinedReceiver", filename: "main.xgo", owner: "Record", newServer: newTestServer,
				source: "type Record struct { |Count int }\ntype Adapter Record\nvar item Adapter\nitem.|Count = 1\n"},
			{name: "PromotedField", filename: "main.xgo", owner: "Record", newServer: newTestServer,
				source: "type Record struct { |Count int }\ntype Adapter struct { Record }\nvar item Adapter\nitem.|Count = 1\n"},
			{name: "LocalType", filename: "main.xgo", owner: "Record", newServer: newTestServer,
				source: "func run() {\ntype Record struct { |Count int }\ntype Adapter Record\nvar item Adapter\nitem.|Count = 1\n}\n"},
			{name: "ProjectField", filename: "main_fixture.gox", owner: "App", newServer: newFrameworkTestServer,
				source: "var |Count int\n|Count = 1\n", extra: map[string][]byte{"helpers.xgo": []byte("type Adapter App\n")}},
			{name: "WorkField", filename: "Worker_fixture.gox", owner: "Worker", newServer: newFrameworkTestServer,
				source: "var |Count int\n|Count = 1\n", extra: map[string][]byte{"main_fixture.gox": nil, "helpers.xgo": []byte("type Adapter Worker\n")}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				parts := strings.Split(tt.source, "|")
				require.Len(t, parts, 3)
				files := map[string][]byte{tt.filename: []byte(strings.Join(parts, ""))}
				for filename, content := range tt.extra {
					files[filename] = content
				}
				s := tt.newServer(t, files)
				_, err := s.getProj().TypeInfo()
				require.NoError(t, err)
				replier := newMockReplier()
				s.replier = replier
				var changes []TextEdit
				prefix := ""
				for _, part := range parts[:2] {
					prefix += part
					position := Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(UTF16Len(prefix[strings.LastIndex(prefix, "\n")+1:]))}
					changes = append(changes, TextEdit{Range: Range{Start: position, End: Position{Line: position.Line, Character: position.Character + 5}}, NewText: "Total"})
				}
				uri := s.toDocumentURI(tt.filename)
				edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: changes[1].Range.Start, NewName: "Total"})
				require.NoError(t, err)
				assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{uri: changes})
				messages := replier.getMessages()
				require.Len(t, messages, 1)
				notification := requireValueAs[*jsonrpc2.Notification](t, messages[0])
				assert.Equal(t, "textDocument/xgo.propertyRenamed", notification.Method())
				var params PropertyRenamedParams
				require.NoError(t, json.Unmarshal(notification.Params(), &params))
				assert.Equal(t, PropertyRenamedParams{Target: tt.owner, OldName: "Count", NewName: "Total", TextDocument: TextDocumentIdentifier{URI: uri}}, params)
			})
		}
	})

	t.Run("FileUpdatesWithTypeErrors", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"values.xgo": []byte("var value int\n"),
			"main.xgo":   []byte("_ = value\n"),
		})
		params := &RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: Position{Line: 0, Character: 4}, NewName: "count"}
		edit, err := s.textDocumentRename(params)
		require.NoError(t, err)
		assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
			"file:///values.xgo": {{Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}, NewText: "count"}},
			"file:///main.xgo":   {{Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}, NewText: "count"}},
		})
		s.ModifyFiles([]FileChange{
			{Path: "values.xgo", Content: []byte("\nvar value int\nfunc broken() { missing() }\n"), Version: 1},
			{Path: "main.xgo", Content: []byte("_ = value\n_ = value\n"), Version: 1},
		})
		rng, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			Position:     Position{Line: 1, Character: 4},
		}})
		require.NoError(t, err)
		assert.Equal(t, &Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 9}}, rng)
		edit, err = s.textDocumentRename(params)
		require.NoError(t, err)
		assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
			"file:///values.xgo": {{Range: Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 9}}, NewText: "count"}},
			"file:///main.xgo": {
				{Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}, NewText: "count"},
				{Range: Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 9}}, NewText: "count"},
			},
		})
		_, err = s.workspaceRootFS.TypeInfo()
		assert.ErrorContains(t, err, "undefined: missing")
	})

	for _, tt := range []struct {
		name     string
		uri      DocumentURI
		position Position
		wantErr  bool
	}{
		{name: "MissingFile", uri: "file:///missing.xgo"},
		{name: "NonSourceFile", uri: "file:///notes.txt"},
		{name: "InvalidURI", uri: "bucket:///main.xgo", wantErr: true},
		{name: "InvalidPosition", uri: "file:///main.xgo", position: Position{Line: 99, Character: 99}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{
				"main.xgo":  []byte("var value int\n"),
				"notes.txt": []byte("var value int\n"),
			})
			rng, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: tt.uri},
				Position:     tt.position,
			}})
			if tt.wantErr {
				assert.ErrorContains(t, err, "failed to get file path from document URI")
			} else {
				assert.NoError(t, err)
			}
			assert.Nil(t, rng)
			edit, err := s.textDocumentRename(&RenameParams{
				TextDocument: TextDocumentIdentifier{URI: tt.uri},
				Position:     tt.position,
				NewName:      "count",
			})
			if tt.wantErr {
				assert.ErrorContains(t, err, "failed to get file path from document URI")
			} else {
				assert.NoError(t, err)
			}
			assert.Nil(t, edit)
		})
	}

	t.Run("KwargField", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			uri      DocumentURI
			position Position
			newName  string
		}{
			{name: "Definition", uri: "file:///main.xgo", position: Position{Line: 1, Character: 4}, newName: "Total"},
			{name: "Reference", uri: "file:///other.xgo", position: Position{Line: 1, Character: 15}, newName: "total"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte(`type Options struct {
    Count int
}
func configure(opts Options?) {}
func main() {
    configure count = 1
    configure count = 2
}
`),
					"other.xgo": []byte("func another() {\n    configure count = 3\n    var opts Options\n    _ = opts.Count\n}\n"),
				})
				s.replier = newMockReplier()
				params := &RenameParams{TextDocument: TextDocumentIdentifier{URI: tt.uri}, Position: tt.position, NewName: tt.newName}
				edit, err := s.textDocumentRename(params)
				require.NoError(t, err)
				assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
					"file:///main.xgo": {
						{Range: Range{Start: Position{Line: 1, Character: 4}, End: Position{Line: 1, Character: 9}}, NewText: "Total"},
						{Range: Range{Start: Position{Line: 5, Character: 14}, End: Position{Line: 5, Character: 19}}, NewText: "total"},
						{Range: Range{Start: Position{Line: 6, Character: 14}, End: Position{Line: 6, Character: 19}}, NewText: "total"},
					},
					"file:///other.xgo": {
						{Range: Range{Start: Position{Line: 1, Character: 14}, End: Position{Line: 1, Character: 19}}, NewText: "total"},
						{Range: Range{Start: Position{Line: 3, Character: 13}, End: Position{Line: 3, Character: 18}}, NewText: "Total"},
					},
				})
				assert.Equal(t, tt.newName, params.NewName)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("KwargInterfaceMethod", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			uri      DocumentURI
			position Position
			newName  string
		}{
			{name: "Definition", uri: "file:///main.xgo", position: Position{Line: 2, Character: 4}, newName: "Limit"},
			{name: "Reference", uri: "file:///other.xgo", position: Position{Line: 1, Character: 27}, newName: "limit"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"main.xgo": []byte(`type Client struct{}
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
					"other.xgo": []byte("func another() {\n    client.complete \"hi\", maxTokens = 3\n}\n"),
				})
				params := &RenameParams{TextDocument: TextDocumentIdentifier{URI: tt.uri}, Position: tt.position, NewName: tt.newName}
				edit, err := s.textDocumentRename(params)
				require.NoError(t, err)
				assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
					"file:///main.xgo": {
						{Range: Range{Start: Position{Line: 2, Character: 4}, End: Position{Line: 2, Character: 13}}, NewText: "Limit"},
						{Range: Range{Start: Position{Line: 8, Character: 26}, End: Position{Line: 8, Character: 35}}, NewText: "limit"},
						{Range: Range{Start: Position{Line: 9, Character: 27}, End: Position{Line: 9, Character: 36}}, NewText: "limit"},
					},
					"file:///other.xgo": {{Range: Range{Start: Position{Line: 1, Character: 26}, End: Position{Line: 1, Character: 35}}, NewText: "limit"}},
				})
				assert.Equal(t, tt.newName, params.NewName)
				_, err = s.workspaceRootFS.TypeInfo()
				assert.NoError(t, err)
			})
		}
	})

	t.Run("UTF16", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("var value string\n_ = \"\U0001F600\" + value\n")})
		position := Position{Line: 1, Character: 12}
		rng, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
		}})
		require.NoError(t, err)
		assert.Equal(t, &Range{Start: Position{Line: 1, Character: 11}, End: Position{Line: 1, Character: 16}}, rng)
		edit, err := s.textDocumentRename(&RenameParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position, NewName: "text",
		})
		require.NoError(t, err)
		assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
			"file:///main.xgo": {
				{Range: Range{Start: Position{Line: 0, Character: 4}, End: Position{Line: 0, Character: 9}}, NewText: "text"},
				{Range: Range{Start: Position{Line: 1, Character: 11}, End: Position{Line: 1, Character: 16}}, NewText: "text"},
			},
		})
		_, err = s.workspaceRootFS.TypeInfo()
		assert.NoError(t, err)
	})
}

func assertRenameChanges(t *testing.T, edit *WorkspaceEdit, want map[DocumentURI][]TextEdit) {
	t.Helper()

	require.NotNil(t, edit)
	assert.Nil(t, edit.DocumentChanges)
	assert.Nil(t, edit.ChangeAnnotations)
	require.Len(t, edit.Changes, len(want))
	for uri, changes := range want {
		assert.ElementsMatch(t, changes, edit.Changes[uri], "edits for %s", uri)
	}
}

func TestServerTextDocumentRenameIdentifier(t *testing.T) {
	for _, tt := range []struct {
		name    string
		newName string
		valid   bool
	}{
		{name: "Empty"},
		{name: "Blank", newName: "_"},
		{name: "Keyword", newName: "for"},
		{name: "LeadingDigit", newName: "1value"},
		{name: "Whitespace", newName: "new value"},
		{name: "Selector", newName: "value.Count"},
		{name: "Emoji", newName: "\U0001F600"},
		{name: "Identifier", newName: "renamed", valid: true},
		{name: "LeadingUnderscore", newName: "_value", valid: true},
		{name: "UnicodeLetter", newName: "\u503c", valid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, source := range []string{
				"var |value int\nprintln value\n",
				"type Record struct{}\nfunc (Record) |Value() int { return 1 }\nprintln Record{}.Value()\n",
			} {
				source, position := typeDisplayTestSource(t, source)
				s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				replier := newMockReplier()
				s.replier = replier
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
				edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position, NewName: tt.newName})
				if !tt.valid {
					assert.ErrorIs(t, err, jsonrpc2.ErrInvalidParams)
					assert.Nil(t, edit)
					assert.Empty(t, replier.messages)
					continue
				}
				require.NoError(t, err)
				require.NotNil(t, edit)
				updated := applyResourceRenameTestEdits(t, source, edit.Changes["file:///main.xgo"])
				s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
				_, err = s.requestProject().TypeInfo()
				assert.NoError(t, err)
			}
		})
	}
}

func TestServerTextDocumentRenameKeywordKwarg(t *testing.T) {
	for _, tt := range []struct {
		name    string
		source  string
		newName string
	}{
		{name: "KeywordAtCall", source: "type Options struct { Count int }\nfunc configure(opts Options?) {}\nconfigure |count=1\n", newName: "for"},
		{name: "KeywordAtDeclaration", source: "type Options struct { |Count int }\nfunc configure(opts Options?) {}\nconfigure count=1\n", newName: "For"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.source)
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			replier := newMockReplier()
			s.replier = replier
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			edit, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position, NewName: tt.newName})
			assert.ErrorIs(t, err, jsonrpc2.ErrInvalidParams)
			assert.Nil(t, edit)
			assert.Empty(t, replier.messages)
		})
	}
}
