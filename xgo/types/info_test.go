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

package types

import (
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInfoRefIdentsFor(t *testing.T) {
	xVar := gotypes.NewVar(0, nil, "x", gotypes.Typ[gotypes.Int])
	yVar := gotypes.NewVar(0, nil, "y", gotypes.Typ[gotypes.Int])

	xDef := &ast.Ident{Name: "x"}
	xUse1 := &ast.Ident{Name: "x"}
	xUse2 := &ast.Ident{Name: "x"}
	xUse3 := &ast.Ident{Name: "x"}
	yDef := &ast.Ident{Name: "y"}
	yUse := &ast.Ident{Name: "y"}

	info := &Info{
		Info: typesutil.Info{
			Defs: map[*ast.Ident]gotypes.Object{
				xDef: xVar,
				yDef: yVar,
			},
			Uses: map[*ast.Ident]gotypes.Object{
				xUse1: xVar,
				xUse2: xVar,
				xUse3: xVar,
				yUse:  yVar,
			},
		},
		Pkg: gotypes.NewPackage("test", "test"),
		ObjToDef: map[gotypes.Object]*ast.Ident{
			xVar: xDef,
			yVar: yDef,
		},
	}

	t.Run("FindReferences", func(t *testing.T) {
		refs := info.RefIdentsFor(xVar)
		require.Len(t, refs, 3)
		for _, ref := range refs {
			assert.Equal(t, "x", ref.Name)
			assert.Contains(t, []*ast.Ident{xUse1, xUse2, xUse3}, ref)
		}

		refs = info.RefIdentsFor(yVar)
		require.Len(t, refs, 1)
		assert.Equal(t, yUse, refs[0])
	})

	t.Run("NoReferences", func(t *testing.T) {
		zVar := gotypes.NewVar(0, nil, "z", gotypes.Typ[gotypes.Int])
		info.ObjToDef[zVar] = &ast.Ident{Name: "z"}

		refs := info.RefIdentsFor(zVar)
		assert.Empty(t, refs)
	})

	t.Run("NilObject", func(t *testing.T) {
		assert.Nil(t, info.RefIdentsFor(nil))
	})

	t.Run("UnknownObject", func(t *testing.T) {
		unknownObj := gotypes.NewVar(0, nil, "unknown", gotypes.Typ[gotypes.Int])

		refs := info.RefIdentsFor(unknownObj)
		assert.Empty(t, refs)
	})

	t.Run("DeclarationRecordedAsUse", func(t *testing.T) {
		obj := gotypes.NewVar(1, nil, "value", gotypes.Typ[gotypes.Int])
		def := &ast.Ident{NamePos: 1, Name: "value"}
		use := &ast.Ident{NamePos: 10, Name: "value"}
		info := &Info{
			Info: typesutil.Info{
				Defs: map[*ast.Ident]gotypes.Object{def: obj},
				Uses: map[*ast.Ident]gotypes.Object{def: obj, use: obj},
			},
			ObjToDef: map[gotypes.Object]*ast.Ident{obj: def},
		}
		assert.Equal(t, []*ast.Ident{use}, info.RefIdentsFor(obj))
	})

	t.Run("GenericMembers", func(t *testing.T) {
		fset := token.NewFileSet()
		file, err := goparser.ParseFile(fset, "fixture.go", `package fixture
type Box[T any] struct { Value T }
func (b Box[T]) Get() T { return b.Value }
type Other[T any] struct { Value T }
func (b Other[T]) Get() T { return b.Value }
`, 0)
		require.NoError(t, err)
		pkg, err := new(gotypes.Config).Check("example.com/fixture", fset, []*goast.File{file}, nil)
		require.NoError(t, err)
		box := pkg.Scope().Lookup("Box").Type()
		intBox, err := gotypes.Instantiate(nil, box, []gotypes.Type{gotypes.Typ[gotypes.Int]}, true)
		require.NoError(t, err)
		stringBox, err := gotypes.Instantiate(nil, box, []gotypes.Type{gotypes.Typ[gotypes.String]}, true)
		require.NoError(t, err)
		other, err := gotypes.Instantiate(nil, pkg.Scope().Lookup("Other").Type(), []gotypes.Type{gotypes.Typ[gotypes.Int]}, true)
		require.NoError(t, err)

		for _, name := range []string{"Value", "Get"} {
			t.Run(name, func(t *testing.T) {
				origin, _, _ := gotypes.LookupFieldOrMethod(box, true, pkg, name)
				intMember, _, _ := gotypes.LookupFieldOrMethod(intBox, true, pkg, name)
				stringMember, _, _ := gotypes.LookupFieldOrMethod(stringBox, true, pkg, name)
				otherMember, _, _ := gotypes.LookupFieldOrMethod(other, true, pkg, name)
				require.NotNil(t, origin)
				require.NotNil(t, intMember)
				require.NotNil(t, stringMember)
				require.NotNil(t, otherMember)
				intUse := &ast.Ident{NamePos: 1, Name: name}
				stringUse := &ast.Ident{NamePos: 2, Name: name}
				otherUse := &ast.Ident{NamePos: 3, Name: name}
				info := &Info{Info: typesutil.Info{Uses: map[*ast.Ident]gotypes.Object{
					intUse: intMember, stringUse: stringMember, otherUse: otherMember,
				}}}
				for _, target := range []gotypes.Object{origin, intMember, stringMember} {
					assert.ElementsMatch(t, []*ast.Ident{intUse, stringUse}, info.RefIdentsFor(target))
				}
				assert.Equal(t, []*ast.Ident{otherUse}, info.RefIdentsFor(otherMember))
			})
		}
	})
}

func TestInfoSourceObjectOf(t *testing.T) {
	t.Run("Overload", func(t *testing.T) {
		signature := gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
		wrapper := gotypes.NewFunc(1, nil, "Read", signature)
		candidate := gotypes.NewFunc(10, nil, "readInt", signature)
		wrapperDef := &ast.Ident{NamePos: 1, Name: "Read"}
		candidateDef := &ast.Ident{NamePos: 10, Name: "readInt"}
		wrapperUse := &ast.Ident{NamePos: 20, Name: "Read"}
		candidateUse := &ast.Ident{NamePos: 30, Name: "readInt"}
		info := &Info{
			Info: typesutil.Info{
				Defs:      map[*ast.Ident]gotypes.Object{wrapperDef: wrapper, candidateDef: candidate},
				Uses:      map[*ast.Ident]gotypes.Object{wrapperUse: candidate, candidateUse: candidate},
				Overloads: map[*ast.Ident]gotypes.Object{wrapperUse: wrapper},
			},
			ObjToDef: map[gotypes.Object]*ast.Ident{wrapper: wrapperDef, candidate: candidateDef},
		}
		assert.Same(t, wrapper, info.SourceObjectOf(wrapperUse))
		assert.Same(t, candidate, info.ObjectOf(wrapperUse))
		assert.Same(t, wrapper, info.SourceObjectOf(wrapperDef))
		assert.Same(t, candidate, info.SourceObjectOf(candidateUse))
		assert.Equal(t, []*ast.Ident{wrapperUse}, info.RefIdentsFor(wrapper))
		assert.Equal(t, []*ast.Ident{candidateUse}, info.RefIdentsFor(candidate))
		assert.Nil(t, info.SourceObjectOf(&ast.Ident{Name: "missing"}))
	})
	t.Run("EmbeddedField", func(t *testing.T) {
		named := gotypes.NewNamed(gotypes.NewTypeName(1, nil, "Record", nil), gotypes.NewStruct(nil, nil), nil)
		field := gotypes.NewField(10, nil, "Record", named, true)
		typeDef := &ast.Ident{NamePos: 1, Name: "Record"}
		embedded := &ast.Ident{NamePos: 10, Name: "Record"}
		selection := &ast.Ident{NamePos: 20, Name: "Record"}
		info := &Info{
			Info: typesutil.Info{
				Defs: map[*ast.Ident]gotypes.Object{typeDef: named.Obj(), embedded: field},
				Uses: map[*ast.Ident]gotypes.Object{embedded: named.Obj(), selection: field},
			},
			ObjToDef: map[gotypes.Object]*ast.Ident{named.Obj(): typeDef, field: embedded},
		}
		assert.Same(t, field, info.SourceObjectOf(embedded))
		assert.Same(t, field, info.SourceObjectOf(selection))
		assert.Same(t, named.Obj(), info.SourceObjectOf(typeDef))
		assert.Equal(t, []*ast.Ident{embedded}, info.RefIdentsFor(named.Obj()))
		assert.Equal(t, []*ast.Ident{selection}, info.RefIdentsFor(field))
	})
}
