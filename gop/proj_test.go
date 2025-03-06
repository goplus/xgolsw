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

package gop

import (
	"testing"
)

func file(text string) File {
	return File{Content: []byte(text)}
}

func TestBasic(t *testing.T) {
	proj := NewProject(nil, map[string]File{
		"main.spx": file("echo 100"),
		"bar.spx":  file("echo 200"),
	}, FeatAll)
	f, err := proj.AST("main.spx")
	if err != nil || f == nil {
		t.Fatal(err)
	}
	if body := f.ShadowEntry.Body.List; len(body) != 1 {
		t.Fatal("body:", body)
	}
	proj2 := proj.Snapshot()
	f2, err2 := proj2.AST("main.spx")
	if f2 != f || err2 != nil {
		t.Fatal("Snapshot:", f2, err2)
	}
	proj.DeleteFile("main.spx")
	f3, err3 := proj.AST("main.spx")
	if f3 != nil || err3 != ErrNotFound {
		t.Fatal("DeleteFile:", f3, err3)
	}
	f4, err4 := proj2.AST("main.spx")
	if f4 != f || err4 != nil {
		t.Fatal("Snapshot after DeleteFile:", f4, err4)
	}
	if err5 := proj.DeleteFile("main.spx"); err5 != ErrNotFound {
		t.Fatal("DeleteFile after DeleteFile:", err5)
	}
	proj2.Rename("main.spx", "foo.spx")
	f5, err5 := proj2.AST("foo.spx")
	if f5 == f4 || err5 != nil {
		t.Fatal("AST after Rename:", f5, err5)
	}
	if err6 := proj2.Rename("main.spx", "foo.spx"); err6 != ErrNotFound {
		t.Fatal("Rename after Rename:", err6)
	}
	if err7 := proj2.Rename("foo.spx", "bar.spx"); err7 != ErrFileExists {
		t.Fatal("Rename exists:", err7)
	}
}

func TestNewNil(t *testing.T) {
	proj := NewProject(nil, nil, FeatAll)
	proj.PutFile("main.gop", file("echo 100"))
	f, err := proj.AST("main.gop")
	if err != nil || f == nil {
		t.Fatal(err)
	}
	if body := f.ShadowEntry.Body.List; len(body) != 1 {
		t.Fatal("body:", body)
	}
	if files, err := proj.ASTFiles(); err != nil || len(files) != 1 {
		t.Fatal("ASTFiles:", files, err)
	}
	pkg, _, err := proj.TypeInfo()
	if err != nil {
		t.Fatal("TypeInfo:", err)
	}
	if o := pkg.Scope().Lookup("main"); o == nil {
		t.Fatal("Scope.Lookup main failed")
	}
}

func TestNewCallback(t *testing.T) {
	proj := NewProject(nil, func() map[string]File {
		return map[string]File{
			"main.spx": file("echo 100"),
		}
	}, FeatAll)
	f, err := proj.AST("main.spx")
	if err != nil || f == nil {
		t.Fatal(err)
	}
	if body := f.ShadowEntry.Body.List; len(body) != 1 {
		t.Fatal("body:", body)
	}
	if _, err = proj.FileCache("unknown", "main.spx"); err != ErrUnknownKind {
		t.Fatal("FileCache:", err)
	}
}
