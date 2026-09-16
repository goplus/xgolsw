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

package pkgdoc

import (
	goast "go/ast"
	godoc "go/doc"
	"strings"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// PkgDoc is the documentation for a package.
type PkgDoc struct {
	Doc    string
	Path   string
	Name   string
	Vars   map[string]string
	Consts map[string]string
	Types  map[string]*TypeDoc
	Funcs  map[string]string
}

// typeDoc returns the documentation for the given type name. It creates a new
// [TypeDoc] if the type name is not found.
func (p *PkgDoc) typeDoc(typeName string) *TypeDoc {
	typeDoc, ok := p.Types[typeName]
	if !ok {
		typeDoc = &TypeDoc{
			Fields:  make(map[string]string),
			Methods: make(map[string]string),
		}
		p.Types[typeName] = typeDoc
	}
	return typeDoc
}

// TypeDoc is the documentation for a type.
type TypeDoc struct {
	Doc         string
	Fields      map[string]string
	Methods     map[string]string
	EnumMembers map[string]string `json:",omitempty"`
}

// NewGo creates a new [PkgDoc] from the given Go [ast.Package].
func NewGo(pkgPath string, pkg *goast.Package) *PkgDoc {
	docPkg := godoc.New(pkg, pkgPath, godoc.AllDecls|godoc.AllMethods|godoc.PreserveAST)
	pkgDoc := &PkgDoc{
		Doc:    docPkg.Doc,
		Path:   pkgPath,
		Name:   pkg.Name,
		Vars:   make(map[string]string),
		Consts: make(map[string]string),
		Types:  make(map[string]*TypeDoc),
		Funcs:  make(map[string]string),
	}

	vars, consts, funcs := docPkg.Vars, docPkg.Consts, docPkg.Funcs
	for _, t := range docPkg.Types {
		// go/doc associates declarations with their type, including exported
		// declarations whose type is unexported.
		vars = append(vars, t.Vars...)
		consts = append(consts, t.Consts...)
		funcs = append(funcs, t.Funcs...)
		if !token.IsExported(t.Name) {
			continue
		}

		typeDoc := pkgDoc.typeDoc(t.Name)
		typeDoc.Doc = t.Doc
		for _, spec := range t.Decl.Specs {
			typeSpec, ok := spec.(*goast.TypeSpec)
			if !ok {
				continue
			}
			switch typ := typeSpec.Type.(type) {
			case *goast.StructType:
				for _, field := range typ.Fields.List {
					doc := field.Doc.Text()
					if doc == "" {
						doc = field.Comment.Text()
					}
					if len(field.Names) == 0 {
						if name := goEmbeddedFieldName(field.Type); token.IsExported(name) {
							typeDoc.Fields[name] = doc
						}
						continue
					}
					for _, name := range field.Names {
						if token.IsExported(name.Name) {
							typeDoc.Fields[name.Name] = doc
						}
					}
				}
			case *goast.InterfaceType:
				for _, method := range typ.Methods.List {
					for _, name := range method.Names {
						if token.IsExported(name.Name) {
							typeDoc.Methods[name.Name] = method.Doc.Text()
						}
					}
				}
			}
		}
		for _, m := range t.Methods {
			if token.IsExported(m.Name) {
				typeDoc.Methods[m.Name] = m.Doc
			}
		}
	}

	for _, v := range vars {
		for _, name := range v.Names {
			if token.IsExported(name) {
				pkgDoc.Vars[name] = v.Doc
			}
		}
	}
	isXGoPackage := false
	for _, c := range consts {
		for _, name := range c.Names {
			if token.IsExported(name) {
				pkgDoc.Consts[name] = c.Doc
				if xgoutil.IsXGoPackageMarkerName(name) {
					isXGoPackage = true
				}
			}
		}
	}
	for _, f := range funcs {
		if !token.IsExported(f.Name) {
			continue
		}
		pkgDoc.Funcs[f.Name] = f.Doc
		if !isXGoPackage {
			continue
		}
		if strings.HasPrefix(f.Name, xgoutil.XGotPrefix) {
			recvTypeName, methodName, ok := xgoutil.SplitXGotMethodName(f.Name, true)
			if !ok {
				continue
			}
			pkgDoc.typeDoc(recvTypeName).Methods[methodName] = f.Doc
		}
	}

	return pkgDoc
}

// goEmbeddedFieldName returns the field name of an embedded Go type.
func goEmbeddedFieldName(expr goast.Expr) string {
	for {
		switch typ := expr.(type) {
		case *goast.Ident:
			return typ.Name
		case *goast.SelectorExpr:
			return typ.Sel.Name
		case *goast.StarExpr:
			expr = typ.X
		case *goast.IndexExpr:
			expr = typ.X
		case *goast.IndexListExpr:
			expr = typ.X
		default:
			return ""
		}
	}
}
