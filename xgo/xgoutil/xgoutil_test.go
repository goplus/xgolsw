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
	"go/constant"
	"go/types"
	"path"
	"slices"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestFile(filename string, source any) (*token.FileSet, *ast.File, error) {
	fset := token.NewFileSet()
	mode := parser.ParseComments
	if path.Ext(filename) == ".gox" {
		mode |= parser.ParseGoPlusClass
	}
	astFile, err := parser.ParseEntry(fset, filename, source, parser.Config{Mode: mode})
	return fset, astFile, err
}

func newTestPackage(files map[string]*ast.File) *ast.Package {
	if files == nil {
		files = make(map[string]*ast.File)
	}
	return &ast.Package{
		Name:  "main",
		Files: files,
	}
}

func newTestTypeInfo(defs map[*ast.Ident]types.Object, uses map[*ast.Ident]types.Object) *xgotypes.Info {
	if defs == nil {
		defs = make(map[*ast.Ident]types.Object)
	}
	if uses == nil {
		uses = make(map[*ast.Ident]types.Object)
	}
	return &xgotypes.Info{
		Info: typesutil.Info{
			Types:      make(map[ast.Expr]types.TypeAndValue),
			Defs:       defs,
			Uses:       uses,
			Selections: make(map[*ast.SelectorExpr]*types.Selection),
			Implicits:  make(map[ast.Node]types.Object),
			Scopes:     make(map[ast.Node]*types.Scope),
		},
	}
}

func findIdent(astFile *ast.File, name string) *ast.Ident {
	var result *ast.Ident
	ast.Inspect(astFile, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok && ident.Name == name {
			if result == nil {
				result = ident
			}
		}
		return result == nil
	})
	return result
}

func findIdentWithPos(fset *token.FileSet, astFile *ast.File, name string) (*ast.Ident, token.Position) {
	var result *ast.Ident
	ast.Inspect(astFile, func(n ast.Node) bool {
		if ident, ok := n.(*ast.Ident); ok && ident.Name == name {
			if result == nil {
				result = ident
			}
		}
		return result == nil
	})
	if result != nil {
		return result, fset.Position(result.Pos())
	}
	return nil, token.Position{}
}

func markAsXGoPackage(pkg *types.Package) {
	cnst := types.NewConst(token.NoPos, pkg, XGoPackage, types.Typ[types.UntypedBool], constant.MakeBool(true))
	pkg.Scope().Insert(cnst)
}

