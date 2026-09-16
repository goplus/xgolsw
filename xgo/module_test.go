package xgo

import (
	"slices"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/goplus/mod/xgomod"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gomodfile "golang.org/x/mod/modfile"
)

func TestNewModule(t *testing.T) {
	t.Run("RegistrationIsolation", func(t *testing.T) {
		config := testframework.NewModule(t).Module
		project := config.Opt.Projects[0]
		project.AutoLambdas = map[string]int{"onStart": 0}
		project.Import = []*modfile.Import{{Name: "fmt", Path: "fmt"}}
		project.Pack = &modfile.Pack{Directory: "assets", IndexFile: "index.json"}
		mod := newTestModule(t, config)
		class, ok := mod.LookupClass("_fixture.gox")
		require.True(t, ok)
		assert.NotSame(t, project, class)
		for _, builtin := range []*modfile.Project{xgomod.TestProject, xgomod.GshProject} {
			loaded, ok := mod.LookupClass(builtin.Ext)
			require.True(t, ok)
			assert.NotSame(t, builtin, loaded)
		}

		project.Ext = ".other"
		project.Works[0].Ext = ".work"
		project.Works[0].Prefix = "Other"
		project.PkgPaths[0] = "example.com/other"
		project.AutoLambdas["onStart"] = 1
		project.Import[0].Path = "example.com/fmt"
		project.Pack.Directory = "other"
		config.Opt.Projects = nil
		assert.True(t, mod.IsClass("_fixture.gox"))
		assert.False(t, mod.IsClass(".other"))
		assert.Equal(t, "_fixture.gox", class.Works[0].Ext)
		assert.Empty(t, class.Works[0].Prefix)
		assert.Equal(t, []string{testframework.PkgPath}, class.PkgPaths)
		assert.Equal(t, "fmt", class.Import[0].Path)
		assert.Equal(t, "assets", class.Pack.Directory)
		autoLambdas, isProject, ok := mod.ClassInfo("main_fixture.gox")
		require.True(t, ok)
		assert.True(t, isProject)
		assert.Equal(t, map[string]int{"onStart": 0}, autoLambdas)
		assert.Contains(t, slices.Collect(mod.ClassProjects()), class)
	})

	t.Run("EffectiveRegistrations", func(t *testing.T) {
		for _, tt := range []struct {
			name     string
			workExt  string
			want     []string
			wantWork string
		}{
			{name: "ProjectAndWork", workExt: ".work", want: []string{"New"}, wantWork: "New"},
			{name: "ProjectOnly", workExt: ".otherwork", want: []string{"Old", "New"}, wantWork: "Old"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				mod := newTestModule(t, modload.Module{Opt: &modfile.File{Projects: []*modfile.Project{
					{Ext: ".project", Class: "Old", Works: []*modfile.Class{{Ext: ".work", Class: "OldWork"}}},
					{Ext: ".project", Class: "New", Works: []*modfile.Class{{Ext: tt.workExt, Class: "NewWork"}}},
				}}})
				var names []string
				for project := range mod.ClassProjects() {
					if project.Ext == ".project" {
						names = append(names, project.Class)
					}
				}
				assert.Equal(t, tt.want, names)
				project, ok := mod.LookupClass(".project")
				require.True(t, ok)
				assert.Equal(t, "New", project.Class)
				work, ok := mod.LookupClass(".work")
				require.True(t, ok)
				assert.Equal(t, tt.wantWork, work.Class)
			})
		}
	})

	t.Run("InvalidClassModule", func(t *testing.T) {
		mod, err := NewModule(modload.Module{
			File: new(gomodfile.File), Opt: &modfile.File{ClassMods: []string{"example.com/missing"}},
		})
		assert.ErrorIs(t, err, xgomod.ErrNotFound)
		assert.Nil(t, mod)
	})
}

func TestProjectSetModule(t *testing.T) {
	config := testframework.NewModule(t).Module
	proj := newFrameworkTestProject(t, map[string]*File{
		"main_fixture.gox":   file("echo 1\n"),
		"Worker_fixture.gox": file("// Count belongs to the worker.\nvar Count int\n"),
	}, FeatAll)
	uncachedSnapshot := proj.Snapshot()
	oldAST, err := proj.ASTFile("Worker_fixture.gox")
	require.NoError(t, err)
	oldTypes, err := proj.TypeInfo()
	require.NoError(t, err)
	oldDoc, err := proj.PkgDoc()
	require.NoError(t, err)
	cachedSnapshot := proj.Snapshot()
	oldModule := proj.Module()

	config.Opt.Projects[0].Works[0].Prefix = "Actor"
	mod := newTestModule(t, config)
	proj.SetModule(mod)
	assert.Same(t, mod, proj.Module())
	newAST, err := proj.ASTFile("Worker_fixture.gox")
	require.NoError(t, err)
	assert.NotSame(t, oldAST, newAST)
	newTypes, err := proj.TypeInfo()
	require.NoError(t, err)
	assert.NotSame(t, oldTypes, newTypes)
	assert.NotNil(t, newTypes.Pkg.Scope().Lookup("ActorWorker"))
	assert.Nil(t, newTypes.Pkg.Scope().Lookup("Worker"))
	newDoc, err := proj.PkgDoc()
	require.NoError(t, err)
	assert.NotSame(t, oldDoc, newDoc)
	assert.Contains(t, newDoc.Types, "ActorWorker")
	assert.NotContains(t, newDoc.Types, "Worker")

	for _, snapshot := range []*Project{uncachedSnapshot, cachedSnapshot} {
		assert.Same(t, oldModule, snapshot.Module())
		info, err := snapshot.TypeInfo()
		require.NoError(t, err)
		assert.NotNil(t, info.Pkg.Scope().Lookup("Worker"))
		assert.Nil(t, info.Pkg.Scope().Lookup("ActorWorker"))
		doc, err := snapshot.PkgDoc()
		require.NoError(t, err)
		assert.Contains(t, doc.Types, "Worker")
		assert.NotContains(t, doc.Types, "ActorWorker")
	}
}
