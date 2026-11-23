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
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/gogen"
	"github.com/goplus/mod/xgomod"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/internal"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/qiniu/x/errors"
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

func TestTypeCheckDiagnostics(t *testing.T) {
	fset := token.NewFileSet()
	file := fset.AddFile("main.xgo", -1, 16)
	pos, end := file.Pos(4), file.Pos(11)
	detail := fmt.Errorf("compiler error")

	t.Run("Unstructured", func(t *testing.T) {
		diagnostics := typeCheckDiagnostics(fset, new(types.Info), detail)
		require.Len(t, diagnostics, 1)
		assert.ErrorIs(t, diagnostics[0], detail)
	})

	t.Run("StructuredList", func(t *testing.T) {
		info := new(types.Info)
		diagnostics := typeCheckDiagnostics(fset, info, errors.List{
			detail,
			&gogen.CodeError{Pos: pos, End: end, Msg: "undefined: missing"},
			&gogen.ImportError{Pos: pos, End: end, Path: "example.com/missing", Err: detail},
		})
		assert.Equal(t, errors.List{
			typesutil.Error{Fset: fset, Pos: pos, End: end, Msg: "undefined: missing"},
			typesutil.Error{Fset: fset, Pos: pos, End: end, Msg: "compiler error"},
		}, diagnostics)
		assert.Equal(t, map[token.Pos]string{pos: "example.com/missing"}, info.FailedImportPaths)
	})
}

func TestHasInterfaceEmbeddingCycle(t *testing.T) {
	// Type names left by failed alias declarations have no type to inspect.
	assert.False(t, hasInterfaceEmbeddingCycle(nil, make(map[*gotypes.Interface]bool)))
}

