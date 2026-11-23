package pkgdoc

import (
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// collectXGoFileDirectives records directives under the file's resolved class name.
func (p *PkgDoc) collectXGoFileDirectives(file *ast.File, className string, classFields *ast.GenDecl) {
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.GenDecl:
			if skipClassVarDecl(file, decl, classFields) {
				continue
			}
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.ValueSpec:
					metadata := ParseDirectives(decl.Doc, spec.Doc)
					if decl == classFields && len(spec.Names) == 0 {
						if field := xgoutil.EmbeddedFieldIdent(spec.Type); field != nil {
							p.recordDirectives(className+"."+field.Name, metadata)
						}
					}
					for _, name := range spec.Names {
						key := name.Name
						if decl == classFields {
							key = className + "." + key
						}
						p.recordDirectives(key, metadata)
					}
				case *ast.TypeSpec:
					p.recordDirectives(spec.Name.Name, ParseDirectives(decl.Doc, spec.Doc))
					switch typ := xgoutil.DeclaredMemberType(spec).(type) {
					case *ast.StructType:
						p.collectXGoFieldDirectives(spec.Name.Name, typ.Fields)
					case *ast.InterfaceType:
						p.collectXGoFieldDirectives(spec.Name.Name, typ.Methods)
					}
				}
			}
		case *ast.FuncDecl:
			if !decl.Shadow && !decl.Static {
				p.recordXGoFunctionDirectives(file, className, decl.Name.Name, decl.Recv, decl.Doc)
			}
		case *ast.OverloadFuncDecl:
			p.recordXGoFunctionDirectives(file, className, decl.Name.Name, decl.Recv, decl.Doc)
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
		owner := xgoutil.EmbeddedFieldIdent(recv.List[0].Type)
		if owner == nil {
			return
		}
		name = owner.Name + "." + name
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
			if ident := xgoutil.EmbeddedFieldIdent(field.Type); ident != nil {
				p.recordDirectives(owner+"."+ident.Name, metadata)
			}
		}
		for _, name := range field.Names {
			p.recordDirectives(owner+"."+name.Name, metadata)
		}
	}
}
