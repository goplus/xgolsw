// Package config assembles project configuration for the WebAssembly application.
package config

import (
	"fmt"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/goplus/xgolsw/internal"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/xgo"
)

// Options configures one language server.
type Options struct {
	// ClassFrameworkConfig contains class framework registrations in gox.mod
	// syntax. An empty string selects only XGo's builtin class frameworks.
	ClassFrameworkConfig string
	// PkgData supplies immutable archives for this instance.
	// Nil selects the embedded package data.
	PkgData *pkgdata.Data
}

// NewProject creates a project and its package data from an instance configuration.
// Module registrations, package archives, and imported types are fixed at creation.
func NewProject(files map[string]*xgo.File, options Options) (*xgo.Project, *pkgdata.Data, error) {
	frameworkConfig, err := modfile.Parse("gox.mod", []byte(options.ClassFrameworkConfig), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid class framework configuration: %w", err)
	}
	module, err := xgo.NewModule(modload.Module{Opt: frameworkConfig})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load class framework configuration: %w", err)
	}
	data := options.PkgData
	if data == nil {
		data, err = pkgdata.NewWithEmbedded(nil)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid embedded package data: %w", err)
		}
	}
	project := xgo.NewProject(nil, files, xgo.FeatAll)
	project.PkgPath = "main"
	project.SetModule(module)
	project.Importer = internal.NewImporter(data.OpenExport)
	return project, data, nil
}
