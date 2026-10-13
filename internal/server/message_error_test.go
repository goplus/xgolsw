package server

import (
	"encoding/json"
	"testing"

	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerHandleMessageInvalidCalls(t *testing.T) {
	for _, tt := range []struct{ name, method string }{
		{"Initialize", "initialize"},
		{"Hover", "textDocument/hover"},
		{"Completion", "textDocument/completion"},
		{"SignatureHelp", "textDocument/signatureHelp"},
		{"Declaration", "textDocument/declaration"},
		{"Definition", "textDocument/definition"},
		{"TypeDefinition", "textDocument/typeDefinition"},
		{"Implementation", "textDocument/implementation"},
		{"References", "textDocument/references"},
		{"DocumentHighlight", "textDocument/documentHighlight"},
		{"DocumentLink", "textDocument/documentLink"},
		{"DocumentDiagnostic", "textDocument/diagnostic"},
		{"WorkspaceDiagnostic", "workspace/diagnostic"},
		{"Formatting", "textDocument/formatting"},
		{"PrepareRename", "textDocument/prepareRename"},
		{"Rename", "textDocument/rename"},
		{"SemanticTokens", "textDocument/semanticTokens/full"},
		{"InlayHint", "textDocument/inlayHint"},
		{"ExecuteCommand", "workspace/executeCommand"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, nil)
			replier := newMockReplier()
			s.replier = replier
			if tt.method != "initialize" {
				initializeServerForTest(t, s, replier)
			}
			id := jsonrpc2.NewStringID("invalid-params")
			call, err := jsonrpc2.NewCall(id, tt.method, []any{})
			require.NoError(t, err)
			require.NoError(t, s.HandleMessage(call))
			messages := replier.getMessages()
			require.Len(t, messages, 1)
			response := requireResponseForID(t, messages, id)
			require.ErrorIs(t, response.Err(), jsonrpc2.ErrParse)

			encoded, err := json.Marshal(response)
			require.NoError(t, err)
			var decoded jsonrpc2.Response
			require.NoError(t, json.Unmarshal(encoded, &decoded))
			var wireErr *jsonrpc2.WireError
			require.ErrorAs(t, decoded.Err(), &wireErr)
			assert.Equal(t, int64(protocol.ParseError), wireErr.Code)
			assert.Equal(t, id, decoded.ID())
			if tt.method == "initialize" {
				assert.False(t, s.isInitialized())
				replier.clearMessages()
				initializeServerForTest(t, s, replier)
				assert.True(t, s.isInitialized())
			}
		})
	}
}

func TestServerHandleMessageInvalidNotifications(t *testing.T) {
	for _, tt := range []struct{ name, method, wantErr string }{
		{"CancelRequest", "$/cancelRequest", "failed to parse cancelRequest params"},
		{"Initialized", "initialized", "failed to parse initialized params"},
		{"DidOpen", "textDocument/didOpen", "failed to parse didOpen params"},
		{"DidChange", "textDocument/didChange", "failed to parse didChange params"},
		{"DidSave", "textDocument/didSave", "failed to parse didSave params"},
		{"DidClose", "textDocument/didClose", "failed to parse didClose params"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, nil)
			replier := newMockReplier()
			s.replier = replier
			initializeServerForTest(t, s, replier)
			notification, err := jsonrpc2.NewNotification(tt.method, []any{})
			require.NoError(t, err)
			require.ErrorContains(t, s.HandleMessage(notification), tt.wantErr)
			assert.Empty(t, replier.getMessages())
		})
	}
}

func TestServerHandleMessageUnsupportedMessages(t *testing.T) {
	response, err := jsonrpc2.NewResponse(jsonrpc2.NewIntID(1), nil, nil)
	require.NoError(t, err)
	for _, tt := range []struct {
		name    string
		message jsonrpc2.Message
	}{
		{name: "Nil"},
		{name: "Response", message: response},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, nil)
			replier := newMockReplier()
			s.replier = replier
			require.ErrorContains(t, s.HandleMessage(tt.message), "unsupported message type")
			assert.Empty(t, replier.getMessages())
		})
	}
}

func TestServerWorkspaceExecuteCommandInvalidArguments(t *testing.T) {
	for _, tt := range []struct {
		name      string
		command   string
		arguments []json.RawMessage
		wantErr   string
	}{
		{
			name: "RenameResources", command: CommandXGoRenameResources, arguments: []json.RawMessage{json.RawMessage(`[]`)},
			wantErr: "failed to unmarshal command argument as XGoRenameResourceParams",
		},
		{
			name: "InputSlots", command: CommandXGoGetInputSlots, arguments: []json.RawMessage{json.RawMessage(`[]`)},
			wantErr: "failed to unmarshal command argument as XGoGetInputSlotsParams",
		},
		{
			name: "Properties", command: CommandXGoGetProperties, arguments: []json.RawMessage{json.RawMessage(`[]`)},
			wantErr: "failed to unmarshal command argument as XGoGetPropertiesParams",
		},
		{
			name: "MissingPropertyArgument", command: CommandXGoGetProperties,
			wantErr: "expected exactly one argument",
		},
		{
			name: "ExtraPropertyArgument", command: CommandXGoGetProperties,
			arguments: []json.RawMessage{json.RawMessage(`{}`), json.RawMessage(`{}`)},
			wantErr:   "expected exactly one argument",
		},
		{
			name: "UnknownCommand", command: "unknown.command", wantErr: "unknown command: unknown.command",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, nil)
			result, err := s.workspaceExecuteCommand(&ExecuteCommandParams{Command: tt.command, Arguments: tt.arguments})
			require.ErrorContains(t, err, tt.wantErr)
			assert.Nil(t, result)
		})
	}
}
