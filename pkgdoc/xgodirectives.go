package pkgdoc

import (
	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/cl"
)

// collectXGoDirectives records package and class declarations from raw comments.
func (p *PkgDoc) collectXGoDirectives(pkg *ast.Package, lookupClass func(string) (*modfile.Project, bool)) {
	for filename, file := range pkg.Files {
		var className string
		if file.IsClass {
			className, _ = cl.GetFileClassType(file, filename, lookupClass)
		}
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.GenDecl:
				if decl == file.ClassFields && className == "" {
					continue
				}
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.ValueSpec:
						metadata := ParseDirectives(decl.Doc, spec.Doc)
						if decl == file.ClassFields && len(spec.Names) == 0 {
							if name := embeddedFieldName(spec.Type); name != "" {
								p.recordDirectives(className+"."+name, metadata)
							}
						}
						for _, name := range spec.Names {
							key := name.Name
							if decl == file.ClassFields {
								key = className + "." + key
							}
							p.recordDirectives(key, metadata)
						}
					case *ast.TypeSpec:
						p.recordDirectives(spec.Name.Name, ParseDirectives(decl.Doc, spec.Doc))
						switch typ := spec.Type.(type) {
						case *ast.StructType:
							p.collectXGoFieldDirectives(spec.Name.Name, typ.Fields)
						case *ast.InterfaceType:
							p.collectXGoFieldDirectives(spec.Name.Name, typ.Methods)
						}
					}
				}
			case *ast.FuncDecl:
				if !decl.Shadow {
					p.recordXGoFunctionDirectives(file, className, decl.Name.Name, decl.Recv, decl.Doc)
				}
			case *ast.OverloadFuncDecl:
				p.recordXGoFunctionDirectives(file, className, decl.Name.Name, decl.Recv, decl.Doc)
			}
		}
	}
}

// recordXGoFunctionDirectives keeps explicit and implicit class receivers in
// declaration names, including overload declarations and generic receivers.
func (p *PkgDoc) recordXGoFunctionDirectives(file *ast.File, className, name string, recv *ast.FieldList, doc *ast.CommentGroup) {
	if recv != nil {
		if len(recv.List) != 1 {
			return
		}
		owner := embeddedFieldName(recv.List[0].Type)
		if owner == "" {
			return
		}
		name = owner + "." + name
	} else if file.IsClass || file.ClassFields != nil {
		if className == "" {
			return
		}
		name = className + "." + name
	}
	p.recordDirectives(name, ParseDirectives(doc))
}

// collectXGoFieldDirectives records struct fields and named interface methods.
func (p *PkgDoc) collectXGoFieldDirectives(owner string, fields *ast.FieldList) {
	for _, field := range fields.List {
		metadata := ParseDirectives(field.Doc)
		if len(field.Names) == 0 {
			if name := embeddedFieldName(field.Type); name != "" {
				p.recordDirectives(owner+"."+name, metadata)
			}
		}
		for _, name := range field.Names {
			p.recordDirectives(owner+"."+name.Name, metadata)
		}
	}
}
