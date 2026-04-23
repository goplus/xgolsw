/*
 * Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/types"
	"io/fs"
	"maps"
	"path"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/goplus/mod/modfile"
	dqlmaps "github.com/goplus/xgo/dql/maps"
	"github.com/goplus/xgo/tool"
	"github.com/goplus/xgolsw/internal/pkgdata"
)

// ClassfileResourceAPIScopeBinding is one standardized resource-api-scope-binding.
type ClassfileResourceAPIScopeBinding struct {
	TargetParam    int
	SourceReceiver bool
	SourceParam    int
}

// ClassfileResourceKind is one standardized classfile resource kind.
type ClassfileResourceKind struct {
	Name               string
	CanonicalType      types.Type
	HandleTypes        []types.Type
	DiscoveryQuery     string
	NameDiscoveryQuery string
}

// ParentName reports the direct parent kind name. It returns an empty string
// if the kind is top-level.
func (k *ClassfileResourceKind) ParentName() string {
	if k == nil {
		return ""
	}
	idx := strings.LastIndexByte(k.Name, '.')
	if idx < 0 {
		return ""
	}
	return k.Name[:idx]
}

// ClassfileResourceSchema is the runtime view of one serialized classfile
// resource schema.
type ClassfileResourceSchema struct {
	Package *types.Package
	Kinds   []*ClassfileResourceKind

	byKind           map[string]*ClassfileResourceKind
	canonicalTypes   map[types.Type]*ClassfileResourceKind
	handleTypes      map[*types.TypeName]*ClassfileResourceKind
	apiScopeBindings map[*types.Func][]ClassfileResourceAPIScopeBinding
}

// Kind reports the standardized resource kind with the given name.
func (s *ClassfileResourceSchema) Kind(name string) (*ClassfileResourceKind, bool) {
	ret, ok := s.byKind[name]
	return ret, ok
}

// CanonicalKindOfType reports the canonical resource kind determined by typ by
// following alias declarations only.
func (s *ClassfileResourceSchema) CanonicalKindOfType(typ types.Type) (*ClassfileResourceKind, bool) {
	seen := make(map[types.Type]struct{})
	for typ != nil {
		if _, ok := seen[typ]; ok {
			return nil, false
		}
		seen[typ] = struct{}{}

		if kind, ok := s.canonicalTypes[typ]; ok {
			return kind, true
		}

		alias, ok := typ.(*types.Alias)
		if !ok {
			return nil, false
		}
		rhs := alias.Rhs()
		if rhs == nil || rhs == typ {
			return nil, false
		}
		typ = rhs
	}
	return nil, false
}

// HandleKindOfType reports the handle-bearing resource kind determined by typ.
func (s *ClassfileResourceSchema) HandleKindOfType(typ types.Type) (*ClassfileResourceKind, bool) {
	for {
		ptr, ok := typ.(*types.Pointer)
		if !ok {
			break
		}
		typ = ptr.Elem()
	}
	named, ok := typ.(*types.Named)
	if !ok {
		return nil, false
	}
	ret, ok := s.handleTypes[named.Obj()]
	return ret, ok
}

// APIScopeBindings reports the standardized API-position scope bindings
// declared on fn.
func (s *ClassfileResourceSchema) APIScopeBindings(fn *types.Func) []ClassfileResourceAPIScopeBinding {
	ret := s.apiScopeBindings[fn]
	if len(ret) == 0 {
		return nil
	}
	return slices.Clone(ret)
}

// ClassfileResource is one standardized resource instance in one classfile
// project.
type ClassfileResource struct {
	Kind        *ClassfileResourceKind
	Name        string
	Parent      *ClassfileResource
	Value       any
	OriginNodes []dqlmaps.Node
	ImpliedBy   []string

	key string
}

// Decode decodes the representative resource payload into dst.
func (r *ClassfileResource) Decode(dst any) error {
	if r == nil || r.Value == nil {
		return nil
	}
	data, err := json.Marshal(r.Value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// ClassfileResourceSet is the standardized resource set of one classfile
// framework registration in one project.
type ClassfileResourceSet struct {
	Project *modfile.Project
	Schema  *ClassfileResourceSchema

	byKind   map[string][]*ClassfileResource
	byKey    map[string]*ClassfileResource
	children map[*ClassfileResource]map[string][]*ClassfileResource
}

// Resources reports all resources of the given kind.
func (s *ClassfileResourceSet) Resources(kind string) []*ClassfileResource {
	if s == nil {
		return nil
	}
	return slices.Clone(s.byKind[kind])
}

// Children reports all child resources of kind under parent.
func (s *ClassfileResourceSet) Children(parent *ClassfileResource, kind string) []*ClassfileResource {
	if s == nil || parent == nil {
		return nil
	}
	return slices.Clone(s.children[parent][kind])
}

// Resource reports the resource of kind, parent, and name.
func (s *ClassfileResourceSet) Resource(kind string, parent *ClassfileResource, name string) (*ClassfileResource, bool) {
	if s == nil {
		return nil, false
	}
	ret, ok := s.byKey[classfileResourceKey(kind, parent, name)]
	return ret, ok
}

// ClassfileResources contains all cached classfile resource sets discovered in
// one project.
type ClassfileResources struct {
	byExt map[string]*ClassfileResourceSet
}

// SetForExt reports the standardized classfile resource set for ext.
func (r *ClassfileResources) SetForExt(ext string) (*ClassfileResourceSet, bool) {
	if r == nil {
		return nil, false
	}
	ret, ok := r.byExt[ext]
	return ret, ok
}

// Exts reports all classfile extensions with resource sets in this project.
func (r *ClassfileResources) Exts() []string {
	if r == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(r.byExt))
}

// classfileResourceSchemaCacheKey identifies one cached framework schema.
type classfileResourceSchemaCacheKey struct {
	importer string
	pkgPath  string
}

var classfileResourceSchemaCache sync.Map // map[classfileResourceSchemaCacheKey]func() (*ClassfileResourceSchema, error)

// LoadClassfileResourceSchema loads and resolves the standardized classfile
// resource schema for one framework project.
func LoadClassfileResourceSchema(project *modfile.Project, importer types.Importer) (*ClassfileResourceSchema, error) {
	if project == nil {
		return nil, fmt.Errorf("classfile project is nil")
	}
	if importer == nil {
		return nil, fmt.Errorf("classfile resource schema importer is nil")
	}
	if len(project.PkgPaths) == 0 {
		return nil, fmt.Errorf("classfile project has no framework package path")
	}

	key := classfileResourceSchemaCacheKey{
		importer: importerCacheKey(importer),
		pkgPath:  project.PkgPaths[0],
	}
	loadIface, _ := classfileResourceSchemaCache.LoadOrStore(key, sync.OnceValues(func() (*ClassfileResourceSchema, error) {
		return loadClassfileResourceSchema(project, importer)
	}))
	return loadIface.(func() (*ClassfileResourceSchema, error))()
}

// loadClassfileResourceSchema resolves one framework schema from serialized
// pkgdata metadata and importer-backed type information.
func loadClassfileResourceSchema(project *modfile.Project, importer types.Importer) (*ClassfileResourceSchema, error) {
	pkgPath := project.PkgPaths[0]
	pkgSchema, err := pkgdata.GetPkgResourceSchema(pkgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load package resource schema for %q: %w", pkgPath, err)
	}

	pkg, err := importer.Import(pkgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to import framework package %q: %w", pkgPath, err)
	}

	schema := &ClassfileResourceSchema{
		Package:          pkg,
		Kinds:            make([]*ClassfileResourceKind, 0, len(pkgSchema.Kinds)),
		byKind:           make(map[string]*ClassfileResourceKind, len(pkgSchema.Kinds)),
		canonicalTypes:   make(map[types.Type]*ClassfileResourceKind, len(pkgSchema.Kinds)),
		handleTypes:      make(map[*types.TypeName]*ClassfileResourceKind),
		apiScopeBindings: make(map[*types.Func][]ClassfileResourceAPIScopeBinding),
	}
	for _, rawKind := range pkgSchema.Kinds {
		kind := &ClassfileResourceKind{
			Name:               rawKind.Name,
			DiscoveryQuery:     rawKind.DiscoveryQuery,
			NameDiscoveryQuery: rawKind.NameDiscoveryQuery,
		}
		if rawKind.CanonicalType != "" {
			obj, ok := pkg.Scope().Lookup(rawKind.CanonicalType).(*types.TypeName)
			if !ok {
				return nil, fmt.Errorf("failed to resolve canonical classfile resource type %q in package %q", rawKind.CanonicalType, pkgPath)
			}
			kind.CanonicalType = obj.Type()
			schema.canonicalTypes[obj.Type()] = kind
		}
		kind.HandleTypes = make([]types.Type, 0, len(rawKind.HandleTypes))
		for _, handleType := range rawKind.HandleTypes {
			obj, ok := pkg.Scope().Lookup(handleType).(*types.TypeName)
			if !ok {
				return nil, fmt.Errorf("failed to resolve classfile resource handle type %q in package %q", handleType, pkgPath)
			}
			kind.HandleTypes = append(kind.HandleTypes, obj.Type())
			schema.handleTypes[obj] = kind
		}
		schema.Kinds = append(schema.Kinds, kind)
		schema.byKind[kind.Name] = kind
	}

	callables := buildClassfileResourceCallableIndex(pkg, schema.Kinds)
	for _, rawBinding := range pkgSchema.APIScopeBindings {
		fn, ok := callables[rawBinding.Callable]
		if !ok {
			return nil, fmt.Errorf("failed to resolve classfile resource callable %q in package %q", rawBinding.Callable, pkgPath)
		}
		schema.apiScopeBindings[fn] = append(schema.apiScopeBindings[fn], ClassfileResourceAPIScopeBinding{
			TargetParam:    rawBinding.TargetParam,
			SourceReceiver: rawBinding.SourceReceiver,
			SourceParam:    rawBinding.SourceParam,
		})
	}
	return schema, nil
}

// buildClassfileResourceCallableIndex builds the callable lookup used to bind
// serialized API-scope metadata back to imported functions and methods.
func buildClassfileResourceCallableIndex(pkg *types.Package, kinds []*ClassfileResourceKind) map[string]*types.Func {
	callables := make(map[string]*types.Func)
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		fn, ok := scope.Lookup(name).(*types.Func)
		if !ok {
			continue
		}
		callables[fn.FullName()] = fn
	}

	seenHandles := make(map[*types.TypeName]struct{})
	var handles []*types.TypeName
	for _, kind := range kinds {
		for _, handleType := range kind.HandleTypes {
			named, ok := handleType.(*types.Named)
			if !ok {
				continue
			}
			obj := named.Obj()
			if _, ok := seenHandles[obj]; ok {
				continue
			}
			seenHandles[obj] = struct{}{}
			handles = append(handles, obj)
		}
	}
	slices.SortFunc(handles, func(a, b *types.TypeName) int {
		return strings.Compare(a.Name(), b.Name())
	})
	for _, obj := range handles {
		named, ok := obj.Type().(*types.Named)
		if !ok {
			continue
		}
		if iface, ok := named.Underlying().(*types.Interface); ok {
			for fn := range iface.ExplicitMethods() {
				callables[classfileResourceCallableKey(fn, obj)] = fn
			}
			continue
		}
		for fn := range named.Methods() {
			callables[classfileResourceCallableKey(fn, obj)] = fn
		}
	}
	return callables
}

// classfileResourceCallableKey reports the serialized callable identity for fn.
func classfileResourceCallableKey(fn *types.Func, owner *types.TypeName) string {
	if owner != nil {
		if named, ok := owner.Type().(*types.Named); ok {
			if _, ok := named.Underlying().(*types.Interface); ok {
				return fmt.Sprintf("(%s.%s).%s", owner.Pkg().Path(), owner.Name(), fn.Name())
			}
		}
	}
	return fn.FullName()
}

// importerCacheKey reports the stable cache key segment for importer.
func importerCacheKey(importer types.Importer) string {
	v := reflect.ValueOf(importer)
	if !v.IsValid() {
		return "<nil>"
	}
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan, reflect.UnsafePointer:
		return fmt.Sprintf("%T:%x", importer, v.Pointer())
	default:
		return fmt.Sprintf("%T:%v", importer, importer)
	}
}

// classfileResourcesCacheKind is the cache kind for project resource sets.
type classfileResourcesCacheKind struct{}

// classfileResourcesCache stores project resource sets.
type classfileResourcesCache struct {
	resources *ClassfileResources
}

// buildClassfileResourcesCache builds all classfile resource sets for proj.
func buildClassfileResourcesCache(proj *Project) (any, error) {
	resources := &ClassfileResources{byExt: make(map[string]*ClassfileResourceSet)}
	if proj.Mod == nil {
		return &classfileResourcesCache{resources: resources}, nil
	}

	projects := make(map[string]*modfile.Project)
	for filePath := range proj.Files() {
		ext := modfile.ClassExt(path.Base(filePath))
		classProj, ok := proj.Mod.LookupClass(ext)
		if !ok {
			continue
		}
		projects[classProj.Ext] = classProj
	}
	for _, ext := range slices.Sorted(maps.Keys(projects)) {
		classProj := projects[ext]
		resourceSet, err := buildClassfileResourceSet(proj, classProj)
		if err != nil {
			return nil, err
		}
		resources.byExt[classProj.Ext] = resourceSet
		for _, work := range classProj.Works {
			resources.byExt[work.Ext] = resourceSet
		}
	}
	return &classfileResourcesCache{resources: resources}, nil
}

// ClassfileResources retrieves the cached classfile resource sets from the
// project.
func (p *Project) ClassfileResources() (*ClassfileResources, error) {
	cacheIface, err := p.Cache(classfileResourcesCacheKind{})
	if err != nil {
		return nil, err
	}
	cache := cacheIface.(*classfileResourcesCache)
	return cache.resources, nil
}

// ClassfileResourceSet retrieves the cached classfile resource set for ext.
func (p *Project) ClassfileResourceSet(ext string) (*ClassfileResourceSet, error) {
	resources, err := p.ClassfileResources()
	if err != nil {
		return nil, err
	}
	ret, ok := resources.SetForExt(ext)
	if !ok {
		return nil, fs.ErrNotExist
	}
	return ret, nil
}

// buildClassfileResourceSet builds the standardized resource set for one
// framework registration in proj.
func buildClassfileResourceSet(proj *Project, classProj *modfile.Project) (*ClassfileResourceSet, error) {
	schema, err := LoadClassfileResourceSchema(classProj, proj.Importer)
	if err != nil {
		return nil, err
	}

	var packDoc map[string]any
	if classProj.Pack != nil {
		packDoc, err = classfilePackDocument(proj, classProj.Pack.Directory, classProj.Pack.IndexFile)
		if err != nil {
			return nil, err
		}
	}

	set := &ClassfileResourceSet{
		Project:  classProj,
		Schema:   schema,
		byKind:   make(map[string][]*ClassfileResource),
		byKey:    make(map[string]*ClassfileResource),
		children: make(map[*ClassfileResource]map[string][]*ClassfileResource),
	}
	if err := addImpliedClassfileResources(set, proj, classProj, schema); err != nil {
		return nil, err
	}
	if packDoc == nil {
		return set, nil
	}

	topLevelKinds := make([]*ClassfileResourceKind, 0)
	scopedKinds := make([]*ClassfileResourceKind, 0)
	for _, kind := range schema.Kinds {
		if kind.ParentName() == "" {
			topLevelKinds = append(topLevelKinds, kind)
		} else {
			scopedKinds = append(scopedKinds, kind)
		}
	}
	slices.SortFunc(topLevelKinds, func(a, b *ClassfileResourceKind) int {
		return strings.Compare(a.Name, b.Name)
	})
	slices.SortFunc(scopedKinds, func(a, b *ClassfileResourceKind) int {
		return strings.Compare(a.Name, b.Name)
	})

	for _, kind := range topLevelKinds {
		if kind.DiscoveryQuery == "" {
			continue
		}
		nodes, err := evalClassfileResourceDQLNodes(kind.DiscoveryQuery, packDoc)
		if err != nil {
			return nil, fmt.Errorf("failed to discover classfile resources of kind %q: %w", kind.Name, err)
		}
		for _, node := range nodes {
			name, err := classfileResourceLocalName(kind, node)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve classfile resource name for kind %q: %w", kind.Name, err)
			}
			origin := node
			set.mergeResource(kind, nil, name, node.Value, &origin, "")
		}
	}
	for _, kind := range scopedKinds {
		parentKindName := kind.ParentName()
		for _, parent := range slices.Clone(set.byKind[parentKindName]) {
			for _, origin := range parent.OriginNodes {
				nodes, err := evalClassfileResourceDQLNodes(kind.DiscoveryQuery, origin)
				if err != nil {
					return nil, fmt.Errorf("failed to discover classfile resources of kind %q under %q: %w", kind.Name, parent.Name, err)
				}
				for _, node := range nodes {
					name, err := classfileResourceLocalName(kind, node)
					if err != nil {
						return nil, fmt.Errorf("failed to resolve classfile resource name for kind %q: %w", kind.Name, err)
					}
					origin := node
					set.mergeResource(kind, parent, name, node.Value, &origin, "")
				}
			}
		}
	}
	return set, nil
}

// addImpliedClassfileResources adds top-level resources implied by work
// classfiles before pack-document discovery runs.
func addImpliedClassfileResources(set *ClassfileResourceSet, proj *Project, classProj *modfile.Project, schema *ClassfileResourceSchema) error {
	if len(classProj.Works) == 0 {
		return nil
	}

	impliedKinds := make(map[string]*ClassfileResourceKind, len(classProj.Works))
	for _, work := range classProj.Works {
		obj, ok := schema.Package.Scope().Lookup(work.Class).(*types.TypeName)
		if !ok {
			return fmt.Errorf("failed to resolve work class %q in framework package %q", work.Class, schema.Package.Path())
		}
		kind, ok := schema.HandleKindOfType(obj.Type())
		if !ok {
			continue
		}
		if kind.ParentName() != "" {
			return fmt.Errorf("work class %q implies non-top-level classfile resource kind %q", work.Class, kind.Name)
		}
		if prev, ok := impliedKinds[work.Ext]; ok && prev != kind {
			return fmt.Errorf("work extension %q implies multiple classfile resource kinds (%q and %q)", work.Ext, prev.Name, kind.Name)
		}
		impliedKinds[work.Ext] = kind
	}

	for filePath := range proj.Files() {
		base := path.Base(filePath)
		ext := modfile.ClassExt(base)
		if !strings.HasSuffix(base, ext) {
			continue
		}
		kind, ok := impliedKinds[ext]
		if !ok {
			continue
		}
		if classProj.IsProj(ext, base) {
			continue
		}
		name := strings.TrimSuffix(base, ext)
		if name == "" {
			continue
		}
		set.mergeResource(kind, nil, name, nil, nil, filePath)
	}
	return nil
}

// mergeResource merges one discovered or implied resource into the set by
// logical resource identity.
func (s *ClassfileResourceSet) mergeResource(
	kind *ClassfileResourceKind,
	parent *ClassfileResource,
	name string,
	value any,
	origin *dqlmaps.Node,
	impliedBy string,
) *ClassfileResource {
	key := classfileResourceKey(kind.Name, parent, name)
	ret, ok := s.byKey[key]
	if !ok {
		ret = &ClassfileResource{
			Kind: kind,
			Name: name,
			key:  key,
		}
		if parent != nil {
			ret.Parent = parent
		}
		s.byKey[key] = ret
		s.byKind[kind.Name] = append(s.byKind[kind.Name], ret)
		if parent != nil {
			if s.children[parent] == nil {
				s.children[parent] = make(map[string][]*ClassfileResource)
			}
			s.children[parent][kind.Name] = append(s.children[parent][kind.Name], ret)
		}
	}
	if ret.Value == nil && value != nil {
		ret.Value = value
	}
	if origin != nil {
		ret.OriginNodes = append(ret.OriginNodes, *origin)
	}
	if impliedBy != "" {
		ret.ImpliedBy = append(ret.ImpliedBy, impliedBy)
	}
	return ret
}

// classfileResourceKey reports the stable identity key for one resource.
func classfileResourceKey(kindName string, parent *ClassfileResource, name string) string {
	if parent == nil {
		return kindName + "\x00" + name
	}
	return kindName + "\x00" + parent.key + "\x00" + name
}

// classfilePackDocument builds the merged pack document for one classfile
// project view.
func classfilePackDocument(proj *Project, dir string, indexFile string) (map[string]any, error) {
	fsys := classfileProjectReadDirFS{proj: proj}
	packed, err := tool.PackProject(fsys, dir, indexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to pack classfile project: %w", err)
	}
	var packDoc map[string]any
	if err := json.Unmarshal(packed, &packDoc); err != nil {
		return nil, fmt.Errorf("failed to decode packed classfile document: %w", err)
	}
	if err := restoreEmptyClassfilePackObjects(fsys, dir, indexFile, packDoc); err != nil {
		return nil, fmt.Errorf("failed to restore empty classfile pack objects: %w", err)
	}
	return packDoc, nil
}

// restoreEmptyClassfilePackObjects restores empty directory objects that the
// current packer omits from the packed JSON document.
func restoreEmptyClassfilePackObjects(fsys fs.ReadDirFS, current string, indexFile string, obj map[string]any) error {
	entries, err := fsys.ReadDir(current)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		childDir := joinClassfileProjectFSPath(current, name)
		childObj := make(map[string]any)
		if existing, ok := obj[name]; ok {
			var isMap bool
			childObj, isMap = existing.(map[string]any)
			if !isMap {
				return fmt.Errorf("collision: key %q already exists at path %q", name, childDir)
			}
		}
		if err := restoreEmptyClassfilePackObjects(fsys, childDir, indexFile, childObj); err != nil {
			return err
		}
		hasIndex, err := hasClassfilePackConfig(fsys, childDir, indexFile)
		if err != nil {
			return err
		}
		if hasIndex || len(childObj) > 0 {
			obj[name] = childObj
		}
	}
	return nil
}

// hasClassfilePackConfig reports whether dir contains the framework pack index.
func hasClassfilePackConfig(fsys fs.ReadDirFS, dir string, indexFile string) (bool, error) {
	_, err := fs.ReadFile(fsys, joinClassfileProjectFSPath(dir, indexFile))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// joinClassfileProjectFSPath joins one classfile project directory path.
func joinClassfileProjectFSPath(dir string, name string) string {
	if dir == "." {
		return name
	}
	return dir + "/" + name
}

// evalClassfileResourceDQLNodes evaluates one discovery query and materializes
// its matched nodes.
func evalClassfileResourceDQLNodes(query string, src any) ([]dqlmaps.Node, error) {
	nodes, err := dqlmaps.Eval(normalizeClassfileResourceDQLQuery(query), src)
	if err != nil {
		return nil, err
	}
	var ret []dqlmaps.Node
	nodes.Data(func(node dqlmaps.Node) bool {
		ret = append(ret, node)
		return true
	})
	return ret, nil
}

// normalizeClassfileResourceDQLQuery rewrites schema queries into dql/maps
// field access syntax.
func normalizeClassfileResourceDQLQuery(query string) string {
	return strings.ReplaceAll(query, "$type", `$"type"`)
}

// classfileResourceLocalName resolves the local resource name from one
// discovery origin node.
func classfileResourceLocalName(kind *ClassfileResourceKind, node dqlmaps.Node) (string, error) {
	if kind.NameDiscoveryQuery != "" {
		nameNodes, err := dqlmaps.Eval(normalizeClassfileResourceDQLQuery(kind.NameDiscoveryQuery), node)
		if err != nil {
			return "", err
		}
		nameValue, err := nameNodes.XGo_value__1()
		if err != nil {
			return "", err
		}
		name, ok := nameValue.(string)
		if !ok || name == "" {
			return "", fmt.Errorf("name discovery for %q did not yield one non-empty string", kind.Name)
		}
		return name, nil
	}
	if node.Name != "" {
		return node.Name, nil
	}
	if obj, ok := node.Value.(map[string]any); ok {
		if name, ok := obj["name"].(string); ok && name != "" {
			return name, nil
		}
	}
	return "", fmt.Errorf("classfile resource kind %q has no local name", kind.Name)
}

// classfileProjectReadDirFS exposes project files as an [fs.ReadDirFS].
type classfileProjectReadDirFS struct {
	proj *Project
}

// Open implements [fs.FS].
func (fsys classfileProjectReadDirFS) Open(name string) (fs.File, error) {
	name = normalizeClassfileProjectFSPath(name)
	file, ok := fsys.proj.File(name)
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &classfileProjectFSFile{
		name: name,
		rd:   bytes.NewReader(file.Content),
	}, nil
}

// ReadDir implements [fs.ReadDirFS].
func (fsys classfileProjectReadDirFS) ReadDir(name string) ([]fs.DirEntry, error) {
	name = normalizeClassfileProjectFSPath(name)
	prefix := ""
	if name != "." {
		prefix = name + "/"
	}

	dirs := make(map[string]struct{})
	files := make(map[string]int64)
	for filePath, file := range fsys.proj.Files() {
		if !strings.HasPrefix(filePath, prefix) {
			continue
		}
		rest := strings.TrimPrefix(filePath, prefix)
		part, tail, ok := strings.Cut(rest, "/")
		if !ok {
			files[part] = int64(len(file.Content))
			continue
		}
		if part != "" && tail != "" {
			dirs[part] = struct{}{}
		}
	}

	if name != "." && len(dirs) == 0 && len(files) == 0 {
		return nil, fs.ErrNotExist
	}

	entries := make([]fs.DirEntry, 0, len(dirs)+len(files))
	for _, dirName := range slices.Sorted(maps.Keys(dirs)) {
		entries = append(entries, classfileProjectDirEntry{name: dirName, dir: true})
	}
	for _, fileName := range slices.Sorted(maps.Keys(files)) {
		entries = append(entries, classfileProjectDirEntry{name: fileName, size: files[fileName]})
	}
	return entries, nil
}

// normalizeClassfileProjectFSPath normalizes one virtual project file path.
func normalizeClassfileProjectFSPath(name string) string {
	if name == "" || name == "." {
		return "."
	}
	return strings.TrimPrefix(path.Clean(name), "./")
}

// classfileProjectFSFile is one virtual file opened from project content.
type classfileProjectFSFile struct {
	name string
	rd   *bytes.Reader
}

// Stat implements [fs.File].
func (f *classfileProjectFSFile) Stat() (fs.FileInfo, error) {
	return classfileProjectFileInfo{name: path.Base(f.name), size: f.rd.Size()}, nil
}

// Read implements [fs.File].
func (f *classfileProjectFSFile) Read(p []byte) (int, error) {
	return f.rd.Read(p)
}

// Close implements [fs.File].
func (f *classfileProjectFSFile) Close() error {
	return nil
}

// classfileProjectFileInfo describes one virtual project file.
type classfileProjectFileInfo struct {
	name string
	size int64
}

// Name implements [fs.FileInfo].
func (i classfileProjectFileInfo) Name() string {
	return i.name
}

// Size implements [fs.FileInfo].
func (i classfileProjectFileInfo) Size() int64 {
	return i.size
}

// Mode implements [fs.FileInfo].
func (i classfileProjectFileInfo) Mode() fs.FileMode {
	return 0
}

// ModTime implements [fs.FileInfo].
func (i classfileProjectFileInfo) ModTime() time.Time {
	return time.Time{}
}

// IsDir implements [fs.FileInfo].
func (i classfileProjectFileInfo) IsDir() bool {
	return false
}

// Sys implements [fs.FileInfo].
func (i classfileProjectFileInfo) Sys() any {
	return nil
}

// classfileProjectDirEntry is one virtual directory entry in project content.
type classfileProjectDirEntry struct {
	name string
	dir  bool
	size int64
}

// Name implements [fs.DirEntry].
func (e classfileProjectDirEntry) Name() string {
	return e.name
}

// IsDir implements [fs.DirEntry].
func (e classfileProjectDirEntry) IsDir() bool {
	return e.dir
}

// Type implements [fs.DirEntry].
func (e classfileProjectDirEntry) Type() fs.FileMode {
	if e.dir {
		return fs.ModeDir
	}
	return 0
}

// Info implements [fs.DirEntry].
func (e classfileProjectDirEntry) Info() (fs.FileInfo, error) {
	return classfileProjectDirInfo{name: e.name, dir: e.dir, size: e.size}, nil
}

// classfileProjectDirInfo describes one virtual directory entry.
type classfileProjectDirInfo struct {
	name string
	dir  bool
	size int64
}

// Name implements [fs.FileInfo].
func (i classfileProjectDirInfo) Name() string {
	return i.name
}

// Size implements [fs.FileInfo].
func (i classfileProjectDirInfo) Size() int64 {
	return i.size
}

// Mode implements [fs.FileInfo].
func (i classfileProjectDirInfo) Mode() fs.FileMode {
	if i.dir {
		return fs.ModeDir
	}
	return 0
}

// ModTime implements [fs.FileInfo].
func (i classfileProjectDirInfo) ModTime() time.Time {
	return time.Time{}
}

// IsDir implements [fs.FileInfo].
func (i classfileProjectDirInfo) IsDir() bool {
	return i.dir
}

// Sys implements [fs.FileInfo].
func (i classfileProjectDirInfo) Sys() any {
	return nil
}
