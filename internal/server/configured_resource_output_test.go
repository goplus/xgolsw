package server

import (
	"encoding/json"
	"testing"

	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerConfiguredResourceSourceRanges(t *testing.T) {
	for _, tt := range []struct {
		name    string
		literal string
		value   string
		end     Position
	}{
		{"CarriageReturn", "`Stu\rdio`", "Studio", Position{Character: 14}},
		{"MultilineUnicode", "`Stu\r\n\U0001f600dio`", "Stu\n\U0001f600dio", Position{Line: 1, Character: 6}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, state := range []struct {
				name   string
				exists bool
			}{
				{"Existing", true},
				{"Missing", false},
			} {
				t.Run(state.name, func(t *testing.T) {
					files := map[string][]byte{
						"main_fixture.gox": []byte("play " + tt.literal + "\n"),
						"types.xgo":        []byte("import clips \"example.com/support/v2\"\nfunc play(value clips.Name) {}\n"),
						"resources.json":   []byte(`{}`),
					}
					if state.exists {
						manifest, err := json.Marshal(map[string][]string{"demo://resources/clips": {tt.value}})
						require.NoError(t, err)
						files["resources.json"] = manifest
					}
					s := newConfiguredResourceTestServer(t, files, resourceTestConfig)
					id := configuredResourceID{"demo://resources/clips", tt.value}
					wantRange := Range{Start: Position{Character: 5}, End: tt.end}
					position := tt.end
					position.Character--
					hover, err := s.textDocumentHover(&HoverParams{TextDocumentPositionParams: TextDocumentPositionParams{
						TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}, Position: position,
					}})
					require.NoError(t, err)
					require.NotNil(t, hover)
					assert.Equal(t, wantRange, hover.Range)
					assert.Equal(t, resourceMarkupContent(id.URI(), Markdown), hover.Contents)

					slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{
						TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
					}})
					require.NoError(t, err)
					slot := findInputSlot(slots, id.URI(), "", XGoInputTypeResourceName, XGoInputKindInPlace)
					require.NotNil(t, slot)
					assert.Equal(t, XGoInputTypeResourceName, slot.Accept.Type)
					assert.Equal(t, ToPtr(XGoResourceContextURI("demo://resources/clips")), slot.Accept.ResourceContext)
					assert.Equal(t, wantRange, slot.Range)

					links, err := s.textDocumentDocumentLink(&DocumentLinkParams{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}})
					require.NoError(t, err)
					if state.exists {
						assert.Contains(t, links, DocumentLink{
							Range: wantRange, Target: toURI(string(id.URI())),
							Data: XGoResourceRefDocumentLinkData{Kind: XGoResourceRefKindStringLiteral},
						})
					} else {
						assert.NotContains(t, documentLinkTargets(t, links), string(id.URI()))
					}
					report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}})
					require.NoError(t, err)
					require.NotNil(t, report)
					diagnostics := requireRelatedFullDocumentDiagnosticReport(t, report).Items
					if state.exists {
						assert.Empty(t, diagnostics)
					} else {
						require.Len(t, diagnostics, 1)
						assert.Equal(t, wantRange, diagnostics[0].Range)
						assert.Equal(t, SeverityError, diagnostics[0].Severity)
						assert.Contains(t, diagnostics[0].Message, "not found")
					}
				})
			}
		})
	}
}

func TestServerConfiguredResourceDocumentation(t *testing.T) {
	t.Run("PkgDocLookup", func(t *testing.T) {
		files := map[string][]byte{
			"main_fixture.gox": []byte("import \"fmt\"\nfmt.Println(1)\nplay \"Sound\"\n"),
			"types.xgo":        []byte("import clips \"example.com/support/v2\"\nfunc play(value clips.Name) {}\n"),
			"resources.json":   []byte(`{"demo://resources/clips":["Sound"]}`),
		}
		s := newConfiguredResourceTestServer(t, files, resourceTestConfig)
		const wantDoc = "Documentation supplied by the server."
		doc := &pkgdoc.PkgDoc{
			Path:  "fmt",
			Funcs: map[string]string{"Println": wantDoc},
		}
		var lookups []string
		s.lookupPkgDoc = func(pkgPath string) (*pkgdoc.PkgDoc, error) {
			assert.Equal(t, "fmt", pkgPath)
			lookups = append(lookups, pkgPath)
			return doc, nil
		}

		ctx := &definitionContext{proj: s.getProj(), lookupPkgDoc: s.lookupPkgDoc}
		pkg, err := ctx.proj.Importer.Import("fmt")
		require.NoError(t, err)
		defs := ctx.definitionsFor(pkg.Scope().Lookup("Println"), "")
		require.Len(t, defs, 1)
		assert.Equal(t, wantDoc, defs[0].Detail)
		assert.Equal(t, []string{"fmt"}, lookups)

		hover, err := s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 1, Character: 5},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Contains(t, hover.Contents.Value, wantDoc)
		assert.Equal(t, []string{"fmt", "fmt"}, lookups)

		hover, err = s.textDocumentHover(&HoverParams{
			TextDocumentPositionParams: TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"},
				Position:     Position{Line: 2, Character: 7},
			},
		})
		require.NoError(t, err)
		require.NotNil(t, hover)
		assert.Equal(t, resourceMarkupContent("demo://resources/clips/Sound", Markdown), hover.Contents)
		assert.Equal(t, []string{"fmt", "fmt"}, lookups)
	})

}
