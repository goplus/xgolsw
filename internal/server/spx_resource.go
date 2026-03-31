package server

import (
	xgoast "github.com/goplus/xgo/ast"
	classfilespx "github.com/goplus/xgolsw/internal/classfile/spx"
	"github.com/goplus/xgolsw/xgo"
)

// SpxResourceID is the ID of an spx resource.
type SpxResourceID interface {
	Name() string
	URI() SpxResourceURI
	ContextURI() SpxResourceContextURI
}

// SpxResourceRef is a reference to an spx resource.
type SpxResourceRef struct {
	ID   SpxResourceID
	Kind SpxResourceRefKind
	Node xgoast.Node
}

type (
	SpxResourceRefKind         = classfilespx.ResourceRefKind
	SpxResourceSet             = classfilespx.ResourceSet
	SpxBackdropResource        = classfilespx.BackdropResource
	SpxSpriteResource          = classfilespx.SpriteResource
	SpxSpriteCostumeResource   = classfilespx.SpriteCostumeResource
	SpxSpriteAnimationResource = classfilespx.SpriteAnimationResource
	SpxSoundResource           = classfilespx.SoundResource
	SpxWidgetResource          = classfilespx.WidgetResource
)

// SpxBackdropResourceID is the ID of an spx backdrop resource.
type SpxBackdropResourceID struct {
	BackdropName string
}

// Name implements [SpxResourceID].
func (id SpxBackdropResourceID) Name() string {
	return id.BackdropName
}

// URI implements [SpxResourceID].
func (id SpxBackdropResourceID) URI() SpxResourceURI {
	return SpxResourceURI(classfilespx.BackdropResourceID{BackdropName: id.BackdropName}.URI())
}

// ContextURI implements [SpxResourceID].
func (id SpxBackdropResourceID) ContextURI() SpxResourceContextURI {
	return SpxBackdropResourceContextURI
}

// SpxSpriteResourceID is the ID of an spx sprite resource.
type SpxSpriteResourceID struct {
	SpriteName string
}

// Name implements [SpxResourceID].
func (id SpxSpriteResourceID) Name() string {
	return id.SpriteName
}

// URI implements [SpxResourceID].
func (id SpxSpriteResourceID) URI() SpxResourceURI {
	return SpxResourceURI(classfilespx.SpriteResourceID{SpriteName: id.SpriteName}.URI())
}

// ContextURI implements [SpxResourceID].
func (id SpxSpriteResourceID) ContextURI() SpxResourceContextURI {
	return SpxSpriteResourceContextURI
}

// SpxSpriteCostumeResourceID is the ID of an spx sprite costume resource.
type SpxSpriteCostumeResourceID struct {
	SpriteName  string
	CostumeName string
}

// Name implements [SpxResourceID].
func (id SpxSpriteCostumeResourceID) Name() string {
	return id.CostumeName
}

// URI implements [SpxResourceID].
func (id SpxSpriteCostumeResourceID) URI() SpxResourceURI {
	return SpxResourceURI(classfilespx.SpriteCostumeResourceID{
		SpriteName:  id.SpriteName,
		CostumeName: id.CostumeName,
	}.URI())
}

// ContextURI implements [SpxResourceID].
func (id SpxSpriteCostumeResourceID) ContextURI() SpxResourceContextURI {
	return FormatSpxSpriteCostumeResourceContextURI(id.SpriteName)
}

// SpxSpriteAnimationResourceID is the ID of an spx sprite animation resource.
type SpxSpriteAnimationResourceID struct {
	SpriteName    string
	AnimationName string
}

// Name implements [SpxResourceID].
func (id SpxSpriteAnimationResourceID) Name() string {
	return id.AnimationName
}

// URI implements [SpxResourceID].
func (id SpxSpriteAnimationResourceID) URI() SpxResourceURI {
	return SpxResourceURI(classfilespx.SpriteAnimationResourceID{
		SpriteName:    id.SpriteName,
		AnimationName: id.AnimationName,
	}.URI())
}

// ContextURI implements [SpxResourceID].
func (id SpxSpriteAnimationResourceID) ContextURI() SpxResourceContextURI {
	return FormatSpxSpriteAnimationResourceContextURI(id.SpriteName)
}

// SpxSoundResourceID is the ID of an spx sound resource.
type SpxSoundResourceID struct {
	SoundName string
}

// Name implements [SpxResourceID].
func (id SpxSoundResourceID) Name() string {
	return id.SoundName
}

// URI implements [SpxResourceID].
func (id SpxSoundResourceID) URI() SpxResourceURI {
	return SpxResourceURI(classfilespx.SoundResourceID{SoundName: id.SoundName}.URI())
}

