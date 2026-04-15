package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"go/types"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/goplus/mod/modfile"
	xgoast "github.com/goplus/xgo/ast"
	dqlmaps "github.com/goplus/xgo/dql/maps"
	"github.com/goplus/xgo/tool"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// spxClassfileResourceSchema is the runtime view of the serialized spx resource schema.
type spxClassfileResourceSchema struct {
	kinds            map[string]pkgdata.PkgResourceKind
	canonicalTypes   map[types.Type]struct{}
	apiScopeBindings map[string][]pkgdata.PkgResourceAPIScopeBinding
}

// getSpxClassfileResourceSchema loads and resolves the serialized spx resource schema once.
var getSpxClassfileResourceSchema = sync.OnceValues(func() (*spxClassfileResourceSchema, error) {
	pkgSchema, err := pkgdata.GetPkgResourceSchema(SpxPkgPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load spx resource schema: %w", err)
	}

	spxPkg := GetSpxPkg()
	kinds := make(map[string]pkgdata.PkgResourceKind, len(pkgSchema.Kinds))
	canonicalTypes := make(map[types.Type]struct{}, len(pkgSchema.Kinds))
	apiScopeBindings := make(map[string][]pkgdata.PkgResourceAPIScopeBinding)
	for _, kind := range pkgSchema.Kinds {
		kinds[kind.Name] = kind
		if kind.CanonicalType == "" {
			continue
		}
		obj := spxPkg.Scope().Lookup(kind.CanonicalType)
		if obj == nil {
			return nil, fmt.Errorf("failed to resolve canonical spx resource type %q", kind.CanonicalType)
		}
		canonicalTypes[obj.Type()] = struct{}{}
	}
	for _, binding := range pkgSchema.APIScopeBindings {
		apiScopeBindings[binding.Callable] = append(apiScopeBindings[binding.Callable], binding)
	}
	return &spxClassfileResourceSchema{
		kinds:            kinds,
		canonicalTypes:   canonicalTypes,
		apiScopeBindings: apiScopeBindings,
	}, nil
})

// canonicalType resolves one canonical spx resource name type through alias chains.
func (s *spxClassfileResourceSchema) canonicalType(typ types.Type) types.Type {
	seen := make(map[types.Type]struct{})
	for typ != nil {
		if _, ok := seen[typ]; ok {
			return nil
		}
		seen[typ] = struct{}{}

		if _, ok := s.canonicalTypes[typ]; ok {
			return typ
		}

		alias, ok := typ.(*types.Alias)
		if !ok {
			return nil
		}
		rhs := alias.Rhs()
		if rhs == nil || rhs == typ {
			return nil
		}
		typ = rhs
	}
	return nil
}

