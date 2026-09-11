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

type reentrantResponseReplier struct {
	*mockReplier
	id         jsonrpc2.ID
	onResponse func() error
	once       sync.Once
	errMu      sync.Mutex
	err        error
}

func newReentrantResponseReplier(id jsonrpc2.ID, onResponse func() error) *reentrantResponseReplier {
	return &reentrantResponseReplier{
		mockReplier: newMockReplier(),
		id:          id,
		onResponse:  onResponse,
	}
}

func (m *reentrantResponseReplier) ReplyMessage(msg jsonrpc2.Message) error {
	response, ok := msg.(*jsonrpc2.Response)
	if ok && response.ID() == m.id {
		m.once.Do(func() {
			m.setErr(m.onResponse())
		})
	}
	return m.mockReplier.ReplyMessage(msg)
}

func (m *reentrantResponseReplier) Err() error {
	m.errMu.Lock()
	defer m.errMu.Unlock()
	return m.err
}

func (m *reentrantResponseReplier) setErr(err error) {
	m.errMu.Lock()
	m.err = err
	m.errMu.Unlock()
}

func (m *mockReplier) getMessages() []jsonrpc2.Message {
	m.mu.Lock()
	result := make([]jsonrpc2.Message, len(m.messages))
	copy(result, m.messages)
	m.mu.Unlock()
	return result
}

func (m *mockReplier) clearMessages() {
	m.mu.Lock()
	m.messages = nil
	m.mu.Unlock()
}

