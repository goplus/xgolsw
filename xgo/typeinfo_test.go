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
	"testing"

	"github.com/goplus/mod/xgomod"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTypeInfoCache(t *testing.T) {
	t.Run("ValidProject", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`
var x int = 42
var y string = "hello"

func add(a, b int) int {
	return a + b
}

func main() {
	result := add(x, 10)
	println(result, y)
}
`),
		}, FeatASTCache|FeatTypeInfoCache)

		cache, err := buildTypeInfoCache(proj)
		require.NoError(t, err)
		require.NotNil(t, cache)

		typeInfoCache, ok := cache.(*typeInfoCache)
		require.True(t, ok)
		require.NotNil(t, typeInfoCache.typeInfo)
		assert.NoError(t, typeInfoCache.checkerErr)

		// Verify the type info structure.
		typeInfo := typeInfoCache.typeInfo
		require.NotNil(t, typeInfo.Pkg)
		assert.Equal(t, proj.PkgPath, typeInfo.Pkg.Path())
		assert.Equal(t, "main", typeInfo.Pkg.Name())
		assert.NotEmpty(t, typeInfo.Defs)
		assert.NotEmpty(t, typeInfo.Uses)
	})

	t.Run("ASTPackageError", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"invalid.xgo": file(`invalid syntax {{{`),
		}, 0)

		cache, err := buildTypeInfoCache(proj)
		assert.ErrorIs(t, err, ErrUnknownCacheKind)
		assert.Nil(t, cache)
	})

	t.Run("ASTCacheUnavailable", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`var x int`),
		}, FeatTypeInfoCache)

		cache, err := buildTypeInfoCache(proj)
		require.Error(t, err)
		assert.Nil(t, cache)
		assert.Contains(t, err.Error(), "failed to retrieve AST package")
		assert.Contains(t, err.Error(), ErrUnknownCacheKind.Error())
	})

	t.Run("TypeCheckingError", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`
var x int = "string" // Type error
var y = undefinedVar // Undefined variable

func test() {
	z := x + y
}
`),
		}, FeatASTCache|FeatTypeInfoCache)

		cache, err := buildTypeInfoCache(proj)
		require.NoError(t, err)
		require.NotNil(t, cache)

		typeInfoCache, ok := cache.(*typeInfoCache)
		require.True(t, ok)
		require.NotNil(t, typeInfoCache.typeInfo)

		// Should have type checking errors.
		assert.Error(t, typeInfoCache.checkerErr)

		// But still should have some type information.
		typeInfo := typeInfoCache.typeInfo
		require.NotNil(t, typeInfo.Pkg)
	})
}

