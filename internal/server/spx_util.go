package server

import (
	gotypes "go/types"
	"path"
	"regexp"
	"strings"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/cl"
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
	if obj == nil {
		return false
	}
	pkg := obj.Pkg()
	return pkg != nil && pkg.Path() == SpxPkgPath && pkg == GetSpxPkg()
}

// GetSimplifiedTypeString returns the string representation of the given type,
// with the spx package name omitted while other packages use their short names.
func GetSimplifiedTypeString(typ gotypes.Type) string {
	return gotypes.TypeString(typ, func(p *gotypes.Package) string {
		if p.Path() == SpxPkgPath && p == GetSpxPkg() {
			return ""
		}
		return p.Name()
	})
}

// sourceParamLabel formats a source-facing function parameter label.
func sourceParamLabel(sig *gotypes.Signature, params *gotypes.Tuple, paramIndex int) string {
	param := params.At(paramIndex)
	paramType := xgoutil.SourceParamType(param)
	typeName := GetSimplifiedTypeString(paramType)
	if sig.Variadic() && paramIndex == params.Len()-1 {
		if slice, ok := paramType.(*gotypes.Slice); ok {
			typeName = "..." + GetSimplifiedTypeString(slice.Elem())
		}
	}
	return xgoutil.SourceParamName(param) + " " + typeName
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

	if field, ok := obj.(*gotypes.Var); ok && field.IsField() {
		for node := range xgoutil.PathEnclosingIntervalNodes(astFile, ident.Pos(), ident.End(), false) {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel != ident {
				continue
			}
			if name := fieldSelectorTypeName(typeInfo.TypeOf(selector.X), field); name != "" {
				return name
			}
			break
		}
		if astFile.IsClass {
			className, _ := cl.GetFileClassType(astFile, xgoutil.NodeFilename(proj.Fset, astFile), proj.Mod.LookupClass)
			if class := typeInfo.Pkg.Scope().Lookup(className); class != nil {
				if name := fieldSelectorTypeName(class.Type(), field); name != "" {
					return name
				}
			}
		}
	}
	return memberTypeName(proj, obj)
}

// fieldSelectorTypeName returns the type used to select field through receiver.
// It shares the member traversal used by completion, including promoted fields.
func fieldSelectorTypeName(receiver gotypes.Type, field *gotypes.Var) string {
	for member := range xgoutil.StructMembers(resolvedNamedType(receiver)) {
		if selected, ok := member.Member.(*gotypes.Var); ok && selected.Origin() == field.Origin() {
			return extractTypeName(member.Selector)
		}
	}
	return ""
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

	spxFile := proj.Fset.File(ident.Pos()).Name()
	if path.Base(spxFile) == "main.spx" {
		return "Game"
	}
	return "Sprite"
}

// memberTypeName returns the declaring type name of a field or method.
func memberTypeName(proj *xgo.Project, obj gotypes.Object) string {
	switch obj := obj.(type) {
	case *gotypes.Var:
		if !obj.IsField() {
			return ""
		}
		return findFieldOwnerType(proj, obj)
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

// findFieldOwnerType returns the declaring type name of field. Project fields
// use their source declaration because defined types can share field objects.
func findFieldOwnerType(proj *xgo.Project, field *gotypes.Var) string {
	field = field.Origin()
	if typeInfo, _ := proj.TypeInfo(); typeInfo != nil && field.Pkg() == typeInfo.Pkg {
		astFile := sourceASTFile(proj, field.Pos())
		if astFile == nil {
			return ""
		}
		var structType *ast.StructType
		for node := range xgoutil.PathEnclosingIntervalNodes(astFile, field.Pos(), field.Pos(), false) {
			switch node := node.(type) {
			case *ast.StructType:
				if structType != nil {
					return ""
				}
				structType = node
			case *ast.TypeSpec:
				if node.Type == structType {
					return node.Name.Name
				}
				return ""
			case *ast.GenDecl:
				if structType == nil && node == astFile.ClassFields {
					name, _ := cl.GetFileClassType(astFile, xgoutil.NodeFilename(proj.Fset, astFile), proj.Mod.LookupClass)
					return name
				}
				return ""
			}
		}
		return ""
	}

	// Imported fields have no project AST. Only a unique containing type can
	// identify their owner without a receiver expression.
	pkg := field.Pkg()
	if pkg == nil {
		return ""
	}
	var owner *gotypes.Named
	for _, name := range pkg.Scope().Names() {
		obj, ok := pkg.Scope().Lookup(name).(*gotypes.TypeName)
		if !ok {
			continue
		}
		named, ok := obj.Type().(*gotypes.Named)
		if !ok {
			continue
		}
		structType, ok := named.Underlying().(*gotypes.Struct)
		if !ok {
			continue
		}
		for member := range structType.Fields() {
			if member == field {
				if owner != nil {
					return ""
				}
				owner = named
			}
		}
	}
	if owner == nil {
		return ""
	}
	return extractTypeName(owner)
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
