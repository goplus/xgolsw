package server

import (
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// notRecommendedDeclarations tracks declarations marked not recommended for one
// request. It checks source positions in the project and package metadata for imports.
type notRecommendedDeclarations struct {
	proj         *xgo.Project
	lookupPkgDoc func(string) (*pkgdoc.PkgDoc, error)
	files        map[*ast.File]map[token.Pos]bool
	objects      map[gotypes.Object]bool
}

// newNotRecommendedDeclarations creates a request-scoped lookup for the directive.
func newNotRecommendedDeclarations(proj *xgo.Project, lookupPkgDoc func(string) (*pkgdoc.PkgDoc, error)) *notRecommendedDeclarations {
	return &notRecommendedDeclarations{
		proj: proj, lookupPkgDoc: lookupPkgDoc,
		files: make(map[*ast.File]map[token.Pos]bool), objects: make(map[gotypes.Object]bool),
	}
}

// hasObject reports whether the object's declaration is marked not recommended.
func (p *notRecommendedDeclarations) hasObject(obj gotypes.Object) bool {
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
		marked = p.hasSourcePosition(file, pos)
	} else if obj.Pkg() != nil && (info == nil || obj.Pkg() != info.Pkg) && p.lookupPkgDoc != nil {
		if doc, _ := p.lookupPkgDoc(obj.Pkg().Path()); doc != nil {
			marked = doc.ObjectMetadata(obj).NotRecommended
		}
	}
	p.objects[obj] = marked
	return marked
}

// hasDefinition reports whether the source declaration, callable
// implementation, or selected type overload is marked not recommended.
func (p *notRecommendedDeclarations) hasDefinition(def symbolDefinition) bool {
	if p.hasObject(def.SourceObject) || p.hasObject(def.Implementation) {
		return true
	}
	if source, ok := def.SourceObject.(*gotypes.TypeName); ok {
		if selected, ok := def.TypeHint.(*gotypes.Named); ok && selected.Obj() != source {
			return p.hasObject(selected.Obj())
		}
	}
	return false
}

// hasSourcePosition looks up a declaration position in the snapshot's index.
func (p *notRecommendedDeclarations) hasSourcePosition(file *ast.File, pos token.Pos) bool {
	marked, ok := p.files[file]
	if !ok {
		data, err := p.proj.FileCache(notRecommendedFileCacheKind{}, p.proj.Fset.File(pos).Name())
		if err != nil {
			return false
		}
		marked = data.(map[token.Pos]bool)
		p.files[file] = marked
	}
	return marked[pos]
}

// notRecommendedFileCacheKind identifies the directive index for one file.
type notRecommendedFileCacheKind struct{}

// buildNotRecommendedFileCache indexes declaration positions in the current AST.
func buildNotRecommendedFileCache(proj *xgo.Project, path string, _ *xgo.File) (any, error) {
	file, err := proj.ASTFile(path)
	if file == nil {
		return nil, err
	}
	return collectNotRecommendedPositions(file), nil
}

// collectNotRecommendedPositions indexes marked declarations. A group annotation
// applies to its specs, while nested declarations need their own annotations.
func collectNotRecommendedPositions(file *ast.File) map[token.Pos]bool {
	if !pkgdoc.ParseDirectives(file.Comments...).NotRecommended {
		return nil
	}
	marked := make(map[token.Pos]bool)
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
					names := spec.Names
					if len(names) == 0 && node == file.ClassFields {
						if ident := xgoutil.EmbeddedFieldIdent(spec.Type); ident != nil {
							names = []*ast.Ident{ident}
						}
					}
					add(names, node.Doc, spec.Doc)
				case *ast.TypeSpec:
					add([]*ast.Ident{spec.Name}, node.Doc, spec.Doc)
				}
			}
		case *ast.TypeSpec:
			var fields *ast.FieldList
			switch typ := xgoutil.DeclaredMemberType(node).(type) {
			case *ast.StructType:
				fields = typ.Fields
			case *ast.InterfaceType:
				fields = typ.Methods
			}
			if fields != nil {
				for _, field := range fields.List {
					names := field.Names
					if len(names) == 0 {
						if ident := xgoutil.EmbeddedFieldIdent(field.Type); ident != nil {
							names = []*ast.Ident{ident}
						}
					}
					add(names, field.Doc)
				}
			}
		case *ast.FuncDecl:
			add([]*ast.Ident{node.Name}, node.Doc)
		case *ast.OverloadFuncDecl:
			add([]*ast.Ident{node.Name}, node.Doc)
		}
		return true
	})
	return marked
}
