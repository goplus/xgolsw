package server

import (
	gotypes "go/types"
	"slices"
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerResourceAutoPropertyContexts(t *testing.T) {
	for _, tt := range []struct{ name, declarations, body string }{
		{"Comparison", "var saved Asset\nfunc Current() Asset { return saved }", `_ = current == "first"`},
		{"Switch", "var saved Asset\nfunc Current() Asset { return saved }", `switch current { case "first": }`},
		{"Index", "func Lookup() map[Asset]int { return nil }", `_ = lookup["first"]`},
		{"Send", "func Stream() chan Asset { return nil }", `stream <- "first"`},
		{"Member", "type Record struct { Value Asset }\nfunc (r Record) Current() Asset { return r.Value }\nvar record Record", `_ = record.current == "first"`},
		{"FunctionVariable", "var saved Asset\nvar Current = func() Asset { return saved }", `_ = current == "first"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, file := range []struct{ name, filename string }{
				{"PlainXGo", "main.xgo"},
				{"RegisteredClassfile", "main_fixture.gox"},
			} {
				t.Run(file.name, func(t *testing.T) {
					filename := file.filename
					source := tt.body + "\n"
					s := newImportTestServer(t, map[string][]byte{filename: []byte(source), "types.xgo": []byte("type Asset string\n" + tt.declarations + "\n")})
					proj := s.requestProject()
					_, err := proj.TypeInfo()
					require.NoError(t, err)
					id := testResourceID{"files", "first"}
					result := newTestResourceAnalysis(id)
					for ref := range resourceReferences(proj, testResourceResolver(t, proj)) {
						result.addResourceRef(ref)
					}
					require.Len(t, result.resourceRefs, 1)
					ref := result.resourceRefs[0]
					assert.Equal(t, id, ref.ID)
					assert.Equal(t, XGoResourceRefKindStringLiteral, ref.Kind)
					links := result.resourceDocumentLinks(proj, filename)
					require.Len(t, links, 1)
					assert.Equal(t, ToPtr(URI(id.URI())), links[0].Target)
					assert.Equal(t, RangeForNode(proj, ref.Node), links[0].Range)

					changes, err := s.renameResourcesAtRefs(proj, result, map[resourceID]string{id: "renamed"})
					require.NoError(t, err)
					uri := s.toDocumentURI(filename)
					updated := applyResourceRenameTestEdits(t, source, changes[uri])
					assert.Equal(t, strings.Replace(source, "first", "renamed", 1), updated)
					s.ModifyFiles([]FileChange{{Path: filename, Content: []byte(updated), Version: 1}})
					next := s.requestProject()
					_, err = next.TypeInfo()
					require.NoError(t, err)
					nextResult := newTestResourceAnalysis(id)
					for ref := range resourceReferences(next, testResourceResolver(t, next)) {
						nextResult.addResourceRef(ref)
						if !nextResult.contains(ref.ID) {
							addResourceDiagnostic(next, nextResult, ref.Node, "resource not found")
						}
					}
					require.Len(t, nextResult.resourceRefs, 1)
					assert.Equal(t, "renamed", nextResult.resourceRefs[0].ID.Name())
					assert.Empty(t, nextResult.resourceDocumentLinks(next, filename))
					diagnostics := resourceDiagnostics(s, nextResult)
					require.Len(t, diagnostics.diagnostics[uri], 1)
					assert.Equal(t, "resource not found", diagnostics.diagnostics[uri][0].Message)
					assert.Len(t, result.resourceDocumentLinks(proj, filename), 1)
				})
			}
		})
	}
}

func TestServerInlayHintAutoPropertyArguments(t *testing.T) {
	for _, tt := range []struct{ name, declarations, value, last string }{
		{"Complete", `func Label() string { return "" }`, "label", "1"},
		{"Incomplete", `func Label() string { return "" }`, "label", "missing"},
		{"Parenthesized", `func Label() string { return "" }`, "(label)", "missing"},
		{"FunctionVariable", `var Label = func() string { return "" }`, "label", "missing"},
		{"Member", "type Record struct{}\nfunc (Record) Label() string { return \"\" }\nvar record Record", "record.label", "missing"},
		{"ExplicitCall", `func Label() string { return "" }`, "Label()", "missing"},
		{"Literal", "", `"value"`, "missing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newImportTestServer(t, map[string][]byte{
				"types.xgo": []byte(tt.declarations + `
func text(text string, count int) {}
func number(number int, count int) {}
func Choose = (text; number)
`),
				"main.xgo": []byte("choose " + tt.value + ", " + tt.last + "\n"),
			})
			_, err := s.requestProject().TypeInfo()
			if tt.last == "1" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "undefined: missing")
			}
			hints, err := s.textDocumentInlayHint(&InlayHintParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Range: Range{End: Position{Line: 1}}})
			require.NoError(t, err)
			var labels []string
			for _, hint := range hints {
				labels = append(labels, hint.Label)
			}
			if tt.last == "1" {
				assert.Equal(t, []string{"text", "count"}, labels)
			} else {
				assert.Equal(t, []string{"text"}, labels)
			}
		})
	}
}

func TestServerKwargAutoPropertyArguments(t *testing.T) {
	for _, tt := range []struct{ name, declarations, value string }{
		{"Property", `func Label() string { return "" }`, "label"},
		{"Parenthesized", `func Label() string { return "" }`, "(label)"},
		{"FunctionVariable", `var Label = func() string { return "" }`, "label"},
		{"Member", "type Record struct{}\nfunc (Record) Label() string { return \"\" }\nvar record Record", "record.label"},
		{"Call", `func Label() string { return "" }`, "Label()"},
		{"Literal", "", `"value"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "choose "+tt.value+", missing, co|unt = 1\n")
			declarations := tt.declarations + `
type TextOptions struct { Count int }
type NumberOptions struct { Size int }
func text(text string, value int, opts TextOptions?) {}
func number(number int, value int, opts NumberOptions?) {}
func Choose = (text; number)
`
			s := newImportTestServer(t, map[string][]byte{"types.xgo": []byte(declarations), "main.xgo": []byte(source)})
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.ErrorContains(t, err, "undefined: missing")
			options, ok := info.Pkg.Scope().Lookup("TextOptions").Type().(*gotypes.Named)
			require.True(t, ok)
			fields, ok := options.Underlying().(*gotypes.Struct)
			require.True(t, ok)
			field := fields.Field(0)
			location := s.objectDefinitionLocation(proj, info, field)
			require.NotNil(t, location)
			position := TextDocumentPositionParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: pos}
			kwargRange := Range{Start: Position{Character: pos.Character - 2}, End: Position{Character: pos.Character + 3}}
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: position})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, "Count int")
			assert.Equal(t, kwargRange, hover.Range)
			definition, err := s.textDocumentDefinition(&DefinitionParams{TextDocumentPositionParams: position})
			require.NoError(t, err)
			assert.Equal(t, *location, definition)
			refs, err := s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: position})
			require.NoError(t, err)
			assert.Equal(t, []Location{{URI: position.TextDocument.URI, Range: kwargRange}}, refs)
			highlights, err := s.textDocumentDocumentHighlight(&DocumentHighlightParams{TextDocumentPositionParams: position})
			require.NoError(t, err)
			require.NotNil(t, highlights)
			assert.Equal(t, []DocumentHighlight{{Range: kwargRange, Kind: Read}}, *highlights)
			prepare, err := s.textDocumentPrepareRename(&PrepareRenameParams{TextDocumentPositionParams: position})
			require.NoError(t, err)
			assert.Equal(t, &kwargRange, prepare)
			links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: position.TextDocument})
			require.NoError(t, err)
			assert.True(t, slices.ContainsFunc(links, func(link DocumentLink) bool {
				return link.Range == kwargRange && link.Target != nil && strings.Contains(string(*link.Target), "TextOptions.Count")
			}))
			tokens, err := s.textDocumentSemanticTokensFull(&SemanticTokensParams{TextDocument: position.TextDocument})
			require.NoError(t, err)
			require.NotNil(t, tokens)
			assert.Contains(t, decodeSemanticTokens(tokens.Data), decodedSemanticToken{
				character: kwargRange.Start.Character,
				length:    5,
				tokenType: PropertyType,
			})
			edits, err := s.textDocumentRename(&RenameParams{TextDocument: TextDocumentIdentifier{URI: location.URI}, Position: location.Range.Start, NewName: "Total"})
			require.NoError(t, err)
			require.NotNil(t, edits)
			assert.Equal(t, []TextEdit{{Range: kwargRange, NewText: "total"}}, edits.Changes["file:///main.xgo"])
			updated := applyResourceRenameTestEdits(t, source, edits.Changes["file:///main.xgo"])
			declarations = applyResourceRenameTestEdits(t, declarations, edits.Changes["file:///types.xgo"])
			s.ModifyFiles([]FileChange{
				{Path: "main.xgo", Content: []byte(updated), Version: 1},
				{Path: "types.xgo", Content: []byte(declarations), Version: 1},
			})
			_, err = s.requestProject().TypeInfo()
			require.ErrorContains(t, err, "undefined: missing")
			hover, err = s.textDocumentHover(&HoverParams{TextDocumentPositionParams: position})
			require.NoError(t, err)
			require.NotNil(t, hover)
			assert.Contains(t, hover.Contents.Value, "Total int")
			refs, err = s.textDocumentReferences(&ReferenceParams{TextDocumentPositionParams: position})
			require.NoError(t, err)
			assert.Equal(t, []Location{{URI: position.TextDocument.URI, Range: kwargRange}}, refs)
		})
	}
}

