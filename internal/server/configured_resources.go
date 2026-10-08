package server

import (
	"encoding/json"
	"fmt"
	gotypes "go/types"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/goplus/xgolsw/internal/config"
	"github.com/goplus/xgolsw/xgo"
)

// configuredResourceID identifies a named resource within a configured collection.
type configuredResourceID struct {
	context XGoResourceContextURI
	name    string
}

// Name returns the resource's source spelling.
func (id configuredResourceID) Name() string { return id.name }

// URI returns the resource URI with its name encoded as one path segment.
func (id configuredResourceID) URI() XGoResourceURI {
	return XGoResourceURI(string(id.context) + "/" + url.PathEscape(id.name))
}

// ContextURI returns the resource collection URI.
func (id configuredResourceID) ContextURI() XGoResourceContextURI { return id.context }

// resourceCollection keeps errors local to the affected collection. A reserved
// collection belongs to an adapter and is never claimed by instance configuration.
type resourceCollection struct {
	names    map[string]bool
	err      error
	reserved bool
}

// configuredResources contains declaration bindings and resource data for one snapshot.
type configuredResources struct {
	*resourceAnalysis
	types       map[gotypes.Type]XGoResourceContextURI
	collections map[XGoResourceContextURI]*resourceCollection
}

// newConfiguredResources binds declarations and loads independently available
// collections from the same snapshot as source analysis.
func newConfiguredResources(proj *xgo.Project, config *config.ResourceConfig, reserved []*resourceProvider) *configuredResources {
	info, _ := proj.TypeInfo()
	if info == nil {
		return nil
	}
	r := &configuredResources{
		resourceAnalysis: new(resourceAnalysis),
		types:            make(map[gotypes.Type]XGoResourceContextURI),
		collections:      make(map[XGoResourceContextURI]*resourceCollection),
	}
	active := make(map[XGoResourceContextURI]bool)
	for binding := range config.Types() {
		context := XGoResourceContextURI(binding.ContextURI)
		collection := r.collections[context]
		if collection == nil {
			collection = new(resourceCollection)
			r.collections[context] = collection
			for _, provider := range reserved {
				if _, recognized, _ := provider.parseURI(XGoResourceURI(binding.ContextURI + "/resource")); recognized {
					collection.reserved = true
					collection.err = fmt.Errorf("resource collection %q is already handled by a framework adapter", context)
					r.addConfigDiagnostic(config.DataFile(), collection.err)
					break
				}
			}
		}
		if collection.reserved {
			continue
		}
		typ, err := resolveResourceType(proj, binding)
		if err == nil {
			for _, provider := range reserved {
				if provider.matchesType(typ) {
					err = fmt.Errorf("resource type %s.%s is already handled by a framework adapter", binding.PkgPath, binding.TypeName)
					break
				}
			}
		}
		if err != nil {
			r.addConfigDiagnostic(config.DataFile(), err)
			if !active[context] {
				collection.err = err
			}
			continue
		}
		r.types[resourceTypeIdentity(typ)] = context
		active[context] = true
		collection.err = nil
	}
	r.readManifest(proj, config.DataFile())
	r.contains = func(id resourceID) bool {
		resource, ok := id.(configuredResourceID)
		if !ok {
			return false
		}
		collection := r.collections[resource.context]
		return collection != nil && !collection.reserved && collection.err == nil && collection.names[resource.name]
	}
	return r
}

// resolveResourceType resolves a string declaration without erasing alias identity.
func resolveResourceType(proj *xgo.Project, binding config.ResourceType) (gotypes.Type, error) {
	info, _ := proj.TypeInfo()
	pkg := info.Pkg
	if binding.PkgPath != pkg.Path() {
		var err error
		pkg, err = proj.Import(binding.PkgPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load resource package %q: %w", binding.PkgPath, err)
		}
	}
	obj, ok := pkg.Scope().Lookup(binding.TypeName).(*gotypes.TypeName)
	if !ok {
		return nil, fmt.Errorf("resource type %s.%s not found", binding.PkgPath, binding.TypeName)
	}
	if !gotypes.Identical(obj.Type().Underlying(), gotypes.Typ[gotypes.String]) {
		return nil, fmt.Errorf("resource type %s.%s must have underlying type string", binding.PkgPath, binding.TypeName)
	}
	switch obj.Type().(type) {
	case *gotypes.Named, *gotypes.Alias:
	default:
		return nil, fmt.Errorf("resource type %s.%s must retain its declaration identity", binding.PkgPath, binding.TypeName)
	}
	return obj.Type(), nil
}

// resourceTypeIdentity matches instantiations to their declaring type or alias.
func resourceTypeIdentity(typ gotypes.Type) gotypes.Type {
	switch typ := typ.(type) {
	case *gotypes.Named:
		return typ.Origin()
	case *gotypes.Alias:
		return typ.Origin()
	default:
		return typ
	}
}

// addConfigDiagnostic reports configuration and inventory errors at the manifest.
func (r *configuredResources) addConfigDiagnostic(filename string, err error) {
	r.diagnostics = append(r.diagnostics, sourceDiagnostic{filename, Diagnostic{
		Severity: SeverityError,
		Message:  fmt.Sprintf("failed to load resources: %v", err),
	}})
}

