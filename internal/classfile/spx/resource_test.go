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
