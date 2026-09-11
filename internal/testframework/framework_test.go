package testframework

import (
	"fmt"
	gotypes "go/types"
	"io/fs"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/xgomod"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBaseModule(t *testing.T) {
	first := NewBaseModule(t)
	second := NewBaseModule(t)
	first.Opt.Projects = NewModule(t).Opt.Projects
	require.NoError(t, first.ImportClasses())
	assert.True(t, first.IsClass("_fixture.gox"))
	for _, mod := range []*xgomod.Module{second, NewBaseModule(t)} {
		assert.Empty(t, mod.Opt.Projects)
		assert.Empty(t, mod.Opt.ClassMods)
		assert.False(t, mod.IsClass("_fixture.gox"))
		assert.False(t, mod.IsClass(".spx"))
		_, isProject, ok := mod.ClassInfo("Check_test.gox")
		assert.True(t, ok)
		assert.False(t, isProject)
	}
}

func TestNewModule(t *testing.T) {
	first := NewModule(t)
	second := NewModule(t)
	class, ok := first.LookupClass("_fixture.gox")
	require.True(t, ok)
	require.Len(t, class.PkgPaths, 1)
	require.Len(t, class.Works, 1)
	class.Ext = "_changed.gox"
	class.FullExt = "main_changed.gox"
	class.Class = "Changed"
	class.PkgPaths[0] = "example.com/changed"
	class.Works[0].Ext = "_changed.gox"
	class.Works[0].Class = "ChangedItem"
	class.Works[0].Prefix = "Task"
	require.NoError(t, first.ImportClasses())
	assert.True(t, first.IsClass("_changed.gox"))
	assert.False(t, first.IsClass("_fixture.gox"))

	for _, mod := range []*xgomod.Module{second, NewModule(t)} {
		assert.False(t, mod.IsClass("_changed.gox"))
		assert.False(t, mod.IsClass(".spx"))
		require.Len(t, mod.Opt.Projects, 1)
		project, ok := mod.LookupClass("_fixture.gox")
		require.True(t, ok)
		assert.Equal(t, "main_fixture.gox", project.FullExt)
		assert.Equal(t, "App", project.Class)
		assert.Equal(t, []string{PkgPath}, project.PkgPaths)
		require.Len(t, project.Works, 1)
		assert.Equal(t, modfile.Class{Ext: "_fixture.gox", Class: "Item", Embedded: true}, *project.Works[0])
	}
}

func TestNewImporter(t *testing.T) {
	firstFset := token.NewFileSet()
	firstFset.AddFile("padding.go", -1, 4096)
	first := NewImporter(t, firstFset)
	firstPkg, err := first.Import(PkgPath)
	require.NoError(t, err)
	secondFset := token.NewFileSet()
	secondPkg, err := NewImporter(t, secondFset).Import(PkgPath)
	require.NoError(t, err)
	assert.NotSame(t, firstPkg, secondPkg)
	firstApp := firstPkg.Scope().Lookup("App")
	secondApp := secondPkg.Scope().Lookup("App")
	require.NotNil(t, firstApp)
	require.NotNil(t, secondApp)
	assert.NotSame(t, firstApp.Type(), secondApp.Type())
	assert.Equal(t, "testdata/framework.go", firstFset.Position(firstApp.Pos()).Filename)
	assert.Equal(t, "testdata/framework.go", secondFset.Position(secondApp.Pos()).Filename)
	again, err := first.Import(PkgPath)
	require.NoError(t, err)
	assert.Same(t, firstPkg, again)
}

