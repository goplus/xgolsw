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
	"path/filepath"
	"strings"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/cl"
)

// ClassfileExt returns the class extension using the same basename as the parser.
func ClassfileExt(filename string) string {
	return modfile.ClassExt(filepath.Base(filename))
}

// SourceClassName returns the nominal class name when only syntax is available.
// Flat fragments have no class of their own and require project type information.
func SourceClassName(file *ast.File, filename string, lookupClass func(string) (*modfile.Project, bool)) string {
	if !file.IsClass {
		return ""
	}
	if !file.IsNormalGox {
		if lookupClass == nil {
			return ""
		}
		if framework, ok := lookupClass(ClassfileExt(filename)); !ok || framework.Flat && !file.IsProj {
			return ""
		}
	}
	name, _ := cl.GetFileClassType(file, filename, lookupClass)
	return strings.TrimPrefix(name, "*")
}
