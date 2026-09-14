package server

import (
	"encoding/json"
	"io/fs"
	"testing"

	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewSpxResourceSet(t *testing.T) {
	t.Run("Resources", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"assets/index.json": []byte(`{
				"backdrops":[{"name":"Studio","path":"studio.png"},{"name":"Park","path":"park.png"}],
				"zorder":["Runner",null,42,{}, {"name":42},
					{"name":"Score","type":"monitor","label":"Points","val":"score"},
					{"name":"Timer","type":"monitor","val":"timer"}]
			}`),
			"assets/sounds/Beep/index.json": []byte(`{"name":"Ignored","path":"beep.wav"}`),
			"assets/sounds/Pop/index.json":  []byte(`{"path":"pop.wav"}`),
			"assets/sprites/Runner/index.json": []byte(`{
				"costumes":[{"name":"idle","path":"idle.png"},{"name":"step","path":"step.png"}],
				"costumeIndex":1,"fAnimations":{"walk":{"frameFrom":"step","frameTo":"step"}},
				"defaultAnimation":"walk"
			}`),
			"assets/sprites/Other/index.json": []byte(`{}`),
		})
		set, err := NewSpxResourceSet(s.getProj())
		require.NoError(t, err)
		assert.Equal(t, map[string]*SpxBackdropResource{
			"Studio": {ID: SpxBackdropResourceID{BackdropName: "Studio"}, Name: "Studio", Path: "studio.png"},
			"Park":   {ID: SpxBackdropResourceID{BackdropName: "Park"}, Name: "Park", Path: "park.png"},
		}, set.backdrops)
		assert.Equal(t, map[string]*SpxSoundResource{
			"Beep": {ID: SpxSoundResourceID{SoundName: "Beep"}, Name: "Beep", Path: "beep.wav"},
			"Pop":  {ID: SpxSoundResourceID{SoundName: "Pop"}, Name: "Pop", Path: "pop.wav"},
		}, set.sounds)
		assert.Equal(t, map[string]*SpxWidgetResource{
			"Score": {ID: SpxWidgetResourceID{WidgetName: "Score"}, Name: "Score", Type: "monitor", Label: "Points", Val: "score"},
			"Timer": {ID: SpxWidgetResourceID{WidgetName: "Timer"}, Name: "Timer", Type: "monitor", Val: "timer"},
		}, set.widgets)
		require.Len(t, set.sprites, 2)
		sprite := set.Sprite("Runner")
		require.NotNil(t, sprite)
		assert.Equal(t, SpxSpriteResourceID{SpriteName: "Runner"}, sprite.ID)
		assert.Equal(t, "Runner", sprite.Name)
		assert.Equal(t, 1, sprite.CostumeIndex)
		assert.Equal(t, "walk", sprite.DefaultAnimation)
		assert.Equal(t, []SpxSpriteCostumeResource{
			{ID: SpxSpriteCostumeResourceID{SpriteName: "Runner", CostumeName: "idle"}, Name: "idle", Path: "idle.png"},
			{ID: SpxSpriteCostumeResourceID{SpriteName: "Runner", CostumeName: "step"}, Name: "step", Path: "step.png"},
		}, sprite.Costumes)
		require.Len(t, sprite.Costumes, 2)
		assert.Equal(t, sprite.Costumes[:1], sprite.NormalCostumes)
		assert.Same(t, &sprite.Costumes[1], sprite.Costume("step"))
		assert.Equal(t, &SpxSpriteAnimationResource{
			ID:   SpxSpriteAnimationResourceID{SpriteName: "Runner", AnimationName: "walk"},
			Name: "walk", FromIndex: ToPtr(1), ToIndex: ToPtr(1),
		}, sprite.Animation("walk"))
		other := set.Sprite("Other")
		require.NotNil(t, other)
		assert.Empty(t, other.Costumes)
		assert.Empty(t, other.NormalCostumes)
		assert.Empty(t, other.Animations)
		assert.Nil(t, sprite.Costume("missing"))
		assert.Nil(t, sprite.Animation("missing"))
		assert.Nil(t, other.Costume("step"))
		assert.Nil(t, other.Animation("walk"))

		for _, id := range []SpxResourceID{
			SpxBackdropResourceID{BackdropName: "Studio"},
			SpxSoundResourceID{SoundName: "Beep"},
			SpxSpriteResourceID{SpriteName: "Runner"},
			SpxSpriteCostumeResourceID{SpriteName: "Runner", CostumeName: "step"},
			SpxSpriteAnimationResourceID{SpriteName: "Runner", AnimationName: "walk"},
			SpxWidgetResourceID{WidgetName: "Score"},
		} {
			assert.True(t, set.Contains(id), "%s", id.URI())
		}
		for _, id := range []SpxResourceID{
			SpxBackdropResourceID{BackdropName: "Beep"},
			SpxSoundResourceID{SoundName: "Studio"},
			SpxSpriteResourceID{SpriteName: "missing"},
			SpxSpriteCostumeResourceID{SpriteName: "Runner", CostumeName: "missing"},
			SpxSpriteCostumeResourceID{SpriteName: "Other", CostumeName: "step"},
			SpxSpriteCostumeResourceID{SpriteName: "missing", CostumeName: "step"},
			SpxSpriteAnimationResourceID{SpriteName: "Runner", AnimationName: "missing"},
			SpxSpriteAnimationResourceID{SpriteName: "Other", AnimationName: "walk"},
			SpxSpriteAnimationResourceID{SpriteName: "missing", AnimationName: "walk"},
			SpxWidgetResourceID{WidgetName: "Runner"},
		} {
			assert.False(t, set.Contains(id), "%s", id.URI())
		}
		assert.False(t, set.Contains(nil))
	})

	t.Run("Empty", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{"assets/index.json": []byte(`{}`)})
		set, err := NewSpxResourceSet(s.getProj())
		require.NoError(t, err)
		assert.Empty(t, set.backdrops)
		assert.Empty(t, set.sounds)
		assert.Empty(t, set.sprites)
		assert.Empty(t, set.widgets)
		assert.Nil(t, set.Backdrop("missing"))
		assert.Nil(t, set.Sound("missing"))
		assert.Nil(t, set.Sprite("missing"))
		assert.Nil(t, set.Widget("missing"))
	})

	t.Run("Animations", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			animations string
			wantFrom   *int
			wantTo     *int
			wantNormal []string
		}{
			{
				name: "Range", animations: `{"walk":{"frameFrom":"b","frameTo":"c"}}`,
				wantFrom: ToPtr(1), wantTo: ToPtr(2), wantNormal: []string{"a", "d"},
			},
			{
				name: "SingleFrame", animations: `{"walk":{"frameFrom":"a","frameTo":"a"}}`,
				wantFrom: ToPtr(0), wantTo: ToPtr(0), wantNormal: []string{"b", "c", "d"},
			},
			{
				name:       "OverlappingRanges",
				animations: `{"walk":{"frameFrom":"a","frameTo":"c"},"run":{"frameFrom":"b","frameTo":"d"}}`,
				wantFrom:   ToPtr(0), wantTo: ToPtr(2),
			},
			{
				name: "MissingFrom", animations: `{"walk":{"frameFrom":"missing","frameTo":"c"}}`,
				wantTo: ToPtr(2), wantNormal: []string{"a", "b", "c", "d"},
			},
			{
				name: "MissingTo", animations: `{"walk":{"frameFrom":"b","frameTo":"missing"}}`,
				wantFrom: ToPtr(1), wantNormal: []string{"a", "b", "c", "d"},
			},
			{
				name: "MissingBoth", animations: `{"walk":{}}`,
				wantNormal: []string{"a", "b", "c", "d"},
			},
			{
				name: "ReversedRange", animations: `{"walk":{"frameFrom":"c","frameTo":"b"}}`,
				wantFrom: ToPtr(2), wantTo: ToPtr(1), wantNormal: []string{"a", "b", "c", "d"},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, map[string][]byte{
					"assets/index.json":                []byte(`{}`),
					"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"a"},{"name":"b"},{"name":"c"},{"name":"d"}],"fAnimations":` + tt.animations + `}`),
				})
				set, err := NewSpxResourceSet(s.getProj())
				require.NoError(t, err)
				sprite := set.Sprite("Runner")
				require.NotNil(t, sprite)
				assert.Equal(t, &SpxSpriteAnimationResource{
					ID:   SpxSpriteAnimationResourceID{SpriteName: "Runner", AnimationName: "walk"},
					Name: "walk", FromIndex: tt.wantFrom, ToIndex: tt.wantTo,
				}, sprite.Animation("walk"))
				var normal []string
				for _, costume := range sprite.NormalCostumes {
					normal = append(normal, costume.Name)
				}
				assert.Equal(t, tt.wantNormal, normal)
			})
		}
	})

	t.Run("MetadataErrors", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			files   map[string][]byte
			message string
			missing bool
		}{
			{
				name: "MissingRoot", message: "failed to read metadata", missing: true,
			},
			{
				name: "InvalidRoot", message: "failed to parse metadata",
				files: map[string][]byte{"assets/index.json": []byte(`{`)},
			},
			{
				name: "MissingSound", message: "failed to read sound metadata", missing: true,
				files: map[string][]byte{"assets/index.json": []byte(`{}`), "assets/sounds/Beep/beep.wav": nil},
			},
			{
				name: "InvalidSound", message: "failed to parse sound metadata",
				files: map[string][]byte{"assets/index.json": []byte(`{}`), "assets/sounds/Beep/index.json": []byte(`{`)},
			},
			{
				name: "MissingSprite", message: "failed to read sprite metadata", missing: true,
				files: map[string][]byte{"assets/index.json": []byte(`{}`), "assets/sprites/Runner/costumes/idle.png": nil},
			},
			{
				name: "InvalidSprite", message: "failed to parse sprite metadata",
				files: map[string][]byte{"assets/index.json": []byte(`{}`), "assets/sprites/Runner/index.json": []byte(`{`)},
			},
		} {
			t.Run(tt.name, func(t *testing.T) {
				s := newTestServer(t, tt.files)
				set, err := NewSpxResourceSet(s.getProj())
				require.ErrorContains(t, err, tt.message)
				assert.Nil(t, set)
				if tt.missing {
					assert.ErrorIs(t, err, fs.ErrNotExist)
				} else {
					var syntaxError *json.SyntaxError
					assert.ErrorAs(t, err, &syntaxError)
				}
			})
		}
	})

	t.Run("IndependentLoads", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"assets/index.json":                []byte(`{"backdrops":[{"name":"Studio"}]}`),
			"assets/sprites/Runner/index.json": []byte(`{"costumes":[{"name":"idle"}],"fAnimations":{"rest":{"frameFrom":"idle","frameTo":"idle"}}}`),
		})
		proj := s.getProj()
		first, err := NewSpxResourceSet(proj)
		require.NoError(t, err)
		second, err := NewSpxResourceSet(proj)
		require.NoError(t, err)
		require.Equal(t, first, second)
		for _, set := range []*SpxResourceSet{first, second} {
			require.NotNil(t, set.Backdrop("Studio"))
			sprite := set.Sprite("Runner")
			require.NotNil(t, sprite)
			require.Len(t, sprite.Costumes, 1)
			animation := sprite.Animation("rest")
			require.NotNil(t, animation)
			require.NotNil(t, animation.FromIndex)
		}
		first.Backdrop("Studio").Name = "changed"
		first.Sprite("Runner").Costumes[0].Name = "changed"
		*first.Sprite("Runner").Animation("rest").FromIndex = 10
		assert.Equal(t, "Studio", second.Backdrop("Studio").Name)
		assert.Equal(t, "idle", second.Sprite("Runner").Costumes[0].Name)
		assert.Equal(t, ToPtr(0), second.Sprite("Runner").Animation("rest").FromIndex)

		proj.PutFile("assets/index.json", &xgo.File{Content: []byte(`{}`)})
		require.NoError(t, proj.DeleteFile("assets/sprites/Runner/index.json"))
		proj.PutFile("assets/sounds/Pop/index.json", &xgo.File{Content: []byte(`{"path":"pop.wav"}`)})
		third, err := NewSpxResourceSet(proj)
		require.NoError(t, err)
		assert.Nil(t, third.Backdrop("Studio"))
		assert.Nil(t, third.Sprite("Runner"))
		assert.NotNil(t, third.Sound("Pop"))
		assert.NotNil(t, second.Backdrop("Studio"))
		assert.NotNil(t, second.Sprite("Runner"))
		assert.Nil(t, second.Sound("Pop"))
	})
}