func TestRangeASTSpecs(t *testing.T) {
	t.Run("SingleTypeSpec", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", "type A = int")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		var specs []ast.Spec
		RangeASTSpecs(astPkg, token.TYPE, func(spec ast.Spec) {
			specs = append(specs, spec)
		})

		require.Len(t, specs, 1)
		ts := specs[0].(*ast.TypeSpec)
		assert.Equal(t, "A", ts.Name.Name)
		assert.NotEqual(t, token.NoPos, ts.Assign)
	})

	t.Run("MultipleTypeSpecs", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
type (
	A = int
	B = string
	C struct {
		Field int
	}
)
`)
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		var specs []ast.Spec
		RangeASTSpecs(astPkg, token.TYPE, func(spec ast.Spec) {
			specs = append(specs, spec)
		})

		require.Len(t, specs, 3)
		names := make([]string, len(specs))
		for i, spec := range specs {
			ts := spec.(*ast.TypeSpec)
			names[i] = ts.Name.Name
		}
		assert.Contains(t, names, "A")
		assert.Contains(t, names, "B")
		assert.Contains(t, names, "C")
	})

	t.Run("VariableSpecs", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
var (
	x = 1
	y = "hello"
)
`)
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		var specs []ast.Spec
		RangeASTSpecs(astPkg, token.VAR, func(spec ast.Spec) {
			specs = append(specs, spec)
		})

		require.Len(t, specs, 2)
		names := make([]string, len(specs))
		for i, spec := range specs {
			vs := spec.(*ast.ValueSpec)
			names[i] = vs.Names[0].Name
		}
		assert.Contains(t, names, "x")
		assert.Contains(t, names, "y")
	})

	t.Run("ConstantSpecs", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
const (
	Pi = 3.14
	E  = 2.71
)
`)
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		var specs []ast.Spec
		RangeASTSpecs(astPkg, token.CONST, func(spec ast.Spec) {
			specs = append(specs, spec)
		})

		require.Len(t, specs, 2)
		names := make([]string, len(specs))
		for i, spec := range specs {
			vs := spec.(*ast.ValueSpec)
			names[i] = vs.Names[0].Name
		}
		assert.Contains(t, names, "Pi")
		assert.Contains(t, names, "E")
	})

	t.Run("MultipleFiles", func(t *testing.T) {
		_, mainFile, err := newTestFile("main.xgo", "type MainType = int")
		require.NoError(t, err)
		_, otherFile, err := newTestFile("other.xgo", "type OtherType = string")
		require.NoError(t, err)

		astPkg := newTestPackage(map[string]*ast.File{
			"main.xgo":  mainFile,
			"other.xgo": otherFile,
		})

		var specs []ast.Spec
		RangeASTSpecs(astPkg, token.TYPE, func(spec ast.Spec) {
			specs = append(specs, spec)
		})

		require.Len(t, specs, 2)
		names := make([]string, len(specs))
		for i, spec := range specs {
			ts := spec.(*ast.TypeSpec)
			names[i] = ts.Name.Name
		}
		assert.Contains(t, names, "MainType")
		assert.Contains(t, names, "OtherType")
	})

	t.Run("NoMatchingSpecs", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", "var x = 1")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		var specs []ast.Spec
		RangeASTSpecs(astPkg, token.TYPE, func(spec ast.Spec) {
			specs = append(specs, spec)
		})

		assert.Len(t, specs, 0)
	})

	t.Run("EmptyPackage", func(t *testing.T) {
		astPkg := newTestPackage(nil)

		var specs []ast.Spec
		RangeASTSpecs(astPkg, token.TYPE, func(spec ast.Spec) {
			specs = append(specs, spec)
		})

		assert.Len(t, specs, 0)
	})

	t.Run("MixedDeclarations", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
type MyType = int
var myVar = 1
const myConst = 42
func myFunc() {}`)
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		var typeSpecs []ast.Spec
		RangeASTSpecs(astPkg, token.TYPE, func(spec ast.Spec) {
			typeSpecs = append(typeSpecs, spec)
		})

		var varSpecs []ast.Spec
		RangeASTSpecs(astPkg, token.VAR, func(spec ast.Spec) {
			varSpecs = append(varSpecs, spec)
		})

		var constSpecs []ast.Spec
		RangeASTSpecs(astPkg, token.CONST, func(spec ast.Spec) {
			constSpecs = append(constSpecs, spec)
		})

		assert.Len(t, typeSpecs, 1)
		assert.Len(t, varSpecs, 1)
		assert.Len(t, constSpecs, 1)

		assert.Equal(t, "MyType", typeSpecs[0].(*ast.TypeSpec).Name.Name)
		assert.Equal(t, "myVar", varSpecs[0].(*ast.ValueSpec).Names[0].Name)
		assert.Equal(t, "myConst", constSpecs[0].(*ast.ValueSpec).Names[0].Name)
	})

	t.Run("ImportSpecs", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
import (
	"fmt"
	"strconv"
)
`)
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.xgo": astFile})

		var specs []ast.Spec
		RangeASTSpecs(astPkg, token.IMPORT, func(spec ast.Spec) {
			specs = append(specs, spec)
		})

		require.Len(t, specs, 2)
		paths := make([]string, len(specs))
		for i, spec := range specs {
			is := spec.(*ast.ImportSpec)
			paths[i] = is.Path.Value
		}
		assert.Contains(t, paths, `"fmt"`)
		assert.Contains(t, paths, `"strconv"`)
	})

	t.Run("NilPackage", func(t *testing.T) {
		var specs []ast.Spec
		RangeASTSpecs(nil, token.TYPE, func(spec ast.Spec) {
			specs = append(specs, spec)
		})

		assert.Len(t, specs, 0)
	})
}

func TestIsDefinedInClassFieldsDecl(t *testing.T) {
	t.Run("DefinedInClassFields", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.gox", []byte(`
var (
	x int
	y string
)
`))
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.gox": astFile})

		pkg := types.NewPackage("main", "main")
		xVar := types.NewVar(token.NoPos, pkg, "x", types.Typ[types.Int])
		xIdent := findIdent(astFile, "x")
		require.NotNil(t, xIdent)

		typeInfo := newTestTypeInfo(nil, nil)
		typeInfo.ObjToDef = map[types.Object]*ast.Ident{
			xVar: xIdent,
		}

		result := IsDefinedInClassFieldsDecl(fset, typeInfo, astPkg, xVar)
		assert.True(t, result)
	})

	t.Run("NotDefinedInClassFields", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.gox", `
func test() {
	z := 1
	println(z)
}
`)
		require.NoError(t, err)

		astPkg := newTestPackage(map[string]*ast.File{"main.gox": astFile})

		zVar := types.NewVar(token.NoPos, types.NewPackage("main", "main"), "z", types.Typ[types.Int])
		zIdent := findIdent(astFile, "z")
		require.NotNil(t, zIdent)

		typeInfo := newTestTypeInfo(nil, nil)
		typeInfo.ObjToDef = map[types.Object]*ast.Ident{
			zVar: zIdent,
		}

		result := IsDefinedInClassFieldsDecl(fset, typeInfo, astPkg, zVar)
		assert.False(t, result)
	})

	t.Run("NilObject", func(t *testing.T) {
		fset, astFile, err := newTestFile("main.gox", "var x int")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.gox": astFile})
		typeInfo := newTestTypeInfo(nil, nil)

		result := IsDefinedInClassFieldsDecl(fset, typeInfo, astPkg, nil)
		assert.False(t, result)
	})

	t.Run("NilFileSet", func(t *testing.T) {
		_, astFile, err := newTestFile("main.gox", "var x int")
		require.NoError(t, err)
		astPkg := newTestPackage(map[string]*ast.File{"main.gox": astFile})
		typeInfo := newTestTypeInfo(nil, nil)
		pkg := types.NewPackage("main", "main")
		xVar := types.NewVar(token.NoPos, pkg, "x", types.Typ[types.Int])

		result := IsDefinedInClassFieldsDecl(nil, typeInfo, astPkg, xVar)
		assert.False(t, result)
	})
}

func TestWalkPathEnclosingInterval(t *testing.T) {
	t.Run("WalkFunction", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", "func test() { println(1) }")
		require.NoError(t, err)
		var funcDecl *ast.FuncDecl
		ast.Inspect(astFile, func(n ast.Node) bool {
			if fn, ok := n.(*ast.FuncDecl); ok && fn.Name.Name == "test" {
				funcDecl = fn
				return false
			}
			return true
		})
		require.NotNil(t, funcDecl)

		var nodes []ast.Node
		WalkPathEnclosingInterval(astFile, funcDecl.Body.Pos(), funcDecl.Body.End(), false, func(node ast.Node) bool {
			nodes = append(nodes, node)
			return true
		})
		require.NotEmpty(t, nodes)
		assert.IsType(t, &ast.BlockStmt{}, nodes[0])
		assert.IsType(t, &ast.File{}, nodes[len(nodes)-1])
	})

	t.Run("WalkSinglePosition", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", "var x = 1")
		require.NoError(t, err)

		xIdent := findIdent(astFile, "x")
		require.NotNil(t, xIdent)
		identPos := xIdent.Pos()
		require.NotEqual(t, token.NoPos, identPos)

		var nodes []ast.Node
		WalkPathEnclosingInterval(astFile, identPos, identPos+1, false, func(node ast.Node) bool {
			nodes = append(nodes, node)
			return true
		})
		require.NotEmpty(t, nodes)
		assert.True(t, slices.ContainsFunc(nodes, func(node ast.Node) bool {
			if ident, ok := node.(*ast.Ident); ok && ident.Name == "x" {
				return true
			}
			return false
		}))
	})

	t.Run("StopWalk", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", "func test() { println(1) }")
		require.NoError(t, err)

		var funcDecl *ast.FuncDecl
		ast.Inspect(astFile, func(n ast.Node) bool {
			if fn, ok := n.(*ast.FuncDecl); ok && fn.Name.Name == "test" {
				funcDecl = fn
				return false
			}
			return true
		})
		require.NotNil(t, funcDecl)

		var nodes []ast.Node
		WalkPathEnclosingInterval(astFile, funcDecl.Body.Pos(), funcDecl.Body.End(), false, func(node ast.Node) bool {
			nodes = append(nodes, node)
			return false // Stop after first node.
		})
		assert.Len(t, nodes, 1)
	})

	t.Run("EmptyInterval", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", "package main")
		require.NoError(t, err)

		var nodes []ast.Node
		WalkPathEnclosingInterval(astFile, token.NoPos, token.NoPos, false, func(node ast.Node) bool {
			nodes = append(nodes, node)
			return true
		})
		assert.Len(t, nodes, 1) // Should still return at least the file node.
	})

	t.Run("WalkBackward", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", "func test() { x := 1; y := 2 }")
		require.NoError(t, err)

		var funcDecl *ast.FuncDecl
		ast.Inspect(astFile, func(n ast.Node) bool {
			if fn, ok := n.(*ast.FuncDecl); ok && fn.Name.Name == "test" {
				funcDecl = fn
				return false
			}
			return true
		})
		require.NotNil(t, funcDecl)

		var forwardNodes []ast.Node
		WalkPathEnclosingInterval(astFile, funcDecl.Body.Pos(), funcDecl.Body.End(), false, func(node ast.Node) bool {
			forwardNodes = append(forwardNodes, node)
			return true
		})

		var backwardNodes []ast.Node
		WalkPathEnclosingInterval(astFile, funcDecl.Body.Pos(), funcDecl.Body.End(), true, func(node ast.Node) bool {
			backwardNodes = append(backwardNodes, node)
			return true
		})

		require.NotEmpty(t, forwardNodes)
		require.NotEmpty(t, backwardNodes)
		require.Equal(t, len(forwardNodes), len(backwardNodes))

		// Backward walk should return nodes in reverse order.
		for i := range forwardNodes {
			assert.Equal(t, forwardNodes[i], backwardNodes[len(backwardNodes)-1-i])
		}
	})
}

func TestEnclosingFuncSignature(t *testing.T) {
	t.Run("FuncDecl", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
func foo() string {
	return "ok"
}
`)
		require.NoError(t, err)

		pkg := types.NewPackage("main", "main")
		sig := types.NewSignatureType(nil, nil, nil, nil, types.NewTuple(types.NewVar(token.NoPos, pkg, "", types.Typ[types.String])), false)
		fun := types.NewFunc(token.NoPos, pkg, "foo", sig)

		nameIdent := findIdent(astFile, "foo")
		require.NotNil(t, nameIdent)
		typeInfo := newTestTypeInfo(map[*ast.Ident]types.Object{nameIdent: fun}, nil)

		var ret *ast.ReturnStmt
		ast.Inspect(astFile, func(n ast.Node) bool {
			if r, ok := n.(*ast.ReturnStmt); ok {
				ret = r
				return false
			}
			return true
		})
		require.NotNil(t, ret)
		require.NotEmpty(t, ret.Results)

		path, _ := PathEnclosingInterval(astFile, ret.Results[0].Pos(), ret.Results[0].End())
		result := EnclosingFuncSignature(typeInfo, path)
		require.NotNil(t, result)
		assert.Equal(t, sig, result)
	})

	t.Run("FuncLit", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
func outer() {
	var fn = func() (int, error) {
		return 1, nil
	}
	_ = fn
}
`)
		require.NoError(t, err)

		pkg := types.NewPackage("main", "main")
		errorObj := types.Universe.Lookup("error")
		require.NotNil(t, errorObj)
		retTuple := types.NewTuple(
			types.NewVar(token.NoPos, pkg, "", types.Typ[types.Int]),
			types.NewVar(token.NoPos, pkg, "", errorObj.Type()),
		)
		sig := types.NewSignatureType(nil, nil, nil, nil, retTuple, false)

		typeInfo := newTestTypeInfo(nil, nil)

		var (
			ret *ast.ReturnStmt
			lit *ast.FuncLit
		)
		ast.Inspect(astFile, func(n ast.Node) bool {
			if r, ok := n.(*ast.ReturnStmt); ok {
				ret = r
			}
			if l, ok := n.(*ast.FuncLit); ok {
				lit = l
			}
			return true
		})
		require.NotNil(t, ret)
		require.NotNil(t, lit)
		typeInfo.Types[lit] = types.TypeAndValue{Type: sig}

		basic := ret.Results[0]
		path, _ := PathEnclosingInterval(astFile, basic.Pos(), basic.End())
		result := EnclosingFuncSignature(typeInfo, path)
		require.NotNil(t, result)
		assert.Equal(t, sig, result)
	})

	t.Run("NilTypeInfo", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", "func foo() {}")
		require.NoError(t, err)
		path, _ := PathEnclosingInterval(astFile, astFile.Pos(), astFile.End())
		assert.Nil(t, EnclosingFuncSignature(nil, path))
	})
}

func TestEnclosingNode(t *testing.T) {
	t.Run("ReturnStatement", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
func foo() {
	if true {
		return "ok"
	}
}
`)
		require.NoError(t, err)

		var lit ast.Expr
		ast.Inspect(astFile, func(n ast.Node) bool {
			if bl, ok := n.(*ast.BasicLit); ok {
				lit = bl
				return false
			}
			return true
		})
		require.NotNil(t, lit)

		path, _ := PathEnclosingInterval(astFile, lit.Pos(), lit.End())
		ret := EnclosingNode[*ast.ReturnStmt](path)
		require.NotNil(t, ret)
		assert.Contains(t, ret.Results, lit)
	})

	t.Run("FunctionDeclaration", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
func outer() {
	x := 42
	println(x)
}
`)
		require.NoError(t, err)

		var xIdent *ast.Ident
		ast.Inspect(astFile, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok && ident.Name == "x" {
				xIdent = ident
				return false
			}
			return true
		})
		require.NotNil(t, xIdent)

		path, _ := PathEnclosingInterval(astFile, xIdent.Pos(), xIdent.End())
		funcDecl := EnclosingNode[*ast.FuncDecl](path)
		require.NotNil(t, funcDecl)
		assert.Equal(t, "outer", funcDecl.Name.Name)
	})

	t.Run("IfStatement", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
func test() {
	if x := 10; x > 5 {
		println(x)
	}
}
`)
		require.NoError(t, err)

		var printlnCall *ast.CallExpr
		ast.Inspect(astFile, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "println" {
					printlnCall = call
					return false
				}
			}
			return true
		})
		require.NotNil(t, printlnCall)

		path, _ := PathEnclosingInterval(astFile, printlnCall.Pos(), printlnCall.End())
		ifStmt := EnclosingNode[*ast.IfStmt](path)
		require.NotNil(t, ifStmt)
		assert.NotNil(t, ifStmt.Init)
	})

	t.Run("BlockStatement", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
func test() {
	{
		x := 1
	}
}
`)
		require.NoError(t, err)

		var xIdent *ast.Ident
		ast.Inspect(astFile, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok && ident.Name == "x" {
				xIdent = ident
				return false
			}
			return true
		})
		require.NotNil(t, xIdent)

		path, _ := PathEnclosingInterval(astFile, xIdent.Pos(), xIdent.End())
		block := EnclosingNode[*ast.BlockStmt](path)
		require.NotNil(t, block)
		assert.Len(t, block.List, 1)
	})

	t.Run("FunctionLiteral", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
func test() {
	f := func() {
		return 42
	}
	_ = f
}
`)
		require.NoError(t, err)

		var lit *ast.BasicLit
		ast.Inspect(astFile, func(n ast.Node) bool {
			if bl, ok := n.(*ast.BasicLit); ok && bl.Value == "42" {
				lit = bl
				return false
			}
			return true
		})
		require.NotNil(t, lit)

		path, _ := PathEnclosingInterval(astFile, lit.Pos(), lit.End())
		funcLit := EnclosingNode[*ast.FuncLit](path)
		require.NotNil(t, funcLit)
		assert.NotNil(t, funcLit.Body)
	})

	t.Run("ForStatement", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
func test() {
	for i := 0; i < 10; i++ {
		println(i)
	}
}
`)
		require.NoError(t, err)

		var printlnCall *ast.CallExpr
		ast.Inspect(astFile, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "println" {
					printlnCall = call
					return false
				}
			}
			return true
		})
		require.NotNil(t, printlnCall)

		path, _ := PathEnclosingInterval(astFile, printlnCall.Pos(), printlnCall.End())
		forStmt := EnclosingNode[*ast.ForStmt](path)
		require.NotNil(t, forStmt)
		assert.NotNil(t, forStmt.Init)
		assert.NotNil(t, forStmt.Cond)
		assert.NotNil(t, forStmt.Post)
	})

	t.Run("SwitchStatement", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
func test(x int) {
	switch x {
	case 1:
		println("one")
	case 2:
		println("two")
	}
}
`)
		require.NoError(t, err)

		var lit *ast.BasicLit
		ast.Inspect(astFile, func(n ast.Node) bool {
			if bl, ok := n.(*ast.BasicLit); ok && bl.Value == `"one"` {
				lit = bl
				return false
			}
			return true
		})
		require.NotNil(t, lit)

		path, _ := PathEnclosingInterval(astFile, lit.Pos(), lit.End())
		switchStmt := EnclosingNode[*ast.SwitchStmt](path)
		require.NotNil(t, switchStmt)
		assert.NotNil(t, switchStmt.Tag)
	})

	t.Run("NotFound", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `var x = 1`)
		require.NoError(t, err)

		path, _ := PathEnclosingInterval(astFile, astFile.Pos(), astFile.End())
		// Look for a return statement in a file without any functions
		ret := EnclosingNode[*ast.ReturnStmt](path)
		assert.Nil(t, ret)
	})

	t.Run("EmptyPath", func(t *testing.T) {
		var emptyPath []ast.Node
		ret := EnclosingNode[*ast.ReturnStmt](emptyPath)
		assert.Nil(t, ret)
	})

	t.Run("NilPath", func(t *testing.T) {
		ret := EnclosingNode[*ast.ReturnStmt](nil)
		assert.Nil(t, ret)
	})
}