// inspectReference diagnoses invalid names and missing resources only when the
// corresponding collection is available.
func (r *configuredResources) inspectReference(proj *xgo.Project, ref resourceRef) {
	if !validResourceName(ref.ID.Name()) {
		addResourceDiagnostic(proj, r.resourceAnalysis, ref.Node, "invalid resource name")
		return
	}
	r.addResourceRef(ref)
	if collection := r.collections[ref.ID.ContextURI()]; collection.err == nil && !r.contains(ref.ID) {
		addResourceDiagnostic(proj, r.resourceAnalysis, ref.Node, fmt.Sprintf("resource %q not found in %q", ref.ID.Name(), ref.ID.ContextURI()))
	}
}

// readManifest loads collections independently. A malformed JSON document makes
// all inventory unavailable, while an invalid collection does not affect others.
func (r *configuredResources) readManifest(proj *xgo.Project, filename string) {
	var manifest map[XGoResourceContextURI]json.RawMessage
	var err error
	if file, ok := proj.File(filename); ok {
		err = json.Unmarshal(file.Content, &manifest)
		if err == nil && manifest == nil {
			err = fmt.Errorf("resource manifest must be an object")
		}
	} else {
		err = fmt.Errorf("resource manifest %q not found", filename)
	}
	if err != nil {
		r.addConfigDiagnostic(filename, err)
		for _, collection := range r.collections {
			collection.err = err
		}
		return
	}
	for _, context := range slices.Sorted(maps.Keys(manifest)) {
		collection := r.collections[context]
		if collection == nil {
			r.addConfigDiagnostic(filename, fmt.Errorf("unknown resource collection %q", context))
			continue
		}
		if collection.reserved {
			continue
		}
		names, err := parseResourceNames(manifest[context])
		if err != nil {
			collection.err = fmt.Errorf("invalid resource collection %q: %w", context, err)
			r.addConfigDiagnostic(filename, collection.err)
			continue
		}
		collection.names = names
	}
}

// parseResourceNames accepts an array of nonempty names without publishing a
// partially valid inventory for that collection.
func parseResourceNames(data []byte) (map[string]bool, error) {
	var names []string
	if err := json.Unmarshal(data, &names); err != nil {
		return nil, err
	}
	if names == nil {
		return nil, fmt.Errorf("resource names must be an array")
	}
	result := make(map[string]bool, len(names))
	for _, name := range names {
		if !validResourceName(name) {
			return nil, fmt.Errorf("invalid resource name %q", name)
		}
		result[name] = true
	}
	return result, nil
}

// contextForType gives explicit aliases precedence over their RHS declarations.
// Bare string is never matched by a binding to an alias of string.
func (r *configuredResources) contextForType(typ gotypes.Type) XGoResourceContextURI {
	for typ != nil {
		if context := r.types[resourceTypeIdentity(typ)]; context != "" {
			return context
		}
		alias, ok := typ.(*gotypes.Alias)
		if !ok {
			break
		}
		typ = alias.Rhs()
	}
	return ""
}

// collectCompletions offers names from the resolved literal context or expected types.
func (r *configuredResources) collectCompletions(ctx *completionContext) {
	contexts := make(map[XGoResourceContextURI]bool)
	if literal, ok := r.expressions[ctx.stringLit]; ok {
		if literal.id == nil {
			return
		}
		contexts[literal.id.ContextURI()] = true
	} else {
		for _, typ := range ctx.expectedTypes {
			if context := r.contextForType(typ); context != "" {
				contexts[context] = true
			}
		}
	}
	var ids []resourceID
	for _, context := range slices.Sorted(maps.Keys(contexts)) {
		collection := r.collections[context]
		if collection.err != nil {
			continue
		}
		for _, name := range slices.Sorted(maps.Keys(collection.names)) {
			ids = append(ids, configuredResourceID{context, name})
		}
	}
	ctx.collectResourceNames(ids)
}

// parseURI recognizes a configured collection and decodes one resource name.
func (r *configuredResources) parseURI(uri XGoResourceURI) (resourceID, bool, error) {
	for context, collection := range r.collections {
		if collection.reserved {
			continue
		}
		encoded, ok := strings.CutPrefix(string(uri), string(context)+"/")
		if !ok || strings.Contains(encoded, "/") {
			continue
		}
		name, err := url.PathUnescape(encoded)
		if err != nil || !validResourceName(name) || strings.ContainsAny(encoded, "?#") {
			return nil, true, fmt.Errorf("invalid resource URI %q", uri)
		}
		return configuredResourceID{context, name}, true, nil
	}
	return nil, false, nil
}

// validateRename checks inventory availability and collisions within a collection.
func (r *configuredResources) validateRename(id resourceID, newName string) error {
	collection := r.collections[id.ContextURI()]
	if collection.err != nil {
		return fmt.Errorf("failed to load resources: %w", collection.err)
	}
	if id.Name() != newName && collection.names[newName] {
		return fmt.Errorf("resource rename target %q already exists in %q", newName, id.ContextURI())
	}
	return nil
}

// resourceProvider exposes configured rules through the shared resource pipeline.
func (r *configuredResources) resourceProvider() *resourceProvider {
	return &resourceProvider{
		analysis:    r.resourceAnalysis,
		matchesType: func(typ gotypes.Type) bool { return r.contextForType(typ) != "" },
		resolve: func(_ *xgo.Project, value resourceValue) (resourceID, bool) {
			if context := r.contextForType(value.Type); context != "" {
				return configuredResourceID{context, value.Name}, true
			}
			return nil, false
		},
		inspect:            r.inspectReference,
		parseURI:           r.parseURI,
		validateRename:     r.validateRename,
		collectCompletions: r.collectCompletions,
	}
}
