package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"

	"github.com/goplus/xgolsw/protocol"
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
	InitializeResult     = protocol.InitializeResult
	ServerCapabilities   = protocol.ServerCapabilities
	ServerInfo           = protocol.ServerInfo
	InitializedParams    = protocol.InitializedParams
	ExecuteCommandParams = protocol.ExecuteCommandParams
	CancelParams         = protocol.CancelParams

	DidOpenTextDocumentParams   = protocol.DidOpenTextDocumentParams
	DidChangeTextDocumentParams = protocol.DidChangeTextDocumentParams
	DidCloseTextDocumentParams  = protocol.DidCloseTextDocumentParams
	DidSaveTextDocumentParams   = protocol.DidSaveTextDocumentParams

	InlayHintParams = protocol.InlayHintParams
	InlayHint       = protocol.InlayHint
	InlayHintKind   = protocol.InlayHintKind
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

	Type      = protocol.Type
	Parameter = protocol.Parameter

	RequestCancelled = protocol.RequestCancelled
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

// XGoRenameResourceParams represents parameters to rename an XGo resource in
// the workspace. The bundled spx DSL emits spx-specific resource identifiers.
type XGoRenameResourceParams struct {
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
	return fmt.Sprintf("<resource-preview resource=%q />\n", template.HTMLEscapeString(string(u)))
}

// SpxResourceContextURI represents a URI for resource context.
// Examples:
// - `spx://resources/sprites`
// - `spx://resources/sounds`
// - `spx://resources/sprites/<sName>/costumes`
type SpxResourceContextURI string

// XGoGetDefinitionsParams represents parameters to get definitions at a
// specific position in a document.
type XGoGetDefinitionsParams struct {
	// The text document position params.
	protocol.TextDocumentPositionParams
}

