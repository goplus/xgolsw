package server

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"testing"

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
				"//line mapped.xgo:500\nfunc run() { println missing.",
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
			const original = "type Count int\nvar current Count\nfunc add(v Count) Count { return v + 1 }\nfunc run() {\nprintln add(current)\n}\n"
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
				{name: "MissingResult", source: "type Reader interface { |Read() Missing }\ntype RecordValue struct{}\nfunc (RecordValue) Read() Missing { return nil }\n"},
				{name: "MissingParameter", source: "type Reader interface { |Read(value Missing) }\ntype RecordValue struct{}\nfunc (RecordValue) Read(value Missing) {}\n"},
				{name: "RecursiveInterface", source: "type Reader interface { Reader; |Read() int }\n"},
				{name: "IncompleteSignature", source: "type Reader interface { |Read("},
				{name: "MissingReceiver", source: "func (Missing) |Read() int { return 1 }\n"},
				{name: "RecursiveStruct", source: "type RecordValue struct { RecordValue }\nfunc (RecordValue) |Read() int { return 1 }\n"},
				{name: "MissingOverloadCandidate", source: "func |Read = (\nmissing\n)\n"},
				{name: "VariableOverloadCandidate", source: "var other int\nfunc |Read = (\nother\n)\n"},
				{name: "MissingOverloadAtCall", source: "func Read = (\nmissing\n)\nfunc run() { |Read(1) }\n"},
				{name: "MixedOverloadCandidates", source: "func resolved(value int) {}\nfunc |Read = (\nresolved\nmissing\n)\n"},
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
