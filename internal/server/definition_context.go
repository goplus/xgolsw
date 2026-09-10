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
		return []SpxDefinition{getDefinitionForBuiltinObj(obj, r.proj.Importer, r.lookupPkgDoc)}
	}

	var pkgDoc *pkgdoc.PkgDoc
	if pkgName, ok := obj.(*gotypes.PkgName); ok {
		pkgDoc, _ = r.lookupPkgDoc(pkgName.Imported().Path())
	} else if xgoutil.IsInMainPkg(obj) {
		pkgDoc, _ = r.proj.PkgDoc()
	} else {
		pkgDoc, _ = r.lookupPkgDoc(xgoutil.PkgPath(obj.Pkg()))
	}

	switch obj := obj.(type) {
	case *gotypes.Var:
		typeInfo, _ := r.proj.TypeInfo()
		astPkg, _ := r.proj.ASTPackage()
		forceVar := xgoutil.IsDefinedInClassFieldsDecl(r.proj.Fset, typeInfo, astPkg, obj)
		return []SpxDefinition{GetSpxDefinitionForVar(obj, selectorTypeName, forceVar, pkgDoc)}
	case *gotypes.Const:
		if r.enumInfo.isRegularConstObject(obj) {
			return []SpxDefinition{GetSpxDefinitionForConst(obj, pkgDoc)}
		}
		if r.enumInfo.isSyntheticObject(obj) {
			return nil
		}
		if members := r.enumInfo.membersForObject(obj); len(members) > 0 {
			return []SpxDefinition{r.spxDefinitionForEnumMembers(members...)}
		}
		return []SpxDefinition{GetSpxDefinitionForConst(obj, pkgDoc)}
	case *gotypes.TypeName:
		def := GetSpxDefinitionForType(obj, pkgDoc)
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
				defs = append(defs, GetSpxDefinitionForFunc(funcOverload, selectorTypeName, pkgDoc))
			}
			return defs
		}
		return []SpxDefinition{GetSpxDefinitionForFunc(obj, selectorTypeName, pkgDoc)}
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

// spxDefinitionsForNamedStruct returns all spx definitions for the given named
// struct type.
func (r *definitionContext) spxDefinitionsForNamedStruct(named *gotypes.Named) []SpxDefinition {
	var defs []SpxDefinition
	for structMember := range xgoutil.StructMembers(named) {
		defs = append(defs, r.spxDefinitionsFor(structMember.Member, structMember.Selector.Obj().Name())...)
	}
	return defs
}

// spxDefinitionForField returns the spx definition for the given field and
// optional selector type name.
func (r *definitionContext) spxDefinitionForField(field *gotypes.Var, selectorTypeName string) SpxDefinition {
	typeInfo, _ := r.proj.TypeInfo()
	if typeInfo == nil || !xgoutil.IsInMainPkg(field) {
		pkgDoc, _ := r.lookupPkgDoc(xgoutil.PkgPath(field.Pkg()))
		return GetSpxDefinitionForVar(field, selectorTypeName, false, pkgDoc)
	}
	defIdent := typeInfo.ObjToDef[field]
	if defIdent == nil {
		return GetSpxDefinitionForVar(field, selectorTypeName, false, nil)
	}
	if selectorTypeName == "" {
		selectorTypeName = SelectorTypeNameForIdent(r.proj, defIdent)
	}
	astPkg, _ := r.proj.ASTPackage()
	forceVar := xgoutil.IsDefinedInClassFieldsDecl(r.proj.Fset, typeInfo, astPkg, field)
	pkgDoc, _ := r.proj.PkgDoc()
	return GetSpxDefinitionForVar(field, selectorTypeName, forceVar, pkgDoc)
}

// spxDefinitionForMethod returns the spx definition for the given method and
// optional selector type name.
func (r *definitionContext) spxDefinitionForMethod(method *gotypes.Func, selectorTypeName string) SpxDefinition {
	typeInfo, _ := r.proj.TypeInfo()
	if typeInfo == nil || !xgoutil.IsInMainPkg(method) {
		if idx := strings.LastIndex(selectorTypeName, "."); idx >= 0 {
			selectorTypeName = selectorTypeName[idx+1:]
		}
		pkgDoc, _ := r.lookupPkgDoc(xgoutil.PkgPath(method.Pkg()))
		return GetSpxDefinitionForFunc(method, selectorTypeName, pkgDoc)
	}
	defIdent := typeInfo.ObjToDef[method]
	if defIdent == nil {
		return GetSpxDefinitionForFunc(method, selectorTypeName, nil)
	}
	if selectorTypeName == "" {
		selectorTypeName = SelectorTypeNameForIdent(r.proj, defIdent)
	}
	pkgDoc, _ := r.proj.PkgDoc()
	return GetSpxDefinitionForFunc(method, selectorTypeName, pkgDoc)
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
