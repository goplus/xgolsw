package server

import (
	"strings"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerSpxProjectResources(t *testing.T) {
	t.Run("ExplicitThisWithEscapedResourceName", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx":                         nil,
			"Runner.spx":                       []byte("this.setCostume \"idle/front\"\nthis.setCostume \"idle\"\n"),
			"assets/index.json":                []byte(`{}`),
			"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"idle/front"},{"name":"idle"}]}`),
		})
		requireNoDiagnostics(t, s)
		id := TextDocumentIdentifier{URI: "file:///Runner.spx"}
		const resourceURI XGoResourceURI = "spx://resources/sprites/Runner/costumes/idle%2Ffront"
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: id})
		require.NoError(t, err)
		assert.Contains(t, documentLinkTargets(t, links), string(resourceURI))
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: id}})
		require.NoError(t, err)
		assert.NotNil(t, findInputSlot(slots, resourceURI, "", XGoInputTypeResourceName, XGoInputKindInPlace))
		edit, err := s.renameResources([]XGoRenameResourceParams{{
			Resource: XGoResourceIdentifier{URI: resourceURI}, NewName: "rest",
		}})
		require.NoError(t, err)
		assertRenameChanges(t, edit, map[DocumentURI][]TextEdit{
			id.URI: {{Range: Range{Start: Position{Character: 17}, End: Position{Character: 27}}, NewText: "rest"}},
		})
	})

	t.Run("XGoStrings", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx":                   []byte("var choice = \"Known\"\nplay \"${choice}\"\nplay \"$$\"\n"),
			"assets/index.json":          []byte(`{}`),
			"assets/sounds/$/index.json": []byte(`{}`),
		})
		requireNoDiagnostics(t, s)
		id := TextDocumentIdentifier{URI: "file:///main.spx"}
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: id})
		require.NoError(t, err)
		assert.Contains(t, documentLinkTargets(t, links), "spx://resources/sounds/$")
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: id}})
		require.NoError(t, err)
		assert.NotNil(t, findInputSlot(slots, XGoResourceURI("spx://resources/sounds/$"), "", XGoInputTypeResourceName, XGoInputKindInPlace))
		assert.Nil(t, findInputSlot(slots, "${choice}", "", XGoInputTypeString, XGoInputKindInPlace))
		edits, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{
			URI: (SpxSoundResourceID{SoundName: "${choice}"}).URI(),
		}, NewName: "Renamed"}})
		require.NoError(t, err)
		assert.Empty(t, edits.Changes)
	})

	for _, tt := range []struct {
		name, filename, prefix string
		withoutProject         bool
	}{
		{name: "ProjectClass", filename: "main.spx"},
		{name: "WorkClass", filename: "Runner.spx"},
		{name: "WorkClassWithoutProject", filename: "Runner.spx", withoutProject: true},
		{name: "PlainXGo", filename: "helper.xgo", prefix: "import \"github.com/goplus/spx/v3\"\nfunc play(sound spx.SoundName) {}\n"},
		{name: "PlainXGoAlias", filename: "helper.xgo", prefix: "import \"github.com/goplus/spx/v3\"\ntype Sound = spx.SoundName\ntype Alias = Sound\nfunc play(sound Alias) {}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.prefix+"play \"K|nown\"\nplay \"Missing\"\n")
			files := map[string][]byte{
				tt.filename:                      []byte(source),
				"assets/index.json":              []byte(`{}`),
				"assets/sounds/Known/index.json": []byte(`{}`),
			}
			if tt.filename != "main.spx" && !tt.withoutProject {
				files["main.spx"] = nil
			}
			s := newSpxTestServer(t, files)
			if tt.withoutProject {
				// Embedded work classes require fields in an explicit project class.
				s.getProj().SetModule(newTestModule(t, modload.Module{Opt: &modfile.File{Projects: []*modfile.Project{{
					Ext: ".spx", FullExt: "main.spx", Class: "Game", PkgPaths: []string{SpxPkgPath},
					Works: []*modfile.Class{{Ext: ".spx", Class: "SpriteImpl"}},
				}}}}))
			}
			_, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			id := TextDocumentIdentifier{URI: s.toDocumentURI(tt.filename)}
			span := Range{Start: Position{Line: position.Line, Character: 5}, End: Position{Line: position.Line, Character: 12}}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: id, Position: position}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Equal(t, span, hover.Range)
			assert.Equal(t, resourceMarkupContent("spx://resources/sounds/Known", Markdown), hover.Contents)
			links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: id})
			require.NoError(t, err)
			assert.Contains(t, links, DocumentLink{Range: span, Target: toURI("spx://resources/sounds/Known"), Data: XGoResourceRefDocumentLinkData{Kind: XGoResourceRefKindStringLiteral}})
			assert.NotContains(t, documentLinkTargets(t, links), "spx://resources/sounds/Missing")
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: id}})
			require.NoError(t, err)
			slot := findInputSlot(slots, XGoResourceURI("spx://resources/sounds/Known"), "", XGoInputTypeResourceName, XGoInputKindInPlace)
			require.NotNil(t, slot)
			assert.Equal(t, span, slot.Range)
			assert.Equal(t, ToPtr(SpxSoundResourceContextURI), slot.Accept.ResourceContext)
			assert.Contains(t, completionItemLabels(completionItemsAt(t, s, tt.filename, position)), "Known")
			report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: id})
			require.NoError(t, err)
			assert.Equal(t, []Diagnostic{{
				Severity: SeverityError, Message: `sound resource "Missing" not found`,
				Range: Range{Start: Position{Line: position.Line + 1, Character: 5}, End: Position{Line: position.Line + 1, Character: 14}},
			}}, requireRelatedFullDocumentDiagnosticReport(t, report).Items)
			edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/Known"}, NewName: "Renamed"}})
			require.NoError(t, err)
			assert.Equal(t, map[DocumentURI][]TextEdit{id.URI: {{
				Range: Range{Start: Position{Line: position.Line, Character: 6}, End: Position{Line: position.Line, Character: 11}}, NewText: "Renamed",
			}}}, edit.Changes)
		})
	}

	t.Run("OrdinaryString", func(t *testing.T) {
		source, position := typeDisplayTestSource(t, "type SoundName = string\nfunc play(sound SoundName) {}\nplay \"K|nown\"\n")
		s := newSpxTestServer(t, map[string][]byte{
			"main.spx": nil, "helper.xgo": []byte(source),
			"assets/index.json": []byte(`{}`), "assets/sounds/Known/index.json": []byte(`{}`),
		})
		requireNoDiagnostics(t, s)
		id := TextDocumentIdentifier{URI: "file:///helper.xgo"}
		links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: id})
		require.NoError(t, err)
		assert.NotContains(t, documentLinkTargets(t, links), "spx://resources/sounds/Known")
		hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: id, Position: position}})
		require.NoError(t, err)
		assert.Nil(t, hover)
		slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: id}})
		require.NoError(t, err)
		assert.NotNil(t, findInputSlot(slots, "Known", "", XGoInputTypeString, XGoInputKindInPlace))
		assert.NotContains(t, completionItemLabels(completionItemsAt(t, s, "helper.xgo", position)), "Known")
		edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/Known"}, NewName: "Renamed"}})
		require.NoError(t, err)
		assert.Empty(t, edit.Changes)
	})
}

