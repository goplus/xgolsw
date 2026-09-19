package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerLineDirectives(t *testing.T) {
	const source = "//line virtual.xgo:100:20\nvar value = 1\nfunc show(text string, number int) {}\nshow \"\U0001f600\", value\n"
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
					return s.textDocumentInlayHint(&InlayHintParams{TextDocument: p.TextDocument, Range: Range{End: Position{Line: 4}}})
				}, true},
				{"InputSlots", func(s *Server, p TextDocumentPositionParams) (any, error) {
					return s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: p.TextDocument}})
				}, false},
			} {
				t.Run(feature.name, func(t *testing.T) {
					plainFiles := map[string][]byte{project.filename: []byte(strings.Replace(source, "//line virtual.xgo:100:20", "", 1))}
					directedFiles := map[string][]byte{project.filename: []byte(source)}
					if project.filename == "Worker_fixture.gox" {
						plainFiles["main_fixture.gox"] = nil
						directedFiles["main_fixture.gox"] = nil
					}
					plain := project.newServer(t, plainFiles)
					directed := project.newServer(t, directedFiles)
					params := TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: plain.toDocumentURI(project.filename)},
						Position:     Position{Line: 3, Character: 13},
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