func TestProjectTypeInfo(t *testing.T) {
	t.Run("EmptyProject", func(t *testing.T) {
		proj := newTestProject(t, nil, FeatAll)
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		require.NotNil(t, info)
		assert.True(t, info.Pkg.Complete())
		assert.Empty(t, info.Pkg.Scope().Names())
		assert.Empty(t, info.SelectedOverloads)
	})

	t.Run("LargeInferredCompositeLiterals", func(t *testing.T) {
		var source strings.Builder
		source.WriteString("type Point struct { X int }\n")
		for i := range 2000 {
			fmt.Fprintf(&source, "var value%d = %d\n", i, i)
		}
		source.WriteString(`var points map[string]Point = {"origin": {X: 1}}
var nullable map[string]*Point = {"empty": nil, "typed": (*Point)(nil)}
`)
		proj := newTestProject(t, map[string]*File{"main.xgo": file(source.String())}, FeatAll)
		data, err := pkgdata.New(testframework.NewPkgDataZip(t))
		require.NoError(t, err)
		proj.Importer = internal.NewImporter(data.OpenExport)
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		require.NotNil(t, info)
		require.GreaterOrEqual(t, len(info.Types), 2000)
		for expr := range info.Types {
			assert.NotNil(t, expr, "type information must not contain an absent expression")
		}
		assert.Nil(t, info.TypeOf(nil))
		assert.Nil(t, info.ObjectDeclaration(nil))

		astPkg, err := proj.ASTPackage()
		require.NoError(t, err)
		var inferred int
		ast.Inspect(astPkg.Files["main.xgo"], func(node ast.Node) bool {
			if literal, ok := node.(*ast.CompositeLit); ok && literal.Type == nil {
				inferred++
				tv, ok := info.Types[literal]
				require.True(t, ok)
				assert.NotNil(t, tv.Type)
				assert.True(t, tv.IsValue())
			}
			return true
		})
		assert.GreaterOrEqual(t, inferred, 3)
	})

	t.Run("InterfaceEmbeddingDiagnosticOrder", func(t *testing.T) {
		for range 20 {
			proj := newTestProject(t, map[string]*File{"main.xgo": file(`type Zebra interface { Alpha }
type Alpha interface { Zebra }
type Dependent interface { Zebra }
type Separate interface { Separate }
`)}, FeatAll)
			info, err := proj.TypeInfo()
			require.NotNil(t, info)
			require.EqualError(t, err, "main.xgo:1:6: invalid recursive interface Zebra\n"+
				"main.xgo:2:6: invalid recursive interface Alpha\n"+
				"main.xgo:3:6: invalid recursive interface Dependent\n"+
				"main.xgo:4:6: invalid recursive interface Separate")
		}
	})

	t.Run("InterfaceEmbedding", func(t *testing.T) {
		for _, tt := range []struct {
			name    string
			source  string
			wantErr bool
		}{
			{name: "DirectCycle", source: "type Broken interface { Broken }", wantErr: true},
			{name: "IndirectCycle", source: "type First interface { Second }\ntype Second interface { First }", wantErr: true},
			{name: "AliasCycle", source: "type Broken interface { Alias }\ntype Alias = Broken", wantErr: true},
			{name: "LocalCycle", source: "func use() { type Broken interface { Broken } }", wantErr: true},
			{name: "Diamond", source: `type Base interface { Run() }
type Left interface { Base }
type Right interface { Base }
type Both interface { Left; Right }`},
			{name: "RecursiveMethod", source: "type Node interface { Next() Node }"},
			{name: "StructPointer", source: "type Node struct { Next *Node }"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				proj := newTestProject(t, map[string]*File{"main.xgo": file(tt.source + "\n")}, FeatAll)
				info, err := proj.TypeInfo()
				require.NotNil(t, info)
				if tt.wantErr {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})

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

func TestProjectTypeInfoKeywordLowering(t *testing.T) {
	const filename = "main.xgo"
	proj := newTestProject(t, map[string]*File{filename: file(`type Options struct { Count int }
type Params interface {
	Count(value int) Params
	Enabled(value bool) Params
}
type Client struct{}
type Callback func(keywords map[string]int)
type Flexible interface { Count(value int) Flexible }
type Builder struct{}
var client Client
var callback Callback
func structure(opts Options?) {}
func mapping(opts map[string]int) {}
func (Client) Params() Params { return nil }
func (Client) build(opts Params?) {}
func (Client) Flexible() Builder { return Builder{} }
func (Builder) Count(value int) Builder { return Builder{} }
func Flexible_Init(builder Builder) Flexible { return nil }
func (Client) buildFlexible(opts Flexible?) {}
structure count = 1
mapping count = 1
callback count = 1
client.build count = 1, enabled = true
client.buildFlexible count = 1
`)}, FeatAll)
	data, err := pkgdata.New(testframework.NewPkgDataZip(t))
	require.NoError(t, err)
	proj.Importer = internal.NewImporter(data.OpenExport)
	info, err := proj.TypeInfo()
	require.NoError(t, err)
	parsed, err := proj.ASTFile(filename)
	require.NoError(t, err)
	require.NotNil(t, parsed.ShadowEntry)
	stmts := parsed.ShadowEntry.Body.List
	want := []struct {
		typ     gotypes.Type
		factory string
	}{
		{typ: info.Pkg.Scope().Lookup("Options").Type()},
		{typ: gotypes.NewMap(gotypes.Typ[gotypes.String], gotypes.Typ[gotypes.Int])},
		{},
		{typ: info.Pkg.Scope().Lookup("Params").Type(), factory: "Params"},
		{typ: info.Pkg.Scope().Lookup("Builder").Type(), factory: "Flexible"},
	}
	require.Len(t, stmts, len(want))
	require.Len(t, info.KeywordArgs, len(want))
	for i, stmt := range stmts {
		expr, ok := stmt.(*ast.ExprStmt)
		require.True(t, ok)
		call, ok := expr.X.(*ast.CallExpr)
		require.True(t, ok)
		require.NotEmpty(t, call.Kwargs)
		lowering, recorded := info.KeywordArgs[call.Kwargs[len(call.Kwargs)-1]]
		require.True(t, recorded)
		require.NotNil(t, lowering.Arg)
		typ := info.TypeOf(lowering.Arg)
		assert.True(t, gotypes.Identical(want[i].typ, typ), "got %v, want %v", typ, want[i].typ)
		if want[i].factory == "" {
			assert.Nil(t, lowering.Factory)
			assert.Empty(t, lowering.BuilderSteps)
			continue
		}
		require.NotNil(t, lowering.Factory)
		selector, ok := lowering.Factory.Fun.(*ast.SelectorExpr)
		require.True(t, ok)
		assert.Equal(t, want[i].factory, selector.Sel.Name)
		require.Len(t, lowering.BuilderSteps, len(call.Kwargs))
		for _, arg := range call.Kwargs {
			step := lowering.BuilderSteps[arg.Name.Pos()]
			require.NotNil(t, step)
			require.NotEmpty(t, step.Args)
			assert.Same(t, arg.Value, step.Args[len(step.Args)-1])
		}
		assert.Same(t, lowering.Arg, lowering.BuilderSteps[call.Kwargs[len(call.Kwargs)-1].Name.Pos()])
	}
}

func TestProjectTypeInfoPartialKeywordBuilder(t *testing.T) {
	const filename = "main.xgo"
	proj := newTestProject(t, map[string]*File{filename: file(`type Options interface {
	Count(int) Options
	Enabled(bool) Options
}
type Builder struct{}
type Client struct{}
func (Client) Options() Builder { return Builder{} }
func (Builder) Count(int) Builder { return Builder{} }
func (Builder) Enabled(bool) Options { return nil }
func (Client) Run(Options) {}
func (Client) RunMap(map[string]bool) {}
func (Client).Choose = (
	(Client).Run
	(Client).RunMap
)
var client Client
client.RunMap nil
client.Run count = 1, enabled = true
client.Run count = 1, enabled = "wrong"
client.Choose count = 1, enabled = "wrong"
client.Run enabled = "wrong"
`)}, FeatAll)
	data, err := pkgdata.New(testframework.NewPkgDataZip(t))
	require.NoError(t, err)
	proj.Importer = internal.NewImporter(data.OpenExport)
	info, err := proj.TypeInfo()
	require.Error(t, err)
	require.NotNil(t, info)
	diagnostics, ok := err.(errors.List)
	require.True(t, ok)
	require.NotEmpty(t, diagnostics)
	diagnostic, ok := diagnostics[0].(typesutil.Error)
	require.True(t, ok)
	assert.Equal(t, "cannot use untyped string value as type bool", diagnostic.Msg)
	parsed, err := proj.ASTFile(filename)
	require.NoError(t, err)
	require.NotNil(t, parsed.ShadowEntry)
	stmts := parsed.ShadowEntry.Body.List
	require.Len(t, stmts, 5)
	require.Len(t, info.KeywordArgs, 2)
	for i, stmt := range stmts[1:] {
		expr, ok := stmt.(*ast.ExprStmt)
		require.True(t, ok)
		call, ok := expr.X.(*ast.CallExpr)
		require.True(t, ok)
		require.NotEmpty(t, call.Kwargs)
		lowering, recorded := info.KeywordArgs[call.Kwargs[len(call.Kwargs)-1]]
		switch i {
		case 0:
			require.True(t, recorded)
			assert.NotNil(t, lowering.Arg)
		case 1:
			require.True(t, recorded)
			assert.Nil(t, lowering.Arg)
			require.Len(t, call.Kwargs, 2)
			assert.Equal(t, call.Kwargs[1].Value.Pos(), diagnostic.Pos)
			assert.Equal(t, call.Kwargs[1].Value.End(), diagnostic.End)
			require.Len(t, lowering.BuilderSteps, 1)
			step := lowering.BuilderSteps[call.Kwargs[0].Name.Pos()]
			require.NotNil(t, step)
			assert.Same(t, info.Pkg.Scope().Lookup("Builder").Type(), info.TypeOf(step))
		default:
			assert.False(t, recorded)
		}
	}
}

func TestCallTypeRecorderKeywordCallWithoutLoweredArgument(t *testing.T) {
	arg := &ast.KwargExpr{Name: ast.NewIdent("count"), Value: ast.NewIdent("value")}
	call := &ast.CallExpr{Kwargs: []*ast.KwargExpr{arg}}
	info := &types.Info{KeywordArgs: map[*ast.KwargExpr]types.KeywordLowering{arg: {Arg: &ast.CompositeLit{}}}}
	recorder := callTypeRecorder{info: info}

	recorder.recordKeywordCall(call)
	assert.NotContains(t, info.KeywordArgs, arg)
}

func TestCallTypeRecorderKeywordBuilderLowering(t *testing.T) {
	arg := ast.NewIdent("value")
	const stepPos = token.Pos(10)
	method := &ast.Ident{NamePos: stepPos, Name: "Count"}
	key := keywordArgumentKey{pos: stepPos, value: arg}
	for _, tt := range []struct {
		name    string
		factory ast.Expr
		want    bool
	}{
		{"Builder", &ast.CallExpr{Fun: &ast.SelectorExpr{X: ast.NewIdent("client"), Sel: ast.NewIdent("Options")}}, true},
		{"NonCallReceiver", ast.NewIdent("client"), false},
		{"NonSelectorFactory", &ast.CallExpr{Fun: ast.NewIdent("options")}, false},
		{"FactoryWithArguments", &ast.CallExpr{
			Fun:  &ast.SelectorExpr{X: ast.NewIdent("client"), Sel: ast.NewIdent("Options")},
			Args: []ast.Expr{ast.NewIdent("defaults")},
		}, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			step := &ast.CallExpr{Fun: &ast.SelectorExpr{X: tt.factory, Sel: method}, Args: []ast.Expr{arg}}
			recorder := callTypeRecorder{keywordArgs: map[keywordArgumentKey]ast.Expr{key: step}}
			lowering := recorder.keywordBuilderLowering(step)
			if tt.want {
				assert.Same(t, tt.factory, lowering.Factory)
				assert.Same(t, step, lowering.BuilderSteps[stepPos])
				assert.Empty(t, recorder.keywordArgs)
			} else {
				assert.Zero(t, lowering)
				assert.Same(t, step, recorder.keywordArgs[key])
			}
		})
	}
}

func TestProjectTypeInfoNestedOverloadSource(t *testing.T) {
	proj := newTestProject(t, map[string]*File{"main.xgo": file(`func chooseInt(value int) int { return value }
func chooseString(value string) int { return len(value) }
func Choose = (
	chooseInt
	chooseString
)
func Forward = (Choose)

var Value = Forward(1)
var Text = Forward("text")
`)}, FeatAll)
	data, err := pkgdata.New(testframework.NewPkgDataZip(t))
	require.NoError(t, err)
	proj.Importer = internal.NewImporter(data.OpenExport)
	info, err := proj.TypeInfo()
	require.NoError(t, err)

	var implementations []gotypes.Object
	for ident := range info.Uses {
		if ident.Name == "Forward" {
			assert.Same(t, info.Pkg.Scope().Lookup("Forward"), info.SourceObjectOf(ident))
			assert.Same(t, info.Pkg.Scope().Lookup("Choose"), info.ObjectOf(ident))
			implementations = append(implementations, info.SelectedOverloads[ident].Implementation)
		}
	}
	assert.ElementsMatch(t, []gotypes.Object{
		info.Pkg.Scope().Lookup("chooseInt"),
		info.Pkg.Scope().Lookup("chooseString"),
	}, implementations)
}

func TestProjectTypeInfoForwardedTypeArguments(t *testing.T) {
	const path = "example.com/generics"
	proj := newTestProject(t, map[string]*File{"main.xgo": file(`import dep "example.com/generics"

var Direct = dep.either(int, 1)
var Inferred = dep.forward(1)
var Explicit = dep.forward(int, 1)
var Transformed = dep.zTransform(string, "text")
var Parenthesized = (dep.forward)(int, 1)
`)}, FeatAll)
	src, err := goparser.ParseFile(proj.Fset, "generics.go", `package generics
const XGoPackage = true
func XGox_Convert[T any](value T) T { return value }
const XGoo_Either = "XGox_Convert,Convert"
const XGoo_Forward = "Either"
func TransformInt[T ~int](value T) T { return value }
func TransformString[T ~string](value T) T { return value }
const XGoo__XGox_Transform = "TransformInt,TransformString"
const XGoo__ZTransform = "Transform"
`, 0)
	require.NoError(t, err)
	dep, err := new(gotypes.Config).Check(path, proj.Fset, []*goast.File{src}, nil)
	require.NoError(t, err)
	archive, err := pkgdata.New(testframework.NewPkgDataZip(t))
	require.NoError(t, err)
	base := internal.NewImporter(archive.OpenExport)
	proj.Importer = pkgDocTestImporter(func(path string) (*gotypes.Package, error) {
		if path == dep.Path() {
			return dep, nil
		}
		return base.Import(path)
	})

	info, err := proj.TypeInfo()
	require.NoError(t, err)
	scope := dep.Scope()
	calls := make(map[int]*ast.Ident)
	for ident := range info.Overloads {
		calls[proj.Fset.PositionFor(ident.Pos(), false).Line] = ident
	}
	require.Len(t, calls, 5)
	assert.Same(t, scope.Lookup("Either"), info.SourceObjectOf(calls[3]))
	assert.Same(t, scope.Lookup("Convert"), info.ObjectOf(calls[3]))
	assert.Empty(t, info.SelectedOverloads[calls[3]])
	for _, tt := range []struct {
		line                            int
		source, recorded, impl, binding string
	}{
		{4, "Forward", "Either", "XGox_Convert", ""},
		{5, "Forward", "Either", "XGox_Convert", "Convert"},
		{6, "ZTransform", "Transform", "TransformString", "Transform"},
		{7, "Forward", "Forward", "XGox_Convert", "Convert"},
	} {
		ident := calls[tt.line]
		require.NotNil(t, ident)
		assert.Same(t, scope.Lookup(tt.source), info.SourceObjectOf(ident))
		assert.Same(t, scope.Lookup(tt.recorded), info.ObjectOf(ident))
		selection := info.SelectedOverloads[ident]
		assert.Same(t, scope.Lookup(tt.impl), selection.Implementation)
		if tt.binding != "" {
			assert.Same(t, scope.Lookup(tt.binding), selection.Binding)
		} else {
			assert.Nil(t, selection.Binding)
		}
	}
}

func TestProjectTypeInfoOverloadRetries(t *testing.T) {
	const declarations = `func chooseInt(number int) int { return number }
func chooseText(text string) int { return len(text) }
func Choose = (chooseInt; chooseText)
func Forward = (Choose)

func withInt(callback func(int) int, flag string) int { return callback(1) }
func withBool(callback func(bool) int, flag int) int { return callback(false) }
func withText(callback func(string) int, flag int) int { return callback("text") }
func Invalid = (withInt; withBool)
func Valid = (withInt; withText)
`
	for _, tt := range []struct {
		name, call, wantImplementation string
	}{
		{name: "Incomplete", call: "Invalid(value => Forward(value), 1)"},
		{name: "IncompleteParenthesized", call: "Invalid(value => ((Forward)(value)), 1)"},
		{name: "Checked", call: "Valid(value => Forward(value), 1)", wantImplementation: "chooseText"},
		{name: "CheckedParenthesized", call: "Valid(value => ((Forward)(value)), 1)", wantImplementation: "chooseText"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			proj := newTestProject(t, map[string]*File{"main.xgo": file(declarations + "var Result = " + tt.call + "\n")}, FeatAll)
			data, err := pkgdata.New(testframework.NewPkgDataZip(t))
			require.NoError(t, err)
			proj.Importer = internal.NewImporter(data.OpenExport)
			info, err := proj.TypeInfo()
			if tt.wantImplementation == "" {
				require.ErrorContains(t, err, "cannot use value (type bool)")
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, info)
			var calls []*ast.Ident
			for ident := range info.Uses {
				if ident.Name == "Forward" {
					calls = append(calls, ident)
				}
			}
			require.Len(t, calls, 1)
			ident := calls[0]
			assert.Same(t, info.Pkg.Scope().Lookup("Forward"), info.SourceObjectOf(ident))
			selection := info.SelectedOverloads[ident]
			if tt.wantImplementation == "" {
				assert.Empty(t, selection)
			} else {
				assert.Same(t, info.Pkg.Scope().Lookup(tt.wantImplementation), selection.Implementation)
			}
		})
	}
}

func TestProjectTypeInfoRetriedReceiver(t *testing.T) {
	proj := newTestProject(t, map[string]*File{"main.xgo": file(`type First struct{}
func (First) readNumber(number int) int { return number }
func (First) readText(text string) int { return len(text) }
func (First).Read = ((First).readNumber; (First).readText)

type Second struct{}
func (Second) Read(number int) int { return number }
func callFirst(callback func(First) int, flag string) int { return callback(First{}) }
func callSecond(callback func(Second) int, flag int) int { return callback(Second{}) }
func Retry = (callFirst; callSecond)
var Result = Retry(value => value.read(1), 1)
`)}, FeatAll)
	data, err := pkgdata.New(testframework.NewPkgDataZip(t))
	require.NoError(t, err)
	proj.Importer = internal.NewImporter(data.OpenExport)
	info, err := proj.TypeInfo()
	require.NoError(t, err)

	second := info.Pkg.Scope().Lookup("Second")
	require.NotNil(t, second)
	want, _, _ := gotypes.LookupFieldOrMethod(second.Type(), false, info.Pkg, "Read")
	require.NotNil(t, want)
	var calls []*ast.Ident
	for ident := range info.Uses {
		if ident.Name == "read" {
			calls = append(calls, ident)
		}
	}
	require.Len(t, calls, 1)
	assert.Same(t, want, info.SourceObjectOf(calls[0]))
	assert.NotContains(t, info.Overloads, calls[0])
}

func TestProjectTypeInfoRetriedIndexedCallable(t *testing.T) {
	proj := newTestProject(t, map[string]*File{"main.xgo": file(`func callFirst(callback func([]func() int) any, flag string) any { return nil }
func callSecond(callback func([]func() string) any, flag int) any { return nil }
func Retry = (callFirst; callSecond)
var Result = Retry(callbacks => ((callbacks[0])()), 1)
`)}, FeatAll)
	data, err := pkgdata.New(testframework.NewPkgDataZip(t))
	require.NoError(t, err)
	proj.Importer = internal.NewImporter(data.OpenExport)
	info, err := proj.TypeInfo()
	require.NoError(t, err)
	astFile, err := proj.ASTFile("main.xgo")
	require.NoError(t, err)
	var indexedCall *ast.CallExpr
	ast.Inspect(astFile, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if callee, ok := call.Fun.(*ast.ParenExpr); ok {
			if _, indexed := callee.X.(*ast.IndexExpr); indexed {
				indexedCall = call
			}
		}
		return true
	})
	require.NotNil(t, indexedCall)
	assert.Same(t, gotypes.Typ[gotypes.String], info.TypeOf(indexedCall))
	assert.Empty(t, info.SelectedOverloads)
}

func TestProjectTypeInfoParenthesizedOverloadVariables(t *testing.T) {
	proj := newTestProject(t, map[string]*File{"main.xgo": file(`import dep "example.com/dependency"
var Ambiguous = (dep.Choose)()
var Forwarded = (dep.Forward)()
var Identified = (dep.Narrow)()
`)}, FeatAll)
	pkg := gotypes.NewPackage("example.com/dependency", "dependency")
	result := gotypes.NewParam(token.NoPos, pkg, "", gotypes.Typ[gotypes.Int])
	sig := gotypes.NewSignatureType(nil, nil, nil, nil, gotypes.NewTuple(result), false)
	// The importer can supply different objects with the same signature.
	first := gotypes.NewVar(token.NoPos, pkg, "First", sig)
	second := gotypes.NewVar(token.NoPos, pkg, "Second", sig)
	choose := gogen.NewOverloadFunc(token.NoPos, pkg, "Choose", first, second)
	forward := gogen.NewOverloadFunc(token.NoPos, pkg, "Forward", choose)
	onlyFirst := gogen.NewOverloadFunc(token.NoPos, pkg, "OnlyFirst", first)
	narrow := gogen.NewOverloadFunc(token.NoPos, pkg, "Narrow", onlyFirst, second)
	for _, obj := range []gotypes.Object{first, second, choose, forward, onlyFirst, narrow} {
		require.Nil(t, pkg.Scope().Insert(obj))
	}
	pkg.MarkComplete()
	data, err := pkgdata.New(testframework.NewPkgDataZip(t))
	require.NoError(t, err)
	base := internal.NewImporter(data.OpenExport)
	proj.Importer = pkgDocTestImporter(func(path string) (*gotypes.Package, error) {
		if path == pkg.Path() {
			return pkg, nil
		}
		return base.Import(path)
	})
	info, err := proj.TypeInfo()
	require.NoError(t, err)

	calls := make(map[string]*ast.Ident)
	for ident := range info.Overloads {
		calls[ident.Name] = ident
	}
	require.Len(t, calls, 3)
	assert.Same(t, choose, info.SourceObjectOf(calls["Choose"]))
	assert.Same(t, forward, info.SourceObjectOf(calls["Forward"]))
	assert.Same(t, narrow, info.SourceObjectOf(calls["Narrow"]))
	assert.NotContains(t, info.SelectedOverloads, calls["Choose"])
	assert.NotContains(t, info.SelectedOverloads, calls["Forward"])
	assert.Same(t, first, info.SelectedOverloads[calls["Narrow"]].Implementation)
}

func TestProjectTypeInfoNestedMethodOverloadSource(t *testing.T) {
	proj := newTestProject(t, map[string]*File{"main.xgo": file(`type Record struct{}
func (Record) chooseInt(value int) int { return value }
func (Record) chooseString(value string) int { return len(value) }
func (Record).Choose = (
	(Record).chooseInt
	(Record).chooseString
)
func (Record).Forward = ((Record).Choose)

func decorate(value int, body func()) { body() }

var record Record
@decorate(record.forward(1))
func run() {}
`)}, FeatAll)
	data, err := pkgdata.New(testframework.NewPkgDataZip(t))
	require.NoError(t, err)
	proj.Importer = internal.NewImporter(data.OpenExport)
	info, err := proj.TypeInfo()
	require.NoError(t, err)
	assert.Len(t, info.FuncDecorators, 1)

	var call *ast.Ident
	for ident := range info.Uses {
		if ident.Name == "forward" {
			call = ident
			break
		}
	}
	require.NotNil(t, call)
	record := info.Pkg.Scope().Lookup("record")
	require.NotNil(t, record)
	receiver := record.Type()
	forward, _, _ := gotypes.LookupFieldOrMethod(receiver, true, info.Pkg, "Forward")
	choose, _, _ := gotypes.LookupFieldOrMethod(receiver, true, info.Pkg, "Choose")
	require.NotNil(t, forward)
	require.NotNil(t, choose)
	assert.Same(t, forward, info.SourceObjectOf(call))
	assert.Same(t, choose, info.ObjectOf(call))
	implementation, _, _ := gotypes.LookupFieldOrMethod(receiver, true, info.Pkg, "chooseInt")
	require.NotNil(t, implementation)
	assert.Same(t, implementation, info.SelectedOverloads[call].Implementation)
}

func TestProjectTypeInfoClassOverloadSource(t *testing.T) {
	const filename = "main_fixture.gox"
	proj := newFrameworkTestProject(t, map[string]*File{filename: file(`var value int = render(1)
func Read() int { return render(2) }
func Explicit() int { return this.render(3) }
func .StaticRead() int { return render(nil, 4) }
`)}, FeatAll)
	file, err := goparser.ParseFile(proj.Fset, "framework.go", `package framework
const XGoPackage = true
type App struct{}
type Item struct{}
func XGot_App_Main(app any, items ...any) {}
func RenderValue(app any, value int) int { return value }
const XGoo_RenderBase = "RenderValue"
const XGoo__Render = "RenderBase"
const XGoo__XGot_App_Render = "RenderBase"
`, 0)
	require.NoError(t, err)
	framework, err := new(gotypes.Config).Check(testframework.PkgPath, proj.Fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	data, err := pkgdata.New(testframework.NewPkgDataZip(t))
	require.NoError(t, err)
	base := internal.NewImporter(data.OpenExport)
	proj.Importer = pkgDocTestImporter(func(path string) (*gotypes.Package, error) {
		if path == framework.Path() {
			return framework, nil
		}
		return base.Import(path)
	})

	info, err := proj.TypeInfo()
	require.NoError(t, err)
	app := framework.Scope().Lookup("App")
	require.NotNil(t, app)
	method, _, _ := gotypes.LookupFieldOrMethod(app.Type(), true, framework, "Render")
	require.NotNil(t, method)
	function := framework.Scope().Lookup("Render")
	require.NotNil(t, function)

	sources := make(map[int]gotypes.Object)
	for ident := range info.Uses {
		if ident.Name == "render" {
			pos := proj.Fset.PositionFor(ident.Pos(), false)
			if pos.Filename == filename {
				sources[pos.Line] = info.SourceObjectOf(ident)
			}
		}
	}
	assert.Equal(t, map[int]gotypes.Object{1: method, 2: method, 3: method, 4: function}, sources)
}

func TestProjectTypeInfoInlineClassOverloads(t *testing.T) {
	const filename = "Record.gox"
	const packageFunction = "func Choose__0(value string) string { return value }\n"
	for _, tt := range []struct {
		name, function, packageSource, wantError string
	}{
		{name: "Method", function: "func(value int) int { return value }"},
		{name: "SameNamedPackageFunction", function: "func(value int) int { return value }", packageSource: packageFunction},
		{name: "IncompleteBody", function: "func(value int) int { return missing }", wantError: "undefined: missing"},
		{name: "IncompleteSignature", function: "func(value Missing) int { return 1 }", packageSource: packageFunction,
			wantError: "Missing is not a type"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			proj := newTestProject(t, map[string]*File{
				filename:   file("func Choose = (\n\t" + tt.function + "\n)\n"),
				"main.xgo": file(tt.packageSource),
			}, FeatAll)
			data, err := pkgdata.New(testframework.NewPkgDataZip(t))
			require.NoError(t, err)
			proj.Importer = internal.NewImporter(data.OpenExport)

			info, err := proj.TypeInfo()
			if tt.wantError == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantError)
			}
			astFile, err := proj.ASTFile(filename)
			require.NoError(t, err)
			require.Len(t, astFile.Decls, 1)
			decl, ok := astFile.Decls[0].(*ast.OverloadFuncDecl)
			require.True(t, ok)
			require.Len(t, decl.Funcs, 1)
			literal, ok := decl.Funcs[0].(*ast.FuncLit)
			require.True(t, ok)
			owner := info.ClassTypes[astFile]
			require.NotNil(t, owner)
			method, _, _ := gotypes.LookupFieldOrMethod(owner, true, info.Pkg, "Choose__0")
			require.NotNil(t, method)
			assert.Same(t, method, info.Implicits[literal])
			assert.Same(t, method.Type(), info.TypeOf(literal))
		})
	}
}

