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
	"iter"
	"maps"
	"slices"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/goplus/mod/xgomod"
)

// defaultModule supplies XGo's builtin class frameworks to new projects.
var defaultModule = func() *Module {
	mod, err := NewModule(modload.Module{Opt: &modfile.File{}})
	if err != nil {
		panic(err)
	}
	return mod
}()

// Module holds a loaded module and its effective class framework registrations.
// It is read-only and can be shared by projects and their snapshots.
type Module struct {
	mod        *xgomod.Module
	frameworks []*modfile.Project
}

// NewModule loads class framework registrations from config. Later changes to the
// registration configuration or its files do not affect the returned module.
// The Go module file in config must not be modified after this call.
func NewModule(config modload.Module) (*Module, error) {
	opt := *config.Opt
	// Override the compiler's shared builtin registrations with owned copies.
	opt.Projects = []*modfile.Project{cloneClassFramework(xgomod.TestProject), cloneClassFramework(xgomod.GshProject)}
	for _, framework := range config.Opt.Projects {
		opt.Projects = append(opt.Projects, cloneClassFramework(framework))
	}
	opt.ClassMods = slices.Clone(opt.ClassMods)
	config.Opt = &opt
	mod := xgomod.New(config)
	var registered []*modfile.Project
	if err := mod.ImportClasses(func(framework *modfile.Project) {
		registered = append(registered, framework)
	}); err != nil {
		return nil, err
	}
	frameworks := slices.DeleteFunc(registered, func(framework *modfile.Project) bool {
		if current, _ := mod.LookupClass(framework.Ext); current == framework {
			return false
		}
		for _, work := range framework.Works {
			if current, _ := mod.LookupClass(work.Ext); current == framework {
				return false
			}
		}
		return true
	})
	return &Module{mod: mod, frameworks: frameworks}, nil
}

// cloneClassFramework copies a class framework's mutable registration data.
func cloneClassFramework(framework *modfile.Project) *modfile.Project {
	clone := *framework
	clone.PkgPaths = slices.Clone(framework.PkgPaths)
	clone.AutoLambdas = maps.Clone(framework.AutoLambdas)
	clone.Works = slices.Clone(framework.Works)
	for i, work := range framework.Works {
		work := *work
		clone.Works[i] = &work
	}
	clone.Import = slices.Clone(framework.Import)
	for i, imp := range framework.Import {
		imp := *imp
		clone.Import[i] = &imp
	}
	if framework.Pack != nil {
		pack := *framework.Pack
		clone.Pack = &pack
	}
	return &clone
}

// ClassFrameworks returns the effective class framework registrations without
// reading module files. The returned registrations must not be modified.
func (m *Module) ClassFrameworks() iter.Seq[*modfile.Project] {
	return slices.Values(m.frameworks)
}

// LookupClass returns the class framework registration for ext. The returned
// registration must not be modified.
func (m *Module) LookupClass(ext string) (*modfile.Project, bool) {
	return m.mod.LookupClass(ext)
}

// ClassInfo returns the parser configuration for filename. The returned map
// must not be modified.
func (m *Module) ClassInfo(filename string) (map[string]int, bool, bool) {
	return m.mod.ClassInfo(filename)
}

// IsClass reports whether ext is a registered classfile extension.
func (m *Module) IsClass(ext string) bool {
	return m.mod.IsClass(ext)
}

// Module returns the project's loaded module.
func (p *Project) Module() *Module {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.module
}

// SetModule replaces the project's loaded module and invalidates syntax, type,
// and documentation caches. Existing snapshots retain their original module.
// As with source changes, callers must finish work on this project or use a
// snapshot before changing its configuration.
func (p *Project) SetModule(module *Module) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.module = module
	p.cacheRevision++
	clear(p.caches)
	clear(p.fileCaches)
	p.sharedAST = false
}
