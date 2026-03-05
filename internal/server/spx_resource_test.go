package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpxResourceIDURI(t *testing.T) {
	t.Run("BackdropASCII", func(t *testing.T) {
		id := SpxBackdropResourceID{BackdropName: "backdrop1"}
		assert.Equal(t, SpxResourceURI("spx://resources/backdrops/backdrop1"), id.URI())
	})

	t.Run("BackdropWithSpaces", func(t *testing.T) {
		id := SpxBackdropResourceID{BackdropName: "my backdrop"}
		assert.Equal(t, SpxResourceURI("spx://resources/backdrops/my%20backdrop"), id.URI())
	})

	t.Run("BackdropNonASCII", func(t *testing.T) {
		id := SpxBackdropResourceID{BackdropName: "背景"}
		assert.Equal(t, SpxResourceURI("spx://resources/backdrops/%E8%83%8C%E6%99%AF"), id.URI())
	})

	t.Run("SoundASCII", func(t *testing.T) {
		id := SpxSoundResourceID{SoundName: "Sound1"}
		assert.Equal(t, SpxResourceURI("spx://resources/sounds/Sound1"), id.URI())
	})

	t.Run("SoundWithSpaces", func(t *testing.T) {
		id := SpxSoundResourceID{SoundName: "my sound"}
		assert.Equal(t, SpxResourceURI("spx://resources/sounds/my%20sound"), id.URI())
	})

	t.Run("SoundNonASCII", func(t *testing.T) {
		id := SpxSoundResourceID{SoundName: "音效"}
		assert.Equal(t, SpxResourceURI("spx://resources/sounds/%E9%9F%B3%E6%95%88"), id.URI())
	})

	t.Run("SpriteASCII", func(t *testing.T) {
		id := SpxSpriteResourceID{SpriteName: "Sprite1"}
		assert.Equal(t, SpxResourceURI("spx://resources/sprites/Sprite1"), id.URI())
	})

	t.Run("SpriteWithSpaces", func(t *testing.T) {
		id := SpxSpriteResourceID{SpriteName: "my sprite"}
		assert.Equal(t, SpxResourceURI("spx://resources/sprites/my%20sprite"), id.URI())
	})

	t.Run("SpriteNonASCII", func(t *testing.T) {
		id := SpxSpriteResourceID{SpriteName: "小猫"}
		assert.Equal(t, SpxResourceURI("spx://resources/sprites/%E5%B0%8F%E7%8C%AB"), id.URI())
	})

	t.Run("SpriteCostumeASCII", func(t *testing.T) {
		id := SpxSpriteCostumeResourceID{SpriteName: "Sprite1", CostumeName: "costume1"}
		assert.Equal(t, SpxResourceURI("spx://resources/sprites/Sprite1/costumes/costume1"), id.URI())
	})

	t.Run("SpriteCostumeWithSpaces", func(t *testing.T) {
		id := SpxSpriteCostumeResourceID{SpriteName: "my sprite", CostumeName: "my costume"}
		assert.Equal(t, SpxResourceURI("spx://resources/sprites/my%20sprite/costumes/my%20costume"), id.URI())
	})

	t.Run("SpriteCostumeNonASCII", func(t *testing.T) {
		id := SpxSpriteCostumeResourceID{SpriteName: "小猫", CostumeName: "跑步"}
		assert.Equal(t, SpxResourceURI("spx://resources/sprites/%E5%B0%8F%E7%8C%AB/costumes/%E8%B7%91%E6%AD%A5"), id.URI())
	})

	t.Run("SpriteAnimationASCII", func(t *testing.T) {
		id := SpxSpriteAnimationResourceID{SpriteName: "Sprite1", AnimationName: "anim1"}
		assert.Equal(t, SpxResourceURI("spx://resources/sprites/Sprite1/animations/anim1"), id.URI())
	})

	t.Run("SpriteAnimationWithSpaces", func(t *testing.T) {
		id := SpxSpriteAnimationResourceID{SpriteName: "my sprite", AnimationName: "my anim"}
		assert.Equal(t, SpxResourceURI("spx://resources/sprites/my%20sprite/animations/my%20anim"), id.URI())
	})

	t.Run("SpriteAnimationNonASCII", func(t *testing.T) {
		id := SpxSpriteAnimationResourceID{SpriteName: "小猫", AnimationName: "奔跑"}
		assert.Equal(t, SpxResourceURI("spx://resources/sprites/%E5%B0%8F%E7%8C%AB/animations/%E5%A5%94%E8%B7%91"), id.URI())
	})

	t.Run("WidgetASCII", func(t *testing.T) {
		id := SpxWidgetResourceID{WidgetName: "widget1"}
		assert.Equal(t, SpxResourceURI("spx://resources/widgets/widget1"), id.URI())
	})

	t.Run("WidgetWithSpaces", func(t *testing.T) {
		id := SpxWidgetResourceID{WidgetName: "my widget"}
		assert.Equal(t, SpxResourceURI("spx://resources/widgets/my%20widget"), id.URI())
	})

	t.Run("WidgetNonASCII", func(t *testing.T) {
		id := SpxWidgetResourceID{WidgetName: "分数"}
		assert.Equal(t, SpxResourceURI("spx://resources/widgets/%E5%88%86%E6%95%B0"), id.URI())
	})
}

