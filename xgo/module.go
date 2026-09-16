package xgo

import (
	"iter"
	"maps"
	"slices"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/goplus/mod/xgomod"
)

// Module holds a loaded module and its effective classfile registrations. It is
// read-only and can be shared by projects and their snapshots.
type Module struct {
	mod      *xgomod.Module
	projects []*modfile.Project
}

// NewModule loads classfile registrations from config. Later changes to the
// registration configuration or its files do not affect the returned module.
// The Go module file in config must not be modified after this call.
func NewModule(config modload.Module) (*Module, error) {
	opt := *config.Opt
	// Override the compiler's shared builtin registrations with owned copies.
	opt.Projects = []*modfile.Project{cloneClassProject(xgomod.TestProject), cloneClassProject(xgomod.GshProject)}
	for _, project := range config.Opt.Projects {
		opt.Projects = append(opt.Projects, cloneClassProject(project))
	}
	opt.ClassMods = slices.Clone(opt.ClassMods)
	config.Opt = &opt
	mod := xgomod.New(config)
	var registered []*modfile.Project
	if err := mod.ImportClasses(func(project *modfile.Project) {
		registered = append(registered, project)
	}); err != nil {
		return nil, err
	}
	projects := slices.DeleteFunc(registered, func(project *modfile.Project) bool {
		if current, _ := mod.LookupClass(project.Ext); current == project {
			return false
		}
		for _, work := range project.Works {
			if current, _ := mod.LookupClass(work.Ext); current == project {
				return false
			}
		}
		return true
	})
	return &Module{mod: mod, projects: projects}, nil
}

// cloneClassProject copies the mutable configuration of a classfile project.
func cloneClassProject(project *modfile.Project) *modfile.Project {
	clone := *project
	clone.PkgPaths = slices.Clone(project.PkgPaths)
	clone.AutoLambdas = maps.Clone(project.AutoLambdas)
	clone.Works = slices.Clone(project.Works)
	for i, work := range project.Works {
		work := *work
		clone.Works[i] = &work
	}
	clone.Import = slices.Clone(project.Import)
	for i, imp := range project.Import {
		imp := *imp
		clone.Import[i] = &imp
	}
	if project.Pack != nil {
		pack := *project.Pack
		clone.Pack = &pack
	}
	return &clone
}

// ClassProjects returns the effective classfile projects without reading module
// files. The returned registrations must not be modified.
func (m *Module) ClassProjects() iter.Seq[*modfile.Project] {
	return slices.Values(m.projects)
}

// LookupClass returns the registration for ext. The returned registration must
// not be modified.
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
	clear(p.caches)
	clear(p.fileCaches)
}
