package server

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/protocol"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockReplier struct {
	mu       sync.Mutex
	cond     *sync.Cond
	messages []jsonrpc2.Message
}

func newMockReplier() *mockReplier {
	m := &mockReplier{}
	m.cond = sync.NewCond(&m.mu)
	return m
}

func (m *mockReplier) ReplyMessage(msg jsonrpc2.Message) error {
	m.mu.Lock()
	m.messages = append(m.messages, msg)
	m.cond.Broadcast()
	m.mu.Unlock()
	return nil
}

func (m *mockReplier) getMessages() []jsonrpc2.Message {
	m.mu.Lock()
	result := make([]jsonrpc2.Message, len(m.messages))
	copy(result, m.messages)
	m.mu.Unlock()
	return result
}

func (m *mockReplier) waitForMessages(count int, timeout time.Duration) []jsonrpc2.Message {
	// For count=0, wait a short time to ensure no unexpected messages arrive
	if count == 0 {
		time.Sleep(10 * time.Millisecond)
		m.mu.Lock()
		result := make([]jsonrpc2.Message, len(m.messages))
		copy(result, m.messages)
		m.mu.Unlock()
		return result
	}

	m.mu.Lock()

	timedOut := false
	timer := time.AfterFunc(timeout, func() {
		m.mu.Lock()
		timedOut = true
		m.cond.Broadcast()
		m.mu.Unlock()
	})

	for len(m.messages) < count && !timedOut {
		m.cond.Wait()
	}

	result := make([]jsonrpc2.Message, len(m.messages))
	copy(result, m.messages)
	m.mu.Unlock()
	timer.Stop()
	return result
}

func newProjectWithoutModTime(files map[string][]byte) *xgo.Project {
	fileMap := make(map[string]*xgo.File)
	for k, v := range files {
		fileMap[k] = &xgo.File{Content: v}
	}
	return xgo.NewProject(nil, fileMap, xgo.FeatAll)
}

func requireValueAs[T any](t *testing.T, value any) T {
	t.Helper()

	typed, ok := value.(T)
	require.True(t, ok)
	return typed
}

func fileMapGetter(files map[string][]byte) func() map[string]*xgo.File {
	return func() map[string]*xgo.File {
		fileMap := make(map[string]*xgo.File)
		for k, v := range files {
			fileMap[k] = &xgo.File{Content: v}
		}
		return fileMap
	}
}

// MockScheduler implements [Scheduler]
type MockScheduler struct{}

func (s *MockScheduler) Sched() {
	time.Sleep(1 * time.Millisecond)
}

