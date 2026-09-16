//go:build !test_no_pkgdata

package server

import (
	"testing"

	"github.com/goplus/xgolsw/internal"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSpx(t *testing.T) {
	files := map[string][]byte{"main.spx": []byte("var Count int\nCount = 1\n")}
	proj := xgo.NewProject(nil, newFileMap(files), xgo.FeatAll)
	s := New(proj, nil, fileMapGetter(files), &MockScheduler{})
	assert.Same(t, proj, s.getProj())
	assert.Equal(t, "main", proj.PkgPath)
	assert.Same(t, internal.Importer, proj.Importer)
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
	wantDoc, err := pkgdata.GetPkgDoc(SpxPkgPath)
	require.NoError(t, err)
	assert.Same(t, wantDoc, doc)
}
