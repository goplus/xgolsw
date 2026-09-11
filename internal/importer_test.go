package internal

import (
	"bytes"
	"errors"
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"io"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"
	"testing/iotest"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/gcexportdata"
)

func TestImporterImport(t *testing.T) {
	t.Run("ExportedSymbolsAndCache", func(t *testing.T) {
		const pkgPath = "example.com/sample"
		data := exportTestPackage(t, pkgPath, `package sample
const Limit = 7
var Current Value
type Value struct { Number int }
func (v Value) Double() int { return v.Number * 2 }
func Read() Value { return Current }
`, nil)
		var opened int
		reader := &exportReadCloser{Reader: bytes.NewReader(data)}
		imp := newImporter(func(path string) (io.ReadCloser, error) {
			assert.Equal(t, pkgPath, path)
			opened++
			return reader, nil
		})
		pkg, err := imp.Import(pkgPath)
		require.NoError(t, err)
		require.NotNil(t, pkg)
		assert.True(t, pkg.Complete())
		assert.Equal(t, pkgPath, pkg.Path())
		assert.Equal(t, "sample", pkg.Name())
		limit, ok := pkg.Scope().Lookup("Limit").(*gotypes.Const)
		require.True(t, ok)
		assert.Equal(t, "7", limit.Val().ExactString())
		valueObj := pkg.Scope().Lookup("Value")
		require.NotNil(t, valueObj)
		value, ok := valueObj.Type().(*gotypes.Named)
		require.True(t, ok)
		underlying, ok := value.Underlying().(*gotypes.Struct)
		require.True(t, ok)
		require.Equal(t, 1, underlying.NumFields())
		assert.Equal(t, "Number", underlying.Field(0).Name())
		assert.Equal(t, gotypes.Typ[gotypes.Int], underlying.Field(0).Type())
		require.Equal(t, 1, value.NumMethods())
		assert.Equal(t, "Double", value.Method(0).Name())
		current := pkg.Scope().Lookup("Current")
		require.NotNil(t, current)
		assert.Same(t, value, current.Type())
		read, ok := pkg.Scope().Lookup("Read").(*gotypes.Func)
		require.True(t, ok)
		require.Equal(t, 1, read.Signature().Results().Len())
		assert.Same(t, value, read.Signature().Results().At(0).Type())
		assert.True(t, reader.closed)

		cached, err := imp.Import(pkgPath)
		require.NoError(t, err)
		assert.Same(t, pkg, cached)
		assert.Equal(t, 1, opened)
	})

	t.Run("Unsafe", func(t *testing.T) {
		var opened bool
		imp := newImporter(func(string) (io.ReadCloser, error) {
			opened = true
			return nil, fs.ErrNotExist
		})
		pkg, err := imp.Import("unsafe")
		require.NoError(t, err)
		assert.Same(t, gotypes.Unsafe, pkg)
		assert.False(t, opened)
	})

	t.Run("DependencyIdentity", func(t *testing.T) {
		const depPath = "example.com/dependency"
		const consumerPath = "example.com/consumer"
		depData := exportTestPackage(t, depPath, `package dependency
type Value struct { Number int }
func Extra() {}
`, nil)
		files := fstest.MapFS{depPath + ".pkgexport": {Data: depData}}
		consumerData := exportTestPackage(t, consumerPath, `package consumer
import "example.com/dependency"
func Read() dependency.Value { return dependency.Value{} }
`, importerForTestFiles(files))
		files[consumerPath+".pkgexport"] = &fstest.MapFile{Data: consumerData}

		for _, tt := range []struct {
			name            string
			dependencyFirst bool
		}{
			{name: "DependencyFirst", dependencyFirst: true},
			{name: "ConsumerFirst"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				imp := importerForTestFiles(files)
				if tt.dependencyFirst {
					_, err := imp.Import(depPath)
					require.NoError(t, err)
				}
				consumer, err := imp.Import(consumerPath)
				require.NoError(t, err)
				read, ok := consumer.Scope().Lookup("Read").(*gotypes.Func)
				require.True(t, ok)
				require.Equal(t, 1, read.Signature().Results().Len())
				resultType, ok := read.Signature().Results().At(0).Type().(*gotypes.Named)
				require.True(t, ok)

				dep, err := imp.Import(depPath)
				require.NoError(t, err)
				assert.True(t, dep.Complete())
				assert.NotNil(t, dep.Scope().Lookup("Extra"))
				assert.Same(t, dep, resultType.Obj().Pkg())
				value := dep.Scope().Lookup("Value")
				require.NotNil(t, value)
				assert.Same(t, value.Type(), resultType)
			})
		}
	})

	t.Run("MissingExportAndRetry", func(t *testing.T) {
		const pkgPath = "example.com/retry"
		files := fstest.MapFS{}
		imp := importerForTestFiles(files)
		pkg, err := imp.Import(pkgPath)
		require.ErrorIs(t, err, fs.ErrNotExist)
		assert.ErrorContains(t, err, "failed to open package export file")
		assert.Nil(t, pkg)

		files[pkgPath+".pkgexport"] = &fstest.MapFile{Data: exportTestPackage(t, pkgPath, "package retry\nconst Ready = true\n", nil)}
		pkg, err = imp.Import(pkgPath)
		require.NoError(t, err)
		assert.NotNil(t, pkg.Scope().Lookup("Ready"))
	})

	for _, tt := range []struct {
		name    string
		reader  io.Reader
		message string
	}{
		{name: "EmptyExport", reader: bytes.NewReader(nil), message: "empty export data"},
		{name: "MalformedExport", reader: bytes.NewBufferString("invalid export"), message: "failed to parse package export data"},
		{name: "ReadError", reader: iotest.ErrReader(fs.ErrPermission), message: "permission denied"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader := &exportReadCloser{Reader: tt.reader}
			imp := newImporter(func(string) (io.ReadCloser, error) { return reader, nil })
			pkg, err := imp.Import("example.com/invalid")
			require.ErrorContains(t, err, "failed to parse package export data")
			assert.ErrorContains(t, err, tt.message)
			assert.Nil(t, pkg)
			assert.True(t, reader.closed)
		})
	}

	t.Run("MalformedExportAndRetry", func(t *testing.T) {
		const pkgPath = "example.com/retry"
		files := fstest.MapFS{pkgPath + ".pkgexport": {Data: []byte("invalid export")}}
		imp := importerForTestFiles(files)
		_, err := imp.Import(pkgPath)
		require.Error(t, err)
		files[pkgPath+".pkgexport"] = &fstest.MapFile{Data: exportTestPackage(t, pkgPath, "package retry\nconst Ready = true\n", nil)}
		pkg, err := imp.Import(pkgPath)
		require.NoError(t, err)
		assert.NotNil(t, pkg.Scope().Lookup("Ready"))
	})

	t.Run("ConcurrentImports", func(t *testing.T) {
		const pkgPath = "example.com/concurrent"
		data := exportTestPackage(t, pkgPath, "package concurrent\ntype Value struct { Number int }\n", nil)
		var opened int
		imp := newImporter(func(string) (io.ReadCloser, error) {
			opened++
			return io.NopCloser(bytes.NewReader(data)), nil
		})
		const count = 16
		pkgs := make([]*gotypes.Package, count)
		errs := make([]error, count)
		var wg sync.WaitGroup
		for i := range count {
			wg.Go(func() { pkgs[i], errs[i] = imp.Import(pkgPath) })
		}
		wg.Wait()
		for i := range count {
			require.NoError(t, errs[i])
			require.NotNil(t, pkgs[i])
			assert.Same(t, pkgs[0], pkgs[i])
		}
		assert.Equal(t, 1, opened)
	})

	t.Run("OpenError", func(t *testing.T) {
		wantErr := errors.New("export unavailable")
		imp := newImporter(func(string) (io.ReadCloser, error) { return nil, wantErr })
		pkg, err := imp.Import("example.com/unavailable")
		require.ErrorIs(t, err, wantErr)
		assert.Nil(t, pkg)
	})
}

func exportTestPackage(t *testing.T, path, source string, imp gotypes.Importer) []byte {
	t.Helper()

	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "fixture.go", source, 0)
	require.NoError(t, err)
	config := gotypes.Config{Importer: imp}
	pkg, err := config.Check(path, fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, gcexportdata.Write(&buf, fset, pkg))
	return buf.Bytes()
}

func importerForTestFiles(files fstest.MapFS) *importer {
	return newImporter(func(path string) (io.ReadCloser, error) { return files.Open(path + ".pkgexport") })
}

type exportReadCloser struct {
	io.Reader
	closed bool
}

func (r *exportReadCloser) Close() error {
	r.closed = true
	return nil
}
