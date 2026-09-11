//go:build !test_no_pkgdata

package server

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
)

func TestIsInSpxPkgIdentity(t *testing.T) {
	for _, tt := range []struct {
		name string
		pkg  *gotypes.Package
		want bool
	}{
		{name: "SpxPackage", pkg: GetSpxPkg(), want: true},
		{name: "DistinctSpxPackage", pkg: gotypes.NewPackage(SpxPkgPath, "spx")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			obj := gotypes.NewTypeName(token.NoPos, tt.pkg, "Value", gotypes.Typ[gotypes.Int])
			assert.Equal(t, tt.want, IsInSpxPkg(obj))
		})
	}
}

func TestGetSimplifiedTypeStringSpx(t *testing.T) {
	for _, tt := range []struct {
		name string
		pkg  *gotypes.Package
		want string
	}{
		{name: "SpxPackage", pkg: GetSpxPkg(), want: "Value"},
		{name: "DistinctSpxPackage", pkg: gotypes.NewPackage(SpxPkgPath, "spx"), want: "spx.Value"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			named := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, tt.pkg, "Value", nil), gotypes.NewStruct(nil, nil), nil)
			assert.Equal(t, tt.want, GetSimplifiedTypeString(named))
		})
	}
}