func TestServerSpxProjectWithoutMainDiagnostics(t *testing.T) {
	s := newSpxTestServer(t, map[string][]byte{
		"Worker.spx":        []byte("println missing\nvalues := [1, 2]\nvalues = append(values)\n"),
		"assets/index.json": []byte(`{}`),
	})
	s.getProj().SetModule(newTestModule(t, modload.Module{Opt: &modfile.File{Projects: []*modfile.Project{{
		Ext: ".spx", FullExt: "main.spx", Class: "Game", PkgPaths: []string{SpxPkgPath},
		Works: []*modfile.Class{{Ext: ".spx", Class: "SpriteImpl"}},
	}}}}))
	report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: "file:///Worker.spx"}})
	require.NoError(t, err)
	want := []Diagnostic{
		{Severity: SeverityError, Message: "undefined: missing", Range: Range{Start: Position{Character: 8}, End: Position{Character: 15}}},
		{Severity: SeverityError, Message: "append with no values", Range: Range{Start: Position{Line: 2, Character: 9}, End: Position{Line: 2, Character: 23}}},
	}
	assert.Equal(t, want, requireRelatedFullDocumentDiagnosticReport(t, report).Items)
	workspaceReport, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
	require.NoError(t, err)
	require.Len(t, workspaceReport.Items, 1)
	full := requireWorkspaceFullDocumentDiagnosticReport(t, workspaceReport.Items[0])
	assert.Equal(t, DocumentURI("file:///Worker.spx"), full.URI)
	assert.Equal(t, want, full.Items)
}

