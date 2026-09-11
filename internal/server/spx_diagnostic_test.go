//go:build !test_no_pkgdata

package server

import (
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
			class, ok := proj.Mod.LookupClass(".spx")
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

	t.Run("SoundResourceNotFound", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
play "Sound1"
`),
			"MySprite.spx": []byte(`
const ConstSoundName = "ConstSoundName"
var (
	VarSoundName string
)
VarSoundName = "VarSoundName"
onStart => {
	play ""
	play ConstSoundName
	play "LiteralSoundName"
	play VarSoundName
	play "Sound1"
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
			switch fullReport.URI {
			case "file:///main.spx":
				require.Len(t, fullReport.Items, 1)
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  `sound resource "Sound1" not found`,
					Range: Range{
						Start: Position{Line: 1, Character: 5},
						End:   Position{Line: 1, Character: 13},
					},
				})
			case "file:///MySprite.spx":
				require.Len(t, fullReport.Items, 4)
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  "sound resource name cannot be empty",
					Range: Range{
						Start: Position{Line: 7, Character: 6},
						End:   Position{Line: 7, Character: 8},
					},
				})
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  `sound resource "ConstSoundName" not found`,
					Range: Range{
						Start: Position{Line: 8, Character: 6},
						End:   Position{Line: 8, Character: 20},
					},
				})
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  `sound resource "LiteralSoundName" not found`,
					Range: Range{
						Start: Position{Line: 9, Character: 6},
						End:   Position{Line: 9, Character: 24},
					},
				})
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  `sound resource "Sound1" not found`,
					Range: Range{
						Start: Position{Line: 11, Character: 6},
						End:   Position{Line: 11, Character: 14},
					},
				})
			default:
				assert.Empty(t, fullReport.Items)
			}
		}
	})

	t.Run("SoundResourceNotFoundInKwargs", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Options struct {
	Sound SoundName
}

var client Client

func configure(opts Options?) {}

func configureMap(opts map[string]SoundName?) {}

type Player interface {
	Sound(sound SoundName) Player
}

type Client struct{}

func (c Client) Player() Player { return nil }

func (c Client) play(params Player?) {}

onStart => {
	configure sound = "MissingStructSound"
	configureMap sound = "MissingMapSound"
	client.play sound = "MissingInterfaceSound"
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
		require.Len(t, fullReport.Items, 3)
		assert.Contains(t, fullReport.Items, Diagnostic{
			Severity: SeverityError,
			Message:  `sound resource "MissingStructSound" not found`,
			Range: Range{
				Start: Position{Line: 22, Character: 19},
				End:   Position{Line: 22, Character: 39},
			},
		})
		assert.Contains(t, fullReport.Items, Diagnostic{
			Severity: SeverityError,
			Message:  `sound resource "MissingMapSound" not found`,
			Range: Range{
				Start: Position{Line: 23, Character: 22},
				End:   Position{Line: 23, Character: 39},
			},
		})
		assert.Contains(t, fullReport.Items, Diagnostic{
			Severity: SeverityError,
			Message:  `sound resource "MissingInterfaceSound" not found`,
			Range: Range{
				Start: Position{Line: 24, Character: 21},
				End:   Position{Line: 24, Character: 44},
			},
		})
	})

	t.Run("SoundResourceNotFoundInOverloadKwargs", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
type Worker struct{}

type Options struct {
	Sound SoundName
}

var worker Worker

func (w *Worker) playSound(opts Options?) {}

func (Worker).play = (
	(Worker).playSound
)

onStart => {
	worker.play sound = "MissingOverloadSound"
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
			Message:  `sound resource "MissingOverloadSound" not found`,
			Range: Range{
				Start: Position{Line: 16, Character: 21},
				End:   Position{Line: 16, Character: 43},
			},
		})
	})

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

	t.Run("BackdropResourceNotFound", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
onBackdrop "", func() {}
onBackdrop "NonExistentBackdrop", func() {}
`),
			"MySprite.spx": []byte(`
const ConstBackdropName = "ConstBackdropName"
var VarBackdropName string
VarBackdropName = "VarBackdropName"
onStart => {
	onBackdrop ConstBackdropName, func() {}
	onBackdrop "LiteralBackdropName", func() {}
	onBackdrop VarBackdropName, func() {}
}
`),
			"assets/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
		require.NoError(t, err)
		require.NotNil(t, report)
		assert.Len(t, report.Items, 2)
		for _, item := range report.Items {
			fullReport := requireWorkspaceFullDocumentDiagnosticReport(t, item)
			assert.Equal(t, string(DiagnosticFull), fullReport.Kind)
			switch fullReport.URI {
			case "file:///main.spx":
				require.Len(t, fullReport.Items, 2)
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  "backdrop resource name cannot be empty",
					Range: Range{
						Start: Position{Line: 1, Character: 11},
						End:   Position{Line: 1, Character: 13},
					},
				})
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  `backdrop resource "NonExistentBackdrop" not found`,
					Range: Range{
						Start: Position{Line: 2, Character: 11},
						End:   Position{Line: 2, Character: 32},
					},
				})
			case "file:///MySprite.spx":
				require.Len(t, fullReport.Items, 2)
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  `backdrop resource "ConstBackdropName" not found`,
					Range: Range{
						Start: Position{Line: 5, Character: 12},
						End:   Position{Line: 5, Character: 29},
					},
				})
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  `backdrop resource "LiteralBackdropName" not found`,
					Range: Range{
						Start: Position{Line: 6, Character: 12},
						End:   Position{Line: 6, Character: 33},
					},
				})
			default:
				assert.Empty(t, fullReport.Items)
			}
		}
	})

	t.Run("SpriteResourceNotFound", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
MySprite.say "hi"
MySprite.touching "OtherSprite"
`),
			"MySprite.spx": []byte(`
onStart => {
	say "hi"
	touching "OtherSprite"
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
			switch fullReport.URI {
			case "file:///main.spx":
				require.Len(t, fullReport.Items, 1)
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  `sprite resource "OtherSprite" not found`,
					Range: Range{
						Start: Position{Line: 2, Character: 18},
						End:   Position{Line: 2, Character: 31},
					},
				})
			case "file:///MySprite.spx":
				require.Len(t, fullReport.Items, 1)
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  `sprite resource "OtherSprite" not found`,
					Range: Range{
						Start: Position{Line: 3, Character: 10},
						End:   Position{Line: 3, Character: 23},
					},
				})
			default:
				assert.Empty(t, fullReport.Items)
			}
		}
	})

	t.Run("SpriteCostumeResourceNotFound", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
`),
			"MySprite.spx": []byte(`
onStart => {
	setCostume ""
	setCostume "NonExistentCostume"
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
			switch fullReport.URI {
			case "file:///MySprite.spx":
				require.Len(t, fullReport.Items, 2)
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  "sprite costume resource name cannot be empty",
					Range: Range{
						Start: Position{Line: 2, Character: 12},
						End:   Position{Line: 2, Character: 14},
					},
				})
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  `costume resource "NonExistentCostume" not found in sprite "MySprite"`,
					Range: Range{
						Start: Position{Line: 3, Character: 12},
						End:   Position{Line: 3, Character: 32},
					},
				})
			default:
				assert.Empty(t, fullReport.Items)
			}
		}
	})

	t.Run("SpriteAnimationResourceNotFound", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
`),
			"MySprite.spx": []byte(`
onStart => {
	animate ""
	animate "roll-in"
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
			switch fullReport.URI {
			case "file:///MySprite.spx":
				require.Len(t, fullReport.Items, 2)
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  "sprite animation resource name cannot be empty",
					Range: Range{
						Start: Position{Line: 2, Character: 9},
						End:   Position{Line: 2, Character: 11},
					},
				})
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  `animation resource "roll-in" not found in sprite "MySprite"`,
					Range: Range{
						Start: Position{Line: 3, Character: 9},
						End:   Position{Line: 3, Character: 18},
					},
				})
			default:
				assert.Empty(t, fullReport.Items)
			}
		}
	})

	t.Run("WidgetResourceNotFound", func(t *testing.T) {
		m := map[string][]byte{
			"main.spx": []byte(`
`),
			"MySprite.spx": []byte(`
const ConstWidgetName = "ConstWidgetName"
var VarWidgetName string
VarWidgetName = "VarWidgetName"
onStart => {
	getWidget Monitor, ""
	getWidget Monitor, ConstWidgetName
	getWidget Monitor, "LiteralWidgetName"
	getWidget Monitor, VarWidgetName
}
`),
			"assets/index.json": []byte(`{}`),
		}
		s := newSpxTestServer(t, m)

		report, err := s.workspaceDiagnostic(&WorkspaceDiagnosticParams{})
		require.NoError(t, err)
		require.NotNil(t, report)
		assert.Len(t, report.Items, 2)
		for _, item := range report.Items {
			fullReport := requireWorkspaceFullDocumentDiagnosticReport(t, item)
			assert.Equal(t, string(DiagnosticFull), fullReport.Kind)
			switch fullReport.URI {
			case "file:///MySprite.spx":
				require.Len(t, fullReport.Items, 3)
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  "widget resource name cannot be empty",
					Range: Range{
						Start: Position{Line: 5, Character: 20},
						End:   Position{Line: 5, Character: 22},
					},
				})
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  `widget resource "ConstWidgetName" not found`,
					Range: Range{
						Start: Position{Line: 6, Character: 20},
						End:   Position{Line: 6, Character: 35},
					},
				})
				assert.Contains(t, fullReport.Items, Diagnostic{
					Severity: SeverityError,
					Message:  `widget resource "LiteralWidgetName" not found`,
					Range: Range{
						Start: Position{Line: 7, Character: 20},
						End:   Position{Line: 7, Character: 39},
					},
				})
			default:
				assert.Empty(t, fullReport.Items)
			}
		}
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
