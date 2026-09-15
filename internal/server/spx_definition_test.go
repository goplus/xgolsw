//go:build !test_no_pkgdata

package server

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
)

func TestCanonicalSpxResourceNameType(t *testing.T) {
	pkg := gotypes.NewPackage("example.com/pkg", "pkg")
	soundAlias := gotypes.NewAlias(
		gotypes.NewTypeName(token.NoPos, pkg, "MySoundName", nil),
		GetSpxSoundNameType(),
	)
	soundAliasChain := gotypes.NewAlias(
		gotypes.NewTypeName(token.NoPos, pkg, "MySoundNameChain", nil),
		soundAlias,
	)

	for _, tt := range []struct {
		name string
		typ  gotypes.Type
		want gotypes.Type
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
			typ:  gotypes.Typ[gotypes.String],
			want: nil,
		},
		{
			name: "AliasToBasicString",
			typ:  gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, pkg, "MyString", nil), gotypes.Typ[gotypes.String]),
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