// buildSpxResourceSet builds one [SpxResourceSet] from the standardized spx classfile resource schema.
func buildSpxResourceSet(proj *xgo.Project) (*SpxResourceSet, error) {
	schema, err := getSpxClassfileResourceSchema()
	if err != nil {
		return nil, err
	}

	pack, err := spxPack(proj)
	if err != nil {
		return nil, err
	}
	packDoc, err := spxPackDocument(proj, pack.Directory, pack.IndexFile)
	if err != nil {
		return nil, err
	}

	set := &SpxResourceSet{
		backdrops: make(map[string]*SpxBackdropResource),
		sounds:    make(map[string]*SpxSoundResource),
		sprites:   make(map[string]*SpxSpriteResource),
		widgets:   make(map[string]*SpxWidgetResource),
	}

	spriteOrigins := make(map[string]dqlmaps.Node)
	for _, topLevelKind := range []string{"backdrop", "sound", "sprite", "widget"} {
		kind, ok := schema.kinds[topLevelKind]
		if !ok {
			return nil, fmt.Errorf("spx resource schema is missing kind %q", topLevelKind)
		}
		nodes, err := evalDQLNodes(kind.DiscoveryQuery, packDoc)
		if err != nil {
			return nil, fmt.Errorf("failed to discover %s resources: %w", topLevelKind, err)
		}
		for _, node := range nodes {
			name, err := resourceLocalName(kind, node)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve %s resource name: %w", topLevelKind, err)
			}

			switch topLevelKind {
			case "backdrop":
				var backdrop SpxBackdropResource
				if err := decodeNodeValue(node.Value, &backdrop); err != nil {
					return nil, fmt.Errorf("failed to decode backdrop %q: %w", name, err)
				}
				backdrop.Name = name
				backdrop.ID = SpxBackdropResourceID{BackdropName: name}
				set.backdrops[name] = &backdrop
			case "sound":
				var sound SpxSoundResource
				if err := decodeNodeValue(node.Value, &sound); err != nil {
					return nil, fmt.Errorf("failed to decode sound %q: %w", name, err)
				}
				sound.Name = name
				sound.ID = SpxSoundResourceID{SoundName: name}
				set.sounds[name] = &sound
			case "sprite":
				var sprite SpxSpriteResource
				if err := decodeNodeValue(node.Value, &sprite); err != nil {
					return nil, fmt.Errorf("failed to decode sprite %q: %w", name, err)
				}
				sprite.Name = name
				sprite.ID = SpxSpriteResourceID{SpriteName: name}
				sprite.Costumes = nil
				sprite.Animations = nil
				set.sprites[name] = &sprite
				spriteOrigins[name] = node
			case "widget":
				var widget SpxWidgetResource
				if err := decodeNodeValue(node.Value, &widget); err != nil {
					return nil, fmt.Errorf("failed to decode widget %q: %w", name, err)
				}
				widget.Name = name
				widget.ID = SpxWidgetResourceID{WidgetName: name}
				set.widgets[name] = &widget
			}
		}
	}

	costumeKind, ok := schema.kinds["sprite.costume"]
	if !ok {
		return nil, fmt.Errorf("spx resource schema is missing kind %q", "sprite.costume")
	}
	animationKind, ok := schema.kinds["sprite.animation"]
	if !ok {
		return nil, fmt.Errorf("spx resource schema is missing kind %q", "sprite.animation")
	}
	for spriteName, sprite := range set.sprites {
		origin, ok := spriteOrigins[spriteName]
		if !ok {
			return nil, fmt.Errorf("missing discovery origin for sprite %q", spriteName)
		}
		costumes, err := discoverSpriteCostumes(spriteName, origin, costumeKind)
		if err != nil {
			return nil, err
		}
		sprite.Costumes = costumes
		animations, err := discoverSpriteAnimations(spriteName, origin, animationKind, costumes)
		if err != nil {
			return nil, err
		}
		sprite.Animations = animations
	}

	return set, nil
}

// spxPack resolves the standardized pack metadata for one spx project.
func spxPack(proj *xgo.Project) (*modfile.Pack, error) {
	project, ok := proj.Mod.LookupClass(".spx")
	if !ok || project.Pack == nil {
		return nil, fmt.Errorf("failed to resolve spx pack metadata")
	}
	return project.Pack, nil
}

// spxPackDocument packs one spx project into one merged pack document.
func spxPackDocument(proj *xgo.Project, dir string, indexFile string) (map[string]any, error) {
	fsys := projectReadDirFS{proj: proj}
	packed, err := tool.PackProject(fsys, dir, indexFile)
	if err != nil {
		return nil, fmt.Errorf("failed to pack spx project: %w", err)
	}
	var packDoc map[string]any
	if err := json.Unmarshal(packed, &packDoc); err != nil {
		return nil, fmt.Errorf("failed to decode packed spx document: %w", err)
	}
	if err := restoreEmptyPackObjects(fsys, dir, indexFile, packDoc); err != nil {
		return nil, fmt.Errorf("failed to restore empty pack objects: %w", err)
	}
	return packDoc, nil
}

// restoreEmptyPackObjects restores descendant pack objects whose empty config
// maps were dropped by the current pack implementation.
func restoreEmptyPackObjects(fsys fs.ReadDirFS, current string, indexFile string, obj map[string]any) error {
	entries, err := fsys.ReadDir(current)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		childDir := joinProjectFSPath(current, name)
		childObj := make(map[string]any)
		if existing, ok := obj[name]; ok {
			var isMap bool
			childObj, isMap = existing.(map[string]any)
			if !isMap {
				return fmt.Errorf("collision: key %q already exists at path %q", name, childDir)
			}
		}
		if err := restoreEmptyPackObjects(fsys, childDir, indexFile, childObj); err != nil {
			return err
		}
		hasIndex, err := hasPackConfig(fsys, childDir, indexFile)
		if err != nil {
			return err
		}
		if hasIndex || len(childObj) > 0 {
			obj[name] = childObj
		}
	}
	return nil
}

// hasPackConfig reports whether one pack directory contains the exact index file.
func hasPackConfig(fsys fs.ReadDirFS, dir string, indexFile string) (bool, error) {
	_, err := fs.ReadFile(fsys, joinProjectFSPath(dir, indexFile))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, err
}

