//go:build !test_no_pkgdata

package server

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerXGoGetPropertiesSpx(t *testing.T) {
	t.Run("ClassMembers", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx": []byte("var score int\n"),
			"MySprite.spx": []byte(`var health int

func GetDamage() int { return 10 }
`),
		}
		s := newSpxTestServer(t, files)
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		for _, tt := range []struct {
			target string
			field  string
			owner  string
		}{
			{target: "Game", field: "score", owner: "Game"},
			{target: "MySprite", field: "health", owner: "Sprite"},
		} {
			properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: tt.target})
			require.NoError(t, err)
			assert.Contains(t, properties, XGoProperty{
				Name: tt.field, Type: "int", Kind: XGoPropertyKindField,
				Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr(tt.target + "." + tt.field)},
			})
			idx := slices.IndexFunc(properties, func(property XGoProperty) bool { return property.Name == "volume" })
			require.NotEqual(t, -1, idx, "volume")
			volume := properties[idx]
			assert.Equal(t, XGoPropertyKindMethod, volume.Kind)
			assert.NotEmpty(t, volume.Doc, "%s.volume", tt.target)
			assert.Equal(t, XGoDefinitionIdentifier{Package: ToPtr(SpxPkgPath), Name: ToPtr(tt.owner + ".volume")}, volume.Definition)
			if tt.target == "MySprite" {
				assert.Contains(t, properties, XGoProperty{
					Name: "xpos", Type: "float64", Kind: XGoPropertyKindMethod,
					Definition: XGoDefinitionIdentifier{Package: ToPtr(SpxPkgPath), Name: ToPtr("Sprite.xpos")},
				})
				assert.Contains(t, properties, XGoProperty{
					Name: "getDamage", Type: "int", Kind: XGoPropertyKindMethod,
					Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("MySprite.GetDamage")},
				})
			}
		}
	})

	t.Run("ClassNameConflict", func(t *testing.T) {
		files := map[string][]byte{
			"main.spx":     []byte("var MySprite Sprite\n"),
			"MySprite.spx": []byte("var hp int\n"),
		}
		s := newSpxTestServer(t, files)
		typeInfo, err := s.workspaceRootFS.TypeInfo()
		require.ErrorContains(t, err, "MySprite conflicts with class name")
		require.NotNil(t, typeInfo)
		properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "MySprite"})
		require.NoError(t, err)
		assert.Contains(t, properties, XGoProperty{
			Name: "hp", Type: "int", Kind: XGoPropertyKindField,
			Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("MySprite.hp")},
		})
	})

	t.Run("ValueAndList", func(t *testing.T) {
		files := map[string][]byte{"main.spx": []byte(`var (
    value Value
    list List
)

func CurrentValue() Value { return value }
func CurrentList() List { return list }
`)}
		s := newSpxTestServer(t, files)
		_, err := s.workspaceRootFS.TypeInfo()
		require.NoError(t, err)
		properties, err := s.xgoGetProperties(XGoGetPropertiesParams{Target: "Game"})
		require.NoError(t, err)
		for _, want := range []XGoProperty{
			{Name: "value", Type: "Value", Kind: XGoPropertyKindField, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Game.value")}},
			{Name: "list", Type: "List", Kind: XGoPropertyKindField, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Game.list")}},
			{Name: "currentValue", Type: "Value", Kind: XGoPropertyKindMethod, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Game.CurrentValue")}},
			{Name: "currentList", Type: "List", Kind: XGoPropertyKindMethod, Definition: XGoDefinitionIdentifier{Package: ToPtr("main"), Name: ToPtr("Game.CurrentList")}},
		} {
			assert.Contains(t, properties, want)
		}
	})
}