func TestServerCancellation(t *testing.T) {
	t.Run("CancelRequest", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx": []byte(`
var x = 100
echo x
`),
		}
		replier := newMockReplier()
		s := New(newProjectWithoutModTime(files), replier, fileMapGetter(files), &MockScheduler{})

		call1, _ := jsonrpc2.NewCall(jsonrpc2.NewStringID("test-request-1"), "$/cancelRequest", &CancelParams{ID: "test-request-1"})
		call2, _ := jsonrpc2.NewCall(jsonrpc2.NewStringID("test-request-2"), "$/cancelRequest", &CancelParams{ID: "test-request-2"})

		var request1Runned bool
		var request2Runned bool
		s.runForCall(call1, func() (any, error) {
			request1Runned = true
			return "should not reach here", nil
		})
		s.runForCall(call2, func() (any, error) {
			request2Runned = true
			return "should not reach here either", nil
		})

		err1 := s.cancelRequest(&CancelParams{ID: "test-request-1"})
		require.NoError(t, err1)
		err2 := s.cancelRequest(&CancelParams{ID: "test-request-2"})
		require.NoError(t, err2)

		messages := replier.waitForMessages(2, 5*time.Second)

		assert.False(t, request1Runned, "Function should not have been executed for cancelled request")
		assert.False(t, request2Runned, "Function should not have been executed for cancelled request")
		require.Len(t, messages, 2)

		var response1, response2 *jsonrpc2.Response
		require.Len(t, messages, 2, "Should receive two Response messages")
		for _, v := range messages {
			response, ok := v.(*jsonrpc2.Response)
			require.True(t, ok, "Should receive a Response message")
			if response.ID() == call1.ID() {
				response1 = response
			} else if response.ID() == call2.ID() {
				response2 = response
			}
		}

		assert.Equal(t, call1.ID(), response1.ID())
		assert.NotNil(t, response1.Err())
		var wireErr1 *jsonrpc2.WireError
		require.True(t, errors.As(response1.Err(), &wireErr1))
		assert.Equal(t, int64(RequestCancelled), wireErr1.Code)
		assert.Contains(t, wireErr1.Message, "Request cancelled")

		assert.Equal(t, call2.ID(), response2.ID())
		assert.NotNil(t, response2.Err())
		var wireErr2 *jsonrpc2.WireError
		require.True(t, errors.As(response2.Err(), &wireErr2))
		assert.Equal(t, int64(RequestCancelled), wireErr2.Code)
		assert.Contains(t, wireErr2.Message, "Request cancelled")
	})

	t.Run("CancelRequestWithInvalidID", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx": []byte(`var x = 100`),
		}
		replier := &mockReplier{}
		s := New(newProjectWithoutModTime(files), replier, fileMapGetter(files), &MockScheduler{})

		testCases := []struct {
			name string
			id   any
		}{
			{"InvalidType", []int{1, 2, 3}},
			{"EmptyMap", map[string]string{}},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				err := s.cancelRequest(&CancelParams{ID: tc.id})
				// Should return an error for invalid ID
				require.Error(t, err)
				assert.Contains(t, err.Error(), "cancelRequest:")
			})
		}
	})
}

func TestHandleMessageCall(t *testing.T) {
	testCases := []struct {
		name   string
		method string
		params any
		files  map[string][]byte
		msgNum int
	}{
		{
			name:   "MethodNotFound",
			method: "unknown/method",
			msgNum: 1,
		},
		{
			name:   "ShutDown",
			method: "shutdown",
			params: nil,
			msgNum: 2, // 1 response + 1 notification
		},
		{
			name:   "TextDocumentHover",
			method: "textDocument/hover",
			params: &HoverParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 2, Character: 1},
				},
			},
			files: map[string][]byte{
				"main.spx": []byte(`
import (
	"fmt"
	"image"
)

fmt.Println("Hello, World!")
`),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentCompletion",
			method: "textDocument/completion",
			params: CompletionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 1, Character: 5},
				},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentSignatureHelp",
			method: "textDocument/signatureHelp",
			params: SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 1, Character: 5},
				},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho(x)"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentDeclaration",
			method: "textDocument/declaration",
			params: DeclarationParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 1, Character: 5},
				},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentDefinition",
			method: "textDocument/definition",
			params: DefinitionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 1, Character: 5},
				},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentTypeDefinition",
			method: "textDocument/typeDefinition",
			params: TypeDefinitionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 1, Character: 5},
				},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentImplementation",
			method: "textDocument/implementation",
			params: ImplementationParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 1, Character: 5},
				},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentReferences",
			method: "textDocument/references",
			params: ReferenceParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 1, Character: 5},
				},
				Context: ReferenceContext{
					IncludeDeclaration: true,
				},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentDocumentHighlight",
			method: "textDocument/documentHighlight",
			params: DocumentHighlightParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 1, Character: 5},
				},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentDocumentLink",
			method: "textDocument/documentLink",
			params: DocumentLinkParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
			},
			files: map[string][]byte{
				"main.spx": []byte(`import "fmt"`),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentDiagnostic",
			method: "textDocument/diagnostic",
			params: DocumentDiagnosticParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "WorkspaceDiagnostic",
			method: "workspace/diagnostic",
			params: WorkspaceDiagnosticParams{},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentFormatting",
			method: "textDocument/formatting",
			params: DocumentFormattingParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x=100\necho   x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentPrepareRename",
			method: "textDocument/prepareRename",
			params: PrepareRenameParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
					Position:     Position{Line: 0, Character: 5},
				},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentRename",
			method: "textDocument/rename",
			params: RenameParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Position:     Position{Line: 0, Character: 5},
				NewName:      "y",
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentSemanticTokensFull",
			method: "textDocument/semanticTokens/full",
			params: SemanticTokensParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentInlayHint",
			method: "textDocument/inlayHint",
			params: InlayHintParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
				Range: Range{
					Start: Position{Line: 0, Character: 0},
					End:   Position{Line: 1, Character: 6},
				},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "WorkspaceExecuteCommand",
			method: "workspace/executeCommand",
			params: ExecuteCommandParams{
				Command: CommandXGoRenameResources,
				Arguments: func() []json.RawMessage {
					arg := map[string]any{
						"resource": map[string]any{
							"uri": "spx://resources/sprites/sprite1",
						},
						"newName": "sprite2",
					}
					data, _ := json.Marshal(arg)
					return []json.RawMessage{data}
				}(),
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "WorkspaceExecuteCommandLegacy",
			method: "workspace/executeCommand",
			params: ExecuteCommandParams{
				Command: CommandSpxRenameResources,
				Arguments: func() []json.RawMessage {
					arg := map[string]any{
						"resource": map[string]any{
							"uri": "spx://resources/sprites/sprite1",
						},
						"newName": "sprite2",
					}
					data, _ := json.Marshal(arg)
					return []json.RawMessage{data}
				}(),
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			replier := newMockReplier()
			server := New(newProjectWithoutModTime(tc.files), replier, fileMapGetter(tc.files), &MockScheduler{})

			var params json.RawMessage
			if tc.params != nil {
				var err error
				params, err = json.Marshal(tc.params)
				require.NoError(t, err, "Failed to marshal params")
			}

			id := jsonrpc2.NewIntID(1)
			call, err := jsonrpc2.NewCall(id, tc.method, params)
			require.NoError(t, err, "Failed to create call")

			err = server.HandleMessage(call)
			require.NoError(t, err, "Failed to handle message")

			msgs := replier.waitForMessages(tc.msgNum, 5*time.Second)
			assert.Len(t, msgs, tc.msgNum,
				"method %q got %d messages, want %d",
				tc.method, len(msgs), tc.msgNum)
		})
	}
}

func TestHandleMessageNotification(t *testing.T) {
	testCases := []struct {
		name   string
		method string
		params any
		files  map[string][]byte
		msgNum int
	}{
		{
			name:   "Initialized",
			method: "initialized",
			params: InitializedParams{},
			msgNum: 1, // only telemetry event
		},
		{
			name:   "Exit",
			method: "exit",
			params: nil,
			msgNum: 0, // exit 不发送任何消息
		},
		{
			name:   "CancelRequest",
			method: "$/cancelRequest",
			params: CancelParams{
				ID: jsonrpc2.NewStringID("test-request"),
			},
			msgNum: 1, // only telemetry event
		},
		{
			name:   "TextDocumentDidOpen",
			method: "textDocument/didOpen",
			params: DidOpenTextDocumentParams{
				TextDocument: protocol.TextDocumentItem{
					URI:        "file:///main.spx",
					LanguageID: "spx",
					Version:    1,
					Text:       "var x = 100\necho x",
				},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2, // telemetry event + diagnostics notification
		},
		{
			name:   "TextDocumentDidChange",
			method: "textDocument/didChange",
			params: DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{
					TextDocumentIdentifier: TextDocumentIdentifier{
						URI: "file:///main.spx",
					},
					Version: 2,
				},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{
					{
						Text: "var y = 200\necho y",
					},
				},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2, // telemetry event + diagnostics notification
		},
		{
			name:   "TextDocumentDidSave",
			method: "textDocument/didSave",
			params: DidSaveTextDocumentParams{
				TextDocument: TextDocumentIdentifier{
					URI: "file:///main.spx",
				},
				Text: func() *string {
					text := "var x = 100\necho x"
					return &text
				}(),
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2, // telemetry event + diagnostics notification
		},
		{
			name:   "TextDocumentDidClose",
			method: "textDocument/didClose",
			params: DidCloseTextDocumentParams{
				TextDocument: TextDocumentIdentifier{
					URI: "file:///main.spx",
				},
			},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2, // telemetry event + diagnostics notification
		},
		{
			name:   "UnknownNotificationMethod",
			method: "unknown/method",
			params: nil,
			msgNum: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			replier := newMockReplier()
			server := New(newProjectWithoutModTime(tc.files), replier, fileMapGetter(tc.files), &MockScheduler{})

			var params json.RawMessage
			if tc.params != nil {
				var err error
				params, err = json.Marshal(tc.params)
				require.NoError(t, err, "Failed to marshal params")
			}

			call, err := jsonrpc2.NewNotification(tc.method, params)
			require.NoError(t, err, "Failed to create call")

			err = server.HandleMessage(call)
			require.NoError(t, err, "Failed to handle message")

			msgs := replier.waitForMessages(tc.msgNum, 5*time.Second)
			assert.Len(t, msgs, tc.msgNum,
				"method %q got %d messages, want %d",
				tc.method, len(msgs), tc.msgNum)
		})
	}
}

func TestNotifyPropertyRenamed(t *testing.T) {
	t.Run("PropertyFieldRenamed", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
var MySprite Sprite

func MainEntry() {
}
`),
			"MySprite.spx": []byte(`
var (
	x int
	y int
)
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}

		replier := newMockReplier()
		s := New(newProjectWithoutModTime(m), replier, fileMapGetter(m), &MockScheduler{})

		// Perform rename on property field x
		workspaceEdit, err := s.textDocumentRename(&RenameParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///MySprite.spx"},
			Position:     Position{Line: 2, Character: 1}, // Position on "x"
			NewName:      "posX",
		})
		require.NoError(t, err)
		require.NotNil(t, workspaceEdit)

		// Wait for messages and filter for property renamed notification
		msgs := replier.waitForMessages(1, 1*time.Second)
		require.Len(t, msgs, 1, "want 1 notification message")

		notif, ok := msgs[0].(*jsonrpc2.Notification)
		require.True(t, ok, "Message should be a notification")
		assert.Equal(t, "textDocument/xgo.propertyRenamed", notif.Method())

		var params PropertyRenamedParams
		err = json.Unmarshal(notif.Params(), &params)
		require.NoError(t, err)

		assert.Equal(t, "MySprite", params.Target)
		assert.Equal(t, "x", params.OldName)
		assert.Equal(t, "posX", params.NewName)
		assert.Equal(t, DocumentURI("file:///MySprite.spx"), params.TextDocument.URI)
	})
}
