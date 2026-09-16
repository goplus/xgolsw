package server

import (
	gotypes "go/types"
	"strconv"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// definitionContext holds project data and documentation used to describe symbols.
type definitionContext struct {
	typeDisplay
	proj     *xgo.Project
	enumInfo *enumInfo

	// lookupPkgDoc returns immutable package documentation. Reuse the same
	// instance for unchanged documentation and return a new instance when it
	// changes, as definition caches include documentation identity.
	lookupPkgDoc func(string) (*pkgdoc.PkgDoc, error)
}

// spxDefinitionsFor returns all spx definitions for the given object. It
// returns multiple definitions only if the object is an XGo overloadable
// function.
func (r *definitionContext) spxDefinitionsFor(obj gotypes.Object, selectorTypeName string) []SpxDefinition {
	if obj == nil {
		return nil
	}
	if xgoutil.IsInBuiltinPkg(obj) {
		return []SpxDefinition{r.getDefinitionForBuiltinObj(obj, r.proj.Importer, r.lookupPkgDoc)}
	}

	pkgDoc := r.pkgDocForObject(obj)

	switch obj := obj.(type) {
	case *gotypes.Var:
		typeInfo, _ := r.proj.TypeInfo()
		astPkg, _ := r.proj.ASTPackage()
		forceVar := xgoutil.IsDefinedInClassFieldsDecl(r.proj.Fset, typeInfo, astPkg, obj)
		return []SpxDefinition{r.definitionForVar(obj, selectorTypeName, forceVar, pkgDoc)}
	case *gotypes.Const:
		if r.enumInfo.isRegularConstObject(obj) {
			return []SpxDefinition{r.definitionForConst(obj, pkgDoc)}
		}
		if r.enumInfo.isSyntheticObject(obj) {
			return nil
		}
		if members := r.enumInfo.membersForObject(obj); len(members) > 0 {
			return []SpxDefinition{r.spxDefinitionForEnumMembers(members...)}
		}
		return []SpxDefinition{r.definitionForConst(obj, pkgDoc)}
	case *gotypes.TypeName:
		def := r.definitionForType(obj, pkgDoc)
		if r.enumInfo.typeFor(obj.Type()) != nil {
			def.CompletionItemKind = EnumCompletion
		}
		return []SpxDefinition{def}
	case *gotypes.Func:
		if typeInfo, _ := r.proj.TypeInfo(); typeInfo != nil {
			if defIdent := typeInfo.ObjToDef[obj]; defIdent != nil && defIdent.Implicit() {
				return nil
			}
		}
		if xgoutil.IsUnexpandableXGoOverloadableFunc(obj) {
			return nil
		}
		if funcOverloads := xgoutil.ExpandXGoOverloadableFunc(obj); funcOverloads != nil {
			defs := make([]SpxDefinition, 0, len(funcOverloads))
			for _, funcOverload := range funcOverloads {
				defs = append(defs, r.definitionForFunc(funcOverload, selectorTypeName, pkgDoc))
			}
			return defs
		}
		return []SpxDefinition{r.definitionForFunc(obj, selectorTypeName, pkgDoc)}
	case *gotypes.PkgName:
		return []SpxDefinition{GetSpxDefinitionForPkg(obj, pkgDoc)}
	}
	return nil
}

// spxDefinitionsForIdent returns all spx definitions for the given identifier.
// It returns multiple definitions only if the identifier is an XGo
// overloadable function.
func (r *definitionContext) spxDefinitionsForIdent(ident *ast.Ident) []SpxDefinition {
	if ident.Name == "_" {
		return nil
	}
	typeInfo, _ := r.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	if members := r.enumInfo.membersForIdent(r.proj, typeInfo, ident); len(members) > 0 {
		return []SpxDefinition{r.spxDefinitionForEnumMembers(members...)}
	}
	obj := r.enumInfo.objectForIdent(typeInfo, ident)
	return r.spxDefinitionsFor(obj, SelectorTypeNameForIdent(r.proj, ident))
}

// spxDefinitionsForStruct returns all spx definitions for the given struct type.
func (r *definitionContext) spxDefinitionsForStruct(typ gotypes.Type) []SpxDefinition {
	var defs []SpxDefinition
	for member := range xgoutil.StructMembers(typ) {
		var selector string
		if member.Selector != nil {
			selector = member.Selector.Obj().Name()
		} else {
			selector = memberTypeName(r.proj, member.Member)
		}
		defs = append(defs, r.spxDefinitionsFor(member.Member, selector)...)
	}
	return defs
}

// spxDefinitionForField returns the spx definition for the given field and
// optional selector type name.
func (r *definitionContext) spxDefinitionForField(field *gotypes.Var, selectorTypeName string) SpxDefinition {
	typeInfo, _ := r.proj.TypeInfo()
	if typeInfo == nil || field.Pkg() != typeInfo.Pkg {
		pkgDoc, _ := r.lookupPkgDoc(xgoutil.PkgPath(field.Pkg()))
		return r.definitionForVar(field, selectorTypeName, false, pkgDoc)
	}
	defIdent := typeInfo.ObjToDef[field.Origin()]
	if defIdent == nil {
		return r.definitionForVar(field, selectorTypeName, false, nil)
	}
	if selectorTypeName == "" {
		selectorTypeName = SelectorTypeNameForIdent(r.proj, defIdent)
	}
	astPkg, _ := r.proj.ASTPackage()
	forceVar := xgoutil.IsDefinedInClassFieldsDecl(r.proj.Fset, typeInfo, astPkg, field)
	pkgDoc, _ := r.proj.PkgDoc()
	return r.definitionForVar(field, selectorTypeName, forceVar, pkgDoc)
}

// spxDefinitionForMethod returns the spx definition for the given method and
// optional selector type name.
func (r *definitionContext) spxDefinitionForMethod(method *gotypes.Func, selectorTypeName string) SpxDefinition {
	typeInfo, _ := r.proj.TypeInfo()
	if typeInfo == nil || method.Pkg() != typeInfo.Pkg {
		if idx := strings.LastIndex(selectorTypeName, "."); idx >= 0 {
			selectorTypeName = selectorTypeName[idx+1:]
		}
		pkgDoc, _ := r.lookupPkgDoc(xgoutil.PkgPath(method.Pkg()))
		return r.definitionForFunc(method, selectorTypeName, pkgDoc)
	}
	defIdent := typeInfo.ObjToDef[method.Origin()]
	if defIdent == nil {
		return r.definitionForFunc(method, selectorTypeName, nil)
	}
	if selectorTypeName == "" {
		selectorTypeName = SelectorTypeNameForIdent(r.proj, defIdent)
	}
	pkgDoc, _ := r.proj.PkgDoc()
	return r.definitionForFunc(method, selectorTypeName, pkgDoc)
}

// spxImportsAtASTFilePosition returns the import at the given position in the given AST file.
func (r *definitionContext) spxImportsAtASTFilePosition(astFile *ast.File, position token.Position) *SpxReferencePkg {
	fset := r.proj.Fset
	for _, imp := range astFile.Imports {
		nodePos := fset.Position(imp.Pos())
		nodeEnd := fset.Position(imp.End())
		if nodePos.Filename != position.Filename ||
			position.Line != nodePos.Line ||
			position.Column < nodePos.Column ||
			position.Column > nodeEnd.Column {
			continue
		}

		pkg, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		pkgDoc, err := r.lookupPkgDoc(pkg)
		if err != nil {
			continue
		}
		return &SpxReferencePkg{
			Pkg:     pkgDoc,
			PkgPath: pkg,
			Node:    imp,
		}
	}
	return nil
}

// pkgDocForObject resolves documentation from the project or configured lookup.
func (r *definitionContext) pkgDocForObject(obj gotypes.Object) *pkgdoc.PkgDoc {
	var doc *pkgdoc.PkgDoc
	info, _ := r.proj.TypeInfo()
	if pkgName, ok := obj.(*gotypes.PkgName); ok {
		doc, _ = r.lookupPkgDoc(pkgName.Imported().Path())
	} else if info != nil && obj.Pkg() == info.Pkg {
		doc, _ = r.proj.PkgDoc()
	} else {
		doc, _ = r.lookupPkgDoc(xgoutil.PkgPath(obj.Pkg()))
	}
	return doc
}

// definitionForVar describes a variable with documentation from its declaration.
func (r *definitionContext) definitionForVar(v *gotypes.Var, selectorTypeName string, forceVar bool, doc *pkgdoc.PkgDoc) SpxDefinition {
	return r.withSourceDocumentation(v, r.typeDisplay.definitionForVar(v, selectorTypeName, forceVar, doc))
}

// definitionForConst describes a constant with documentation from its declaration.
func (r *definitionContext) definitionForConst(c *gotypes.Const, doc *pkgdoc.PkgDoc) SpxDefinition {
	return r.withSourceDocumentation(c, r.typeDisplay.definitionForConst(c, doc))
}

// definitionForType describes a type with documentation from its declaration.
func (r *definitionContext) definitionForType(typ *gotypes.TypeName, doc *pkgdoc.PkgDoc) SpxDefinition {
	return r.withSourceDocumentation(typ, r.typeDisplay.definitionForType(typ, doc))
}

// definitionForFunc describes a function using its source declaration, including
// interface methods whose receiver is recorded as an unnamed interface.
func (r *definitionContext) definitionForFunc(fun *gotypes.Func, selectorTypeName string, doc *pkgdoc.PkgDoc) SpxDefinition {
	if owner, field := interfaceMethodDeclaration(r.proj, fun); field != nil {
		selectorTypeName = owner
	}
	return r.withSourceDocumentation(fun, r.typeDisplay.definitionForFunc(fun, selectorTypeName, doc))
}

// withSourceDocumentation replaces package documentation when the object has a
// project declaration, even if that declaration has no documentation.
func (r *definitionContext) withSourceDocumentation(obj gotypes.Object, def SpxDefinition) SpxDefinition {
	if doc, ok := r.sourceDocumentation(obj); ok {
		def.Detail = doc
	}
	return def
}

// functionDocumentation resolves documentation for the actual function
// declaration, including local and anonymous interface methods.
func (r *definitionContext) functionDocumentation(fun *gotypes.Func) string {
	if doc, ok := r.sourceDocumentation(fun); ok {
		return doc
	}
	return functionDocumentation(fun, "", r.pkgDocForObject(fun))
}
