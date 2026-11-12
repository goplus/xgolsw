//go:build !test_no_pkgdata

package server

import (
	_ "embed"
	"testing"

	"github.com/goplus/xgolsw/internal/config"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:generate sh -c "GOTOOLCHAIN=\"go$(go list -m -f '{{.GoVersion}}')\" go tool pkgdatagen -no-defaults -o testdata/spx-pkgdata.zip github.com/goplus/spx/v4 github.com/goplus/spx/v4/pkg/spx/pkg/engine"

//go:embed testdata/spx-pkgdata.zip
var spxIntegrationPkgDataZip []byte

func newSpxIntegrationTestServer(t testing.TB, files map[string][]byte) *Server {
	t.Helper()

	data, err := pkgdata.NewWithEmbedded(spxIntegrationPkgDataZip)
	require.NoError(t, err)
	proj, data, err := config.NewProject(newFileMap(files), config.Options{
		ClassFrameworkConfig: `project main.spx Game github.com/goplus/spx/v4 math
class -embed *.spx SpriteImpl
`,
		PkgData: data,
	})
	require.NoError(t, err)
	return New(proj, nil, fileMapGetter(files), &MockScheduler{}, data.ListPkgs, data.GetPkgDoc)
}

func TestSpxIntegrationSymbols(t *testing.T) {
	s := newSpxIntegrationTestServer(t, nil)
	ctx := newSpxSymbols(s.getProj())
	for _, name := range []string{
		"Sprite", "SpriteImpl", "BackdropName", "SpriteName", "SpriteCostumeName",
		"SpriteAnimationName", "SoundName", "WidgetName", "Direction", "layerAction",
		"dirAction", "EffectKind", "Key", "Edge", "RotationStyle", "PropertyName", "Value", "List",
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, name, ctx.spxTypeName(spxTestType(t, s, name)))
		})
	}
}

func TestSpxIntegrationDiagnostics(t *testing.T) {
	s := newSpxIntegrationTestServer(t, map[string][]byte{
		"main.spx": []byte("play \"Missing\"\n"), "assets/index.json": []byte(`{}`),
	})
	replier := newMockReplier()
	s.replier = replier
	require.NoError(t, s.didOpen(&DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{URI: "file:///main.spx", Version: 1, Text: "play \"Missing\"\n"},
	}))
	diagnostics := requirePublishedDiagnostics(t, replier, "file:///main.spx")
	require.Len(t, diagnostics, 1)
	assert.Equal(t, `sound resource "Missing" not found`, diagnostics[0].Message)
	report, err := s.textDocumentDiagnostic(&DocumentDiagnosticParams{TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"}})
	require.NoError(t, err)
	assert.Equal(t, diagnostics, requireRelatedFullDocumentDiagnosticReport(t, report).Items)
}

func TestSpxIntegrationSpriteRename(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		conflict     bool
	}{
		{"Safe", "onStart => { echo Runner }\n", false},
		{"ConstantConversion", "const target = \"Run\" + string(\"ner\")\nonStart => { Runner.turnTo target }\n", false},
		{"CapturedVariable", "onStart => { Player := 123; echo Runner; echo Player }\n", true},
		{"CapturedType", "onStart => { type Player int; var target *Runner; echo target }\n", true},
		{"Qualified", "onStart => { Player := 123; echo this.Runner; echo Player }\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string][]byte{
				"main.spx":                         []byte(tt.source),
				"Runner.spx":                       nil,
				"assets/index.json":                []byte(`{}`),
				"assets/sprites/Runner/index.json": []byte(`{}`),
			}
			s := newSpxIntegrationTestServer(t, files)
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)
			edit, err := s.renameResources([]XGoRenameResourceParams{{
				Resource: XGoResourceIdentifier{URI: "spx://resources/sprites/Runner"}, NewName: "Player",
			}})
			if tt.conflict {
				assert.ErrorContains(t, err, "binding would change")
				assert.Nil(t, edit)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, edit)
			files["main.spx"] = []byte(applyResourceRenameTestEdits(t, tt.source, edit.Changes["file:///main.spx"]))
			files["Player.spx"] = files["Runner.spx"]
			delete(files, "Runner.spx")
			files["assets/sprites/Player/index.json"] = files["assets/sprites/Runner/index.json"]
			delete(files, "assets/sprites/Runner/index.json")
			updated := newSpxIntegrationTestServer(t, files)
			t.Cleanup(updated.Close)
			requireNoDiagnostics(t, updated)
		})
	}
}
