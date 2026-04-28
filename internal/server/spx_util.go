package server

import (
	gotypes "go/types"
	"path"
	"regexp"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// spxEventHandlerFuncNameRE is the regular expression of the spx event handler
// function name.
var spxEventHandlerFuncNameRE = regexp.MustCompile(`^on[A-Z]\w*$`)

// IsSpxEventHandlerFuncName reports whether the given function name is an
// spx event handler function name.
func IsSpxEventHandlerFuncName(name string) bool {
	return spxEventHandlerFuncNameRE.MatchString(name)
}

// IsInSpxPkg reports whether the given object is defined in the spx package.
func IsInSpxPkg(obj gotypes.Object) bool {
	return obj != nil && obj.Pkg() == GetSpxPkg()
}

// GetSimplifiedTypeString returns the string representation of the given type,
// with the spx package name omitted while other packages use their short names.
func GetSimplifiedTypeString(typ gotypes.Type) string {
	return gotypes.TypeString(typ, func(p *gotypes.Package) string {
		if p == GetSpxPkg() {
			return ""
		}
		return p.Name()
	})
}

// resolvedNamedType resolves aliases and pointer indirections until it reaches
// a named type. It returns nil if typ does not resolve to a named type.
func resolvedNamedType(typ gotypes.Type) *gotypes.Named {
	seen := make(map[gotypes.Type]struct{})
	for typ != nil {
		if _, ok := seen[typ]; ok {
			return nil
		}
		seen[typ] = struct{}{}

		typ = gotypes.Unalias(typ)
		switch t := typ.(type) {
		case *gotypes.Named:
			return t
		case *gotypes.Pointer:
			typ = t.Elem()
		default:
			return nil
		}
	}
	return nil
}

// SelectorTypeNameForIdent returns the selector type name for the given
// identifier. It returns empty string if no selector can be inferred.
func SelectorTypeNameForIdent(proj *xgo.Project, ident *ast.Ident) string {
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return ""
	}
	astPkg, _ := proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(proj.Fset, astPkg, ident)
	if astFile == nil {
		return ""
	}

	obj := typeInfo.ObjectOf(ident)
	if obj == nil || obj.Pkg() == nil {
		return ""
	}

	// Handle spx package's implicit receiver semantics.
	if typeName := tryGetSpxImplicitReceiver(proj, astFile, ident, obj); typeName != "" {
		return typeName
	}

	// Infer type from object properties.
	return getTypeFromObject(typeInfo, obj)
}

// tryGetSpxImplicitReceiver handles spx package's special implicit receiver semantics.
func tryGetSpxImplicitReceiver(proj *xgo.Project, astFile *ast.File, ident *ast.Ident, obj gotypes.Object) string {
	if !IsInSpxPkg(obj) {
		return ""
	}
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return ""
	}
	astPkg, _ := proj.ASTPackage()

	astFileScope := typeInfo.Scopes[astFile]
	innermostScope := xgoutil.InnermostScopeAt(proj.Fset, typeInfo, astPkg, ident.Pos())

	// Check if we're in the right scope context.
	if innermostScope != astFileScope && (!astFile.HasShadowEntry() || xgoutil.InnermostScopeAt(proj.Fset, typeInfo, astPkg, astFile.ShadowEntry.Pos()) != innermostScope) {
		return ""
	}

	spxFile := xgoutil.NodeFilename(proj.Fset, ident)
	if path.Base(spxFile) == "main.spx" {
		return "Game"
	}
	return "Sprite"
}

// getTypeFromObject infers type from the identifier's object.
func getTypeFromObject(typeInfo *types.Info, obj gotypes.Object) string {
	switch obj := obj.(type) {
	case *gotypes.Var:
		if !obj.IsField() {
			return ""
		}
		return findFieldOwnerType(typeInfo, obj)
	case *gotypes.Func:
		recv := obj.Signature().Recv()
		if recv == nil {
			return ""
		}
		return extractTypeName(xgoutil.DerefType(recv.Type()))
	}
	return ""
}

