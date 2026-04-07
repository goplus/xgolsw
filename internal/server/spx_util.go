package server

import (
	"go/types"
	"path"
	"regexp"
	"strings"

	xgoast "github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
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
func IsInSpxPkg(obj types.Object) bool {
	return obj != nil && obj.Pkg() == GetSpxPkg()
}

// GetSimplifiedTypeString returns the string representation of the given type,
// with the spx package name omitted while other packages use their short names.
func GetSimplifiedTypeString(typ types.Type) string {
	return types.TypeString(typ, func(p *types.Package) string {
		if p == GetSpxPkg() {
			return ""
		}
		return p.Name()
	})
}

// resolvedNamedType resolves aliases and pointer indirections until it reaches
// a named type. It returns nil if typ does not resolve to a named type.
func resolvedNamedType(typ types.Type) *types.Named {
	seen := make(map[types.Type]struct{})
	for typ != nil {
		if _, ok := seen[typ]; ok {
			return nil
		}
		seen[typ] = struct{}{}

		typ = types.Unalias(typ)
		switch t := typ.(type) {
		case *types.Named:
			return t
		case *types.Pointer:
			typ = t.Elem()
		default:
			return nil
		}
	}
	return nil
}

// SelectorTypeNameForIdent returns the selector type name for the given
// identifier. It returns empty string if no selector can be inferred.
func SelectorTypeNameForIdent(proj *xgo.Project, ident *xgoast.Ident) string {
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return ""
	}

	obj := typeInfo.ObjectOf(ident)
	if obj == nil || obj.Pkg() == nil {
		return ""
	}

	if named := SelectorNamedTypeForIdent(proj, ident); named != nil {
		return selectorDisplayTypeNameForIdent(proj, typeInfo, ident, obj, named)
	}

	// Fall back to the declaration owner when no concrete use-site selector can
	// be inferred.
	return getTypeFromObject(typeInfo, obj)
}

// SelectorNamedTypeForIdent returns the concrete named type on which the
// identifier is selected. It prefers use-site surface types over declaration
// owners and returns nil when no selector can be inferred.
func SelectorNamedTypeForIdent(proj *xgo.Project, ident *xgoast.Ident) *types.Named {
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	astPkg, _ := proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(proj.Fset, astPkg, ident)
	if astFile == nil {
		return nil
	}

	if sel := selectorExprForIdent(proj, astFile, ident); sel != nil {
		if selection := typeInfo.Selections[sel]; selection != nil {
			if named := resolvedNamedType(selection.Recv()); named != nil {
				return named
			}
		}
		if tv, ok := typeInfo.Types[sel.X]; ok && tv.Type != nil {
			if named := resolvedNamedType(tv.Type); named != nil {
				return named
			}
		}
	}

	obj := typeInfo.ObjectOf(ident)
	if obj == nil || obj.Pkg() == nil {
		return nil
	}

	return tryGetSpxImplicitReceiverNamedType(proj, typeInfo, astPkg, astFile, ident, obj)
}

// selectorExprForIdent returns the selector expression that owns the given
// identifier when the identifier is used as the selector part of "x.sel".
func selectorExprForIdent(proj *xgo.Project, astFile *xgoast.File, ident *xgoast.Ident) *xgoast.SelectorExpr {
	if proj == nil || astFile == nil || ident == nil {
		return nil
	}

	var path []xgoast.Node
	xgoutil.WalkPathEnclosingInterval(astFile, ident.Pos(), ident.End(), false, func(node xgoast.Node) bool {
		path = append(path, node)
		return len(path) < 2
	})
	if len(path) < 2 {
		return nil
	}

	sel, ok := path[1].(*xgoast.SelectorExpr)
	if !ok || sel.Sel != ident {
		return nil
	}
	return sel
}

// selectorDisplayTypeNameForIdent maps a concrete selected type to the public
// surface name used by spx definition IDs.
func selectorDisplayTypeNameForIdent(proj *xgo.Project, typeInfo *xgotypes.Info, ident *xgoast.Ident, obj types.Object, named *types.Named) string {
	if named == nil || named.Obj() == nil || !IsInSpxPkg(obj) {
		if named != nil && named.Obj() != nil {
			return spxPublicTypeName(named.Obj().Pkg(), named.Obj().Name())
		}
		return ""
	}

	astPkg, _ := proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(proj.Fset, astPkg, ident)
	if sel := selectorExprForIdent(proj, astFile, ident); sel != nil {
		if xIdent, ok := sel.X.(*xgoast.Ident); ok {
			if xObj := typeInfo.ObjectOf(xIdent); xObj != nil {
				if xVar, ok := xObj.(*types.Var); ok && xVar.IsField() {
					if surfaceTypeName := spxSurfaceTypeNameForFieldOwner(findFieldOwnerType(typeInfo, xVar), xVar.Type()); surfaceTypeName != "" {
						return surfaceTypeName
					}
				}
			}
		}
	}

	return spxPublicTypeName(named.Obj().Pkg(), named.Obj().Name())
}

