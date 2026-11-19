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
	"cmp"
	goast "go/ast"
	godoc "go/doc"
	"maps"
	"slices"
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

	// Declarations records language-service directives by declaration key.
	// Package-level keys use declaration names, and member keys use Type.Member.
	// Names retain their original overload spelling. Receiver aliases use the
	// resolved type name. Member entries may also appear under exported types
	// derived from unexported types. Static methods also use the generated
	// function name as a key.
	Declarations map[string]DeclarationMetadata `json:",omitempty"`
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
	typesByName := goTypesByName(pkg)
	embeddedPrivate := goEmbeddedPrivateTypes(typesByName)
	aliasedPrivate := make(map[string]bool)
	pkgDoc.collectGoDirectives(pkg, typesByName)

	vars, consts, funcs := docPkg.Vars, docPkg.Consts, docPkg.Funcs
	type methodKey struct{ owner, name string }
	type methodComment struct {
		text        string
		hasReceiver bool
	}
	methods := make(map[methodKey]methodComment)
	for _, t := range docPkg.Types {
		// go/doc associates declarations with their type, including exported
		// declarations whose type is unexported.
		vars = append(vars, t.Vars...)
		consts = append(consts, t.Consts...)
		funcs = append(funcs, t.Funcs...)
		var typeDoc *TypeDoc
		public := token.IsExported(t.Name)
		if public || embeddedPrivate[t.Name] {
			typeDoc = pkgDoc.typeDoc(t.Name)
			if public {
				typeDoc.Doc = t.Doc
			}
		}
		owner := goReceiverName(t.Name, typesByName)
		if public && !token.IsExported(owner) {
			aliasedPrivate[owner] = true
		}
		for _, spec := range t.Decl.Specs {
			typeSpec, ok := spec.(*goast.TypeSpec)
			if !ok {
				continue
			}
			if typeDoc == nil {
				continue
			}
			switch typ := goDocumentedMemberType(typeSpec, typesByName).(type) {
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
							methods[methodKey{owner, name.Name}] = methodComment{text: method.Doc.Text()}
						}
					}
				}
			}
		}
		for _, m := range t.Methods {
			if m.Level == 0 {
				methods[methodKey{owner, m.Name}] = methodComment{text: m.Doc, hasReceiver: true}
				if owner != t.Name && typeDoc != nil && token.IsExported(m.Name) {
					pkgDoc.typeDoc(owner)
				}
			}
			if typeDoc != nil && token.IsExported(m.Name) {
				typeDoc.Methods[m.Name] = m.Doc
			}
		}
	}
	// Aliases expose the original receiver's methods. Methods declared on an
	// alias also belong to that receiver and can hide promoted methods.
	// Interface method comments can already be found through the public types.
	for key, comment := range methods {
		if !token.IsExported(key.name) {
			continue
		}
		doc := pkgDoc.Types[key.owner]
		if doc == nil && aliasedPrivate[key.owner] && comment.hasReceiver && comment.text != "" {
			doc = pkgDoc.typeDoc(key.owner)
		}
		if doc != nil {
			doc.Methods[key.name] = comment.text
		}
	}
	// Private receiver types only need entries for comments used by selectors.
	emptyComment := func(_, comment string) bool { return comment == "" }
	for name, doc := range pkgDoc.Types {
		if token.IsExported(name) {
			continue
		}
		maps.DeleteFunc(doc.Fields, emptyComment)
		maps.DeleteFunc(doc.Methods, emptyComment)
		if len(doc.Fields) == 0 && len(doc.Methods) == 0 {
			delete(pkgDoc.Types, name)
		}
	}

	for _, v := range vars {
		for _, name := range v.Names {
			if token.IsExported(name) {
				pkgDoc.Vars[name] = v.Doc
			}
		}
	}
	for _, c := range consts {
		for _, spec := range c.Decl.Specs {
			value := spec.(*goast.ValueSpec)
			for _, name := range value.Names {
				if !name.IsExported() {
					continue
				}
				doc := c.Doc
				if strings.HasPrefix(name.Name, xgoutil.XGooPrefix) {
					// A marker can document its overload within a const group.
					doc = cmp.Or(value.Doc.Text(), doc)
				}
				pkgDoc.Consts[name.Name] = doc
			}
		}
	}
	_, isXGoPackage := pkgDoc.Consts[xgoutil.XGoPackage]
	if _, legacy := pkgDoc.Consts[xgoutil.GopPackage]; legacy && !isXGoPackage {
		_, hasVar := pkgDoc.Vars[xgoutil.XGoPackage]
		_, hasType := pkgDoc.Types[xgoutil.XGoPackage]
		hasFunc := slices.ContainsFunc(funcs, func(f *godoc.Func) bool {
			return f.Name == xgoutil.XGoPackage
		})
		isXGoPackage = !hasVar && !hasType && !hasFunc
	}
	if isXGoPackage {
		// Gogen reconstructs members in package scope order. An existing
		// direct method takes precedence, while promoted methods can be hidden.
		slices.SortFunc(funcs, func(a, b *godoc.Func) int {
			return cmp.Compare(a.Name, b.Name)
		})
	}
	for _, f := range funcs {
		if !token.IsExported(f.Name) {
			continue
		}
		pkgDoc.Funcs[f.Name] = f.Doc
		if !isXGoPackage {
			continue
		}
		owner, method, ok := xgoutil.SplitXGotMethodName(f.Name, true)
		if !ok {
			owner, method, ok = xgoutil.SplitXGosMemberName(f.Name)
		}
		key := methodKey{owner, method}
		decl := typesByName[owner]
		if _, claimed := methods[key]; ok && decl != nil && !decl.Assign.IsValid() && !claimed {
			pkgDoc.typeDoc(owner).Methods[method] = f.Doc
			methods[key] = methodComment{text: f.Doc}
		}
	}

	return pkgDoc
}

