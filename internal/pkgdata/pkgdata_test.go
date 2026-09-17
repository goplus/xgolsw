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

func TestDataListPkgs(t *testing.T) {
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
			custom: newPkgDataZip(t, map[string]string{"example.com/beta.pkgexport": "export"}),
			want:   []string{"example.com/beta"},
		},
		{
			name: "MergeAndDeduplicate",
			embedded: map[string]string{
				"example.com/beta.pkgexport": "embedded",
				"example.com/zeta.pkgexport": "export",
			},
			custom: newPkgDataZip(t, map[string]string{
				"example.com/alpha.pkgexport": "export",
				"example.com/beta.pkgexport":  "custom",
			}),
			want: []string{"example.com/alpha", "example.com/beta", "example.com/zeta"},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkgData, err := newData(newPkgDataZip(t, tt.embedded), tt.custom)
			require.NoError(t, err)
			pkgs, err := pkgData.ListPkgs()
			require.NoError(t, err)
			if len(tt.want) == 0 {
				assert.Empty(t, pkgs)
			} else {
				require.Equal(t, tt.want, pkgs)
				pkgs[0] = "modified"
				pkgs, err = pkgData.ListPkgs()
				require.NoError(t, err)
				assert.Equal(t, tt.want, pkgs)
			}
		})
	}
}

func TestDataOpenExport(t *testing.T) {
	const pkgPath = "example.com/sample"
	for _, tt := range []struct {
		name   string
		custom []byte
		want   string
	}{
		{name: "Embedded", want: "embedded export"},
		{name: "CustomOverridesEmbedded", custom: newPkgDataZip(t, map[string]string{pkgPath + ".pkgexport": "custom export"}), want: "custom export"},
		{name: "MissingCustomFallsBack", custom: newPkgDataZip(t, map[string]string{"example.com/other.pkgexport": "other export"}), want: "embedded export"},
		{name: "EmptyCustomFallsBack", custom: newPkgDataZip(t, nil), want: "embedded export"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkgData, err := newData(newPkgDataZip(t, map[string]string{pkgPath + ".pkgexport": "embedded export"}), tt.custom)
			require.NoError(t, err)
			rc, err := pkgData.OpenExport(pkgPath)
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
		{name: "Missing", embedded: newPkgDataZip(t, nil), custom: newPkgDataZip(t, nil), wantErr: fs.ErrNotExist, message: pkgPath},
		{
			name:     "UnsupportedBaseCompression",
			embedded: unsupportedCompressionZip(t, pkgPath+".pkgexport"), wantErr: zip.ErrAlgorithm,
		},
		{
			name:     "UnsupportedCompressionDoesNotFallBack",
			embedded: newPkgDataZip(t, map[string]string{pkgPath + ".pkgexport": "embedded export"}),
			custom:   unsupportedCompressionZip(t, pkgPath+".pkgexport"), wantErr: zip.ErrAlgorithm, message: "failed to open custom package export file",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkgData, err := newData(tt.embedded, tt.custom)
			require.NoError(t, err)
			rc, err := pkgData.OpenExport(pkgPath)
			require.ErrorIs(t, err, tt.wantErr)
			assert.ErrorContains(t, err, tt.message)
			assert.Nil(t, rc)
		})
	}
}

