package server

import (
	"encoding/json"
	gotypes "go/types"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func codeActionClientCapabilitiesForTest() ClientCapabilities {
	return ClientCapabilities{
		TextDocument: protocol.TextDocumentClientCapabilities{
			CodeAction: protocol.CodeActionClientCapabilities{
				CodeActionLiteralSupport: protocol.ClientCodeActionLiteralOptions{
					CodeActionKind: protocol.ClientCodeActionKindOptions{
						ValueSet: []protocol.CodeActionKind{protocol.QuickFix},
					},
				},
			},
		},
	}
}

func applyCodeActionTestEdits(t *testing.T, source string, action CodeAction, uri DocumentURI) string {
	t.Helper()

	require.NotNil(t, action.Edit)
	var edits []TextEdit
	if len(action.Edit.DocumentChanges) > 0 {
		assert.Empty(t, action.Edit.Changes)
		require.Len(t, action.Edit.DocumentChanges, 1)
		change := action.Edit.DocumentChanges[0].TextDocumentEdit
		require.NotNil(t, change)
		assert.Equal(t, uri, change.TextDocument.URI)
		for _, edit := range change.Edits {
			edits = append(edits, requireValueAs[TextEdit](t, edit.Value))
		}
	} else {
		require.Len(t, action.Edit.Changes, 1)
		var ok bool
		edits, ok = action.Edit.Changes[uri]
		require.True(t, ok)
	}
	require.NotEmpty(t, edits)
	edits = slices.Clone(edits)
	slices.SortFunc(edits, func(a, b TextEdit) int {
		return comparePositions(a.Range.Start, b.Range.Start)
	})
	var result strings.Builder
	previousEnd := 0
	for _, edit := range edits {
		start := PositionOffset([]byte(source), edit.Range.Start)
		end := PositionOffset([]byte(source), edit.Range.End)
		require.GreaterOrEqual(t, start, previousEnd, "edits must not overlap")
		require.GreaterOrEqual(t, end, start)
		result.WriteString(source[previousEnd:start])
		result.WriteString(edit.NewText)
		previousEnd = end
	}
	result.WriteString(source[previousEnd:])
	return result.String()
}

func TestServerTextDocumentCodeAction(t *testing.T) {
	t.Run("MergeDeclarations", func(t *testing.T) {
		for _, tt := range []struct {
			name          string
			filename      string
			source        string
			want          string
			wantDiagCount int
		}{
			{
				name: "SingleDeclarations",
				source: `var score int
var speed int = 10
var title string = "Game"
`,
				want: `var (
	score int

	speed int = 10

	title string = "Game"
)
`,
				wantDiagCount: 2,
			},
			{
				name: "GroupedDeclarations",
				source: `// Fields.
var (
	score int
)
var (
	speed int = 10
)
`,
				want: `// Fields.
var (
	score int

	speed int = 10
)
`,
				wantDiagCount: 1,
			},
			{
				name: "StaticAndInstanceFields",
				source: `var count int
var .total int = 1
`,
				want: `var (
	count int

	.total int = 1
)
`,
				wantDiagCount: 1,
			},
			{
				name: "EmptyBlocks",
				source: `var ()
var ()
`,
				want:          "var ()\n",
				wantDiagCount: 1,
			},
			{
				name:          "NormalClassfile",
				filename:      "Counter.gox",
				source:        "var count int\nvar step int = 1\n",
				want:          "var (\n\tcount int\n\n\tstep int = 1\n)\n",
				wantDiagCount: 1,
			},
			{
				name:          "WorkClassfile",
				filename:      "Counter_fixture.gox",
				source:        "var count int\nvar step int = 1\n",
				want:          "var (\n\tcount int\n\n\tstep int = 1\n)\n",
				wantDiagCount: 1,
			},
			{
				name: "AfterFunction",
				source: `// The method.
func run() {}
var score int
var speed int = 10
`,
				want: `var (
	score int

	speed int = 10
)

// The method.
func run() {}
`,
				wantDiagCount: 2,
			},
			{
				name: "InterveningDeclarations",
				source: `var score int
type Marker int
const limit = 10
func (Marker) Run() { println   "unchanged" }
var speed int = limit
println speed
var local int = 20
println local
`,
				want: `var (
	score int

	speed int = limit
)
type Marker int
const limit = 10
func (Marker) Run() { println   "unchanged" }
println speed
var local int = 20
println local
`,
				wantDiagCount: 1,
			},
			{
				name:          "ExplicitSemicolons",
				source:        "var score int; var speed int = 10\n",
				want:          "var (\n\tscore int\n\n\tspeed int = 10\n)\n",
				wantDiagCount: 1,
			},
			{
				name:          "NoFinalNewline",
				source:        "var score int\nvar speed int = 10",
				want:          "var (\n\tscore int\n\n\tspeed int = 10\n)\n",
				wantDiagCount: 1,
			},
			{
				name:          "CRLF",
				source:        "var score int\r\nvar speed int = 10\r\n",
				want:          "var (\r\n\tscore int\r\n\r\n\tspeed int = 10\r\n)\r\n",
				wantDiagCount: 1,
			},
			{
				name:          "CRLFAfterFunction",
				source:        "func run() {}\r\nvar count int\r\nvar step int\r\n",
				want:          "var (\r\n\tcount int\r\n\r\n\tstep int\r\n)\r\n\r\nfunc run() {}\r\n",
				wantDiagCount: 2,
			},
			{
				name:          "MultipleNamesAndInitializationOrder",
				source:        "var first, second int = 1, 2\nvar third int = first + second\n",
				want:          "var (\n\tfirst, second int = 1, 2\n\n\tthird int = first + second\n)\n",
				wantDiagCount: 1,
			},
			{
				name: "OverloadBeforeFields",
				source: `// Overload documentation.
func run = (func() {}; func(n int) {})
var count int
var step int
`,
				want: `var (
	count int

	step int
)

// Overload documentation.
func run = (func() {}; func(n int) {})
`,
				wantDiagCount: 2,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				filename := tt.filename
				if filename == "" {
					filename = "main_fixture.gox"
				}
				uri := DocumentURI("file:///" + filename)
				files := map[string][]byte{filename: []byte(tt.source)}
				if filename == "Counter_fixture.gox" {
					files["main_fixture.gox"] = nil
				}
				s := newFrameworkTestServer(t, files)
				t.Cleanup(s.Close)
				params := &CodeActionParams{
					TextDocument: TextDocumentIdentifier{URI: uri},
					Range:        Range{End: Position{Line: 100}},
				}
				actions, err := s.textDocumentCodeAction(params)
				require.NoError(t, err)
				require.Len(t, actions, 1)
				action := actions[0]
				assert.Equal(t, "Merge top-level var declarations", action.Title)
				assert.Equal(t, codeActionKindMergeVarDeclarations, action.Kind)
				assert.Nil(t, action.Command)
				assert.False(t, action.IsPreferred)
				assert.Len(t, action.Diagnostics, tt.wantDiagCount)
				diagnostics, err := s.diagnosticsAt(s.requestProject())
				require.NoError(t, err)
				for _, diagnostic := range action.Diagnostics {
					assert.Contains(t, diagnostics.diagnostics[uri], diagnostic)
				}
				got := applyCodeActionTestEdits(t, tt.source, action, uri)
				assert.Equal(t, tt.want, got)
				original, ok := s.getProj().File(filename)
				require.True(t, ok)
				assert.Equal(t, tt.source, string(original.Content), "request must not apply its edits")

				files[filename] = []byte(got)
				fixed := newFrameworkTestServer(t, files)
				t.Cleanup(fixed.Close)
				requireNoDiagnostics(t, fixed)
				actions, err = fixed.textDocumentCodeAction(params)
				require.NoError(t, err)
				assert.Empty(t, actions)
			})
		}
	})

	t.Run("FieldTags", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			source     string
			wantTags   []string
			wantValues []string
		}{
			{
				name:     "QuotedTagOnFirstDeclaration",
				source:   "var score int \"score\"\nvar speed int\n",
				wantTags: []string{"\"score\"", ""},
			},
			{
				name:     "QuotedTagOnSecondDeclaration",
				source:   "var score int\nvar speed int \"speed\"\n",
				wantTags: []string{"", "\"speed\""},
			},
			{
				name:     "QuotedTagWithTrailingComment",
				source:   "var score int \"score\" // Score.\nvar speed int\n",
				wantTags: []string{"\"score\"", ""},
			},
			{
				name:     "QuotedTagWithCRLF",
				source:   "var score int\r\nvar speed int \"speed\"\r\n",
				wantTags: []string{"", "\"speed\""},
			},
			{
				name:     "RawTag",
				source:   "var score int `json:\"score\"`\nvar speed int\n",
				wantTags: []string{"`json:\"score\"`", ""},
			},
			{
				name:     "EmbeddedFieldTag",
				source:   "type Base struct{}\nvar Base \"base\"\nvar speed int\n",
				wantTags: []string{"\"base\"", ""},
			},
			{
				name:     "EmptyFirstBlock",
				source:   "var ()\nvar score int \"score\"\n",
				wantTags: []string{"\"score\""},
			},
			{
				name:     "EmptyLastBlock",
				source:   "var score int \"score\"\nvar ()\n",
				wantTags: []string{"\"score\""},
			},
			{
				name:       "GroupedInitializedTag",
				source:     "var (\n score int = 1 \"score\"\n)\nvar ()\n",
				wantTags:   []string{"\"score\""},
				wantValues: []string{"1"},
			},
			{
				name:       "InitializedTags",
				source:     "var score int = 1 \"score\"\nvar speed int = 2 \"speed\"\n",
				wantTags:   []string{"\"score\"", "\"speed\""},
				wantValues: []string{"1", "2"},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const filename = "main_fixture.gox"
				s := newFrameworkTestServer(t, map[string][]byte{filename: []byte(tt.source)})
				t.Cleanup(s.Close)
				actions, err := s.textDocumentCodeAction(&CodeActionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
					Range:        Range{End: Position{Line: 100}},
				})
				require.NoError(t, err)
				require.Len(t, actions, 1)
				diagnostics, err := s.diagnosticsAt(s.requestProject())
				require.NoError(t, err)
				for _, diagnostic := range actions[0].Diagnostics {
					assert.Contains(t, diagnostics.diagnostics["file:///main_fixture.gox"], diagnostic)
				}
				got := applyCodeActionTestEdits(t, tt.source, actions[0], "file:///main_fixture.gox")
				fixed := newFrameworkTestServer(t, map[string][]byte{filename: []byte(got)})
				t.Cleanup(fixed.Close)
				requireNoDiagnostics(t, fixed)
				file, err := fixed.getProj().ASTFile(filename)
				require.NoError(t, err)
				require.NotNil(t, file.ClassFields)
				require.Len(t, file.ClassFields.Specs, len(tt.wantTags))
				for i, spec := range file.ClassFields.Specs {
					value, ok := spec.(*ast.ValueSpec)
					require.True(t, ok)
					if tt.wantTags[i] == "" {
						assert.Nil(t, value.Tag)
					} else {
						require.NotNil(t, value.Tag)
						assert.Equal(t, tt.wantTags[i], value.Tag.Value)
					}
					if tt.wantValues != nil {
						require.Len(t, value.Values, 1)
						initializer := requireValueAs[*ast.BasicLit](t, value.Values[0])
						assert.Equal(t, tt.wantValues[i], initializer.Value)
					}
				}
				if file.ShadowEntry != nil {
					assert.Empty(t, file.ShadowEntry.Body.List, "field tags must not become entry statements")
				}
			})
		}
	})

	t.Run("StaticNameSyntax", func(t *testing.T) {
		const source = "type Other int\nvar .shared, count int = 1, 2\nvar Other.total int = 3\n"
		s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
		t.Cleanup(s.Close)
		actions, err := s.textDocumentCodeAction(&CodeActionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
			Range:        Range{End: Position{Line: 100}},
		})
		require.NoError(t, err)
		require.Len(t, actions, 1)
		got := applyCodeActionTestEdits(t, source, actions[0], "file:///main_fixture.gox")
		fixed := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(got)})
		t.Cleanup(fixed.Close)
		file, err := fixed.getProj().ASTFile("main_fixture.gox")
		require.NoError(t, err)
		require.NotNil(t, file.ClassFields)
		require.Len(t, file.ClassFields.Specs, 2)
		first := requireValueAs[*ast.ValueSpec](t, file.ClassFields.Specs[0])
		assert.True(t, first.HasStatic)
		require.Len(t, first.Names, 2)
		assert.Equal(t, ".shared", first.Names[0].Name)
		assert.Equal(t, "count", first.Names[1].Name)
		assert.Len(t, first.Values, 2)
		second := requireValueAs[*ast.ValueSpec](t, file.ClassFields.Specs[1])
		assert.True(t, second.HasStatic)
		require.Len(t, second.Names, 1)
		assert.Equal(t, "Other.total", second.Names[0].Name)
		assert.Len(t, second.Values, 1)
	})

	t.Run("InitializerErrors", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			initializer string
			wantError   string
		}{
			{name: "UnresolvedName", initializer: "missing", wantError: "undefined: missing"},
			{name: "TypeMismatch", initializer: "\"bad\"", wantError: "cannot use"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				const filename = "main_fixture.gox"
				source := "var broken int = " + tt.initializer + "\nvar later bool\nfunc check() { println later }\n"
				s := newFrameworkTestServer(t, map[string][]byte{filename: []byte(source)})
				t.Cleanup(s.Close)
				_, err := s.requestProject().TypeInfo()
				require.ErrorContains(t, err, tt.wantError)
				actions, err := s.textDocumentCodeAction(&CodeActionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				})
				require.NoError(t, err)
				require.Len(t, actions, 1)
				got := applyCodeActionTestEdits(t, source, actions[0], "file:///main_fixture.gox")
				assert.Contains(t, got, "broken int = "+tt.initializer)
				fixed := newFrameworkTestServer(t, map[string][]byte{filename: []byte(got)})
				t.Cleanup(fixed.Close)
				proj := fixed.requestProject()
				info, err := proj.TypeInfo()
				require.ErrorContains(t, err, tt.wantError)
				require.NotNil(t, info)
				file, err := proj.ASTFile(filename)
				require.NoError(t, err)
				require.NotNil(t, file.ClassFields)
				require.Len(t, file.ClassFields.Specs, 2)
				for i, wantType := range []string{"int", "bool"} {
					spec := requireValueAs[*ast.ValueSpec](t, file.ClassFields.Specs[i])
					require.Len(t, spec.Names, 1)
					field := requireValueAs[*gotypes.Var](t, info.ObjectOf(spec.Names[0]))
					assert.True(t, field.IsField())
					assert.Equal(t, wantType, field.Type().String())
				}
			})
		}
	})

	t.Run("CurrentOpenDocument", func(t *testing.T) {
		const uri = "file:///main_fixture.gox"
		s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte("var saved int\n")})
		t.Cleanup(s.Close)
		require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
			TextDocument: protocol.TextDocumentItem{
				URI:     uri,
				Version: 7,
				Text:    "var count int\nvar step int\n",
			},
		}))
		params := &CodeActionParams{TextDocument: TextDocumentIdentifier{URI: uri}}
		actions, err := s.textDocumentCodeAction(params)
		require.NoError(t, err)
		require.Len(t, actions, 1)
		got := applyCodeActionTestEdits(t, "var count int\nvar step int\n", actions[0], uri)
		assert.Equal(t, "var (\n\tcount int\n\n\tstep int\n)\n", got)
		params.Context.Diagnostics = actions[0].Diagnostics
		require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
			TextDocument: protocol.VersionedTextDocumentIdentifier{
				TextDocumentIdentifier: TextDocumentIdentifier{URI: uri},
				Version:                8,
			},
			ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: got}},
		}))
		actions, err = s.textDocumentCodeAction(params)
		require.NoError(t, err)
		assert.Empty(t, actions, "stale diagnostics must not offer an obsolete fix")
		requireNoDiagnostics(t, s)
	})

	t.Run("CommentsAndFieldSyntax", func(t *testing.T) {
		const source = `type Base struct{}

// Fields.
var ( // First opening.
	// Score documentation.
	score int "score" // Score trailing.
) // First closing.

// Second group.
var /* Before opening. */ ( /* Second opening. */
	*Base
	speed int = 10
) /* Second closing. */

// Title documentation.
var /* Inline comment. */ title string = "Game" // Title trailing.

// Entry documentation.
println title
var local int = 2
println local
`
		s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
		t.Cleanup(s.Close)
		actions, err := s.textDocumentCodeAction(&CodeActionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
			Range:        Range{Start: Position{Line: 3}, End: Position{Line: 3}},
		})
		require.NoError(t, err)
		require.Len(t, actions, 1)
		got := applyCodeActionTestEdits(t, source, actions[0], "file:///main_fixture.gox")
		for _, comment := range []string{
			"// Fields.", "// First opening.", "// Score documentation.", "// Score trailing.", "// First closing.",
			"// Second group.", "/* Before opening. */", "/* Second opening. */", "/* Second closing. */",
			"// Title documentation.", "/* Inline comment. */", "// Title trailing.", "// Entry documentation.",
		} {
			assert.Equal(t, 1, strings.Count(got, comment), "%s must appear exactly once", comment)
		}
		assert.Contains(t, got, "score int \"score\"")
		assert.Contains(t, got, "*Base")
		assert.Contains(t, got, "// Entry documentation.\nprintln title\nvar local int = 2\nprintln local\n")
		fixed := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(got)})
		t.Cleanup(fixed.Close)
		requireNoDiagnostics(t, fixed)
	})

	t.Run("InlineBlockComments", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			source string
		}{
			{"SingleDeclarations", "var a int /* first */; /* separator */ var b int /* second */\n"},
			{"GroupedDeclarations", "var (a int;) /* first */; /* separator */ var (b int;) /* second */\n"},
			{"MultilineComment", "var a int /* multiline\ncomment */\nvar b int\n"},
			{"CRLFComment", "var a int /* multiline\r\ncomment */\r\nvar b int\r\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source := tt.source
				s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
				t.Cleanup(s.Close)
				before, err := s.getProj().ASTFile("main_fixture.gox")
				require.NoError(t, err)
				actions, err := s.textDocumentCodeAction(&CodeActionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				})
				require.NoError(t, err)
				require.Len(t, actions, 1)
				got := applyCodeActionTestEdits(t, source, actions[0], "file:///main_fixture.gox")
				fixed := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(got)})
				t.Cleanup(fixed.Close)
				requireNoDiagnostics(t, fixed)
				after, err := fixed.getProj().ASTFile("main_fixture.gox")
				require.NoError(t, err)
				var wantComments, gotComments []string
				for _, group := range before.Comments {
					for _, comment := range group.List {
						wantComments = append(wantComments, strings.Join(strings.Fields(comment.Text), " "))
					}
				}
				for _, group := range after.Comments {
					for _, comment := range group.List {
						gotComments = append(gotComments, strings.Join(strings.Fields(comment.Text), " "))
					}
				}
				assert.Equal(t, wantComments, gotComments)
			})
		}
	})

	t.Run("CRLFRawString", func(t *testing.T) {
		const source = "var text = `first\r\nsecond`\r\nvar step int\r\n"
		s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
		t.Cleanup(s.Close)
		actions, err := s.textDocumentCodeAction(&CodeActionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
		})
		require.NoError(t, err)
		require.Len(t, actions, 1)
		got := applyCodeActionTestEdits(t, source, actions[0], "file:///main_fixture.gox")
		assert.Contains(t, got, "`first\r\nsecond`")
		fixed := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(got)})
		t.Cleanup(fixed.Close)
		requireNoDiagnostics(t, fixed)
	})

	t.Run("FlatFramework", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			filename string
			want     bool
		}{
			{name: "Project", filename: "main_fixture.gox", want: true},
			{name: "Fragment", filename: "Part_fixture.gox"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				mod := testframework.NewModule(t)
				mod.Opt.Projects[0].Flat = true
				require.NoError(t, mod.ImportClasses())
				s := newFrameworkTestServerWithModule(t, map[string][]byte{
					"main_fixture.gox": nil,
					tt.filename:        []byte("var a int\nvar b int\n"),
				}, mod)
				t.Cleanup(s.Close)
				actions, err := s.textDocumentCodeAction(&CodeActionParams{
					TextDocument: TextDocumentIdentifier{URI: DocumentURI("file:///" + tt.filename)},
				})
				require.NoError(t, err)
				if tt.want {
					assert.Len(t, actions, 1)
				} else {
					assert.Empty(t, actions)
				}
			})
		}
	})

	t.Run("UTF16Positions", func(t *testing.T) {
		const source = "var first string = \"\U0001f600\"; var second int\n"
		s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
		t.Cleanup(s.Close)
		actions, err := s.textDocumentCodeAction(&CodeActionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
			Range:        Range{Start: Position{Character: 25}, End: Position{Character: 25}},
		})
		require.NoError(t, err)
		require.Len(t, actions, 1)
		require.Len(t, actions[0].Diagnostics, 1)
		assert.Equal(t, Range{Start: Position{Character: 25}, End: Position{Character: 39}}, actions[0].Diagnostics[0].Range)
		got := applyCodeActionTestEdits(t, source, actions[0], "file:///main_fixture.gox")
		assert.Contains(t, got, "first string = \"\U0001f600\"")
		fixed := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(got)})
		t.Cleanup(fixed.Close)
		requireNoDiagnostics(t, fixed)
	})

	t.Run("Filtering", func(t *testing.T) {
		for _, tt := range []struct {
			name      string
			filename  string
			source    string
			selection Range
			only      []protocol.CodeActionKind
			want      bool
		}{
			{name: "FirstDeclaration", source: "var a int\nvar b int\n", want: true},
			{name: "SecondDeclaration", source: "var a int\nvar b int\n", selection: Range{Start: Position{Line: 1}, End: Position{Line: 1}}, want: true},
			{name: "DeclarationEnd", source: "var a int\nvar b int\n", selection: Range{Start: Position{Character: 9}, End: Position{Character: 9}}, want: true},
			{name: "OtherCode", source: "const n = 1\nvar a int\nvar b int\n"},
			{name: "ReversedRange", source: "var a int\nvar b int\n", selection: Range{Start: Position{Line: 2}}},
			{name: "OutsideDocument", source: "var a int\nvar b int\n", selection: Range{Start: Position{Line: 10}, End: Position{Line: 10}}},
			{name: "QuickFix", source: "var a int\nvar b int\n", only: []protocol.CodeActionKind{protocol.QuickFix}, want: true},
			{name: "XGoQuickFix", source: "var a int\nvar b int\n", only: []protocol.CodeActionKind{"quickfix.xgo"}, want: true},
			{name: "MergeVarDeclarations", source: "var a int\nvar b int\n", only: []protocol.CodeActionKind{"quickfix.xgo.mergeVarDeclarations"}, want: true},
			{name: "AllKinds", source: "var a int\nvar b int\n", only: []protocol.CodeActionKind{protocol.Empty}, want: true},
			{name: "EmptyKindFilter", source: "var a int\nvar b int\n", only: []protocol.CodeActionKind{}, want: true},
			{name: "MultipleKinds", source: "var a int\nvar b int\n", only: []protocol.CodeActionKind{protocol.Refactor, protocol.QuickFix}, want: true},
			{name: "Refactor", source: "var a int\nvar b int\n", only: []protocol.CodeActionKind{protocol.Refactor}},
			{name: "FixAll", source: "var a int\nvar b int\n", only: []protocol.CodeActionKind{protocol.SourceFixAll}},
			{name: "QuickFixSubtype", source: "var a int\nvar b int\n", only: []protocol.CodeActionKind{"quickfix.other"}},
			{name: "OtherXGoQuickFix", source: "var a int\nvar b int\n", only: []protocol.CodeActionKind{"quickfix.xgo.other"}},
			{name: "PartialKindSegment", source: "var a int\nvar b int\n", only: []protocol.CodeActionKind{"quickfix.xg"}},
			{name: "PartialActionKind", source: "var a int\nvar b int\n", only: []protocol.CodeActionKind{"quickfix.xgo.mergeVarDeclaration"}},
			{name: "MoreSpecificKind", source: "var a int\nvar b int\n", only: []protocol.CodeActionKind{"quickfix.xgo.mergeVarDeclarations.other"}},
			{name: "OrdinaryXGo", filename: "main.xgo", source: "var a int\nvar b int\n"},
			{name: "NonSource", filename: "notes.txt", source: "var a int\nvar b int\n"},
			{name: "MissingFile", filename: "missing.gox"},
			{name: "NoVariables", source: "println 1\n"},
			{name: "SingleVariable", source: "var a int\n"},
			{name: "LocalVariables", source: "println 1\nvar a int\nvar b int\n"},
			{name: "NestedVariables", source: "var a int\nfunc run() { var b int; println b }\n"},
			{name: "IncompleteSyntax", source: "var a int\nvar (\nb int\n"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				filename := tt.filename
				if filename == "" {
					filename = "main_fixture.gox"
				}
				files := map[string][]byte{}
				if tt.source != "" {
					files[filename] = []byte(tt.source)
				}
				s := newFrameworkTestServer(t, files)
				t.Cleanup(s.Close)
				actions, err := s.textDocumentCodeAction(&CodeActionParams{
					TextDocument: TextDocumentIdentifier{URI: DocumentURI("file:///" + filename)},
					Range:        tt.selection,
					Context:      protocol.CodeActionContext{Only: tt.only},
				})
				require.NoError(t, err)
				if tt.want {
					assert.Len(t, actions, 1)
				} else {
					assert.Empty(t, actions)
				}
			})
		}
	})

	t.Run("InvalidURI", func(t *testing.T) {
		s := newFrameworkTestServer(t, nil)
		t.Cleanup(s.Close)
		actions, err := s.textDocumentCodeAction(&CodeActionParams{
			TextDocument: TextDocumentIdentifier{URI: "https://example.com/file.gox"},
		})
		require.Error(t, err)
		assert.Empty(t, actions)
	})
}

