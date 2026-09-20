package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerRangeExpressionSourcePositions(t *testing.T) {
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
				name string
				loop string
			}{
				{name: "Bound", loop: "for value = range 0:len(values) { echo value }"},
				{name: "Step", loop: "for value = range 0:5:len(values) { echo value }"},
				{name: "BoundAndStep", loop: "for value = range 0:len(values):len(values) { echo value }"},
			} {
				t.Run(tt.name, func(t *testing.T) {
					const prefix = "func run() {\nvalues := [1, 2]\nvar value int\n"
					source := prefix + tt.loop + "\n}\n"
					files := map[string][]byte{kind.filename: []byte(source)}
					if kind.filename == "Worker_fixture.gox" {
						files["main_fixture.gox"] = nil
					}
					s := kind.newServer(t, files)
					_, err := s.requestProject().TypeInfo()
					require.NoError(t, err)
					uri := s.toDocumentURI(kind.filename)
					params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 3}}
					for _, request := range []struct {
						name string
						read func() (any, error)
					}{
						{name: "Hover", read: func() (any, error) { return s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params}) }},
						{name: "Definition", read: func() (any, error) {
							return s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
						}},
						{name: "References", read: func() (any, error) {
							return s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: params})
						}},
						{name: "Highlight", read: func() (any, error) {
							return s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: params})
						}},
						{name: "PrepareRename", read: func() (any, error) {
							return s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: params})
						}},
						{name: "Rename", read: func() (any, error) {
							return s.textDocumentRename(&RenameParams{TextDocument: params.TextDocument, Position: params.Position, NewName: "entry"})
						}},
					} {
						t.Run(request.name, func(t *testing.T) {
							got, err := request.read()
							require.NoError(t, err)
							assert.Empty(t, got, "the for keyword must not resolve to a generated variable")
						})
					}

					links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: params.TextDocument})
					require.NoError(t, err)
					for i, link := range links {
						assert.NotEqual(t, params.Position, link.Range.Start, "the for keyword must not link to a generated variable")
						assert.NotContains(t, links[:i], link)
					}
					tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{TextDocument: params.TextDocument})
					require.NoError(t, err)
					require.NotNil(t, tokens)
					decoded := decodeSemanticTokens(tokens.Data)
					assert.Contains(t, decoded, decodedSemanticToken{line: 3, length: 3, tokenType: KeywordType})
					assert.Contains(t, decoded, decodedSemanticToken{line: 3, character: 4, length: 5, tokenType: VariableType})
					for i, current := range decoded {
						if i > 0 && decoded[i-1].line == current.line {
							assert.GreaterOrEqual(t, current.character, decoded[i-1].character+decoded[i-1].length)
						}
					}
					cached, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{TextDocument: params.TextDocument})
					require.NoError(t, err)
					assert.Equal(t, tokens, cached)
					var edit *WorkspaceEdit
					for _, position := range []Position{{Line: 2, Character: 4}, {Line: 3, Character: 4}} {
						edit, err = s.textDocumentRename(&RenameParams{TextDocument: params.TextDocument, Position: position, NewName: "entry"})
						require.NoError(t, err)
						lastUse := uint32(strings.LastIndex(tt.loop, "value"))
						assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{uri: {
							{Range: Range{Start: Position{Line: 2, Character: 4}, End: Position{Line: 2, Character: 9}}, NewText: "entry"},
							{Range: Range{Start: Position{Line: 3, Character: 4}, End: Position{Line: 3, Character: 9}}, NewText: "entry"},
							{Range: Range{Start: Position{Line: 3, Character: lastUse}, End: Position{Line: 3, Character: lastUse + 5}}, NewText: "entry"},
						}})
					}
					updated := applyResourceRenameTestEdits(t, source, edit.Changes[uri])
					s.ModifyFiles([]FileChange{{Path: kind.filename, Content: []byte(updated), Version: 1}})
					_, err = s.requestProject().TypeInfo()
					assert.NoError(t, err)
				})
			}
		})
	}
}

