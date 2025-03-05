package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/goplus/goxlsw/protocol"
)

type (
	URI         = protocol.URI
	DocumentURI = protocol.DocumentURI
	Position    = protocol.Position
	Range       = protocol.Range
	Location    = protocol.Location

	TextEdit      = protocol.TextEdit
	WorkspaceEdit = protocol.WorkspaceEdit

	TextDocumentPositionParams = protocol.TextDocumentPositionParams
	TextDocumentIdentifier     = protocol.TextDocumentIdentifier

	InsertTextFormat = protocol.InsertTextFormat

	MarkupContent = protocol.MarkupContent

	DocumentHighlightParams = protocol.DocumentHighlightParams
	DocumentHighlight       = protocol.DocumentHighlight

	DocumentFormattingParams = protocol.DocumentFormattingParams

	PrepareRenameParams = protocol.PrepareRenameParams
	RenameParams        = protocol.RenameParams

	Diagnostic                            = protocol.Diagnostic
	DocumentDiagnosticParams              = protocol.DocumentDiagnosticParams
	WorkspaceDiagnosticParams             = protocol.WorkspaceDiagnosticParams
	DocumentDiagnosticReport              = protocol.DocumentDiagnosticReport
	FullDocumentDiagnosticReport          = protocol.FullDocumentDiagnosticReport
	RelatedFullDocumentDiagnosticReport   = protocol.RelatedFullDocumentDiagnosticReport
	WorkspaceDiagnosticReport             = protocol.WorkspaceDiagnosticReport
	WorkspaceDocumentDiagnosticReport     = protocol.WorkspaceDocumentDiagnosticReport
	WorkspaceFullDocumentDiagnosticReport = protocol.WorkspaceFullDocumentDiagnosticReport
	PublishDiagnosticsParams              = protocol.PublishDiagnosticsParams

	CompletionParams                = protocol.CompletionParams
	CompletionItemKind              = protocol.CompletionItemKind
	CompletionItem                  = protocol.CompletionItem
	Or_CompletionItem_documentation = protocol.Or_CompletionItem_documentation

	DocumentLinkParams = protocol.DocumentLinkParams
	DocumentLink       = protocol.DocumentLink

	DeclarationParams    = protocol.DeclarationParams
	DefinitionParams     = protocol.DefinitionParams
	TypeDefinitionParams = protocol.TypeDefinitionParams

	ReferenceParams  = protocol.ReferenceParams
	ReferenceContext = protocol.ReferenceContext

	HoverParams = protocol.HoverParams
	Hover       = protocol.Hover

	ImplementationParams = protocol.ImplementationParams

	SemanticTokenTypes     = protocol.SemanticTokenTypes
	SemanticTokenModifiers = protocol.SemanticTokenModifiers
	SemanticTokensParams   = protocol.SemanticTokensParams
	SemanticTokens         = protocol.SemanticTokens

	SignatureHelpParams  = protocol.SignatureHelpParams
	SignatureHelp        = protocol.SignatureHelp
	SignatureInformation = protocol.SignatureInformation
	ParameterInformation = protocol.ParameterInformation

	InitializeParams     = protocol.InitializeParams
	InitializedParams    = protocol.InitializedParams
	ExecuteCommandParams = protocol.ExecuteCommandParams

	DidOpenTextDocumentParams   = protocol.DidOpenTextDocumentParams
	DidChangeTextDocumentParams = protocol.DidChangeTextDocumentParams
	DidCloseTextDocumentParams  = protocol.DidCloseTextDocumentParams
	DidSaveTextDocumentParams   = protocol.DidSaveTextDocumentParams
)

