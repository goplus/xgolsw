package config

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"go/constant"
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/gcexportdata"
)

func TestNewProject(t *testing.T) {
	t.Run("PlainXGo", func(t *testing.T) {
		project, _, err := NewProject(map[string]*xgo.File{
			"main.xgo": {Content: []byte("var Count = 1\n")},
		}, Options{PkgData: testPkgData(t)})
		require.NoError(t, err)
		assert.False(t, project.Module().IsClass(".spx"))
		assert.True(t, project.Module().IsClass("_test.gox"))
		_, err = project.TypeInfo()
		require.NoError(t, err)
	})

	t.Run("ClassfilesAndAutoImports", func(t *testing.T) {
		classes := "project main.actor App example.com/framework\nclass -embed -prefix=Actor *.actor Item\nimport f example.com/framework\n"
		files := map[string]*xgo.File{
			"main.actor":   {Content: []byte("var (\nCount = measure(1)\nLimit = f.Limit\n)\n")},
			"Worker.actor": {Content: []byte("var Count = 1\n")},
		}
		options := Options{ClassfileConfig: classes, PkgData: testPkgData(t)}
		first, data, err := NewProject(files, options)
		require.NoError(t, err)
		second, _, err := NewProject(files, options)
		require.NoError(t, err)
		assert.NotSame(t, first.Module(), second.Module())
		assert.NotSame(t, first.Importer, second.Importer)
		info, err := first.TypeInfo()
		require.NoError(t, err)
		assert.NotNil(t, info.Pkg.Scope().Lookup("ActorWorker"))
		firstPackage, err := first.Importer.Import(testframework.PkgPath)
		require.NoError(t, err)
		secondPackage, err := second.Importer.Import(testframework.PkgPath)
		require.NoError(t, err)
		assert.NotSame(t, firstPackage, secondPackage)
		doc, err := data.GetPkgDoc(testframework.PkgPath)
		require.NoError(t, err)
		assert.Equal(t, testframework.NewPkgDoc(t), doc)
		assert.False(t, first.Module().IsClass(".spx"))
		snapshot := first.Snapshot()
		options.ClassfileConfig = ""
		plain, _, err := NewProject(nil, options)
		require.NoError(t, err)
		assert.False(t, plain.Module().IsClass(".actor"))
		assert.Same(t, options.PkgData, data)
		_, err = second.TypeInfo()
		require.NoError(t, err)
		assert.Same(t, first.Module(), snapshot.Module())
		_, err = snapshot.TypeInfo()
		require.NoError(t, err)
	})

	for _, tt := range []struct{ name, source string }{
		{"InvalidSyntax", "project"},
		{"InvalidExtension", "project . App example.com/framework"},
		{"MissingProject", "class *.actor Item"},
		{"InvalidImport", "import alias example.com/framework"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			project, data, err := NewProject(nil, Options{ClassfileConfig: tt.source})
			require.ErrorContains(t, err, "invalid classfile configuration")
			assert.Nil(t, project)
			assert.Nil(t, data)
		})
	}
}

func TestNewProjectDefaults(t *testing.T) {
	project, data, err := NewProject(nil, Options{})
	require.NoError(t, err)
	assert.True(t, project.Module().IsClass("_test.gox"))
	assert.False(t, project.Module().IsClass(".spx"))
	packages, err := data.ListPkgs()
	require.NoError(t, err)
	assert.NotContains(t, packages, testframework.PkgPath)
	assert.NotContains(t, packages, "github.com/goplus/spx/v3")
}

func testPkgData(t *testing.T) *pkgdata.Data {
	t.Helper()
	data, err := pkgdata.New(testframework.NewPkgDataZip(t))
	require.NoError(t, err)
	return data
}

func TestNewProjectPkgDataIsolation(t *testing.T) {
	const pkgPath = "example.com/versioned"
	var projects []*xgo.Project
	var archives []*pkgdata.Data
	for _, version := range []string{"first", "second"} {
		pkg := gotypes.NewPackage(pkgPath, "versioned")
		pkg.Scope().Insert(gotypes.NewConst(token.NoPos, pkg, "Version", gotypes.Typ[gotypes.String], constant.MakeString(version)))
		pkg.MarkComplete()
		var buf bytes.Buffer
		writer := zip.NewWriter(&buf)
		export, err := writer.Create(pkgPath + ".pkgexport")
		require.NoError(t, err)
		require.NoError(t, gcexportdata.Write(export, token.NewFileSet(), pkg))
		doc, err := writer.Create(pkgPath + ".pkgdoc")
		require.NoError(t, err)
		require.NoError(t, json.NewEncoder(doc).Encode(pkgdoc.PkgDoc{Doc: version}))
		require.NoError(t, writer.Close())
		data, err := pkgdata.New(buf.Bytes())
		require.NoError(t, err)
		project, data, err := NewProject(nil, Options{PkgData: data})
		require.NoError(t, err)
		projects = append(projects, project)
		archives = append(archives, data)
	}
	// Defer imports and documentation reads until both instances exist.
	for i, version := range []string{"first", "second"} {
		pkg, err := projects[i].Importer.Import(pkgPath)
		require.NoError(t, err)
		value, ok := pkg.Scope().Lookup("Version").(*gotypes.Const)
		require.True(t, ok)
		assert.Equal(t, version, constant.StringVal(value.Val()))
		doc, err := archives[i].GetPkgDoc(pkgPath)
		require.NoError(t, err)
		assert.Equal(t, version, doc.Doc)
	}
}