func TestProjectTypeInfoStaticMethodReceivers(t *testing.T) {
	proj := newTestProject(t, map[string]*File{"main.xgo": file(`type Record struct{}
func Record.Choose() {}
func Missing.Choose() {}
`)}, FeatAll)
	info, err := proj.TypeInfo()
	require.NoError(t, err)
	astFile, err := proj.ASTFile("main.xgo")
	require.NoError(t, err)
	var receivers []*ast.Ident
	for _, decl := range astFile.Decls {
		if method, ok := decl.(*ast.FuncDecl); ok && method.Static {
			ident, ok := method.Recv.List[0].Type.(*ast.Ident)
			require.True(t, ok)
			receivers = append(receivers, ident)
		}
	}
	require.Len(t, receivers, 2)
	owner := info.Pkg.Scope().Lookup("Record")
	require.NotNil(t, owner)
	assert.Same(t, owner, info.Uses[receivers[0]])
	assert.Nil(t, info.Uses[receivers[1]])
}

func TestProjectFlatClassTypes(t *testing.T) {
	const entry = "main_fixture.gox"
	const fragment = "nested/Chunk_fixture.gox"
	for _, tt := range []struct {
		name, source, peer, ordinary string
		baseAlias, typeError         string
		hasEntry, hasOwner           bool
	}{
		{name: "Body", hasEntry: true, hasOwner: true, source: "//xgols:notRecommended\nfunc Read() int { return Value }"},
		{name: "EmptyFragment", hasEntry: true, hasOwner: true},
		{name: "SynthesizedOwner", hasOwner: true, peer: "const Limit = 5", ordinary: "var Current *App"},
		{name: "SynthesizedAliasOwner", hasOwner: true, ordinary: "var Current *AppAlias",
			baseAlias: "AppAlias", typeError: "has no method Main"},
		{name: "NoCheckedOwner", peer: "const Limit = 5"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]*File{
				fragment:     file(tt.source),
				"main.xgo":   file("type Chunk struct{}\n" + tt.ordinary),
				"Record.gox": file("func .Choose(value int) int { return value }"),
			}
			if tt.hasEntry {
				files[entry] = file("//xgols:notRecommended\nvar Value int")
			}
			if tt.peer != "" {
				files["Peer_fixture.gox"] = file(tt.peer)
			}
			proj := newTestProject(t, files, FeatAll)
			config := testframework.NewModule(t).Module
			framework := config.Opt.Projects[0]
			framework.Flat = true
			framework.Works = nil
			wantOwner := framework.Class
			if tt.baseAlias != "" {
				framework.Class = tt.baseAlias
				wantOwner = tt.baseAlias
			}
			module, err := NewModule(config)
			require.NoError(t, err)
			proj.SetModule(module)
			baseSource := `package framework
const XGoPackage = true
type App struct{}
func XGot_App_Main(app any, items ...any) {}
`
			if tt.baseAlias != "" {
				baseSource += "type " + tt.baseAlias + " = App\n"
			}
			baseFile, err := goparser.ParseFile(proj.Fset, "framework.go", baseSource, 0)
			require.NoError(t, err)
			base, err := new(gotypes.Config).Check(testframework.PkgPath, proj.Fset, []*goast.File{baseFile}, nil)
			require.NoError(t, err)
			fallback := proj.Importer
			proj.Importer = pkgDocTestImporter(func(path string) (*gotypes.Package, error) {
				if path == base.Path() {
					return base, nil
				}
				return fallback.Import(path)
			})

			info, err := proj.TypeInfo()
			if tt.typeError != "" {
				require.ErrorContains(t, err, tt.typeError)
			} else {
				require.NoError(t, err)
			}
			file, err := proj.ASTFile(fragment)
			require.NoError(t, err)
			if tt.hasOwner {
				owner := info.ClassTypes[file]
				require.NotNil(t, owner)
				assert.Same(t, info.Pkg.Scope().Lookup(wantOwner), owner.Obj())
			} else {
				assert.Nil(t, info.ClassTypes[file])
			}
			doc, err := proj.PkgDoc()
			require.NoError(t, err)
			require.Contains(t, doc.Types, "Chunk")
			assert.Empty(t, doc.Types["Chunk"].Methods)
			assert.Contains(t, doc.Types, "Record")
			if tt.hasEntry {
				assert.True(t, doc.Declarations[wantOwner+".Value"].NotRecommended)
			}
			if tt.source != "" {
				assert.True(t, doc.Declarations[wantOwner+".Read"].NotRecommended)
				assert.NotContains(t, doc.Declarations, "Chunk.Read")
			}
		})
	}
}
