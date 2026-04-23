package server

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCanonicalSpxResourceNameType(t *testing.T) {
	pkg := types.NewPackage("example.com/pkg", "pkg")
	soundAlias := types.NewAlias(
		types.NewTypeName(token.NoPos, pkg, "MySoundName", nil),
		GetSpxSoundNameType(),
	)
	soundAliasChain := types.NewAlias(
		types.NewTypeName(token.NoPos, pkg, "MySoundNameChain", nil),
		soundAlias,
	)

	for _, tt := range []struct {
		name string
		typ  types.Type
		want types.Type
	}{
		{
			name: "Nil",
			typ:  nil,
			want: nil,
		},
		{
			name: "DirectBackdropName",
			typ:  GetSpxBackdropNameType(),
			want: GetSpxBackdropNameType(),
		},
		{
			name: "AliasToSoundName",
			typ:  soundAlias,
			want: GetSpxSoundNameType(),
		},
		{
			name: "AliasChainToSoundName",
			typ:  soundAliasChain,
			want: GetSpxSoundNameType(),
		},
		{
			name: "BasicString",
			typ:  types.Typ[types.String],
			want: nil,
		},
		{
			name: "AliasToBasicString",
			typ:  types.NewAlias(types.NewTypeName(token.NoPos, pkg, "MyString", nil), types.Typ[types.String]),
			want: nil,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalSpxResourceNameType(tt.typ)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Same(t, tt.want, got)
		})
	}
}
