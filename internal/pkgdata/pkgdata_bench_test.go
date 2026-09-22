package pkgdata

import (
	"fmt"
	"io"
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"
)

func BenchmarkData(b *testing.B) {
	files := make(map[string]string)
	for i := range 100 {
		pkgPath := fmt.Sprintf("example.com/pkg%03d", i)
		files[pkgPath+".pkgexport"] = "package export data"
		files[pkgPath+".pkgdoc"] = `{"Doc":"Package documentation."}`
	}
	archive := newPkgDataZip(b, files)

	b.Run("ListPkgs", func(b *testing.B) {
		data, err := New(archive)
		require.NoError(b, err)
		b.ReportAllocs()
		for b.Loop() {
			_, err := data.ListPkgs()
			require.NoError(b, err)
		}
	})
	b.Run("OpenExport", func(b *testing.B) {
		data, err := New(archive)
		require.NoError(b, err)
		b.ReportAllocs()
		for b.Loop() {
			rc, err := data.OpenExport("example.com/pkg099")
			require.NoError(b, err)
			_, err = io.Copy(io.Discard, rc)
			require.NoError(b, err)
			require.NoError(b, rc.Close())
		}
	})
	b.Run("MissingPkgDoc", func(b *testing.B) {
		data, err := New(archive)
		require.NoError(b, err)
		b.ReportAllocs()
		for b.Loop() {
			_, err := data.GetPkgDoc("example.com/missing")
			require.ErrorIs(b, err, fs.ErrNotExist)
		}
	})
	b.Run("ImportCompletion", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			data, err := New(archive)
			require.NoError(b, err)
			packages, err := data.ListPkgs()
			require.NoError(b, err)
			for _, pkgPath := range packages {
				_, err := data.GetPkgDoc(pkgPath)
				require.NoError(b, err)
			}
		}
	})
}
