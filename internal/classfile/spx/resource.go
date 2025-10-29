package spx

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"iter"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/goplus/xgolsw/xgo"
)

// ResourceID is the ID of an spx resource.
type ResourceID interface {
	Name() string
	URI() ResourceURI
	ContextURI() ResourceContextURI
}

// ParseResourceURI parses a [ResourceURI] and returns the corresponding [ResourceID].
func ParseResourceURI(uri ResourceURI) (ResourceID, error) {
	u, err := url.Parse(string(uri))
	if err != nil {
		return nil, fmt.Errorf("failed to parse spx resource URI: %w", err)
	}
	pathParts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if u.Scheme != "spx" || u.Host != "resources" || path.Clean(u.Path) != u.Path || len(pathParts) < 2 {
		return nil, fmt.Errorf("invalid spx resource URI: %s", uri)
	}
	switch pathParts[0] {
	case "backdrops":
		return BackdropResourceID{BackdropName: pathParts[1]}, nil
	case "sounds":
		return SoundResourceID{SoundName: pathParts[1]}, nil
	case "sprites":
		if len(pathParts) == 2 {
			return SpriteResourceID{SpriteName: pathParts[1]}, nil
		}
		if len(pathParts) > 3 {
			switch pathParts[2] {
			case "costumes":
				return SpriteCostumeResourceID{SpriteName: pathParts[1], CostumeName: pathParts[3]}, nil
			case "animations":
				return SpriteAnimationResourceID{SpriteName: pathParts[1], AnimationName: pathParts[3]}, nil
			}
		}
	case "widgets":
		return WidgetResourceID{WidgetName: pathParts[1]}, nil
	}
	return nil, fmt.Errorf("unsupported or malformed spx resource type in URI: %s", uri)
}

// ResourceURI represents a URI string for an spx resource.
type ResourceURI string

// HTML returns the HTML representation of the spx resource URI.
func (u ResourceURI) HTML() string {
	return fmt.Sprintf("<resource-preview resource=%q />\n", template.HTMLEscapeString(string(u)))
}

// ResourceContextURI represents a URI for an spx resource context.
//
// Examples:
// - "spx://resources/sprites"
// - "spx://resources/sprites/<sName>/costumes"
// - "spx://resources/sounds"
type ResourceContextURI string

// ResourceSet holds resolved spx resources grouped by kind, keyed by their
// resource names.
type ResourceSet struct {
	backdrops map[string]*BackdropResource
	sprites   map[string]*SpriteResource
	sounds    map[string]*SoundResource
	widgets   map[string]*WidgetResource
}

