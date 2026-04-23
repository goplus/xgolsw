package pkgdata

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sync"
)

// PkgResourceSuffix is the archive suffix used for serialized package resource schemas.
const PkgResourceSuffix = ".pkgresource"

// PkgResourceSchema is the serialized classfile resource schema for one package.
type PkgResourceSchema struct {
	Kinds            []PkgResourceKind            `json:"kinds"`
	APIScopeBindings []PkgResourceAPIScopeBinding `json:"apiScopeBindings,omitempty"`
}

// PkgResourceKind is one serialized classfile resource kind.
type PkgResourceKind struct {
	Name               string   `json:"name"`
	CanonicalType      string   `json:"canonicalType,omitempty"`
	HandleTypes        []string `json:"handleTypes,omitempty"`
	DiscoveryQuery     string   `json:"discoveryQuery,omitempty"`
	NameDiscoveryQuery string   `json:"nameDiscoveryQuery,omitempty"`
}

// PkgResourceAPIScopeBinding is one serialized resource-api-scope-binding.
type PkgResourceAPIScopeBinding struct {
	Callable       string `json:"callable"`
	TargetParam    int    `json:"targetParam"`
	SourceReceiver bool   `json:"sourceReceiver,omitempty"`
	SourceParam    int    `json:"sourceParam,omitempty"`
}

// pkgResourceSchemaCache caches decoded package resource schemas.
var pkgResourceSchemaCache sync.Map // map[string]*PkgResourceSchema

// GetPkgResourceSchema gets the serialized classfile resource schema for one package.
func GetPkgResourceSchema(pkgPath string) (pkgSchema *PkgResourceSchema, err error) {
	if pkgSchemaIface, ok := pkgResourceSchemaCache.Load(pkgPath); ok {
		return pkgSchemaIface.(*PkgResourceSchema), nil
	}
	defer func() {
		if err == nil {
			pkgResourceSchemaCache.Store(pkgPath, pkgSchema)
		}
	}()

	if len(customPkgdataZip) > 0 {
		pkgSchema, err = getPkgResourceSchema(customPkgdataZip, pkgPath)
		if err == nil {
			return pkgSchema, nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("failed to get custom package resource schema: %w", err)
		}
	}
	return getPkgResourceSchema(pkgdataZip, pkgPath)
}

// getPkgResourceSchema gets the serialized classfile resource schema for one
// package from the provided zip data.
func getPkgResourceSchema(zipData []byte, pkgPath string) (*PkgResourceSchema, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("failed to create zip reader: %w", err)
	}
	pkgResourceFile := pkgPath + PkgResourceSuffix
	for _, f := range zr.File {
		if f.Name != pkgResourceFile {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("failed to open resource schema file for package %q: %w", pkgPath, err)
		}
		defer rc.Close()

		var pkgSchema PkgResourceSchema
		if err := json.NewDecoder(rc).Decode(&pkgSchema); err != nil {
			return nil, fmt.Errorf("failed to decode resource schema for package %q: %w", pkgPath, err)
		}
		return &pkgSchema, nil
	}
	return nil, fmt.Errorf("failed to find resource schema file for package %q: %w", pkgPath, fs.ErrNotExist)
}
