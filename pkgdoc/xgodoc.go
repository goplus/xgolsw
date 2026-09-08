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
	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/cl"
	"github.com/goplus/xgo/token"
)

// NewXGo creates a new [PkgDoc] for an XGo package. It uses the files' classfile
// flags to resolve generated type names. Class fields and implicit methods are
// omitted when their class cannot be resolved.
//
// lookupClass resolves classfile extensions using the package's module
// registration. It may be nil when the package contains no framework classfiles.
func NewXGo(pkgPath string, pkg *ast.Package, lookupClass func(ext string) (*modfile.Project, bool)) *PkgDoc {
	pkgDoc := &PkgDoc{
		Path:   pkgPath,
		Name:   pkg.Name,
		Vars:   make(map[string]string),
		Consts: make(map[string]string),
		Types:  make(map[string]*TypeDoc),
		Funcs:  make(map[string]string),
	}

	for _, astFile := range pkg.Files {
		if astFile.Doc != nil {
			pkgDoc.Doc = astFile.Doc.Text()
			break
		}
	}

	for filename, astFile := range pkg.Files {
		var classTypeDoc *TypeDoc
		if astFile.IsClass {
			className, _ := cl.GetFileClassType(astFile, filename, lookupClass)
			if className != "" {
				classTypeDoc = pkgDoc.typeDoc(className)
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
						if spec.Doc != nil {
							doc = spec.Doc.Text()
						}
					case *ast.TypeSpec:
						if spec.Doc != nil {
							doc = spec.Doc.Text()
						}
					case *ast.ImportSpec:
						if spec.Doc != nil {
							doc = spec.Doc.Text()
						}
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
						switch typ := spec.Type.(type) {
						case *ast.StructType:
							typeDoc := pkgDoc.typeDoc(spec.Name.Name)
							typeDoc.Doc = doc
							for _, field := range typ.Fields.List {
								fieldDoc := ""
								if field.Doc != nil {
									fieldDoc = field.Doc.Text()
								}

								if len(field.Names) == 0 {
									ident, ok := field.Type.(*ast.Ident)
									if !ok {
										continue
									}
									typeDoc.Fields[ident.Name] = fieldDoc
								} else {
									for _, name := range field.Names {
										typeDoc.Fields[name.Name] = fieldDoc
									}
								}
							}
						case *ast.EnumType:
							typeDoc := pkgDoc.typeDoc(spec.Name.Name)
							typeDoc.Doc = doc
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

				var doc string
				if decl.Doc != nil {
					doc = decl.Doc.Text()
				}

				funcDocs := pkgDoc.Funcs
				if decl.Recv != nil {
					if len(decl.Recv.List) != 1 {
						continue
					}
					recvType := decl.Recv.List[0].Type
					if star, ok := recvType.(*ast.StarExpr); ok {
						recvType = star.X
					}
					recvIdent, ok := recvType.(*ast.Ident)
					if !ok {
						continue
					}
					funcDocs = pkgDoc.typeDoc(recvIdent.Name).Methods
				} else if astFile.IsClass || astFile.ClassFields != nil {
					if classTypeDoc == nil {
						continue
					}
					funcDocs = classTypeDoc.Methods
				}
				funcDocs[decl.Name.Name] = doc
			}
		}
	}

	return pkgDoc
}
