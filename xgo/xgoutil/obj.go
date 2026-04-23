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

import gotypes "go/types"

// IsInBuiltinPkg reports whether the given object is defined in the "builtin" package.
func IsInBuiltinPkg(obj gotypes.Object) bool {
	return obj != nil && IsBuiltinPkg(obj.Pkg())
}

// IsInMainPkg reports whether the given object is defined in the "main" package.
func IsInMainPkg(obj gotypes.Object) bool {
	return obj != nil && IsMainPkg(obj.Pkg())
}

// IsExportedOrInMainPkg reports whether the given object is exported or
// defined in the "main" package.
func IsExportedOrInMainPkg(obj gotypes.Object) bool {
	return obj != nil && (obj.Exported() || IsInMainPkg(obj))
}

// IsRenameable reports whether the given object can be renamed.
func IsRenameable(obj gotypes.Object) bool {
	if !IsInMainPkg(obj) || !obj.Pos().IsValid() || obj.Parent() == gotypes.Universe {
		return false
	}
	switch obj.(type) {
	case *gotypes.Var, *gotypes.Const, *gotypes.TypeName, *gotypes.Func, *gotypes.Label:
		return true
	case *gotypes.PkgName:
		return false
	}
	return false
}
