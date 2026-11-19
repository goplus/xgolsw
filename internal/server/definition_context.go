package server

import (
	gotypes "go/types"
	"slices"
	"strconv"
	"strings"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// definitionContext holds project data and documentation used to describe symbols.
type definitionContext struct {
	typeDisplay
	proj               *xgo.Project
	enumInfo           *enumInfo
	frameworkBaseTypes map[*gotypes.Named]struct{}
	framework          frameworkAdapter
	frameworkResolved  bool

	// memberSelectors indexes receiver members for this request only.
	memberSelectors map[gotypes.Type]map[gotypes.Object]*gotypes.Named

	// functionComments indexes top-level function declarations by source position.
	functionComments map[*ast.File]map[token.Pos]*ast.CommentGroup

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
		if member := types.StaticMemberObject(obj); member != nil {
			return r.staticMemberDefinitions(obj, member)
		}
		if xgoutil.IsUnexpandableXGoOverloadableFunc(obj) {
			return nil
		}
		if funcOverloads := xgoutil.ExpandXGoOverloadableFunc(obj); funcOverloads != nil {
			groupDoc := r.sourceOverloadDocumentation(obj)
			defs := make([]symbolDefinition, 0, len(funcOverloads))
			for _, funcOverload := range funcOverloads {
				def := r.definitionForFunc(funcOverload, selectorTypeName, pkgDoc)
				def.SourceObject = obj
				if def.Detail == "" {
					def.Detail = groupDoc
				}
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

// staticMemberDefinitions describes the implementation with its source selector.
func (r *definitionContext) staticMemberDefinitions(wrapper *gotypes.Func, obj gotypes.Object) []symbolDefinition {
	name := wrapper.Name()
	label := name
	if _, callable := obj.(*gotypes.Func); callable && xgoutil.IsMarkedAsXGoPackage(wrapper.Pkg()) {
		label, _ = xgoutil.ParseXGoFuncName(name)
	}
	owner := memberTypeName(r.proj, wrapper)
	ownerName := r.frameworkDisplayTypeName(wrapper, owner)
	defs := r.definitionsFor(obj, owner)
	for i := range defs {
		def := &defs[i]
		def.Overview = strings.Replace(def.Overview, def.CompletionItemLabel, label, 1)
		def.SourceObject = wrapper
		def.staticMemberName = name
		def.ID.Package = new(xgoutil.PkgPath(wrapper.Pkg()))
		def.ID.Name = new(ownerName + "." + label)
		def.CompletionItemLabel = label
		def.CompletionItemInsertText = label
	}
	return defs
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
	source, _ := typeInfo.SourceObjectOf(ident).(*gotypes.Func)
	if fun, ok := obj.(*gotypes.Func); ok && xgoutil.IsOverloadPlaceholder(fun) {
		obj = typeInfo.Overloads[ident]
	}
	if source != nil && source != obj {
		if types.StaticMemberObject(source) != nil {
			return r.staticMemberDefinitions(source, obj)
		}
	}
	if fun, ok := obj.(*gotypes.Func); ok && xgoutil.ReferencesEncodedFuncName(fun, ident.Name) {
		if overloads := xgoutil.ExpandXGoOverloadableFunc(fun); overloads != nil {
			defs := make([]symbolDefinition, 0, len(overloads))
			for _, overload := range overloads {
				defs = append(defs, r.encodedFunctionDefinition(overload, ident.Name))
			}
			return defs
		}
		if xgoutil.IsUnexpandableXGoOverloadableFunc(fun) {
			return nil
		}
		return []symbolDefinition{r.encodedFunctionDefinition(fun, ident.Name)}
	}
	var defs []symbolDefinition
	if member := r.memberForIdent(ident, obj); member != nil {
		defs = r.definitionsForMember(*member)
	} else {
		defs = r.definitionsFor(obj, memberTypeName(r.proj, obj))
	}
	if source != nil {
		r.describeOverloadUse(ident, source, defs)
	}
	if typ, ok := obj.(*gotypes.TypeName); ok && len(defs) == 1 && usesOverloadedTypeName(typ, ident.Name) {
		defs[0].Overview = r.typeOverview(typ, ident.Name)
	}
	return defs
}

// usesOverloadedTypeName reports whether name denotes the reconstructed group
// that selected typ.
func usesOverloadedTypeName(typ *gotypes.TypeName, name string) bool {
	if typ.Pkg() == nil || name == typ.Name() {
		return false
	}
	group, ok := typ.Pkg().Scope().Lookup(name).(*gotypes.TypeName)
	if !ok {
		return false
	}
	overloaded, ok := gogen.CheckOverloadNamed(group.Type())
	return ok && slices.ContainsFunc(overloaded.Types, func(variant *gotypes.Named) bool {
		return variant.Obj() == typ
	})
}

// describeOverloadUse displays the source callable while retaining the
// implementation's definition identity and documentation.
func (r *definitionContext) describeOverloadUse(ident *ast.Ident, source *gotypes.Func, defs []symbolDefinition) {
	if xgoutil.IsInBuiltinPkg(source) {
		return
	}
	ext, _ := gogen.CheckFuncEx(source.Signature())
	switch ext.(type) {
	case *gogen.TyOverloadFunc, *gogen.TyOverloadMethod:
		_, name, _ := displayedFuncName(source)
		groupDoc, _ := r.sourceDocumentation(source)
		for i := range defs {
			defs[i].nameFunctionOverview(name)
			if defs[i].Detail == "" {
				defs[i].Detail = groupDoc
			}
		}
	case *gogen.TyTemplateRecvMethod:
		if _, encoded := xgoutil.SplitXGoxFuncName(source.Name()); !encoded {
			return
		}
		for i := range defs {
			fun := defs[i].Function
			if fun == nil || fun.Signature().Params().Len() == 0 {
				continue
			}
			sig := fun.Signature()
			params := slices.Collect(sig.Params().Variables())
			defs[i].Overview = r.functionOverview(ident.Name, sig, gotypes.NewTuple(params[1:]...), 0)
		}
	}
}

// isTemplateMethodWrapper reports whether obj is a reconstructed receiver method.
func isTemplateMethodWrapper(obj gotypes.Object) bool {
	fun, ok := obj.(*gotypes.Func)
	if !ok {
		return false
	}
	ext, _ := gogen.CheckFuncEx(fun.Signature())
	_, ok = ext.(*gogen.TyTemplateRecvMethod)
	return ok
}

// sourceOverloadDocumentation returns the comment on a source overload group.
func (r *definitionContext) sourceOverloadDocumentation(obj gotypes.Object) string {
	fun, ok := obj.(*gotypes.Func)
	if !ok {
		return ""
	}
	ext, _ := gogen.CheckFuncEx(fun.Signature())
	switch ext.(type) {
	case *gogen.TyOverloadFunc, *gogen.TyOverloadMethod:
		doc, _ := r.sourceDocumentation(fun)
		return doc
	}
	return ""
}

// encodedFunctionDefinition describes a direct reference to an encoded Go
// function, including parameters that are bound when using a method selector.
func (r *definitionContext) encodedFunctionDefinition(fun *gotypes.Func, name string) symbolDefinition {
	def := r.definitionForFunc(fun, "", r.pkgDocForObject(fun))
	sig := fun.Signature()
	def.Overview = r.functionOverview(name, sig, sig.Params(), 0)
	def.ID.Name = new(name)
	if xgoutil.IsXGoOverloadedFuncName(name) {
		// The exact implementation name already includes its overload index.
		def.ID.OverloadID = nil
	}
	def.CompletionItemLabel = name
	def.CompletionItemInsertText = name
	return def
}

// definitionsForInstanceMembers describes the fields and methods of a value.
func (r *definitionContext) definitionsForInstanceMembers(typ gotypes.Type) []symbolDefinition {
	var defs []symbolDefinition
	for member := range r.selectorMembers(typ) {
		if method, ok := member.Member.(*gotypes.Func); ok && types.StaticMemberObject(method) != nil {
			continue
		}
		defs = append(defs, r.definitionsForMember(member)...)
	}
	return defs
}

// definitionsForSelectorMethods describes method candidates for a type or
// interface selector. The compiler can resolve an alias hidden by another member.
func (r *definitionContext) definitionsForSelectorMethods(receiver gotypes.Type, isType bool) []symbolDefinition {
	var defs []symbolDefinition
	for obj := range memberCandidates(receiver) {
		method, ok := obj.(*gotypes.Func)
		if !ok || !xgoutil.IsExportedOrInMainPkg(method) {
			continue
		}
		if static := types.StaticMemberObject(method); static != nil {
			if _, callable := static.(*gotypes.Func); !isType || !callable {
				continue
			}
		}
		defs = append(defs, r.definitionsForSelection(method, receiver)...)
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
			defs[i].ID.Package = new(xgoutil.PkgPath(selector.Pkg()))
			defs[i].ID.Name = new(name + "." + defs[i].CompletionItemLabel)
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
	return r.withSourceDocumentation(typ, r.typeDisplay.definitionForType(typ, typ.Name(), doc))
}

// definitionForFunc describes a function using its source declaration, including
// interface methods whose receiver is recorded as an unnamed interface.
func (r *definitionContext) definitionForFunc(fun *gotypes.Func, selectorTypeName string, doc *pkgdoc.PkgDoc) symbolDefinition {
	if owner, field := interfaceMethodDeclaration(r.proj, fun); field != nil {
		selectorTypeName = owner
	}
	if selectorTypeName == "" {
		selectorTypeName, _, _ = displayedFuncName(fun)
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
