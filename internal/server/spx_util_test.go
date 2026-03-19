package server

import (
	"go/token"
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolvedNamedType(t *testing.T) {
	pkg := types.NewPackage("example.com/pkg", "pkg")
	named := types.NewNamed(types.NewTypeName(token.NoPos, pkg, "Point", nil), types.NewStruct(nil, nil), nil)
	aliasToNamed := types.NewAlias(types.NewTypeName(token.NoPos, pkg, "PointAlias", nil), named)
	aliasChainToNamed := types.NewAlias(types.NewTypeName(token.NoPos, pkg, "PointAliasChain", nil), aliasToNamed)
	aliasToBasic := types.NewAlias(types.NewTypeName(token.NoPos, pkg, "StringAlias", nil), types.Typ[types.String])
	aliasToPointerNamed := types.NewAlias(types.NewTypeName(token.NoPos, pkg, "PointPtrAlias", nil), types.NewPointer(named))
	aliasChainToPointerNamed := types.NewAlias(types.NewTypeName(token.NoPos, pkg, "PointPtrAliasChain", nil), aliasToPointerNamed)

	for _, tt := range []struct {
		name string
		typ  types.Type
		want *types.Named
	}{
		{
			name: "Nil",
			typ:  nil,
			want: nil,
		},
		{
			name: "Named",
			typ:  named,
			want: named,
		},
		{
			name: "PointerToNamed",
			typ:  types.NewPointer(named),
			want: named,
		},
		{
			name: "AliasToNamed",
			typ:  aliasToNamed,
			want: named,
		},
		{
			name: "PointerToAliasToNamed",
			typ:  types.NewPointer(aliasToNamed),
			want: named,
		},
		{
			name: "AliasChainToNamed",
			typ:  aliasChainToNamed,
			want: named,
		},
		{
			name: "Basic",
			typ:  types.Typ[types.Int],
			want: nil,
		},
		{
			name: "AliasToBasic",
			typ:  aliasToBasic,
			want: nil,
		},
		{
			name: "AliasToPointerNamed",
			typ:  aliasToPointerNamed,
			want: named,
		},
		{
			name: "AliasChainToPointerNamed",
			typ:  aliasChainToPointerNamed,
			want: named,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := resolvedNamedType(tt.typ)
			if tt.want == nil {
				assert.Nil(t, got)
				return
			}
			assert.Same(t, tt.want, got)
		})
	}
}
