package server

import (
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
)

// recommendationPolicy resolves declaration annotations for one request. Source
// positions belong to this project's ASTs; imported objects use package metadata.
type recommendationPolicy struct {
	proj         *xgo.Project
	lookupPkgDoc func(string) (*pkgdoc.PkgDoc, error)
	files        map[*ast.File]map[token.Pos]bool
	objects      map[gotypes.Object]bool
}

// newRecommendationPolicy creates a policy without changing semantic lookup.
func newRecommendationPolicy(proj *xgo.Project, lookupPkgDoc func(string) (*pkgdoc.PkgDoc, error)) *recommendationPolicy {
	return &recommendationPolicy{
		proj: proj, lookupPkgDoc: lookupPkgDoc,
		files: make(map[*ast.File]map[token.Pos]bool), objects: make(map[gotypes.Object]bool),
	}
}

// notRecommended reports whether the actual declaration opts out of suggestions.
// Missing import metadata leaves existing recommendation behavior unchanged.
func (p *recommendationPolicy) notRecommended(obj gotypes.Object) bool {
	if obj == nil {
		return false
	}
	info, _ := p.proj.TypeInfo()
	if info != nil {
		obj = info.ObjectDeclaration(obj)
	} else {
		obj = types.ObjectOrigin(obj)
	}
	if marked, ok := p.objects[obj]; ok {
		return marked
	}
	var marked bool
	if file, pos := objectSource(p.proj, obj); file != nil {
		marked = p.sourceNotRecommended(file, pos)
	} else if obj.Pkg() != nil && (info == nil || obj.Pkg() != info.Pkg) && p.lookupPkgDoc != nil {
		if doc, _ := p.lookupPkgDoc(obj.Pkg().Path()); doc != nil {
			marked = doc.ObjectMetadata(obj).NotRecommended
		}
	}
	p.objects[obj] = marked
	return marked
}

// recommendsDefinition checks both an overload's source alias and its concrete
// implementation, retaining unmarked declarations that share a display name.
func (p *recommendationPolicy) recommendsDefinition(def symbolDefinition) bool {
	return !p.notRecommended(def.SourceObject) && (def.Function == nil || !p.notRecommended(def.Function))
}

// sourceNotRecommended indexes only directly annotated declarations. A group
// annotation applies to its specs, but a type or function annotation does not
// propagate to members or declarations nested inside its body.
func (p *recommendationPolicy) sourceNotRecommended(file *ast.File, pos token.Pos) bool {
	marked, ok := p.files[file]
	if !ok {
		marked = make(map[token.Pos]bool)
		add := func(names []*ast.Ident, groups ...*ast.CommentGroup) {
			if pkgdoc.ParseDirectives(groups...).NotRecommended {
				for _, name := range names {
					marked[name.Pos()] = true
				}
			}
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.GenDecl:
				for _, spec := range node.Specs {
					switch spec := spec.(type) {
					case *ast.ValueSpec:
						add(spec.Names, node.Doc, spec.Doc)
					case *ast.TypeSpec:
						add([]*ast.Ident{spec.Name}, node.Doc, spec.Doc)
					}
				}
			case *ast.TypeSpec:
				var fields *ast.FieldList
				switch typ := node.Type.(type) {
				case *ast.StructType:
					fields = typ.Fields
				case *ast.InterfaceType:
					fields = typ.Methods
				}
				if fields != nil {
					for _, field := range fields.List {
						add(field.Names, field.Doc)
						if len(field.Names) == 0 && pkgdoc.ParseDirectives(field.Doc).NotRecommended {
							marked[embeddedFieldPosition(field.Type)] = true
						}
					}
				}
			case *ast.FuncDecl:
				add([]*ast.Ident{node.Name}, node.Doc)
			case *ast.OverloadFuncDecl:
				add([]*ast.Ident{node.Name}, node.Doc)
			}
			return true
		})
		p.files[file] = marked
	}
	return marked[pos]
}

// embeddedFieldPosition finds an embedded field's source identifier, preserving
// its own declaration position rather than the embedded type's declaration.
func embeddedFieldPosition(expr ast.Expr) token.Pos {
	for {
		switch typ := expr.(type) {
		case *ast.Ident:
			return typ.Pos()
		case *ast.SelectorExpr:
			return typ.Sel.Pos()
		case *ast.StarExpr:
			expr = typ.X
		case *ast.IndexExpr:
			expr = typ.X
		case *ast.IndexListExpr:
			expr = typ.X
		default:
			return token.NoPos
		}
	}
}
