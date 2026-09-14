package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerAddEmptySpxResourceNameDiagnostic(t *testing.T) {
	for _, resource := range []struct {
		name string
		kind string
	}{
		{"Backdrop", "backdrop"},
		{"Sound", "sound"},
		{"Sprite", "sprite"},
		{"Costume", "sprite costume"},
		{"Animation", "sprite animation"},
		{"Widget", "widget"},
	} {
		t.Run(resource.name, func(t *testing.T) {
			for _, form := range []struct {
				name      string
				source    string
				wantRange Range
			}{
				{"Literal", "echo \"\"\n", Range{Start: Position{Character: 5}, End: Position{Character: 7}}},
				{"RawLiteral", "echo `\r`\n", Range{Start: Position{Character: 5}, End: Position{Character: 8}}},
				{"Constant", "const Empty = \"\"\necho Empty\n", Range{Start: Position{Line: 1, Character: 5}, End: Position{Line: 1, Character: 10}}},
			} {
				t.Run(form.name, func(t *testing.T) {
					s := newTestServer(t, map[string][]byte{"main.xgo": []byte(form.source)})
					proj := s.getProj()
					call := spxResourceTestCall(t, proj, "main.xgo")
					require.Len(t, call.Args, 1)
					result := newCompileResult(proj, s.lookupPkgDoc)
					s.addEmptySpxResourceNameDiagnostic(result, call.Args[0], resource.kind)
					s.addEmptySpxResourceNameDiagnostic(result, call.Args[0], resource.kind)
					assert.Equal(t, map[DocumentURI][]Diagnostic{"file:///main.xgo": {{
						Severity: SeverityError, Range: form.wantRange,
						Message: resource.kind + " resource name cannot be empty",
					}}}, result.diagnostics)
					assert.True(t, result.hasErrorSeverityDiagnostic)
				})
			}
		})
	}
}

func TestServerAddSpxResourceNotFoundDiagnostic(t *testing.T) {
	for _, tt := range []struct {
		name         string
		resourceType string
		spriteName   string
		want         string
	}{
		{"Backdrop", "backdrop", "", `backdrop resource "Ghost" not found`},
		{"Sound", "sound", "", `sound resource "Ghost" not found`},
		{"Sprite", "sprite", "", `sprite resource "Ghost" not found`},
		{"Costume", "costume", "Runner", `costume resource "Ghost" not found in sprite "Runner"`},
		{"Animation", "animation", "Runner", `animation resource "Ghost" not found in sprite "Runner"`},
		{"Widget", "widget", "", `widget resource "Ghost" not found`},
		{"EscapedContext", "costume", "A\"B", `costume resource "Ghost" not found in sprite "A\"B"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte("const Missing = \"Ghost\"\necho \"Ghost\", Missing, \"Ghost\"\n")})
			proj := s.getProj()
			call := spxResourceTestCall(t, proj, "main.xgo")
			require.Len(t, call.Args, 3)
			result := newCompileResult(proj, s.lookupPkgDoc)
			var want []Diagnostic
			for i, span := range [][2]uint32{{5, 12}, {14, 21}, {23, 30}} {
				s.addSpxResourceNotFoundDiagnostic(result, call.Args[i], tt.resourceType, "Ghost", tt.spriteName)
				s.addSpxResourceNotFoundDiagnostic(result, call.Args[i], tt.resourceType, "Ghost", tt.spriteName)
				want = append(want, Diagnostic{
					Severity: SeverityError, Message: tt.want,
					Range: Range{Start: Position{Line: 1, Character: span[0]}, End: Position{Line: 1, Character: span[1]}},
				})
			}
			assert.Equal(t, map[DocumentURI][]Diagnostic{"file:///main.xgo": want}, result.diagnostics)
			assert.True(t, result.hasErrorSeverityDiagnostic)
			assert.Empty(t, newCompileResult(proj, s.lookupPkgDoc).diagnostics)
		})
	}

	t.Run("EscapedName", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.xgo": []byte("echo `A\"B\nC`\n")})
		proj := s.getProj()
		call := spxResourceTestCall(t, proj, "main.xgo")
		require.Len(t, call.Args, 1)
		result := newCompileResult(proj, s.lookupPkgDoc)
		s.addSpxResourceNotFoundDiagnostic(result, call.Args[0], "sound", "A\"B\nC", "")
		assert.Equal(t, map[DocumentURI][]Diagnostic{"file:///main.xgo": {{
			Severity: SeverityError, Message: `sound resource "A\"B\nC" not found`,
			Range: Range{Start: Position{Character: 5}, End: Position{Line: 1, Character: 2}},
		}}}, result.diagnostics)
	})
}

func TestServerInspectForSpxResourceSet(t *testing.T) {
	t.Run("Resources", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"assets/index.json":             []byte(`{"backdrops":[{"name":"Studio"}]}`),
			"assets/sounds/Beep/index.json": []byte(`{}`),
		})
		proj := s.getProj()
		result := newCompileResult(proj, s.lookupPkgDoc)
		s.inspectForSpxResourceSet(proj, result)
		assert.NotNil(t, result.spxResourceSet.Backdrop("Studio"))
		assert.NotNil(t, result.spxResourceSet.Sound("Beep"))
		assert.Empty(t, result.diagnostics)
		assert.False(t, result.hasErrorSeverityDiagnostic)
	})

	t.Run("MetadataErrors", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			files   map[string][]byte
			message string
		}{
			{"MissingRoot", nil, "failed to read metadata: file does not exist"},
			{"InvalidRoot", map[string][]byte{"assets/index.json": []byte(`{`)}, "failed to parse metadata: unexpected end of JSON input"},
			{"MissingSound", map[string][]byte{"assets/index.json": []byte(`{}`), "assets/sounds/Beep/beep.wav": nil}, "failed to read sound metadata: file does not exist"},
			{"InvalidSound", map[string][]byte{"assets/index.json": []byte(`{}`), "assets/sounds/Beep/index.json": []byte(`{`)}, "failed to parse sound metadata: unexpected end of JSON input"},
			{"MissingSprite", map[string][]byte{"assets/index.json": []byte(`{}`), "assets/sprites/Runner/image.png": nil}, "failed to read sprite metadata: file does not exist"},
			{"InvalidSprite", map[string][]byte{"assets/index.json": []byte(`{}`), "assets/sprites/Runner/index.json": []byte(`{`)}, "failed to parse sprite metadata: unexpected end of JSON input"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, tt.files)
				proj := s.getProj()
				result := newCompileResult(proj, s.lookupPkgDoc)
				result.mainSpxFile = "project/Stage.spx"
				prior := Diagnostic{Severity: SeverityWarning, Message: "Existing warning"}
				result.addDiagnostics("file:///other.xgo", prior)
				s.inspectForSpxResourceSet(proj, result)
				s.inspectForSpxResourceSet(proj, result)
				assert.Equal(t, map[DocumentURI][]Diagnostic{
					"file:///other.xgo":         {prior},
					"file:///project/Stage.spx": {{Severity: SeverityError, Message: "failed to create spx resource set: " + tt.message}},
				}, result.diagnostics)
				assert.True(t, result.hasErrorSeverityDiagnostic)
				assert.Equal(t, SpxResourceSet{}, result.spxResourceSet)
			})
		}
	})
}