func TestNewBaseImporter(t *testing.T) {
	base := NewBaseImporter(t, token.NewFileSet())
	_, err := base.Import(PkgPath)
	assert.ErrorIs(t, err, fs.ErrNotExist)

	framework, err := NewImporter(t, token.NewFileSet()).Import(PkgPath)
	require.NoError(t, err)
	require.NotNil(t, framework.Scope().Lookup("App"))
	_, err = base.Import(PkgPath)
	assert.ErrorIs(t, err, fs.ErrNotExist)
	_, err = NewBaseImporter(t, token.NewFileSet()).Import(PkgPath)
	assert.ErrorIs(t, err, fs.ErrNotExist)

	for _, pkgPath := range []string{"strings", "github.com/qiniu/x/osx"} {
		pkg, err := base.Import(pkgPath)
		require.NoError(t, err)
		assert.Equal(t, pkgPath, pkg.Path())
		assert.True(t, pkg.Complete())
		again, err := base.Import(pkgPath)
		require.NoError(t, err)
		assert.Same(t, pkg, again)
	}
}

func TestImporterImport(t *testing.T) {
	for _, tt := range []struct {
		name string
		path string
	}{
		{name: "Spx", path: "github.com/goplus/spx"},
		{name: "VersionedSpx", path: "github.com/goplus/spx/v3"},
		{name: "SpxSubpackage", path: "github.com/goplus/spx/v3/internal/engine"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recorder := &failureRecorder{TB: t}
			called := false
			i := &importer{t: recorder, fallback: importerFunc(func(path string) (*gotypes.Package, error) {
				called = true
				return gotypes.NewPackage(path, "unexpected"), nil
			})}
			pkg, err := i.Import(tt.path)
			assert.Nil(t, pkg)
			assert.EqualError(t, err, "unexpected spx import: "+tt.path)
			assert.False(t, called)
			require.Len(t, recorder.messages, 1)
			assert.Contains(t, recorder.messages[0], "unexpected spx import: "+tt.path)
		})
	}
	t.Run("SimilarPackagePath", func(t *testing.T) {
		const pkgPath = "github.com/goplus/spxutils"
		want := gotypes.NewPackage(pkgPath, "spxutils")
		i := &importer{t: t, fallback: importerFunc(func(path string) (*gotypes.Package, error) {
			assert.Equal(t, pkgPath, path)
			return want, nil
		})}
		pkg, err := i.Import(pkgPath)
		require.NoError(t, err)
		assert.Same(t, want, pkg)
	})
}

func TestNewPkgDoc(t *testing.T) {
	first := NewPkgDoc(t)
	second := NewPkgDoc(t)
	require.NotNil(t, first.Types["App"])
	require.NotNil(t, first.Types["Item"])
	first.Doc = "Changed package documentation."
	first.Consts["Low"] = "Changed constant documentation."
	first.Funcs["RunWhen"] = "Changed function documentation."
	first.Types["App"].Methods["OnStart"] = "Changed method documentation."
	first.Types["Item"].Fields["Value"] = "Changed field documentation."
	for _, doc := range []*pkgdoc.PkgDoc{second, NewPkgDoc(t)} {
		assert.Equal(t, PkgPath, doc.Path)
		assert.Equal(t, "framework", doc.Name)
		assert.Equal(t, "Package framework provides a minimal classfile framework for language tests.\n", doc.Doc)
		assert.Equal(t, "Low and High are values used by framework methods.\n", doc.Consts["Low"])
		assert.Equal(t, "RunWhen accepts a deferred condition and a callback.\n", doc.Funcs["RunWhen"])
		require.NotNil(t, doc.Types["App"])
		require.NotNil(t, doc.Types["Item"])
		assert.Equal(t, "OnStart accepts a project callback.\n", doc.Types["App"].Methods["OnStart"])
		assert.Equal(t, "Value stores the work item's value.\n", doc.Types["Item"].Fields["Value"])
	}
}

type importerFunc func(string) (*gotypes.Package, error)

func (f importerFunc) Import(path string) (*gotypes.Package, error) {
	return f(path)
}

type failureRecorder struct {
	testing.TB
	messages []string
}

func (r *failureRecorder) Errorf(format string, args ...any) {
	r.messages = append(r.messages, fmt.Sprintf(format, args...))
}
