package server

import (
	"testing"
	"testing/synctest"

	"github.com/goplus/xgolsw/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerDocumentURI(t *testing.T) {
	for _, tt := range []struct {
		name    string
		path    string
		escaped string
	}{
		{name: "Spaces", path: "nested/a b.xgo", escaped: "nested/a%20b.xgo"},
		{name: "Percent", path: "literal%2F.xgo", escaped: "literal%252F.xgo"},
		{name: "Fragment", path: "a#b.xgo", escaped: "a%23b.xgo"},
		{name: "Query", path: "a?b.xgo", escaped: "a%3Fb.xgo"},
		{name: "Plus", path: "a+b.xgo", escaped: "a+b.xgo"},
		{name: "Unicode", path: "\u503c.xgo", escaped: "%E5%80%BC.xgo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const original = "var value int\nprintln value\n"
			const updated = "var replacement int\nprintln replacement\n"
			s := newTestServer(t, map[string][]byte{tt.path: []byte(original)})
			s.workspaceRootURI = "file:///work%20space/"
			s.replier = newMockReplier()
			uri := DocumentURI("file:///work%20space/" + tt.escaped)
			assert.Equal(t, uri, s.toDocumentURI(tt.path))
			path, err := s.fromDocumentURI(uri)
			require.NoError(t, err)
			assert.Equal(t, tt.path, path)
			params := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: uri}, Position: Position{Line: 1, Character: 9}}
			definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			assert.Equal(t, Location{URI: uri, Range: Range{Start: Position{Character: 4}, End: Position{Character: 9}}}, definition)
			synctest.Test(t, func(t *testing.T) {
				require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: uri, Version: 1, Text: updated}}))
				synctest.Wait()
				file, ok := s.requestProject().File(tt.path)
				require.True(t, ok)
				assert.Equal(t, updated, string(file.Content))
				pkg, err := s.requestProject().ASTPackage()
				require.NoError(t, err)
				assert.Len(t, pkg.Files, 1)
				require.NoError(t, s.didClose(&DidCloseTextDocumentParams{TextDocument: params.TextDocument}))
				synctest.Wait()
				file, ok = s.requestProject().File(tt.path)
				require.True(t, ok)
				assert.Equal(t, original, string(file.Content))
			})
		})
	}
}

func TestServerFromDocumentURI(t *testing.T) {
	for _, tt := range []struct {
		name      string
		uri       DocumentURI
		wantError string
	}{
		{name: "OutsideWorkspace", uri: "https://example.com/main.xgo", wantError: "workspace root URI"},
		{name: "TruncatedEscape", uri: "file:///main%.xgo", wantError: "invalid URL escape"},
		{name: "InvalidEscape", uri: "file:///main%zz.xgo", wantError: "invalid URL escape"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, nil)
			path, err := s.fromDocumentURI(tt.uri)
			assert.ErrorContains(t, err, tt.wantError)
			assert.Empty(t, path)
			report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: tt.uri}})
			assert.ErrorContains(t, err, tt.wantError)
			assert.Nil(t, report)
		})
	}
}

func TestServerDocumentURICrossFileEdits(t *testing.T) {
	const declaration = "var value int\n"
	const use = "println value\n"
	s := newTestServer(t, map[string][]byte{
		"declaration #.xgo": []byte(declaration),
		"use %.xgo":         []byte(use),
	})
	s.workspaceRootURI = "file:///work%20space/"
	const declarationURI DocumentURI = "file:///work%20space/declaration%20%23.xgo"
	const useURI DocumentURI = "file:///work%20space/use%20%25.xgo"
	params := TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: useURI},
		Position:     Position{Character: 9},
	}
	definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
	require.NoError(t, err)
	assert.Equal(t, Location{URI: declarationURI, Range: Range{Start: Position{Character: 4}, End: Position{Character: 9}}}, definition)
	edit, err := s.textDocumentRename(&RenameParams{TextDocument: params.TextDocument, Position: params.Position, NewName: "result"})
	require.NoError(t, err)
	require.NotNil(t, edit)
	require.Len(t, edit.Changes, 2)
	updatedDeclaration := applyResourceRenameTestEdits(t, declaration, edit.Changes[declarationURI])
	updatedUse := applyResourceRenameTestEdits(t, use, edit.Changes[useURI])
	assert.Equal(t, "var result int\n", updatedDeclaration)
	assert.Equal(t, "println result\n", updatedUse)
	s.ModifyFiles([]FileChange{
		{Path: "declaration #.xgo", Content: []byte(updatedDeclaration), Version: 1},
		{Path: "use %.xgo", Content: []byte(updatedUse), Version: 1},
	})
	_, err = s.requestProject().TypeInfo()
	assert.NoError(t, err)
}

func TestServerDocumentURIDiagnostics(t *testing.T) {
	for _, tt := range []struct {
		name string
		path string
		uri  DocumentURI
	}{
		{name: "EscapedLetter", path: "main.xgo", uri: "file:///ma%69n.xgo"},
		{name: "LowercaseEscape", path: "\u0394.xgo", uri: "file:///%ce%94.xgo"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const source = "println missing\n"
			s := newTestServer(t, map[string][]byte{tt.path: []byte(source)})
			replier := newMockReplier()
			s.replier = replier
			canonical := s.toDocumentURI(tt.path)
			want, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: canonical}})
			require.NoError(t, err)
			require.NotEmpty(t, requireValueAs[RelatedFullDocumentDiagnosticReport](t, want.Value).Items)
			got, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: tt.uri}})
			require.NoError(t, err)
			assert.Equal(t, want, got)
			synctest.Test(t, func(t *testing.T) {
				require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{TextDocument: protocol.TextDocumentItem{URI: tt.uri, Version: 1, Text: source}}))
				synctest.Wait()
				assert.NotEmpty(t, requirePublishedDiagnostics(t, replier, canonical))
				require.NoError(t, s.didChange(&DidChangeTextDocumentParams{
					TextDocument:   protocol.VersionedTextDocumentIdentifier{TextDocumentIdentifier: TextDocumentIdentifier{URI: tt.uri}, Version: 2},
					ContentChanges: []protocol.TextDocumentContentChangeEvent{{Text: "println 1\n"}},
				}))
				synctest.Wait()
				assert.Empty(t, requirePublishedDiagnostics(t, replier, canonical))
				require.NoError(t, s.didSave(&DidSaveTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: tt.uri}, Text: ToPtr(source)}))
				synctest.Wait()
				assert.NotEmpty(t, requirePublishedDiagnostics(t, replier, canonical))
				require.NoError(t, s.didClose(&DidCloseTextDocumentParams{TextDocument: TextDocumentIdentifier{URI: tt.uri}}))
				synctest.Wait()
				assert.Empty(t, requirePublishedDiagnostics(t, replier, canonical))
			})
		})
	}
}
