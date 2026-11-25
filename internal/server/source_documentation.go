package server

import (
	gotypes "go/types"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// isGeneratedVariable reports whether a recorded variable declaration comes
// from compiler-generated syntax. Imported variables have no local declaration.
func isGeneratedVariable(proj *xgo.Project, obj *gotypes.Var) bool {
	info, _ := proj.TypeInfo()
	ident := info.ObjToDef[obj.Origin()]
	if ident == nil {
		// Compiler-created closure results have neither a recorded declaration
		// nor a source position. Package members and imported objects stay visible.
		return obj.Pkg() == info.Pkg && !obj.IsField() && obj.Parent() != info.Pkg.Scope() && !obj.Pos().IsValid()
	}
	astPkg, _ := proj.ASTPackage()
	file := xgoutil.NodeTokenFile(proj.Fset, ident)
	if file == nil || astPkg.Files[file.Name()] == nil {
		return true
	}
	return !xgoutil.IsSourceIdent(file, astPkg.Files[file.Name()].Code, ident)
}

// objectSource returns the current source file and position of a project object.
// Imported objects can use positions from another file set, so package identity
// must be checked before interpreting the position in the project's file set.
func objectSource(proj *xgo.Project, obj gotypes.Object) (*ast.File, token.Pos) {
	info, _ := proj.TypeInfo()
	if info == nil || obj.Pkg() != info.Pkg {
		return nil, token.NoPos
	}
	pos := obj.Pos()
	// Local types and generated range variables can lack object positions.
	if !pos.IsValid() {
		if ident := info.ObjToDef[obj]; ident != nil && !ident.Implicit() {
			pos = ident.Pos()
		}
	}
	if pos.IsValid() {
		return sourceASTFile(proj, pos), pos
	}
	return nil, token.NoPos
}

// sourceDocumentation resolves documentation at an object's declaration.
// The boolean distinguishes absent source from a declaration without a comment.
// Local declarations must never inherit package documentation by name.
func (r *definitionContext) sourceDocumentation(obj gotypes.Object) (string, bool) {
	file, pos := objectSource(r.proj, obj)
	if file == nil {
		return "", false
	}
	if _, ok := obj.(*gotypes.Func); ok {
		if doc, found := r.sourceFunctionComment(file, pos); found {
			return doc.Text(), true
		}
	}
	var fieldIdent *ast.Ident
	if field, ok := obj.(*gotypes.Var); ok && field.IsField() && !field.Embedded() {
		info, _ := r.proj.TypeInfo()
		if ident := info.ObjToDef[field.Origin()]; ident != nil && !ident.Implicit() && ident.Pos() == pos {
			if r.pendingStructFields != nil {
				r.indexStructFields(r.pendingStructFields)
				r.pendingStructFields = nil
			}
			if decl := r.structFields[ident]; decl != nil {
				return fieldDocumentation(decl), true
			}
			fieldIdent = ident
		}
	}
	declares := func(names []*ast.Ident) bool {
		return slices.ContainsFunc(names, func(name *ast.Ident) bool { return name.Pos() == pos })
	}
	var spec ast.Spec
	var trailing string
	path, _ := xgoutil.PathEnclosingInterval(file, pos, pos)
	for i, node := range path {
		switch node := node.(type) {
		case *ast.Field:
			if len(node.Names) != 0 && !declares(node.Names) {
				return "", true
			}
			if fieldIdent != nil && i+2 < len(path) && slices.Contains(node.Names, fieldIdent) {
				if owner, ok := path[i+2].(*ast.StructType); ok && path[i+1] == owner.Fields {
					if r.structFields == nil {
						r.pendingStructFields = owner.Fields
					} else {
						r.indexStructFields(owner.Fields)
					}
				}
			}
			return fieldDocumentation(node), true
		case *ast.ValueSpec:
			if len(node.Names) == 0 {
				field := xgoutil.EmbeddedFieldIdent(node.Type)
				if field == nil || field.Pos() != pos {
					return "", true
				}
			} else if !declares(node.Names) {
				return "", true
			}
			if doc := node.Doc.Text(); doc != "" {
				return doc, true
			}
			trailing = node.Comment.Text()
			spec = node
		case *ast.TypeSpec:
			if node.Name.Pos() != pos {
				return "", true
			}
			if doc := node.Doc.Text(); doc != "" {
				return doc, true
			}
			trailing = node.Comment.Text()
			spec = node
		case *ast.GenDecl:
			if len(node.Specs) == 1 && node.Specs[0] == spec {
				if doc := node.Doc.Text(); doc != "" {
					return doc, true
				}
			}
			return trailing, true
		case *ast.FuncDecl:
			if node.Name.Pos() == pos {
				return node.Doc.Text(), true
			}
			return "", true
		case *ast.OverloadFuncDecl:
			if node.Name.Pos() == pos {
				return node.Doc.Text(), true
			}
			return "", true
		case *ast.AssignStmt, *ast.RangeStmt, *ast.FuncLit:
			return "", true
		}
	}
	return "", true
}

// fieldDocumentation returns a field's leading or trailing documentation.
func fieldDocumentation(field *ast.Field) string {
	if doc := field.Doc.Text(); doc != "" {
		return doc
	}
	return field.Comment.Text()
}

// indexStructFields records sibling fields so bulk requests can reuse the
// declaration lookup without walking unrelated parts of the file.
func (r *definitionContext) indexStructFields(fields *ast.FieldList) {
	if r.structFields == nil {
		r.structFields = make(map[*ast.Ident]*ast.Field, len(fields.List))
	}
	for _, field := range fields.List {
		for _, name := range field.Names {
			if previous, found := r.structFields[name]; found && previous != field {
				r.structFields[name] = nil
			} else {
				r.structFields[name] = field
			}
		}
	}
}

// sourceFunctionComment indexes top-level function declarations once per file
// so completion can describe many methods without repeatedly walking the file.
func (r *definitionContext) sourceFunctionComment(file *ast.File, pos token.Pos) (*ast.CommentGroup, bool) {
	comments, indexed := r.functionComments[file]
	if !indexed {
		comments = make(map[token.Pos]*ast.CommentGroup)
		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				comments[decl.Name.Pos()] = decl.Doc
			case *ast.OverloadFuncDecl:
				comments[decl.Name.Pos()] = decl.Doc
			}
		}
		if r.functionComments == nil {
			r.functionComments = make(map[*ast.File]map[token.Pos]*ast.CommentGroup)
		}
		r.functionComments[file] = comments
	}
	comment, ok := comments[pos]
	return comment, ok
}