func TestDataGetPkgDoc(t *testing.T) {
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
		{name: "CustomOverridesEmbedded", custom: newPkgDataZip(t, map[string]string{pkgPath + ".pkgdoc": encodeDoc(customDoc)}), want: customDoc},
		{name: "MissingCustomFallsBack", custom: newPkgDataZip(t, map[string]string{"example.com/other.pkgdoc": "{}"}), want: embeddedDoc},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkgData, err := newData(newPkgDataZip(t, map[string]string{pkgPath + ".pkgdoc": encodeDoc(embeddedDoc)}), tt.custom)
			require.NoError(t, err)
			doc, err := pkgData.GetPkgDoc(pkgPath)
			require.NoError(t, err)
			assert.Equal(t, tt.want, doc)
			cachedDoc, err := pkgData.GetPkgDoc(pkgPath)
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
		{name: "Missing", embedded: newPkgDataZip(t, nil), custom: newPkgDataZip(t, nil), wantErr: fs.ErrNotExist, message: pkgPath},
		{
			name:     "UnsupportedBaseCompression",
			embedded: unsupportedCompressionZip(t, pkgPath+".pkgdoc"), wantErr: zip.ErrAlgorithm, message: "failed to open doc file",
		},
		{
			name:     "InvalidBaseJSON",
			embedded: newPkgDataZip(t, map[string]string{pkgPath + ".pkgdoc": "{"}), wantErr: io.ErrUnexpectedEOF, message: "failed to decode doc",
		},
		{
			name:     "InvalidJSONDoesNotFallBack",
			embedded: newPkgDataZip(t, map[string]string{pkgPath + ".pkgdoc": encodeDoc(embeddedDoc)}),
			custom:   newPkgDataZip(t, map[string]string{pkgPath + ".pkgdoc": "{"}), wantErr: io.ErrUnexpectedEOF, message: "failed to decode doc",
		},
		{
			name:     "UnsupportedCompressionDoesNotFallBack",
			embedded: newPkgDataZip(t, map[string]string{pkgPath + ".pkgdoc": encodeDoc(embeddedDoc)}),
			custom:   unsupportedCompressionZip(t, pkgPath+".pkgdoc"), wantErr: zip.ErrAlgorithm, message: "failed to open doc file",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkgData, err := newData(tt.embedded, tt.custom)
			require.NoError(t, err)
			doc, err := pkgData.GetPkgDoc(pkgPath)
			require.ErrorIs(t, err, tt.wantErr)
			assert.ErrorContains(t, err, tt.message)
			assert.Nil(t, doc)
			doc, err = pkgData.GetPkgDoc(pkgPath)
			require.ErrorIs(t, err, tt.wantErr, "failed lookups must not be cached as successful results")
			assert.Nil(t, doc)
		})
	}
}

func newPkgDataZip(t testing.TB, files map[string]string) []byte {
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

func TestNew(t *testing.T) {
	const pkgPath = "example.com/sample"
	var instances []*Data
	for _, name := range []string{"first", "second"} {
		archive := newPkgDataZip(t, map[string]string{
			pkgPath + ".pkgexport": name,
			pkgPath + ".pkgdoc":    `{"Doc":"` + name + `"}`,
		})
		data, err := New(archive)
		require.NoError(t, err)
		clear(archive)
		instances = append(instances, data)
	}
	for i, name := range []string{"first", "second"} {
		data := instances[i]
		doc, err := data.GetPkgDoc(pkgPath)
		require.NoError(t, err)
		assert.Equal(t, name, doc.Doc)
		rc, err := data.OpenExport(pkgPath)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, rc.Close()) })
		content, err := io.ReadAll(rc)
		require.NoError(t, err)
		assert.Equal(t, name, string(content))
	}
	invalid, err := New([]byte("invalid"))
	assert.ErrorIs(t, err, zip.ErrFormat)
	assert.ErrorContains(t, err, "failed to read base package archive")
	assert.Nil(t, invalid)
	empty, err := New(newPkgDataZip(t, nil))
	require.NoError(t, err)
	packages, err := empty.ListPkgs()
	require.NoError(t, err)
	assert.Empty(t, packages)
}

func TestNewWithEmbedded(t *testing.T) {
	const pkgPath = "example.com/sample"
	archive := newPkgDataZip(t, map[string]string{pkgPath + ".pkgdoc": `{"Doc":"custom"}`})
	data, err := NewWithEmbedded(archive)
	require.NoError(t, err)
	clear(archive)
	doc, err := data.GetPkgDoc(pkgPath)
	require.NoError(t, err)
	assert.Equal(t, "custom", doc.Doc)
	embedded, err := NewWithEmbedded(nil)
	require.NoError(t, err)
	_, err = embedded.GetPkgDoc(pkgPath)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	invalid, err := NewWithEmbedded([]byte("invalid"))
	assert.ErrorIs(t, err, zip.ErrFormat)
	assert.ErrorContains(t, err, "failed to read custom package archive")
	assert.Nil(t, invalid)
}

func TestDataConcurrentAccess(t *testing.T) {
	const pkgPath = "example.com/sample"
	data, err := New(newPkgDataZip(t, map[string]string{
		pkgPath + ".pkgexport": "export",
		pkgPath + ".pkgdoc":    `{"Doc":"documentation"}`,
	}))
	require.NoError(t, err)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for range 20 {
				packages, err := data.ListPkgs()
				assert.NoError(t, err)
				assert.Equal(t, []string{pkgPath}, packages)
				clear(packages)
				rc, err := data.OpenExport(pkgPath)
				if !assert.NoError(t, err) {
					return
				}
				content, err := io.ReadAll(rc)
				assert.NoError(t, err)
				assert.NoError(t, rc.Close())
				assert.Equal(t, "export", string(content))
				doc, err := data.GetPkgDoc(pkgPath)
				if !assert.NoError(t, err) {
					return
				}
				assert.Equal(t, "documentation", doc.Doc)
			}
		})
	}
	wg.Wait()
}
