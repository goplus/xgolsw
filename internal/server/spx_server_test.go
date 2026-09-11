//go:build !test_no_pkgdata

package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/goplus/xgolsw/internal"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSpx(t *testing.T) {
	files := map[string][]byte{"main.spx": []byte("var Count int\nCount = 1\n")}
	proj := xgo.NewProject(nil, newFileMap(files), xgo.FeatAll)
	s := New(proj, nil, fileMapGetter(files), &MockScheduler{})
	assert.Same(t, proj, s.getProj())
	assert.Equal(t, "main", proj.PkgPath)
	assert.Same(t, internal.Importer, proj.Importer)
	_, isProject, ok := proj.Mod.ClassInfo("main.spx")
	assert.True(t, isProject)
	assert.True(t, ok)
	_, err := proj.TypeInfo()
	require.NoError(t, err)
	hover, err := s.textDocumentHover(&HoverParams{
		TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
			Position:     Position{Line: 1},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, hover)
	assert.Contains(t, hover.Contents.Value, `def-id="xgo:main?Game.Count"`)

	pkgs, err := s.listPkgs()
	require.NoError(t, err)
	assert.Contains(t, pkgs, SpxPkgPath)
	doc, err := s.lookupPkgDoc(SpxPkgPath)
	require.NoError(t, err)
	wantDoc, err := pkgdata.GetPkgDoc(SpxPkgPath)
	require.NoError(t, err)
	assert.Same(t, wantDoc, doc)
}

func TestHandleMessageCallSpx(t *testing.T) {
	for _, command := range []struct {
		name    string
		command string
	}{
		{name: "RenameResources", command: CommandXGoRenameResources},
		{name: "LegacyRenameResources", command: CommandSpxRenameResources},
	} {
		t.Run(command.name, func(t *testing.T) {
			for _, tt := range []struct {
				name     string
				files    map[string][]byte
				argument json.RawMessage
				want     map[DocumentURI][]TextEdit
			}{
				{
					name:     "SoundReference",
					files:    map[string][]byte{"main.spx": []byte(`play "beep"`), "assets/index.json": []byte(`{}`)},
					argument: json.RawMessage(`{"resource":{"uri":"spx://resources/sounds/beep"},"newName":"click"}`),
					want: map[DocumentURI][]TextEdit{
						"file:///main.spx": {{Range: Range{Start: Position{Character: 6}, End: Position{Character: 10}}, NewText: "click"}},
					},
				},
				{
					name:     "SpriteWithoutAssets",
					files:    map[string][]byte{"main.spx": []byte(`var x = 100`)},
					argument: json.RawMessage(`{"resource":{"uri":"spx://resources/sprites/sprite1"},"newName":"sprite2"}`),
				},
			} {
				t.Run(tt.name, func(t *testing.T) {
					replier := newMockReplier()
					s := newSpxTestServer(t, tt.files)
					s.replier = replier
					initializeServerForTest(t, s, replier)
					call, err := jsonrpc2.NewCall(jsonrpc2.NewIntID(1), "workspace/executeCommand", ExecuteCommandParams{
						Command:   command.command,
						Arguments: []json.RawMessage{tt.argument},
					})
					require.NoError(t, err)
					require.NoError(t, s.HandleMessage(call))
					messages := replier.waitForMessages(2, 5*time.Second)
					require.Len(t, messages, 2)
					response := requireResponseForID(t, messages, call.ID())
					require.NoError(t, response.Err())
					var edit WorkspaceEdit
					require.NoError(t, json.Unmarshal(response.Result(), &edit))
					assert.Equal(t, tt.want, edit.Changes)
				})
			}
		})
	}
}
