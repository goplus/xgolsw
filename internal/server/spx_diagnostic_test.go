//go:build !test_no_pkgdata

package server

import (
	"strings"
	"testing"

	"github.com/goplus/xgo/x/typesutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerDiagnosticsForSpx(t *testing.T) {
	for _, tt := range []struct {
		name               string
		files              map[string][]byte
		unavailablePackage string
		want               map[DocumentURI][]Diagnostic
	}{
		{name: "EmptyWorkspace", want: map[DocumentURI][]Diagnostic{}},
		{
			name:  "PlainXGoWithoutClassfiles",
			files: map[string][]byte{"main.xgo": []byte("println missing\n")},
			want: map[DocumentURI][]Diagnostic{"file:///main.xgo": {{
				Severity: SeverityError, Message: "undefined: missing",
				Range: Range{Start: Position{Character: 8}, End: Position{Character: 15}},
			}}},
		},
		{
			name:  "WorkClassWithoutMain",
			files: map[string][]byte{"Worker.spx": []byte("println 1\n")},
			want:  map[DocumentURI][]Diagnostic{"file:///Worker.spx": {}},
		},
		{
			name:  "SyntaxErrorWithoutMain",
			files: map[string][]byte{"Worker.spx": []byte("var (\n    x int\n")},
			want: map[DocumentURI][]Diagnostic{"file:///Worker.spx": {
				{Severity: SeverityError, Message: "expected ')', found 'EOF'", Range: Range{Start: Position{Line: 1, Character: 9}, End: Position{Line: 1, Character: 9}}},
				{Severity: SeverityError, Message: "expected ';', found 'EOF'", Range: Range{Start: Position{Line: 1, Character: 9}, End: Position{Line: 1, Character: 9}}},
			}},
		},
		{
			name: "UnavailableFramework",
			files: map[string][]byte{
				"main.spx":          []byte("println 1\n"),
				"assets/index.json": []byte(`{}`),
			},
			unavailablePackage: SpxPkgPath,
			want:               map[DocumentURI][]Diagnostic{"file:///main.spx": {}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newSpxTestServer(t, tt.files)
			proj := s.getProj()
			class, ok := proj.Module().LookupClass(".spx")
			require.True(t, ok)
			require.Contains(t, class.PkgPaths, SpxPkgPath)
			if tt.unavailablePackage != "" {
				proj.Importer = completionTestImporter{Importer: proj.Importer, unavailablePath: tt.unavailablePackage}
				_, err := proj.TypeInfo()
				var typeErr typesutil.Error
				require.ErrorAs(t, err, &typeErr)
				require.False(t, typeErr.Pos.IsValid())
			}
			for uri, diagnostics := range tt.want {
				report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: uri}})
				require.NoError(t, err)
				full := requireRelatedFullDocumentDiagnosticReport(t, report)
				assert.Equal(t, string(DiagnosticFull), full.Kind)
				assert.Equal(t, diagnostics, full.Items)
			}
			report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
			require.NoError(t, err)
			require.Len(t, report.Items, len(tt.want))
			got := make(map[DocumentURI][]Diagnostic)
			for _, item := range report.Items {
				full := requireWorkspaceFullDocumentDiagnosticReport(t, item)
				assert.Equal(t, string(DiagnosticFull), full.Kind)
				got[full.URI] = full.Items
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestServerTextDocumentDiagnosticSpx(t *testing.T) {
	t.Run("FuncDecoratorResourceArgument", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`func withSound(sound SoundName, fn func()) {
	fn()
}

@withSound("Missing")
func run() {
}
`),
			"assets/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		})
		require.NoError(t, err)
		require.NotNil(t, report)

		fullReport := requireRelatedFullDocumentDiagnosticReport(t, report)
		assert.Contains(t, fullReport.Items, Diagnostic{
			Severity: SeverityError,
			Message:  `sound resource "Missing" not found`,
			Range: Range{
				Start: Position{Line: 4, Character: 11},
				End:   Position{Line: 4, Character: 20},
			},
		})
	})

	t.Run("NonMainPackageDecl", func(t *testing.T) {
		fileMap := map[string][]byte{}
		fileMap["main.spx"] = []byte("package nonmain")
		s := newSpxTestServer(t, fileMap)
		params := &DocumentDiagnosticParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
		}

		report, err := s.textDocumentDiagnostic(params)
		require.NoError(t, err)
		require.NotNil(t, report)

		fullReport := requireRelatedFullDocumentDiagnosticReport(t, report)
		assert.Equal(t, string(DiagnosticFull), fullReport.Kind)
		require.Len(t, fullReport.Items, 1)
		assert.Contains(t, fullReport.Items, Diagnostic{
			Severity: SeverityError,
			Message:  "package name must be main",
			Range: Range{
				Start: Position{Line: 0, Character: 8},
				End:   Position{Line: 0, Character: 15},
			},
		})
	})
}

