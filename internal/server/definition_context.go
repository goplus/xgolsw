package server

import (
	gotypes "go/types"
	"strconv"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// definitionContext holds project data and documentation used to describe symbols.
type definitionContext struct {
	typeDisplay
	proj              *xgo.Project
	enumInfo          *enumInfo
	classTypes        map[*gotypes.Named]struct{}
	framework         frameworkAdapter
	frameworkResolved bool

	// memberSelectors indexes receiver members for this request only.
	memberSelectors map[gotypes.Type]map[gotypes.Object]*gotypes.Named

	// lookupPkgDoc resolves documentation for imported packages and builtins.
	lookupPkgDoc func(string) (*pkgdoc.PkgDoc, error)
}

// definitionsFor returns all definitions for the given object. It
// returns multiple definitions only if the object is an XGo overloadable
// function.
func (r *definitionContext) definitionsFor(obj gotypes.Object, selectorTypeName string) []symbolDefinition {
	if obj == nil {
		return nil
	}
	if xgoutil.IsInBuiltinPkg(obj) {
		// Builtin interface methods, such as error.Error, retain their member
		// declarations rather than using builtin symbol descriptions.
		if fun, ok := obj.(*gotypes.Func); !ok || fun.Signature().Recv() == nil {
			return []symbolDefinition{r.definitionForBuiltin(obj, r.proj, r.lookupPkgDoc)}
		}
	}

	pkgDoc := r.pkgDocForObject(obj)

	switch obj := obj.(type) {
	case *gotypes.Var:
		typeInfo, _ := r.proj.TypeInfo()
		astPkg, _ := r.proj.ASTPackage()
		forceVar := xgoutil.IsDefinedInClassFieldsDecl(r.proj.Fset, typeInfo, astPkg, obj)
		return []symbolDefinition{r.definitionForVar(obj, selectorTypeName, forceVar, pkgDoc)}
	case *gotypes.Const:
		if r.enumInfo.isRegularConstObject(obj) {
			return []symbolDefinition{r.definitionForConst(obj, pkgDoc)}
		}
		if r.enumInfo.isSyntheticObject(obj) {
			return nil
		}
		if members := r.enumInfo.membersForObject(obj); len(members) > 0 {
			return []symbolDefinition{r.definitionForEnumMembers(members...)}
		}
		return []symbolDefinition{r.definitionForConst(obj, pkgDoc)}
	case *gotypes.TypeName:
		def := r.definitionForType(obj, pkgDoc)
		if r.enumInfo.typeFor(obj.Type()) != nil {
			def.CompletionItemKind = EnumCompletion
		}
		return []symbolDefinition{def}
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
			defs := make([]symbolDefinition, 0, len(funcOverloads))
			for _, funcOverload := range funcOverloads {
				def := r.definitionForFunc(funcOverload, selectorTypeName, pkgDoc)
				def.SourceObject = obj
				defs = append(defs, def)
			}
			return defs
		}
		return []symbolDefinition{r.definitionForFunc(obj, selectorTypeName, pkgDoc)}
	case *gotypes.PkgName:
		return []symbolDefinition{definitionForPkg(obj, pkgDoc)}
	}
	return nil
}

// definitionsForIdent returns all definitions for the given identifier.
// It returns multiple definitions only if the identifier is an XGo
// overloadable function.
func (r *definitionContext) definitionsForIdent(ident *ast.Ident) []symbolDefinition {
	if ident.Name == "_" {
		return nil
	}
	typeInfo, _ := r.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	if members := r.enumInfo.membersForIdent(r.proj, typeInfo, ident); len(members) > 0 {
		return []symbolDefinition{r.definitionForEnumMembers(members...)}
	}
	obj := r.enumInfo.objectForIdent(typeInfo, ident)
	if member := r.memberForIdent(ident, obj); member != nil {
		return r.definitionsForMember(*member)
	}
	return r.definitionsFor(obj, memberTypeName(r.proj, obj))
}

// definitionsForStruct describes the fields and methods of the given type.
func (r *definitionContext) definitionsForStruct(typ gotypes.Type) []symbolDefinition {
	var defs []symbolDefinition
	for member := range xgoutil.StructMembers(typ, r.isClassBaseType) {
		defs = append(defs, r.definitionsForMember(member)...)
	}
	return defs
}