func TestServerReusedRangeExpression(t *testing.T) {
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
			const source = "type Options struct { Offset int }\nfunc pick(index int, options Options?) int { return index + options.Offset }\nfunc run() {\nvalues := [1, 2]\nfor values[pick(0, offset = 1)] = range 0:len(values) {}\n}\n"
			files := map[string][]byte{kind.filename: []byte(source)}
			if kind.filename == "Worker_fixture.gox" {
				files["main_fixture.gox"] = nil
			}
			s := kind.newServer(t, files)
			document := TextDocumentIdentifier{URI: s.toDocumentURI(kind.filename)}
			for version := range 2 {
				line := uint32(4 + version)
				if version > 0 {
					s.ModifyFiles([]FileChange{{Path: kind.filename, Content: []byte("\n" + source), Version: version}})
				}
				_, err := s.requestProject().TypeInfo()
				require.NoError(t, err)
				hints, err := s.textDocumentInlayHint(&InlayHintParams{TextDocument: document, Range: Range{End: Position{Line: 10}}})
				require.NoError(t, err)
				assert.Equal(t, []InlayHint{{Position: Position{Line: line, Character: 16}, Label: "index", Kind: Parameter}}, hints)
				links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: document})
				require.NoError(t, err)
				var callLinks []DocumentLink
				var kwargLinks []DocumentLink
				for i, link := range links {
					assert.NotContains(t, links[:i], link)
					if link.Range.Start == (Position{Line: line, Character: 11}) {
						callLinks = append(callLinks, link)
					}
					if link.Range.Start == (Position{Line: line, Character: 19}) {
						kwargLinks = append(kwargLinks, link)
					}
				}
				assert.Len(t, callLinks, 1)
				assert.Len(t, kwargLinks, 1)
				tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{TextDocument: document})
				require.NoError(t, err)
				require.NotNil(t, tokens)
				decoded := decodeSemanticTokens(tokens.Data)
				assert.Contains(t, decoded, decodedSemanticToken{line: line, character: 16, length: 1, tokenType: NumberType})
				for i, current := range decoded {
					if i > 0 && decoded[i-1].line == current.line {
						assert.GreaterOrEqual(t, current.character, decoded[i-1].character+decoded[i-1].length)
					}
				}
			}
		})
	}
}

func TestServerLineDirectives(t *testing.T) {
	const source = "//line virtual.xgo:100:20\n\nvar value = 1\nfunc show(text string, number int) {}\nshow \"\U0001f600\", value\n"
	for _, project := range []struct {
		name      string
		filename  string
		newServer testServerFactory
	}{
		{"PlainXGo", "main.xgo", newTestServer},
		{"ProjectClass", "main_fixture.gox", newFrameworkTestServer},
		{"WorkClass", "Worker_fixture.gox", newFrameworkTestServer},
	} {
		t.Run(project.name, func(t *testing.T) {
			for _, feature := range []struct {
				name      string
				read      func(*Server, TextDocumentPositionParams) (any, error)
				unordered bool
			}{
				{"Hover", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentHover(&HoverParams{TextDocumentPositionParams: p})
				}, false},
				{"Definition", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: p})
				}, false},
				{"References", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: p, Context: ReferenceContext{IncludeDeclaration: true}})
				}, true},
				{"Highlight", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: p})
				}, false},
				{"PrepareRename", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: p})
				}, false},
				{"DocumentLinks", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: p.TextDocument})
				}, true},
				{"Completion", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentCompletion(&CompletionParams{TextDocumentPositionParams: p})
				}, false},
				{"SemanticTokens", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentSemanticTokensFull(&SemanticTokensParams{TextDocument: p.TextDocument})
				}, false},
				{"InlayHints", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentInlayHint(&InlayHintParams{TextDocument: p.TextDocument, Range: Range{End: Position{Line: 5}}})
				}, true},
				{"InputSlots", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: p.TextDocument}})
				}, false},
			} {
				t.Run(feature.name, func(t *testing.T) {
					// Keep the comment span identical while disabling the line directive.
					plainFiles := map[string][]byte{project.filename: []byte(strings.Replace(source, "//line", "//note", 1))}
					directedFiles := map[string][]byte{project.filename: []byte(source)}
					if project.filename == "Worker_fixture.gox" {
						plainFiles["main_fixture.gox"] = nil
						directedFiles["main_fixture.gox"] = nil
					}
					plain := project.newServer(t, plainFiles)
					directed := project.newServer(t, directedFiles)
					params := TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: plain.toDocumentURI(project.filename)},
						Position:     Position{Line: 4, Character: 13},
					}
					want, err := feature.read(plain, params)
					require.NoError(t, err)
					require.NotEmpty(t, want)
					for range 2 {
						got, err := feature.read(directed, params)
						require.NoError(t, err)
						if feature.unordered {
							assert.ElementsMatch(t, want, got)
						} else {
							assert.Equal(t, want, got)
						}
					}
					_, err = directed.getProj().TypeInfo()
					require.NoError(t, err)
				})
			}
		})
	}

	t.Run("ImportDocumentation", func(t *testing.T) {
		s := newFrameworkTestServer(t, map[string][]byte{
			"main.xgo": []byte("//line virtual.xgo:100:20\nimport \"example.com/framework\"\nprintln framework.High\n"),
		})
		hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: Position{Line: 1, Character: 20},
		}})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, "minimal classfile framework")
		assert.Equal(t, Range{Start: Position{Line: 1, Character: 7}, End: Position{Line: 1, Character: 30}}, hover.Range)
	})
}

