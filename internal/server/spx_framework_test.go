package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerAnalyzeFramework(t *testing.T) {
	t.Run("UnregisteredClassfile", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"main.spx": []byte("println 1\n")})
		result, err := s.analyzeFramework(s.getProj())
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("UnavailableSDK", func(t *testing.T) {
		s := newSpxTestServer(t, map[string][]byte{"main.spx": []byte("println 1\n")})
		proj := s.getProj()
		proj.Importer = completionTestImporter{Importer: proj.Importer, unavailablePath: SpxPkgPath}
		result, err := s.analyzeFramework(proj)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}
