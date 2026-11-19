package server

import (
	"cmp"
	"fmt"
	gotypes "go/types"
	"html/template"
	"slices"
	"strings"

	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// symbolDefinition describes a symbol for hover, completion, and definition links.
type symbolDefinition struct {
	// TypeHint represents a type hint for this definition. It may be nil if
	// the definition has no associated type.
	TypeHint gotypes.Type

	// SourceObject is the source declaration before overload expansion.
	SourceObject gotypes.Object

	// Implementation supplies the signature and documentation of a callable.
	Implementation gotypes.Object

	// parameterBinding identifies a callable that binds the implementation's
	// arguments differently from SourceObject.
	parameterBinding *gotypes.Func

	// staticMemberName is the exact selector spelling of a reconstructed static member.
	staticMemberName string

	// functionValueSpelling overrides the ordinary insertion name when the
	// compiler requires a particular spelling for the callable value.
	functionValueSpelling string

	// AutoPropertyType is the inferred result of an implicit call.
	// It distinguishes a property returning a function from a method value.
	AutoPropertyType gotypes.Type

	ID       XGoDefinitionIdentifier
	Overview string
	Detail   string

	CompletionItemLabel            string
	CompletionItemKind             CompletionItemKind
	CompletionItemInsertText       string
	CompletionItemInsertTextFormat InsertTextFormat
}

// html returns the HTML representation of the definition.
func (def symbolDefinition) html() string {
	return fmt.Sprintf("<pre is=\"definition-item\" def-id=%q overview=%q>\n%s</pre>\n", template.HTMLEscapeString(def.ID.String()), template.HTMLEscapeString(def.Overview), def.Detail)
}

// markupContent constructs markup content from the definition.
func (def symbolDefinition) markupContent(kind MarkupKind) MarkupContent {
	plainText := def.Overview
	if detail := strings.TrimSpace(def.Detail); detail != "" {
		if plainText != "" {
			plainText += "\n\n"
		}
		plainText += detail
	}
	return markupContent(kind, def.html(), plainText)
}

// completionItem constructs a [CompletionItem] from the definition.
func (def symbolDefinition) completionItem(documentationKind MarkupKind) CompletionItem {
	return CompletionItem{
		Label:            def.CompletionItemLabel,
		Kind:             def.CompletionItemKind,
		Documentation:    completionDocumentation(def.markupContent(documentationKind)),
		InsertText:       def.CompletionItemInsertText,
		InsertTextFormat: &def.CompletionItemInsertTextFormat,
		Data: &CompletionItemData{
			Definition: &def.ID,
		},
	}
}

// definitionsForPkg describes the exported symbols of the given package.
func (d typeDisplay) definitionsForPkg(pkg *gotypes.Package, pkgDoc *pkgdoc.PkgDoc) []symbolDefinition {
	names := pkg.Scope().Names()
	defs := make([]symbolDefinition, 0, len(names))
	for _, name := range names {
		obj := pkg.Scope().Lookup(name)
		if obj == nil || !obj.Exported() {
			continue
		}
		switch obj := obj.(type) {
		case *gotypes.Var:
			defs = append(defs, d.definitionForVar(obj, "", false, pkgDoc))
		case *gotypes.Const:
			defs = append(defs, d.definitionForConst(obj, pkgDoc))
		case *gotypes.TypeName:
			defs = append(defs, d.definitionForType(obj, obj.Name(), pkgDoc))
		case *gotypes.Func:
			// Template methods are selected through their receiver. Their
			// display names do not resolve as package-level functions.
			if _, _, template := xgoutil.TemplateMethodName(obj, false); template {
				continue
			}
			if overloads := xgoutil.OverloadImplementations(obj); overloads != nil {
				groupDoc := overloadMarkerDocumentation(xgoutil.OverloadGroupMarker(obj), pkgDoc)
				for implementation, binding := range overloads {
					def := d.definitionForOverload(implementation, "", pkgDoc)
					d.describeSourceCallable(&def, obj, binding, def.CompletionItemLabel, groupDoc)
					defs = append(defs, def)
				}
			} else {
				defs = append(defs, d.definitionForFunc(obj, "", pkgDoc))
			}
		case *gotypes.PkgName:
			defs = append(defs, definitionForPkg(obj, pkgDoc))
		}
	}
	return slices.Clip(defs)
}

// definitionForOverload describes a callable in an overload's implementation list.
func (d typeDisplay) definitionForOverload(obj gotypes.Object, selectorTypeName string, pkgDoc *pkgdoc.PkgDoc) symbolDefinition {
	if fun, ok := obj.(*gotypes.Func); ok {
		return d.definitionForFunc(fun, selectorTypeName, pkgDoc)
	}
	def := d.definitionForVar(obj.(*gotypes.Var), selectorTypeName, false, pkgDoc)
	def.Implementation = obj
	def.CompletionItemKind = FunctionCompletion
	return def
}

// describeSourceCallable supplies the source signature and shared documentation
// for a definition backed by a reconstructed callable.
func (d typeDisplay) describeSourceCallable(def *symbolDefinition, source, binding *gotypes.Func, name, doc string) {
	def.SourceObject = source
	def.Detail = cmp.Or(def.Detail, doc)
	if def.Implementation == nil {
		return
	}
	binding = cmp.Or(binding, def.parameterBinding, source)
	if binding == source {
		def.parameterBinding = nil
	} else {
		def.parameterBinding = binding
	}
	if sig, params := xgoutil.ResolveSourceFuncSignature(def.Implementation, source, binding); sig != nil {
		def.Overview = d.functionOverview(name, sig, params, xgoutil.NormalizedCallExprTypeArgCount(def.function(), params))
	}
}

// definitionForVar describes the provided variable or field.
func (d typeDisplay) definitionForVar(v *gotypes.Var, selectorTypeName string, forceVar bool, pkgDoc *pkgdoc.PkgDoc) symbolDefinition {
	var overview strings.Builder
	completionItemKind := VariableCompletion
	if !v.IsField() || forceVar {
		overview.WriteString("var ")
	} else {
		overview.WriteString("field ")
		completionItemKind = FieldCompletion
	}
	overview.WriteString(v.Name())
	overview.WriteString(" ")
	overview.WriteString(d.typeString(v.Type()))

	var detail string
	if pkgDoc != nil {
		if v.IsField() {
			detail = memberDocumentation(v, pkgDoc)
		} else {
			detail = pkgDoc.Vars[v.Name()]
		}
	}

	idName := v.Name()
	if selectorTypeName != "" {
		idName = selectorTypeName + "." + idName
	}
	return symbolDefinition{
		TypeHint:     v.Type(),
		SourceObject: v,

		ID: XGoDefinitionIdentifier{
			Package: new(xgoutil.PkgPath(v.Pkg())),
			Name:    &idName,
		},
		Overview: overview.String(),
		Detail:   detail,

		CompletionItemLabel:            v.Name(),
		CompletionItemKind:             completionItemKind,
		CompletionItemInsertText:       v.Name(),
		CompletionItemInsertTextFormat: PlainTextTextFormat,
	}
}

// definitionForConst describes the provided constant in the source context.
func (d typeDisplay) definitionForConst(c *gotypes.Const, pkgDoc *pkgdoc.PkgDoc) symbolDefinition {
	var overview strings.Builder
	overview.WriteString("const ")
	overview.WriteString(c.Name())
	if !isUntypedType(c.Type()) {
		overview.WriteString(" ")
		overview.WriteString(d.typeString(c.Type()))
	}
	overview.WriteString(" = ")
	overview.WriteString(c.Val().String())

	var detail string
	if pkgDoc != nil {
		detail = pkgDoc.Consts[c.Name()]
	}

	return symbolDefinition{
		TypeHint:     c.Type(),
		SourceObject: c,

		ID: XGoDefinitionIdentifier{
			Package: new(xgoutil.PkgPath(c.Pkg())),
			Name:    new(c.Name()),
		},
		Overview: overview.String(),
		Detail:   detail,

		CompletionItemLabel:            c.Name(),
		CompletionItemKind:             ConstantCompletion,
		CompletionItemInsertText:       c.Name(),
		CompletionItemInsertTextFormat: PlainTextTextFormat,
	}
}

// definitionForType describes the provided type in the source context.
func (d typeDisplay) definitionForType(typeName *gotypes.TypeName, name string, pkgDoc *pkgdoc.PkgDoc) symbolDefinition {
	overview := d.typeOverview(typeName, name)

	var detail string
	if pkgDoc != nil {
		typeDoc, ok := pkgDoc.Types[typeName.Name()]
		if ok {
			detail = typeDoc.Doc
		}
	}

	return symbolDefinition{
		TypeHint:     typeName.Type(),
		SourceObject: typeName,

		ID: XGoDefinitionIdentifier{
			Package: new(xgoutil.PkgPath(typeName.Pkg())),
			Name:    new(typeName.Name()),
		},
		Overview: overview,
		Detail:   detail,

		CompletionItemLabel:            name,
		CompletionItemKind:             typeCompletionKind(typeName.Type()),
		CompletionItemInsertText:       name,
		CompletionItemInsertTextFormat: PlainTextTextFormat,
	}
}

// typeCompletionKind classifies a type by its underlying representation.
// Incomplete types use the generic class kind.
func typeCompletionKind(typ gotypes.Type) CompletionItemKind {
	if typ == nil {
		return ClassCompletion
	}
	switch typ.Underlying().(type) {
	case *gotypes.Interface:
		return InterfaceCompletion
	case *gotypes.Struct:
		return StructCompletion
	default:
		return ClassCompletion
	}
}

// definitionForFunc describes the provided function or method.
func (d typeDisplay) definitionForFunc(fun *gotypes.Func, recvTypeName string, pkgDoc *pkgdoc.PkgDoc) symbolDefinition {
	overview, parsedRecvTypeName, parsedName, overloadID := d.funcOverview(fun)
	if recvTypeName == "" {
		recvTypeName = parsedRecvTypeName
	}

	detail := functionDocumentation(fun, pkgDoc)

	idName := parsedName
	if recvTypeName != "" {
		idName = recvTypeName + "." + idName
	}
	return symbolDefinition{
		TypeHint:       fun.Type(),
		Implementation: fun,
		SourceObject:   fun,

		ID: XGoDefinitionIdentifier{
			Package:    new(xgoutil.PkgPath(fun.Pkg())),
			Name:       &idName,
			OverloadID: overloadID,
		},
		Overview: overview,
		Detail:   detail,

		CompletionItemLabel:            parsedName,
		CompletionItemKind:             FunctionCompletion,
		CompletionItemInsertText:       parsedName,
		CompletionItemInsertTextFormat: PlainTextTextFormat,
	}
}

// nameFunctionOverview displays the callable's name while leaving its
// definition identifier and insertion spelling available to the caller.
func (def *symbolDefinition) nameFunctionOverview(name string) {
	if rest, ok := strings.CutPrefix(def.Overview, "func "+def.CompletionItemLabel); ok &&
		(strings.HasPrefix(rest, "(") || strings.HasPrefix(rest, "[")) {
		def.Overview = "func " + name + rest
	}
}

// useIndexedImplementationName shows the implementation's written or inserted
// name in the overview when it includes the overload index.
func (def *symbolDefinition) useIndexedImplementationName(name string) {
	if fun := def.function(); fun != nil && isDirectOverloadImplementationName(fun, name) {
		def.nameFunctionOverview(name)
	}
}

// functionValueName returns the selector spelling for inserting a function value.
func (def symbolDefinition) functionValueName() string {
	if def.functionValueSpelling != "" {
		return def.functionValueSpelling
	}
	if def.staticMemberName != "" {
		return def.staticMemberName
	}
	if fun := def.function(); fun != nil {
		return functionValueName(fun)
	}
	return def.Implementation.Name()
}

// function returns the implementation when it is a function declaration.
func (def symbolDefinition) function() *gotypes.Func {
	fun, _ := def.Implementation.(*gotypes.Func)
	return fun
}

// functionValueName preserves the declared spelling needed to refer to a
// function value instead of invoking a lowercase XGo alias.
func functionValueName(fun *gotypes.Func) string {
	name := fun.Name()
	if xgoutil.IsMarkedAsXGoPackage(fun.Pkg()) {
		if _, method, ok := xgoutil.TemplateMethodName(fun, true); ok {
			return method
		}
		if function, ok := xgoutil.SplitXGoxFuncName(name); ok {
			return function
		}
	}
	return name
}

// definitionForPkg describes the provided package import.
func definitionForPkg(pkgName *gotypes.PkgName, pkgDoc *pkgdoc.PkgDoc) symbolDefinition {
	var detail string
	if pkgDoc != nil {
		detail = pkgDoc.Doc
	}

	return symbolDefinition{
		TypeHint: pkgName.Type(),

		ID: XGoDefinitionIdentifier{
			Package: new(xgoutil.PkgPath(pkgName.Imported())),
		},
		Overview: "package " + pkgName.Name(),
		Detail:   detail,

		CompletionItemLabel:            pkgName.Name(),
		CompletionItemKind:             ModuleCompletion,
		CompletionItemInsertText:       pkgName.Name(),
		CompletionItemInsertTextFormat: PlainTextTextFormat,
	}
}

// functionDocumentation returns a function or method's source documentation.
func functionDocumentation(fun *gotypes.Func, doc *pkgdoc.PkgDoc) string {
	if doc == nil {
		return ""
	}
	if fun.Signature().Recv() == nil {
		return doc.Funcs[fun.Name()]
	}
	recvTypeName, _, _ := displayedFuncName(fun)
	if typeDoc := doc.Types[recvTypeName]; typeDoc != nil {
		if comment, ok := typeDoc.Methods[fun.Name()]; ok {
			return comment
		}
	}
	if gotypes.IsInterface(fun.Signature().Recv().Type()) {
		return memberDocumentation(fun, doc)
	}
	return ""
}

// overloadMarkerDocumentation returns the archived comment on an overload declaration.
func overloadMarkerDocumentation(marker *gotypes.Const, doc *pkgdoc.PkgDoc) string {
	if marker == nil || doc == nil {
		return ""
	}
	return doc.Consts[marker.Name()]
}

// memberDocumentation looks up a field or interface method by declaration
// identity. Public types can expose members declared by private types.
func memberDocumentation(member gotypes.Object, doc *pkgdoc.PkgDoc) string {
	pkg := member.Pkg()
	if pkg == nil {
		return ""
	}
	member = types.ObjectOrigin(member)
	_, method := member.(*gotypes.Func)
	scope := pkg.Scope()
	for _, name := range scope.Names() {
		typeDoc := doc.Types[name]
		if typeDoc == nil {
			continue
		}
		comments := typeDoc.Fields
		if method {
			comments = typeDoc.Methods
		}
		comment, ok := comments[member.Name()]
		if !ok {
			continue
		}
		typ, ok := scope.Lookup(name).(*gotypes.TypeName)
		if !ok || typ.Type() == nil {
			continue
		}
		container := typ.Type()
		if !method {
			container = xgoutil.DerefType(container.Underlying())
		}
		switch underlying := container.Underlying().(type) {
		case *gotypes.Struct:
			for candidate := range underlying.Fields() {
				if candidate.Origin() == member {
					return comment
				}
			}
		case *gotypes.Interface:
			for candidate := range underlying.ExplicitMethods() {
				if candidate.Origin() == member {
					return comment
				}
			}
		}
	}
	return ""
}