func TestProjectTypeInfo(t *testing.T) {
	t.Run("TypeSwitchRecovery", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			body    string
			wantVar bool
			wantErr bool
			refs    int
		}{
			{name: "NoBinding", body: "switch input.(type) { case int: }"},
			{name: "BlankBinding", body: "switch _ := input.(type) { case int: }"},
			{name: "EmptySwitch", body: "switch value := input.(type) {}", wantVar: true},
			{name: "InvalidInput", body: "switch value := (1).(type) { default: }", wantErr: true},
			{name: "MissingInput", body: "switch value := missing.(type) { default: }", wantErr: true},
			{name: "AssignmentBinding", body: "switch value = input.(type) { default: }", wantErr: true},
			{name: "MultipleBindings", body: "switch other, value := input.(type) { default: }", wantErr: true},
			{name: "IncompleteBody", body: "switch value := input.(type) {", wantVar: true, wantErr: true},
			{name: "BodyError", body: "switch value := input.(type) { case int: echo value; missing() }", wantVar: true, wantErr: true, refs: 1},
		} {
			t.Run(tt.name, func(t *testing.T) {
				proj := newTestProject(t, map[string]*File{"main.xgo": file("func use(input any) {\n" + tt.body + "\n}\n")}, FeatAll)
				info, err := proj.TypeInfo()
				require.NotNil(t, info)
				_, parseErr := proj.ASTFile("main.xgo")
				if tt.wantErr {
					assert.True(t, err != nil || parseErr != nil)
				} else {
					require.NoError(t, err)
					require.NoError(t, parseErr)
				}
				var declarations []gotypes.Object
				for ident, obj := range info.Defs {
					if ident.Name == "value" && obj != nil {
						declarations = append(declarations, obj)
					}
				}
				if tt.wantVar {
					require.Len(t, declarations, 1)
					assert.Len(t, info.RefIdentsFor(declarations[0]), tt.refs)
				} else {
					assert.Empty(t, declarations)
				}
			})
		}
	})

	t.Run("TypeSwitchDeclarations", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{"main.xgo": file(`func use(input any) {
switch value := input.(type) {
case int: echo value
case string: echo value
case bool, float64: echo value
default: echo value
}
switch value := input.(type) {
case int: echo value
}
}
`)}, FeatAll)
		_, err := proj.TypeInfo()
		require.NoError(t, err)
		for _, view := range []*Project{proj, proj.Snapshot(), proj.Fork()} {
			info, err := view.TypeInfo()
			require.NoError(t, err)
			astFile, err := view.ASTFile("main.xgo")
			require.NoError(t, err)
			var declarations []gotypes.Object
			ast.Inspect(astFile, func(node ast.Node) bool {
				stmt, ok := node.(*ast.TypeSwitchStmt)
				if !ok {
					return true
				}
				assign, ok := stmt.Assign.(*ast.AssignStmt)
				require.True(t, ok)
				ident, ok := assign.Lhs[0].(*ast.Ident)
				require.True(t, ok)
				declaration := info.Defs[ident]
				require.NotNil(t, declaration)
				declarations = append(declarations, declaration)
				assert.True(t, gotypes.IsInterface(declaration.Type()))
				assert.Same(t, ident, info.ObjToDef[declaration])
				assert.Len(t, info.RefIdentsFor(declaration), len(stmt.Body.List))
				for _, clause := range stmt.Body.List {
					scope := info.Scopes[clause]
					require.NotNil(t, scope)
					variable := scope.Lookup(ident.Name)
					require.NotNil(t, variable)
					assert.NotSame(t, declaration, variable)
					assert.Same(t, ident, info.ObjToDef[variable])
					assert.Same(t, declaration, info.ObjectDeclaration(variable))
					assert.ElementsMatch(t, info.RefIdentsFor(declaration), info.RefIdentsFor(variable))
				}
				return true
			})
			require.Len(t, declarations, 2)
			assert.NotSame(t, declarations[0], declarations[1])
			var referenceTypes []string
			for ident, obj := range info.Uses {
				if ident.Name == "value" {
					referenceTypes = append(referenceTypes, obj.Type().String())
				}
			}
			assert.ElementsMatch(t, []string{"int", "int", "string", "interface{}", "interface{}"}, referenceTypes)
		}
	})

	t.Run("LocalTypeDeclarations", func(t *testing.T) {
		for _, tt := range []struct {
			name       string
			filename   string
			newProject func(*testing.T, map[string]*File, uint) *Project
		}{
			{"XGo", "main.xgo", newTestProject},
			{"NormalClass", "Record.gox", newTestProject},
			{"ProjectClass", "main_fixture.gox", newFrameworkTestProject},
			{"WorkClass", "Worker_fixture.gox", newFrameworkTestProject},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string]*File{tt.filename: file(`func first() {
	type Item int
	var value Item
	echo value
	{
		type Item = string
		var value Item
		echo value
	}
}
func second() {
	type Item struct { Value int }
	var value Item
	echo value
}
`)}
				if tt.name == "WorkClass" {
					files["main_fixture.gox"] = file("")
				}
				proj := tt.newProject(t, files, FeatAll)
				_, err := proj.TypeInfo()
				require.NoError(t, err)
				for _, view := range []*Project{proj, proj.Snapshot(), proj.Fork()} {
					info, err := view.TypeInfo()
					require.NoError(t, err)
					astFile, err := view.ASTFile(tt.filename)
					require.NoError(t, err)
					objects := make(map[gotypes.Object]bool)
					ast.Inspect(astFile, func(node ast.Node) bool {
						if spec, ok := node.(*ast.TypeSpec); ok {
							obj := info.Defs[spec.Name]
							require.NotNil(t, obj)
							assert.Same(t, spec.Name, info.ObjToDef[obj])
							assert.Same(t, info.Pkg, obj.Pkg())
							assert.False(t, objects[obj], "same-named local types must have distinct objects")
							objects[obj] = true
							assert.Len(t, info.RefIdentsFor(obj), 1)
						}
						return true
					})
					assert.Len(t, objects, 3)
				}
			})
		}
	})

	t.Run("OverloadDeclarations", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`func pickInt(value int) {}
func pickString(value string) {}
func pick = (
	pickInt
	pickString
)
pick 1
pick "text"
`),
		}, FeatASTCache|FeatTypeInfoCache)
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		require.Len(t, info.Overloads, 2)
		var selected []string
		for ident, declaration := range info.Overloads {
			assert.Equal(t, "pick", ident.Name)
			assert.Same(t, info.Pkg.Scope().Lookup("pick"), declaration)
			obj := info.ObjectOf(ident)
			require.NotNil(t, obj)
			selected = append(selected, obj.Name())
			decl, members := info.OverloadOf(ident)
			assert.Same(t, declaration, decl)
			assert.ElementsMatch(t, []gotypes.Object{
				info.Pkg.Scope().Lookup("pickInt"), info.Pkg.Scope().Lookup("pickString"),
			}, members)
		}
		assert.ElementsMatch(t, []string{"pickInt", "pickString"}, selected)
	})

	t.Run("NormalClass", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"Record.gox": file(`var value int

func Double() int {
	return value * 2
}
`),
		}, FeatASTCache|FeatTypeInfoCache)

		typeInfo, err := proj.TypeInfo()
		require.NoError(t, err)
		require.NotNil(t, typeInfo)
		require.NotNil(t, typeInfo.Pkg)
		record := typeInfo.Pkg.Scope().Lookup("Record")
		require.NotNil(t, record)
		recordStruct, ok := record.Type().Underlying().(*gotypes.Struct)
		require.True(t, ok)
		require.Equal(t, 1, recordStruct.NumFields())
		assert.Equal(t, "value", recordStruct.Field(0).Name())
		assert.Equal(t, gotypes.Typ[gotypes.Int], recordStruct.Field(0).Type())
		method, _, _ := gotypes.LookupFieldOrMethod(record.Type(), true, typeInfo.Pkg, "Double")
		double, ok := method.(*gotypes.Func)
		require.True(t, ok)
		assert.True(t, gotypes.Identical(gotypes.NewPointer(record.Type()), double.Signature().Recv().Type()))
		for _, pkg := range typeInfo.Pkg.Imports() {
			assert.NotEqual(t, testframework.PkgPath, pkg.Path())
		}
	})

	t.Run("FrameworkClasses", func(t *testing.T) {
		proj := newFrameworkTestProject(t, map[string]*File{
			"main_fixture.gox": file(`var total int

onStart => {
	total = measure(1)
	total = measure("sample")
}
`),
			"Worker_fixture.gox": file(`var value int

onValue amount => {
	value = amount
	total = value
}
echo label
`),
		}, FeatASTCache|FeatTypeInfoCache)

		_, registeredByDefault := xgomod.Default.LookupClass("_fixture.gox")
		assert.False(t, registeredByDefault)
		_, hasSpx := proj.Module().LookupClass(".spx")
		assert.False(t, hasSpx)

		typeInfo, err := proj.TypeInfo()
		require.NoError(t, err)
		require.NotNil(t, typeInfo)
		require.NotNil(t, typeInfo.Pkg)
		app := typeInfo.Pkg.Scope().Lookup("App")
		require.NotNil(t, app)
		appStruct, ok := app.Type().Underlying().(*gotypes.Struct)
		require.True(t, ok)
		require.Equal(t, 3, appStruct.NumFields())
		worker := typeInfo.Pkg.Scope().Lookup("Worker")
		require.NotNil(t, worker)
		workerStruct, ok := worker.Type().Underlying().(*gotypes.Struct)
		require.True(t, ok)
		require.Equal(t, 3, workerStruct.NumFields())

		framework, err := proj.Importer.Import(testframework.PkgPath)
		require.NoError(t, err)
		require.NotNil(t, framework)
		frameworkApp := framework.Scope().Lookup("App")
		require.NotNil(t, frameworkApp)
		frameworkItem := framework.Scope().Lookup("Item")
		require.NotNil(t, frameworkItem)
		assert.Same(t, frameworkApp.Type(), appStruct.Field(0).Type())
		assert.True(t, appStruct.Field(0).Embedded())
		assert.Equal(t, "Worker", appStruct.Field(1).Name())
		assert.True(t, gotypes.Identical(gotypes.NewPointer(worker.Type()), appStruct.Field(1).Type()))
		assert.Same(t, frameworkItem.Type(), workerStruct.Field(0).Type())
		assert.True(t, workerStruct.Field(0).Embedded())
		assert.True(t, gotypes.Identical(gotypes.NewPointer(app.Type()), workerStruct.Field(1).Type()))
		assert.True(t, workerStruct.Field(1).Embedded())

		total := appStruct.Field(2)
		assert.Equal(t, "total", total.Name())
		assert.Equal(t, gotypes.Typ[gotypes.Int], total.Type())
		assert.Equal(t, "value", workerStruct.Field(2).Name())
		assert.Equal(t, gotypes.Typ[gotypes.Int], workerStruct.Field(2).Type())

		uses := make(map[string][]gotypes.Object)
		for ident, obj := range typeInfo.Uses {
			if !ident.Implicit() {
				uses[ident.Name] = append(uses[ident.Name], obj)
			}
		}
		require.Len(t, uses["total"], 3)
		for _, obj := range uses["total"] {
			assert.Same(t, total, obj)
		}
		require.Len(t, uses["measure"], 2)
		assert.ElementsMatch(t, []string{"Measure__0", "Measure__1"}, []string{
			uses["measure"][0].Name(), uses["measure"][1].Name(),
		})
		require.Len(t, uses["amount"], 1)
		assert.Equal(t, gotypes.Typ[gotypes.Int], uses["amount"][0].Type())
		require.Len(t, uses["label"], 1)
		assert.Equal(t, "Label", uses["label"][0].Name())
		assert.Same(t, framework, uses["label"][0].Pkg())
	})

	t.Run("FrameworkTypeError", func(t *testing.T) {
		proj := newFrameworkTestProject(t, map[string]*File{
			"main_fixture.gox": file(`var total int`),
			"Worker_fixture.gox": file(`onValue amount => {
	total = "invalid"
}
`),
		}, FeatASTCache|FeatTypeInfoCache)

		typeInfo, err := proj.TypeInfo()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Worker_fixture.gox")
		assert.Contains(t, err.Error(), "cannot use")
		require.NotNil(t, typeInfo)
		require.NotNil(t, typeInfo.Pkg)
		assert.NotNil(t, typeInfo.Pkg.Scope().Lookup("App"))
		assert.NotNil(t, typeInfo.Pkg.Scope().Lookup("Worker"))
	})

	t.Run("FrameworkCacheInvalidation", func(t *testing.T) {
		proj := newFrameworkTestProject(t, map[string]*File{
			"main_fixture.gox":   file(`var total int`),
			"Worker_fixture.gox": file(`var value int`),
		}, FeatASTCache|FeatTypeInfoCache)

		before, err := proj.TypeInfo()
		require.NoError(t, err)
		require.NotNil(t, before)
		require.NotNil(t, before.Pkg)
		beforeWorker := before.Pkg.Scope().Lookup("Worker")
		require.NotNil(t, beforeWorker)
		beforeField, _, _ := gotypes.LookupFieldOrMethod(beforeWorker.Type(), true, before.Pkg, "value")
		require.NotNil(t, beforeField)
		assert.Equal(t, gotypes.Typ[gotypes.Int], beforeField.Type())

		proj.PutFile("Worker_fixture.gox", file(`var value string`))
		after, err := proj.TypeInfo()
		require.NoError(t, err)
		require.NotNil(t, after)
		require.NotNil(t, after.Pkg)
		assert.NotSame(t, before, after)
		afterWorker := after.Pkg.Scope().Lookup("Worker")
		require.NotNil(t, afterWorker)
		afterField, _, _ := gotypes.LookupFieldOrMethod(afterWorker.Type(), true, after.Pkg, "value")
		require.NotNil(t, afterField)
		assert.Equal(t, gotypes.Typ[gotypes.String], afterField.Type())

		beforeBase, _, _ := gotypes.LookupFieldOrMethod(beforeWorker.Type(), true, before.Pkg, "Item")
		require.NotNil(t, beforeBase)
		afterBase, _, _ := gotypes.LookupFieldOrMethod(afterWorker.Type(), true, after.Pkg, "Item")
		require.NotNil(t, afterBase)
		assert.Same(t, beforeBase.Type(), afterBase.Type())
	})

	t.Run("Basic", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`
var counter int = 0

func increment() {
	counter = counter + 1
}

func getCounter() int {
	return counter
}
`),
		}, FeatASTCache|FeatTypeInfoCache)

		typeInfo, err := proj.TypeInfo()
		require.NoError(t, err)
		require.NotNil(t, typeInfo)

		require.NotNil(t, typeInfo.Pkg)
		assert.Equal(t, proj.PkgPath, typeInfo.Pkg.Path())
		assert.Equal(t, "main", typeInfo.Pkg.Name())

		// Verify that we have type information.
		assert.NotEmpty(t, typeInfo.Defs)
		assert.NotEmpty(t, typeInfo.Uses)

		// Check that counter variable is properly typed.
		var counterObj gotypes.Object
		for ident, obj := range typeInfo.Defs {
			if ident.Name == "counter" {
				counterObj = obj
				break
			}
		}
		require.NotNil(t, counterObj)
		assert.Equal(t, "int", counterObj.Type().String())
	})

	t.Run("Cache", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`var x int`),
		}, FeatASTCache|FeatTypeInfoCache)

		// First call.
		typeInfo1, err1 := proj.TypeInfo()
		require.NoError(t, err1)
		require.NotNil(t, typeInfo1)

		// Second call should return the same cached instance.
		typeInfo2, err2 := proj.TypeInfo()
		require.NoError(t, err2)
		require.NotNil(t, typeInfo2)

		// Should be the same instance due to caching.
		assert.Same(t, typeInfo1, typeInfo2)
	})

	t.Run("CacheError", func(t *testing.T) {
		proj := newTestProject(t, map[string]*File{
			"main.xgo": file(`var x int`),
		}, 0)

		typeInfo, err := proj.TypeInfo()
		assert.ErrorIs(t, err, ErrUnknownCacheKind)
		assert.Nil(t, typeInfo)
	})
}
