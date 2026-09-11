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

var (
	//go:embed pkgdata.zip
	pkgdataZip []byte

	// pkgdataMu synchronizes package data replacement with reads and cache updates.
	pkgdataMu sync.RWMutex

	// customPkgdataZip holds the user-provided package data which has
	// higher priority than the embedded one.
	customPkgdataZip []byte
)

// SetCustomPkgdataZip replaces the custom package data and clears cached documentation.
// It does not refresh types already loaded by importers. Set export data before
// importing the affected packages. The caller must not modify data after this call.
func SetCustomPkgdataZip(data []byte) {
	pkgdataMu.Lock()
	defer pkgdataMu.Unlock()

	customPkgdataZip = data
	pkgDocCache.Clear()
}

const (
	pkgExportSuffix = ".pkgexport"
	pkgDocSuffix    = ".pkgdoc"
)

// ListPkgs lists all packages in the pkgdata.zip file.
func ListPkgs() ([]string, error) {
	pkgdataMu.RLock()
	defer pkgdataMu.RUnlock()

	pkgs, err := listPkgs(pkgdataZip)
	if err != nil {
		return nil, fmt.Errorf("failed to list embed packages: %w", err)
	}
	if len(customPkgdataZip) > 0 {
		customPkgs, err := listPkgs(customPkgdataZip)
		if err != nil {
			return nil, fmt.Errorf("failed to list custom packages: %w", err)
		}
		pkgs = append(pkgs, customPkgs...)
		slices.Sort(pkgs)
		pkgs = slices.Compact(pkgs)
	}
	return pkgs, nil
}

// listPkgs lists all packages in the provided zip data.
func listPkgs(zipData []byte) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create zip reader: %w", err)
	}
	pkgs := make([]string, 0, len(zr.File)/2)
	for _, f := range zr.File {
		if pkg, ok := strings.CutSuffix(f.Name, pkgExportSuffix); ok {
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs, nil
}

// OpenExport opens a package export file.
func OpenExport(pkgPath string) (io.ReadCloser, error) {
	pkgdataMu.RLock()
	defer pkgdataMu.RUnlock()

	if len(customPkgdataZip) > 0 {
		rc, err := openExport(customPkgdataZip, pkgPath)
		if err == nil {
			return rc, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("failed to open custom package export file: %w", err)
		}
	}
	return openExport(pkgdataZip, pkgPath)
}

// openExport opens a package export file from the provided zip data.
func openExport(zipData []byte, pkgPath string) (io.ReadCloser, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create zip reader: %w", err)
	}
	pkgExportFile := pkgPath + pkgExportSuffix
	for _, f := range zr.File {
		if f.Name == pkgExportFile {
			return f.Open()
		}
	}
	return nil, fmt.Errorf("failed to find export file for package %q: %w", pkgPath, fs.ErrNotExist)
}

// pkgDocCache is a cache for package documentation.
var pkgDocCache sync.Map // map[string]*pkgdoc.PkgDoc

// GetPkgDoc gets the documentation for a package.
func GetPkgDoc(pkgPath string) (pkgDoc *pkgdoc.PkgDoc, err error) {
	pkgdataMu.RLock()
	defer pkgdataMu.RUnlock()

	if pkgDocIface, ok := pkgDocCache.Load(pkgPath); ok {
		return pkgDocIface.(*pkgdoc.PkgDoc), nil
	}
	defer func() {
		if err == nil {
			pkgDocCache.Store(pkgPath, pkgDoc)
		}
	}()

	if len(customPkgdataZip) > 0 {
		pkgDoc, err = getPkgDoc(customPkgdataZip, pkgPath)
		if err == nil {
			return pkgDoc, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("failed to get custom package doc: %w", err)
		}
	}
	return getPkgDoc(pkgdataZip, pkgPath)
}

// getPkgDoc gets the documentation for a package from the provided zip data.
func getPkgDoc(zipData []byte, pkgPath string) (pkgDoc *pkgdoc.PkgDoc, err error) {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create zip reader: %w", err)
	}
	pkgDocFile := pkgPath + pkgDocSuffix
	for _, f := range zr.File {
		if f.Name == pkgDocFile {
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
	}
	return nil, fmt.Errorf("failed to find doc file for package %q: %w", pkgPath, fs.ErrNotExist)
}