func TestFormatSpxSpriteCostumeResourceContextURI(t *testing.T) {
	t.Run("ASCII", func(t *testing.T) {
		result := FormatSpxSpriteCostumeResourceContextURI("Sprite1")
		assert.Equal(t, SpxResourceContextURI("spx://resources/sprites/Sprite1/costumes"), result)
	})

	t.Run("WithSpaces", func(t *testing.T) {
		result := FormatSpxSpriteCostumeResourceContextURI("my sprite")
		assert.Equal(t, SpxResourceContextURI("spx://resources/sprites/my%20sprite/costumes"), result)
	})

	t.Run("NonASCII", func(t *testing.T) {
		result := FormatSpxSpriteCostumeResourceContextURI("小猫")
		assert.Equal(t, SpxResourceContextURI("spx://resources/sprites/%E5%B0%8F%E7%8C%AB/costumes"), result)
	})
}

func TestFormatSpxSpriteAnimationResourceContextURI(t *testing.T) {
	t.Run("ASCII", func(t *testing.T) {
		result := FormatSpxSpriteAnimationResourceContextURI("Sprite1")
		assert.Equal(t, SpxResourceContextURI("spx://resources/sprites/Sprite1/animations"), result)
	})

	t.Run("WithSpaces", func(t *testing.T) {
		result := FormatSpxSpriteAnimationResourceContextURI("my sprite")
		assert.Equal(t, SpxResourceContextURI("spx://resources/sprites/my%20sprite/animations"), result)
	})

	t.Run("NonASCII", func(t *testing.T) {
		result := FormatSpxSpriteAnimationResourceContextURI("小猫")
		assert.Equal(t, SpxResourceContextURI("spx://resources/sprites/%E5%B0%8F%E7%8C%AB/animations"), result)
	})
}