// NewResourceSet constructs an spx resource set rooted at the provided directory.
func NewResourceSet(proj *xgo.Project, rootDir string) (*ResourceSet, error) {
	// Read and parse the main index.json for backdrops and widgets.
	metadataPath := path.Join(rootDir, "index.json")
	metadataFile, ok := proj.File(metadataPath)
	if !ok {
		return nil, fmt.Errorf("failed to read metadata: %w", fs.ErrNotExist)
	}

	var assets struct {
		Backdrops []BackdropResource `json:"backdrops"`
		Zorder    []json.RawMessage  `json:"zorder"`
	}
	if err := json.Unmarshal(metadataFile.Content, &assets); err != nil {
		return nil, fmt.Errorf("failed to parse metadata: %w", err)
	}

	// Process backdrops.
	backdrops := make(map[string]*BackdropResource, len(assets.Backdrops))
	for _, backdrop := range assets.Backdrops {
		backdrop.ID = BackdropResourceID{BackdropName: backdrop.Name}
		backdrops[backdrop.Name] = &backdrop
	}

	// Read sprites directory.
	spriteDirs := listSubdirs(proj, path.Join(rootDir, "sprites"))
	sprites := make(map[string]*SpriteResource, len(spriteDirs))
	for _, spriteName := range spriteDirs {
		metadataPath := path.Join(rootDir, "sprites", spriteName, "index.json")
		metadataFile, ok := proj.File(metadataPath)
		if !ok {
			return nil, fmt.Errorf("failed to read sprite metadata: %w", fs.ErrNotExist)
		}

		sprite := SpriteResource{
			ID:   SpriteResourceID{SpriteName: spriteName},
			Name: spriteName,
		}
		if err := json.Unmarshal(metadataFile.Content, &sprite); err != nil {
			return nil, fmt.Errorf("failed to parse sprite metadata: %w", err)
		}

		// Process costumes.
		costumeIndexes := make(map[string]int, len(sprite.Costumes))
		for i, costume := range sprite.Costumes {
			costumeIndexes[costume.Name] = i
			sprite.Costumes[i].ID = SpriteCostumeResourceID{
				SpriteName:  spriteName,
				CostumeName: costume.Name,
			}
		}

		// Process animations.
		animationCostumes := make(map[int]struct{})
		sprite.Animations = make([]SpriteAnimationResource, 0, len(sprite.FAnimations))
		for name, fAnim := range sprite.FAnimations {
			animation := SpriteAnimationResource{
				ID:   SpriteAnimationResourceID{SpriteName: spriteName, AnimationName: name},
				Name: name,
			}
			if fromIdx, ok := costumeIndexes[fAnim.FrameFrom]; ok {
				animation.FromIndex = &fromIdx
			}
			if toIdx, ok := costumeIndexes[fAnim.FrameTo]; ok {
				animation.ToIndex = &toIdx
			}
			if animation.FromIndex != nil && animation.ToIndex != nil {
				for i := *animation.FromIndex; i <= *animation.ToIndex; i++ {
					if i >= 0 && i < len(sprite.Costumes) {
						animationCostumes[i] = struct{}{}
					}
				}
			}
			sprite.Animations = append(sprite.Animations, animation)
		}

		// Process normal costumes.
		sprite.NormalCostumes = make([]SpriteCostumeResource, 0, len(sprite.Costumes))
		for i, costume := range sprite.Costumes {
			if _, ok := animationCostumes[i]; !ok {
				sprite.NormalCostumes = append(sprite.NormalCostumes, costume)
			}
		}

		sprites[spriteName] = &sprite
	}

	// Read sounds directory.
	soundDirs := listSubdirs(proj, path.Join(rootDir, "sounds"))
	sounds := make(map[string]*SoundResource, len(soundDirs))
	for _, soundName := range soundDirs {
		metadataPath := path.Join(rootDir, "sounds", soundName, "index.json")
		metadataFile, ok := proj.File(metadataPath)
		if !ok {
			return nil, fmt.Errorf("failed to read sound metadata: %w", fs.ErrNotExist)
		}

		var sound SoundResource
		if err := json.Unmarshal(metadataFile.Content, &sound); err != nil {
			return nil, fmt.Errorf("failed to parse sound metadata: %w", err)
		}
		sound.Name = soundName
		sound.ID = SoundResourceID{SoundName: soundName}
		sounds[soundName] = &sound
	}

	// Process widgets from zorder.
	widgets := make(map[string]*WidgetResource, len(assets.Zorder))
	for _, item := range assets.Zorder {
		var widget WidgetResource
		if err := json.Unmarshal(item, &widget); err != nil || widget.Name == "" {
			continue
		}
		widget.ID = WidgetResourceID{WidgetName: widget.Name}
		widgets[widget.Name] = &widget
	}

	return &ResourceSet{
		backdrops: backdrops,
		sprites:   sprites,
		sounds:    sounds,
		widgets:   widgets,
	}, nil
}

// Backdrops returns all spx backdrop resources.
func (set *ResourceSet) Backdrops() iter.Seq2[string, *BackdropResource] {
	if set == nil || len(set.backdrops) == 0 {
		return emptySeq2[string, *BackdropResource]()
	}
	return maps.All(set.backdrops)
}

// Backdrop returns the spx backdrop with the given name. It returns nil if not found.
func (set *ResourceSet) Backdrop(name string) *BackdropResource {
	if set == nil {
		return nil
	}
	return set.backdrops[name]
}

// Sprites returns all spx sprite resources.
func (set *ResourceSet) Sprites() iter.Seq2[string, *SpriteResource] {
	if set == nil || len(set.sprites) == 0 {
		return emptySeq2[string, *SpriteResource]()
	}
	return maps.All(set.sprites)
}

// Sprite returns the spx sprite with the given name. It returns nil if not found.
func (set *ResourceSet) Sprite(name string) *SpriteResource {
	if set == nil {
		return nil
	}
	return set.sprites[name]
}

// Sounds returns all spx sound resources.
func (set *ResourceSet) Sounds() iter.Seq2[string, *SoundResource] {
	if set == nil || len(set.sounds) == 0 {
		return emptySeq2[string, *SoundResource]()
	}
	return maps.All(set.sounds)
}

// Sound returns the spx sound with the given name. It returns nil if not found.
func (set *ResourceSet) Sound(name string) *SoundResource {
	if set == nil {
		return nil
	}
	return set.sounds[name]
}

