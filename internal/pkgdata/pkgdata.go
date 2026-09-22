package pkgdata

import (
	"archive/zip"
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"sync"

	"github.com/goplus/xgolsw/pkgdoc"
)

//go:generate sh -c "GOTOOLCHAIN=\"go$(go list -m -f '{{.GoVersion}}')\" go tool pkgdatagen"

// pkgDataZip holds the bundled package archive.
//
//go:embed pkgdata.zip
var pkgDataZip []byte

// Data holds immutable package archives and their documentation cache.
// Returned documentation must not be modified by callers.
type Data struct {
	base        *zip.Reader
	custom      *zip.Reader
	pkgs        []string
	pkgDocCache sync.Map
}

// New creates package data from a complete archive without embedded fallback.
// It retains a private copy of the supplied bytes.
func New(archive []byte) (*Data, error) {
	return newData(bytes.Clone(archive), nil)
}

// NewWithEmbedded creates package data with a private copy of custom and the
// embedded archive as fallback. Empty custom data selects only embedded data.
func NewWithEmbedded(custom []byte) (*Data, error) {
	return newData(pkgDataZip, bytes.Clone(custom))
}

// newData parses owned archives and collects their packages once.
func newData(base, custom []byte) (*Data, error) {
	baseReader, err := zip.NewReader(bytes.NewReader(base), int64(len(base)))
	if err != nil {
		return nil, fmt.Errorf("failed to read base package archive: %w", err)
	}
	data := &Data{base: baseReader, pkgs: listPkgs(baseReader)}
	if len(custom) == 0 {
		return data, nil
	}
	data.custom, err = zip.NewReader(bytes.NewReader(custom), int64(len(custom)))
	if err != nil {
		return nil, fmt.Errorf("failed to read custom package archive: %w", err)
	}
	data.pkgs = append(data.pkgs, listPkgs(data.custom)...)
	slices.Sort(data.pkgs)
	data.pkgs = slices.Compact(data.pkgs)
	return data, nil
}

const (
	pkgExportSuffix = ".pkgexport"
	pkgDocSuffix    = ".pkgdoc"
)

// ListPkgs lists packages with export data across the base and custom archives.
// The returned slice can be modified by the caller.
func (d *Data) ListPkgs() ([]string, error) {
	return slices.Clone(d.pkgs), nil
}

// listPkgs lists packages with export data in the archive's entry order.
func listPkgs(zr *zip.Reader) []string {
	pkgs := make([]string, 0, len(zr.File)/2)
	for _, f := range zr.File {
		if pkg, ok := strings.CutSuffix(f.Name, pkgExportSuffix); ok {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs
}

// OpenExport opens a package export file.
func (d *Data) OpenExport(pkgPath string) (io.ReadCloser, error) {
	if d.custom != nil {
		rc, err := openExport(d.custom, pkgPath)
		if err == nil {
			return rc, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("failed to open custom package export file: %w", err)
		}
	}
	return openExport(d.base, pkgPath)
}

// openExport opens a package export file from the parsed archive.
func openExport(zr *zip.Reader, pkgPath string) (io.ReadCloser, error) {
	pkgExportFile := pkgPath + pkgExportSuffix
	for _, f := range zr.File {
		if f.Name == pkgExportFile {
			return f.Open()
		}
	}
	return nil, fmt.Errorf("failed to find export file for package %q: %w", pkgPath, fs.ErrNotExist)
}

// GetPkgDoc gets the documentation for a package.
func (d *Data) GetPkgDoc(pkgPath string) (pkgDoc *pkgdoc.PkgDoc, err error) {
	if pkgDocIface, ok := d.pkgDocCache.Load(pkgPath); ok {
		return pkgDocIface.(*pkgdoc.PkgDoc), nil
	}
	defer func() {
		if err == nil {
			d.pkgDocCache.Store(pkgPath, pkgDoc)
		}
	}()

	if d.custom != nil {
		pkgDoc, err = getPkgDoc(d.custom, pkgPath)
		if err == nil {
			return pkgDoc, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("failed to get custom package doc: %w", err)
		}
	}
	return getPkgDoc(d.base, pkgPath)
}

// getPkgDoc gets the documentation for a package from the parsed archive.
func getPkgDoc(zr *zip.Reader, pkgPath string) (*pkgdoc.PkgDoc, error) {
	pkgDocFile := pkgPath + pkgDocSuffix
	for _, f := range zr.File {
		if f.Name != pkgDocFile {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open doc file for package %q: %w", pkgPath, err)
		}
		defer rc.Close()

		var pkgDoc pkgdoc.PkgDoc
		if err := json.NewDecoder(rc).Decode(&pkgDoc); err != nil {
			return nil, fmt.Errorf("failed to decode doc for package %q: %w", pkgPath, err)
		}
		return &pkgDoc, nil
	}
	return nil, fmt.Errorf("failed to find doc file for package %q: %w", pkgPath, fs.ErrNotExist)
}
