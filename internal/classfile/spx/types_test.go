package spx

import (
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsResourceNameType(t *testing.T) {
	spriteName := SpriteNameType()
	require.NotNil(t, spriteName)
	assert.True(t, IsResourceNameType(spriteName))
	assert.False(t, IsResourceNameType(types.Typ[types.String]))
}