func (m *mockReplier) waitForMessages(count int, timeout time.Duration) []jsonrpc2.Message {
	// For count=0, wait a short time to ensure no unexpected messages arrive
	if count == 0 {
		time.Sleep(10 * time.Millisecond)
		return m.getMessages()
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

func (m *mockReplier) waitForResponse(id jsonrpc2.ID, timeout time.Duration) *jsonrpc2.Response {
	m.mu.Lock()
	defer m.mu.Unlock()

	timedOut := false
	timer := time.AfterFunc(timeout, func() {
		m.mu.Lock()
		timedOut = true
		m.cond.Broadcast()
		m.mu.Unlock()
	})
	defer timer.Stop()

	for !timedOut {
		for _, message := range m.messages {
			response, ok := message.(*jsonrpc2.Response)
			if ok && response.ID() == id {
				return response
			}
		}
		m.cond.Wait()
	}
	return nil
}

// MockScheduler implements [Scheduler]
type MockScheduler struct{}

func (s *MockScheduler) Sched() {
	time.Sleep(1 * time.Millisecond)
}

type blockingScheduler struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingScheduler() *blockingScheduler {
	return &blockingScheduler{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (s *blockingScheduler) Sched() {
	s.once.Do(func() {
		close(s.started)
	})
	<-s.release
}

func initializeServerForTest(t *testing.T, server *Server, replier *mockReplier) {
	t.Helper()

	call, err := jsonrpc2.NewCall(jsonrpc2.NewStringID("initialize"), "initialize", InitializeParams{
		XInitializeParams: protocol.XInitializeParams{
			RootURI: "file:///",
			Capabilities: protocol.ClientCapabilities{
				TextDocument: protocol.TextDocumentClientCapabilities{
					Completion: protocol.CompletionClientCapabilities{
						CompletionItem: protocol.ClientCompletionItemOptions{
							SnippetSupport:      true,
							DocumentationFormat: []protocol.MarkupKind{protocol.Markdown},
						},
						CompletionItemKind: &protocol.ClientCompletionItemOptionsKind{
							ValueSet: []protocol.CompletionItemKind{
								protocol.TextCompletion,
								protocol.MethodCompletion,
								protocol.FunctionCompletion,
								protocol.FieldCompletion,
								protocol.VariableCompletion,
								protocol.ClassCompletion,
								protocol.InterfaceCompletion,
								protocol.ModuleCompletion,
								protocol.PropertyCompletion,
								protocol.UnitCompletion,
								protocol.KeywordCompletion,
								protocol.ReferenceCompletion,
								protocol.ConstantCompletion,
								protocol.StructCompletion,
							},
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
				},
			},
		},
	})
	require.NoError(t, err)
	require.NoError(t, server.HandleMessage(call))
	messages := replier.waitForMessages(2, 5*time.Second)
	require.Len(t, messages, 2)
	require.NoError(t, requireResponseForID(t, messages, call.ID()).Err())
	replier.clearMessages()
}

func requireResponseForID(t *testing.T, messages []jsonrpc2.Message, id jsonrpc2.ID) *jsonrpc2.Response {
	t.Helper()

	for _, message := range messages {
		response, ok := message.(*jsonrpc2.Response)
		if ok && response.ID() == id {
			return response
		}
	}
	require.Failf(t, "missing response", "missing response for id %q", id)
	return nil
}

func TestHandleMessageInitialization(t *testing.T) {
	t.Run("RequestBeforeInitialize", func(t *testing.T) {
		replier := newMockReplier()
		server := newTestServer(t, nil)
		server.replier = replier
		call, err := jsonrpc2.NewCall(jsonrpc2.NewStringID("hover"), "textDocument/hover", HoverParams{})
		require.NoError(t, err)
		require.NoError(t, server.HandleMessage(call))

		messages := replier.waitForMessages(1, 5*time.Second)
		response := requireResponseForID(t, messages, call.ID())
		require.Error(t, response.Err())
		var wireErr *jsonrpc2.WireError
		require.True(t, errors.As(response.Err(), &wireErr))
		assert.Equal(t, int64(ServerNotInitialized), wireErr.Code)
	})

	t.Run("NotificationBeforeInitialize", func(t *testing.T) {
		replier := newMockReplier()
		server := newTestServer(t, nil)
		server.replier = replier
		notification, err := jsonrpc2.NewNotification("textDocument/didOpen", DidOpenTextDocumentParams{})
		require.NoError(t, err)
		require.NoError(t, server.HandleMessage(notification))
		assert.Empty(t, replier.waitForMessages(0, time.Second))
	})

	t.Run("CancelRequestBeforeInitialize", func(t *testing.T) {
		replier := newMockReplier()
		server := newTestServer(t, nil)
		server.replier = replier
		notification, err := jsonrpc2.NewNotification("$/cancelRequest", CancelParams{ID: "initialize"})
		require.NoError(t, err)
		require.NoError(t, server.HandleMessage(notification))
		assert.Empty(t, replier.waitForMessages(0, time.Second))
	})

	t.Run("CancelNotificationDuringInitialize", func(t *testing.T) {
		replier := newMockReplier()
		scheduler := newBlockingScheduler()
		server := newTestServer(t, nil)
		server.replier = replier
		server.scheduler = scheduler
		call, err := jsonrpc2.NewCall(jsonrpc2.NewStringID("initialize"), "initialize", InitializeParams{})
		require.NoError(t, err)
		require.NoError(t, server.HandleMessage(call))

		select {
		case <-scheduler.started:
		case <-time.After(5 * time.Second):
			require.Fail(t, "initialize did not start")
		}

		notification, err := jsonrpc2.NewNotification("$/cancelRequest", CancelParams{ID: "initialize"})
		require.NoError(t, err)
		require.NoError(t, server.HandleMessage(notification))
		close(scheduler.release)

		response := replier.waitForResponse(call.ID(), 5*time.Second)
		require.NotNil(t, response)
		require.NoError(t, response.Err())
		assert.True(t, server.isInitialized())
	})

	t.Run("ExitBeforeInitialize", func(t *testing.T) {
		replier := newMockReplier()
		server := newTestServer(t, nil)
		server.replier = replier
		notification, err := jsonrpc2.NewNotification("exit", nil)
		require.NoError(t, err)
		require.NoError(t, server.HandleMessage(notification))
		assert.Empty(t, replier.waitForMessages(0, time.Second))
	})

	t.Run("InitializeOnlyOnce", func(t *testing.T) {
		replier := newMockReplier()
		server := newTestServer(t, nil)
		server.replier = replier
		initializeServerForTest(t, server, replier)

		call, err := jsonrpc2.NewCall(jsonrpc2.NewStringID("initialize-again"), "initialize", InitializeParams{})
		require.NoError(t, err)
		require.NoError(t, server.HandleMessage(call))
		messages := replier.waitForMessages(1, 5*time.Second)
		response := requireResponseForID(t, messages, call.ID())
		require.Error(t, response.Err())
		var wireErr *jsonrpc2.WireError
		require.True(t, errors.As(response.Err(), &wireErr))
		assert.Equal(t, int64(protocol.InvalidRequest), wireErr.Code)
	})

	t.Run("InitializeAcceptsReentrantRequestDuringReply", func(t *testing.T) {
		initializeID := jsonrpc2.NewStringID("initialize")
		shutdownID := jsonrpc2.NewStringID("shutdown")
		var server *Server
		replier := newReentrantResponseReplier(initializeID, func() error {
			shutdownCall, err := jsonrpc2.NewCall(shutdownID, "shutdown", nil)
			if err != nil {
				return err
			}
			return server.HandleMessage(shutdownCall)
		})
		server = newTestServer(t, nil)
		server.replier = replier
		initializeCall, err := jsonrpc2.NewCall(initializeID, "initialize", InitializeParams{})
		require.NoError(t, err)
		require.NoError(t, server.HandleMessage(initializeCall))

		initializeResponse := replier.waitForResponse(initializeCall.ID(), 5*time.Second)
		require.NotNil(t, initializeResponse)
		require.NoError(t, initializeResponse.Err())
		shutdownResponse := replier.waitForResponse(shutdownID, 5*time.Second)
		require.NotNil(t, shutdownResponse)
		require.NoError(t, shutdownResponse.Err())
		require.NoError(t, replier.Err())
		assert.True(t, server.isInitialized())
	})
}

func TestServerCancellation(t *testing.T) {
	t.Run("CancelRequest", func(t *testing.T) {
		files := map[string][]byte{
			"main.xgo": []byte(`
var x = 100
println x
`),
		}
		replier := newMockReplier()
		s := newTestServer(t, files)
		s.replier = replier

		call1, _ := jsonrpc2.NewCall(jsonrpc2.NewStringID("test-request-1"), "$/cancelRequest", &CancelParams{ID: "test-request-1"})
		call2, _ := jsonrpc2.NewCall(jsonrpc2.NewStringID("test-request-2"), "$/cancelRequest", &CancelParams{ID: "test-request-2"})

		var request1Ran bool
		var request2Ran bool
		s.runForCall(call1, func() (any, error) {
			request1Ran = true
			return "should not reach here", nil
		})
		s.runForCall(call2, func() (any, error) {
			request2Ran = true
			return "should not reach here either", nil
		})

		err1 := s.cancelRequest(&CancelParams{ID: "test-request-1"})
		require.NoError(t, err1)
		err2 := s.cancelRequest(&CancelParams{ID: "test-request-2"})
		require.NoError(t, err2)

		messages := replier.waitForMessages(2, 5*time.Second)

		assert.False(t, request1Ran, "Function should not have been executed for cancelled request")
		assert.False(t, request2Ran, "Function should not have been executed for cancelled request")
		require.Len(t, messages, 2)

		response1 := requireResponseForID(t, messages, call1.ID())
		response2 := requireResponseForID(t, messages, call2.ID())

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
			"main.xgo": []byte(`var x = 100`),
		}
		replier := newMockReplier()
		s := newTestServer(t, files)
		s.replier = replier

		for _, tc := range []struct {
			name string
			id   any
		}{
			{"InvalidType", []int{1, 2, 3}},
			{"EmptyMap", map[string]string{}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				err := s.cancelRequest(&CancelParams{ID: tc.id})
				// Should return an error for invalid ID
				require.Error(t, err)
				assert.Contains(t, err.Error(), "cancelRequest:")
			})
		}
	})
}

func TestHandleMessageNotificationOrdering(t *testing.T) {
	files := map[string][]byte{"main.xgo": []byte("println \"a\"\n")}
	server := newTestServer(t, files)
	project := server.getProj()
	project.PutFile("main.xgo", &xgo.File{Content: files["main.xgo"], Version: 1})
	replier := newMockReplier()
	server.replier = replier
	initializeServerForTest(t, server, replier)
	var changeCount int
	t.Cleanup(func() {
		// Wait for every background diagnostic notification before the test ends.
		require.Eventually(t, func() bool {
			var diagnosticCount int
			for _, message := range replier.getMessages() {
				if notification, ok := message.(*jsonrpc2.Notification); ok && notification.Method() == "textDocument/publishDiagnostics" {
					diagnosticCount++
				}
			}
			return diagnosticCount == changeCount
		}, 5*time.Second, time.Millisecond)
	})

	for _, params := range []DidChangeTextDocumentParams{
		{
			TextDocument: protocol.VersionedTextDocumentIdentifier{
				TextDocumentIdentifier: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Version:                2,
			},
			ContentChanges: []protocol.TextDocumentContentChangeEvent{{
				Range: &protocol.Range{
					Start: protocol.Position{Line: 0, Character: 10},
					End:   protocol.Position{Line: 0, Character: 10},
				},
				Text: "b",
			}},
		},
		{
			TextDocument: protocol.VersionedTextDocumentIdentifier{
				TextDocumentIdentifier: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Version:                3,
			},
			ContentChanges: []protocol.TextDocumentContentChangeEvent{{
				Range: &protocol.Range{
					Start: protocol.Position{Line: 0, Character: 11},
					End:   protocol.Position{Line: 0, Character: 11},
				},
				Text: "c",
			}},
		},
	} {
		notification, err := jsonrpc2.NewNotification("textDocument/didChange", params)
		require.NoError(t, err)
		require.NoError(t, server.HandleMessage(notification))
		changeCount++
	}

	file, ok := project.File("main.xgo")
	require.True(t, ok)
	assert.Equal(t, "println \"abc\"\n", string(file.Content))
	assert.Equal(t, 3, file.Version)
}

func TestHandleMessageCall(t *testing.T) {
	for _, tc := range []struct {
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
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 2, Character: 1},
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte(`
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
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 1, Character: 8},
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentSignatureHelp",
			method: "textDocument/signatureHelp",
			params: SignatureHelpParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 1, Character: 8},
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln(x)"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentDeclaration",
			method: "textDocument/declaration",
			params: DeclarationParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 1, Character: 8},
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentDefinition",
			method: "textDocument/definition",
			params: DefinitionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 1, Character: 8},
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentTypeDefinition",
			method: "textDocument/typeDefinition",
			params: TypeDefinitionParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 1, Character: 8},
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentImplementation",
			method: "textDocument/implementation",
			params: ImplementationParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 1, Character: 8},
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentReferences",
			method: "textDocument/references",
			params: ReferenceParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 1, Character: 8},
				},
				Context: ReferenceContext{
					IncludeDeclaration: true,
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentDocumentHighlight",
			method: "textDocument/documentHighlight",
			params: DocumentHighlightParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 1, Character: 8},
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentDocumentLink",
			method: "textDocument/documentLink",
			params: DocumentLinkParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			},
			files: map[string][]byte{
				"main.xgo": []byte(`import "fmt"`),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentDiagnostic",
			method: "textDocument/diagnostic",
			params: DocumentDiagnosticParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2,
		},
		{
			name:   "WorkspaceDiagnostic",
			method: "workspace/diagnostic",
			params: WorkspaceDiagnosticParams{},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentFormatting",
			method: "textDocument/formatting",
			params: DocumentFormattingParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x=100\nprintln   x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentPrepareRename",
			method: "textDocument/prepareRename",
			params: PrepareRenameParams{
				TextDocumentPositionParams: TextDocumentPositionParams{
					TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
					Position:     Position{Line: 0, Character: 5},
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentRename",
			method: "textDocument/rename",
			params: RenameParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Position:     Position{Line: 0, Character: 5},
				NewName:      "y",
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentSemanticTokensFull",
			method: "textDocument/semanticTokens/full",
			params: SemanticTokensParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2,
		},
		{
			name:   "TextDocumentInlayHint",
			method: "textDocument/inlayHint",
			params: InlayHintParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"},
				Range: Range{
					Start: Position{Line: 0, Character: 0},
					End:   Position{Line: 1, Character: 9},
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replier := newMockReplier()
			server := newTestServer(t, tc.files)
			server.replier = replier
			initializeServerForTest(t, server, replier)

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
			response := requireResponseForID(t, msgs, id)
			if tc.method == "unknown/method" {
				require.ErrorIs(t, response.Err(), jsonrpc2.ErrMethodNotFound)
			} else {
				require.NoError(t, response.Err())
				assert.True(t, json.Valid(response.Result()))
			}
		})
	}
}

func TestHandleMessageNotification(t *testing.T) {
	for _, tc := range []struct {
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
			msgNum: 0, // exit does not send any messages
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
					URI:        "file:///main.xgo",
					LanguageID: "xgo",
					Version:    1,
					Text:       "var x = 100\nprintln x",
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2, // telemetry event + diagnostics notification
		},
		{
			name:   "TextDocumentDidChange",
			method: "textDocument/didChange",
			params: DidChangeTextDocumentParams{
				TextDocument: protocol.VersionedTextDocumentIdentifier{
					TextDocumentIdentifier: TextDocumentIdentifier{
						URI: "file:///main.xgo",
					},
					Version: 2,
				},
				ContentChanges: []protocol.TextDocumentContentChangeEvent{
					{
						Text: "var y = 200\nprintln y",
					},
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2, // telemetry event + diagnostics notification
		},
		{
			name:   "TextDocumentDidSave",
			method: "textDocument/didSave",
			params: DidSaveTextDocumentParams{
				TextDocument: TextDocumentIdentifier{
					URI: "file:///main.xgo",
				},
				Text: func() *string {
					text := "var x = 100\nprintln x"
					return &text
				}(),
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2, // telemetry event + diagnostics notification
		},
		{
			name:   "TextDocumentDidClose",
			method: "textDocument/didClose",
			params: DidCloseTextDocumentParams{
				TextDocument: TextDocumentIdentifier{
					URI: "file:///main.xgo",
				},
			},
			files: map[string][]byte{
				"main.xgo": []byte("var x = 100\nprintln x"),
			},
			msgNum: 2, // telemetry event + diagnostics notification
		},
		{
			name:   "UnknownNotificationMethod",
			method: "unknown/method",
			params: nil,
			msgNum: 0,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			replier := newMockReplier()
			server := newTestServer(t, tc.files)
			server.replier = replier
			initializeServerForTest(t, server, replier)

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
			if tc.msgNum == 2 {
				var diagnostics []jsonrpc2.Message
				for _, message := range msgs {
					notification := requireValueAs[*jsonrpc2.Notification](t, message)
					if notification.Method() == "textDocument/publishDiagnostics" {
						diagnostics = append(diagnostics, notification)
					}
				}
				require.Len(t, diagnostics, 1)
				notification := requireValueAs[*jsonrpc2.Notification](t, diagnostics[0])
				var params PublishDiagnosticsParams
				require.NoError(t, json.Unmarshal(notification.Params(), &params))
				assert.Equal(t, DocumentURI("file:///main.xgo"), params.URI)
				assert.Equal(t, []Diagnostic{}, params.Diagnostics)
			}
		})
	}
}