func TestServerPositionsAtEOF(t *testing.T) {
	for _, project := range []struct {
		name      string
		filename  string
		newServer testServerFactory
	}{
		{"PlainXGo", "main.xgo", newTestServer},
		{"NormalClass", "Record.gox", newTestServer},
		{"ProjectClass", "main_fixture.gox", newFrameworkTestServer},
		{"WorkClass", "Worker_fixture.gox", newFrameworkTestServer},
	} {
		t.Run(project.name, func(t *testing.T) {
			for _, feature := range []struct {
				name string
				read func(*Server, TextDocumentPositionParams) (any, error)
			}{
				{"Hover", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentHover(&HoverParams{TextDocumentPositionParams: p})
				}},
				{"Definition", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: p})
				}},
				{"References", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: p, Context: ReferenceContext{IncludeDeclaration: true}})
				}},
				{"Highlight", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: p})
				}},
				{"PrepareRename", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: p})
				}},
				{"Rename", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.textDocumentRename(&RenameParams{TextDocument: p.TextDocument, Position: p.Position, NewName: "renamed"})
				}},
			} {
				t.Run(feature.name, func(t *testing.T) {
					files := map[string][]byte{project.filename: []byte("var value = 1\nvalue = 2\n")}
					if project.filename == "Worker_fixture.gox" {
						files["main_fixture.gox"] = nil
					}
					s := project.newServer(t, files)
					replier := newMockReplier()
					s.replier = replier
					for range 2 {
						got, err := feature.read(s, TextDocumentPositionParams{
							TextDocument: TextDocumentIdentifier{URI: s.toDocumentURI(project.filename)},
							Position:     Position{Line: 2},
						})
						require.NoError(t, err)
						assert.Empty(t, got)
					}
					_, err := s.getProj().TypeInfo()
					require.NoError(t, err)
					assert.Empty(t, replier.getMessages())
				})
			}
		})
	}
}

