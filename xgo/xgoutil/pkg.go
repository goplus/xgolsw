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
	"strings"
)

const (
	// XGoPackage indicates an XGo package.
	XGoPackage = "XGoPackage"

	// GopPackage indicates an XGo package using the legacy marker name.
	//
	// Deprecated: Use XGoPackage for new packages. GopPackage is retained for backwards compatibility.
	GopPackage = "GopPackage"
)

// IsMarkedAsXGoPackage reports whether the given package is marked as an XGo package.
// It recognizes both the current XGoPackage marker and the legacy GopPackage marker.
func IsMarkedAsXGoPackage(pkg *gotypes.Package) bool {
	if pkg == nil {
		return false
	}
	scope := pkg.Scope()
	if scope == nil {
		return false
	}
	return isXGoPackageMarker(scope.Lookup(XGoPackage)) || isXGoPackageMarker(scope.Lookup(GopPackage))
}

// IsXGoPackageMarkerName reports whether name is a supported XGo package marker name.
func IsXGoPackageMarkerName(name string) bool {
	return name == XGoPackage || name == GopPackage
}

// IsXGoInternalName reports whether name is reserved for XGo-generated internals.
func IsXGoInternalName(name string) bool {
	if IsXGoPackageMarkerName(name) {
		return true
	}
	return strings.HasPrefix(name, "XGo_") ||
		strings.HasPrefix(name, "Gop_") ||
		strings.HasPrefix(name, "__xgo_") ||
		strings.HasPrefix(name, "__gop_")
}

// isXGoPackageMarker reports whether the object is a truthy XGo package marker.
func isXGoPackageMarker(obj gotypes.Object) bool {
	if obj == nil {
		return false
	}
	cnst, ok := obj.(*gotypes.Const)
	if !ok {
		return false
	}
	if cnst.Type() != gotypes.Typ[gotypes.UntypedBool] {
		return false
	}
	cnstVal := cnst.Val()
	return cnstVal != nil && constant.BoolVal(cnstVal)
}

// PkgPath returns the package path of the given pkg. It returns "builtin" if
// the pkg is nil.
func PkgPath(pkg *gotypes.Package) string {
	if pkg == nil {
		return "builtin"
	}
	pkgPath := pkg.Path()
	if pkgPath == "" {
		// Builtin objects do not belong to any package. But in the type system of XGo,
		// they may have non-nil package with an empty path, e.g., append.
		return "builtin"
	}
	return pkgPath
}

// IsBuiltinPkg reports whether the given package is the "builtin" package.
func IsBuiltinPkg(pkg *gotypes.Package) bool {
	return PkgPath(pkg) == "builtin"
}

// IsMainPkg reports whether the given package is the "main" package.
func IsMainPkg(pkg *gotypes.Package) bool {
	return PkgPath(pkg) == "main"
}
