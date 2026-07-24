package main

import (
	"archive/zip"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateSpxEnginePackage(t *testing.T) {
	pkgPath := "github.com/goplus/spx/v3/pkg/spx/pkg/engine"
	outputFile := filepath.Join(t.TempDir(), "pkgdata.zip")
	require.NoError(t, generate([]string{pkgPath}, outputFile))

	zipReader, err := zip.OpenReader(outputFile)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, zipReader.Close()) })

	fileNames := make([]string, 0, len(zipReader.File))
	for _, file := range zipReader.File {
		fileNames = append(fileNames, file.Name)
	}
	assert.Contains(t, fileNames, pkgPath+".pkgexport")
	assert.Contains(t, fileNames, pkgPath+".pkgdoc")
}