// definitionsForMember keeps a promoted member's public selector separate from
// its declaration so definition IDs and documentation use the correct packages.
func (r *definitionContext) definitionsForMember(member xgoutil.StructMember) []symbolDefinition {
	var owner string
	if member.Selector == nil {
		owner = memberTypeName(r.proj, member.Member)
	}
	defs := r.definitionsFor(member.Member, owner)
	if member.Selector != nil {
		selector := member.Selector.Obj()
		name := r.frameworkDisplayTypeName(selector, selector.Name())
		for i := range defs {
			defs[i].ID.Package = ToPtr(xgoutil.PkgPath(selector.Pkg()))
			defs[i].ID.Name = ToPtr(name + "." + defs[i].CompletionItemLabel)
		}
	}
	return defs
}

// definitionsForSelection describes a member selected through receiver while
// retaining its declaration for documentation lookup.
func (r *definitionContext) definitionsForSelection(obj gotypes.Object, receiver gotypes.Type) []symbolDefinition {
	if member := r.memberForObject(receiver, obj); member != nil {
		return r.definitionsForMember(*member)
	}
	return r.definitionsFor(obj, memberTypeName(r.proj, obj))
}

// importDocumentationAtPosition returns documentation and the import declaration
// at the given position in the AST file.
func (r *definitionContext) importDocumentationAtPosition(astFile *ast.File, position token.Position) (*pkgdoc.PkgDoc, *ast.ImportSpec) {
	fset := r.proj.Fset
	for _, imp := range astFile.Imports {
		nodePos := fset.PositionFor(imp.Pos(), false)
		nodeEnd := fset.PositionFor(imp.End(), false)
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
		return pkgDoc, imp
	}
	return nil, nil
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
func (r *definitionContext) definitionForVar(v *gotypes.Var, selectorTypeName string, forceVar bool, doc *pkgdoc.PkgDoc) symbolDefinition {
	selectorTypeName = r.frameworkDisplayTypeName(v, selectorTypeName)
	return r.withSourceDocumentation(v, r.typeDisplay.definitionForVar(v, selectorTypeName, forceVar, doc))
}

// definitionForConst describes a constant with documentation from its declaration.
func (r *definitionContext) definitionForConst(c *gotypes.Const, doc *pkgdoc.PkgDoc) symbolDefinition {
	return r.withSourceDocumentation(c, r.typeDisplay.definitionForConst(c, doc))
}

// definitionForType describes a type with documentation from its declaration.
func (r *definitionContext) definitionForType(typ *gotypes.TypeName, doc *pkgdoc.PkgDoc) symbolDefinition {
	return r.withSourceDocumentation(typ, r.typeDisplay.definitionForType(typ, doc))
}

// definitionForFunc describes a function using its source declaration, including
// interface methods whose receiver is recorded as an unnamed interface.
func (r *definitionContext) definitionForFunc(fun *gotypes.Func, selectorTypeName string, doc *pkgdoc.PkgDoc) symbolDefinition {
	if owner, field := interfaceMethodDeclaration(r.proj, fun); field != nil {
		selectorTypeName = owner
	}
	if selectorTypeName == "" {
		selectorTypeName, _, _, _ = displayedFuncName(fun)
	}
	selectorTypeName = r.frameworkDisplayTypeName(fun, selectorTypeName)
	def := r.typeDisplay.definitionForFunc(fun, selectorTypeName, doc)
	if detail, ok := r.frameworkFunctionDocumentation(fun, doc); ok {
		def.Detail = detail
	}
	return r.withSourceDocumentation(fun, def)
}

// withSourceDocumentation replaces package documentation when the object has a
// project declaration, even if that declaration has no documentation.
func (r *definitionContext) withSourceDocumentation(obj gotypes.Object, def symbolDefinition) symbolDefinition {
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
	doc := r.pkgDocForObject(fun)
	if detail, ok := r.frameworkFunctionDocumentation(fun, doc); ok {
		return detail
	}
	return functionDocumentation(fun, doc)
}
