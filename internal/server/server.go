package server

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/goplus/mod/modload"
	"github.com/goplus/mod/xgomod"
	xgoast "github.com/goplus/xgo/ast"
	xgotoken "github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal"
	"github.com/goplus/xgolsw/internal/analysis"
	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// MessageReplier is an interface for sending messages back to the client.
type MessageReplier interface {
	// ReplyMessage sends a message back to the client.
	//
	// The message can be one of:
	//   - [jsonrpc2.Response]: sent in response to a call.
	//   - [jsonrpc2.Notification]: sent for server-initiated notifications.
	ReplyMessage(m jsonrpc2.Message) error
}

// FileMapGetter is a function that returns a map of file names to [xgo.File]s.
type FileMapGetter func() map[string]*xgo.File

// Scheduler is an interface for task scheduling.
type Scheduler interface {
	// Sched yields the processor, allowing other routines to run.
	// "routines" here refers to not just goroutines, but also other tasks, for example, Javascript event loop in browsers.
	Sched()
}

// Server is the core language server implementation that handles LSP messages.
type Server struct {
	workspaceRootURI DocumentURI
	workspaceRootFS  *xgo.Project
	replier          MessageReplier
	analyzers        []*analysis.Analyzer
	fileMapGetter    FileMapGetter // TODO(wyvern): Remove this field.
	cancelCauseFuncs sync.Map      // Map of request IDs to cancel functions (with cause).
	scheduler        Scheduler
}

func (s *Server) getProj() *xgo.Project {
	return s.workspaceRootFS
}

func (s *Server) getProjWithFile() *xgo.Project {
	proj := s.workspaceRootFS
	proj.UpdateFiles(s.fileMapGetter())
	return proj
}

// New creates a new Server instance.
func New(proj *xgo.Project, replier MessageReplier, fileMapGetter FileMapGetter, scheduler Scheduler) *Server {
	mod := xgomod.New(modload.Default)
	if err := mod.ImportClasses(); err != nil {
		panic(fmt.Errorf("failed to import classes: %w", err))
	}
	proj.PkgPath = "main"
	proj.Mod = mod
	proj.Importer = internal.Importer
	return &Server{
		// TODO(spxls): Initialize request should set workspaceRootURI value
		workspaceRootURI: "file:///",
		workspaceRootFS:  proj,
		replier:          replier,
		analyzers:        initAnalyzers(true),
		fileMapGetter:    fileMapGetter,
		scheduler:        scheduler,
	}
}

// InitAnalyzers initializes the analyzers for the server.
func initAnalyzers(staticcheck bool) []*analysis.Analyzer {
	analyzers := slices.Collect(maps.Values(analysis.DefaultAnalyzers))
	if staticcheck {
		analyzers = slices.AppendSeq(analyzers, maps.Values(analysis.StaticcheckAnalyzers))
	}
	return analyzers
}

// HandleMessage handles an incoming LSP message.
func (s *Server) HandleMessage(m jsonrpc2.Message) error {
	switch m := m.(type) {
	case *jsonrpc2.Call:
		return s.handleCall(m)
	case *jsonrpc2.Notification:
		return s.handleNotification(m)
	}
	return fmt.Errorf("unsupported message type: %T", m)
}

// handleCall handles a call message.
func (s *Server) handleCall(c *jsonrpc2.Call) error {
	switch c.Method() {
	case "initialize":
		var params InitializeParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		return errors.New("TODO")
	case "shutdown":
		s.runForCall(c, func() (any, error) {
			return nil, nil // Protocol conformance only.
		})
	case "textDocument/hover":
		var params HoverParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentHover(&params)
		})
	case "textDocument/completion":
		var params CompletionParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentCompletion(&params)
		})
	case "textDocument/signatureHelp":
		var params SignatureHelpParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentSignatureHelp(&params)
		})
	case "textDocument/declaration":
		var params DeclarationParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentDeclaration(&params)
		})
	case "textDocument/definition":
		var params DefinitionParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentDefinition(&params)
		})
	case "textDocument/typeDefinition":
		var params TypeDefinitionParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentTypeDefinition(&params)
		})
	case "textDocument/implementation":
		var params ImplementationParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentImplementation(&params)
		})
	case "textDocument/references":
		var params ReferenceParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentReferences(&params)
		})
	case "textDocument/documentHighlight":
		var params DocumentHighlightParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentDocumentHighlight(&params)
		})
	case "textDocument/documentLink":
		var params DocumentLinkParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentDocumentLink(&params)
		})
	case "textDocument/diagnostic":
		var params DocumentDiagnosticParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentDiagnostic(&params)
		})
	case "workspace/diagnostic":
		var params WorkspaceDiagnosticParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.workspaceDiagnostic(&params)
		})
	case "textDocument/formatting":
		var params DocumentFormattingParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentFormatting(&params)
		})
	case "textDocument/prepareRename":
		var params PrepareRenameParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentPrepareRename(&params)
		})
	case "textDocument/rename":
		var params RenameParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentRename(&params)
		})
	case "textDocument/semanticTokens/full":
		var params SemanticTokensParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentSemanticTokensFull(&params)
		})
	case "textDocument/inlayHint":
		var params InlayHintParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.textDocumentInlayHint(&params)
		})
	case "workspace/executeCommand":
		var params ExecuteCommandParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runForCall(c, func() (any, error) {
			return s.workspaceExecuteCommand(&params)
		})
	default:
		return s.replyMethodNotFound(c.ID(), c.Method())
	}
	return nil
}

