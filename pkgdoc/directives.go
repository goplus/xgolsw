package pkgdoc

import (
	goast "go/ast"
	gotypes "go/types"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// DeclarationMetadata records language-service policies for one declaration.
type DeclarationMetadata struct {
	// NotRecommended omits the declaration from proactive symbol recommendations.
	// It does not affect explicit uses or other semantic language services.
	NotRecommended bool `json:",omitempty"`
}

// ParseDirectives reads supported directives from declaration documentation.
// Callers supply only leading Doc groups, never trailing Comment groups. The
// directive must occupy an exact line comment, apart from trailing whitespace.
func ParseDirectives(groups ...*ast.CommentGroup) DeclarationMetadata {
	for _, group := range groups {
		if group == nil {
			continue
		}
		for _, comment := range group.List {
			if strings.TrimRight(comment.Text, " \t\r\n") == "//xgols:notrecommended" {
				return DeclarationMetadata{NotRecommended: true}
			}
		}
	}
	return DeclarationMetadata{}
}

// recordDirectives retains nonempty metadata without changing unannotated
// package archives. Blank identifiers do not declare recommendable symbols.
func (p *PkgDoc) recordDirectives(name string, metadata DeclarationMetadata) {
	if !metadata.NotRecommended || name == "" || name == "_" || strings.HasSuffix(name, "._") {
		return
	}
	if p.Declarations == nil {
		p.Declarations = make(map[string]DeclarationMetadata)
	}
	p.Declarations[name] = metadata
}

// ObjectMetadata resolves an object's actual declaration, independent of its
// displayed aliases, overload label, or the type used to select a member.
func (p *PkgDoc) ObjectMetadata(obj gotypes.Object) DeclarationMetadata {
	obj = types.ObjectOrigin(obj)
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != p.Path {
		return DeclarationMetadata{}
	}
	switch obj := obj.(type) {
	case *gotypes.Func:
		if recv := obj.Signature().Recv(); recv != nil {
			if named, ok := gotypes.Unalias(xgoutil.DerefType(recv.Type())).(*gotypes.Named); ok {
				return p.Declarations[named.Obj().Name()+"."+obj.Name()]
			}
			return DeclarationMetadata{}
		}
	case *gotypes.Var:
		if obj.IsField() {
			return p.fieldMetadata(obj)
		}
	}
	if obj.Parent() != obj.Pkg().Scope() {
		return DeclarationMetadata{}
	}
	return p.Declarations[obj.Name()]
}

// fieldMetadata locates the declaring struct by field identity. Defined types
// and aliases can reuse its fields, so names alone do not identify ownership.
func (p *PkgDoc) fieldMetadata(field *gotypes.Var) DeclarationMetadata {
	for key, metadata := range p.Declarations {
		owner, name, member := strings.Cut(key, ".")
		if !member || name != field.Name() {
			continue
		}
		typ, ok := field.Pkg().Scope().Lookup(owner).(*gotypes.TypeName)
		if !ok {
			continue
		}
		st, ok := typ.Type().Underlying().(*gotypes.Struct)
		if !ok {
			continue
		}
		for candidate := range st.Fields() {
			if candidate.Origin() == field {
				return metadata
			}
		}
	}
	return DeclarationMetadata{}
}

// collectGoDirectives reads raw Go comments before go/doc normalizes them.
func (p *PkgDoc) collectGoDirectives(pkg *goast.Package) {
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *goast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *goast.ValueSpec:
						metadata := ParseDirectives(decl.Doc, spec.Doc)
						for _, name := range spec.Names {
							p.recordDirectives(name.Name, metadata)
						}
					case *goast.TypeSpec:
						p.recordDirectives(spec.Name.Name, ParseDirectives(decl.Doc, spec.Doc))
						switch typ := spec.Type.(type) {
						case *goast.StructType:
							p.collectGoFieldDirectives(spec.Name.Name, typ.Fields)
						case *goast.InterfaceType:
							p.collectGoFieldDirectives(spec.Name.Name, typ.Methods)
						}
					}
				}
			case *goast.FuncDecl:
				name := decl.Name.Name
				if decl.Recv != nil {
					if len(decl.Recv.List) != 1 {
						continue
					}
					owner := goEmbeddedFieldName(decl.Recv.List[0].Type)
					if owner == "" {
						continue
					}
					name = owner + "." + name
				}
				p.recordDirectives(name, ParseDirectives(decl.Doc))
			}
		}
	}
}

// collectGoFieldDirectives records struct fields and named interface methods.
func (p *PkgDoc) collectGoFieldDirectives(owner string, fields *goast.FieldList) {
	for _, field := range fields.List {
		metadata := ParseDirectives(field.Doc)
		if len(field.Names) == 0 {
			if name := goEmbeddedFieldName(field.Type); name != "" {
				p.recordDirectives(owner+"."+name, metadata)
			}
		}
		for _, name := range field.Names {
			p.recordDirectives(owner+"."+name.Name, metadata)
		}
	}
}