// goEmbeddedPrivateTypes finds package-local private types that supply
// promoted members to exported structs and interfaces.
func goEmbeddedPrivateTypes(typesByName map[string]*goast.TypeSpec) map[string]bool {
	embedded := make(map[string]bool)
	var visitEmbedding func(goast.Expr)
	visitMembers := func(expr goast.Expr) {
		var fields *goast.FieldList
		switch typ := expr.(type) {
		case *goast.StructType:
			fields = typ.Fields
		case *goast.InterfaceType:
			fields = typ.Methods
		default:
			return
		}
		for _, field := range fields.List {
			if len(field.Names) == 0 {
				visitEmbedding(field.Type)
			}
		}
	}
	visitEmbedding = func(expr goast.Expr) {
		if ptr, ok := goast.Unparen(expr).(*goast.StarExpr); ok {
			expr = ptr.X
		}
		name := goLocalTypeName(expr)
		spec := typesByName[name]
		if spec == nil || goast.IsExported(name) || embedded[name] {
			return
		}
		embedded[name] = true
		visitMembers(goast.Unparen(spec.Type))
		visitEmbedding(spec.Type)
	}
	for _, spec := range typesByName {
		if spec.Name.IsExported() {
			visitMembers(goDocumentedMemberType(spec, typesByName))
		}
	}
	return embedded
}

// goDocumentedMemberType finds the declaration supplying a public type's
// members, including fields accessed through a defined pointer or pointer alias.
func goDocumentedMemberType(spec *goast.TypeSpec, typesByName map[string]*goast.TypeSpec) goast.Expr {
	decl := spec
	written := goast.Unparen(spec.Type)
	pointer := false
	for i := 0; spec != nil && i < len(typesByName); i++ {
		expr := goast.Unparen(spec.Type)
		if ptr, ok := expr.(*goast.StarExpr); ok {
			if pointer {
				break
			}
			pointer = true
			expr = goast.Unparen(ptr.X)
		}
		if spec == decl || !spec.Name.IsExported() {
			switch expr.(type) {
			case *goast.StructType:
				return expr
			case *goast.InterfaceType:
				if !pointer {
					return expr
				}
			}
		}
		spec = typesByName[goLocalTypeName(expr)]
	}
	return written
}

// goEmbeddedFieldName returns the field name of an embedded Go type.
func goEmbeddedFieldName(expr goast.Expr) string {
	for {
		switch typ := goast.Unparen(expr).(type) {
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