func TestServerCodeActionProtocol(t *testing.T) {
	for _, tt := range []struct {
		name           string
		literalSupport bool
		only           []protocol.CodeActionKind
		malformed      bool
	}{
		{name: "LiteralActions", literalSupport: true},
		{name: "SpecificKind", literalSupport: true, only: []protocol.CodeActionKind{"quickfix.xgo.mergeVarDeclarations"}},
		{name: "NoLiteralSupport"},
		{name: "MalformedParams", literalSupport: true, malformed: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const source = "var score int\nvar speed int = 10\n"
			s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
			t.Cleanup(s.Close)
			replier := newMockReplier()
			s.replier = replier
			capabilities := ClientCapabilities{}
			if tt.literalSupport {
				capabilities = codeActionClientCapabilitiesForTest()
			}
			initialize, err := jsonrpc2.NewCall(jsonrpc2.NewIntID(100), "initialize", InitializeParams{
				XInitializeParams: protocol.XInitializeParams{Capabilities: capabilities},
			})
			require.NoError(t, err)
			require.NoError(t, s.HandleMessage(initialize))
			response := replier.waitForResponse(initialize.ID(), 5*time.Second)
			require.NotNil(t, response)
			require.NoError(t, response.Err())
			replier.clearMessages()
			var params any = CodeActionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Context:      protocol.CodeActionContext{Only: tt.only},
			}
			if tt.malformed {
				params = json.RawMessage(`{"textDocument":123}`)
			}
			call, err := jsonrpc2.NewCall(jsonrpc2.NewIntID(101), "textDocument/codeAction", params)
			require.NoError(t, err)
			require.NoError(t, s.HandleMessage(call))
			response = replier.waitForResponse(call.ID(), 5*time.Second)
			require.NotNil(t, response)
			if tt.malformed {
				require.ErrorIs(t, response.Err(), jsonrpc2.ErrParse)
				return
			}
			require.NoError(t, response.Err())
			var actions []CodeAction
			require.NoError(t, json.Unmarshal(response.Result(), &actions))
			if tt.literalSupport {
				require.Len(t, actions, 1)
				assert.Equal(t, protocol.CodeActionKind("quickfix.xgo.mergeVarDeclarations"), actions[0].Kind)
				assert.Equal(t, "Merge top-level var declarations", actions[0].Title)
				assert.Contains(t, applyCodeActionTestEdits(t, source, actions[0], "file:///main_fixture.gox"), "speed int = 10")
			} else {
				assert.Empty(t, actions)
			}
		})
	}
}

