package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func requireCapabilityMap(t *testing.T, capabilities ServerCapabilities) map[string]any {
	t.Helper()

	data, err := json.Marshal(capabilities)
	require.NoError(t, err)
	var capabilityMap map[string]any
	require.NoError(t, json.Unmarshal(data, &capabilityMap))
	return capabilityMap
}

func TestServerInitialize(t *testing.T) {
	t.Run("Capabilities", func(t *testing.T) {
		replier := newMockReplier()
		server := New(newProjectWithoutModTime(nil), replier, fileMapGetter(nil), &MockScheduler{})
		call, err := jsonrpc2.NewCall(jsonrpc2.NewStringID("initialize"), "initialize", InitializeParams{
			XInitializeParams: protocol.XInitializeParams{
				RootURI: "file:///workspace",
				Capabilities: protocol.ClientCapabilities{
					TextDocument: protocol.TextDocumentClientCapabilities{
						Rename: &protocol.RenameClientCapabilities{
							PrepareSupport: true,
						},
						SignatureHelp: &protocol.SignatureHelpClientCapabilities{
							ContextSupport: true,
						},
					},
				},
			},
		})
		require.NoError(t, err)
		require.NoError(t, server.HandleMessage(call))

		messages := replier.waitForMessages(2, 5*time.Second)
		response := requireResponseForID(t, messages, call.ID())
		require.NoError(t, response.Err())

		var result InitializeResult
		require.NoError(t, json.Unmarshal(response.Result(), &result))
		require.NotNil(t, result.ServerInfo)
		assert.Equal(t, "XGo Language Server", result.ServerInfo.Name)
		assert.Equal(t, "0.1.0", result.ServerInfo.Version)
		assert.Equal(t, DocumentURI("file:///workspace/"), server.workspaceRootURI)

		capabilities := requireCapabilityMap(t, result.Capabilities)
		assert.Equal(t, "utf-16", capabilities["positionEncoding"])
		assert.Contains(t, capabilities, "textDocumentSync")
		textDocumentSync := requireValueAs[map[string]any](t, capabilities["textDocumentSync"])
		assert.Equal(t, true, textDocumentSync["openClose"])
		assert.Equal(t, float64(protocol.Incremental), textDocumentSync["change"])
		assert.Contains(t, textDocumentSync, "save")

		completionProvider := requireValueAs[map[string]any](t, capabilities["completionProvider"])
		assert.Equal(t, []any{"."}, completionProvider["triggerCharacters"])
		assert.NotContains(t, completionProvider, "resolveProvider")
		assert.Equal(t, true, capabilities["hoverProvider"])

		signatureHelpProvider := requireValueAs[map[string]any](t, capabilities["signatureHelpProvider"])
		assert.Equal(t, []any{"(", ","}, signatureHelpProvider["triggerCharacters"])
		assert.Equal(t, []any{","}, signatureHelpProvider["retriggerCharacters"])
		assert.Equal(t, true, capabilities["declarationProvider"])
		assert.Equal(t, true, capabilities["definitionProvider"])
		assert.Equal(t, true, capabilities["typeDefinitionProvider"])
		assert.Equal(t, true, capabilities["implementationProvider"])
		assert.Equal(t, true, capabilities["referencesProvider"])
		assert.Equal(t, true, capabilities["documentHighlightProvider"])
		assert.Equal(t, map[string]any{}, capabilities["documentLinkProvider"])

		diagnosticProvider := requireValueAs[map[string]any](t, capabilities["diagnosticProvider"])
		assert.Equal(t, true, diagnosticProvider["interFileDependencies"])
		assert.Equal(t, true, diagnosticProvider["workspaceDiagnostics"])
		assert.Equal(t, true, capabilities["documentFormattingProvider"])

		renameProvider := requireValueAs[map[string]any](t, capabilities["renameProvider"])
		assert.Equal(t, true, renameProvider["prepareProvider"])

		executeCommandProvider := requireValueAs[map[string]any](t, capabilities["executeCommandProvider"])
		assert.ElementsMatch(t, []any{
			CommandXGoRenameResources,
			CommandSpxRenameResources,
			CommandXGoGetInputSlots,
			CommandSpxGetInputSlots,
			CommandXGoGetProperties,
		}, executeCommandProvider["commands"])

		semanticTokensProvider := requireValueAs[map[string]any](t, capabilities["semanticTokensProvider"])
		semanticTokensLegend := requireValueAs[map[string]any](t, semanticTokensProvider["legend"])
		assert.NotEmpty(t, semanticTokensLegend["tokenTypes"])
		assert.NotEmpty(t, semanticTokensLegend["tokenModifiers"])
		assert.Equal(t, true, semanticTokensProvider["full"])
		assert.NotContains(t, semanticTokensProvider, "range")
		assert.Equal(t, map[string]any{}, capabilities["inlayHintProvider"])

		assert.NotContains(t, capabilities, "documentSymbolProvider")
		assert.NotContains(t, capabilities, "workspaceSymbolProvider")
		assert.NotContains(t, capabilities, "codeActionProvider")
		assert.NotContains(t, capabilities, "codeLensProvider")
		assert.NotContains(t, capabilities, "documentRangeFormattingProvider")
		assert.NotContains(t, capabilities, "documentOnTypeFormattingProvider")
		assert.NotContains(t, capabilities, "foldingRangeProvider")
		assert.NotContains(t, capabilities, "selectionRangeProvider")
	})

	t.Run("RenameWithoutPrepareSupport", func(t *testing.T) {
		capabilities := serverCapabilities(&InitializeParams{})
		capabilityMap := requireCapabilityMap(t, capabilities)
		assert.Equal(t, true, capabilityMap["renameProvider"])
	})

	t.Run("SignatureHelpWithoutContextSupport", func(t *testing.T) {
		capabilities := serverCapabilities(&InitializeParams{})
		capabilityMap := requireCapabilityMap(t, capabilities)
		signatureHelpProvider := requireValueAs[map[string]any](t, capabilityMap["signatureHelpProvider"])
		assert.Equal(t, []any{"(", ","}, signatureHelpProvider["triggerCharacters"])
		assert.NotContains(t, signatureHelpProvider, "retriggerCharacters")
	})

	t.Run("WorkspaceFolderRoot", func(t *testing.T) {
		replier := newMockReplier()
		server := New(newProjectWithoutModTime(nil), replier, fileMapGetter(nil), &MockScheduler{})
		call, err := jsonrpc2.NewCall(jsonrpc2.NewStringID("initialize"), "initialize", InitializeParams{
			XInitializeParams: protocol.XInitializeParams{
				RootURI: "file:///root-uri",
			},
			WorkspaceFoldersInitializeParams: protocol.WorkspaceFoldersInitializeParams{
				WorkspaceFolders: []protocol.WorkspaceFolder{{
					URI:  "file:///workspace-folder",
					Name: "workspace-folder",
				}},
			},
		})
		require.NoError(t, err)
		require.NoError(t, server.HandleMessage(call))
		messages := replier.waitForMessages(2, 5*time.Second)
		require.NoError(t, requireResponseForID(t, messages, call.ID()).Err())
		assert.Equal(t, DocumentURI("file:///workspace-folder/"), server.workspaceRootURI)
	})
}
