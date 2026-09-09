package server

import (
	"encoding/json"
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
				s := newTestServer(t, map[string][]byte{
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
			name     string
			filename string
		}{
			{name: "ProjectClass", filename: "main_fixture.gox"},
			{name: "WorkClass", filename: "Worker_fixture.gox"},
			{name: "NormalClass", filename: "Record.gox"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{"main_fixture.gox": nil}
				files[tt.filename] = []byte("_ = this\n")
				s := newTestServer(t, files)
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
			name     string
			filename string
			source   string
			position Position
		}{
			{name: "BlankIdent", filename: "main.xgo", source: "const _ = 1\n", position: Position{Line: 0, Character: 6}},
			{name: "BuiltinType", filename: "main.xgo", source: "var value int\n", position: Position{Line: 0, Character: 10}},
			{name: "BuiltinFunc", filename: "main.xgo", source: "println 1\n"},
			{name: "ImportedPackage", filename: "main.xgo", source: "import \"fmt\"\nfmt.println 1\n", position: Position{Line: 1}},
			{name: "ImportedMember", filename: "main.xgo", source: "import \"fmt\"\nfmt.println 1\n", position: Position{Line: 1, Character: 4}},
			{name: "StringLiteral", filename: "main.xgo", source: "_ = \"value\"\n", position: Position{Line: 0, Character: 5}},
			{name: "GeneratedClass", filename: "main_fixture.gox", source: "Worker.apply Low\n"},
			{name: "ImportedFrameworkMember", filename: "main_fixture.gox", source: "Worker.apply Low\n", position: Position{Line: 0, Character: 7}},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{"main_fixture.gox": nil, "Worker_fixture.gox": nil}
				files[tt.filename] = []byte(tt.source)
				s := newTestServer(t, files)
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
				s := newTestServer(t, map[string][]byte{
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
				s := newTestServer(t, map[string][]byte{
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