// Widgets returns all spx widget resources.
func (set *ResourceSet) Widgets() iter.Seq2[string, *WidgetResource] {
	if set == nil || len(set.widgets) == 0 {
		return emptySeq2[string, *WidgetResource]()
	}
	return maps.All(set.widgets)
}

// Widget returns the spx widget with the given name. It returns nil if not found.
func (set *ResourceSet) Widget(name string) *WidgetResource {
	if set == nil {
		return nil
	}
	return set.widgets[name]
}

// BackdropResource represents an spx backdrop resource.
type BackdropResource struct {
	ID   BackdropResourceID `json:"-"`
	Name string             `json:"name"`
	Path string             `json:"path"`
}

// BackdropResourceID is the ID of an spx backdrop resource.
type BackdropResourceID struct {
	BackdropName string
}

// Name implements [ResourceID].
func (id BackdropResourceID) Name() string {
	return id.BackdropName
}

// URI implements [ResourceID].
func (id BackdropResourceID) URI() ResourceURI {
	return ResourceURI(fmt.Sprintf("%s/%s", id.ContextURI(), id.BackdropName))
}

// BackdropResourceContextURI is the [ResourceContextURI] of [BackdropResource].
const BackdropResourceContextURI ResourceContextURI = "spx://resources/backdrops"

// ContextURI implements [ResourceID].
func (id BackdropResourceID) ContextURI() ResourceContextURI {
	return BackdropResourceContextURI
}

// spriteFAnimation mirrors the raw frame range metadata in a sprite's
// fAnimations section.
type spriteFAnimation struct {
	FrameFrom string `json:"frameFrom"`
	FrameTo   string `json:"frameTo"`
}

// SpriteResource represents an spx sprite resource.
type SpriteResource struct {
	ID               SpriteResourceID            `json:"-"`
	Name             string                      `json:"name"`
	Costumes         []SpriteCostumeResource     `json:"costumes"`
	NormalCostumes   []SpriteCostumeResource     `json:"-"`
	CostumeIndex     int                         `json:"costumeIndex"`
	FAnimations      map[string]spriteFAnimation `json:"fAnimations"`
	Animations       []SpriteAnimationResource   `json:"-"`
	DefaultAnimation string                      `json:"defaultAnimation"`
}

// SpriteResourceID is the ID of an spx sprite resource.
type SpriteResourceID struct {
	SpriteName string
}

// Name implements [ResourceID].
func (id SpriteResourceID) Name() string {
	return id.SpriteName
}

// URI implements [ResourceID].
func (id SpriteResourceID) URI() ResourceURI {
	return ResourceURI(fmt.Sprintf("%s/%s", id.ContextURI(), id.SpriteName))
}

// SpriteResourceContextURI is the [ResourceContextURI] of [SpriteResource].
const SpriteResourceContextURI ResourceContextURI = "spx://resources/sprites"

// ContextURI implements [ResourceID].
func (id SpriteResourceID) ContextURI() ResourceContextURI {
	return SpriteResourceContextURI
}

// Costume returns the costume with the given name. It returns nil if not found.
func (sprite *SpriteResource) Costume(name string) *SpriteCostumeResource {
	idx := slices.IndexFunc(sprite.Costumes, func(costume SpriteCostumeResource) bool {
		return costume.Name == name
	})
	if idx < 0 {
		return nil
	}
	return &sprite.Costumes[idx]
}

// Animation returns the animation with the given name. It returns nil if not found.
func (sprite *SpriteResource) Animation(name string) *SpriteAnimationResource {
	idx := slices.IndexFunc(sprite.Animations, func(animation SpriteAnimationResource) bool {
		return animation.Name == name
	})
	if idx < 0 {
		return nil
	}
	return &sprite.Animations[idx]
}

// SpriteCostumeResource represents an spx sprite costume resource.
type SpriteCostumeResource struct {
	ID   SpriteCostumeResourceID `json:"-"`
	Name string                  `json:"name"`
	Path string                  `json:"path"`
}

// SpriteCostumeResourceID is the ID of an spx sprite costume resource.
type SpriteCostumeResourceID struct {
	SpriteName  string
	CostumeName string
}

// Name implements [ResourceID].
func (id SpriteCostumeResourceID) Name() string {
	return id.CostumeName
}

// URI implements [ResourceID].
func (id SpriteCostumeResourceID) URI() ResourceURI {
	return ResourceURI(fmt.Sprintf("%s/%s", id.ContextURI(), id.CostumeName))
}