func TestEnclosingReturnStmt(t *testing.T) {
	_, astFile, err := newTestFile("main.xgo", `
func foo() string {
	return "ok"
}
`)
	require.NoError(t, err)

	var lit ast.Expr
	ast.Inspect(astFile, func(n ast.Node) bool {
		if bl, ok := n.(*ast.BasicLit); ok {
			lit = bl
			return false
		}
		return true
	})
	require.NotNil(t, lit)

	path, _ := PathEnclosingInterval(astFile, lit.Pos(), lit.End())
	ret := EnclosingReturnStmt(path)
	require.NotNil(t, ret)
	assert.Contains(t, ret.Results, lit)
}

func TestReturnValueIndex(t *testing.T) {
	t.Run("ExactMatch", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
func foo() (string, string) {
	return "left", "right"
}
`)
		require.NoError(t, err)

		var ret *ast.ReturnStmt
		ast.Inspect(astFile, func(n ast.Node) bool {
			if r, ok := n.(*ast.ReturnStmt); ok {
				ret = r
				return false
			}
			return true
		})
		require.NotNil(t, ret)

		assert.Equal(t, 0, ReturnValueIndex(ret, ret.Results[0]))
		assert.Equal(t, 1, ReturnValueIndex(ret, ret.Results[1]))
		assert.Equal(t, -1, ReturnValueIndex(nil, ret.Results[0]))
		assert.Equal(t, -1, ReturnValueIndex(ret, nil))
	})

	t.Run("ContainedExpr", func(t *testing.T) {
		_, astFile, err := newTestFile("main.xgo", `
func foo() (string, string) {
	return ("left"), ("right")
}
`)
		require.NoError(t, err)

		var (
			ret      *ast.ReturnStmt
			leftLit  *ast.BasicLit
			rightLit *ast.BasicLit
		)
		ast.Inspect(astFile, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.ReturnStmt:
				ret = node
			case *ast.BasicLit:
				switch node.Value {
				case `"left"`:
					leftLit = node
				case `"right"`:
					rightLit = node
				}
			}
			return true
		})
		require.NotNil(t, ret)
		require.NotNil(t, leftLit)
		require.NotNil(t, rightLit)

		assert.Equal(t, 0, ReturnValueIndex(ret, leftLit))
		assert.Equal(t, 1, ReturnValueIndex(ret, rightLit))
	})
}

func TestToLowerCamelCase(t *testing.T) {
	t.Run("EmptyString", func(t *testing.T) {
		assert.Equal(t, "", ToLowerCamelCase(""))
	})

	t.Run("SingleCharacterUpper", func(t *testing.T) {
		assert.Equal(t, "a", ToLowerCamelCase("A"))
	})

	t.Run("SingleCharacterLower", func(t *testing.T) {
		assert.Equal(t, "a", ToLowerCamelCase("a"))
	})

	t.Run("PascalCase", func(t *testing.T) {
		assert.Equal(t, "pascalCase", ToLowerCamelCase("PascalCase"))
	})

	t.Run("AlreadyCamelCase", func(t *testing.T) {
		assert.Equal(t, "camelCase", ToLowerCamelCase("camelCase"))
	})

	t.Run("AllUpperCase", func(t *testing.T) {
		assert.Equal(t, "aLLUPPERCASE", ToLowerCamelCase("ALLUPPERCASE"))
	})

	t.Run("MixedCaseWithNumbers", func(t *testing.T) {
		assert.Equal(t, "test123Variable", ToLowerCamelCase("Test123Variable"))
	})

	t.Run("WithSpecialCharacters", func(t *testing.T) {
		assert.Equal(t, "test_Variable", ToLowerCamelCase("Test_Variable"))
	})
}

func TestStringLitOrConstValue(t *testing.T) {
	t.Run("StringLiteral", func(t *testing.T) {
		strLit := &ast.BasicLit{
			Kind:  token.STRING,
			Value: `"literal"`,
		}

		tv := types.TypeAndValue{}
		value, ok := StringLitOrConstValue(strLit, tv)
		assert.True(t, ok)
		assert.Equal(t, "literal", value)
	})

	t.Run("StringConstant", func(t *testing.T) {
		ident := &ast.Ident{Name: "strConst"}
		tv := types.TypeAndValue{Value: constant.MakeString("constant value")}

		value, ok := StringLitOrConstValue(ident, tv)
		assert.True(t, ok)
		assert.Equal(t, "constant value", value)
	})

	t.Run("StringVariable", func(t *testing.T) {
		ident := &ast.Ident{Name: "strVar"}
		tv := types.TypeAndValue{Value: nil} // Variables don't have constant values

		value, ok := StringLitOrConstValue(ident, tv)
		assert.False(t, ok)
		assert.Equal(t, "", value)
	})

	t.Run("NonStringLiteral", func(t *testing.T) {
		intLit := &ast.BasicLit{
			Kind:  token.INT,
			Value: "42",
		}

		tv := types.TypeAndValue{}
		value, ok := StringLitOrConstValue(intLit, tv)
		assert.False(t, ok)
		assert.Equal(t, "", value)
	})

	t.Run("NonStringConstant", func(t *testing.T) {
		ident := &ast.Ident{Name: "intConst"}
		tv := types.TypeAndValue{Value: constant.MakeInt64(42)}

		value, ok := StringLitOrConstValue(ident, tv)
		assert.False(t, ok)
		assert.Equal(t, "", value)
	})

	t.Run("InvalidStringLiteral", func(t *testing.T) {
		invalidLit := &ast.BasicLit{
			Kind:  token.STRING,
			Value: `"invalid\x"`, // Invalid escape sequence.
		}

		tv := types.TypeAndValue{}
		value, ok := StringLitOrConstValue(invalidLit, tv)
		assert.False(t, ok)
		assert.Equal(t, "", value)
	})

	t.Run("UnsupportedExpression", func(t *testing.T) {
		binExpr := &ast.BinaryExpr{
			X:  &ast.BasicLit{Kind: token.STRING, Value: `"hello"`},
			Op: token.ADD,
			Y:  &ast.BasicLit{Kind: token.STRING, Value: `"world"`},
		}

		tv := types.TypeAndValue{}
		value, ok := StringLitOrConstValue(binExpr, tv)
		assert.False(t, ok)
		assert.Equal(t, "", value)
	})

	t.Run("IdentWithNilValue", func(t *testing.T) {
		ident := &ast.Ident{Name: "test"}
		tv := types.TypeAndValue{Value: nil}
		value, ok := StringLitOrConstValue(ident, tv)
		assert.False(t, ok)
		assert.Equal(t, "", value)
	})

	t.Run("IdentWithNonStringConstant", func(t *testing.T) {
		ident := &ast.Ident{Name: "test"}
		tv := types.TypeAndValue{Value: constant.MakeInt64(42)}
		value, ok := StringLitOrConstValue(ident, tv)
		assert.False(t, ok)
		assert.Equal(t, "", value)
	})
}
