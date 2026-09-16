package server

import (
	"fmt"
	gotypes "go/types"
	"slices"
	"strings"

	"github.com/goplus/xgolsw/pkgdoc"
)

var (
	// builtinDefinitionOverviews contains source-like descriptions of builtins.
	builtinDefinitionOverviews = map[string]string{
		// Variables.
		"nil": "var nil Type",

		// Constants.
		"false": "const false = 0 != 0",
		"iota":  "const iota = 0",
		"true":  "const true = 0 == 0",

		// Types.
		"any":        "type any",
		"bool":       "type bool",
		"byte":       "type byte",
		"complex64":  "type complex64",
		"complex128": "type complex128",
		"error":      "type error",
		"float32":    "type float32",
		"float64":    "type float64",
		"int":        "type int",
		"int8":       "type int8",
		"int16":      "type int16",
		"int32":      "type int32",
		"int64":      "type int64",
		"rune":       "type rune",
		"string":     "type string",
		"uint":       "type uint",
		"uint8":      "type uint8",
		"uint16":     "type uint16",
		"uint32":     "type uint32",
		"uint64":     "type uint64",
		"uintptr":    "type uintptr",

		// Functions.
		"append":  "func append(slice []T, elems ...T) []T",
		"cap":     "func cap(v Type) int",
		"clear":   "func clear(m Type)",
		"close":   "func close(c chan<- Type)",
		"complex": "func complex(r, i FloatType) ComplexType",
		"copy":    "func copy(dst, src []Type) int",
		"delete":  "func delete(m map[Type]Type1, key Type)",
		"imag":    "func imag(c ComplexType) FloatType",
		"len":     "func len(v Type) int",
		"make":    "func make(t Type, size ...IntegerType) Type",
		"max":     "func max(x Type, y ...Type) Type",
		"min":     "func min(x Type, y ...Type) Type",
		"new":     "func new(Type) *Type",
		"panic":   "func panic(v interface{})",
		"print":   "func print(args ...Type)",
		"println": "func println(args ...Type)",
		"real":    "func real(c ComplexType) FloatType",
		"recover": "func recover() interface{}",
	}

	// xgoBuiltinAliases contains aliases for XGo builtins.
	//
	// See github.com/goplus/xgo/cl.initBuiltin for the list of XGo builtin aliases.
	xgoBuiltinAliases = map[string]string{
		// Types.
		"bigfloat": "github.com/qiniu/x/xgo/ng#Bigfloat",
		"bigint":   "github.com/qiniu/x/xgo/ng#Bigint",
		"bigrat":   "github.com/qiniu/x/xgo/ng#Bigrat",
		"int128":   "github.com/qiniu/x/xgo/ng#Int128",
		"uint128":  "github.com/qiniu/x/xgo/ng#Uint128",

		// Functions.
		"blines":   "github.com/qiniu/x/osx#BLines",
		"create":   "os#Create",
		"echo":     "fmt#Println",
		"errorf":   "fmt#Errorf",
		"fprint":   "fmt#Fprint",
		"fprintf":  "fmt#Fprintf",
		"fprintln": "fmt#Fprintln",
		"lines":    "github.com/qiniu/x/osx#Lines",
		"newRange": "github.com/qiniu/x/xgo#NewRange__0",
		"open":     "os#Open",
		"print":    "fmt#Print",
		"printf":   "fmt#Printf",
		"println":  "fmt#Println",
		"sprint":   "fmt#Sprint",
		"sprintf":  "fmt#Sprintf",
		"sprintln": "fmt#Sprintln",
		// "type":     "reflect#TypeOf",
	}
)

