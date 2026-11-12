package server

import (
	"encoding/json"
	"fmt"
	gotypes "go/types"
	"maps"
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerRequestsDuringSyntaxRecovery(t *testing.T) {
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
			for _, source := range []string{
				"func run(",
				"func run() { value :=",
				"func run() { value := []int{",
				"func run() { for i := 0; i <",
				"func run() { println [n for n <-",
				"func run() { println {n: n for n <- [1], v :=",
				"func run() { switch v := any(1).(type) { case",
				"func run() { switch v := any(1).(type) { case int: println v",
				"func run[T any](v T) { println v",
				"type Value[T any] struct { Next *Value[",
				"type Value = struct { Field",
				"func run() { println f(value=",
				"func run() { println \"${",
				`//line mapped.xgo:500
func run() { println missing.`,
				"// Prefix \U0001F600\r\nfunc run() { println 1 +",
			} {
				files := map[string][]byte{kind.filename: []byte(source)}
				if kind.name == "WorkClass" {
					files["main_fixture.gox"] = nil
				}
				s := newFrameworkTestServer(t, files)
				p := TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(kind.filename)},
					Position: Position{
						Line:      uint32(strings.Count(source, "\n")),
						Character: uint32(UTF16Len(source[strings.LastIndexByte(source, '\n')+1:])),
					},
				}
				t.Logf("Checking %q", source)
				want := projectRequestObservations(t, s, p, false)
				assert.Equal(t, want, projectRequestObservations(t, s, p, true), source)
			}
		})
	}
}

func TestServerRequestsWithLineEndings(t *testing.T) {
	for _, ending := range []struct{ name, first, second, last string }{
		{"LF", "\n", "\n", "\n"},
		{"CRLF", "\r\n", "\r\n", "\r\n"},
		{"CR", "\r", "\r", "\r"},
		{"Mixed", "\r\n", "\r", "\n"},
	} {
		t.Run(ending.name, func(t *testing.T) {
			for _, kind := range []struct{ name, filename string }{
				{"XGo", "main.xgo"},
				{"NormalClass", "Record.gox"},
				{"ProjectClass", "main_fixture.gox"},
				{"WorkClass", "Worker_fixture.gox"},
			} {
				t.Run(kind.name, func(t *testing.T) {
					source, pos := typeDisplayTestSource(t, "var value = 1;"+ending.first+"println `\U0001f680`;"+ending.second+"println |value;"+ending.last)
					files := map[string][]byte{kind.filename: []byte(source)}
					if kind.name == "WorkClass" {
						files["main_fixture.gox"] = nil
					}
					s := newImportTestServer(t, files)
					t.Cleanup(s.Close)
					_, err := s.requestProject().TypeInfo()
					require.NoError(t, err)
					p := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(kind.filename)}, Position: pos}
					want := projectRequestObservations(t, s, p, false)
					assert.Equal(t, want, projectRequestObservations(t, s, p, true))
					hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: p})
					require.NoError(t, err)
					require.NotNil(t, hover)
					assert.Equal(t, Range{Start: Position{Line: 2, Character: 8}, End: Position{Line: 2, Character: 13}}, hover.Range)
					assert.Contains(t, hover.Contents.Value, "value int")
					definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: p})
					require.NoError(t, err)
					assert.Equal(t, Position{Character: 4}, requireValueAs[Location](t, definition).Range.Start)
					tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{TextDocument: p.TextDocument})
					require.NoError(t, err)
					assert.Contains(t, decodeSemanticTokens(tokens.Data), decodedSemanticToken{line: 1, character: 8, length: 4, tokenType: StringType})

					s.ModifyFiles([]FileChange{{Path: kind.filename, Content: []byte(strings.Replace(source, "println value;", "println missing;", 1)), Version: 1}})
					diagnostics := s.getDiagnostics(t, kind.filename)
					require.Len(t, diagnostics, 1)
					assert.Contains(t, diagnostics[0].Message, "undefined: missing")
					assert.Equal(t, Range{Start: Position{Line: 2, Character: 8}, End: Position{Line: 2, Character: 15}}, diagnostics[0].Range)
				})
			}
		})
	}
}

