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

func semanticTokensClientCapabilitiesForTest() protocol.SemanticTokensClientCapabilities {
	return protocol.SemanticTokensClientCapabilities{
		Requests: protocol.ClientSemanticTokensRequestOptions{
			Full: &protocol.Or_ClientSemanticTokensRequestOptions_full{Value: true},
		},
		TokenTypes:              semanticTokenTypesLegendStrings(),
		TokenModifiers:          semanticTokenModifiersLegendStrings(),
		Formats:                 []protocol.TokenFormat{protocol.Relative},
		OverlappingTokenSupport: true,
		MultilineTokenSupport:   true,
	}
}

func stringsAsAny(values []string) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
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
						Completion: protocol.CompletionClientCapabilities{
							CompletionItem: protocol.ClientCompletionItemOptions{
								SnippetSupport:      true,
								DocumentationFormat: []protocol.MarkupKind{protocol.Markdown},
							},
						},
						Hover: &protocol.HoverClientCapabilities{
							ContentFormat: []protocol.MarkupKind{protocol.Markdown},
						},
						Rename: &protocol.RenameClientCapabilities{
							PrepareSupport: true,
						},
						SignatureHelp: &protocol.SignatureHelpClientCapabilities{
							ContextSupport: true,
						},
						SemanticTokens: semanticTokensClientCapabilitiesForTest(),
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
		completionClientCapabilities, ok := server.completionClientCapabilities()
		require.True(t, ok)
		assert.True(t, completionClientCapabilities.CompletionItem.SnippetSupport)
		assert.Equal(t, []MarkupKind{Markdown}, completionClientCapabilities.CompletionItem.DocumentationFormat)
		hoverClientCapabilities, ok := server.hoverClientCapabilities()
		require.True(t, ok)
		assert.Equal(t, []MarkupKind{Markdown}, hoverClientCapabilities.ContentFormat)

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

		assert.Equal(t, map[string]any{}, capabilities["inlayHintProvider"])

		semanticTokensProvider := requireValueAs[map[string]any](t, capabilities["semanticTokensProvider"])
		legend := requireValueAs[map[string]any](t, semanticTokensProvider["legend"])
		assert.Equal(t, stringsAsAny(semanticTokenTypesLegendStrings()), legend["tokenTypes"])
		assert.Equal(t, stringsAsAny(semanticTokenModifiersLegendStrings()), legend["tokenModifiers"])
		assert.Equal(t, true, semanticTokensProvider["full"])
		assert.NotContains(t, semanticTokensProvider, "range")

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

	t.Run("SemanticTokensCapabilityNegotiation", func(t *testing.T) {
		for _, tt := range []struct {
			name   string
			mutate func(*protocol.SemanticTokensClientCapabilities)
			want   bool
		}{
			{
				name: "Supported",
				want: true,
			},
			{
				name: "FullMissing",
				mutate: func(capabilities *protocol.SemanticTokensClientCapabilities) {
					capabilities.Requests.Full = nil
				},
			},
			{
				name: "FullFalse",
				mutate: func(capabilities *protocol.SemanticTokensClientCapabilities) {
					capabilities.Requests.Full.Value = false
				},
			},
			{
				name: "RelativeFormatMissing",
				mutate: func(capabilities *protocol.SemanticTokensClientCapabilities) {
					capabilities.Formats = nil
				},
			},
			{
				name: "TokenTypeMissing",
				mutate: func(capabilities *protocol.SemanticTokensClientCapabilities) {
					capabilities.TokenTypes = capabilities.TokenTypes[:len(capabilities.TokenTypes)-1]
				},
			},
			{
				name: "TokenModifierMissing",
				mutate: func(capabilities *protocol.SemanticTokensClientCapabilities) {
					capabilities.TokenModifiers = capabilities.TokenModifiers[:len(capabilities.TokenModifiers)-1]
				},
			},
			{
				name: "OverlappingTokenUnsupported",
				mutate: func(capabilities *protocol.SemanticTokensClientCapabilities) {
					capabilities.OverlappingTokenSupport = false
				},
			},
			{
				name: "MultilineTokenUnsupported",
				mutate: func(capabilities *protocol.SemanticTokensClientCapabilities) {
					capabilities.MultilineTokenSupport = false
				},
			},
			{
				name: "FullDeltaRequestSupported",
				mutate: func(capabilities *protocol.SemanticTokensClientCapabilities) {
					capabilities.Requests.Full.Value = protocol.ClientSemanticTokensRequestFullDelta{Delta: true}
				},
				want: true,
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				semanticTokens := semanticTokensClientCapabilitiesForTest()
				if tt.mutate != nil {
					tt.mutate(&semanticTokens)
				}
				capabilities := serverCapabilities(&InitializeParams{
					XInitializeParams: protocol.XInitializeParams{
						Capabilities: protocol.ClientCapabilities{
							TextDocument: protocol.TextDocumentClientCapabilities{
								SemanticTokens: semanticTokens,
							},
						},
					},
				})
				capabilityMap := requireCapabilityMap(t, capabilities)
				if tt.want {
					assert.Contains(t, capabilityMap, "semanticTokensProvider")
				} else {
					assert.NotContains(t, capabilityMap, "semanticTokensProvider")
				}
			})
		}
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

	t.Run("RootPathFallback", func(t *testing.T) {
		for _, tt := range []struct {
			name        string
			rootPath    string
			wantRootURI DocumentURI
			documentURI DocumentURI
		}{
			{
				name:        "UnixPath",
				rootPath:    "/legacy-root",
				wantRootURI: "file:///legacy-root/",
				documentURI: "file:///legacy-root/main.spx",
			},
			{
				name:        "WindowsDrivePath",
				rootPath:    `C:\Users\me\proj`,
				wantRootURI: "file:///C:/Users/me/proj/",
				documentURI: "file:///C:/Users/me/proj/main.spx",
			},
			{
				name:        "WindowsUNCPath",
				rootPath:    `\\server\share\proj`,
				wantRootURI: "file://server/share/proj/",
				documentURI: "file://server/share/proj/main.spx",
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				replier := newMockReplier()
				server := New(newProjectWithoutModTime(nil), replier, fileMapGetter(nil), &MockScheduler{})
				call, err := jsonrpc2.NewCall(jsonrpc2.NewStringID("initialize"), "initialize", InitializeParams{
					XInitializeParams: protocol.XInitializeParams{
						RootPath: tt.rootPath,
					},
				})
				require.NoError(t, err)
				require.NoError(t, server.HandleMessage(call))
				messages := replier.waitForMessages(2, 5*time.Second)
				require.NoError(t, requireResponseForID(t, messages, call.ID()).Err())
				assert.Equal(t, tt.wantRootURI, server.workspaceRootURI)

				path, err := server.fromDocumentURI(tt.documentURI)
				require.NoError(t, err)
				assert.Equal(t, "main.spx", path)
			})
		}
	})
}
