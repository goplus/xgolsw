/*
 * Copyright (c) 2025 The GoPlus Authors (goplus.org). All rights reserved.
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

package goputil

import (
	"testing"

	"github.com/goplus/gop/ast"
	"github.com/goplus/gop/token"
	"github.com/goplus/goxlsw/gop"
)

func file(text string) gop.File {
	return &gop.FileImpl{Content: []byte(text)}
}

func TestRangeASTSpecs(t *testing.T) {
	proj := gop.NewProject(nil, map[string]gop.File{
		"main.gop": file("type A = int"),
	}, gop.FeatAll)
	RangeASTSpecs(proj, token.TYPE, func(spec ast.Spec) {
		ts := spec.(*ast.TypeSpec)
		if ts.Name.Name != "A" || ts.Assign == 0 {
			t.Fatal("RangeASTSpecs:", *ts)
		}
	})
}

func TestIsShadow(t *testing.T) {
	proj := gop.NewProject(nil, map[string]gop.File{
		"main.gop": file("echo 100"),
	}, gop.FeatAll)
	f, err := proj.AST("main.gop")
	if err != nil {
		t.Fatal("AST:", err)
	}
	if !IsShadow(proj, f.ShadowEntry.Name) {
		t.Fatal("IsShadow: failed")
	}
}

func TestClassFieldsDecl_Basic(t *testing.T) {
	proj := gop.NewProject(nil, map[string]gop.File{
		"main.gox": file(`import "a"; type T int; const pi=3.14; var x int`),
	}, gop.FeatAll)
	f, err := proj.AST("main.gox")
	if err != nil {
		t.Fatal("AST:", err)
	}
	if g := ClassFieldsDecl(f); g == nil || g.Tok != token.VAR {
		t.Fatal("ClassFieldsDecl: failed:", g)
	}
}

func TestClassFieldsDecl_NotFound(t *testing.T) {
	proj := gop.NewProject(nil, map[string]gop.File{
		"main.gox": file(`import "a"; func f(); type T int; const pi=3.14; var x int`),
	}, gop.FeatAll)
	f, err := proj.AST("main.gox")
	if err != nil {
		t.Fatal("AST:", err)
	}
	if g := ClassFieldsDecl(f); g != nil {
		t.Fatal("ClassFieldsDecl: failed:", g)
	}
}