// tryGetSpxImplicitReceiverNamedType handles spx package implicit receiver
// semantics by resolving the concrete work-class type from the current file.
func tryGetSpxImplicitReceiverNamedType(proj *xgo.Project, typeInfo *xgotypes.Info, astPkg *xgoast.Package, astFile *xgoast.File, ident *xgoast.Ident, obj types.Object) *types.Named {
	if !IsInSpxPkg(obj) {
		return nil
	}
	if typeInfo == nil {
		return nil
	}

	astFileScope := typeInfo.Scopes[astFile]
	innermostScope := xgoutil.InnermostScopeAt(proj.Fset, typeInfo, astPkg, ident.Pos())

	// Check if we're in the right scope context.
	if innermostScope != astFileScope && (!astFile.HasShadowEntry() || xgoutil.InnermostScopeAt(proj.Fset, typeInfo, astPkg, astFile.ShadowEntry.Pos()) != innermostScope) {
		return nil
	}

	spxFile := xgoutil.NodeFilename(proj.Fset, ident)
	return namedTypeForSpxFile(typeInfo, spxFile, "main.spx")
}

// namedTypeForSpxFile returns the concrete work-class named type associated
// with the given spx file.
func namedTypeForSpxFile(typeInfo *xgotypes.Info, spxFile, mainSpxFile string) *types.Named {
	if typeInfo == nil || typeInfo.Pkg == nil {
		return nil
	}

	typeName := spxWorkTypeNameForFile(spxFile, mainSpxFile)
	if typeName == "" {
		return nil
	}

	obj := typeInfo.Pkg.Scope().Lookup(typeName)
	if obj == nil {
		return nil
	}
	tn, ok := obj.(*types.TypeName)
	if !ok {
		return nil
	}
	return resolvedNamedType(tn.Type())
}

// spxWorkTypeNameForFile returns the work-class type name implied by spxFile.
func spxWorkTypeNameForFile(spxFile, mainSpxFile string) string {
	if spxFile == "" {
		return ""
	}
	if path.Base(spxFile) == path.Base(mainSpxFile) {
		return "Game"
	}
	return strings.TrimSuffix(path.Base(spxFile), ".spx")
}

// spxSpriteNameForFile returns the sprite resource name implied by spxFile. It
// returns empty string for the main work file.
func spxSpriteNameForFile(spxFile, mainSpxFile string) string {
	if spxFile == "" || path.Base(spxFile) == path.Base(mainSpxFile) {
		return ""
	}
	return strings.TrimSuffix(path.Base(spxFile), ".spx")
}

// getTypeFromObject infers type from the identifier's object.
func getTypeFromObject(typeInfo *xgotypes.Info, obj types.Object) string {
	switch obj := obj.(type) {
	case *types.Var:
		if !obj.IsField() {
			return ""
		}
		return findFieldOwnerType(typeInfo, obj)
	case *types.Func:
		sig, ok := obj.Type().(*types.Signature)
		if !ok {
			return ""
		}
		recv := sig.Recv()
		if recv == nil {
			return ""
		}
		return extractTypeName(xgoutil.DerefType(recv.Type()))
	}
	return ""
}

// extractTypeName extracts a clean type name from a types.Type.
func extractTypeName(typ types.Type) string {
	switch typ := typ.(type) {
	case *types.Named:
		if obj := typ.Obj(); obj != nil {
			return obj.Name()
		}
	case *types.Interface:
		if typ.String() == "interface{}" {
			return ""
		}
		return typ.String()
	}
	return ""
}

// findFieldOwnerType finds the type that owns a given field.
func findFieldOwnerType(typeInfo *xgotypes.Info, field *types.Var) string {
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
		typeName, ok := obj.(*types.TypeName)
		if !ok {
			continue
		}

		named, ok := xgoutil.DerefType(typeName.Type()).(*types.Named)
		if !ok || !xgoutil.IsNamedStructType(named) {
			continue
		}

		// Check if this struct contains our field.
		if ownerName := checkStructForField(named, field, fieldPkg); ownerName != "" {
			return ownerName
		}
	}

	// Fallback: search through all type definitions.
	if typeInfo == nil {
		return ""
	}
	return searchAllDefsForField(typeInfo, field)
}

// checkStructForField checks if a struct type contains the given field.
func checkStructForField(named *types.Named, field *types.Var, fieldPkg *types.Package) string {
	selection, ok := types.LookupSelection(named, false, fieldPkg, field.Name())
	if !ok {
		return ""
	}

	foundField, ok := selection.Obj().(*types.Var)
	if !ok || foundField != field {
		return ""
	}

	typeName := named.Obj().Name()
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
func PropertyTargetNamedTypeForCall(typeInfo *xgotypes.Info, call *xgoast.CallExpr, spxFile, mainSpxFile string) *types.Named {
	if sel, ok := call.Fun.(*xgoast.SelectorExpr); ok {
		if tv, ok := typeInfo.Types[sel.X]; ok && tv.Type != nil {
			return resolvedNamedType(tv.Type)
		}
		return nil
	}
	return namedTypeForSpxFile(typeInfo, spxFile, mainSpxFile)
}

// searchAllDefsForField is a fallback method that searches all type definitions.
func searchAllDefsForField(typeInfo *xgotypes.Info, field *types.Var) string {
	if typeInfo == nil {
		return ""
	}
	fieldPkg := field.Pkg()
	for _, def := range typeInfo.Defs {
		if def == nil || def.Pkg() != fieldPkg {
			continue
		}

		named, ok := xgoutil.DerefType(def.Type()).(*types.Named)
		if !ok || !xgoutil.IsNamedStructType(named) {
			continue
		}

		if ownerName := checkStructForField(named, field, fieldPkg); ownerName != "" {
			return ownerName
		}
	}
	return ""
}
