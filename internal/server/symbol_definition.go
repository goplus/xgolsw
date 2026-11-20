package server

import (
	"fmt"
	gotypes "go/types"
	"html/template"
	"slices"
	"strings"

	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// symbolDefinition describes a symbol for hover, completion, and definition links.
type symbolDefinition struct {
	// TypeHint represents a type hint for this definition. It may be nil if
	// the definition has no associated type.
	TypeHint gotypes.Type
	Function *gotypes.Func

	// SourceObject is the source declaration before overload expansion.
	// Function supplies the implementation signature and documentation.
	SourceObject gotypes.Object

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
			if funcOverloads := xgoutil.ExpandXGoOverloadableFunc(obj); funcOverloads != nil {
				for _, funcOverload := range funcOverloads {
					def := d.definitionForFunc(funcOverload, "", pkgDoc)
					def.SourceObject = obj
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
			detail = fieldDocumentation(v, pkgDoc)
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
		TypeHint:     fun.Type(),
		Function:     fun,
		SourceObject: fun,

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
	def.Overview = strings.Replace(def.Overview, "func "+def.CompletionItemLabel, "func "+name, 1)
}

// functionValueName returns the selector spelling for inserting a function value.
func (def symbolDefinition) functionValueName() string {
	if def.functionValueSpelling != "" {
		return def.functionValueSpelling
	}
	if def.staticMemberName != "" {
		return def.staticMemberName
	}
	return functionValueName(def.Function)
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
		return typeDoc.Methods[fun.Name()]
	}
	return ""
}

// fieldDocumentation finds the field's declaration among its package types.
// Defined types can share field objects, but only the declaring type has a
// field entry in package documentation, even when its comment is empty.
func fieldDocumentation(field *gotypes.Var, doc *pkgdoc.PkgDoc) string {
	pkg := field.Pkg()
	if pkg == nil {
		return ""
	}
	field = field.Origin()
	for _, name := range pkg.Scope().Names() {
		typeDoc := doc.Types[name]
		if typeDoc == nil {
			continue
		}
		comment, ok := typeDoc.Fields[field.Name()]
		if !ok {
			continue
		}
		typ, ok := pkg.Scope().Lookup(name).(*gotypes.TypeName)
		if !ok || typ.Type() == nil {
			continue
		}
		st, ok := typ.Type().Underlying().(*gotypes.Struct)
		if !ok {
			continue
		}
		for candidate := range st.Fields() {
			if candidate.Origin() == field {
				return comment
			}
		}
	}
	return ""
}
