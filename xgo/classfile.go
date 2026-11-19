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
	gotypes "go/types"
	"strings"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// recordClassTypes associates classfiles with types produced by the checker.
func recordClassTypes(info *types.Info, pkg *ast.Package, module *Module) {
	classFiles := make(map[token.Pos]*ast.File)
	for _, file := range pkg.Files {
		if file.IsClass && file.Pos().IsValid() {
			classFiles[file.Pos()] = file
		}
	}
	if len(classFiles) == 0 {
		return
	}

	info.ClassTypes = make(map[*ast.File]*gotypes.Named, len(classFiles))
	// A generated embedded base records the source position of its classfile.
	// A synthesized project without an entry file instead has no position.
	type baseClass struct{ pkg, name string }
	synthesized := make(map[baseClass]*gotypes.Named)
	scope := info.Pkg.Scope()
	for _, name := range scope.Names() {
		obj, ok := scope.Lookup(name).(*gotypes.TypeName)
		if !ok || info.ObjToDef[obj] != nil {
			continue
		}
		named, ok := obj.Type().(*gotypes.Named)
		if !ok {
			continue
		}
		structure, ok := named.Underlying().(*gotypes.Struct)
		if !ok || structure.NumFields() == 0 {
			continue
		}
		base := structure.Field(0)
		if !base.Embedded() {
			continue
		}
		if file := classFiles[base.Pos()]; file != nil {
			info.ClassTypes[file] = named
		} else if !base.Pos().IsValid() {
			// Framework metadata names the embedded declaration, which may
			// be an alias even when the checker has produced a partial type.
			if baseType, ok := xgoutil.DerefType(base.Type()).(interface{ Obj() *gotypes.TypeName }); ok {
				baseObj := baseType.Obj()
				if baseObj.Pkg() != nil && baseObj.Pkg() != info.Pkg {
					synthesized[baseClass{baseObj.Pkg().Path(), baseObj.Name()}] = named
				}
			}
		}
	}

	flatFiles := make(map[*ast.File]*modfile.Project)
	flatOwners := make(map[string]*gotypes.Named)
	for filename, file := range pkg.Files {
		if !file.IsClass {
			continue
		}
		if declType := classMethodReceiver(info, file); declType != nil {
			info.ClassTypes[file] = declType
		}
		if file.IsNormalGox && info.ClassTypes[file] == nil {
			// Empty ordinary classfiles have neither a method nor an embedded base.
			name := xgoutil.SourceClassName(file, filename, module.LookupClass)
			if obj, ok := scope.Lookup(name).(*gotypes.TypeName); ok && info.ObjToDef[obj] == nil {
				if named, ok := obj.Type().(*gotypes.Named); ok {
					info.ClassTypes[file] = named
				}
			}
		}
		if framework, ok := module.LookupClass(xgoutil.ClassfileExt(filename)); ok && framework.Flat && !file.IsNormalGox {
			flatFiles[file] = framework
			if owner := info.ClassTypes[file]; owner != nil && (file.IsProj || flatOwners[framework.Ext] == nil) {
				flatOwners[framework.Ext] = owner
			}
		}
	}
	for file, framework := range flatFiles {
		if info.ClassTypes[file] != nil {
			continue
		}
		owner := flatOwners[framework.Ext]
		if owner == nil && len(framework.PkgPaths) > 0 {
			owner = synthesized[baseClass{framework.PkgPaths[0], strings.TrimPrefix(framework.Class, "*")}]
		}
		if owner != nil {
			info.ClassTypes[file] = owner
		}
	}
}

// classMethodReceiver returns the compiler-assigned receiver of a class method.
func classMethodReceiver(info *types.Info, file *ast.File) *gotypes.Named {
	for _, decl := range file.Decls {
		method, ok := decl.(*ast.FuncDecl)
		if !ok || !method.IsClass {
			continue
		}
		fun, ok := info.Defs[method.Name].(*gotypes.Func)
		if !ok || fun.Signature().Recv() == nil {
			continue
		}
		if named, ok := xgoutil.DerefType(fun.Signature().Recv().Type()).(*gotypes.Named); ok {
			return named.Origin()
		}
	}
	return nil
}

// recordClassOverloadImplementations links inline class overloads to their
// lowered methods. The compiler's package-scope lookup can associate a
// same-named package function with an inline overload.
func recordClassOverloadImplementations(info *types.Info, pkg *ast.Package) {
	var literals map[token.Pos]*ast.FuncLit
	for _, file := range pkg.Files {
		if !file.IsClass {
			continue
		}
		for _, decl := range file.Decls {
			overload, ok := decl.(*ast.OverloadFuncDecl)
			if !ok || !overload.IsClass {
				continue
			}
			for _, candidate := range overload.Funcs {
				if literal, ok := candidate.(*ast.FuncLit); ok {
					if literals == nil {
						literals = make(map[token.Pos]*ast.FuncLit)
					}
					literals[literal.Pos()] = literal
				}
			}
		}
	}
	if len(literals) == 0 {
		return
	}
	for _, literal := range literals {
		delete(info.Implicits, literal)
		delete(info.Types, literal)
		delete(info.Types, literal.Type)
	}
	for _, obj := range info.Defs {
		method, ok := obj.(*gotypes.Func)
		if !ok || method.Signature().Recv() == nil {
			continue
		}
		// The generated method's name starts at the literal's func token.
		if literal := literals[method.Pos()]; literal != nil {
			info.Implicits[literal] = method
		}
	}
}