func TestServerRequestsWithIncompleteTypeInfo(t *testing.T) {
	s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte("var Good = 1\nBox\n")})
	t.Cleanup(s.Close)
	proj := s.requestProject()
	info, err := proj.TypeInfo()
	require.NotNil(t, info)
	require.Error(t, err)
	// A failed declaration can leave a type name without its type in scope.
	typeName := gotypes.NewTypeName(token.NoPos, info.Pkg, "Box", nil)
	require.Nil(t, info.Pkg.Scope().Insert(typeName))
	p := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")},
		Position:     Position{Line: 1, Character: 1},
	}
	astFile, err := proj.ASTFile("main.xgo")
	require.NoError(t, err)
	ast.Inspect(astFile, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok && ident.Name == "Box" {
			info.Uses[ident] = typeName
		}
		return true
	})
	want := projectRequestObservations(t, s, p, false)
	assert.Equal(t, want, projectRequestObservations(t, s, p, true))
	items := completionItemsAt(t, s, "main.xgo", p.Position)
	assert.Contains(t, completionItemLabels(items), "Good")
	assert.Contains(t, completionItemLabels(items), "int")
	item := completionItemByLabel(items, "Box")
	require.NotNil(t, item)
	assert.Equal(t, ClassCompletion, item.Kind)
	assert.Equal(t, "Box", item.InsertText)
	data := requireValueAs[*CompletionItemData](t, item.Data)
	assert.Equal(t, "xgo:main?Box", data.Definition.String())
	doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
	assert.Contains(t, doc.Value, "type Box")
	implementation, err := s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: p})
	require.NoError(t, err)
	assert.Nil(t, implementation)

	s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte("var Good = 1\ntype Box struct{}\n\n"), Version: 1}})
	_, err = s.requestProject().TypeInfo()
	require.NoError(t, err)
	item = completionItemByLabel(completionItemsAt(t, s, "main.xgo", Position{Line: 2}), "Box")
	require.NotNil(t, item)
	assert.Equal(t, StructCompletion, item.Kind)
}

func TestServerRequestsWithInvalidAliases(t *testing.T) {
	for _, tt := range []struct{ name, source string }{
		{"Self", "type Box = Box\n"},
		{"Mutual", "type Box = Other\ntype Other = Box\n"},
		{"Missing", "type Box = Missing\n"},
		{"StructSelf", "type Box = struct { Value Box }\n"},
		{"LocalSelf", "func run() { type Box = Box }\n"},
		{"LocalMutual", "func run() { type Box = Other; type Other = Box }\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "var Good = 1\n"+tt.source+"echo |Good\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			info, err := s.requestProject().TypeInfo()
			require.NotNil(t, info)
			require.Error(t, err)
			p := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI("main.xgo")}, Position: pos}
			want := projectRequestObservations(t, s, p, false)
			assert.Equal(t, want, projectRequestObservations(t, s, p, true))
			assert.Contains(t, completionItemLabels(completionItemsAt(t, s, "main.xgo", pos)), "Good")
			p.Position = Position{Character: 4}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: p})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, "var Good int")
		})
	}
}

func TestServerRequestHistoryIndependence(t *testing.T) {
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
			const original = `type Count int
var current Count
func add(v Count) Count { return v + 1 }
func run() {
println add(current)
}
`
			files := map[string][]byte{kind.filename: []byte(original)}
			if kind.name == "WorkClass" {
				files["main_fixture.gox"] = nil
			}
			s := kind.newServer(t, files)
			originalProject := s.requestProject()
			originalAST, err := originalProject.ASTFile(kind.filename)
			require.NoError(t, err)
			for version, state := range []struct {
				name   string
				source string
			}{
				{name: "Initial", source: original},
				{name: "Moved", source: "// Moved source.\n" + original},
				{name: "InvalidPackage", source: "package"},
				{name: "UnfinishedRange", source: "for value <-"},
				{name: "UnfinishedType", source: "type Value struct {"},
				{name: "Empty", source: ""},
				{name: "Restored", source: original},
			} {
				t.Logf("Checking state %s", state.name)
				s.ModifyFiles([]FileChange{{Path: kind.filename, Content: []byte(state.source), Version: version + 1}})
				currentFiles := maps.Clone(files)
				currentFiles[kind.filename] = []byte(state.source)
				fresh := kind.newServer(t, currentFiles)
				p := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(kind.filename)}, Position: Position{Line: 4, Character: 13}}
				want := projectRequestObservations(t, fresh, p, false)
				got := projectRequestObservations(t, s, p, true)
				assert.Equal(t, want, got)
				assert.Equal(t, want, projectRequestObservations(t, s, p, false))
				assert.Equal(t, original, string(originalAST.Code))
				retained, err := originalProject.ASTFile(kind.filename)
				require.NoError(t, err)
				assert.Same(t, originalAST, retained)
			}
		})
	}
}

