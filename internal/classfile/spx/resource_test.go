package spx

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseResourceURI(t *testing.T) {
	testCases := []struct {
		name    string
		uri     ResourceURI
		wantID  ResourceID
		wantErr bool
	}{
		{"Backdrop", "spx://resources/backdrops/Sky", BackdropResourceID{BackdropName: "Sky"}, false},
		{"Sprite", "spx://resources/sprites/Hero", SpriteResourceID{SpriteName: "Hero"}, false},
		{"Costume", "spx://resources/sprites/Hero/costumes/Idle", SpriteCostumeResourceID{SpriteName: "Hero", CostumeName: "Idle"}, false},
		{"Animation", "spx://resources/sprites/Hero/animations/Run", SpriteAnimationResourceID{SpriteName: "Hero", AnimationName: "Run"}, false},
		{"Sound", "spx://resources/sounds/Bell", SoundResourceID{SoundName: "Bell"}, false},
		{"Widget", "spx://resources/widgets/Start", WidgetResourceID{WidgetName: "Start"}, false},
		{"Invalid", "spx://resources/unknown/Item", nil, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			id, err := ParseResourceURI(tc.uri)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.wantID, id)
		})
	}
}

func TestResourceURIHTML(t *testing.T) {
	uri := ResourceURI("spx://resources/sounds/Bell")
	assert.Equal(t, "<resource-preview resource=\"spx://resources/sounds/Bell\" />\n", uri.HTML())
}

func TestResourceIDURI(t *testing.T) {
	testCases := []struct {
		name string
		id   ResourceID
		want ResourceURI
	}{
		{"BackdropASCII", BackdropResourceID{BackdropName: "Sky"}, "spx://resources/backdrops/Sky"},
		{"BackdropWithSpaces", BackdropResourceID{BackdropName: "Sky Blue"}, "spx://resources/backdrops/Sky%20Blue"},
		{"SpriteNonASCII", SpriteResourceID{SpriteName: "小猫"}, "spx://resources/sprites/%E5%B0%8F%E7%8C%AB"},
		{"CostumeWithSpaces", SpriteCostumeResourceID{SpriteName: "Hero", CostumeName: "Idle Loop"}, "spx://resources/sprites/Hero/costumes/Idle%20Loop"},
		{"AnimationNonASCII", SpriteAnimationResourceID{SpriteName: "Hero", AnimationName: "奔跑"}, "spx://resources/sprites/Hero/animations/%E5%A5%94%E8%B7%91"},
		{"SoundWithSpaces", SoundResourceID{SoundName: "Menu Click"}, "spx://resources/sounds/Menu%20Click"},
		{"WidgetNonASCII", WidgetResourceID{WidgetName: "分数"}, "spx://resources/widgets/%E5%88%86%E6%95%B0"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.id.URI())
		})
	}
}

func TestFormatSpriteResourceContextURI(t *testing.T) {
	t.Run("CostumeWithSpaces", func(t *testing.T) {
		assert.Equal(
			t,
			ResourceContextURI("spx://resources/sprites/Hero%20One/costumes"),
			FormatSpriteCostumeResourceContextURI("Hero One"),
		)
	})

	t.Run("AnimationNonASCII", func(t *testing.T) {
		assert.Equal(
			t,
			ResourceContextURI("spx://resources/sprites/%E5%B0%8F%E7%8C%AB/animations"),
			FormatSpriteAnimationResourceContextURI("小猫"),
		)
	})
}

func TestNewResourceSet(t *testing.T) {
	proj := newTestProject(defaultProjectFiles(), 0)

	set, err := NewResourceSet(proj, "assets")
	require.NoError(t, err)
	require.NotNil(t, set)

	assert.NotNil(t, set.Backdrop("Sky"))
	assert.NotNil(t, set.Sound("Click"))
	widget := set.Widget("StartButton")
	require.NotNil(t, widget)
	assert.Equal(t, "button", widget.Type)

	sprite := set.Sprite("Hero")
	require.NotNil(t, sprite)
	assert.Equal(t, "Hero", sprite.Name)
	assert.NotNil(t, sprite.Costume("Idle"))
	assert.NotNil(t, sprite.Animation("RunLoop"))
	require.Len(t, sprite.NormalCostumes, 1)
	assert.Equal(t, "Idle", sprite.NormalCostumes[0].Name)
}