// joinProjectFSPath joins one directory and one relative name using fs.FS path
// conventions.
func joinProjectFSPath(dir string, name string) string {
	if dir == "." {
		return name
	}
	return dir + "/" + name
}

// discoverSpriteCostumes discovers one sprite's costume resources from one origin node.
func discoverSpriteCostumes(spriteName string, origin dqlmaps.Node, kind pkgdata.PkgResourceKind) ([]SpxSpriteCostumeResource, error) {
	nodes, err := evalDQLNodes(kind.DiscoveryQuery, origin)
	if err != nil {
		return nil, fmt.Errorf("failed to discover sprite costumes for %q: %w", spriteName, err)
	}
	costumes := make([]SpxSpriteCostumeResource, 0, len(nodes))
	for _, node := range nodes {
		name, err := resourceLocalName(kind, node)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve costume name for %q: %w", spriteName, err)
		}
		var costume SpxSpriteCostumeResource
		if err := decodeNodeValue(node.Value, &costume); err != nil {
			return nil, fmt.Errorf("failed to decode costume %q for %q: %w", name, spriteName, err)
		}
		costume.Name = name
		costume.ID = SpxSpriteCostumeResourceID{
			SpriteName:  spriteName,
			CostumeName: name,
		}
		costumes = append(costumes, costume)
	}
	return costumes, nil
}

// discoverSpriteAnimations discovers one sprite's animation resources from one origin node.
func discoverSpriteAnimations(
	spriteName string,
	origin dqlmaps.Node,
	kind pkgdata.PkgResourceKind,
	costumes []SpxSpriteCostumeResource,
) ([]SpxSpriteAnimationResource, error) {
	nodes, err := evalDQLNodes(kind.DiscoveryQuery, origin)
	if err != nil {
		return nil, fmt.Errorf("failed to discover sprite animations for %q: %w", spriteName, err)
	}
	costumeIndexes := make(map[string]int, len(costumes))
	for i, costume := range costumes {
		costumeIndexes[costume.Name] = i
	}
	animations := make([]SpxSpriteAnimationResource, 0, len(nodes))
	for _, node := range nodes {
		name, err := resourceLocalName(kind, node)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve animation name for %q: %w", spriteName, err)
		}
		var raw struct {
			FrameFrom string `json:"frameFrom"`
			FrameTo   string `json:"frameTo"`
		}
		if err := decodeNodeValue(node.Value, &raw); err != nil {
			return nil, fmt.Errorf("failed to decode animation %q for %q: %w", name, spriteName, err)
		}
		animation := SpxSpriteAnimationResource{
			ID: SpxSpriteAnimationResourceID{
				SpriteName:    spriteName,
				AnimationName: name,
			},
			Name: name,
		}
		if idx, ok := costumeIndexes[raw.FrameFrom]; ok {
			animation.FromIndex = &idx
		}
		if idx, ok := costumeIndexes[raw.FrameTo]; ok {
			animation.ToIndex = &idx
		}
		animations = append(animations, animation)
	}
	return animations, nil
}