func TestSpxResourceSetContains(t *testing.T) {
	t.Run("ZeroValue", func(t *testing.T) {
		var set SpxResourceSet
		for _, id := range []SpxResourceID{
			SpxBackdropResourceID{BackdropName: "Studio"},
			SpxSoundResourceID{SoundName: "Beep"},
			SpxSpriteResourceID{SpriteName: "Runner"},
			SpxSpriteCostumeResourceID{SpriteName: "Runner", CostumeName: "idle"},
			SpxSpriteAnimationResourceID{SpriteName: "Runner", AnimationName: "walk"},
			SpxWidgetResourceID{WidgetName: "Score"},
		} {
			assert.False(t, set.Contains(id), "%s", id.URI())
		}
	})
}

func TestListSubdirs(t *testing.T) {
	for _, dir := range []string{"assets/sprites", "assets/sprites/", "assets/./sprites"} {
		s := newTestServer(t, map[string][]byte{
			"assets/sprites/Runner/index.json":        nil,
			"assets/sprites/Runner/costumes/idle.png": nil,
			"assets/sprites/Other/index.json":         nil,
			"assets/sprites/index.json":               nil,
			"assets/sprites-extra/Third/index.json":   nil,
			"other/assets/sprites/Fourth/index.json":  nil,
		})
		assert.Equal(t, []string{"Other", "Runner"}, listSubdirs(s.getProj(), dir), "%s", dir)
		assert.Empty(t, listSubdirs(s.getProj(), "missing"))
	}
}

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
