package server

import (
	gotypes "go/types"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
)

// SpxPkgPath is the path to the spx package.
const SpxPkgPath = "github.com/goplus/spx/v4"

// isSpxFramework reports whether spx supplies the registration's base classes.
func isSpxFramework(framework *modfile.Project) bool {
	return len(framework.PkgPaths) != 0 && framework.PkgPaths[0] == SpxPkgPath
}

// spxFrameworkForFile returns the spx class framework registration for filename, if any.
func spxFrameworkForFile(proj *xgo.Project, filename string) *modfile.Project {
	framework, ok := proj.Module().LookupClass(modfile.ClassExt(filename))
	if ok && isSpxFramework(framework) {
		return framework
	}
	return nil
}

// spxSymbols holds cached SDK declarations for one project state. Type keys
// preserve resource aliases even when their underlying types are all strings.
type spxSymbols struct {
	pkg   *gotypes.Package
	types map[gotypes.Type]string
}

// newSpxSymbols resolves SDK declarations through the project's importer.
// Missing registrations or exports leave the optional adapter inactive.
func newSpxSymbols(proj *xgo.Project) *spxSymbols {
	symbols := &spxSymbols{}
	for framework := range proj.Module().ClassFrameworks() {
		if !isSpxFramework(framework) {
			continue
		}
		pkg, err := proj.Import(SpxPkgPath)
		if err != nil {
			return symbols
		}
		symbols.pkg = pkg
		symbols.types = make(map[gotypes.Type]string)
		for _, name := range []string{
			"Game", "Sprite", "SpriteImpl", "BackdropName", "SpriteName", "SpriteCostumeName",
			"SpriteAnimationName", "SoundName", "WidgetName", "Direction", "layerAction",
			"dirAction", "EffectKind", "Key", "Edge", "RotationStyle", "PropertyName", "Value", "List",
		} {
			if obj := pkg.Scope().Lookup(name); obj != nil {
				symbols.types[obj.Type()] = name
			}
		}
		break
	}
	return symbols
}

// isSpxSymbol reports whether obj belongs to the project's registered SDK.
func (r *spxSymbols) isSpxSymbol(obj gotypes.Object) bool {
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != SpxPkgPath {
		return false
	}
	return obj.Pkg() == r.pkg
}

// spxTypeName resolves alias chains to a recognized SDK type symbol.
// Edge identifies the SDK's private special-object type.
// Defined types and same-path packages from other importers remain distinct.
func (r *spxSymbols) spxTypeName(typ gotypes.Type) string {
	seen := make(map[gotypes.Type]struct{})
	for typ != nil {
		if _, ok := seen[typ]; ok {
			return ""
		}
		seen[typ] = struct{}{}
		if name := r.types[typ]; name != "" {
			return name
		}
		alias, ok := typ.(*gotypes.Alias)
		if !ok {
			return ""
		}
		typ = alias.Rhs()
	}
	return ""
}

// spxResourceNameType returns the SDK declaration naming a resource type.
func (r *spxSymbols) spxResourceNameType(typ gotypes.Type) string {
	switch name := r.spxTypeName(typ); name {
	case "BackdropName", "SpriteName", "SpriteCostumeName", "SpriteAnimationName", "SoundName", "WidgetName":
		return name
	}
	return ""
}

// isSpxPropertyNameType reports whether typ aliases the SDK's PropertyName.
func (r *spxSymbols) isSpxPropertyNameType(typ gotypes.Type) bool {
	return r.spxTypeName(typ) == "PropertyName"
}

// functionDocumentation resolves the implementation documentation for a
// public Sprite method. Absent implementation entries leave the declaration's
// documentation in effect.
func (r *spxSymbols) functionDocumentation(fun *gotypes.Func, doc *pkgdoc.PkgDoc) (string, bool) {
	if doc == nil {
		return "", false
	}
	recvTypeName, _, _, _ := displayedFuncName(fun)
	if recvTypeName != "Sprite" || !r.isSpxSymbol(fun) {
		return "", false
	}
	if typeDoc := doc.Types["SpriteImpl"]; typeDoc != nil {
		detail, ok := typeDoc.Methods[fun.Name()]
		return detail, ok
	}
	return "", false
}

// displayTypeName maps the project's spx implementation type to its public
// name. Other symbols retain their declared type names.
func (r *spxSymbols) displayTypeName(obj gotypes.Object, typeName string) string {
	if typeName == "SpriteImpl" && r.isSpxSymbol(obj) {
		return "Sprite"
	}
	return typeName
}