// evalDQLNodes evaluates one DQL query and collects the resulting nodes.
func evalDQLNodes(query string, src any) ([]dqlmaps.Node, error) {
	nodes, err := dqlmaps.Eval(normalizeDQLQuery(query), src)
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

// resourceLocalName resolves the local resource name for one discovered node.
func resourceLocalName(kind pkgdata.PkgResourceKind, node dqlmaps.Node) (string, error) {
	if kind.NameDiscoveryQuery != "" {
		nameNodes, err := dqlmaps.Eval(normalizeDQLQuery(kind.NameDiscoveryQuery), node)
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
	return "", fmt.Errorf("resource kind %q has no local name", kind.Name)
}

// normalizeDQLQuery rewrites parser-hostile env attribute syntax to the quoted form.
func normalizeDQLQuery(query string) string {
	return strings.ReplaceAll(query, "$type", `$"type"`)
}

// decodeNodeValue decodes one node value into dst.
func decodeNodeValue(value any, dst any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// resolveSpxSpriteResourceForNode resolves the sprite resource context for one node.
func resolveSpxSpriteResourceForNode(result *compileResult, node xgoast.Node) *SpxSpriteResource {
	sprite := resolveSpxSpriteResourceFromEnclosingCall(result, node)
	if sprite != nil {
		return sprite
	}
	return inferSpxSpriteResourceEnclosingNode(result, node)
}

// resolveSpxSpriteResourceFromCallArg resolves the sprite resource context for
// one call argument mapped to targetParam on fun.
func resolveSpxSpriteResourceFromCallArg(result *compileResult, callExpr *xgoast.CallExpr, fun *types.Func, targetParam int) *SpxSpriteResource {
	schema, err := getSpxClassfileResourceSchema()
	if err != nil {
		return inferSpxSpriteResourceEnclosingNode(result, callExpr)
	}
	for _, binding := range schema.apiScopeBindings[fun.FullName()] {
		if binding.TargetParam != targetParam {
			continue
		}
		if sprite := resolveSpxSpriteResourceFromBinding(result, callExpr, binding); sprite != nil {
			return sprite
		}
		break
	}
	return inferSpxSpriteResourceEnclosingNode(result, callExpr)
}

// resolveSpxSpriteResourceFromEnclosingCall resolves the sprite resource context
// for one node by checking API-position scope bindings on its enclosing call.
func resolveSpxSpriteResourceFromEnclosingCall(result *compileResult, node xgoast.Node) *SpxSpriteResource {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	astPkg, _ := result.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(result.proj.Fset, astPkg, node)
	if astFile == nil {
		return nil
	}

	var sprite *SpxSpriteResource
	xgoutil.WalkPathEnclosingInterval(astFile, node.Pos(), node.End(), false, func(pathNode xgoast.Node) bool {
		callExpr, ok := pathNode.(*xgoast.CallExpr)
		if !ok {
			return true
		}
		xgoutil.WalkCallExprArgs(typeInfo, callExpr, func(fun *types.Func, params *types.Tuple, paramIndex int, arg xgoast.Expr, argIndex int) bool {
			if node.Pos() < arg.Pos() || node.End() > arg.End() {
				return true
			}
			sprite = resolveSpxSpriteResourceFromCallArg(result, callExpr, fun, paramIndex)
			return false
		})
		return false
	})
	return sprite
}

// resolveSpxSpriteResourceFromBinding resolves the sprite resource context for
// one binding source.
func resolveSpxSpriteResourceFromBinding(result *compileResult, callExpr *xgoast.CallExpr, binding pkgdata.PkgResourceAPIScopeBinding) *SpxSpriteResource {
	if binding.SourceReceiver {
		return resolveSpxSpriteResourceFromReceiver(result, callExpr)
	}
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	var sprite *SpxSpriteResource
	xgoutil.WalkCallExprArgs(typeInfo, callExpr, func(fun *types.Func, params *types.Tuple, paramIndex int, arg xgoast.Expr, argIndex int) bool {
		if paramIndex != binding.SourceParam {
			return true
		}
		sprite = resolveSpxSpriteResourceFromExpr(result, arg)
		return false
	})
	return sprite
}

// resolveSpxSpriteResourceFromReceiver resolves the sprite resource context from
// one call receiver.
func resolveSpxSpriteResourceFromReceiver(result *compileResult, callExpr *xgoast.CallExpr) *SpxSpriteResource {
	switch fun := callExpr.Fun.(type) {
	case *xgoast.Ident:
		return inferSpxSpriteResourceEnclosingNode(result, callExpr)
	case *xgoast.SelectorExpr:
		return resolveSpxSpriteResourceFromExpr(result, fun.X)
	default:
		return nil
	}
}

// resolveSpxSpriteResourceFromExpr resolves the sprite resource context from one expression.
func resolveSpxSpriteResourceFromExpr(result *compileResult, expr xgoast.Expr) *SpxSpriteResource {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	typ := xgoutil.DerefType(typeInfo.TypeOf(expr))
	switch canonicalSpxResourceNameType(typ) {
	case GetSpxSpriteNameType():
		tv := typeInfo.Types[expr]
		spriteName, ok := xgoutil.StringLitOrConstValue(expr, tv)
		if !ok || spriteName == "" {
			return nil
		}
		return result.spxResourceSet.Sprite(spriteName)
	}

	switch expr := expr.(type) {
	case *xgoast.Ident:
		obj := typeInfo.ObjectOf(expr)
		if obj == nil {
			return nil
		}
		if _, ok := result.spxSpriteResourceAutoBindings[obj]; !ok {
			return nil
		}
		return result.spxResourceSet.Sprite(obj.Name())
	}
	return nil
}

// projectReadDirFS adapts one [xgo.Project] to [fs.ReadDirFS].
type projectReadDirFS struct {
	proj *xgo.Project
}

// Open opens one project file for reading.
func (fsys projectReadDirFS) Open(name string) (fs.File, error) {
	name = normalizeProjectFSPath(name)
	file, ok := fsys.proj.File(name)
	if !ok {
		return nil, fs.ErrNotExist
	}
	return &projectFSFile{
		name: name,
		rd:   bytes.NewReader(file.Content),
	}, nil
}

// ReadDir lists one project directory.
func (fsys projectReadDirFS) ReadDir(name string) ([]fs.DirEntry, error) {
	name = normalizeProjectFSPath(name)
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
		entries = append(entries, projectDirEntry{name: dirName, dir: true})
	}
	for _, fileName := range slices.Sorted(maps.Keys(files)) {
		entries = append(entries, projectDirEntry{name: fileName, size: files[fileName]})
	}
	return entries, nil
}

// normalizeProjectFSPath normalizes one fs-style path for project-backed access.
func normalizeProjectFSPath(name string) string {
	if name == "" || name == "." {
		return "."
	}
	return strings.TrimPrefix(path.Clean(name), "./")
}

// projectFSFile is one read-only project-backed file.
type projectFSFile struct {
	name string
	rd   *bytes.Reader
}

// Stat reports the synthetic file info for one project-backed file.
func (f *projectFSFile) Stat() (fs.FileInfo, error) {
	return projectFileInfo{name: path.Base(f.name), size: f.rd.Size()}, nil
}

// Read reads file content.
func (f *projectFSFile) Read(p []byte) (int, error) {
	return f.rd.Read(p)
}

// Close closes one project-backed file.
func (f *projectFSFile) Close() error {
	return nil
}

// projectFileInfo is one synthetic [fs.FileInfo] for one project-backed file.
type projectFileInfo struct {
	name string
	size int64
}

// Name reports the base name.
func (i projectFileInfo) Name() string {
	return i.name
}

// Size reports the file size in bytes.
func (i projectFileInfo) Size() int64 {
	return i.size
}

// Mode reports a regular read-only file mode.
func (i projectFileInfo) Mode() fs.FileMode {
	return 0444
}

// ModTime reports the zero time because project files are versioned, not timestamped.
func (i projectFileInfo) ModTime() time.Time {
	return time.Time{}
}

// IsDir reports whether the entry is a directory.
func (i projectFileInfo) IsDir() bool {
	return false
}

// Sys reports no underlying system data.
func (i projectFileInfo) Sys() any {
	return nil
}

// projectDirEntry is one synthetic [fs.DirEntry] for one project-backed directory entry.
type projectDirEntry struct {
	name string
	dir  bool
	size int64
}

// Name reports the entry name.
func (e projectDirEntry) Name() string {
	return e.name
}

// IsDir reports whether the entry is a directory.
func (e projectDirEntry) IsDir() bool {
	return e.dir
}

// Type reports the entry mode type bits.
func (e projectDirEntry) Type() fs.FileMode {
	if e.dir {
		return fs.ModeDir | 0555
	}
	return 0444
}

// Info reports synthetic file info for the entry.
func (e projectDirEntry) Info() (fs.FileInfo, error) {
	if e.dir {
		return projectDirInfo{name: e.name}, nil
	}
	return projectFileInfo{name: e.name, size: e.size}, nil
}

// projectDirInfo is one synthetic [fs.FileInfo] for one project-backed directory.
type projectDirInfo struct {
	name string
}

// Name reports the directory name.
func (i projectDirInfo) Name() string {
	return i.name
}

// Size reports zero for synthetic directories.
func (i projectDirInfo) Size() int64 {
	return 0
}

// Mode reports a read-only directory mode.
func (i projectDirInfo) Mode() fs.FileMode {
	return fs.ModeDir | 0555
}

// ModTime reports the zero time because project files are versioned, not timestamped.
func (i projectDirInfo) ModTime() time.Time {
	return time.Time{}
}

// IsDir reports that the info describes a directory.
func (i projectDirInfo) IsDir() bool {
	return true
}

// Sys reports no underlying system data.
func (i projectDirInfo) Sys() any {
	return nil
}

var (
	_ fs.ReadDirFS = projectReadDirFS{}
	_ fs.File      = (*projectFSFile)(nil)
	_ fs.DirEntry  = projectDirEntry{}
)
