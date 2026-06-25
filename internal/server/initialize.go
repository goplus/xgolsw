package server

import (
	"strings"

	"golang.org/x/text/language"

	"github.com/goplus/xgolsw/i18n"
	"github.com/goplus/xgolsw/protocol"
)

// initialize handles the initialize request and sets up the server capabilities
// and client-derived configuration.
func (s *Server) initialize(params *InitializeParams) (*InitializeResult, error) {
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

// serverCapabilities returns the static LSP capabilities supported by this
// server's currently registered request and notification handlers.
func serverCapabilities(params *InitializeParams) ServerCapabilities {
	positionEncoding := protocol.UTF16
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
		SemanticTokensProvider: protocol.SemanticTokensOptions{
			Legend: protocol.SemanticTokensLegend{
				TokenTypes:     semanticTokenTypes(),
				TokenModifiers: semanticTokenModifiers(),
			},
			Full: &protocol.Or_SemanticTokensOptions_full{Value: true},
		},
		InlayHintProvider: protocol.InlayHintOptions{},
	}
	if params.Capabilities.TextDocument.Rename != nil && params.Capabilities.TextDocument.Rename.PrepareSupport {
		capabilities.RenameProvider = protocol.RenameOptions{PrepareProvider: true}
	}
	if params.Capabilities.TextDocument.SignatureHelp != nil &&
		params.Capabilities.TextDocument.SignatureHelp.ContextSupport {
		capabilities.SignatureHelpProvider.RetriggerCharacters = []string{","}
	}
	return capabilities
}

// semanticTokenTypes returns the semantic token type legend in LSP wire form.
func semanticTokenTypes() []string {
	tokenTypes := make([]string, len(semanticTokenTypesLegend))
	for i, tokenType := range semanticTokenTypesLegend {
		tokenTypes[i] = string(tokenType)
	}
	return tokenTypes
}

// semanticTokenModifiers returns the semantic token modifier legend in LSP wire
// form.
func semanticTokenModifiers() []string {
	tokenModifiers := make([]string, len(semanticTokenModifiersLegend))
	for i, tokenModifier := range semanticTokenModifiersLegend {
		tokenModifiers[i] = string(tokenModifier)
	}
	return tokenModifiers
}

// setWorkspaceRootURI sets the server workspace root from initialize params.
func (s *Server) setWorkspaceRootURI(params *InitializeParams) {
	var rootURI string
	if len(params.WorkspaceFolders) > 0 {
		rootURI = string(params.WorkspaceFolders[0].URI)
	} else {
		rootURI = string(params.RootURI)
	}
	if rootURI == "" {
		return
	}
	s.workspaceRootURI = DocumentURI(ensureTrailingSlash(rootURI))
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