func TestServerLocalTypeNavigation(t *testing.T) {
	for _, tt := range []struct {
		name   string
		prefix string
		suffix string
	}{
		{name: "Function", prefix: "func run() {\n", suffix: "}\n"},
		{name: "Sibling", prefix: "func other() { type Item string; var value Item; echo value }\nfunc run() {\n", suffix: "}\n"},
		{name: "Nested", prefix: "func run() {\ntype Item string\nvar outer Item\necho outer\n{\n", suffix: "}\n}\n"},
		{name: "Closure", prefix: "func run() {\nwork := func() {\n", suffix: "}\nwork()\n}\n"},
		{name: "Switch", prefix: "func run() {\nswitch 1 {\ncase 1:\n", suffix: "}\n}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, declaration := range []struct {
				name string
				text string
			}{
				{name: "Named", text: "type Item int\n"},
				{name: "Alias", text: "type Item = int\n"},
			} {
				t.Run(declaration.name, func(t *testing.T) {
					s := newTestServer(t, map[string][]byte{
						"main.xgo": []byte(tt.prefix + declaration.text + "var value Item\necho value\n" + tt.suffix),
					})
					_, err := s.requestProject().TypeInfo()
					require.NoError(t, err)
					line := uint32(strings.Count(tt.prefix, "\n"))
					declRange := Range{Start: Position{Line: line, Character: 5}, End: Position{Line: line, Character: 9}}
					useRange := Range{Start: Position{Line: line + 1, Character: 10}, End: Position{Line: line + 1, Character: 14}}
					uri := DocumentURI("file:///main.xgo")
					for _, span := range []Range{declRange, useRange} {
						params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: span.Start}
						def, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
						require.NoError(t, err)
						assert.Equal(t, Location{URI: uri, Range: declRange}, def)
						typ, err := s.textDocumentTypeDefinition(&TypeDefinitionParams{TextDocumentPositionParams: params})
						require.NoError(t, err)
						assert.Equal(t, Location{URI: uri, Range: Range{Start: declRange.Start, End: declRange.Start}}, typ)
						impl, err := s.textDocumentImplementation(&ImplementationParams{TextDocumentPositionParams: params})
						require.NoError(t, err)
						assert.Equal(t, typ, impl)
						for _, includeDeclaration := range []bool{false, true} {
							refs, err := s.textDocumentReferences(&ReferenceParams{
								TextDocumentPositionParams: params, Context: ReferenceContext{IncludeDeclaration: includeDeclaration},
							})
							require.NoError(t, err)
							want := []Location{{URI: uri, Range: useRange}}
							if includeDeclaration {
								want = append(want, Location{URI: uri, Range: declRange})
							}
							assert.ElementsMatch(t, want, refs)
						}
						highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: params})
						require.NoError(t, err)
						require.NotNil(t, highlights)
						assert.ElementsMatch(t, []DocumentHighlight{{Range: declRange, Kind: Write}, {Range: useRange, Kind: Text}}, *highlights)
						prepared, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: params})
						require.NoError(t, err)
						assert.Equal(t, &span, prepared)
						edit, err := s.textDocumentRename(&RenameParams{TextDocument: params.TextDocument, Position: params.Position, NewName: "Entry"})
						require.NoError(t, err)
						require.NotNil(t, edit)
						require.Len(t, edit.Changes, 1)
						assert.ElementsMatch(t, []TextEdit{{Range: declRange, NewText: "Entry"}, {Range: useRange, NewText: "Entry"}}, edit.Changes[uri])
					}
				})
			}
		})
	}
}