// FormatSpriteCostumeResourceContextURI formats the [ResourceContextURI] for an
// spx sprite's costume resources.
func FormatSpriteCostumeResourceContextURI(spriteName string) ResourceContextURI {
	return ResourceContextURI(fmt.Sprintf("%s/%s/costumes", SpriteResourceContextURI, spriteName))
}

// ContextURI implements [ResourceID].
func (id SpriteCostumeResourceID) ContextURI() ResourceContextURI {
	return FormatSpriteCostumeResourceContextURI(id.SpriteName)
}

// SpriteAnimationResource represents an spx sprite animation resource.
type SpriteAnimationResource struct {
	ID        SpriteAnimationResourceID `json:"-"`
	Name      string                    `json:"name"`
	FromIndex *int                      `json:"-"`
	ToIndex   *int                      `json:"-"`
}

// SpriteAnimationResourceID is the ID of an spx sprite animation resource.
type SpriteAnimationResourceID struct {
	SpriteName    string
	AnimationName string
}

// Name implements [ResourceID].
func (id SpriteAnimationResourceID) Name() string {
	return id.AnimationName
}

// URI implements [ResourceID].
func (id SpriteAnimationResourceID) URI() ResourceURI {
	return ResourceURI(fmt.Sprintf("%s/%s", id.ContextURI(), id.AnimationName))
}

// FormatSpriteAnimationResourceContextURI formats the [ResourceContextURI] for
// an spx sprite's animation resources.
func FormatSpriteAnimationResourceContextURI(spriteName string) ResourceContextURI {
	return ResourceContextURI(fmt.Sprintf("%s/%s/animations", SpriteResourceContextURI, spriteName))
}

// ContextURI implements [ResourceID].
func (id SpriteAnimationResourceID) ContextURI() ResourceContextURI {
	return FormatSpriteAnimationResourceContextURI(id.SpriteName)
}

// SoundResource represents an spx sound resource.
type SoundResource struct {
	ID   SoundResourceID `json:"-"`
	Name string          `json:"name"`
	Path string          `json:"path"`
}

// SoundResourceID is the ID of an spx sound resource.
type SoundResourceID struct {
	SoundName string
}

// Name implements [ResourceID].
func (id SoundResourceID) Name() string {
	return id.SoundName
}

// URI implements [ResourceID].
func (id SoundResourceID) URI() ResourceURI {
	return ResourceURI(fmt.Sprintf("%s/%s", id.ContextURI(), id.SoundName))
}

// SoundResourceContextURI is the [ResourceContextURI] of [SoundResource].
const SoundResourceContextURI ResourceContextURI = "spx://resources/sounds"

// ContextURI implements [ResourceID].
func (id SoundResourceID) ContextURI() ResourceContextURI {
	return SoundResourceContextURI
}

// WidgetResource represents an spx widget resource.
type WidgetResource struct {
	ID    WidgetResourceID `json:"-"`
	Name  string           `json:"name"`
	Type  string           `json:"type"`
	Label string           `json:"label"`
	Val   string           `json:"val"`
}

// WidgetResourceID is the ID of an spx widget resource.
type WidgetResourceID struct {
	WidgetName string
}

// Name implements [ResourceID].
func (id WidgetResourceID) Name() string {
	return id.WidgetName
}

// URI implements [ResourceID].
func (id WidgetResourceID) URI() ResourceURI {
	return ResourceURI(fmt.Sprintf("%s/%s", id.ContextURI(), id.WidgetName))
}

// WidgetResourceContextURI is the [ResourceContextURI] of [WidgetResource].
const WidgetResourceContextURI ResourceContextURI = "spx://resources/widgets"

// ContextURI implements [ResourceID].
func (id WidgetResourceID) ContextURI() ResourceContextURI {
	return WidgetResourceContextURI
}

// listSubdirs returns a list of subdirectories under the given directory.
func listSubdirs(proj *xgo.Project, dir string) []string {
	prefix := path.Clean(dir) + "/"
	subdirs := make(map[string]struct{})
	for file := range proj.Files() {
		if strings.HasPrefix(file, prefix) {
			remaining := file[len(prefix):]
			if idx := strings.IndexByte(remaining, '/'); idx > 0 {
				subdirs[remaining[:idx]] = struct{}{}
			}
		}
	}

	result := slices.Collect(maps.Keys(subdirs))
	slices.Sort(result)
	return result
}

// emptySeq2 returns an empty [iter.Seq2].
func emptySeq2[K comparable, V any]() iter.Seq2[K, V] {
	return func(func(K, V) bool) {}
}