func projectRequestObservations(t *testing.T, s *Server, p TextDocumentPositionParams, reverse bool) map[string]string {
	t.Helper()

	requests := []struct {
		name string
		read func() (any, error)
	}{
		{"Declaration", func() (any, error) {
			return s.textDocumentDeclaration(&DeclarationParams{TextDocumentPositionParams: p})
		}},
		{"Definition", func() (any, error) { return s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: p}) }},
		{"TypeDefinition", func() (any, error) {
			return s.textDocumentTypeDefinition(&TypeDefinitionParams{TextDocumentPositionParams: p})
		}},
		{"Implementation", func() (any, error) {
			return s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: p})
		}},
		{"Hover", func() (any, error) { return s.textDocumentHover(&HoverParams{TextDocumentPositionParams: p}) }},
		{"Completion", func() (any, error) { return s.textDocumentCompletion(&CompletionParams{TextDocumentPositionParams: p}) }},
		{"SignatureHelp", func() (any, error) {
			return s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: p})
		}},
		{"References", func() (any, error) {
			return s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: p, Context: ReferenceContext{IncludeDeclaration: true}})
		}},
		{"Highlight", func() (any, error) {
			return s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: p})
		}},
		{"PrepareRename", func() (any, error) {
			return s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: p})
		}},
		{"Rename", func() (any, error) {
			return s.textDocumentRename(&RenameParams{TextDocument: p.TextDocument, Position: p.Position, NewName: "renamed"})
		}},
		{"DocumentLink", func() (any, error) {
			return s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: p.TextDocument})
		}},
		{"SemanticTokens", func() (any, error) {
			return s.textDocumentSemanticTokensFull(&SemanticTokensParams{TextDocument: p.TextDocument})
		}},
		{"InlayHints", func() (any, error) {
			return s.textDocumentInlayHint(&InlayHintParams{TextDocument: p.TextDocument, Range: Range{End: Position{Line: 100}}})
		}},
		{"InputSlots", func() (any, error) {
			return s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: p.TextDocument}})
		}},
		{"Diagnostics", func() (any, error) {
			return s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: p.TextDocument})
		}},
		{"Formatting", func() (any, error) {
			return s.textDocumentFormatting(&DocumentFormattingParams{TextDocument: p.TextDocument})
		}},
	}
	observations := make(map[string]string, len(requests))
	for i := range requests {
		index := i
		if reverse {
			index = len(requests) - 1 - i
		}
		request := requests[index]
		var result any
		var err error
		require.NotPanics(t, func() { result, err = request.read() }, request.name)
		data, marshalErr := json.Marshal(result)
		require.NoError(t, marshalErr)
		observations[request.name] = fmt.Sprintf("%s\n%v", data, err)
	}
	return observations
}

func TestServerRequestsWithIncompleteMethods(t *testing.T) {
	for _, kind := range []struct {
		name string
		path string
	}{
		{name: "XGo", path: "main.xgo"},
		{name: "NormalClass", path: "Record.gox"},
		{name: "ProjectClass", path: "main_fixture.gox"},
		{name: "WorkClass", path: "Worker_fixture.gox"},
	} {
		t.Run(kind.name, func(t *testing.T) {
			for _, tt := range []struct {
				name   string
				source string
			}{
				{name: "MissingResult", source: `type Reader interface { |Read() Missing }
type RecordValue struct{}
func (RecordValue) Read() Missing { return nil }
`},
				{name: "MissingParameter", source: `type Reader interface { |Read(value Missing) }
type RecordValue struct{}
func (RecordValue) Read(value Missing) {}
`},
				{name: "RecursiveInterface", source: "type Reader interface { Reader; |Read() int }\n"},
				{name: "IncompleteSignature", source: "type Reader interface { |Read("},
				{name: "MissingReceiver", source: "func (Missing) |Read() int { return 1 }\n"},
				{name: "RecursiveStruct", source: `type RecordValue struct { RecordValue }
func (RecordValue) |Read() int { return 1 }
`},
				{name: "MissingOverloadCandidate", source: `func |Read = (
missing
)
`},
				{name: "VariableOverloadCandidate", source: `var other int
func |Read = (
other
)
`},
				{name: "MissingOverloadAtCall", source: `func Read = (
missing
)
func run() { |Read(1) }
`},
				{name: "MixedOverloadCandidates", source: `func resolved(value int) {}
func |Read = (
resolved
missing
)
`},
				{name: "EmptyOverload", source: "func |Read = ()\n"},
				{name: "IncompleteOverload", source: "func |Read = (\nfunc(value int)"},
			} {
				t.Run(tt.name, func(t *testing.T) {
					source, position := typeDisplayTestSource(t, tt.source)
					files := map[string][]byte{kind.path: []byte(source)}
					if kind.name == "WorkClass" {
						files["main_fixture.gox"] = nil
					}
					s := newFrameworkTestServer(t, files)
					s.replier = newMockReplier()
					params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(kind.path)}, Position: position}
					projectRequestObservations(t, s, params, false)
					projectRequestObservations(t, s, params, true)
				})
			}
		})
	}
}