func TestServerTypeSwitchNavigation(t *testing.T) {
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
			const source = `func use(input any) {
switch value := input.(type) {
case int: echo value
case string: echo value
case bool, float64: echo value
default: echo value
}
var value = "unrelated"
echo value
{
switch value := input.(type) {
case int: echo value
}
}
}
`
			files := map[string][]byte{kind.filename: []byte(source)}
			if kind.name == "WorkClass" {
				files["main_fixture.gox"] = nil
			}
			s := kind.newServer(t, files)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			uri := s.toDocumentURI(kind.filename)
			spans := []Range{
				{Start: Position{Line: 1, Character: 7}, End: Position{Line: 1, Character: 12}},
				{Start: Position{Line: 2, Character: 15}, End: Position{Line: 2, Character: 20}},
				{Start: Position{Line: 3, Character: 18}, End: Position{Line: 3, Character: 23}},
				{Start: Position{Line: 4, Character: 25}, End: Position{Line: 4, Character: 30}},
				{Start: Position{Line: 5, Character: 14}, End: Position{Line: 5, Character: 19}},
			}
			var wantRefs []Location
			var wantEdits []TextEdit
			var wantHighlights []DocumentHighlight
			for i, span := range spans {
				kind := Read
				if i == 0 {
					kind = Write
				}
				wantRefs = append(wantRefs, Location{URI: uri, Range: span})
				wantEdits = append(wantEdits, TextEdit{Range: span, NewText: "item"})
				wantHighlights = append(wantHighlights, DocumentHighlight{Range: span, Kind: kind})
			}
			for i, span := range spans {
				params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: span.Start}
				definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
				require.NoError(t, err)
				assert.Equal(t, wantRefs[0], definition)
				for _, includeDeclaration := range []bool{false, true} {
					refs, err := s.textDocumentReferences(&ReferenceParams{
						TextDocumentPositionParams: params, Context: ReferenceContext{IncludeDeclaration: includeDeclaration},
					})
					require.NoError(t, err)
					want := wantRefs[1:]
					if includeDeclaration {
						want = wantRefs
					}
					assert.ElementsMatch(t, want, refs)
				}
				highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: params})
				require.NoError(t, err)
				require.NotNil(t, highlights)
				assert.ElementsMatch(t, wantHighlights, *highlights)
				prepared, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: params})
				require.NoError(t, err)
				assert.Equal(t, &span, prepared)
				edit, err := s.textDocumentRename(&RenameParams{TextDocument: params.TextDocument, Position: params.Position, NewName: "item"})
				require.NoError(t, err)
				assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{uri: wantEdits})
				if i == 1 || i == 2 {
					hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
					require.NoError(t, err)
					require.NotNil(t, hover)
					typeName := "int"
					if i == 2 {
						typeName = "string"
					}
					assert.Contains(t, hover.Contents.Value, "var value "+typeName)
				}
			}
			tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{TextDocument: TextDocumentIdentifier{URI: uri}})
			require.NoError(t, err)
			require.NotNil(t, tokens)
			assertSemanticTokenModifierMask(t, tokens.Data, decodedSemanticToken{
				line: 1, character: 7, length: 5, tokenType: VariableType,
			}, getSemanticTokenModifiersMask([]SemanticTokenModifiers{ModDeclaration}))
			updated := applyResourceRenameTestEdits(t, source, wantEdits)
			s.ModifyFiles([]FileChange{{Path: kind.filename, Content: []byte(updated), Version: 1}})
			_, err = s.requestProject().TypeInfo()
			assert.NoError(t, err)
		})
	}
}

func TestServerRangeInputSlots(t *testing.T) {
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
				name string
				loop string
			}{
				{name: "Bound", loop: "for value = range 0:len(values)"},
				{name: "Step", loop: "for value = range 0:5:len(values)"},
				{name: "BoundAndStep", loop: "for value = range 0:len(values):len(values)"},
				{name: "ForPhrase", loop: "for value <- 0:len(values)"},
			} {
				t.Run(tt.name, func(t *testing.T) {
					const prefix = "func run() {\nvalues := [1, 2]\nvar value int\n"
					source := prefix + tt.loop + " {\necho 42\n}\n}\n"
					files := map[string][]byte{kind.filename: []byte(source)}
					if kind.filename == "Worker_fixture.gox" {
						files["main_fixture.gox"] = nil
					}
					s := kind.newServer(t, files)
					document := TextDocumentIdentifier{URI: s.toDocumentURI(kind.filename)}
					for version := range 2 {
						line := uint32(4 + version)
						if version > 0 {
							source = strings.Replace(source, "echo 42", "var _xgo_k, _xgo_end, _xgo_step int\necho 42", 1)
							s.ModifyFiles([]FileChange{{Path: kind.filename, Content: []byte(source), Version: version}})
						}
						_, err := s.requestProject().TypeInfo()
						require.NoError(t, err)
						slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: document}})
						require.NoError(t, err)
						for _, slot := range slots {
							if slot.Input.Name != "" {
								start := PositionOffset([]byte(source), slot.Range.Start)
								end := PositionOffset([]byte(source), slot.Range.End)
								assert.Equal(t, slot.Input.Name, source[start:end])
							}
						}
						slot := findInputSlot(slots, int64(42), "", XGoInputTypeInteger, XGoInputKindInPlace)
						require.NotNil(t, slot)
						assert.Equal(t, Range{Start: Position{Line: line, Character: 5}, End: Position{Line: line, Character: 7}}, slot.Range)
						assert.Contains(t, slot.PredefinedNames, "value")
						items := completionItemsAt(t, s, kind.filename, slot.Range.Start)
						labels := completionItemLabels(items)
						assert.Contains(t, labels, "value")
						for _, name := range []string{"_xgo_k", "_xgo_end", "_xgo_step"} {
							if version == 0 {
								assert.NotContains(t, labels, name)
								for _, slot := range slots {
									assert.NotContains(t, slot.PredefinedNames, name)
								}
							} else {
								assert.Contains(t, labels, name)
								assert.Contains(t, slot.PredefinedNames, name)
							}
						}
					}
				})
			}
		})
	}
}