func TestServerInspectDiagnosticsAnalyzersAutoPropertyArguments(t *testing.T) {
	for _, tt := range []struct{ name, value, last string }{
		{"Complete", "label", "1"},
		{"Incomplete", "label", "missing"},
		{"Call", "Label()", "missing"},
		{"Literal", `"value"`, "missing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newImportTestServer(t, map[string][]byte{
				"types.xgo": []byte(`func Label() string { return "" }
type PropertyName string
func text(text string, property PropertyName, count int) {}
func number(number int, property PropertyName, count int) {}
func Choose = (text; number)
`),
				"main.xgo": []byte("choose " + tt.value + ", \"Missing\", " + tt.last + "\n"),
			})
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			if tt.last == "1" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "undefined: missing")
			}
			propertyType := info.Pkg.Scope().Lookup("PropertyName").Type()
			result := newDiagnosticResult()
			s.inspectDiagnosticsAnalyzers(proj, &result, func(_ string, pass *protocol.Pass) {
				pass.IsPropertyNameType = func(typ gotypes.Type) bool { return typ == propertyType }
				pass.GetPropertyNamesForCall = func(*ast.CallExpr) map[string]struct{} {
					return map[string]struct{}{"Known": {}}
				}
			})
			require.Len(t, result.diagnostics["file:///main.xgo"], 1)
			diagnostic := result.diagnostics["file:///main.xgo"][0]
			assert.Equal(t, `unknown property "Missing"`, diagnostic.Message)
			assert.Equal(t, SeverityError, diagnostic.Severity)
		})
	}
}

