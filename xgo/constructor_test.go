package xgo

import (
	gotypes "go/types"
	"io/fs"
	"testing"

	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTestProject(t *testing.T) {
	files := map[string]*File{
		"main.xgo":   file("import \"strings\"\n// Count stores the text length.\nvar Count = len(strings.ToUpper(\"text\"))\n"),
		"Record.gox": file("// Value stores the count.\nvar value int\n"),
	}
	plain := newTestProject(t, files, FeatAll)
	check := func(proj *Project) {
		t.Helper()

		assert.False(t, proj.Mod.IsClass("_fixture.gox"))
		assert.False(t, proj.Mod.IsClass(".spx"))
		_, err := proj.Importer.Import(testframework.PkgPath)
		assert.ErrorIs(t, err, fs.ErrNotExist)
		astPkg, err := proj.ASTPackage()
		require.NoError(t, err)
		require.Len(t, astPkg.Files, 2)
		require.NotNil(t, astPkg.Files["main.xgo"])
		require.NotNil(t, astPkg.Files["Record.gox"])
		assert.False(t, astPkg.Files["main.xgo"].IsClass)
		assert.True(t, astPkg.Files["Record.gox"].IsNormalGox)
		typeInfo, err := proj.TypeInfo()
		require.NoError(t, err)
		count := typeInfo.Pkg.Scope().Lookup("Count")
		require.NotNil(t, count)
		assert.Equal(t, gotypes.Typ[gotypes.Int], count.Type())
		assert.Nil(t, typeInfo.Pkg.Scope().Lookup("App"))
		for _, pkg := range typeInfo.Pkg.Imports() {
			assert.NotEqual(t, testframework.PkgPath, pkg.Path())
		}
		doc, err := proj.PkgDoc()
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"Count": "Count stores the text length.\n"}, doc.Vars)
		require.Len(t, doc.Types, 1)
		record := doc.Types["Record"]
		require.NotNil(t, record)
		assert.Equal(t, map[string]string{"value": "Value stores the count.\n"}, record.Fields)
	}
	check(plain)

	framework := newFrameworkTestProject(t, map[string]*File{
		"main_fixture.gox": file("onStart => {\n    measure 1\n}\n"),
	}, FeatAll)
	_, err := framework.TypeInfo()
	require.NoError(t, err)
	check(plain)
	check(newTestProject(t, files, FeatAll))
}

func TestNewFrameworkTestProject(t *testing.T) {
	files := map[string]*File{
		"main_fixture.gox":   file("onStart => {\n    measure 1\n}\n"),
		"Worker_fixture.gox": file("// Value stores the count.\nvar value int\n"),
	}
	first := newFrameworkTestProject(t, files, FeatAll)
	second := newFrameworkTestProject(t, files, FeatAll)
	assert.NotSame(t, first.Mod, second.Mod)
	assert.NotSame(t, first.Importer, second.Importer)
	assert.NotSame(t, first.Fset, second.Fset)
	firstTypes, err := first.TypeInfo()
	require.NoError(t, err)
	secondTypes, err := second.TypeInfo()
	require.NoError(t, err)
	firstPkg, err := first.Importer.Import(testframework.PkgPath)
	require.NoError(t, err)
	secondPkg, err := second.Importer.Import(testframework.PkgPath)
	require.NoError(t, err)
	assert.NotSame(t, firstPkg, secondPkg)
	for _, tt := range []struct {
		typeInfo *types.Info
		pkg      *gotypes.Package
	}{
		{typeInfo: firstTypes, pkg: firstPkg},
		{typeInfo: secondTypes, pkg: secondPkg},
	} {
		var callback gotypes.Object
		for ident, obj := range tt.typeInfo.Uses {
			if ident.Name == "onStart" {
				callback = obj
				break
			}
		}
		require.NotNil(t, callback)
		assert.Same(t, tt.pkg, callback.Pkg())
	}
	firstAST, err := first.ASTFile("Worker_fixture.gox")
	require.NoError(t, err)
	secondAST, err := second.ASTFile("Worker_fixture.gox")
	require.NoError(t, err)
	firstDoc, err := first.PkgDoc()
	require.NoError(t, err)
	secondDoc, err := second.PkgDoc()
	require.NoError(t, err)

	first.PutFile("Worker_fixture.gox", file("// Value stores a label.\nvar value string\n"))
	updatedTypes, err := first.TypeInfo()
	require.NoError(t, err)
	assert.NotSame(t, firstTypes, updatedTypes)
	worker := updatedTypes.Pkg.Scope().Lookup("Worker")
	require.NotNil(t, worker)
	value, _, _ := gotypes.LookupFieldOrMethod(worker.Type(), true, updatedTypes.Pkg, "value")
	require.NotNil(t, value)
	assert.Equal(t, gotypes.Typ[gotypes.String], value.Type())
	updatedAST, err := first.ASTFile("Worker_fixture.gox")
	require.NoError(t, err)
	assert.NotSame(t, firstAST, updatedAST)
	updatedDoc, err := first.PkgDoc()
	require.NoError(t, err)
	assert.NotSame(t, firstDoc, updatedDoc)
	require.NotNil(t, updatedDoc.Types["Worker"])
	require.NotNil(t, firstDoc.Types["Worker"])
	assert.Equal(t, "Value stores a label.\n", updatedDoc.Types["Worker"].Fields["value"])
	assert.Equal(t, "Value stores the count.\n", firstDoc.Types["Worker"].Fields["value"])

	unchangedTypes, err := second.TypeInfo()
	require.NoError(t, err)
	assert.Same(t, secondTypes, unchangedTypes)
	unchangedAST, err := second.ASTFile("Worker_fixture.gox")
	require.NoError(t, err)
	assert.Same(t, secondAST, unchangedAST)
	unchangedDoc, err := second.PkgDoc()
	require.NoError(t, err)
	assert.Same(t, secondDoc, unchangedDoc)
	require.NotNil(t, unchangedDoc.Types["Worker"])
	assert.Equal(t, "Value stores the count.\n", unchangedDoc.Types["Worker"].Fields["value"])
}
