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
	"maps"
	"slices"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// NewXGo creates a new [PkgDoc] for an XGo package. It uses the files' classfile
// flags to resolve generated type names. Class fields and implicit methods are
// omitted when their class cannot be resolved.
//
// lookupClass finds the class framework registered for a file extension.
// It may be nil for packages that need no framework registration.
func NewXGo(pkgPath string, pkg *ast.Package, lookupClass func(ext string) (*modfile.Project, bool)) *PkgDoc {
	return newXGo(pkgPath, pkg, func(file *ast.File, filename string) string {
		return xgoutil.SourceClassName(file, filename, lookupClass)
	})
}

// NewCheckedXGo creates a [PkgDoc] using the compiler-resolved class owners.
func NewCheckedXGo(pkgPath string, pkg *ast.Package, className func(*ast.File) string) *PkgDoc {
	return newXGo(pkgPath, pkg, func(file *ast.File, _ string) string {
		return className(file)
	})
}

// newXGo builds source documentation using the supplied class name resolver.
func newXGo(pkgPath string, pkg *ast.Package, className func(*ast.File, string) string) *PkgDoc {
	pkgDoc := &PkgDoc{
		Path:   pkgPath,
		Name:   pkg.Name,
		Vars:   make(map[string]string),
		Consts: make(map[string]string),
		Types:  make(map[string]*TypeDoc),
		Funcs:  make(map[string]string),
	}
	pkgDoc.collectXGoDirectives(pkg, lookupClass)

	filenames := slices.Sorted(maps.Keys(pkg.Files))
	// Select the first documented file in source filename order.
	for _, filename := range filenames {
		astFile := pkg.Files[filename]
		if astFile.Doc != nil {
			pkgDoc.Doc = astFile.Doc.Text()
			break
		}
	}

	for _, filename := range filenames {
		astFile := pkg.Files[filename]
		var classTypeName string
		var classTypeDoc *TypeDoc
		if astFile.IsClass {
			classTypeName = className(astFile, filename)
			if classTypeName != "" {
				classTypeDoc = pkgDoc.typeDoc(classTypeName)
			}
		}

		for _, decl := range astFile.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				if decl == astFile.ClassFields && classTypeDoc == nil {
					continue
				}
				for _, spec := range decl.Specs {
					var doc string
					switch spec := spec.(type) {
					case *ast.ValueSpec:
						doc = spec.Doc.Text()
					case *ast.TypeSpec:
						doc = spec.Doc.Text()
					case *ast.ImportSpec:
						doc = spec.Doc.Text()
					}
					if doc == "" && decl.Doc != nil && len(decl.Specs) == 1 {
						doc = decl.Doc.Text()
					}

					switch spec := spec.(type) {
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							switch decl.Tok {
							case token.VAR:
								if decl == astFile.ClassFields {
									classTypeDoc.Fields[name.Name] = doc
								} else {
									pkgDoc.Vars[name.Name] = doc
								}
							case token.CONST:
								pkgDoc.Consts[name.Name] = doc
							}
						}
					case *ast.TypeSpec:
						typeDoc := pkgDoc.typeDoc(spec.Name.Name)
						typeDoc.Doc = doc
						switch typ := spec.Type.(type) {
						case *ast.StructType:
							for _, field := range typ.Fields.List {
								fieldDoc := field.Doc.Text()
								if fieldDoc == "" {
									fieldDoc = field.Comment.Text()
								}

								if len(field.Names) == 0 {
									if name := embeddedFieldName(field.Type); name != "" {
										typeDoc.Fields[name] = fieldDoc
									}
									continue
								}
								for _, name := range field.Names {
									typeDoc.Fields[name.Name] = fieldDoc
								}
							}
						case *ast.InterfaceType:
							for _, method := range typ.Methods.List {
								for _, name := range method.Names {
									typeDoc.Methods[name.Name] = method.Doc.Text()
								}
							}
						case *ast.EnumType:
							for _, enumSpec := range typ.Specs {
								valueSpec := enumSpec.(*ast.ValueSpec)
								var valueDoc string
								if valueSpec.Doc != nil {
									valueDoc = valueSpec.Doc.Text()
								}
								for _, name := range valueSpec.Names {
									if name.Name == "_" {
										continue
									}
									if typeDoc.EnumMembers == nil {
										typeDoc.EnumMembers = make(map[string]string)
									}
									typeDoc.EnumMembers[name.Name] = valueDoc
								}
							}
						}
					}
				}
			case *ast.FuncDecl:
				if decl.Shadow {
					continue
				}

				doc := decl.Doc.Text()
				if decl.Static {
					owner := classTypeName
					if !decl.IsClass && len(decl.Recv.List) != 0 {
						recv, ok := decl.Recv.List[0].Type.(*ast.Ident)
						if !ok {
							continue
						}
						owner = recv.Name
					}
					if owner != "" {
						name := decl.Name.Name
						pkgDoc.typeDoc(owner).Methods[name] = doc
						pkgDoc.Funcs[xgoutil.XGosMemberName(owner, name)] = doc
					}
					continue
				}

				funcDocs := pkgDoc.Funcs
				if decl.Recv != nil {
					owner := methodReceiverName(decl.Recv)
					if owner == "" {
						continue
					}
					funcDocs = pkgDoc.typeDoc(owner).Methods
				} else if astFile.IsClass || astFile.ClassFields != nil {
					if classTypeDoc == nil {
						continue
					}
					funcDocs = classTypeDoc.Methods
				}
				funcDocs[decl.Name.Name] = doc
			case *ast.OverloadFuncDecl:
				doc := decl.Doc.Text()
				switch {
				case decl.IsClass || decl.Recv == nil && (astFile.IsClass || astFile.ClassFields != nil):
					if classTypeDoc != nil {
						classTypeDoc.Methods[decl.Name.Name] = doc
					}
				case decl.Recv == nil:
					pkgDoc.Funcs[decl.Name.Name] = doc
				default:
					if owner := methodReceiverName(decl.Recv); owner != "" {
						pkgDoc.typeDoc(owner).Methods[decl.Name.Name] = doc
					}
				}
			}
		}
	}

	return pkgDoc
}

// methodReceiverName returns the local type named by a method receiver.
func methodReceiverName(recv *ast.FieldList) string {
	if len(recv.List) == 1 {
		typ := astutil.Unparen(recv.List[0].Type)
		if ptr, ok := typ.(*ast.StarExpr); ok {
			typ = astutil.Unparen(ptr.X)
		}
		if ident, ok := typ.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return ""
}

// embeddedFieldName returns the field name of an embedded XGo type.
func embeddedFieldName(expr ast.Expr) string {
	for {
		switch typ := expr.(type) {
		case *ast.Ident:
			return typ.Name
		case *ast.SelectorExpr:
			return typ.Sel.Name
		case *ast.StarExpr:
			expr = typ.X
		case *ast.IndexExpr:
			expr = typ.X
		case *ast.IndexListExpr:
			expr = typ.X
		default:
			return ""
		}
	}
}