func TestServerClassReceiverSourcePositions(t *testing.T) {
	for _, kind := range []struct {
		name     string
		filename string
	}{
		{name: "NormalClass", filename: "Record.gox"},
		{name: "ProjectClass", filename: "main_fixture.gox"},
		{name: "WorkClass", filename: "Worker_fixture.gox"},
	} {
		t.Run(kind.name, func(t *testing.T) {
			for _, marked := range []string{
				"var value int\nfunc run() { println |this.value, |this.value }\n",
				"|this = |this\n",
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
				uri := s.toDocumentURI(kind.filename)
				var wantReferences []Location
				var wantHighlights []DocumentHighlight
				prefix := ""
				for i, part := range parts[:len(parts)-1] {
					prefix += part
					position := Position{Line: uint32(strings.Count(prefix, "\n")), Character: uint32(UTF16Len(prefix[strings.LastIndexByte(prefix, '\n')+1:]))}
					end := position
					end.Character += 4
					rng := Range{Start: position, End: end}
					wantReferences = append(wantReferences, Location{URI: uri, Range: rng})
					kind := Read
					if strings.HasPrefix(marked, "|this =") && i == 0 {
						kind = Write
					}
					wantHighlights = append(wantHighlights, DocumentHighlight{Range: rng, Kind: kind})
				}
				for _, target := range wantReferences {
					params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: target.Range.Start}
					highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: params})
					require.NoError(t, err)
					require.NotNil(t, highlights)
					assert.ElementsMatch(t, wantHighlights, *highlights)
					for _, includeDeclaration := range []bool{false, true} {
						references, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: params, Context: ReferenceContext{IncludeDeclaration: includeDeclaration}})
						require.NoError(t, err)
						assert.ElementsMatch(t, wantReferences, references)
					}
					definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
					require.NoError(t, err)
					assert.Nil(t, definition)
				}
			}
		})
	}
}

func TestServerExplicitThisSourcePositions(t *testing.T) {
	for _, marked := range []string{
		"|this := 1\nprintln this\n",
		"type Value int\nfunc (|this Value) use() { println this }\n",
	} {
		source, position := typeDisplayTestSource(t, marked)
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		_, err := s.requestProject().TypeInfo()
		require.NoError(t, err)
		params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position}
		end := position
		end.Character += 4
		want := Location{URI: "file:///main.xgo", Range: Range{Start: position, End: end}}
		definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
		require.NoError(t, err)
		assert.Equal(t, want, definition)
		references, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: params, Context: ReferenceContext{IncludeDeclaration: true}})
		require.NoError(t, err)
		assert.Len(t, references, 2)
		assert.Contains(t, references, want)
		prepared, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: params})
		require.NoError(t, err)
		assert.Equal(t, &want.Range, prepared)
	}
}
