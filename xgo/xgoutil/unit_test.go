/*
 * Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
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
	goast "go/ast"
	"go/constant"
	goparser "go/parser"
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUnitDecl(t *testing.T) {
	t.Run("Valid", func(t *testing.T) {
		specs := parseUnitDecl("m=1000,\u00b5s=1000,_step=2,step2=3", gotypes.Typ[gotypes.Int])

		require.Len(t, specs, 4)
		assert.Equal(t, "m", specs[0].Name)
		assert.Equal(t, "\u00b5s", specs[1].Name)
		assert.Equal(t, "_step", specs[2].Name)
		assert.Equal(t, "step2", specs[3].Name)
	})

	t.Run("InvalidName", func(t *testing.T) {
		specs := parseUnitDecl("bad`name=1,1m=2,bad-name=3,\xff=4,ok=5", gotypes.Typ[gotypes.Int])

		require.Len(t, specs, 1)
		assert.Equal(t, "ok", specs[0].Name)
	})

	t.Run("InvalidFactor", func(t *testing.T) {
		specs := parseUnitDecl("unknown=abc,empty=,ok=0.5", gotypes.Typ[gotypes.Int])

		require.Len(t, specs, 1)
		assert.Equal(t, "ok", specs[0].Name)
		assert.Equal(t, "0.5", specs[0].Factor)
	})
}

func TestUnitSpecsForType(t *testing.T) {
	pkg := gotypes.NewPackage("example.com/units", "units")
	obj := gotypes.NewTypeName(token.NoPos, pkg, "Distance", nil)
	typ := gotypes.NewNamed(obj, gotypes.Typ[gotypes.Int], nil)
	pkg.Scope().Insert(obj)
	pkg.Scope().Insert(gotypes.NewConst(token.NoPos, pkg, "XGou_Distance", gotypes.Typ[gotypes.UntypedString],
		constant.MakeString("m=1,cm=2,mm=3,m=10,cm=invalid")))

	want := UnitSpec{Name: "m", Factor: "10", SourceType: typ}
	assert.Equal(t, []UnitSpec{
		want,
		{Name: "mm", Factor: "3", SourceType: typ},
	}, UnitSpecsForType(typ))
	spec, ok := UnitSpecForType(typ, "m")
	require.True(t, ok)
	assert.Equal(t, want, spec)
	_, ok = UnitSpecForType(typ, "cm")
	assert.False(t, ok)
}

func TestUnitTableForType(t *testing.T) {
	t.Run("MainPackageAliasOfImportedType", func(t *testing.T) {
		timePkg := gotypes.NewPackage("time", "time")
		duration := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, timePkg, "Duration", nil), gotypes.Typ[gotypes.Int64], nil)
		mainPkg := gotypes.NewPackage("main", "main")
		alias := gotypes.NewAlias(gotypes.NewTypeName(token.NoPos, mainPkg, "Delay", nil), duration)

		_, found := UnitSpecForType(alias, "ms")
		assert.False(t, found)
		assert.Empty(t, UnitSpecsForType(alias))
	})

	const distance = "type Distance int\nconst XGou_Distance = \"m=100\"\n"
	for _, tt := range []struct {
		name, path, typ, declarations string
		unit, factor, owner           string
	}{
		{name: "Builtin", path: "time", typ: "Duration", declarations: "type Duration int64",
			unit: "ms", factor: "1000000", owner: "Duration"},
		{name: "AliasTable", path: "example.com/units", typ: "Alias",
			declarations: distance + "type Alias = Distance\nconst XGou_Alias = \"m=-1\"",
			unit:         "m", factor: "-1", owner: "Alias"},
		{name: "AliasFallback", path: "example.com/units", typ: "Alias",
			declarations: distance + "type Alias = Distance", unit: "m", factor: "100", owner: "Distance"},
		{name: "ScalarAlias", path: "example.com/units", typ: "Alias", declarations: "type Alias = int", unit: "m"},
		{name: "MissingTable", path: "example.com/units", typ: "Distance", declarations: "type Distance int", unit: "m"},
		{name: "NonStringTable", path: "example.com/units", typ: "Distance",
			declarations: "type Distance int\nconst XGou_Distance = 1", unit: "m"},
		{name: "MainPackage", path: "main", typ: "Distance", declarations: distance, unit: "m"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := goparser.ParseFile(fset, "units.go", "package units\n"+tt.declarations+"\n", 0)
			require.NoError(t, err)
			pkg, err := new(gotypes.Config).Check(tt.path, fset, []*goast.File{file}, nil)
			require.NoError(t, err)

			typ := pkg.Scope().Lookup(tt.typ).Type()
			spec, found := UnitSpecForType(typ, tt.unit)
			if tt.owner == "" {
				assert.False(t, found)
				assert.Empty(t, UnitSpecsForType(typ))
				return
			}
			require.True(t, found)
			want := UnitSpec{Name: tt.unit, Factor: tt.factor, SourceType: pkg.Scope().Lookup(tt.owner).Type()}
			assert.Equal(t, want, spec)
			assert.Contains(t, UnitSpecsForType(typ), want)
		})
	}
}
