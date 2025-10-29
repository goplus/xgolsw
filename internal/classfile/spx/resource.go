package spx

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"

	"github.com/goplus/xgolsw/xgo"
)

// ResourceSet holds resolved spx resources grouped by kind.
type ResourceSet struct {
	backdrops map[string]*BackdropResource
	sounds    map[string]*SoundResource
	sprites   map[string]*SpriteResource
	widgets   map[string]*WidgetResource
}

// ResourceID identifies a specific resource instance.
type ResourceID interface {
	Name() string
	URI() ResourceURI
	ContextURI() ResourceContextURI
}

// ResourceURI is an spx resource URI.
type ResourceURI string

// ResourceContextURI is the URI representing a resource namespace.
type ResourceContextURI string

// NewResourceSet constructs a resource set rooted at the provided directory.
func NewResourceSet(proj *xgo.Project, rootDir string) (*ResourceSet, error) {
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

	backdrops := make(map[string]*BackdropResource, len(assets.Backdrops))
	for _, backdrop := range assets.Backdrops {
		b := backdrop
		b.ID = BackdropResourceID{BackdropName: b.Name}
		backdrops[b.Name] = &b
	}

	widgets := make(map[string]*WidgetResource, len(assets.Zorder))
	for _, item := range assets.Zorder {
		var widget WidgetResource
		if err := json.Unmarshal(item, &widget); err != nil || widget.Name == "" {
			continue
		}
		widget.ID = WidgetResourceID{WidgetName: widget.Name}
		widgets[widget.Name] = &widget
	}

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

		costumeIndexes := make(map[string]int, len(sprite.Costumes))
		for i := range sprite.Costumes {
			costume := &sprite.Costumes[i]
			costumeIndexes[costume.Name] = i
			costume.ID = SpriteCostumeResourceID{SpriteName: spriteName, CostumeName: costume.Name}
		}

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

		sprite.NormalCostumes = make([]SpriteCostumeResource, 0, len(sprite.Costumes))
		for i := range sprite.Costumes {
			if _, ok := animationCostumes[i]; ok {
				continue
			}
			sprite.NormalCostumes = append(sprite.NormalCostumes, sprite.Costumes[i])
		}

		sprites[spriteName] = &sprite
	}

	return &ResourceSet{
		backdrops: backdrops,
		sounds:    sounds,
		sprites:   sprites,
		widgets:   widgets,
	}, nil
}

// Backdrop returns the backdrop with the given name.
func (set *ResourceSet) Backdrop(name string) *BackdropResource {
	if set == nil {
		return nil
	}
	return set.backdrops[name]
}

// Sound returns the sound with the given name.
func (set *ResourceSet) Sound(name string) *SoundResource {
	if set == nil {
		return nil
	}
	return set.sounds[name]
}

// Sprite returns the sprite with the given name.
func (set *ResourceSet) Sprite(name string) *SpriteResource {
	if set == nil {
		return nil
	}
	return set.sprites[name]
}

// Widget returns the widget with the given name.
func (set *ResourceSet) Widget(name string) *WidgetResource {
	if set == nil {
		return nil
	}
	return set.widgets[name]
}

// BackdropResource represents a backdrop entry.
type BackdropResource struct {
	ID   BackdropResourceID `json:"-"`
	Name string             `json:"name"`
}

// BackdropResourceID uniquely identifies a backdrop.
type BackdropResourceID struct {
	BackdropName string
}

func (id BackdropResourceID) Name() string { return id.BackdropName }

// BackdropResourceContextURI is the context URI for backdrops.
const BackdropResourceContextURI ResourceContextURI = "spx://resources/backdrops"

func (id BackdropResourceID) URI() ResourceURI {
	return ResourceURI(fmt.Sprintf("%s/%s", id.ContextURI(), id.BackdropName))
}

func (id BackdropResourceID) ContextURI() ResourceContextURI { return BackdropResourceContextURI }

// SoundResource represents a sound entry.
type SoundResource struct {
	ID   SoundResourceID `json:"-"`
	Name string          `json:"name"`
	Path string          `json:"path"`
}

// SoundResourceID uniquely identifies a sound.
type SoundResourceID struct {
	SoundName string
}

func (id SoundResourceID) Name() string { return id.SoundName }

// SoundResourceContextURI is the context URI for sounds.
const SoundResourceContextURI ResourceContextURI = "spx://resources/sounds"

func (id SoundResourceID) URI() ResourceURI {
	return ResourceURI(fmt.Sprintf("%s/%s", id.ContextURI(), id.SoundName))
}

func (id SoundResourceID) ContextURI() ResourceContextURI { return SoundResourceContextURI }

// SpriteResource encodes sprite metadata.
type SpriteResource struct {
	ID       SpriteResourceID        `json:"-"`
	Name     string                  `json:"name"`
	Costumes []SpriteCostumeResource `json:"costumes"`

	NormalCostumes []SpriteCostumeResource   `json:"-"`
	FAnimations    map[string]spriteFAnim    `json:"fAnimations"`
	Animations     []SpriteAnimationResource `json:"-"`

	CostumeIndex     int    `json:"costumeIndex"`
	DefaultAnimation string `json:"defaultAnimation"`
}

