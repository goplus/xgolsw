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

	// pendingStructFields defers indexing until a request looks up more than one field.
	pendingStructFields *ast.FieldList

	// structFields indexes explicit field declarations by their source identifiers.
	structFields map[*ast.Ident]*ast.Field

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
			return r.staticMemberDefinitions(obj, member, nil)
		}
		if xgoutil.IsUnexpandableXGoOverloadableFunc(obj) {
			return nil
		}
		if overloads := xgoutil.OverloadImplementations(obj); overloads != nil {
			groupDoc := r.overloadDocumentation(obj)
			var defs []symbolDefinition
			for implementation, binding := range overloads {
				def := r.definitionForOverload(implementation, selectorTypeName, pkgDoc)
				r.describeSourceCallable(&def, obj, binding, def.CompletionItemLabel, groupDoc)
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

// definitionForOverload describes a function or function-valued variable
// listed as an overload implementation.
func (r *definitionContext) definitionForOverload(obj gotypes.Object, selectorTypeName string, pkgDoc *pkgdoc.PkgDoc) symbolDefinition {
	if fun, ok := obj.(*gotypes.Func); ok {
		return r.definitionForFunc(fun, selectorTypeName, pkgDoc)
	}
	return r.withSourceDocumentation(obj, r.typeDisplay.definitionForOverload(obj, selectorTypeName, pkgDoc))
}

// staticMemberDefinitions describes the implementation with its source selector.
func (r *definitionContext) staticMemberDefinitions(wrapper *gotypes.Func, obj gotypes.Object, binding *gotypes.Func) []symbolDefinition {
	name := wrapper.Name()
	label := name
	_, method := types.StaticMemberObject(wrapper).(*gotypes.Func)
	if method && xgoutil.IsMarkedAsXGoPackage(wrapper.Pkg()) {
		label, _ = xgoutil.ParseXGoFuncName(name)
	}
	owner := memberTypeName(r.proj, wrapper)
	ownerName := r.frameworkDisplayTypeName(wrapper, owner)
	var defs []symbolDefinition
	if variable, ok := obj.(*gotypes.Var); method && ok {
		defs = []symbolDefinition{r.definitionForOverload(variable, owner, r.pkgDocForObject(variable))}
	} else {
		defs = r.definitionsFor(obj, owner)
	}
	groupDoc := r.overloadDocumentation(wrapper)
	for i := range defs {
		def := &defs[i]
		def.Overview = strings.Replace(def.Overview, def.CompletionItemLabel, label, 1)
		r.describeSourceCallable(def, wrapper, binding, label, groupDoc)
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
	selected := typeInfo.SelectedOverloads[ident]
	var binding *gotypes.Func
	if selected.Implementation != nil {
		obj, binding = selected.Implementation, selected.Binding
	} else if fun, ok := obj.(*gotypes.Func); ok {
		if xgoutil.IsOverloadPlaceholder(fun) {
			obj = typeInfo.Overloads[ident]
		} else if source != nil && source != fun {
			ext, _ := gogen.CheckFuncEx(fun.Signature())
			switch ext.(type) {
			case *gogen.TyTypeAsParams:
				obj, binding = xgoutil.CallableImplementation(fun)
				if obj == fun {
					obj = source
				}
			case *gogen.TyOverloadFunc, *gogen.TyOverloadMethod:
				// The selected nested group does not identify a concrete implementation.
				obj = source
			}
		}
	}
	if source != nil && source != obj && types.StaticMemberObject(source) != nil {
		return r.staticMemberDefinitions(source, obj, binding)
	}
	if fun, ok := obj.(*gotypes.Func); ok && xgoutil.ReferencesEncodedFuncName(fun, ident.Name) {
		if overloads := xgoutil.ExpandXGoOverloads(fun); overloads != nil {
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
		r.describeOverloadUse(ident, source, binding, obj, defs)
	}
	for i := range defs {
		defs[i].useIndexedImplementationName(ident.Name)
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
func (r *definitionContext) describeOverloadUse(ident *ast.Ident, source, binding *gotypes.Func, selected gotypes.Object, defs []symbolDefinition) {
	if xgoutil.IsInBuiltinPkg(source) {
		return
	}
	ext, _ := gogen.CheckFuncEx(source.Signature())
	_, name, _ := displayedFuncName(source)
	switch ext.(type) {
	case *gogen.TyOverloadFunc, *gogen.TyOverloadMethod, *gogen.TyTypeAsParams:
	case *gogen.TyTemplateRecvMethod:
		if _, encoded := xgoutil.SplitXGoxFuncName(source.Name()); encoded {
			name = ident.Name
		}
	default:
		return
	}
	groupDoc := r.overloadDocumentation(source)
	for i := range defs {
		if variable, ok := selected.(*gotypes.Var); ok {
			defs[i].Implementation = variable
		}
		r.describeSourceCallable(&defs[i], source, binding, name, groupDoc)
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

// overloadDocumentation returns the overload declaration's shared comment.
func (r *definitionContext) overloadDocumentation(obj gotypes.Object) string {
	fun, ok := obj.(*gotypes.Func)
	if !ok {
		return ""
	}
	ext, _ := gogen.CheckFuncEx(fun.Signature())
	switch ext.(type) {
	case *gogen.TyOverloadFunc, *gogen.TyOverloadMethod:
		if doc, ok := r.sourceDocumentation(fun); ok {
			return doc
		}
	case nil:
		return ""
	}
	if marker := xgoutil.OverloadGroupMarker(fun); marker != nil {
		return overloadMarkerDocumentation(marker, r.pkgDocForObject(marker))
	}
	return ""
}

// encodedFunctionDefinition describes a direct reference to an encoded Go
// function, including parameters that are bound when using a method selector.
func (r *definitionContext) encodedFunctionDefinition(obj gotypes.Object, name string) symbolDefinition {
	def := r.definitionForOverload(obj, "", r.pkgDocForObject(obj))
	sig := signatureType(obj.Type())
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
func (r *definitionContext) definitionsForInstanceMembers(receiver gotypes.Type) []symbolDefinition {
	typ := memberReceiverType(receiver)
	if typ == nil {
		return nil
	}
	var defs []symbolDefinition
	for member := range r.selectorMembers(typ) {
		if _, field := member.Member.(*gotypes.Var); field {
			defs = append(defs, r.definitionsForMember(member)...)
		}
	}
	return append(defs, r.definitionsForSelectorMethods(typ, false)...)
}

// definitionsForSelectorMethods describes method candidates before resolving
// aliases and shadowing for the source selector.
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
func (r *definitionContext) importDocumentationAtPosition(typeInfo *types.Info, astFile *ast.File, position token.Position) (*pkgdoc.PkgDoc, *ast.ImportSpec) {
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

		path := typeInfo.FailedImportPaths[imp.Pos()]
		if name := importPackageName(typeInfo, imp); name != nil {
			path = name.Imported().Path()
		} else if path == "" {
			// The checker may not have reached this import.
			var err error
			path, err = strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
		}
		pkgDoc, err := r.lookupPkgDoc(path)
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