// handleNotification handles a notification message.
func (s *Server) handleNotification(n *jsonrpc2.Notification) error {
	switch n.Method() {
	case "initialized":
		var params InitializedParams
		if err := UnmarshalJSON(n.Params(), &params); err != nil {
			return fmt.Errorf("failed to parse initialized params: %w", err)
		}
		s.runForNotification(n, func() error {
			return errors.New("TODO")
		})
	case "exit":
		// Protocol conformance only.
	case "$/cancelRequest":
		var params CancelParams
		if err := UnmarshalJSON(n.Params(), &params); err != nil {
			return fmt.Errorf("failed to parse cancelRequest params: %w", err)
		}
		s.runForNotification(n, func() error {
			return s.cancelRequest(&params)
		})
	case "textDocument/didOpen":
		var params DidOpenTextDocumentParams
		if err := UnmarshalJSON(n.Params(), &params); err != nil {
			return fmt.Errorf("failed to parse didOpen params: %w", err)
		}
		s.runForNotification(n, func() error {
			return s.didOpen(&params)
		})
	case "textDocument/didChange":
		var params DidChangeTextDocumentParams
		if err := UnmarshalJSON(n.Params(), &params); err != nil {
			return fmt.Errorf("failed to parse didChange params: %w", err)
		}
		s.runForNotification(n, func() error {
			return s.didChange(&params)
		})
	case "textDocument/didSave":
		var params DidSaveTextDocumentParams
		if err := UnmarshalJSON(n.Params(), &params); err != nil {
			return fmt.Errorf("failed to parse didSave params: %w", err)
		}
		s.runForNotification(n, func() error {
			return s.didSave(&params)
		})
	case "textDocument/didClose":
		var params DidCloseTextDocumentParams
		if err := UnmarshalJSON(n.Params(), &params); err != nil {
			return fmt.Errorf("failed to parse didClose params: %w", err)
		}
		s.runForNotification(n, func() error {
			return s.didClose(&params)
		})
	}
	return nil
}

// sendTelemetryEvent sends a telemetry event to the client.
func (s *Server) sendTelemetryEvent(data map[string]any) error {
	n, err := jsonrpc2.NewNotification("telemetry/event", data)
	if err != nil {
		return fmt.Errorf("failed to create telemetry notification: %v", err)
	}
	return s.replier.ReplyMessage(n)
}

// publishDiagnostics sends diagnostic notifications to the client.
func (s *Server) publishDiagnostics(uri DocumentURI, diagnostics []Diagnostic) error {
	params := &PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diagnostics,
	}
	n, err := jsonrpc2.NewNotification("textDocument/publishDiagnostics", params)
	if err != nil {
		return fmt.Errorf("failed to create diagnostic notification: %w", err)
	}
	return s.replier.ReplyMessage(n)
}

// wrapWithMetrics is a helper function to wrap a function with telemetry metrics
func (s *Server) wrapWithMetrics(msg jsonrpc2.Message, fn func() (any, error)) func() (any, error) {
	initTime := time.Now()
	telemetryMsg := make(map[string]any)

	switch m := msg.(type) {
	case *jsonrpc2.Call:
		id := m.ID()
		telemetryMsg = map[string]any{
			"call": map[string]any{
				"id":     &id,
				"method": m.Method(),
				"params": m.Params(),
			},
		}
	case *jsonrpc2.Notification:
		telemetryMsg = map[string]any{
			"notification": map[string]any{
				"method": m.Method(),
				"params": m.Params(),
			},
		}
	}

	return func() (any, error) {
		startTime := time.Now()
		result, err := fn()
		endTime := time.Now()

		telemetryMsg["initTimestamp"] = initTime.UnixMilli()
		telemetryMsg["startTimestamp"] = startTime.UnixMilli()
		telemetryMsg["endTimestamp"] = endTime.UnixMilli()
		telemetryMsg["success"] = err == nil

		s.sendTelemetryEvent(telemetryMsg)
		return result, err
	}
}

