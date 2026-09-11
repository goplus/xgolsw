//go:build test_no_pkgdata

package pkgdata

import (
	"archive/zip"
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmbeddedPackageDataIsEmpty(t *testing.T) {
	reader, err := zip.NewReader(bytes.NewReader(pkgdataZip), int64(len(pkgdataZip)))
	require.NoError(t, err)
	require.Empty(t, reader.File, "run with -tags=test_no_pkgdata -overlay=testdata/no-pkgdata/overlay.json from the repository root")
}
