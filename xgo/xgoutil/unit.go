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
	"go/constant"
	gotoken "go/token"
	gotypes "go/types"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// timeDurationUnitDecl is the built-in unit declaration for time.Duration.
const timeDurationUnitDecl = "ns=1,us=1000,\u00b5s=1000,ms=1000000,s=1000000000,m=60000000000,h=3600000000000,d=86400000000000"

// UnitSpec describes one unit suffix available for an XGo unit literal.
type UnitSpec struct {
	Name       string
	Factor     string
	SourceType gotypes.Type
}

// UnitSpecsForType returns the units registered for typ, preferring an alias's
// table when it has one.
func UnitSpecsForType(typ gotypes.Type) []UnitSpec {
	decl, source := unitTableForType(typ)
	if source == nil {
		return nil
	}
	return parseUnitDecl(decl, source)
}

// UnitSpecForType looks up a suffix in the unit table for typ.
func UnitSpecForType(typ gotypes.Type, name string) (UnitSpec, bool) {
	decl, source := unitTableForType(typ)
	if source == nil || !isUnitName(name) {
		return UnitSpec{}, false
	}
	var factor string
	prefix := name + "="
	for part := range strings.SplitSeq(decl, ",") {
		if value, ok := strings.CutPrefix(part, prefix); ok {
			factor = value
		}
	}
	if !isUnitFactor(factor) {
		return UnitSpec{}, false
	}
	return UnitSpec{Name: name, Factor: factor, SourceType: source}, true
}

// unitTableForType returns the declaration and the type that owns the table.
func unitTableForType(typ gotypes.Type) (string, gotypes.Type) {
	if alias, ok := typ.(*gotypes.Alias); ok {
		// Gogen must import the alias's package before it can try the
		// underlying type's table.
		if IsMainPkg(alias.Obj().Pkg()) {
			return "", nil
		}
		if decl, ok := unitDeclForTypeName(alias.Obj()); ok {
			return decl, alias
		}
		typ = gotypes.Unalias(typ)
	}

	named, ok := typ.(*gotypes.Named)
	if !ok {
		return "", nil
	}
	decl, ok := unitDeclForTypeName(named.Obj())
	if !ok {
		return "", nil
	}
	return decl, named
}

// unitDeclForTypeName returns the raw XGo unit declaration string for obj.
func unitDeclForTypeName(obj *gotypes.TypeName) (string, bool) {
	pkg := obj.Pkg()
	if pkg == nil {
		return "", false
	}

	if pkg.Path() == "time" && obj.Name() == "Duration" {
		return timeDurationUnitDecl, true
	}

	// gogen.ValWithUnit imports non-time unit declarations by package path.
	// Main-package unit declarations are not accepted by the compiler.
	if pkg.Path() == "" || IsMainPkg(pkg) {
		return "", false
	}

	unitObj, ok := pkg.Scope().Lookup("XGou_" + obj.Name()).(*gotypes.Const)
	if !ok {
		return "", false
	}
	val := unitObj.Val()
	if val == nil || val.Kind() != constant.String {
		return "", false
	}
	return constant.StringVal(val), true
}

// parseUnitDecl lists units in source order with the last factor for each
// suffix, matching gogen's table.
func parseUnitDecl(decl string, typ gotypes.Type) []UnitSpec {
	var specs []UnitSpec
	indices := make(map[string]int)
	for part := range strings.SplitSeq(decl, ",") {
		name, factor, ok := strings.Cut(part, "=")
		if !ok || !isUnitName(name) {
			continue
		}
		if index, found := indices[name]; found {
			specs[index].Factor = factor
			continue
		}
		indices[name] = len(specs)
		specs = append(specs, UnitSpec{
			Name:       name,
			Factor:     factor,
			SourceType: typ,
		})
	}
	return slices.DeleteFunc(specs, func(spec UnitSpec) bool { return !isUnitFactor(spec.Factor) })
}

// isUnitName reports whether name matches the unit suffix syntax accepted by
// the XGo scanner.
func isUnitName(name string) bool {
	if !utf8.ValidString(name) {
		return false
	}
	for i, r := range name {
		if r == '_' || unicode.IsLetter(r) || i > 0 && unicode.IsDigit(r) {
			continue
		}
		return false
	}
	return name != ""
}

// isUnitFactor reports whether gogen can parse factor as a unit multiplier.
func isUnitFactor(factor string) bool {
	return constant.MakeFromLiteral(factor, gotoken.FLOAT, 0).Kind() != constant.Unknown
}
