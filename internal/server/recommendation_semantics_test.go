package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotRecommendedExplicitSymbolServices(t *testing.T) {
	for _, tt := range []struct {
		name        string
		declaration string
		use         string
		symbol      string
	}{
		{
			name: "Function", symbol: "report",
			declaration: `// report documents an implementation detail.
//xgols:notrecommended
func |report(value int) int { return value }
`,
			use: "println |report(2)\n",
		},
		{
			name: "Method", symbol: "report",
			declaration: `type Record struct{}
// report documents an implementation detail.
//xgols:notrecommended
func (r Record) |report(value int) int { return value }
`,
			use: "var record Record\nprintln record.|report(2)\n",
		},
		{
			name: "Variable", symbol: "counter",
			declaration: `// counter documents an implementation detail.
//xgols:notrecommended
var |counter int
`,
			use: "println |counter\n",
		},
		{
			name: "Constant", symbol: "limit",
			declaration: `// limit documents an implementation detail.
//xgols:notrecommended
const |limit = 2
`,
			use: "println |limit\n",
		},
		{
			name: "Type", symbol: "Record",
			declaration: `// Record documents an implementation detail.
//xgols:notrecommended
type |Record struct { Value int }
`,
			use: "var record |Record\nprintln record.Value\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			declaration, declarationPosition := typeDisplayTestSource(t, tt.declaration)
			source, position := typeDisplayTestSource(t, tt.use)
			s := newTestServer(t, map[string][]byte{
				"declarations.xgo": []byte(declaration),
				"main.xgo":         []byte(source),
			})
			requireNoDiagnostics(t, s)
			params := TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
			}

			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, tt.symbol+" documents an implementation detail.")
			assert.NotContains(t, hover.Contents.Value, "xgols:notrecommended")

			declarationEnd := declarationPosition
			declarationEnd.Character += uint32(UTF16Len(tt.symbol))
			wantDeclaration := Location{URI: "file:///declarations.xgo", Range: Range{Start: declarationPosition, End: declarationEnd}}
			definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			assert.Equal(t, wantDeclaration, requireValueAs[Location](t, definition))

			useEnd := position
			useEnd.Character += uint32(UTF16Len(tt.symbol))
			wantUse := Location{URI: "file:///main.xgo", Range: Range{Start: position, End: useEnd}}
			references, err := s.textDocumentReferences(&ReferenceParams{
				TextDocumentPositionParams: params,
				Context:                    ReferenceContext{IncludeDeclaration: true},
			})
			require.NoError(t, err)
			assert.ElementsMatch(t, []Location{wantDeclaration, wantUse}, references)

			edit, err := s.textDocumentRename(&RenameParams{
				TextDocument: params.TextDocument, Position: position, NewName: "renamed",
			})
			require.NoError(t, err)
			require.NotNil(t, edit)
			assert.Equal(t, []TextEdit{{Range: wantDeclaration.Range, NewText: "renamed"}}, edit.Changes[wantDeclaration.URI])
			assert.Equal(t, []TextEdit{{Range: wantUse.Range, NewText: "renamed"}}, edit.Changes[wantUse.URI])
			updated := applyResourceRenameTestEdits(t, declaration, edit.Changes[wantDeclaration.URI])
			assert.Contains(t, updated, "//xgols:notrecommended")
			s.ModifyFiles([]FileChange{
				{Path: "declarations.xgo", Content: []byte(updated), Version: 1},
				{Path: "main.xgo", Content: []byte(applyResourceRenameTestEdits(t, source, edit.Changes[wantUse.URI])), Version: 1},
			})
			requireNoDiagnostics(t, s)
		})
	}
}

func TestNotRecommendedExplicitCallAssistance(t *testing.T) {
	for _, tt := range []struct {
		name        string
		declaration string
		call        string
		label       string
	}{
		{
			name: "Function", label: "report(value int)",
			declaration: `// report documents an implementation detail.
//xgols:notrecommended
func report(value int) {}
`,
			call: "report |2\n",
		},
		{
			name: "Method", label: "report(value int)",
			declaration: `type Record struct{}
// report documents an implementation detail.
//xgols:notrecommended
func (r Record) report(value int) {}
`,
			call: "var record Record\nrecord.report |2\n",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, position := typeDisplayTestSource(t, tt.call)
			s := newTestServer(t, map[string][]byte{
				"declarations.xgo": []byte(tt.declaration),
				"main.xgo":         []byte(source),
			})
			requireNoDiagnostics(t, s)
			params := TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: position,
			}
			help, err := s.textDocumentSignatureHelp(&SignatureHelpParams{TextDocumentPositionParams: params})
			require.NoError(t, err)
			require.NotNil(t, help)
			require.Len(t, help.Signatures, 1)
			assert.Equal(t, tt.label, help.Signatures[0].Label)
			require.Len(t, help.Signatures[0].Parameters, 1)
			assert.Equal(t, "value int", help.Signatures[0].Parameters[0].Label)

			hints, err := s.textDocumentInlayHint(&InlayHintParams{
				TextDocument: params.TextDocument,
				Range:        Range{End: Position{Line: uint32(strings.Count(source, "\n"))}},
			})
			require.NoError(t, err)
			assert.Contains(t, hints, InlayHint{Position: position, Label: "value", Kind: Parameter})
		})
	}
}

func TestNotRecommendedExplicitInput(t *testing.T) {
	s := newTestServer(t, map[string][]byte{
		"declarations.xgo": []byte("//xgols:notrecommended\nvar counter int\n"),
		"main.xgo":         []byte("println counter\n"),
	})
	requireNoDiagnostics(t, s)
	slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}}})
	require.NoError(t, err)
	require.Len(t, slots, 1)
	assert.Equal(t, XGoInput{Kind: XGoInputKindPredefined, Type: XGoInputTypeInteger, Name: "counter"}, slots[0].Input)
	assert.NotContains(t, slots[0].PredefinedNames, "counter")
}
