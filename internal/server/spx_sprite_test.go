package server

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsSpxSpriteSurfaceType(t *testing.T) {
	newNamed := func(pkg *types.Package, name string) *types.Named {
		return types.NewNamed(types.NewTypeName(token.NoPos, pkg, name, nil), types.NewStruct(nil, nil), nil)
	}

	t.Run("DirectSpriteType", func(t *testing.T) {
		assert.True(t, isSpxSpriteSurfaceType(GetSpxSpriteType()))
	})

	t.Run("IndirectEmbeddedSpriteType", func(t *testing.T) {
		pkg := types.NewPackage("example.com/test", "test")
		base := newNamed(pkg, "Base")
		base.SetUnderlying(types.NewStruct([]*types.Var{
			types.NewField(token.NoPos, pkg, "Sprite", GetSpxSpriteType(), true),
		}, nil))

		hero := newNamed(pkg, "Hero")
		hero.SetUnderlying(types.NewStruct([]*types.Var{
			types.NewField(token.NoPos, pkg, "Base", base, true),
		}, nil))

		assert.True(t, isSpxSpriteSurfaceType(hero))
	})

	t.Run("CyclicEmbedding", func(t *testing.T) {
		pkg := types.NewPackage("example.com/test", "test")
		cycle := newNamed(pkg, "Cycle")
		cycle.SetUnderlying(types.NewStruct([]*types.Var{
			types.NewField(token.NoPos, pkg, "Cycle", types.NewPointer(cycle), true),
		}, nil))

		assert.False(t, isSpxSpriteSurfaceType(cycle))
	})
}