// runForCall runs a function for a call message and replies with the result or error.
func (s *Server) runForCall(call *jsonrpc2.Call, fn func() (any, error)) {
	ctx, cancelCauseFunc := context.WithCancelCause(context.TODO())
	s.cancelCauseFuncs.Store(call.ID(), cancelCauseFunc)
	wrap := s.wrapWithMetrics(call, fn)
	go func() (err error) {
		defer func() {
			s.cancelCauseFuncs.Delete(call.ID())
			if err != nil {
				s.replyError(call.ID(), err)
			}
		}()

		s.scheduler.Sched() // Do scheduling to receive (cancel) notifications on the fly.
		if ctx.Err() != nil {
			err = context.Cause(ctx)
			return err
		}

		result, err := wrap()
		resp, err := jsonrpc2.NewResponse(call.ID(), result, err)
		if err != nil {
			return err
		}
		return s.replier.ReplyMessage(resp)
	}()
}

// runForNotification runs a function for a notification message without expecting a response.
func (s *Server) runForNotification(notify *jsonrpc2.Notification, fn func() error) {
	wrap := s.wrapWithMetrics(notify, func() (any, error) {
		return nil, fn()
	})
	go wrap()
}

var requestCancelled = jsonrpc2.NewError(int64(RequestCancelled), "Request cancelled")

// See https://microsoft.github.io/language-server-protocol/specifications/lsp/3.17/specification/#cancelRequest
func (s *Server) cancelRequest(params *CancelParams) error {
	if params == nil {
		return fmt.Errorf("cancelRequest: missing or invalid parameters")
	}
	id, err := jsonrpc2.MakeID(params.ID)
	if err != nil {
		return fmt.Errorf("cancelRequest: %w", err)
	}
	if cancelCauseFunc, ok := s.cancelCauseFuncs.Load(id); ok {
		if cancelWithCause, ok := cancelCauseFunc.(context.CancelCauseFunc); ok {
			cancelWithCause(requestCancelled)
			return nil
		}
	}
	return nil
}

// replyError replies to the client with an error response.
func (s *Server) replyError(id jsonrpc2.ID, err error) error {
	resp, err := jsonrpc2.NewResponse(id, nil, err)
	if err != nil {
		return err
	}
	return s.replier.ReplyMessage(resp)
}

// replyMethodNotFound replies to the client with a method not found error response.
func (s *Server) replyMethodNotFound(id jsonrpc2.ID, method string) error {
	return s.replyError(id, fmt.Errorf("%w: %s", jsonrpc2.ErrMethodNotFound, method))
}

// replyParseError replies to the client with a parse error response.
func (s *Server) replyParseError(id jsonrpc2.ID, err error) error {
	return s.replyError(id, fmt.Errorf("%w: %s", jsonrpc2.ErrParse, err))
}

// fromDocumentURI returns the relative path from a [DocumentURI].
func (s *Server) fromDocumentURI(documentURI DocumentURI) (string, error) {
	uri := string(documentURI)
	rootURI := string(s.workspaceRootURI)
	if !strings.HasPrefix(uri, rootURI) {
		return "", fmt.Errorf("document URI %q does not have workspace root URI %q as prefix", uri, rootURI)
	}
	return strings.TrimPrefix(uri, rootURI), nil
}

// toDocumentURI returns the [DocumentURI] for a relative path.
func (s *Server) toDocumentURI(path string) DocumentURI {
	return DocumentURI(string(s.workspaceRootURI) + path)
}

// posDocumentURI returns the [DocumentURI] for the given position in the project.
func (s *Server) posDocumentURI(proj *xgo.Project, pos xgotoken.Pos) DocumentURI {
	return s.toDocumentURI(xgoutil.PosFilename(proj, pos))
}

// nodeDocumentURI returns the [DocumentURI] for the given node in the project.
func (s *Server) nodeDocumentURI(proj *xgo.Project, node xgoast.Node) DocumentURI {
	return s.posDocumentURI(proj, node.Pos())
}

// locationForPos returns the [Location] for the given position in the project.
func (s *Server) locationForPos(proj *xgo.Project, pos xgotoken.Pos) Location {
	return Location{
		URI:   s.posDocumentURI(proj, pos),
		Range: RangeForPos(proj, pos),
	}
}

// locationForNode returns the [Location] for the given node in the project.
func (s *Server) locationForNode(proj *xgo.Project, node xgoast.Node) Location {
	return Location{
		URI:   s.nodeDocumentURI(proj, node),
		Range: RangeForNode(proj, node),
	}
}
