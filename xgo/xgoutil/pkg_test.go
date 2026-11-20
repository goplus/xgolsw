/*
 * Copyright (c) 2025 The XGo Authors (xgo.dev). All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package xgoutil

import (
	"go/constant"
	gotypes "go/types"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsMarkedAsXGoPackage(t *testing.T) {
	t.Run("NilPackage", func(t *testing.T) {
		assert.False(t, IsMarkedAsXGoPackage(nil))
	})

	t.Run("PackageWithNilScope", func(t *testing.T) {
		pkg := new(gotypes.Package)
		assert.False(t, IsMarkedAsXGoPackage(pkg))
	})

	t.Run("PackageWithEmptyScope", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		assert.False(t, IsMarkedAsXGoPackage(pkg))
	})

	t.Run("PackageWithoutXGoPackageMarker", func(t *testing.T) {
		pkg := gotypes.NewPackage("test", "test")
		assert.False(t, IsMarkedAsXGoPackage(pkg))
	})

	for _, tt := range []struct {
		name   string
		marker string
		typ    gotypes.Type
		val    constant.Value
		want   bool
	}{
		{name: "XGoPackageMarker", marker: XGoPackage, typ: gotypes.Typ[gotypes.UntypedBool], val: constant.MakeBool(true), want: true},
		{name: "LegacyGopPackageMarker", marker: GopPackage, typ: gotypes.Typ[gotypes.UntypedBool], val: constant.MakeBool(true), want: true},
		{name: "IntXGoPackageMarker", marker: XGoPackage, typ: gotypes.Typ[gotypes.Int], val: constant.MakeInt64(1), want: true},
		{name: "IntGopPackageMarker", marker: GopPackage, typ: gotypes.Typ[gotypes.Int], val: constant.MakeInt64(1), want: true},
		{name: "FalseXGoPackageMarker", marker: XGoPackage, typ: gotypes.Typ[gotypes.UntypedBool], val: constant.MakeBool(false), want: true},
		{name: "FalseGopPackageMarker", marker: GopPackage, typ: gotypes.Typ[gotypes.UntypedBool], val: constant.MakeBool(false), want: true},
		{name: "NilValueXGoPackageMarker", marker: XGoPackage, typ: gotypes.Typ[gotypes.UntypedBool]},
		{name: "NilValueGopPackageMarker", marker: GopPackage, typ: gotypes.Typ[gotypes.UntypedBool]},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := gotypes.NewPackage("test", "test")
			pkg.Scope().Insert(gotypes.NewConst(0, pkg, tt.marker, tt.typ, tt.val))
			assert.Equal(t, tt.want, IsMarkedAsXGoPackage(pkg))
		})
	}

	for _, marker := range []string{XGoPackage, GopPackage} {
		t.Run("NonConst"+marker+"Marker", func(t *testing.T) {
			pkg := gotypes.NewPackage("test", "test")
			pkg.Scope().Insert(gotypes.NewVar(0, pkg, marker, gotypes.Typ[gotypes.UntypedBool]))
			assert.False(t, IsMarkedAsXGoPackage(pkg))
		})
	}
}

func TestXGoPackageDependencies(t *testing.T) {
	for _, tt := range []struct {
		name    string
		current constant.Value
		want    []string
		marked  bool
	}{
		{name: "Historical", want: []string{"legacy.example/pkg"}, marked: true},
		{name: "Current", current: constant.MakeString("example.com/one,example.com/two"),
			want: []string{"example.com/one", "example.com/two"}, marked: true},
		{name: "BooleanWithHistorical", current: constant.MakeBool(true), marked: true},
		{name: "NonConstantWithHistorical"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pkg := gotypes.NewPackage("test", "test")
			scope := pkg.Scope()
			scope.Insert(gotypes.NewConst(0, pkg, GopPackage, gotypes.Typ[gotypes.String], constant.MakeString("legacy.example/pkg")))
			if tt.current != nil {
				typ := gotypes.Typ[gotypes.String]
				if tt.current.Kind() == constant.Bool {
					typ = gotypes.Typ[gotypes.Bool]
				}
				scope.Insert(gotypes.NewConst(0, pkg, XGoPackage, typ, tt.current))
			} else if !tt.marked {
				scope.Insert(gotypes.NewVar(0, pkg, XGoPackage, gotypes.Typ[gotypes.String]))
			}

			assert.Equal(t, tt.marked, IsMarkedAsXGoPackage(pkg))
			assert.Equal(t, tt.want, slices.Collect(XGoPackageDependencies(pkg)))
		})
	}
}

func TestIsXGoPackageMarkerName(t *testing.T) {
	t.Run("XGoPackageMarker", func(t *testing.T) {
		assert.True(t, IsXGoPackageMarkerName(XGoPackage))
	})

	t.Run("LegacyGopPackageMarker", func(t *testing.T) {
		assert.True(t, IsXGoPackageMarkerName(GopPackage))
	})

	t.Run("OtherName", func(t *testing.T) {
		assert.False(t, IsXGoPackageMarkerName("OtherPackage"))
	})
}

func TestIsXGoInternalName(t *testing.T) {
	for _, tt := range []struct {
		name string
		want bool
	}{
		{name: XGoPackage, want: true},
		{name: GopPackage, want: true},
		{name: "XGo_Init", want: true},
		{name: "Gop_Init", want: true},
		{name: "XGoo_Game_Label", want: true},
		{name: "__xgo_optional_arg", want: true},
		{name: "__gop_optional_arg", want: true},
		{name: "XGot_Game_Main", want: false},
		{name: "normalName", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, IsXGoInternalName(tt.name))
		})
	}
}

func TestPkgPath(t *testing.T) {
	t.Run("NilPackage", func(t *testing.T) {
		assert.Equal(t, "builtin", PkgPath(nil))
	})

	t.Run("PackageWithEmptyPath", func(t *testing.T) {
		pkg := gotypes.NewPackage("", "builtin")
		assert.Equal(t, "builtin", PkgPath(pkg))
	})

	t.Run("PackageWithValidPath", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/pkg", "pkg")
		assert.Equal(t, "example.com/pkg", PkgPath(pkg))
	})

	t.Run("MainPackage", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		assert.Equal(t, "main", PkgPath(pkg))
	})

	t.Run("StandardLibraryPackage", func(t *testing.T) {
		pkg := gotypes.NewPackage("fmt", "fmt")
		assert.Equal(t, "fmt", PkgPath(pkg))
	})

	t.Run("NestedPkgPath", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/deep/nested/pkg", "pkg")
		assert.Equal(t, "example.com/deep/nested/pkg", PkgPath(pkg))
	})
}

func TestIsBuiltinPkg(t *testing.T) {
	t.Run("NilPackage", func(t *testing.T) {
		assert.True(t, IsBuiltinPkg(nil))
	})

	t.Run("BuiltinPackageWithEmptyPath", func(t *testing.T) {
		pkg := gotypes.NewPackage("", "builtin")
		assert.True(t, IsBuiltinPkg(pkg))
	})

	t.Run("NonBuiltinPackage", func(t *testing.T) {
		pkg := gotypes.NewPackage("fmt", "fmt")
		assert.False(t, IsBuiltinPkg(pkg))
	})

	t.Run("MainPackage", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		assert.False(t, IsBuiltinPkg(pkg))
	})

	t.Run("ThirdPartyPackage", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/pkg", "pkg")
		assert.False(t, IsBuiltinPkg(pkg))
	})
}

func TestIsMainPkg(t *testing.T) {
	t.Run("NilPackage", func(t *testing.T) {
		assert.False(t, IsMainPkg(nil))
	})

	t.Run("MainPackage", func(t *testing.T) {
		pkg := gotypes.NewPackage("main", "main")
		assert.True(t, IsMainPkg(pkg))
	})

	t.Run("NonMainPackage", func(t *testing.T) {
		pkg := gotypes.NewPackage("fmt", "fmt")
		assert.False(t, IsMainPkg(pkg))
	})

	t.Run("BuiltinPackage", func(t *testing.T) {
		pkg := gotypes.NewPackage("", "builtin")
		assert.False(t, IsMainPkg(pkg))
	})

	t.Run("ThirdPartyPackage", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/pkg", "pkg")
		assert.False(t, IsMainPkg(pkg))
	})

	t.Run("PackageNamedMainButDifferentPath", func(t *testing.T) {
		pkg := gotypes.NewPackage("example.com/main", "main")
		assert.False(t, IsMainPkg(pkg))
	})
}