func TestServerHoverUnitsWithAutoPropertyArguments(t *testing.T) {
	for _, tt := range []struct{ name, declarations, value string }{
		{"Property", `func Label() string { return "" }`, "label"},
		{"Parenthesized", `func Label() string { return "" }`, "(label)"},
		{"FunctionVariable", `var Label = func() string { return "" }`, "label"},
		{"Member", "type Record struct{}\nfunc (Record) Label() string { return \"\" }\nvar record Record", "record.label"},
		{"ExplicitCall", `func Label() string { return "" }`, "Label()"},
		{"Literal", "", `"text"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "choose "+tt.value+", 1|m, missing\n")
			s := newImportTestServer(t, map[string][]byte{
				"main.xgo": []byte(source),
				"types.xgo": []byte("import f \"example.com/framework\"\n" + tt.declarations + `
func text(value string, distance f.Distance, last int) {}
func number(value int, distance f.Distance, last int) {}
func Choose = (text; number)
`),
			})
			setImportTestFrameworkMethods(t, s, "type Distance int\nconst XGou_Distance = \"m=1,km=1000\"\n")
			_, err := s.requestProject().TypeInfo()
			require.ErrorContains(t, err, "undefined: missing")
			hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main.xgo"}, Position: pos,
			}})
			require.NoError(t, err)
			require.NotNil(t, hover)
			end := Position{Line: pos.Line, Character: pos.Character + 1}
			assert.Equal(t, Range{Start: pos, End: end}, hover.Range)
			assert.Contains(t, hover.Contents.Value, "unit `m`")
			assert.Contains(t, hover.Contents.Value, "framework.Distance")
			assert.Contains(t, hover.Contents.Value, "Multiplier: `1`")
			assert.Contains(t, completionItemLabels(completionListAt(t, s, "main.xgo", end).Items), "km")
		})
	}
}
