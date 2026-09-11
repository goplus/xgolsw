package pkgdata

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"maps"
	"slices"
	"sync"
	"testing"

	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListPkgs(t *testing.T) {
	for _, tt := range []struct {
		name     string
		embedded map[string]string
		custom   []byte
		want     []string
	}{
		{name: "Empty"},
		{
			name: "Embedded",
			embedded: map[string]string{
				"example.com/alpha.pkgexport": "export",
				"example.com/alpha.pkgdoc":    "{}",
				"example.com/docs.pkgdoc":     "{}",
				"README.txt":                  "ignored",
			},
			want: []string{"example.com/alpha"},
		},
		{
			name:   "CustomOnly",
			custom: newPkgdataZip(t, map[string]string{"example.com/beta.pkgexport": "export"}),
			want:   []string{"example.com/beta"},
		},
		{
			name: "MergeAndDeduplicate",
			embedded: map[string]string{
				"example.com/beta.pkgexport": "embedded",
				"example.com/zeta.pkgexport": "export",
			},
			custom: newPkgdataZip(t, map[string]string{
				"example.com/alpha.pkgexport": "export",
				"example.com/beta.pkgexport":  "custom",
			}),
			want: []string{"example.com/alpha", "example.com/beta", "example.com/zeta"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setTestPackageData(t, newPkgdataZip(t, tt.embedded), tt.custom)
			pkgs, err := ListPkgs()
			require.NoError(t, err)
			if len(tt.want) == 0 {
				assert.Empty(t, pkgs)
			} else {
				assert.Equal(t, tt.want, pkgs)
			}
		})
	}

	for _, tt := range []struct {
		name     string
		embedded []byte
		custom   []byte
		message  string
	}{
		{name: "InvalidEmbedded", embedded: []byte("invalid zip"), message: "failed to list embed packages"},
		{name: "InvalidCustom", embedded: newPkgdataZip(t, nil), custom: []byte("invalid zip"), message: "failed to list custom packages"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setTestPackageData(t, tt.embedded, tt.custom)
			pkgs, err := ListPkgs()
			require.ErrorIs(t, err, zip.ErrFormat)
			assert.ErrorContains(t, err, tt.message)
			assert.Nil(t, pkgs)
		})
	}
}

func TestOpenExport(t *testing.T) {
	const pkgPath = "example.com/sample"
	for _, tt := range []struct {
		name   string
		custom []byte
		want   string
	}{
		{name: "Embedded", want: "embedded export"},
		{name: "CustomOverridesEmbedded", custom: newPkgdataZip(t, map[string]string{pkgPath + ".pkgexport": "custom export"}), want: "custom export"},
		{name: "MissingCustomFallsBack", custom: newPkgdataZip(t, map[string]string{"example.com/other.pkgexport": "other export"}), want: "embedded export"},
		{name: "EmptyCustomFallsBack", custom: newPkgdataZip(t, nil), want: "embedded export"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setTestPackageData(t, newPkgdataZip(t, map[string]string{pkgPath + ".pkgexport": "embedded export"}), tt.custom)
			rc, err := OpenExport(pkgPath)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, rc.Close()) })
			data, err := io.ReadAll(rc)
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(data))
		})
	}

	for _, tt := range []struct {
		name     string
		embedded []byte
		custom   []byte
		wantErr  error
		message  string
	}{
		{name: "Missing", embedded: newPkgdataZip(t, nil), custom: newPkgdataZip(t, nil), wantErr: fs.ErrNotExist, message: pkgPath},
		{name: "InvalidEmbedded", embedded: []byte("invalid zip"), wantErr: zip.ErrFormat, message: "failed to create zip reader"},
		{
			name:     "InvalidCustomDoesNotFallBack",
			embedded: newPkgdataZip(t, map[string]string{pkgPath + ".pkgexport": "embedded export"}),
			custom:   []byte("invalid zip"), wantErr: zip.ErrFormat, message: "failed to open custom package export file",
		},
		{
			name:     "UnsupportedCompressionDoesNotFallBack",
			embedded: newPkgdataZip(t, map[string]string{pkgPath + ".pkgexport": "embedded export"}),
			custom:   unsupportedCompressionZip(t, pkgPath+".pkgexport"), wantErr: zip.ErrAlgorithm, message: "failed to open custom package export file",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setTestPackageData(t, tt.embedded, tt.custom)
			rc, err := OpenExport(pkgPath)
			require.ErrorIs(t, err, tt.wantErr)
			assert.ErrorContains(t, err, tt.message)
			assert.Nil(t, rc)
		})
	}
}