// definitionForBuiltin describes a builtin using the provided package data.
func (d typeDisplay) definitionForBuiltin(obj gotypes.Object, importer gotypes.Importer, lookupPkgDoc func(string) (*pkgdoc.PkgDoc, error)) symbolDefinition {
	const pkgPath = "builtin"

	idName := obj.Name()
	if def, err := d.definitionForBuiltinAlias(idName, importer, lookupPkgDoc); err == nil {
		return def
	}

	overview, ok := builtinDefinitionOverviews[idName]
	if !ok {
		overview = "builtin " + idName
	}

	var detail string
	if pkgDoc, err := lookupPkgDoc(pkgPath); err == nil {
		if doc, ok := pkgDoc.Vars[idName]; ok {
			detail = doc
		} else if doc, ok := pkgDoc.Consts[idName]; ok {
			detail = doc
		} else if typeDoc, ok := pkgDoc.Types[idName]; ok {
			if doc, ok := typeDoc.Fields[idName]; ok {
				detail = doc
			} else if doc, ok := typeDoc.Methods[idName]; ok {
				detail = doc
			} else {
				detail = typeDoc.Doc
			}
		} else if doc, ok := pkgDoc.Funcs[idName]; ok {
			detail = doc
		}
	}

	completionItemKind := TextCompletion
	switch obj.(type) {
	case *gotypes.Nil:
		completionItemKind = VariableCompletion
	case *gotypes.Const:
		completionItemKind = ConstantCompletion
	case *gotypes.TypeName:
		completionItemKind = typeCompletionKind(obj.Type())
	case *gotypes.Builtin, *gotypes.Func:
		completionItemKind = FunctionCompletion
	}

	return symbolDefinition{
		TypeHint: obj.Type(),

		ID: XGoDefinitionIdentifier{
			Package: ToPtr(pkgPath),
			Name:    &idName,
		},
		Overview: overview,
		Detail:   detail,

		CompletionItemLabel:            obj.Name(),
		CompletionItemKind:             completionItemKind,
		CompletionItemInsertText:       obj.Name(),
		CompletionItemInsertTextFormat: PlainTextTextFormat,
	}
}

// builtinDefinitions describes builtins using the provided importer and documentation lookup.
func (d typeDisplay) builtinDefinitions(importer gotypes.Importer, lookupPkgDoc func(string) (*pkgdoc.PkgDoc, error)) []symbolDefinition {
	names := gotypes.Universe.Names()
	defs := make([]symbolDefinition, 0, len(names)+len(xgoBuiltinAliases))
	for _, name := range names {
		if _, ok := xgoBuiltinAliases[name]; ok {
			continue
		}
		if obj := gotypes.Universe.Lookup(name); obj != nil && obj.Pkg() == nil {
			defs = append(defs, d.definitionForBuiltin(obj, importer, lookupPkgDoc))
		}
	}
	for alias := range xgoBuiltinAliases {
		def, err := d.definitionForBuiltinAlias(alias, importer, lookupPkgDoc)
		if err != nil {
			continue
		}
		defs = append(defs, def)
	}
	return slices.Clip(defs)
}

// definitionForBuiltinAlias resolves a builtin alias using the provided package data.
func (d typeDisplay) definitionForBuiltinAlias(alias string, importer gotypes.Importer, lookupPkgDoc func(string) (*pkgdoc.PkgDoc, error)) (symbolDefinition, error) {
	ref, ok := xgoBuiltinAliases[alias]
	if !ok {
		return symbolDefinition{}, fmt.Errorf("unknown xgo builtin alias: %s", alias)
	}

	pkgPath, name, ok := strings.Cut(ref, "#")
	if !ok {
		return symbolDefinition{}, fmt.Errorf("invalid xgo builtin alias: %s", alias)
	}
	pkg, err := importer.Import(pkgPath)
	if err != nil {
		return symbolDefinition{}, fmt.Errorf("failed to import package for xgo builtin alias %q: %w", alias, err)
	}
	pkgDoc, _ := lookupPkgDoc(pkgPath)

	obj := pkg.Scope().Lookup(name)
	if obj == nil {
		return symbolDefinition{}, fmt.Errorf("symbol %s not found in package %s", name, pkgPath)
	}
	var def symbolDefinition
	switch obj := obj.(type) {
	case *gotypes.TypeName:
		def = d.definitionForType(obj, pkgDoc)
	case *gotypes.Func:
		def = d.definitionForFunc(obj, "", pkgDoc)
	default:
		return symbolDefinition{}, fmt.Errorf("unexpected object type for xgo builtin alias %q: %T", alias, obj)
	}

	def.ID = XGoDefinitionIdentifier{
		Package: ToPtr("builtin"),
		Name:    &alias,
	}
	def.CompletionItemLabel = alias
	def.CompletionItemInsertText = alias
	return def, nil
}