func TestServerSpxProjectUnavailableMetadata(t *testing.T) {
	for _, tt := range []struct {
		name      string
		metadata  map[string][]byte
		wantError string
	}{
		{name: "MissingIndex", wantError: "failed to read metadata"},
		{name: "InvalidIndex", metadata: map[string][]byte{"assets/index.json": []byte(`{`)}, wantError: "failed to parse metadata"},
		{name: "InvalidSound", metadata: map[string][]byte{"assets/index.json": []byte(`{}`), "assets/sounds/Known/index.json": []byte(`{`)}, wantError: "failed to parse sound metadata"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, "var Count = 1\nprintln Cou|nt\nprintln missing\nvalues := [1, 2]\nvalues = append(values)\nplay \"Known\"\nplay \"\"\n")
			files := map[string][]byte{"main.spx": []byte(source), "assets/sounds/Known/index.json": []byte(`{}`)}
			for name, content := range tt.metadata {
				files[name] = content
			}
			s := newSpxTestServer(t, files)
			id := TextDocumentIdentifier{URI: "file:///main.spx"}
			report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: id})
			require.NoError(t, err)
			diagnostics := requireRelatedFullDocumentDiagnosticReport(t, report).Items
			require.Len(t, diagnostics, 4)
			var messages []string
			for _, diagnostic := range diagnostics {
				messages = append(messages, diagnostic.Message)
				assert.NotContains(t, diagnostic.Message, "not found")
			}
			assert.Contains(t, messages, "undefined: missing")
			assert.Contains(t, messages, "append with no values")
			assert.Contains(t, messages, "sound resource name cannot be empty")
			assert.Contains(t, strings.Join(messages, "\n"), tt.wantError)
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{TextDocument: id, Position: position}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, "Count")
			assert.Contains(t, completionItemLabels(completionItemsAt(t, s, "main.spx", position)), "Count")
			links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: id})
			require.NoError(t, err)
			assert.Contains(t, documentLinkTargets(t, links), "xgo:main?Game.Count")
			assert.NotContains(t, documentLinkTargets(t, links), "spx://resources/sounds/Known")
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: id}})
			require.NoError(t, err)
			assert.NotNil(t, findInputSlot(slots, int64(1), "", XGoInputTypeInteger, XGoInputKindInPlace))
			edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/Known"}, NewName: "Renamed"}})
			require.ErrorContains(t, err, tt.wantError)
			assert.Nil(t, edit)
		})
	}

	t.Run("WithoutProjectClass", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{"Worker.spx": []byte("println 1\n"), "assets/index.json": []byte(`{`)})
		report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: "file:///assets/index.json"}})
		require.NoError(t, err)
		diagnostics := requireRelatedFullDocumentDiagnosticReport(t, report).Items
		require.Len(t, diagnostics, 1)
		assert.Contains(t, diagnostics[0].Message, "failed to parse metadata")
	})
}

func TestServerSpxRenameResourcesUnavailableProject(t *testing.T) {
	s := newTestServer(t, map[string][]byte{"main.xgo": []byte("println 1\n")})
	edit, err := s.renameResources([]XGoRenameResourceParams{{Resource: XGoResourceIdentifier{URI: "spx://resources/sounds/Known"}, NewName: "Renamed"}})
	require.ErrorContains(t, err, "resource analysis is unavailable")
	assert.Nil(t, edit)
}
