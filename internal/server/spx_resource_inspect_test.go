package server

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerInspectSpxResourceRef(t *testing.T) {
	for _, resource := range []struct {
		name      string
		newID     func(string) SpxResourceID
		emptyType string
		missing   string
	}{
		{"Backdrop", func(name string) SpxResourceID { return SpxBackdropResourceID{name} }, "backdrop", `backdrop resource %q not found`},
		{"Sound", func(name string) SpxResourceID { return SpxSoundResourceID{name} }, "sound", `sound resource %q not found`},
		{"Sprite", func(name string) SpxResourceID { return SpxSpriteResourceID{name} }, "sprite", `sprite resource %q not found`},
		{"Costume", func(name string) SpxResourceID { return SpxSpriteCostumeResourceID{"Known", name} }, "sprite costume", `costume resource %q not found in sprite "Known"`},
		{"Animation", func(name string) SpxResourceID { return SpxSpriteAnimationResourceID{"Known", name} }, "sprite animation", `animation resource %q not found in sprite "Known"`},
		{"Widget", func(name string) SpxResourceID { return SpxWidgetResourceID{name} }, "widget", `widget resource %q not found`},
	} {
		t.Run(resource.name, func(t *testing.T) {
			for _, value := range []struct {
				name  string
				value string
			}{
				{"Present", "Known"}, {"Missing", "Ghost"}, {"Empty", ""}, {"Escaped", "A\"B"},
			} {
				t.Run(value.name, func(t *testing.T) {
					literal := strconv.Quote(value.value)
					s := newTestServer(t, map[string][]byte{
						"main.xgo":                        []byte("const Value = " + literal + "\necho Value, " + literal + "\n"),
						"assets/index.json":               []byte(`{"backdrops":[{"name":"Known"}],"zorder":[{"name":"Known"}]}`),
						"assets/sounds/Known/index.json":  []byte(`{}`),
						"assets/sprites/Known/index.json": []byte(`{"costumes":[{"name":"Known"}],"fAnimations":{"Known":{}}}`),
					})
					proj := s.getProj()
					call := spxResourceTestCall(t, proj, "main.xgo")
					require.Len(t, call.Args, 2)
					set, err := NewSpxResourceSet(proj)
					require.NoError(t, err)
					result := newCompileResult(proj, s.lookupPkgDoc)
					result.spxResourceSet = *set
					var wantRefs []SpxResourceRef
					var wantDiagnostics []Diagnostic
					var wantLinks []DocumentLink
					for i, kind := range []SpxResourceRefKind{SpxResourceRefKindConstantReference, SpxResourceRefKindStringLiteral} {
						ref := SpxResourceRef{ID: resource.newID(value.value), Kind: kind, Node: call.Args[i]}
						s.inspectSpxResourceRef(result, ref)
						s.inspectSpxResourceRef(result, ref)
						span := Range{Start: Position{Line: 1, Character: 5}, End: Position{Line: 1, Character: 10}}
						if i == 1 {
							span = Range{Start: Position{Line: 1, Character: 12}, End: Position{Line: 1, Character: 12 + uint32(len(literal))}}
						}
						if value.value != "" {
							wantRefs = append(wantRefs, ref)
						}
						switch value.name {
						case "Present":
							wantLinks = append(wantLinks, DocumentLink{Range: span, Target: toURI(string(ref.ID.URI())), Data: SpxResourceRefDocumentLinkData{Kind: kind}})
						case "Empty":
							wantDiagnostics = append(wantDiagnostics, Diagnostic{Range: span, Severity: SeverityError, Message: resource.emptyType + " resource name cannot be empty"})
						default:
							wantDiagnostics = append(wantDiagnostics, Diagnostic{Range: span, Severity: SeverityError, Message: fmt.Sprintf(resource.missing, value.value)})
						}
					}
					assert.Equal(t, wantRefs, result.spxResourceRefs)
					assert.Equal(t, wantDiagnostics, result.diagnostics["file:///main.xgo"])
					assert.Equal(t, len(wantDiagnostics) > 0, result.hasErrorSeverityDiagnostic)
					assert.ElementsMatch(t, wantLinks, result.spxResourceDocumentLinks("main.xgo"))
				})
			}
		})
	}
}
