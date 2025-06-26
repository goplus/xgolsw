package server

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/goplus/xgolsw/internal/vfs"
	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockReplier struct {
	mu       sync.Mutex
	messages []jsonrpc2.Message
}

func (m *mockReplier) ReplyMessage(msg jsonrpc2.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockReplier) getMessages() []jsonrpc2.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]jsonrpc2.Message, len(m.messages))
	copy(result, m.messages)
	return result
}

func (m *mockReplier) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = nil
}

func newMapFSWithoutModTime(files map[string][]byte) *vfs.MapFS {
	fileMap := make(map[string]*vfs.MapFile)
	for k, v := range files {
		fileMap[k] = &vfs.MapFile{Content: v}
	}
	return xgo.NewProject(nil, fileMap, xgo.FeatAll)
}

func fileMapGetter(files map[string][]byte) func() map[string]*vfs.MapFile {
	return func() map[string]*vfs.MapFile {
		fileMap := make(map[string]*vfs.MapFile)
		for k, v := range files {
			fileMap[k] = &vfs.MapFile{Content: v}
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
		replier := &mockReplier{}
		s := New(newMapFSWithoutModTime(files), replier, fileMapGetter(files), &MockScheduler{})

		requestID1 := jsonrpc2.NewStringID("test-request-1")
		requestID2 := jsonrpc2.NewStringID("test-request-2")

		var request1Runned bool
		var request2Runned bool
		s.runWithResponse(requestID1, func() (any, error) {
			request1Runned = true
			return "should not reach here", nil
		})
		s.runWithResponse(requestID2, func() (any, error) {
			request2Runned = true
			return "should not reach here either", nil
		})

		err1 := s.cancelRequest(&CancelParams{ID: "test-request-1"})
		require.NoError(t, err1)
		err2 := s.cancelRequest(&CancelParams{ID: "test-request-2"})
		require.NoError(t, err2)

		time.Sleep(10 * time.Millisecond)

		assert.False(t, request1Runned, "Function should not have been executed for cancelled request")
		assert.False(t, request2Runned, "Function should not have been executed for cancelled request")

		messages := replier.getMessages()
		require.Len(t, messages, 2)

		var response1, response2 *jsonrpc2.Response
		require.Len(t, messages, 2, "Should receive two Response messages")
		for _, v := range messages {
			response, ok := v.(*jsonrpc2.Response)
			require.True(t, ok, "Should receive a Response message")
			if response.ID() == requestID1 {
				response1 = response
			} else if response.ID() == requestID2 {
				response2 = response
			}
		}

		assert.Equal(t, requestID1, response1.ID())
		assert.NotNil(t, response1.Err())
		var wireErr1 *jsonrpc2.WireError
		require.True(t, errors.As(response1.Err(), &wireErr1))
		assert.Equal(t, int64(RequestCancelled), wireErr1.Code)
		assert.Contains(t, wireErr1.Message, "Request cancelled")

		assert.Equal(t, requestID2, response2.ID())
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
		s := New(newMapFSWithoutModTime(files), replier, fileMapGetter(files), &MockScheduler{})

		testCases := []struct {
			name string
			id   interface{}
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

func TestHandleMessage_Call(t *testing.T) {
	testCases := []struct {
		name   string
		method string
		params interface{}
		files  map[string][]byte
		msgNum int
	}{
		{
			name:   "Method Not Found",
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
			name:   "TextDocument/Hover",
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
`)},
			msgNum: 2,
		},
		{
			name:   "TextDocument/Completion",
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
			name:   "TextDocument/SignatureHelp",
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
			name:   "TextDocument/Declaration",
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
			name:   "TextDocument/Definition",
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
			name:   "TextDocument/TypeDefinition",
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
			name:   "TextDocument/Implementation",
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
			name:   "TextDocument/References",
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
			name:   "TextDocument/DocumentHighlight",
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
			name:   "TextDocument/DocumentLink",
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
			name:   "TextDocument/Diagnostic",
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
			name:   "Workspace/Diagnostic",
			method: "workspace/diagnostic",
			params: WorkspaceDiagnosticParams{},
			files: map[string][]byte{
				"main.spx": []byte("var x = 100\necho x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocument/Formatting",
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
			name:   "TextDocument/PrepareRename",
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
			name:   "TextDocument/Rename",
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
			name:   "TextDocument/SemanticTokens/Full",
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
			name:   "TextDocument/InlayHint",
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
			name:   "Workspace/ExecuteCommand",
			method: "workspace/executeCommand",
			params: ExecuteCommandParams{
				Command: "spx.renameResource",
				Arguments: func() []json.RawMessage {
					arg := map[string]interface{}{
						"resource": map[string]interface{}{
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
			replier := &mockReplier{}
			server := New(newMapFSWithoutModTime(tc.files), replier, fileMapGetter(tc.files), &MockScheduler{})

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

			time.Sleep(100 * time.Millisecond)
			msgs := replier.getMessages()
			assert.Len(t, msgs, tc.msgNum,
				"Method '%s': Expected %d messages, got %d",
				tc.method, tc.msgNum, len(msgs))
		})
	}
}
