package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerCompileAt(t *testing.T) {
	t.Run("UnregisteredClassfile", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.spx": []byte("println 1\n")})
		result, err := s.compileAt(s.getProj())
		require.NoError(t, err)
		require.NotNil(t, result)
		assert.Empty(t, result.mainSpxFile)
		assert.True(t, result.hasErrorSeverityDiagnostic)
		assert.Equal(t, map[DocumentURI][]Diagnostic{
			"file:///main.spx": {{
				Severity: SeverityError,
				Message:  "failed to parse source file: unknown file kind",
			}},
		}, result.diagnostics)
	})
}

func TestServerCompileAndGetASTFileForDocumentURI(t *testing.T) {
	t.Run("NoSpxFiles", func(t *testing.T) {
		s := newTestServer(t, nil)
		result, _, astFile, err := s.compileAndGetASTFileForDocumentURI("file:///main.spx")
		require.ErrorIs(t, err, errNoMainSpxFile)
		assert.Nil(t, result)
		assert.Nil(t, astFile)
	})
}
