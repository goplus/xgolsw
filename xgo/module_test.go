/*
 * Copyright (c) 2025 The XGo Authors (xgo.dev). All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

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

func TestDefaultModule(t *testing.T) {
	project := NewProject(nil, nil, 0)
	assert.True(t, project.Module().IsClass("_test.gox"))
	assert.True(t, project.Module().IsClass(".gsh"))
	assert.False(t, project.Module().IsClass(".spx"))
	assert.Empty(t, modload.Default.Opt.Projects)
	_, ok := xgomod.Default.LookupClass(".spx")
	assert.False(t, ok)
}

func TestNewModule(t *testing.T) {
	t.Run("RegistrationIsolation", func(t *testing.T) {
		config := testframework.NewModule(t).Module
		registration := config.Opt.Projects[0]
		registration.AutoLambdas = map[string]int{"onStart": 0}
		registration.Import = []*modfile.Import{{Name: "fmt", Path: "fmt"}}
		registration.Pack = &modfile.Pack{Directory: "assets", IndexFile: "index.json"}
		mod := newTestModule(t, config)
		framework, ok := mod.LookupClass("_fixture.gox")
		require.True(t, ok)
		assert.NotSame(t, registration, framework)
		for _, builtin := range []*modfile.Project{xgomod.TestProject, xgomod.GshProject} {
			loaded, ok := mod.LookupClass(builtin.Ext)
			require.True(t, ok)
			assert.NotSame(t, builtin, loaded)
		}

		registration.Ext = ".other"
		registration.Works[0].Ext = ".work"
		registration.Works[0].Prefix = "Other"
		registration.PkgPaths[0] = "example.com/other"
		registration.AutoLambdas["onStart"] = 1
		registration.Import[0].Path = "example.com/fmt"
		registration.Pack.Directory = "other"
		config.Opt.Projects = nil
		assert.True(t, mod.IsClass("_fixture.gox"))
		assert.False(t, mod.IsClass(".other"))
		assert.Equal(t, "_fixture.gox", framework.Works[0].Ext)
		assert.Empty(t, framework.Works[0].Prefix)
		assert.Equal(t, []string{testframework.PkgPath}, framework.PkgPaths)
		assert.Equal(t, "fmt", framework.Import[0].Path)
		assert.Equal(t, "assets", framework.Pack.Directory)
		autoLambdas, isProject, ok := mod.ClassInfo("main_fixture.gox")
		require.True(t, ok)
		assert.True(t, isProject)
		assert.Equal(t, map[string]int{"onStart": 0}, autoLambdas)
		assert.Contains(t, slices.Collect(mod.ClassFrameworks()), framework)
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
				for framework := range mod.ClassFrameworks() {
					if framework.Ext == ".project" {
						names = append(names, framework.Class)
					}
				}
				assert.Equal(t, tt.want, names)
				projectFramework, ok := mod.LookupClass(".project")
				require.True(t, ok)
				assert.Equal(t, "New", projectFramework.Class)
				workFramework, ok := mod.LookupClass(".work")
				require.True(t, ok)
				assert.Equal(t, tt.wantWork, workFramework.Class)
			})
		}
	})

	t.Run("InvalidFrameworkModule", func(t *testing.T) {
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
		"main_fixture.gox": file("echo 1\n"),
		"Worker_fixture.gox": file(`// Count belongs to the worker.
var Count int
`),
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
