package server

import (
	"bytes"
	"errors"
	"fmt"
	"go/types"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/goplus/gop/ast"
	gopast "github.com/goplus/gop/ast"
	goptoken "github.com/goplus/gop/token"
	"github.com/goplus/gop/x/typesutil"
	"github.com/goplus/goxlsw/gop"
	"github.com/goplus/goxlsw/gop/goputil"
	"github.com/goplus/goxlsw/internal/analysis"
	"github.com/goplus/goxlsw/internal/pkgdata"
	"github.com/goplus/goxlsw/internal/util"
	"github.com/goplus/goxlsw/internal/vfs"
	"github.com/goplus/goxlsw/jsonrpc2"
	"github.com/goplus/goxlsw/pkgdoc"
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

// FileMapGetter is a function that returns a map of file names to [vfs.MapFile]s.
type FileMapGetter func() map[string]vfs.MapFile

// Server is the core language server implementation that handles LSP messages.
type Server struct {
	workspaceRootURI DocumentURI
	workspaceRootFS  *vfs.MapFS
	replier          MessageReplier
	analyzers        []*analysis.Analyzer
	fileMapGetter    FileMapGetter // TODO(wyvern): Remove this field.
}

func (s *Server) getProj() *gop.Project {
	return s.workspaceRootFS
}

// New creates a new Server instance.
func New(mapFS *vfs.MapFS, replier MessageReplier, fileMapGetter FileMapGetter) *Server {
	return &Server{
		// TODO(spxls): Initialize request should set workspaceRootURI value
		workspaceRootURI: "file:///",
		workspaceRootFS:  mapFS,
		replier:          replier,
		analyzers:        initAnalyzers(true),
		fileMapGetter:    fileMapGetter,
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
		s.runWithResponse(c.ID(), func() (any, error) {
			return nil, nil // Protocol conformance only.
		})
	case "textDocument/hover":
		var params HoverParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentHover(&params)
		})
	case "textDocument/completion":
		var params CompletionParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentCompletion(&params)
		})
	case "textDocument/signatureHelp":
		var params SignatureHelpParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentSignatureHelp(&params)
		})
	case "textDocument/declaration":
		var params DeclarationParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentDeclaration(&params)
		})
	case "textDocument/definition":
		var params DefinitionParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentDefinition(&params)
		})
	case "textDocument/typeDefinition":
		var params TypeDefinitionParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentTypeDefinition(&params)
		})
	case "textDocument/implementation":
		var params ImplementationParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentImplementation(&params)
		})
	case "textDocument/references":
		var params ReferenceParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentReferences(&params)
		})
	case "textDocument/documentHighlight":
		var params DocumentHighlightParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentDocumentHighlight(&params)
		})
	case "textDocument/documentLink":
		var params DocumentLinkParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentDocumentLink(&params)
		})
	case "textDocument/diagnostic":
		var params DocumentDiagnosticParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentDiagnostic(&params)
		})
	case "workspace/diagnostic":
		var params WorkspaceDiagnosticParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.workspaceDiagnostic(&params)
		})
	case "textDocument/formatting":
		var params DocumentFormattingParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentFormatting(&params)
		})
	case "textDocument/prepareRename":
		var params PrepareRenameParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentPrepareRename(&params)
		})
	case "textDocument/rename":
		var params RenameParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentRename(&params)
		})
	case "textDocument/semanticTokens/full":
		var params SemanticTokensParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentSemanticTokensFull(&params)
		})
	case "textDocument/inlayHint":
		var params InlayHintParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
			return s.textDocumentInlayHint(&params)
		})
	case "workspace/executeCommand":
		var params ExecuteCommandParams
		if err := UnmarshalJSON(c.Params(), &params); err != nil {
			return s.replyParseError(c.ID(), err)
		}
		s.runWithResponse(c.ID(), func() (any, error) {
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
		return errors.New("TODO")
	case "exit":
		return nil // Protocol conformance only.
	case "textDocument/didOpen":
		var params DidOpenTextDocumentParams
		if err := UnmarshalJSON(n.Params(), &params); err != nil {
			return fmt.Errorf("failed to parse didOpen params: %w", err)
		}

		return s.didOpen(&params)
	case "textDocument/didChange":
		var params DidChangeTextDocumentParams
		if err := UnmarshalJSON(n.Params(), &params); err != nil {
			return fmt.Errorf("failed to parse didChange params: %w", err)
		}
		return s.didChange(&params)
	case "textDocument/didSave":
		var params DidSaveTextDocumentParams
		if err := UnmarshalJSON(n.Params(), &params); err != nil {
			return fmt.Errorf("failed to parse didSave params: %w", err)
		}
		return s.didSave(&params)
	case "textDocument/didClose":
		var params DidCloseTextDocumentParams
		if err := UnmarshalJSON(n.Params(), &params); err != nil {
			return fmt.Errorf("failed to parse didClose params: %w", err)
		}
		return s.didClose(&params)
	}
	return nil
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

// run runs the given function in a goroutine and replies to the client with any
// errors.
func (s *Server) run(id jsonrpc2.ID, fn func() error) {
	go func() {
		if err := fn(); err != nil {
			s.replyError(id, err)
		}
	}()
}

// runWithResponse runs the given function in a goroutine and handles the response.
func (s *Server) runWithResponse(id jsonrpc2.ID, fn func() (any, error)) {
	s.run(id, func() error {
		result, err := fn()
		resp, err := jsonrpc2.NewResponse(id, result, err)
		if err != nil {
			return err
		}
		return s.replier.ReplyMessage(resp)
	})
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

// fromPosition converts a token.Position to an LSP Position.
func (s *Server) fromPosition(proj *gop.Project, astFile *gopast.File, position goptoken.Position) Position {
	tokenFile := proj.Fset.File(astFile.Pos())

	line := position.Line
	lineStart := int(tokenFile.LineStart(line))
	relLineStart := lineStart - tokenFile.Base()
	lineContent := astFile.Code[relLineStart : relLineStart+position.Column-1]
	utf16Offset := utf8OffsetToUTF16(string(lineContent), position.Column-1)

	return Position{
		Line:      uint32(position.Line - 1),
		Character: uint32(utf16Offset),
	}
}

func (s *Server) toPosition(proj *gop.Project, astFile *gopast.File, position Position) goptoken.Position {
	tokenFile := proj.Fset.File(astFile.Pos())

	line := min(int(position.Line)+1, tokenFile.LineCount())
	lineStart := int(tokenFile.LineStart(line))
	relLineStart := lineStart - tokenFile.Base()
	lineContent := astFile.Code[relLineStart:]
	if i := bytes.IndexByte(lineContent, '\n'); i >= 0 {
		lineContent = lineContent[:i]
	}
	utf8Offset := utf16OffsetToUTF8(string(lineContent), int(position.Character))
	column := utf8Offset + 1

	return goptoken.Position{
		Filename: tokenFile.Name(),
		Offset:   relLineStart + utf8Offset,
		Line:     line,
		Column:   column,
	}
}

func (s *Server) identAtASTFilePosition(proj *gop.Project, astFile *gopast.File, position goptoken.Position) *gopast.Ident {
	var (
		bestIdent    *gopast.Ident
		bestNodeSpan int
	)
	fset := proj.Fset
	for _, ident := range s.identsAtASTFileLine(proj, astFile, position.Line) {
		identPos := fset.Position(ident.Pos())
		identEnd := fset.Position(ident.End())
		if position.Column < identPos.Column || position.Column > identEnd.Column {
			continue
		}

		nodeSpan := identEnd.Column - identPos.Column
		if bestIdent == nil || nodeSpan < bestNodeSpan {
			bestIdent = ident
			bestNodeSpan = nodeSpan
		}
	}
	return bestIdent
}

// identsAtASTFileLine returns the identifiers at the given line in the given
// AST file.
func (s *Server) identsAtASTFileLine(proj *gop.Project, astFile *gopast.File, line int) (idents []*gopast.Ident) {
	astFilePos := proj.Fset.Position(astFile.Pos())
	ast.Inspect(astFile, func(node gopast.Node) bool {
		if ident, ok := node.(*gopast.Ident); ok {
			// Check if the identifier is at the given line.
			identPos := proj.Fset.Position(ident.Pos())
			if identPos.Filename == astFilePos.Filename && identPos.Line == line {
				idents = append(idents, ident)
			}
		}
		return true
	})
	return
}

// rangeForASTFilePosition returns a [Range] for the given position in an AST file.
func (s *Server) rangeForASTFilePosition(proj *gop.Project, astFile *gopast.File, position goptoken.Position) Range {
	p := s.fromPosition(proj, astFile, position)
	return Range{Start: p, End: p}
}

// rangeForPos returns the [Range] for the given position.
func (s *Server) rangeForPos(proj *gop.Project, pos goptoken.Pos) Range {
	return s.rangeForASTFilePosition(proj, s.posASTFile(proj, pos), proj.Fset.Position(pos))
}

// posASTFile returns the AST file for the given position.
func (s *Server) posASTFile(proj *gop.Project, pos goptoken.Pos) *gopast.File {
	return getASTPkg(proj).Files[s.posFilename(proj, pos)]
}

// posFilename returns the filename for the given position.
func (s *Server) posFilename(proj *gop.Project, pos goptoken.Pos) string {
	return proj.Fset.Position(pos).Filename
}

// isInFset reports whether the given position exists in the file set.
func (s *Server) isInFset(proj *gop.Project, pos goptoken.Pos) bool {
	return proj.Fset.File(pos) != nil
}

// locationForPos returns the [Location] for the given position.
func (s *Server) locationForPos(proj *gop.Project, pos goptoken.Pos) Location {
	return Location{
		URI:   s.toDocumentURI(s.posFilename(proj, pos)),
		Range: s.rangeForPos(proj, pos),
	}
}

// rangeForNode returns the [Range] for the given node.
func (s *Server) rangeForNode(proj *gop.Project, node gopast.Node) Range {
	return s.rangeForASTFileNode(proj, s.nodeASTFile(proj, node), node)
}

// rangeForASTFileNode returns the [Range] for the given node in the given AST file.
func (s *Server) rangeForASTFileNode(proj *gop.Project, astFile *gopast.File, node gopast.Node) Range {
	fset := proj.Fset
	return Range{
		Start: s.fromPosition(proj, astFile, fset.Position(node.Pos())),
		End:   s.fromPosition(proj, astFile, fset.Position(node.End())),
	}
}

// nodeASTFile returns the AST file for the given node.
func (s *Server) nodeASTFile(proj *gop.Project, node gopast.Node) *gopast.File {
	return s.posASTFile(proj, node.Pos())
}

// nodeFilename returns the filename for the given node.
func (s *Server) nodeFilename(proj *gop.Project, node gopast.Node) string {
	return s.posFilename(proj, node.Pos())
}

// defIdentFor returns the identifier where the given object is defined.
func (s *Server) defIdentFor(info *typesutil.Info, obj types.Object) *gopast.Ident {
	if obj == nil {
		return nil
	}
	for ident, o := range info.Defs {
		if o == obj {
			return ident
		}
	}
	return nil
}

// spxDefinitionsForIdent returns all spx definitions for the given identifier.
// It returns multiple definitions only if the identifier is a Go+ overloadable
// function.
func (s *Server) spxDefinitionsForIdent(proj *gop.Project, typeInfo *typesutil.Info, ident *gopast.Ident) []SpxDefinition {
	if ident.Name == "_" {
		return nil
	}
	return s.spxDefinitionsFor(proj, typeInfo, typeInfo.ObjectOf(ident), s.selectorTypeNameForIdent(proj, typeInfo, ident))
}

// spxDefinitionsFor returns all spx definitions for the given object. It
// returns multiple definitions only if the object is a Go+ overloadable
// function.
func (s *Server) spxDefinitionsFor(proj *gop.Project, typeInfo *typesutil.Info, obj types.Object, selectorTypeName string) []SpxDefinition {
	if obj == nil {
		return nil
	}
	if isBuiltinObject(obj) {
		return []SpxDefinition{GetSpxDefinitionForBuiltinObj(obj)}
	}

	var pkgDoc *pkgdoc.PkgDoc
	if pkgPath := util.PackagePath(obj.Pkg()); pkgPath == "main" {
		pkgDoc = getPkgDoc(proj)
	} else {
		pkgDoc, _ = pkgdata.GetPkgDoc(pkgPath)
	}

	switch obj := obj.(type) {
	case *types.Var:
		return []SpxDefinition{GetSpxDefinitionForVar(obj, selectorTypeName, s.isDefinedInFirstVarBlock(proj, typeInfo, obj), pkgDoc)}
	case *types.Const:
		return []SpxDefinition{GetSpxDefinitionForConst(obj, pkgDoc)}
	case *types.TypeName:
		return []SpxDefinition{GetSpxDefinitionForType(obj, pkgDoc)}
	case *types.Func:
		if defIdent := s.defIdentFor(typeInfo, obj); defIdent != nil && goputil.IsShadow(proj, defIdent) {
			return nil
		}
		if isUnexpandableGopOverloadableFunc(obj) {
			return nil
		}
		if funcOverloads := expandGopOverloadableFunc(obj); funcOverloads != nil {
			defs := make([]SpxDefinition, 0, len(funcOverloads))
			for _, funcOverload := range funcOverloads {
				defs = append(defs, GetSpxDefinitionForFunc(funcOverload, selectorTypeName, pkgDoc))
			}
			return defs
		}
		return []SpxDefinition{GetSpxDefinitionForFunc(obj, selectorTypeName, pkgDoc)}
	case *types.PkgName:
		return []SpxDefinition{GetSpxDefinitionForPkg(obj, pkgDoc)}
	}
	return nil
}

// isDefinedInFirstVarBlock reports whether the given object is defined in the
// first var block of an AST file.
func (s *Server) isDefinedInFirstVarBlock(proj *gop.Project, typeInfo *typesutil.Info, obj types.Object) bool {
	defIdent := s.defIdentFor(typeInfo, obj)
	if defIdent == nil {
		return false
	}
	astFile := s.nodeASTFile(proj, defIdent)
	if astFile == nil {
		return false
	}
	firstVarBlock := astFile.ClassFieldsDecl()
	if firstVarBlock == nil {
		return false
	}
	return defIdent.Pos() >= firstVarBlock.Pos() && defIdent.End() <= firstVarBlock.End()
}

// selectorTypeNameForIdent returns the selector type name for the given
// identifier. It returns empty string if no selector can be inferred.
func (s *Server) selectorTypeNameForIdent(proj *gop.Project, typeInfo *typesutil.Info, ident *gopast.Ident) string {
	astFile := s.nodeASTFile(proj, ident)
	if astFile == nil {
		return ""
	}

	if path, _ := util.PathEnclosingInterval(astFile, ident.Pos(), ident.End()); len(path) > 0 {
		for _, node := range slices.Backward(path) {
			sel, ok := node.(*gopast.SelectorExpr)
			if !ok {
				continue
			}
			tv, ok := typeInfo.Types[sel.X]
			if !ok {
				continue
			}

			switch typ := unwrapPointerType(tv.Type).(type) {
			case *types.Named:
				obj := typ.Obj()
				typeName := obj.Name()
				if isSpxPkgObject(obj) && typeName == "SpriteImpl" {
					typeName = "Sprite"
				}
				return typeName
			case *types.Interface:
				if typ.String() == "interface{}" {
					return ""
				}
				return typ.String()
			}
		}
	}

	obj := typeInfo.ObjectOf(ident)
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	if isSpxPkgObject(obj) {
		astFileScope := typeInfo.Scopes[astFile]
		innermostScope := s.innermostScopeAt(proj, typeInfo, ident.Pos())
		if innermostScope == astFileScope || (astFile.HasShadowEntry() && s.innermostScopeAt(proj, typeInfo, astFile.ShadowEntry.Pos()) == innermostScope) {
			spxFile := s.nodeFilename(proj, ident)
			if spxFileBaseName := path.Base(spxFile); spxFileBaseName == "main.spx" {
				return "Game"
			}
			return "Sprite"
		}
	}
	switch obj := obj.(type) {
	case *types.Var:
		if !obj.IsField() {
			return ""
		}

		for _, def := range typeInfo.Defs {
			if def == nil {
				continue
			}
			named, ok := unwrapPointerType(def.Type()).(*types.Named)
			if !ok || named.Obj().Pkg() != obj.Pkg() || !isNamedStructType(named) {
				continue
			}

			var typeName string
			walkStruct(named, func(member types.Object, selector *types.Named) bool {
				if field, ok := member.(*types.Var); ok && field == obj {
					typeName = selector.Obj().Name()
					return false
				}
				return true
			})
			if isSpxPkgObject(obj) && typeName == "SpriteImpl" {
				typeName = "Sprite"
			}
			if typeName != "" {
				return typeName
			}
		}
	case *types.Func:
		recv := obj.Type().(*types.Signature).Recv()
		if recv == nil {
			return ""
		}

		switch typ := unwrapPointerType(recv.Type()).(type) {
		case *types.Named:
			obj := typ.Obj()
			typeName := obj.Name()
			if isSpxPkgObject(obj) && typeName == "SpriteImpl" {
				typeName = "Sprite"
			}
			return typeName
		case *types.Interface:
			if typ.String() == "interface{}" {
				return ""
			}
			return typ.String()
		}
	}
	return ""
}

// innermostScopeAt returns the innermost scope that contains the given
// position. It returns nil if not found.
func (s *Server) innermostScopeAt(proj *gop.Project, typeInfo *typesutil.Info, pos goptoken.Pos) *types.Scope {
	fileScope := typeInfo.Scopes[s.posASTFile(proj, pos)]
	if fileScope == nil {
		return nil
	}
	innermostScope := fileScope
	for _, scope := range typeInfo.Scopes {
		if scope.Contains(pos) && fileScope.Contains(scope.Pos()) && innermostScope.Contains(scope.Pos()) {
			innermostScope = scope
		}
	}
	return innermostScope
}
