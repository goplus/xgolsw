//go:build !test_no_pkgdata

package server

import (
	"testing"

	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpxIntegrationConfiguration(t *testing.T) {
	files := map[string][]byte{"main.spx": []byte("var Count int\nCount = 1\n")}
	s := newSpxIntegrationTestServer(t, files)
	proj := s.getProj()
	assert.Equal(t, "main", proj.PkgPath)
	assert.NotNil(t, proj.Importer)
	_, isProject, ok := proj.Module().ClassInfo("main.spx")
	assert.True(t, isProject)
	assert.True(t, ok)
	_, err := proj.TypeInfo()
	require.NoError(t, err)
	hover, err := s.textDocumentHover(&HoverParams{
		TextDocumentPositionParams: TextDocumentPositionParams{
			TextDocument: TextDocumentIdentifier{URI: "file:///main.spx"},
			Position:     Position{Line: 1},
		},
	})
	require.NoError(t, err)
	require.NotNil(t, hover)
	assert.Contains(t, hover.Contents.Value, `def-id="xgo:main?Game.Count"`)

	pkgs, err := s.listPkgs()
	require.NoError(t, err)
	assert.Contains(t, pkgs, SpxPkgPath)
	doc, err := s.lookupPkgDoc(SpxPkgPath)
	require.NoError(t, err)
	data, err := pkgdata.New(spxIntegrationPkgDataZip)
	require.NoError(t, err)
	wantDoc, err := data.GetPkgDoc(SpxPkgPath)
	require.NoError(t, err)
	assert.Equal(t, wantDoc, doc)
}
