package server

import (
	"net/url"
	"slices"
	"strings"

	"golang.org/x/text/language"

	"github.com/goplus/xgolsw/i18n"
	"github.com/goplus/xgolsw/protocol"
)

// initialize handles the initialize request and sets up the server capabilities
// and client-derived configuration.
func (s *Server) initialize(params *InitializeParams) (*InitializeResult, error) {
	s.setClientCapabilities(params.Capabilities)
	s.setLanguageFromLocale(params.Locale)
	s.setWorkspaceRootURI(params)

	return &InitializeResult{
		Capabilities: serverCapabilities(params),
		ServerInfo: &ServerInfo{
			Name:    "XGo Language Server",
			Version: "0.1.0",
		},
	}, nil
}

// serverCapabilities returns the LSP capabilities this server can safely
// advertise for the given initialize params.
func serverCapabilities(params *InitializeParams) ServerCapabilities {
	positionEncoding := protocol.UTF16
	textDocument := params.Capabilities.TextDocument
	capabilities := ServerCapabilities{
		PositionEncoding: &positionEncoding,
		TextDocumentSync: protocol.TextDocumentSyncOptions{
			OpenClose: true,
			Change:    protocol.Incremental,
			Save:      &protocol.SaveOptions{},
		},
		CompletionProvider: &protocol.CompletionOptions{
			TriggerCharacters: []string{"."},
		},
		HoverProvider: &protocol.Or_ServerCapabilities_hoverProvider{Value: true},
		SignatureHelpProvider: &protocol.SignatureHelpOptions{
			TriggerCharacters: []string{"(", ","},
		},
		DeclarationProvider:       &protocol.Or_ServerCapabilities_declarationProvider{Value: true},
		DefinitionProvider:        &protocol.Or_ServerCapabilities_definitionProvider{Value: true},
		TypeDefinitionProvider:    &protocol.Or_ServerCapabilities_typeDefinitionProvider{Value: true},
		ImplementationProvider:    &protocol.Or_ServerCapabilities_implementationProvider{Value: true},
		ReferencesProvider:        &protocol.Or_ServerCapabilities_referencesProvider{Value: true},
		DocumentHighlightProvider: &protocol.Or_ServerCapabilities_documentHighlightProvider{Value: true},
		DocumentLinkProvider:      &protocol.DocumentLinkOptions{},
		DiagnosticProvider: &protocol.Or_ServerCapabilities_diagnosticProvider{
			Value: protocol.DiagnosticOptions{InterFileDependencies: true, WorkspaceDiagnostics: true},
		},
		DocumentFormattingProvider: &protocol.Or_ServerCapabilities_documentFormattingProvider{Value: true},
		RenameProvider:             true,
		ExecuteCommandProvider: &protocol.ExecuteCommandOptions{
			Commands: []string{
				CommandXGoRenameResources,
				CommandSpxRenameResources,
				CommandXGoGetInputSlots,
				CommandSpxGetInputSlots,
				CommandXGoGetProperties,
			},
		},
		InlayHintProvider: protocol.InlayHintOptions{},
	}
	if textDocument.Rename != nil && textDocument.Rename.PrepareSupport {
		capabilities.RenameProvider = protocol.RenameOptions{PrepareProvider: true}
	}
	if textDocument.SignatureHelp != nil && textDocument.SignatureHelp.ContextSupport {
		capabilities.SignatureHelpProvider.RetriggerCharacters = []string{","}
	}
	if provider, ok := semanticTokensServerCapabilities(textDocument.SemanticTokens); ok {
		capabilities.SemanticTokensProvider = provider
	}
	return capabilities
}

// semanticTokensServerCapabilities returns semantic token options supported by
// both client and server.
func semanticTokensServerCapabilities(client protocol.SemanticTokensClientCapabilities) (protocol.SemanticTokensOptions, bool) {
	if !semanticTokensFullSupported(client) ||
		!slices.Contains(client.Formats, protocol.Relative) ||
		!client.OverlappingTokenSupport ||
		!client.MultilineTokenSupport {
		return protocol.SemanticTokensOptions{}, false
	}

	tokenTypes := semanticTokenTypesLegendStrings()
	if !semanticTokenLegendSupported(client.TokenTypes, tokenTypes) {
		return protocol.SemanticTokensOptions{}, false
	}
	tokenModifiers := semanticTokenModifiersLegendStrings()
	if !semanticTokenLegendSupported(client.TokenModifiers, tokenModifiers) {
		return protocol.SemanticTokensOptions{}, false
	}
	return protocol.SemanticTokensOptions{
		Legend: protocol.SemanticTokensLegend{
			TokenTypes:     tokenTypes,
			TokenModifiers: tokenModifiers,
		},
		Full: &protocol.Or_SemanticTokensOptions_full{Value: true},
	}, true
}

// semanticTokensFullSupported reports whether the client can send full semantic
// token requests when the server advertises them.
func semanticTokensFullSupported(client protocol.SemanticTokensClientCapabilities) bool {
	if client.Requests.Full == nil {
		return false
	}
	switch full := client.Requests.Full.Value.(type) {
	case bool:
		return full
	case protocol.ClientSemanticTokensRequestFullDelta:
		return true
	default:
		return false
	}
}

// semanticTokenLegendSupported reports whether supported contains every legend value.
func semanticTokenLegendSupported(supported []string, legend []string) bool {
	for _, value := range legend {
		if !slices.Contains(supported, value) {
			return false
		}
	}
	return true
}

// setWorkspaceRootURI sets the server workspace root from initialize params.
func (s *Server) setWorkspaceRootURI(params *InitializeParams) {
	rootURI := workspaceRootURI(params)
	if rootURI == "" {
		return
	}
	s.workspaceRootURI = DocumentURI(ensureTrailingSlash(rootURI))
}

// workspaceRootURI returns the root URI selected from initialize params.
func workspaceRootURI(params *InitializeParams) string {
	if len(params.WorkspaceFolders) > 0 && params.WorkspaceFolders[0].URI != "" {
		return string(params.WorkspaceFolders[0].URI)
	}
	if params.RootURI != "" {
		return string(params.RootURI)
	}
	if params.RootPath != "" {
		return fileURIFromPath(params.RootPath)
	}
	return ""
}

// fileURIFromPath returns a file URI for a filesystem path.
func fileURIFromPath(path string) string {
	path = strings.ReplaceAll(path, "\\", "/")
	if strings.HasPrefix(path, "//") {
		host, rest, ok := strings.Cut(strings.TrimPrefix(path, "//"), "/")
		if ok && host != "" {
			return (&url.URL{Scheme: "file", Host: host, Path: "/" + rest}).String()
		}
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

// ensureTrailingSlash returns uri with a trailing slash.
func ensureTrailingSlash(uri string) string {
	if strings.HasSuffix(uri, "/") {
		return uri
	}
	return uri + "/"
}

// setLanguageFromLocale sets the server language based on the client locale.
func (s *Server) setLanguageFromLocale(locale string) {
	s.language = i18n.LanguageEN

	tag, err := language.Parse(locale)
	if err != nil {
		return
	}

	base, _ := tag.Base()
	chineseBase, _ := language.Chinese.Base()
	if base == chineseBase {
		s.language = i18n.LanguageCN
	}
}

// translate translates a diagnostic message based on the server's current language.
func (s *Server) translate(message string) string {
	return i18n.Translate(message, s.language)
}