func TestParseSpxResourceURI(t *testing.T) {
	t.Run("BackdropASCII", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/backdrops/backdrop1")
		require.NoError(t, err)
		assert.Equal(t, SpxBackdropResourceID{BackdropName: "backdrop1"}, id)
	})

	t.Run("BackdropEncoded", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/backdrops/my%20backdrop")
		require.NoError(t, err)
		assert.Equal(t, SpxBackdropResourceID{BackdropName: "my backdrop"}, id)
	})

	t.Run("BackdropNonASCIIEncoded", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/backdrops/%E8%83%8C%E6%99%AF")
		require.NoError(t, err)
		assert.Equal(t, SpxBackdropResourceID{BackdropName: "背景"}, id)
	})

	t.Run("SoundASCII", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/sounds/Sound1")
		require.NoError(t, err)
		assert.Equal(t, SpxSoundResourceID{SoundName: "Sound1"}, id)
	})

	t.Run("SoundEncoded", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/sounds/my%20sound")
		require.NoError(t, err)
		assert.Equal(t, SpxSoundResourceID{SoundName: "my sound"}, id)
	})

	t.Run("SoundNonASCIIEncoded", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/sounds/%E9%9F%B3%E6%95%88")
		require.NoError(t, err)
		assert.Equal(t, SpxSoundResourceID{SoundName: "音效"}, id)
	})

	t.Run("SpriteASCII", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/sprites/Sprite1")
		require.NoError(t, err)
		assert.Equal(t, SpxSpriteResourceID{SpriteName: "Sprite1"}, id)
	})

	t.Run("SpriteEncoded", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/sprites/my%20sprite")
		require.NoError(t, err)
		assert.Equal(t, SpxSpriteResourceID{SpriteName: "my sprite"}, id)
	})

	t.Run("SpriteNonASCIIEncoded", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/sprites/%E5%B0%8F%E7%8C%AB")
		require.NoError(t, err)
		assert.Equal(t, SpxSpriteResourceID{SpriteName: "小猫"}, id)
	})

	t.Run("SpriteCostumeASCII", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/sprites/Sprite1/costumes/costume1")
		require.NoError(t, err)
		assert.Equal(t, SpxSpriteCostumeResourceID{SpriteName: "Sprite1", CostumeName: "costume1"}, id)
	})

	t.Run("SpriteCostumeEncoded", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/sprites/my%20sprite/costumes/my%20costume")
		require.NoError(t, err)
		assert.Equal(t, SpxSpriteCostumeResourceID{SpriteName: "my sprite", CostumeName: "my costume"}, id)
	})

	t.Run("SpriteCostumeNonASCIIEncoded", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/sprites/%E5%B0%8F%E7%8C%AB/costumes/%E8%B7%91%E6%AD%A5")
		require.NoError(t, err)
		assert.Equal(t, SpxSpriteCostumeResourceID{SpriteName: "小猫", CostumeName: "跑步"}, id)
	})

	t.Run("SpriteAnimationASCII", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/sprites/Sprite1/animations/anim1")
		require.NoError(t, err)
		assert.Equal(t, SpxSpriteAnimationResourceID{SpriteName: "Sprite1", AnimationName: "anim1"}, id)
	})

	t.Run("SpriteAnimationEncoded", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/sprites/my%20sprite/animations/my%20anim")
		require.NoError(t, err)
		assert.Equal(t, SpxSpriteAnimationResourceID{SpriteName: "my sprite", AnimationName: "my anim"}, id)
	})

	t.Run("SpriteAnimationNonASCIIEncoded", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/sprites/%E5%B0%8F%E7%8C%AB/animations/%E5%A5%94%E8%B7%91")
		require.NoError(t, err)
		assert.Equal(t, SpxSpriteAnimationResourceID{SpriteName: "小猫", AnimationName: "奔跑"}, id)
	})

	t.Run("WidgetASCII", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/widgets/widget1")
		require.NoError(t, err)
		assert.Equal(t, SpxWidgetResourceID{WidgetName: "widget1"}, id)
	})

	t.Run("WidgetEncoded", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/widgets/my%20widget")
		require.NoError(t, err)
		assert.Equal(t, SpxWidgetResourceID{WidgetName: "my widget"}, id)
	})

	t.Run("WidgetNonASCIIEncoded", func(t *testing.T) {
		id, err := ParseSpxResourceURI("spx://resources/widgets/%E5%88%86%E6%95%B0")
		require.NoError(t, err)
		assert.Equal(t, SpxWidgetResourceID{WidgetName: "分数"}, id)
	})

	t.Run("RoundTripBackdropASCII", func(t *testing.T) {
		original := SpxBackdropResourceID{BackdropName: "backdrop1"}
		uri := original.URI()
		parsed, err := ParseSpxResourceURI(uri)
		require.NoError(t, err)
		assert.Equal(t, original, parsed)
	})

	t.Run("RoundTripBackdropNonASCII", func(t *testing.T) {
		original := SpxBackdropResourceID{BackdropName: "背景"}
		uri := original.URI()
		parsed, err := ParseSpxResourceURI(uri)
		require.NoError(t, err)
		assert.Equal(t, original, parsed)
	})

	t.Run("RoundTripSoundWithSpaces", func(t *testing.T) {
		original := SpxSoundResourceID{SoundName: "my sound"}
		uri := original.URI()
		parsed, err := ParseSpxResourceURI(uri)
		require.NoError(t, err)
		assert.Equal(t, original, parsed)
	})

	t.Run("RoundTripSoundNonASCII", func(t *testing.T) {
		original := SpxSoundResourceID{SoundName: "音效"}
		uri := original.URI()
		parsed, err := ParseSpxResourceURI(uri)
		require.NoError(t, err)
		assert.Equal(t, original, parsed)
	})

	t.Run("RoundTripWithSpaces", func(t *testing.T) {
		original := SpxSpriteResourceID{SpriteName: "my sprite"}
		uri := original.URI()
		parsed, err := ParseSpxResourceURI(uri)
		require.NoError(t, err)
		assert.Equal(t, original, parsed)
	})

	t.Run("RoundTripNonASCII", func(t *testing.T) {
		original := SpxSpriteResourceID{SpriteName: "小猫"}
		uri := original.URI()
		parsed, err := ParseSpxResourceURI(uri)
		require.NoError(t, err)
		assert.Equal(t, original, parsed)
	})

	t.Run("RoundTripCostumeWithSpaces", func(t *testing.T) {
		original := SpxSpriteCostumeResourceID{SpriteName: "my sprite", CostumeName: "my costume"}
		uri := original.URI()
		parsed, err := ParseSpxResourceURI(uri)
		require.NoError(t, err)
		assert.Equal(t, original, parsed)
	})

	t.Run("RoundTripCostumeNonASCII", func(t *testing.T) {
		original := SpxSpriteCostumeResourceID{SpriteName: "小猫", CostumeName: "跑步"}
		uri := original.URI()
		parsed, err := ParseSpxResourceURI(uri)
		require.NoError(t, err)
		assert.Equal(t, original, parsed)
	})

	t.Run("RoundTripAnimationWithSpaces", func(t *testing.T) {
		original := SpxSpriteAnimationResourceID{SpriteName: "my sprite", AnimationName: "my anim"}
		uri := original.URI()
		parsed, err := ParseSpxResourceURI(uri)
		require.NoError(t, err)
		assert.Equal(t, original, parsed)
	})

	t.Run("RoundTripAnimationNonASCII", func(t *testing.T) {
		original := SpxSpriteAnimationResourceID{SpriteName: "小猫", AnimationName: "奔跑"}
		uri := original.URI()
		parsed, err := ParseSpxResourceURI(uri)
		require.NoError(t, err)
		assert.Equal(t, original, parsed)
	})

	t.Run("RoundTripWidgetWithSpaces", func(t *testing.T) {
		original := SpxWidgetResourceID{WidgetName: "my widget"}
		uri := original.URI()
		parsed, err := ParseSpxResourceURI(uri)
		require.NoError(t, err)
		assert.Equal(t, original, parsed)
	})

	t.Run("RoundTripWidgetNonASCII", func(t *testing.T) {
		original := SpxWidgetResourceID{WidgetName: "分数"}
		uri := original.URI()
		parsed, err := ParseSpxResourceURI(uri)
		require.NoError(t, err)
		assert.Equal(t, original, parsed)
	})

	t.Run("InvalidURI", func(t *testing.T) {
		_, err := ParseSpxResourceURI("invalid")
		assert.Error(t, err)
	})

	t.Run("WrongScheme", func(t *testing.T) {
		_, err := ParseSpxResourceURI("http://resources/sprites/Sprite1")
		assert.Error(t, err)
	})

	t.Run("WrongHost", func(t *testing.T) {
		_, err := ParseSpxResourceURI("spx://assets/sprites/Sprite1")
		assert.Error(t, err)
	})

	t.Run("UnsupportedResourceType", func(t *testing.T) {
		_, err := ParseSpxResourceURI("spx://resources/unknown/item1")
		assert.Error(t, err)
	})

	t.Run("MissingResourceName", func(t *testing.T) {
		_, err := ParseSpxResourceURI("spx://resources/backdrops")
		assert.Error(t, err)
	})

	t.Run("MalformedSpriteCostumePath", func(t *testing.T) {
		_, err := ParseSpxResourceURI("spx://resources/sprites/Sprite1/costumes")
		assert.Error(t, err)
	})

	t.Run("MalformedSpriteAnimationPath", func(t *testing.T) {
		_, err := ParseSpxResourceURI("spx://resources/sprites/Sprite1/animations")
		assert.Error(t, err)
	})
}