const (
	SeverityError   = protocol.SeverityError
	SeverityWarning = protocol.SeverityWarning

	TextCompletion      = protocol.TextCompletion
	ClassCompletion     = protocol.ClassCompletion
	InterfaceCompletion = protocol.InterfaceCompletion
	StructCompletion    = protocol.StructCompletion
	VariableCompletion  = protocol.VariableCompletion
	ConstantCompletion  = protocol.ConstantCompletion
	KeywordCompletion   = protocol.KeywordCompletion
	FieldCompletion     = protocol.FieldCompletion
	MethodCompletion    = protocol.MethodCompletion
	FunctionCompletion  = protocol.FunctionCompletion
	ModuleCompletion    = protocol.ModuleCompletion

	DiagnosticFull = protocol.DiagnosticFull

	Markdown = protocol.Markdown
	Text     = protocol.Text

	Write = protocol.Write
	Read  = protocol.Read

	PlainTextTextFormat = protocol.PlainTextTextFormat
	SnippetTextFormat   = protocol.SnippetTextFormat

	NamespaceType = protocol.NamespaceType
	TypeType      = protocol.TypeType
	InterfaceType = protocol.InterfaceType
	StructType    = protocol.StructType
	EnumType      = protocol.EnumType
	EnumMember    = protocol.EnumMember
	VariableType  = protocol.VariableType
	ParameterType = protocol.ParameterType
	FunctionType  = protocol.FunctionType
	MethodType    = protocol.MethodType
	PropertyType  = protocol.PropertyType
	KeywordType   = protocol.KeywordType
	CommentType   = protocol.CommentType
	StringType    = protocol.StringType
	NumberType    = protocol.NumberType
	OperatorType  = protocol.OperatorType
	LabelType     = protocol.LabelType

	ModDeclaration    = protocol.ModDeclaration
	ModReadonly       = protocol.ModReadonly
	ModStatic         = protocol.ModStatic
	ModDefinition     = protocol.ModDefinition
	ModDefaultLibrary = protocol.ModDefaultLibrary
)

// UnmarshalJSON unmarshals msg into the variable pointed to by params.
// In JSONRPC, optional messages may be "null", in which case it is a no-op.
func UnmarshalJSON(msg json.RawMessage, v any) error {
	if len(msg) == 0 || bytes.Equal(msg, []byte("null")) {
		return nil
	}
	return json.Unmarshal(msg, v)
}

// toURI converts a string to a [URI].
func toURI(s string) *URI {
	u := URI(s)
	return &u
}

// SpxRenameResourceParams represents parameters to rename an spx resource in
// the workspace.
type SpxRenameResourceParams struct {
	// The spx resource.
	Resource SpxResourceIdentifier `json:"resource"`
	// The new name of the spx resource.
	NewName string `json:"newName"`
}

// SpxResourceIdentifier identifies an spx resource.
type SpxResourceIdentifier struct {
	// The spx resource's URI.
	URI SpxResourceURI `json:"uri"`
}

// SpxResourceURI represents a URI string for an spx resource.
type SpxResourceURI string

// HTML returns the HTML representation of the spx resource URI.
func (u SpxResourceURI) HTML() string {
	return fmt.Sprintf("<resource-preview resource=%s />\n", attr(string(u)))
}

// SpxGetDefinitionsParams represents parameters to get definitions at a
// specific position in a document.
type SpxGetDefinitionsParams struct {
	// The text document position params.
	protocol.TextDocumentPositionParams
}

// SpxDefinitionIdentifier identifies an spx definition.
type SpxDefinitionIdentifier struct {
	// Full name of source package.
	// If not provided, it's assumed to be kind-statement.
	// If `main`, it's the current user package.
	// Examples:
	// - `fmt`
	// - `github.com/goplus/spx`
	// - `main`
	Package *string `json:"package,omitempty"`

	// Exported name of the definition.
	// If not provided, it's assumed to be kind-package.
	// Examples:
	// - `Println`
	// - `Sprite`
	// - `Sprite.turn`
	// - `for_statement_with_single_condition`
	Name *string `json:"name,omitempty"`

	// Overload Identifier.
	OverloadID *string `json:"overloadId,omitempty"`
}

// String implements [fmt.Stringer].
func (id SpxDefinitionIdentifier) String() string {
	s := "gop:"
	if id.Package != nil {
		s += *id.Package
	}
	if id.Name != nil {
		s += "?" + url.QueryEscape(*id.Name)
		if id.OverloadID != nil {
			s += "#" + url.QueryEscape(*id.OverloadID)
		}
	}
	return s
}

// SpxResourceRefDocumentLinkData represents data for an spx resource reference
// document link.
type SpxResourceRefDocumentLinkData struct {
	// The kind of the spx resource reference.
	Kind SpxResourceRefKind `json:"kind"`
}

// CompletionItemData represents data in a completion item.
type CompletionItemData struct {
	// The corresponding definition of the completion item.
	Definition *SpxDefinitionIdentifier `json:"definition,omitempty"`
}
