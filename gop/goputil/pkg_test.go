package goputil

import (
	"go/constant"
	"go/types"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsMarkedAsGopPackage(t *testing.T) {
	t.Run("NilPackage", func(t *testing.T) {
		assert.False(t, IsMarkedAsGopPackage(nil))
	})

	t.Run("PackageWithEmptyScope", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		assert.False(t, IsMarkedAsGopPackage(pkg))
	})

	t.Run("PackageWithoutGopPackageMarker", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		assert.False(t, IsMarkedAsGopPackage(pkg))
	})

	t.Run("PackageWithGopPackageMarker", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		scope := pkg.Scope()
		scope.Insert(types.NewConst(0, pkg, GopPackage, types.Typ[types.UntypedBool], constant.MakeBool(true)))
		assert.True(t, IsMarkedAsGopPackage(pkg))
	})

	t.Run("PackageWithWrongTypeGopPackageMarker", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		scope := pkg.Scope()
		scope.Insert(types.NewConst(0, pkg, GopPackage, types.Typ[types.Int], constant.MakeInt64(1)))
		assert.False(t, IsMarkedAsGopPackage(pkg))
	})

	t.Run("PackageWithWrongValueGopPackageMarker", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		scope := pkg.Scope()
		scope.Insert(types.NewConst(0, pkg, GopPackage, types.Typ[types.UntypedBool], constant.MakeBool(false)))
		assert.False(t, IsMarkedAsGopPackage(pkg))
	})

	t.Run("PackageWithNonConstGopPackageMarker", func(t *testing.T) {
		pkg := types.NewPackage("test", "test")
		scope := pkg.Scope()
		scope.Insert(types.NewVar(0, pkg, GopPackage, types.Typ[types.UntypedBool]))
		assert.False(t, IsMarkedAsGopPackage(pkg))
	})
}

func TestPkgPath(t *testing.T) {
	t.Run("NilPackage", func(t *testing.T) {
		assert.Equal(t, "builtin", PkgPath(nil))
	})

	t.Run("PackageWithEmptyPath", func(t *testing.T) {
		pkg := types.NewPackage("", "builtin")
		assert.Equal(t, "builtin", PkgPath(pkg))
	})

	t.Run("PackageWithValidPath", func(t *testing.T) {
		pkg := types.NewPackage("example.com/pkg", "pkg")
		assert.Equal(t, "example.com/pkg", PkgPath(pkg))
	})

	t.Run("MainPackage", func(t *testing.T) {
		pkg := types.NewPackage("main", "main")
		assert.Equal(t, "main", PkgPath(pkg))
	})

	t.Run("StandardLibraryPackage", func(t *testing.T) {
		pkg := types.NewPackage("fmt", "fmt")
		assert.Equal(t, "fmt", PkgPath(pkg))
	})

	t.Run("NestedPackagePath", func(t *testing.T) {
		pkg := types.NewPackage("example.com/deep/nested/pkg", "pkg")
		assert.Equal(t, "example.com/deep/nested/pkg", PkgPath(pkg))
	})
}

func TestIsBuiltinPkg(t *testing.T) {
	t.Run("NilPackage", func(t *testing.T) {
		assert.True(t, IsBuiltinPkg(nil))
	})

	t.Run("BuiltinPackageWithEmptyPath", func(t *testing.T) {
		pkg := types.NewPackage("", "builtin")
		assert.True(t, IsBuiltinPkg(pkg))
	})

	t.Run("NonBuiltinPackage", func(t *testing.T) {
		pkg := types.NewPackage("fmt", "fmt")
		assert.False(t, IsBuiltinPkg(pkg))
	})

	t.Run("MainPackage", func(t *testing.T) {
		pkg := types.NewPackage("main", "main")
		assert.False(t, IsBuiltinPkg(pkg))
	})

	t.Run("ThirdPartyPackage", func(t *testing.T) {
		pkg := types.NewPackage("example.com/pkg", "pkg")
		assert.False(t, IsBuiltinPkg(pkg))
	})
}

func TestIsMainPkg(t *testing.T) {
	t.Run("NilPackage", func(t *testing.T) {
		assert.False(t, IsMainPkg(nil))
	})

	t.Run("MainPackage", func(t *testing.T) {
		pkg := types.NewPackage("main", "main")
		assert.True(t, IsMainPkg(pkg))
	})

	t.Run("NonMainPackage", func(t *testing.T) {
		pkg := types.NewPackage("fmt", "fmt")
		assert.False(t, IsMainPkg(pkg))
	})

	t.Run("BuiltinPackage", func(t *testing.T) {
		pkg := types.NewPackage("", "builtin")
		assert.False(t, IsMainPkg(pkg))
	})

	t.Run("ThirdPartyPackage", func(t *testing.T) {
		pkg := types.NewPackage("example.com/pkg", "pkg")
		assert.False(t, IsMainPkg(pkg))
	})

	t.Run("PackageNamedMainButDifferentPath", func(t *testing.T) {
		pkg := types.NewPackage("example.com/main", "main")
		assert.False(t, IsMainPkg(pkg))
	})
}
