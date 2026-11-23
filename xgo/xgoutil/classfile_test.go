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
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSourceClassName(t *testing.T) {
	for _, tt := range []struct {
		name, filename, want string
		flags                ast.File
		flatFragment         bool
	}{
		{name: "Plain", filename: "Plain.xgo"},
		{name: "OrdinaryClass", filename: "Record.gox", want: "Record", flags: ast.File{IsClass: true, IsNormalGox: true}},
		{name: "FlatProjectWithPointerBase", filename: "main.flat", want: "App", flags: ast.File{IsClass: true, IsProj: true}},
		{name: "FlatFragment", filename: "nested/Fragment.flat", flags: ast.File{IsClass: true}, flatFragment: true},
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
			assert.Equal(t, tt.flatFragment, IsFlatClassFragment(&tt.flags, tt.filename, lookup))
		})
	}
	assert.Empty(t, SourceClassName(&ast.File{IsClass: true}, "Unregistered.class", nil))
}

func TestClassfileExt(t *testing.T) {
	assert.Equal(t, ".gox", ClassfileExt("work_dir/Record.gox"))
	assert.Equal(t, "_fixture.gox", ClassfileExt("work_dir/Record_fixture.gox"))
	assert.Equal(t, ".flat", ClassfileExt("work_dir/Fragment.flat"))
}

func TestClassReceiverAt(t *testing.T) {
	assert.Nil(t, ClassReceiverAt(&types.Info{}, nil, token.NoPos))
	assert.Nil(t, EnclosingClassReceiver(&types.Info{}, &ast.File{}, token.NoPos, nil))

	const source = `var count int = 42
const limit = 43

// Detached comment.

func Read() { echo count }
func Choose = (
	func() int { return 44 }
)
`
	for _, tt := range []struct {
		name, text, want string
		class, implicit  bool
		binding          string
	}{
		{name: "OrdinaryFile", text: "echo"},
		{name: "ClassFieldInitializer", text: "42", class: true, want: "*Record"},
		{name: "PackageConstant", text: "43", class: true},
		{name: "BetweenDeclarations", text: "Detached", class: true, implicit: true, binding: "*Record"},
		{name: "ExplicitReceiver", text: "echo", class: true, binding: "*Other", want: "*Other"},
		{name: "ImplicitReceiver", text: "echo", class: true, implicit: true, binding: "*Record", want: "*Record"},
		{name: "UnrecordedImplicitReceiver", text: "echo", class: true, implicit: true, want: "*Record"},
		{name: "UnrecordedExplicitReceiver", text: "echo", class: true},
		{name: "FunctionWithoutReceiver", text: "echo", class: true, binding: "func"},
		{name: "OutsideBody", text: "Read", class: true, implicit: true, binding: "*Record"},
		{name: "InlineClassOverload", text: "44", class: true, implicit: true, want: "*Record"},
		{name: "InlineNonClassOverload", text: "44", class: true},
		{name: "OverloadDeclaration", text: "Choose", class: true, implicit: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "Record.gox", source, parser.ParseXGoClass)
			require.NoError(t, err)
			file.IsClass = tt.class
			fields, ok := file.Decls[0].(*ast.GenDecl)
			require.True(t, ok)
			file.ClassFields = fields
			decl, ok := file.Decls[2].(*ast.FuncDecl)
			require.True(t, ok)
			decl.IsClass = tt.implicit
			overload, ok := file.Decls[3].(*ast.OverloadFuncDecl)
			require.True(t, ok)
			overload.IsClass = tt.implicit

			owner := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, nil, "Record", nil), gotypes.NewStruct(nil, nil), nil)
			other := gotypes.NewNamed(gotypes.NewTypeName(token.NoPos, nil, "Other", nil), gotypes.NewStruct(nil, nil), nil)
			info := &types.Info{ClassTypes: map[*ast.File]*gotypes.Named{file: owner}}
			if tt.binding != "" {
				var recv *gotypes.Var
				switch tt.binding {
				case "*Record":
					recv = gotypes.NewParam(token.NoPos, nil, "this", gotypes.NewPointer(owner))
				case "*Other":
					recv = gotypes.NewParam(token.NoPos, nil, "other", gotypes.NewPointer(other))
				}
				info.Defs = map[*ast.Ident]gotypes.Object{
					decl.Name: gotypes.NewFunc(token.NoPos, nil, decl.Name.Name, gotypes.NewSignatureType(recv, nil, nil, nil, nil, false)),
				}
			}
			pos := fset.File(file.Pos()).Pos(strings.Index(source, tt.text))
			receiver := ClassReceiverAt(info, file, pos)
			if tt.want == "" {
				assert.Nil(t, receiver)
			} else {
				require.NotNil(t, receiver)
				assert.Equal(t, tt.want, receiver.String())
			}
		})
	}
}
