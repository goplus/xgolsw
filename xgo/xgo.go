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

package xgo

import (
	"fmt"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/mod/modload"
	"github.com/goplus/mod/xgomod"
)

var spxProject = &modfile.Project{
	Ext:      ".spx",
	FullExt:  "main.spx",
	Class:    "Game",
	PkgPaths: []string{"github.com/goplus/spx/v2", "math"},
	Works:    []*modfile.Class{{Ext: ".spx", Class: "SpriteImpl", Embedded: true}},
}

func init() {
	modload.Default.Opt.Projects = append(modload.Default.Opt.Projects, spxProject)
	if err := xgomod.Default.ImportClasses(); err != nil {
		panic(err)
	}
}

// SetClassfileAutoImportedPackages sets the auto-imported packages for the
// classfile specified by id.
func SetClassfileAutoImportedPackages(id string, pkgs map[string]string) {
	if id != "spx" {
		panic(fmt.Sprintf("unknown classfile id: %s", id))
	}

	imports := make([]*modfile.Import, 0, len(pkgs))
	for name := range pkgs {
		imports = append(imports, &modfile.Import{Name: name, Path: pkgs[name]})
	}

	spxProject.Import = imports
}