// SpriteResourceID uniquely identifies a sprite.
type SpriteResourceID struct {
	SpriteName string
}

func (id SpriteResourceID) Name() string { return id.SpriteName }

// SpriteResourceContextURI is the context URI for sprites.
const SpriteResourceContextURI ResourceContextURI = "spx://resources/sprites"

func (id SpriteResourceID) URI() ResourceURI {
	return ResourceURI(fmt.Sprintf("%s/%s", id.ContextURI(), id.SpriteName))
}

func (id SpriteResourceID) ContextURI() ResourceContextURI { return SpriteResourceContextURI }

// Costume returns the costume with the given name.
func (sprite *SpriteResource) Costume(name string) *SpriteCostumeResource {
	idx := slices.IndexFunc(sprite.Costumes, func(costume SpriteCostumeResource) bool {
		return costume.Name == name
	})
	if idx < 0 {
		return nil
	}
	return &sprite.Costumes[idx]
}

// Animation returns the animation with the given name.
func (sprite *SpriteResource) Animation(name string) *SpriteAnimationResource {
	idx := slices.IndexFunc(sprite.Animations, func(animation SpriteAnimationResource) bool {
		return animation.Name == name
	})
	if idx < 0 {
		return nil
	}
	return &sprite.Animations[idx]
}

// SpriteCostumeResource describes a sprite costume asset.
type SpriteCostumeResource struct {
	ID   SpriteCostumeResourceID `json:"-"`
	Name string                  `json:"name"`
	Path string                  `json:"path"`
}

// SpriteCostumeResourceID uniquely identifies a sprite costume.
type SpriteCostumeResourceID struct {
	SpriteName  string
	CostumeName string
}

func (id SpriteCostumeResourceID) Name() string { return id.CostumeName }

func (id SpriteCostumeResourceID) URI() ResourceURI {
	return ResourceURI(fmt.Sprintf("%s/%s", id.ContextURI(), id.CostumeName))
}

func (id SpriteCostumeResourceID) ContextURI() ResourceContextURI {
	return FormatSpriteCostumeContextURI(id.SpriteName)
}

// SpriteAnimationResource captures animation metadata.
type SpriteAnimationResource struct {
	ID        SpriteAnimationResourceID `json:"-"`
	Name      string                    `json:"name"`
	FromIndex *int                      `json:"-"`
	ToIndex   *int                      `json:"-"`
}

// SpriteAnimationResourceID uniquely identifies a sprite animation.
type SpriteAnimationResourceID struct {
	SpriteName    string
	AnimationName string
}

func (id SpriteAnimationResourceID) Name() string { return id.AnimationName }

func (id SpriteAnimationResourceID) URI() ResourceURI {
	return ResourceURI(fmt.Sprintf("%s/%s", id.ContextURI(), id.AnimationName))
}

func (id SpriteAnimationResourceID) ContextURI() ResourceContextURI {
	return FormatSpriteAnimationContextURI(id.SpriteName)
}

// WidgetResource describes widget metadata.
type WidgetResource struct {
	ID    WidgetResourceID `json:"-"`
	Name  string           `json:"name"`
	Type  string           `json:"type"`
	Label string           `json:"label"`
	Val   string           `json:"val"`
}

// WidgetResourceID uniquely identifies a widget.
type WidgetResourceID struct {
	WidgetName string
}

func (id WidgetResourceID) Name() string { return id.WidgetName }

// WidgetResourceContextURI is the context URI for widgets.
const WidgetResourceContextURI ResourceContextURI = "spx://resources/widgets"

func (id WidgetResourceID) URI() ResourceURI {
	return ResourceURI(fmt.Sprintf("%s/%s", id.ContextURI(), id.WidgetName))
}

func (id WidgetResourceID) ContextURI() ResourceContextURI { return WidgetResourceContextURI }

type spriteFAnim struct {
	FrameFrom string `json:"frameFrom"`
	FrameTo   string `json:"frameTo"`
}

func listSubdirs(proj *xgo.Project, dir string) []string {
	prefix := path.Clean(dir) + "/"
	subdirs := make(map[string]struct{})
	for file := range proj.Files() {
		if !strings.HasPrefix(file, prefix) {
			continue
		}
		remaining := file[len(prefix):]
		if idx := strings.IndexByte(remaining, '/'); idx > 0 {
			subdirs[remaining[:idx]] = struct{}{}
		}
	}

	result := slices.Collect(maps.Keys(subdirs))
	slices.Sort(result)
	return result
}

// ParseResourceURI converts a resource URI back into its identifier.
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

// FormatSpriteCostumeContextURI formats the context URI for sprite costumes.
func FormatSpriteCostumeContextURI(spriteName string) ResourceContextURI {
	return ResourceContextURI(fmt.Sprintf("%s/%s/costumes", SpriteResourceContextURI, spriteName))
}

// FormatSpriteAnimationContextURI formats the context URI for sprite animations.
func FormatSpriteAnimationContextURI(spriteName string) ResourceContextURI {
	return ResourceContextURI(fmt.Sprintf("%s/%s/animations", SpriteResourceContextURI, spriteName))
}
