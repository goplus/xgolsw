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
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/ast"
	"github.com/stretchr/testify/assert"
)

func TestSourceClassName(t *testing.T) {
	for _, tt := range []struct {
		name, filename, want string
		flags                ast.File
	}{
		{name: "Plain", filename: "Plain.xgo"},
		{name: "OrdinaryClass", filename: "Record.gox", want: "Record", flags: ast.File{IsClass: true, IsNormalGox: true}},
		{name: "FlatProjectWithPointerBase", filename: "main.flat", want: "App", flags: ast.File{IsClass: true, IsProj: true}},
		{name: "FlatFragment", filename: "nested/Fragment.flat", flags: ast.File{IsClass: true}},
		{name: "Unregistered", filename: "Unknown.class", flags: ast.File{IsClass: true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lookup := func(ext string) (*modfile.Project, bool) {
				if ext == ".flat" {
					return &modfile.Project{Class: "*App", Flat: true}, true
				}
				return nil, false
			}
			assert.Equal(t, tt.want, SourceClassName(&tt.flags, tt.filename, lookup))
		})
	}
	assert.Empty(t, SourceClassName(&ast.File{IsClass: true}, "Unregistered.class", nil))
}

func TestClassfileExt(t *testing.T) {
	assert.Equal(t, ".gox", ClassfileExt("work_dir/Record.gox"))
	assert.Equal(t, "_fixture.gox", ClassfileExt("work_dir/Record_fixture.gox"))
	assert.Equal(t, ".flat", ClassfileExt("work_dir/Fragment.flat"))
}
