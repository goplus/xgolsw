package server

import (
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
)

func TestIsInSpxPkg(t *testing.T) {
	t.Run("NilObject", func(t *testing.T) {
		assert.False(t, IsInSpxPkg(nil))
	})

	for _, tt := range []struct {
		name string
		pkg  *gotypes.Package
	}{
		{name: "NoPackage"},
		{name: "OtherPackageNamedSpx", pkg: gotypes.NewPackage("example.com/spx", "spx")},
		{name: "SpxSubpackage", pkg: gotypes.NewPackage(SpxPkgPath+"/subpkg", "subpkg")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			obj := gotypes.NewTypeName(token.NoPos, tt.pkg, "Value", gotypes.Typ[gotypes.Int])
			assert.False(t, IsInSpxPkg(obj))
		})
	}
}