func TestGetPkgDoc(t *testing.T) {
	const pkgPath = "example.com/sample"
	embeddedDoc := &pkgdoc.PkgDoc{
		Path: pkgPath, Name: "sample", Doc: "Package sample provides test data.\n",
		Vars:   map[string]string{"Current": "Current holds the value.\n"},
		Consts: map[string]string{"Limit": "Limit bounds the value.\n"},
		Funcs:  map[string]string{"Read": "Read returns the value.\n"},
		Types: map[string]*pkgdoc.TypeDoc{"Value": {
			Doc:         "Value holds a number.\n",
			Fields:      map[string]string{"Number": "Number is the value.\n"},
			Methods:     map[string]string{"Get": "Get returns the number.\n"},
			EnumMembers: map[string]string{"First": "First is the initial value.\n"},
		}},
	}
	customDoc := &pkgdoc.PkgDoc{Path: pkgPath, Name: "sample", Doc: "Custom documentation.\n"}
	encodeDoc := func(doc *pkgdoc.PkgDoc) string {
		t.Helper()
		data, err := json.Marshal(doc)
		require.NoError(t, err)
		return string(data)
	}
	for _, tt := range []struct {
		name   string
		custom []byte
		want   *pkgdoc.PkgDoc
	}{
		{name: "Embedded", want: embeddedDoc},
		{name: "CustomOverridesEmbedded", custom: newPkgdataZip(t, map[string]string{pkgPath + ".pkgdoc": encodeDoc(customDoc)}), want: customDoc},
		{name: "MissingCustomFallsBack", custom: newPkgdataZip(t, map[string]string{"example.com/other.pkgdoc": "{}"}), want: embeddedDoc},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setTestPackageData(t, newPkgdataZip(t, map[string]string{pkgPath + ".pkgdoc": encodeDoc(embeddedDoc)}), tt.custom)
			doc, err := GetPkgDoc(pkgPath)
			require.NoError(t, err)
			assert.Equal(t, tt.want, doc)
			cachedDoc, err := GetPkgDoc(pkgPath)
			require.NoError(t, err)
			assert.Same(t, doc, cachedDoc)
		})
	}

	for _, tt := range []struct {
		name     string
		embedded []byte
		custom   []byte
		wantErr  error
		message  string
	}{
		{name: "Missing", embedded: newPkgdataZip(t, nil), custom: newPkgdataZip(t, nil), wantErr: fs.ErrNotExist, message: pkgPath},
		{name: "InvalidEmbedded", embedded: []byte("invalid zip"), wantErr: zip.ErrFormat, message: "failed to create zip reader"},
		{
			name:     "InvalidCustomDoesNotFallBack",
			embedded: newPkgdataZip(t, map[string]string{pkgPath + ".pkgdoc": encodeDoc(embeddedDoc)}),
			custom:   []byte("invalid zip"), wantErr: zip.ErrFormat, message: "failed to get custom package doc",
		},
		{
			name:     "InvalidJSONDoesNotFallBack",
			embedded: newPkgdataZip(t, map[string]string{pkgPath + ".pkgdoc": encodeDoc(embeddedDoc)}),
			custom:   newPkgdataZip(t, map[string]string{pkgPath + ".pkgdoc": "{"}), wantErr: io.ErrUnexpectedEOF, message: "failed to decode doc",
		},
		{
			name:     "UnsupportedCompressionDoesNotFallBack",
			embedded: newPkgdataZip(t, map[string]string{pkgPath + ".pkgdoc": encodeDoc(embeddedDoc)}),
			custom:   unsupportedCompressionZip(t, pkgPath+".pkgdoc"), wantErr: zip.ErrAlgorithm, message: "failed to open doc file",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setTestPackageData(t, tt.embedded, tt.custom)
			doc, err := GetPkgDoc(pkgPath)
			require.ErrorIs(t, err, tt.wantErr)
			assert.ErrorContains(t, err, tt.message)
			assert.Nil(t, doc)
			doc, err = GetPkgDoc(pkgPath)
			require.ErrorIs(t, err, tt.wantErr, "failed lookups must not be cached as successful results")
			assert.Nil(t, doc)

			SetCustomPkgdataZip(newPkgdataZip(t, map[string]string{pkgPath + ".pkgdoc": encodeDoc(customDoc)}))
			doc, err = GetPkgDoc(pkgPath)
			require.NoError(t, err)
			assert.Equal(t, customDoc, doc)
		})
	}
}

