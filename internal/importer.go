package internal

import (
	"fmt"
	gotypes "go/types"
	"io"
	"sync"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"golang.org/x/tools/go/gcexportdata"
)

// importer implements [go/types.Importer].
type importer struct {
	mu         sync.Mutex
	fset       *token.FileSet
	loaded     map[string]*gotypes.Package
	openExport func(string) (io.ReadCloser, error)
}

// newImporter creates an importer that reads export data using openExport.
func newImporter(openExport func(string) (io.ReadCloser, error)) *importer {
	return &importer{
		fset:       token.NewFileSet(),
		loaded:     map[string]*gotypes.Package{"unsafe": gotypes.Unsafe},
		openExport: openExport,
	}
}

// Import implements [go/types.Importer].
func (imp *importer) Import(path string) (*gotypes.Package, error) {
	imp.mu.Lock()
	defer imp.mu.Unlock()

	if pkg, ok := imp.loaded[path]; ok && pkg.Complete() {
		return pkg, nil
	}

	export, err := imp.openExport(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open package export file: %w", err)
	}
	defer export.Close()

	pkg, err := gcexportdata.Read(export, imp.fset, imp.loaded, path)
	if err != nil {
		return nil, fmt.Errorf("failed to parse package export data: %w", err)
	}
	return pkg, nil
}

// Importer is the global instance of [importer].
var Importer = newImporter(pkgdata.OpenExport)