// XGoDefinitionIdentifier identifies an XGo definition.
type XGoDefinitionIdentifier struct {
	// Full name of source package.
	// If not provided, it's assumed to be kind-statement.
	// If `main`, it's the current user package.
	// Examples:
	// - `fmt`
	// - `github.com/goplus/spx/v2`
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
func (id XGoDefinitionIdentifier) String() string {
	s := "xgo:"
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

// XGoGetInputSlotsParams represents parameters to get input slots for a
// specific document.
type XGoGetInputSlotsParams struct {
	// The text document indentifier.
	TextDocument protocol.TextDocumentIdentifier `json:"textDocument"`
}

// XGoInputSlot represents a modifiable item in the code.
type XGoInputSlot struct {
	Kind            XGoInputSlotKind   `json:"kind"`
	Accept          XGoInputSlotAccept `json:"accept"`
	Input           XGoInput           `json:"input"`
	PredefinedNames []string           `json:"predefinedNames"`
	Range           Range              `json:"range"`
}

// XGoInputSlotKind represents the kind of input slot.
type XGoInputSlotKind string

// XGoInputSlotKind constants.
const (
	// XGoInputSlotKindValue slot accepts value, which may be an in-place value or a predefined identifier.
	XGoInputSlotKindValue XGoInputSlotKind = "value"

	// XGoInputSlotKindAddress slot accepts address, which must be a predefined identifier.
	XGoInputSlotKindAddress XGoInputSlotKind = "address"
)

const (
	// Deprecated: use XGoInputSlotKindValue.
	SpxInputSlotKindValue = XGoInputSlotKindValue
	// Deprecated: use XGoInputSlotKindAddress.
	SpxInputSlotKindAddress = XGoInputSlotKindAddress
)

// XGoInputSlotAccept represents info about what inputs are accepted by a slot.
type XGoInputSlotAccept struct {
	// Type of input accepted by the slot.
	Type SpxInputType `json:"type"`

	// Resource context for SpxInputTypeResourceName.
	// Only valid when Type is SpxInputTypeResourceName.
	ResourceContext *SpxResourceContextURI `json:"resourceContext,omitempty"`
}

// SpxInputType represents the type of input for a slot.
type SpxInputType string

// SpxInputType constants.
const (
	SpxInputTypeInteger       SpxInputType = "integer"
	SpxInputTypeDecimal       SpxInputType = "decimal"
	SpxInputTypeString        SpxInputType = "string"
	SpxInputTypeBoolean       SpxInputType = "boolean"
	SpxInputTypeResourceName  SpxInputType = "spx-resource-name"
	SpxInputTypeDirection     SpxInputType = "spx-direction"
	SpxInputTypeLayerAction   SpxInputType = "spx-layer-action"
	SpxInputTypeDirAction     SpxInputType = "spx-dir-action"
	SpxInputTypeColor         SpxInputType = "spx-color"
	SpxInputTypeEffectKind    SpxInputType = "spx-effect-kind"
	SpxInputTypeKey           SpxInputType = "spx-key"
	SpxInputTypeSpecialObj    SpxInputType = "spx-special-obj"
	SpxInputTypeRotationStyle SpxInputType = "spx-rotation-style"
	SpxInputTypeUnknown       SpxInputType = "unknown"
)

// SpxInputTypeSpxColorConstructor represents the name for color constructors.
type SpxInputTypeSpxColorConstructor string

// SpxInputTypeSpxColorConstructor constants.
const (
	SpxInputTypeSpxColorConstructorHSB  SpxInputTypeSpxColorConstructor = "HSB"
	SpxInputTypeSpxColorConstructorHSBA SpxInputTypeSpxColorConstructor = "HSBA"
)

// SpxColorInputValue represents the value structure for an [SpxInput] when its
// type is [SpxInputTypeColor] and kind is [SpxInputKindInPlace].
type SpxColorInputValue struct {
	Constructor SpxInputTypeSpxColorConstructor `json:"constructor"`
	Args        []float64                       `json:"args"`
}

// XGoInput represents the current input in a slot.
type XGoInput struct {
	Kind  XGoInputKind `json:"kind"`
	Type  SpxInputType `json:"type"`
	Value any          `json:"value,omitempty"` // For InPlace kind
	Name  string       `json:"name,omitempty"`  // For Predefined kind
}

// XGoInputKind represents the kind of input.
type XGoInputKind string

// XGoInputKind constants.
const (
	// XGoInputKindInPlace in-place value like "hello world", 123, true, etc.
	XGoInputKindInPlace XGoInputKind = "in-place"

	// XGoInputKindPredefined reference to user predefined identifier.
	XGoInputKindPredefined XGoInputKind = "predefined"
)

const (
	// Deprecated: use XGoInputKindInPlace.
	SpxInputKindInPlace = XGoInputKindInPlace
	// Deprecated: use XGoInputKindPredefined.
	SpxInputKindPredefined = XGoInputKindPredefined
)

// XGoResourceRefDocumentLinkData represents data for an XGo resource reference
// document link.
type XGoResourceRefDocumentLinkData struct {
	// The kind of the spx resource reference.
	Kind SpxResourceRefKind `json:"kind"`
}

// XGoCompletionItemData represents data in a completion item.
type XGoCompletionItemData struct {
	// The corresponding definition of the completion item.
	Definition *XGoDefinitionIdentifier `json:"definition,omitempty"`
}

// Deprecated: use XGoRenameResourceParams.
type SpxRenameResourceParams = XGoRenameResourceParams

// Deprecated: use XGoGetDefinitionsParams.
type SpxGetDefinitionsParams = XGoGetDefinitionsParams

// Deprecated: use XGoDefinitionIdentifier.
type SpxDefinitionIdentifier = XGoDefinitionIdentifier

// Deprecated: use XGoGetInputSlotsParams.
type SpxGetInputSlotsParams = XGoGetInputSlotsParams

// Deprecated: use XGoInputSlot.
type SpxInputSlot = XGoInputSlot

// Deprecated: use XGoInputSlotKind.
type SpxInputSlotKind = XGoInputSlotKind

// Deprecated: use XGoInputSlotAccept.
type SpxInputSlotAccept = XGoInputSlotAccept

// Deprecated: use XGoInput.
type SpxInput = XGoInput

// Deprecated: use XGoInputKind.
type SpxInputKind = XGoInputKind

// Deprecated: use XGoResourceRefDocumentLinkData.
type SpxResourceRefDocumentLinkData = XGoResourceRefDocumentLinkData

// Deprecated: use XGoCompletionItemData.
type CompletionItemData = XGoCompletionItemData