func TestServerCodeActionDocumentVersion(t *testing.T) {
	for _, tt := range []struct {
		name          string
		open          bool
		version       int32
		workspaceEdit *protocol.WorkspaceEditClientCapabilities
		wantVersioned bool
	}{
		{name: "OpenDocument", open: true, version: 7, workspaceEdit: &protocol.WorkspaceEditClientCapabilities{DocumentChanges: true}, wantVersioned: true},
		{name: "ZeroVersion", open: true, workspaceEdit: &protocol.WorkspaceEditClientCapabilities{DocumentChanges: true}, wantVersioned: true},
		{name: "NoWorkspaceEditSupport", open: true, version: 7},
		{name: "NoVersionedEditSupport", open: true, version: 7, workspaceEdit: &protocol.WorkspaceEditClientCapabilities{}},
		{name: "ClosedDocument", workspaceEdit: &protocol.WorkspaceEditClientCapabilities{DocumentChanges: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const source = "var count int\nvar step int\n"
			const uri = "file:///main_fixture.gox"
			s := newFrameworkTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
			t.Cleanup(s.Close)
			capabilities := codeActionClientCapabilitiesForTest()
			capabilities.Workspace.WorkspaceEdit = tt.workspaceEdit
			_, err := s.initialize(&InitializeParams{XInitializeParams: protocol.XInitializeParams{Capabilities: capabilities}})
			require.NoError(t, err)
			s.finishInitialize(nil)
			if tt.open {
				require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
					TextDocument: protocol.TextDocumentItem{URI: uri, Version: tt.version, Text: source},
				}))
			}
			params := &CodeActionParams{TextDocument: TextDocumentIdentifier{URI: uri}}
			actions, err := s.textDocumentCodeAction(params)
			require.NoError(t, err)
			require.Len(t, actions, 1)
			encoded, err := json.Marshal(actions)
			require.NoError(t, err)
			var decoded []CodeAction
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			require.Len(t, decoded, 1)
			assert.Equal(t, "var (\n\tcount int\n\n\tstep int\n)\n", applyCodeActionTestEdits(t, source, actions[0], uri))
			if !tt.wantVersioned {
				assert.Empty(t, actions[0].Edit.DocumentChanges)
				assert.Len(t, actions[0].Edit.Changes, 1)
				return
			}
			require.Len(t, decoded[0].Edit.DocumentChanges, 1)
			change := decoded[0].Edit.DocumentChanges[0].TextDocumentEdit
			require.NotNil(t, change)
			assert.Equal(t, tt.version, change.TextDocument.Version)

			snapshot, version := s.codeActionSnapshot("main_fixture.gox")
			require.NotNil(t, version)
			updated := "// Updated.\n" + source
			require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{
					TextDocumentIdentifier: params.TextDocument,
					Version:                tt.version + 1,
				},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: updated}},
			}))
			original, ok := snapshot.File("main_fixture.gox")
			require.True(t, ok)
			assert.Equal(t, source, string(original.Content))
			assert.Equal(t, tt.version, *version)
			assert.Equal(t, tt.version, change.TextDocument.Version, "old edits must keep their original version")
			actions, err = s.textDocumentCodeAction(params)
			require.NoError(t, err)
			require.Len(t, actions, 1)
			require.Len(t, actions[0].Edit.DocumentChanges, 1)
			change = actions[0].Edit.DocumentChanges[0].TextDocumentEdit
			require.NotNil(t, change)
			assert.Equal(t, tt.version+1, change.TextDocument.Version)
		})
	}
}

func TestServerCodeActionSnapshot(t *testing.T) {
	const filename = "main_fixture.gox"
	const source = "var score int\nvar speed int = 10\n"
	s := newFrameworkTestServer(t, map[string][]byte{filename: []byte(source)})
	t.Cleanup(s.Close)
	proj := s.requestProject()
	file, err := proj.ASTFile(filename)
	require.NoError(t, err)
	originalFields := file.ClassFields
	originalDecls := slices.Clone(file.Decls)
	params := &CodeActionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			actions, err := s.textDocumentCodeAction(params)
			assert.NoError(t, err)
			assert.Len(t, actions, 1)
		})
	}
	wg.Wait()
	assert.Same(t, originalFields, file.ClassFields)
	assert.Equal(t, originalDecls, file.Decls)
	var vars int
	for _, decl := range file.Decls {
		if decl, ok := decl.(*ast.GenDecl); ok && decl.Tok == token.VAR {
			vars++
		}
	}
	assert.Equal(t, 2, vars)
}
