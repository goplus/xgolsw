//go:build !test_no_pkgdata

package server

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestServerWorkspaceDiagnosticSpxIntegration(t *testing.T) {
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
		s := newSpxIntegrationTestServer(t, m)

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
		s := newSpxIntegrationTestServer(t, m)

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
