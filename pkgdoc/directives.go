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
			if named, ok := gotypes.Unalias(xgoutil.DerefType(gotypes.Unalias(recv.Type()))).(*gotypes.Named); ok {
				return p.Declarations[named.Obj().Name()+"."+obj.Name()]
			}
			return p.memberMetadata(obj)
		}
	case *gotypes.Var:
		if obj.IsField() {
			return p.memberMetadata(obj)
		}
	}
	if obj.Parent() != obj.Pkg().Scope() {
		return DeclarationMetadata{}
	}
	return p.Declarations[obj.Name()]
}

// memberMetadata matches the actual field or explicit interface method. A
// shared underlying type can expose the same declaration through several names.
func (p *PkgDoc) memberMetadata(obj gotypes.Object) DeclarationMetadata {
	for key, metadata := range p.Declarations {
		owner, name, member := strings.Cut(key, ".")
		if !member || name != obj.Name() {
			continue
		}
		typ, ok := obj.Pkg().Scope().Lookup(owner).(*gotypes.TypeName)
		if !ok {
			continue
		}
		switch underlying := typ.Type().Underlying().(type) {
		case *gotypes.Struct:
			for field := range underlying.Fields() {
				if field.Origin() == obj {
					return metadata
				}
			}
		case *gotypes.Interface:
			for method := range underlying.ExplicitMethods() {
				if method.Origin() == obj {
					return metadata
				}
			}
		}
	}
	return DeclarationMetadata{}
}

// collectGoDirectives reads raw Go comments before go/doc normalizes them.
func (p *PkgDoc) collectGoDirectives(pkg *goast.Package) {
	typesByName := goTypesByName(pkg)
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
				metadata := ParseDirectives(decl.Doc)
				if !metadata.NotRecommended {
					continue
				}
				name := decl.Name.Name
				if decl.Recv != nil {
					if len(decl.Recv.List) != 1 {
						continue
					}
					owner := goEmbeddedFieldName(decl.Recv.List[0].Type)
					if owner == "" {
						continue
					}
					name = goDirectiveReceiverName(owner, typesByName) + "." + name
				}
				p.recordDirectives(name, metadata)
			}
		}
	}
	p.recordGoDerivedMembers(typesByName)
}

// goTypesByName indexes the package's type declarations for directive lookup.
func goTypesByName(pkg *goast.Package) map[string]*goast.TypeSpec {
	typesByName := make(map[string]*goast.TypeSpec)
	for _, file := range pkg.Files {
		for _, decl := range file.Decls {
			if decl, ok := decl.(*goast.GenDecl); ok {
				for _, spec := range decl.Specs {
					if spec, ok := spec.(*goast.TypeSpec); ok {
						typesByName[spec.Name.Name] = spec
					}
				}
			}
		}
	}
	return typesByName
}

// goDirectiveReceiverName follows receiver aliases to their declared type.
func goDirectiveReceiverName(name string, typesByName map[string]*goast.TypeSpec) string {
	for range len(typesByName) {
		spec := typesByName[name]
		if spec == nil || !spec.Assign.IsValid() {
			break
		}
		typ := goast.Unparen(spec.Type)
		if ptr, ok := typ.(*goast.StarExpr); ok {
			typ = ptr.X
		}
		if target := goLocalTypeName(typ); target != "" {
			name = target
		} else {
			break
		}
	}
	return name
}

// recordGoDerivedMembers copies member annotations to exported names whose
// underlying declarations may no longer be reachable by name after Go export.
func (p *PkgDoc) recordGoDerivedMembers(typesByName map[string]*goast.TypeSpec) {
	if len(p.Declarations) == 0 {
		return
	}
	for name, spec := range typesByName {
		if !goast.IsExported(name) {
			continue
		}
		for owner, steps := goLocalTypeName(spec.Type), 0; owner != "" && steps < len(typesByName); steps++ {
			base := typesByName[owner]
			if base == nil {
				break
			}
			if !goast.IsExported(owner) {
				switch typ := base.Type.(type) {
				case *goast.StructType:
					p.recordGoMemberAliases(name, owner, typ.Fields)
				case *goast.InterfaceType:
					p.recordGoMemberAliases(name, owner, typ.Methods)
				}
			}
			owner = goLocalTypeName(base.Type)
		}
	}
}

// recordGoMemberAliases carries annotations on the actual members of a type.
func (p *PkgDoc) recordGoMemberAliases(name, owner string, fields *goast.FieldList) {
	for _, field := range fields.List {
		if len(field.Names) == 0 {
			if embedded := goEmbeddedFieldName(field.Type); embedded != "" {
				p.recordDirectives(name+"."+embedded, p.Declarations[owner+"."+embedded])
			}
		}
		for _, field := range field.Names {
			p.recordDirectives(name+"."+field.Name, p.Declarations[owner+"."+field.Name])
		}
	}
}

// goLocalTypeName returns an unqualified reference to a type, including a
// reference instantiated with type arguments.
func goLocalTypeName(expr goast.Expr) string {
	for {
		switch typ := expr.(type) {
		case *goast.Ident:
			return typ.Name
		case *goast.IndexExpr:
			expr = typ.X
		case *goast.IndexListExpr:
			expr = typ.X
		case *goast.ParenExpr:
			expr = typ.X
		default:
			return ""
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