func TestSetCustomPkgdataZip(t *testing.T) {
	t.Run("PackageList", func(t *testing.T) {
		const pkgPath = "example.com/custom"
		setTestPackageData(t, newPkgdataZip(t, nil), nil)
		SetCustomPkgdataZip(newPkgdataZip(t, map[string]string{pkgPath + ".pkgexport": "export"}))
		pkgs, err := ListPkgs()
		require.NoError(t, err)
		assert.Equal(t, []string{pkgPath}, pkgs)

		SetCustomPkgdataZip(nil)
		pkgs, err = ListPkgs()
		require.NoError(t, err)
		assert.Empty(t, pkgs)
	})

	t.Run("DocumentationUpdates", func(t *testing.T) {
		const pkgPath = "example.com/sample"
		setTestPackageData(t, newPkgdataZip(t, map[string]string{pkgPath + ".pkgdoc": `{"Doc":"embedded"}`}), nil)
		for _, tt := range []struct {
			data []byte
			want string
		}{
			{want: "embedded"},
			{data: newPkgdataZip(t, map[string]string{pkgPath + ".pkgdoc": `{"Doc":"custom"}`}), want: "custom"},
			{data: newPkgdataZip(t, map[string]string{pkgPath + ".pkgdoc": `{"Doc":"replacement"}`}), want: "replacement"},
			{want: "embedded"},
		} {
			SetCustomPkgdataZip(tt.data)
			doc, err := GetPkgDoc(pkgPath)
			require.NoError(t, err)
			require.Equal(t, tt.want, doc.Doc)
		}
	})

	t.Run("OpenExportSnapshot", func(t *testing.T) {
		const pkgPath = "example.com/sample"
		setTestPackageData(t, newPkgdataZip(t, nil), newPkgdataZip(t, map[string]string{pkgPath + ".pkgexport": "original"}))
		original, err := OpenExport(pkgPath)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, original.Close()) })

		SetCustomPkgdataZip(newPkgdataZip(t, map[string]string{pkgPath + ".pkgexport": "replacement"}))
		replacement, err := OpenExport(pkgPath)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, replacement.Close()) })
		SetCustomPkgdataZip(nil)

		data, err := io.ReadAll(original)
		require.NoError(t, err)
		assert.Equal(t, "original", string(data))
		data, err = io.ReadAll(replacement)
		require.NoError(t, err)
		assert.Equal(t, "replacement", string(data))
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		const pkgPath = "example.com/sample"
		embedded := newPkgdataZip(t, map[string]string{
			pkgPath + ".pkgexport": "embedded",
			pkgPath + ".pkgdoc":    `{"Doc":"embedded"}`,
		})
		custom := newPkgdataZip(t, map[string]string{
			pkgPath + ".pkgexport": "custom",
			pkgPath + ".pkgdoc":    `{"Doc":"custom"}`,
		})
		replacement := newPkgdataZip(t, map[string]string{
			pkgPath + ".pkgexport": "replacement",
			pkgPath + ".pkgdoc":    `{"Doc":"replacement"}`,
		})
		setTestPackageData(t, embedded, nil)
		start := make(chan struct{})
		var wg sync.WaitGroup
		wg.Go(func() {
			<-start
			for range 100 {
				SetCustomPkgdataZip(custom)
				SetCustomPkgdataZip(nil)
			}
			SetCustomPkgdataZip(replacement)
		})
		for range 4 {
			wg.Go(func() {
				<-start
				for range 100 {
					pkgs, err := ListPkgs()
					assert.NoError(t, err)
					assert.Equal(t, []string{pkgPath}, pkgs)
					rc, err := OpenExport(pkgPath)
					if !assert.NoError(t, err) {
						return
					}
					t.Cleanup(func() { require.NoError(t, rc.Close()) })
					data, err := io.ReadAll(rc)
					assert.NoError(t, err)
					assert.Contains(t, []string{"embedded", "custom", "replacement"}, string(data))
					doc, err := GetPkgDoc(pkgPath)
					if assert.NoError(t, err) && assert.NotNil(t, doc) {
						assert.Contains(t, []string{"embedded", "custom", "replacement"}, doc.Doc)
					}
				}
			})
		}
		close(start)
		wg.Wait()

		doc, err := GetPkgDoc(pkgPath)
		require.NoError(t, err)
		assert.Equal(t, "replacement", doc.Doc)
	})
}

func setTestPackageData(t *testing.T, embedded, custom []byte) {
	t.Helper()

	// These tests own package-level data and must not run in parallel.
	pkgdataMu.Lock()
	originalEmbedded, originalCustom := pkgdataZip, customPkgdataZip
	t.Cleanup(func() {
		pkgdataMu.Lock()
		pkgdataZip, customPkgdataZip = originalEmbedded, originalCustom
		pkgDocCache.Clear()
		pkgdataMu.Unlock()
	})
	pkgdataZip, customPkgdataZip = embedded, custom
	pkgDocCache.Clear()
	pkgdataMu.Unlock()
}

func newPkgdataZip(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range slices.Sorted(maps.Keys(files)) {
		w, err := zw.Create(name)
		require.NoError(t, err)
		_, err = io.WriteString(w, files[name])
		require.NoError(t, err)
	}
	require.NoError(t, zw.Close())
	return buf.Bytes()
}

func unsupportedCompressionZip(t *testing.T, name string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	_, err := zw.CreateRaw(&zip.FileHeader{Name: name, Method: 0xffff})
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	return buf.Bytes()
}
