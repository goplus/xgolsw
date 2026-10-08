package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"net/url"
	"slices"
	"strings"

	"github.com/goplus/xgo/token"
	"golang.org/x/mod/module"
)

// ResourceType binds a string type to a resource collection.
type ResourceType struct {
	PkgPath    string `json:"pkgPath"`
	TypeName   string `json:"typeName"`
	ContextURI string `json:"contextURI"`
}

// ResourceConfig holds immutable resource rules for a language server instance.
// Resource names are read from the configured JSON file in each project revision.
type ResourceConfig struct {
	dataFile string
	types    []ResourceType
}

// ParseResourceConfig validates and owns a JSON resource configuration.
func ParseResourceConfig(data []byte) (*ResourceConfig, error) {
	var value *struct {
		DataFile string         `json:"dataFile"`
		Types    []ResourceType `json:"types"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("invalid resource configuration: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("resource configuration must contain one JSON object")
	}
	if value == nil || !fs.ValidPath(value.DataFile) || strings.Contains(value.DataFile, "\\") || !strings.HasSuffix(value.DataFile, ".json") {
		return nil, fmt.Errorf("resource dataFile must be a project-relative JSON file path")
	}
	if len(value.Types) == 0 {
		return nil, fmt.Errorf("resource types must not be empty")
	}
	seen := make(map[[2]string]bool)
	for _, typ := range value.Types {
		if err := module.CheckImportPath(typ.PkgPath); err != nil {
			return nil, fmt.Errorf("invalid resource pkgPath: %w", err)
		}
		if !token.IsIdentifier(typ.TypeName) || typ.TypeName == "_" {
			return nil, fmt.Errorf("invalid resource typeName %q", typ.TypeName)
		}
		uri, err := url.Parse(typ.ContextURI)
		if err != nil || uri.Scheme == "" || uri.Opaque != "" || uri.RawQuery != "" || uri.ForceQuery || uri.Fragment != "" ||
			strings.HasSuffix(typ.ContextURI, "/") || uri.String() != typ.ContextURI {
			return nil, fmt.Errorf("resource contextURI %q must be an absolute hierarchical URI without a query, fragment, or trailing slash", typ.ContextURI)
		}
		key := [2]string{typ.PkgPath, typ.TypeName}
		if seen[key] {
			return nil, fmt.Errorf("duplicate resource type %s.%s", typ.PkgPath, typ.TypeName)
		}
		seen[key] = true
	}
	return &ResourceConfig{dataFile: value.DataFile, types: value.Types}, nil
}

// DataFile returns the project-relative resource manifest path.
func (c *ResourceConfig) DataFile() string { return c.dataFile }

// Types yields the configured type bindings in declaration order.
func (c *ResourceConfig) Types() iter.Seq[ResourceType] {
	return slices.Values(c.types)
}