// extractTypeName extracts a clean type name from a types.Type.
func extractTypeName(typ gotypes.Type) string {
	switch typ := typ.(type) {
	case *gotypes.Named:
		obj := typ.Obj()
		typeName := obj.Name()
		if IsInSpxPkg(obj) && typeName == "SpriteImpl" {
			return "Sprite"
		}
		return typeName
	case *gotypes.Interface:
		if typ.String() == "interface{}" {
			return ""
		}
		return typ.String()
	}
	return ""
}

// findFieldOwnerType finds the type that owns a given field.
func findFieldOwnerType(typeInfo *types.Info, field *gotypes.Var) string {
	if !field.IsField() {
		return ""
	}

	fieldPkg := field.Pkg()
	if fieldPkg == nil {
		return ""
	}

	// Search through named types in the same package.
	scope := fieldPkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		typeName, ok := obj.(*gotypes.TypeName)
		if !ok {
			continue
		}

		named, ok := xgoutil.DerefType(typeName.Type()).(*gotypes.Named)
		if !ok || !xgoutil.IsNamedStructType(named) {
			continue
		}

		// Check if this struct contains our field.
		if ownerName := checkStructForField(named, field, fieldPkg); ownerName != "" {
			return ownerName
		}
	}

	// Fallback: search through all type definitions.
	return searchAllDefsForField(typeInfo, field)
}

// checkStructForField checks if a struct type contains the given field.
func checkStructForField(named *gotypes.Named, field *gotypes.Var, fieldPkg *gotypes.Package) string {
	selection, ok := gotypes.LookupSelection(named, false, fieldPkg, field.Name())
	if !ok {
		return ""
	}

	foundField, ok := selection.Obj().(*gotypes.Var)
	if !ok || foundField != field {
		return ""
	}

	typeName := named.Obj().Name()
	if IsInSpxPkg(named.Obj()) && typeName == "SpriteImpl" {
		return "Sprite"
	}
	return typeName
}

// PropertyTargetNamedTypeForCall resolves the *types.Named that owns the
// properties being addressed by a call expression.
//
// Resolution rules:
//   - If call.Fun is a SelectorExpr (e.g. x.Method(...) or getObj().Method(...)),
//     the receiver type is read from typeInfo.Types[sel.X]. Returns nil when the
//     receiver type cannot be resolved or is not a named type. No implicit-receiver
//     fallback is attempted.
//   - If call.Fun is a bare identifier (implicit receiver), the target type is
//     deduced from the file name: "main.spx" maps to "Game"; any other
//     "TypeName.spx" maps to "TypeName".
//
// spxFile is the file containing the call expression (e.g. "MySprite.spx").
// mainSpxFile is the main entry file (e.g. "main.spx").
//
// Returns nil when the target cannot be determined.
func PropertyTargetNamedTypeForCall(typeInfo *types.Info, call *ast.CallExpr, spxFile, mainSpxFile string) *gotypes.Named {
	if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
		if tv, ok := typeInfo.Types[sel.X]; ok && tv.Type != nil {
			return resolvedNamedType(tv.Type)
		}
		return nil
	}
	// Implicit receiver: derive target from the file name.
	var typeName string
	switch path.Base(spxFile) {
	case path.Base(mainSpxFile):
		typeName = "Game"
	default:
		typeName = strings.TrimSuffix(path.Base(spxFile), ".spx")
	}
	if typeName == "" {
		return nil
	}
	obj := typeInfo.Pkg.Scope().Lookup(typeName)
	if obj == nil {
		return nil
	}
	tn, ok := obj.(*gotypes.TypeName)
	if !ok {
		return nil
	}
	return resolvedNamedType(tn.Type())
}

// searchAllDefsForField is a fallback method that searches all type definitions.
func searchAllDefsForField(typeInfo *types.Info, field *gotypes.Var) string {
	fieldPkg := field.Pkg()
	for _, def := range typeInfo.Defs {
		if def == nil || def.Pkg() != fieldPkg {
			continue
		}

		named, ok := xgoutil.DerefType(def.Type()).(*gotypes.Named)
		if !ok || !xgoutil.IsNamedStructType(named) {
			continue
		}

		if ownerName := checkStructForField(named, field, fieldPkg); ownerName != "" {
			return ownerName
		}
	}
	return ""
}
