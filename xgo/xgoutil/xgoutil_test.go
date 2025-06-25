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
	"slices"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func file(content string) *xgo.File {
	return &xgo.File{Content: []byte(content)}
}

func newTestTypeInfo(defs map[*ast.Ident]types.Object, uses map[*ast.Ident]types.Object) *xgo.TypeInfo {
	if defs == nil {
		defs = make(map[*ast.Ident]types.Object)
	}
	if uses == nil {
		uses = make(map[*ast.Ident]types.Object)
	}
	return &xgo.TypeInfo{
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

func TestRangeASTSpecs(t *testing.T) {
	t.Run("SingleTypeSpec", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("type A = int"),
		}, xgo.FeatAll)

		var specs []ast.Spec
		RangeASTSpecs(proj, token.TYPE, func(spec ast.Spec) {
			specs = append(specs, spec)
		})

		require.Len(t, specs, 1)
		ts := specs[0].(*ast.TypeSpec)
		assert.Equal(t, "A", ts.Name.Name)
		assert.NotEqual(t, token.NoPos, ts.Assign)
	})

	t.Run("MultipleTypeSpecs", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file(`
type (
	A = int
	B = string
	C struct {
		Field int
	}
)
`),
		}, xgo.FeatAll)

		var specs []ast.Spec
		RangeASTSpecs(proj, token.TYPE, func(spec ast.Spec) {
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
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file(`
var (
	x = 1
	y = "hello"
)
`),
		}, xgo.FeatAll)

		var specs []ast.Spec
		RangeASTSpecs(proj, token.VAR, func(spec ast.Spec) {
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
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file(`
const (
	Pi = 3.14
	E  = 2.71
)
`),
		}, xgo.FeatAll)

		var specs []ast.Spec
		RangeASTSpecs(proj, token.CONST, func(spec ast.Spec) {
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
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo":  file("type MainType = int"),
			"other.xgo": file("type OtherType = string"),
		}, xgo.FeatAll)

		var specs []ast.Spec
		RangeASTSpecs(proj, token.TYPE, func(spec ast.Spec) {
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
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file("var x = 1"),
		}, xgo.FeatAll)

		var specs []ast.Spec
		RangeASTSpecs(proj, token.TYPE, func(spec ast.Spec) {
			specs = append(specs, spec)
		})

		assert.Len(t, specs, 0)
	})

	t.Run("EmptyProject", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{}, xgo.FeatAll)

		var specs []ast.Spec
		RangeASTSpecs(proj, token.TYPE, func(spec ast.Spec) {
			specs = append(specs, spec)
		})

		assert.Len(t, specs, 0)
	})

	t.Run("MixedDeclarations", func(t *testing.T) {
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file(`
type MyType = int
var myVar = 1
const myConst = 42
func myFunc() {}
`),
		}, xgo.FeatAll)

		var typeSpecs []ast.Spec
		RangeASTSpecs(proj, token.TYPE, func(spec ast.Spec) {
			typeSpecs = append(typeSpecs, spec)
		})

		var varSpecs []ast.Spec
		RangeASTSpecs(proj, token.VAR, func(spec ast.Spec) {
			varSpecs = append(varSpecs, spec)
		})

		var constSpecs []ast.Spec
		RangeASTSpecs(proj, token.CONST, func(spec ast.Spec) {
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
		proj := xgo.NewProject(nil, map[string]*xgo.File{
			"main.xgo": file(`
import (
	"fmt"
	"strconv"
)
`),
		}, xgo.FeatAll)

		var specs []ast.Spec
		RangeASTSpecs(proj, token.IMPORT, func(spec ast.Spec) {
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
}

func TestIsDefinedInClassFieldsDecl(t *testing.T) {
	proj := xgo.NewProject(nil, map[string]*xgo.File{
		"main.gox": file(`
var (
	x int
	y string
)

func test() {
	z := 1
	println(z)
}
`),
	}, xgo.FeatAll)

	typeInfo, _ := proj.TypeInfo()
	require.NotNil(t, typeInfo)

	// Get objects from definitions
	var xObj, zObj types.Object
	for ident, obj := range typeInfo.Defs {
		switch ident.Name {
		case "x":
			xObj = obj
		case "z":
			zObj = obj
		}
	}
	require.NotNil(t, xObj)
	require.NotNil(t, zObj)

	t.Run("DefinedInClassFields", func(t *testing.T) {
		assert.True(t, IsDefinedInClassFieldsDecl(proj, xObj))
	})

	t.Run("NotDefinedInClassFields", func(t *testing.T) {
		assert.False(t, IsDefinedInClassFieldsDecl(proj, zObj))
	})

	t.Run("NilObject", func(t *testing.T) {
		assert.False(t, IsDefinedInClassFieldsDecl(proj, nil))
	})
}

func TestWalkPathEnclosingInterval(t *testing.T) {
	proj := xgo.NewProject(nil, map[string]*xgo.File{
		"main.xgo": file(`
var x = 1
func test() {
	y := x + 2
	println(y)
}
`),
	}, xgo.FeatAll)

	astFile, err := proj.ASTFile("main.xgo")
	require.NoError(t, err)

	t.Run("WalkFunction", func(t *testing.T) {
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
		var identPos token.Pos
		ast.Inspect(astFile, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok && ident.Name == "x" {
				identPos = ident.Pos()
				return false
			}
			return true
		})
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
		var nodes []ast.Node
		WalkPathEnclosingInterval(astFile, token.NoPos, token.NoPos, false, func(node ast.Node) bool {
			nodes = append(nodes, node)
			return true
		})
		assert.Len(t, nodes, 1) // Should still return at least the file node.
	})

	t.Run("WalkBackward", func(t *testing.T) {
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
	proj := xgo.NewProject(nil, map[string]*xgo.File{
		"main.xgo": file(`
const strConst = "constant value"
const intConst = 42
var strVar = "variable value"

func test() {
	local := "local string"
	println("literal", strConst, strVar, local)
}
`),
	}, xgo.FeatAll)

	typeInfo, _ := proj.TypeInfo()
	require.NotNil(t, typeInfo)

	astFile, err := proj.ASTFile("main.xgo")
	require.NoError(t, err)

	t.Run("StringLiteral", func(t *testing.T) {
		var strLit *ast.BasicLit
		ast.Inspect(astFile, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING && lit.Value == `"literal"` {
				strLit = lit
				return false
			}
			return true
		})
		require.NotNil(t, strLit)

		tv := typeInfo.Types[strLit]
		value, ok := StringLitOrConstValue(strLit, tv)
		assert.True(t, ok)
		assert.Equal(t, "literal", value)
	})

	t.Run("StringConstant", func(t *testing.T) {
		var constIdent *ast.Ident
		ast.Inspect(astFile, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok && ident.Name == "strConst" {
				if obj := typeInfo.Uses[ident]; obj != nil {
					constIdent = ident
					return false
				}
			}
			return true
		})
		require.NotNil(t, constIdent)

		tv := typeInfo.Types[constIdent]
		value, ok := StringLitOrConstValue(constIdent, tv)
		assert.True(t, ok)
		assert.Equal(t, "constant value", value)
	})

	t.Run("StringVariable", func(t *testing.T) {
		var varIdent *ast.Ident
		ast.Inspect(astFile, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok && ident.Name == "strVar" {
				if obj := typeInfo.Uses[ident]; obj != nil {
					varIdent = ident
					return false
				}
			}
			return true
		})
		require.NotNil(t, varIdent)

		tv := typeInfo.Types[varIdent]
		value, ok := StringLitOrConstValue(varIdent, tv)
		assert.False(t, ok)
		assert.Equal(t, "", value)
	})

	t.Run("NonStringLiteral", func(t *testing.T) {
		var intLit *ast.BasicLit
		ast.Inspect(astFile, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.INT && lit.Value == "42" {
				intLit = lit
				return false
			}
			return true
		})
		require.NotNil(t, intLit)

		tv := typeInfo.Types[intLit]
		value, ok := StringLitOrConstValue(intLit, tv)
		assert.False(t, ok)
		assert.Equal(t, "", value)
	})

	t.Run("NonStringConstant", func(t *testing.T) {
		var intIdent *ast.Ident
		ast.Inspect(astFile, func(n ast.Node) bool {
			if ident, ok := n.(*ast.Ident); ok && ident.Name == "intConst" {
				if obj := typeInfo.Uses[ident]; obj != nil {
					intIdent = ident
					return false
				}
			}
			return true
		})

		if intIdent != nil {
			tv := typeInfo.Types[intIdent]
			value, ok := StringLitOrConstValue(intIdent, tv)
			assert.False(t, ok)
			assert.Equal(t, "", value)
		}
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
