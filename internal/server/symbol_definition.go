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
			defs = append(defs, d.definitionForType(obj, pkgDoc))
		case *gotypes.Func:
			if funcOverloads := xgoutil.ExpandXGoOverloadableFunc(obj); funcOverloads != nil {
				for _, funcOverload := range funcOverloads {
					defs = append(defs, d.definitionForFunc(funcOverload, "", pkgDoc))
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
		TypeHint: v.Type(),

		ID: XGoDefinitionIdentifier{
			Package: ToPtr(xgoutil.PkgPath(v.Pkg())),
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
		TypeHint: c.Type(),

		ID: XGoDefinitionIdentifier{
			Package: ToPtr(xgoutil.PkgPath(c.Pkg())),
			Name:    ToPtr(c.Name()),
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
func (d typeDisplay) definitionForType(typeName *gotypes.TypeName, pkgDoc *pkgdoc.PkgDoc) symbolDefinition {
	overview := d.typeOverview(typeName)

	var detail string
	if pkgDoc != nil {
		typeDoc, ok := pkgDoc.Types[typeName.Name()]
		if ok {
			detail = typeDoc.Doc
		}
	}

	return symbolDefinition{
		TypeHint: typeName.Type(),

		ID: XGoDefinitionIdentifier{
			Package: ToPtr(xgoutil.PkgPath(typeName.Pkg())),
			Name:    ToPtr(typeName.Name()),
		},
		Overview: overview,
		Detail:   detail,

		CompletionItemLabel:            typeName.Name(),
		CompletionItemKind:             typeCompletionKind(typeName.Type()),
		CompletionItemInsertText:       typeName.Name(),
		CompletionItemInsertTextFormat: PlainTextTextFormat,
	}
}

// typeCompletionKind classifies a type by its underlying representation.
func typeCompletionKind(typ gotypes.Type) CompletionItemKind {
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
		TypeHint: fun.Type(),

		ID: XGoDefinitionIdentifier{
			Package:    ToPtr(xgoutil.PkgPath(fun.Pkg())),
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

// definitionForPkg describes the provided package import.
func definitionForPkg(pkgName *gotypes.PkgName, pkgDoc *pkgdoc.PkgDoc) symbolDefinition {
	var detail string
	if pkgDoc != nil {
		detail = pkgDoc.Doc
	}

	return symbolDefinition{
		TypeHint: pkgName.Type(),

		ID: XGoDefinitionIdentifier{
			Package: ToPtr(xgoutil.PkgPath(pkgName.Imported())),
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
	recvTypeName, _, _, _ := displayedFuncName(fun)
	if recvTypeName == "" && fun.Signature().Recv() != nil {
		return ""
	}
	name := fun.Name()
	if recvTypeName == "" || xgoutil.IsXGotMethodName(name) {
		return doc.Funcs[name]
	}
	if typeDoc := doc.Types[recvTypeName]; typeDoc != nil {
		return typeDoc.Methods[name]
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
		if !ok {
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