// ContextURI implements [SpxResourceID].
func (id SpxSoundResourceID) ContextURI() SpxResourceContextURI {
	return SpxSoundResourceContextURI
}

// SpxWidgetResourceID is the ID of an spx widget resource.
type SpxWidgetResourceID struct {
	WidgetName string
}

// Name implements [SpxResourceID].
func (id SpxWidgetResourceID) Name() string {
	return id.WidgetName
}

// URI implements [SpxResourceID].
func (id SpxWidgetResourceID) URI() SpxResourceURI {
	return SpxResourceURI(classfilespx.WidgetResourceID{WidgetName: id.WidgetName}.URI())
}

// ContextURI implements [SpxResourceID].
func (id SpxWidgetResourceID) ContextURI() SpxResourceContextURI {
	return SpxWidgetResourceContextURI
}

const (
	SpxResourceRefKindStringLiteral        = classfilespx.ResourceRefKindStringLiteral
	SpxResourceRefKindAutoBindingReference = classfilespx.ResourceRefKindAutoBindingReference
	SpxResourceRefKindConstantReference    = classfilespx.ResourceRefKindConstantReference

	SpxBackdropResourceContextURI SpxResourceContextURI = SpxResourceContextURI(classfilespx.BackdropResourceContextURI)
	SpxSpriteResourceContextURI   SpxResourceContextURI = SpxResourceContextURI(classfilespx.SpriteResourceContextURI)
	SpxSoundResourceContextURI    SpxResourceContextURI = SpxResourceContextURI(classfilespx.SoundResourceContextURI)
	SpxWidgetResourceContextURI   SpxResourceContextURI = SpxResourceContextURI(classfilespx.WidgetResourceContextURI)
)

// ParseSpxResourceURI parses an spx resource URI and returns the corresponding
// spx resource ID.
func ParseSpxResourceURI[T ~string](uri T) (SpxResourceID, error) {
	id, err := classfilespx.ParseResourceURI(classfilespx.ResourceURI(uri))
	if err != nil {
		return nil, err
	}
	return wrapSpxResourceID(id), nil
}

// NewSpxResourceSet creates a new spx resource set.
func NewSpxResourceSet(proj *xgo.Project, rootDir string) (*SpxResourceSet, error) {
	return classfilespx.NewResourceSet(proj, rootDir)
}

// FormatSpxSpriteCostumeResourceContextURI formats the [SpxResourceContextURI]
// for a sprite's costume resources.
func FormatSpxSpriteCostumeResourceContextURI(spriteName string) SpxResourceContextURI {
	return SpxResourceContextURI(classfilespx.FormatSpriteCostumeResourceContextURI(spriteName))
}

// FormatSpxSpriteAnimationResourceContextURI formats the [SpxResourceContextURI]
// for a sprite's animation resources.
func FormatSpxSpriteAnimationResourceContextURI(spriteName string) SpxResourceContextURI {
	return SpxResourceContextURI(classfilespx.FormatSpriteAnimationResourceContextURI(spriteName))
}

func wrapSpxResourceID(id classfilespx.ResourceID) SpxResourceID {
	switch id := id.(type) {
	case nil:
		return nil
	case classfilespx.BackdropResourceID:
		return SpxBackdropResourceID{BackdropName: id.BackdropName}
	case classfilespx.SpriteResourceID:
		return SpxSpriteResourceID{SpriteName: id.SpriteName}
	case classfilespx.SpriteCostumeResourceID:
		return SpxSpriteCostumeResourceID{SpriteName: id.SpriteName, CostumeName: id.CostumeName}
	case classfilespx.SpriteAnimationResourceID:
		return SpxSpriteAnimationResourceID{SpriteName: id.SpriteName, AnimationName: id.AnimationName}
	case classfilespx.SoundResourceID:
		return SpxSoundResourceID{SoundName: id.SoundName}
	case classfilespx.WidgetResourceID:
		return SpxWidgetResourceID{WidgetName: id.WidgetName}
	default:
		return nil
	}
}

func wrapSpxResourceRef(ref *classfilespx.ResourceRef) *SpxResourceRef {
	if ref == nil {
		return nil
	}
	id := wrapSpxResourceID(ref.ID)
	if id == nil {
		return nil
	}
	return &SpxResourceRef{
		ID:   id,
		Kind: ref.Kind,
		Node: ref.Node,
	}
}

var (
	FormatSpriteCostumeResourceContextURI   = FormatSpxSpriteCostumeResourceContextURI
	FormatSpriteAnimationResourceContextURI = FormatSpxSpriteAnimationResourceContextURI
)
