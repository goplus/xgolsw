package server

import classfilespx "github.com/goplus/xgolsw/internal/classfile/spx"

type (
	SpxResourceID                = classfilespx.ResourceID
	SpxResourceRef               = classfilespx.ResourceRef
	SpxResourceRefKind           = classfilespx.ResourceRefKind
	SpxResourceURI               = classfilespx.ResourceURI
	SpxResourceContextURI        = classfilespx.ResourceContextURI
	SpxResourceSet               = classfilespx.ResourceSet
	SpxBackdropResource          = classfilespx.BackdropResource
	SpxBackdropResourceID        = classfilespx.BackdropResourceID
	SpxSpriteResource            = classfilespx.SpriteResource
	SpxSpriteResourceID          = classfilespx.SpriteResourceID
	SpxSpriteCostumeResource     = classfilespx.SpriteCostumeResource
	SpxSpriteCostumeResourceID   = classfilespx.SpriteCostumeResourceID
	SpxSpriteAnimationResource   = classfilespx.SpriteAnimationResource
	SpxSpriteAnimationResourceID = classfilespx.SpriteAnimationResourceID
	SpxSoundResource             = classfilespx.SoundResource
	SpxSoundResourceID           = classfilespx.SoundResourceID
	SpxWidgetResource            = classfilespx.WidgetResource
	SpxWidgetResourceID          = classfilespx.WidgetResourceID
)

const (
	SpxResourceRefKindStringLiteral        = classfilespx.ResourceRefKindStringLiteral
	SpxResourceRefKindAutoBindingReference = classfilespx.ResourceRefKindAutoBindingReference
	SpxResourceRefKindConstantReference    = classfilespx.ResourceRefKindConstantReference

	SpxBackdropResourceContextURI = classfilespx.BackdropResourceContextURI
	SpxSpriteResourceContextURI   = classfilespx.SpriteResourceContextURI
	SpxSoundResourceContextURI    = classfilespx.SoundResourceContextURI
	SpxWidgetResourceContextURI   = classfilespx.WidgetResourceContextURI
)

var (
	ParseSpxResourceURI                     = classfilespx.ParseResourceURI
	FormatSpriteCostumeResourceContextURI   = classfilespx.FormatSpriteCostumeResourceContextURI
	FormatSpriteAnimationResourceContextURI = classfilespx.FormatSpriteAnimationResourceContextURI
)
