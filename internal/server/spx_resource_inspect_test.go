package server

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectSpxResourceRef(t *testing.T) {
	for _, resource := range []struct {
		name      string
		newID     func(string) resourceID
		emptyType string
		missing   string
	}{
		{"Backdrop", func(name string) resourceID { return SpxBackdropResourceID{name} }, "backdrop", `backdrop resource %q not found`},
		{"Sound", func(name string) resourceID { return SpxSoundResourceID{name} }, "sound", `sound resource %q not found`},
		{"Sprite", func(name string) resourceID { return SpxSpriteResourceID{name} }, "sprite", `sprite resource %q not found`},
		{"Costume", func(name string) resourceID { return SpxSpriteCostumeResourceID{"Known", name} }, "sprite costume", `costume resource %q not found in sprite "Known"`},
		{"Animation", func(name string) resourceID { return SpxSpriteAnimationResourceID{"Known", name} }, "sprite animation", `animation resource %q not found in sprite "Known"`},
		{"Widget", func(name string) resourceID { return SpxWidgetResourceID{name} }, "widget", `widget resource %q not found`},
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
					call := resourceTestCall(t, proj, "main.xgo")
					require.Len(t, call.Args, 2)
					set, err := NewSpxResourceSet(proj)
					require.NoError(t, err)
					result := newSpxAnalysis(proj)
					result.spxResourceSet = *set
					result.contains = set.Contains
					var wantRefs []resourceRef
					var wantDiagnostics []Diagnostic
					var wantLinks []DocumentLink
					for i, kind := range []XGoResourceRefKind{XGoResourceRefKindConstantReference, XGoResourceRefKindStringLiteral} {
						ref := resourceRef{ID: resource.newID(value.value), Kind: kind, Node: call.Args[i]}
						inspectSpxResourceRef(s.getProj(), result, ref)
						inspectSpxResourceRef(s.getProj(), result, ref)
						span := Range{Start: Position{Line: 1, Character: 5}, End: Position{Line: 1, Character: 10}}
						if i == 1 {
							span = Range{Start: Position{Line: 1, Character: 12}, End: Position{Line: 1, Character: 12 + uint32(len(literal))}}
						}
						if value.value != "" {
							wantRefs = append(wantRefs, ref)
						}
						switch value.name {
						case "Present":
							wantLinks = append(wantLinks, DocumentLink{Range: span, Target: toURI(string(ref.ID.URI())), Data: XGoResourceRefDocumentLinkData{Kind: kind}})
						case "Empty":
							wantDiagnostics = append(wantDiagnostics, Diagnostic{Range: span, Severity: SeverityError, Message: resource.emptyType + " resource name cannot be empty"})
						default:
							wantDiagnostics = append(wantDiagnostics, Diagnostic{Range: span, Severity: SeverityError, Message: fmt.Sprintf(resource.missing, value.value)})
						}
					}
					assert.Equal(t, wantRefs, result.resourceRefs)
					assert.Equal(t, wantDiagnostics, resourceDiagnostics(s, result.resourceAnalysis).diagnostics["file:///main.xgo"])
					assert.Equal(t, len(wantDiagnostics) > 0, resourceDiagnostics(s, result.resourceAnalysis).hasErrorSeverityDiagnostic)
					assert.ElementsMatch(t, wantLinks, result.resourceAnalysis.resourceDocumentLinks(s.getProj(), "main.xgo"))
				})
			}
		})
	}
}