func TestServerWorkspaceDiagnosticSpx(t *testing.T) {
	t.Run("MixedSourceFiles", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx":          []byte(`play "Missing"`),
			"values.xgo":        []byte("var (\n    x int\n"),
			"assets/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, files)
		report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
		require.NoError(t, err)
		require.Len(t, report.Items, 2)
		got := make(map[DocumentURI][]Diagnostic)
		for _, item := range report.Items {
			full := requireWorkspaceFullDocumentDiagnosticReport(t, item)
			assert.Equal(t, string(DiagnosticFull), full.Kind)
			got[full.URI] = full.Items
		}
		assert.Equal(t, map[DocumentURI][]Diagnostic{
			"file:///main.spx": {{
				Severity: SeverityError, Message: `sound resource "Missing" not found`,
				Range: Range{Start: Position{Character: 5}, End: Position{Character: 14}},
			}},
			"file:///values.xgo": {
				{Severity: SeverityError, Message: "expected ')', found 'EOF'", Range: Range{Start: Position{Line: 1, Character: 9}, End: Position{Line: 1, Character: 9}}},
				{Severity: SeverityError, Message: "expected ';', found 'EOF'", Range: Range{Start: Position{Line: 1, Character: 9}, End: Position{Line: 1, Character: 9}}},
			},
		}, got)
	})

	// Keep SDK call binding here. Resource existence and diagnostic formatting
	// are covered with isolated resource sets in TestServerInspectSpxResourceRef.
	for _, resource := range []struct {
		name           string
		call           string
		emptyMessage   string
		missingMessage string
	}{
		{"Sound", "play VALUE", "sound resource name cannot be empty", `sound resource "Ghost" not found`},
		{"Backdrop", "onBackdrop VALUE, func() {}", "backdrop resource name cannot be empty", `backdrop resource "Ghost" not found`},
		{"Sprite", "touching VALUE", "sprite resource name cannot be empty", `sprite resource "Ghost" not found`},
		{"Costume", "setCostume VALUE", "sprite costume resource name cannot be empty", `costume resource "Ghost" not found in sprite "Runner"`},
		{"Animation", "animate VALUE", "sprite animation resource name cannot be empty", `animation resource "Ghost" not found in sprite "Runner"`},
		{"Widget", "getWidget Monitor, VALUE", "widget resource name cannot be empty", `widget resource "Ghost" not found`},
	} {
		t.Run(resource.name, func(t *testing.T) {
			source := "const Missing = \"Ghost\"\nvar dynamic string = \"dynamic\"\n"
			var want []Diagnostic
			for i, arg := range []string{`""`, "Missing", `"Ghost"`, "dynamic"} {
				source += strings.Replace(resource.call, "VALUE", arg, 1) + "\n"
				if arg == "dynamic" {
					continue
				}
				column := uint32(strings.Index(resource.call, "VALUE"))
				message := resource.missingMessage
				if arg == `""` {
					message = resource.emptyMessage
				}
				want = append(want, Diagnostic{
					Range:    Range{Start: Position{Line: uint32(i + 2), Character: column}, End: Position{Line: uint32(i + 2), Character: column + uint32(len(arg))}},
					Severity: SeverityError, Message: message,
				})
			}
			s := newSpxTestServer(t, map[string][]byte{
				"main.spx":                         nil,
				"Runner.spx":                       []byte(source),
				"assets/index.json":                []byte(`{}`),
				"assets/sprites/Runner/index.json": []byte(`{}`),
			})
			_, err := s.getProj().TypeInfo()
			require.NoError(t, err)
			report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
			require.NoError(t, err)
			require.Len(t, report.Items, 2)
			got := make(map[DocumentURI][]Diagnostic)
			for _, item := range report.Items {
				full := requireWorkspaceFullDocumentDiagnosticReport(t, item)
				assert.Equal(t, string(DiagnosticFull), full.Kind)
				got[full.URI] = full.Items
			}
			assert.Empty(t, got["file:///main.spx"])
			assert.ElementsMatch(t, want, got["file:///Runner.spx"])
		})
	}

	t.Run("PropertyNameNotFoundInOverloadKwargs", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Worker struct{}

type Options struct {
	Name PropertyName
}

var worker Worker

func (w *Worker) configureOptions(opts Options?) {}

func (Worker).configure = (
	(Worker).configureOptions
)

onStart => {
	worker.configure name = "unknownProperty"
}
`),
			"assets/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
		require.NoError(t, err)
		require.NotNil(t, report)
		require.Len(t, report.Items, 1)
		fullReport := requireWorkspaceFullDocumentDiagnosticReport(t, report.Items[0])
		require.Len(t, fullReport.Items, 1)
		assert.Contains(t, fullReport.Items, Diagnostic{
			Severity: SeverityError,
			Message:  `unknown property "unknownProperty"`,
			Range: Range{
				Start: Position{Line: 16, Character: 25},
				End:   Position{Line: 16, Character: 42},
			},
		})
	})

	t.Run("WithNonBasicTypeAliases", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
`),
			"MySprite.spx": []byte(`
import "image/color"

onStart => {
	touchingColor HSBA(0, 0, 0, 0)
}
`),
			"assets/index.json":                  []byte(`{}`),
			"assets/sprites/MySprite/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
		require.NoError(t, err)
		require.NotNil(t, report)
		assert.Len(t, report.Items, 2)
		for _, item := range report.Items {
			fullReport := requireWorkspaceFullDocumentDiagnosticReport(t, item)
			assert.Equal(t, string(DiagnosticFull), fullReport.Kind)
			assert.Empty(t, fullReport.Items)
		}
	})

	t.Run("OnKey", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onKey KeyLeft, => {}

onKey [KeyRight, KeyUp, KeyDown], => {}

`),
			"assets/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
		require.NoError(t, err)
		require.NotNil(t, report)
		assert.Len(t, report.Items, 1)
		for _, item := range report.Items {
			fullReport := requireWorkspaceFullDocumentDiagnosticReport(t, item)
			assert.Equal(t, string(DiagnosticFull), fullReport.Kind)
			assert.Empty(t, fullReport.Items)
		}
	})
}
