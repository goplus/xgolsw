package server

import (
	gotypes "go/types"

	"github.com/goplus/mod/modfile"
	"github.com/goplus/xgolsw/pkgdoc"
	"github.com/goplus/xgolsw/xgo"
)

// SpxPkgPath is the path to the spx package.
const SpxPkgPath = "github.com/goplus/spx/v3"

// isSpxClass reports whether spx supplies the registration's base classes.
func isSpxClass(class *modfile.Project) bool {
	return len(class.PkgPaths) != 0 && class.PkgPaths[0] == SpxPkgPath
}

// spxClassForFile returns the spx registration for filename, if any.
func spxClassForFile(proj *xgo.Project, filename string) *modfile.Project {
	class, ok := proj.Module().LookupClass(modfile.ClassExt(filename))
	if ok && isSpxClass(class) {
		return class
	}
	return nil
}

// spxSymbols holds the SDK declarations resolved for one request. Type keys
// preserve resource aliases even when their underlying types are all strings.
type spxSymbols struct {
	pkg   *gotypes.Package
	types map[gotypes.Type]string
}

// spxSymbols resolves the registered SDK through the project's importer. An
// unavailable SDK leaves its optional language-server adaptations inactive.
func (r *definitionContext) spxSymbols() *spxSymbols {
	if r.spx != nil {
		return r.spx
	}
	r.spx = &spxSymbols{}
	for class := range r.proj.Module().ClassProjects() {
		if !isSpxClass(class) {
			continue
		}
		pkg, err := r.proj.Importer.Import(SpxPkgPath)
		if err != nil {
			return r.spx
		}
		r.spx.pkg = pkg
		r.spx.types = make(map[gotypes.Type]string)
		for _, name := range []string{
			"Sprite", "SpriteImpl", "BackdropName", "SpriteName",
			"SpriteCostumeName", "SpriteAnimationName", "SoundName", "WidgetName",
			"Direction", "layerAction", "dirAction", "EffectKind", "Key", "Edge",
			"RotationStyle", "PropertyName", "Value", "List",
		} {
			if obj := pkg.Scope().Lookup(name); obj != nil {
				r.spx.types[obj.Type()] = name
			}
		}
		break
	}
	return r.spx
}

// isSpxSymbol reports whether obj belongs to the project's registered SDK.
func (r *definitionContext) isSpxSymbol(obj gotypes.Object) bool {
	if obj == nil || obj.Pkg() == nil || obj.Pkg().Path() != SpxPkgPath {
		return false
	}
	return obj.Pkg() == r.spxSymbols().pkg
}

// spxTypeName resolves alias chains to a recognized SDK type symbol.
// Edge identifies the SDK's private special-object type.
// Defined types and same-path packages from other importers remain distinct.
func (r *definitionContext) spxTypeName(typ gotypes.Type) string {
	seen := make(map[gotypes.Type]struct{})
	for typ != nil {
		if _, ok := seen[typ]; ok {
			return ""
		}
		seen[typ] = struct{}{}
		if name := r.spxSymbols().types[typ]; name != "" {
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
func (r *definitionContext) spxResourceNameType(typ gotypes.Type) string {
	switch name := r.spxTypeName(typ); name {
	case "BackdropName", "SpriteName", "SpriteCostumeName", "SpriteAnimationName", "SoundName", "WidgetName":
		return name
	}
	return ""
}

// isSpxPropertyNameType reports whether typ aliases the SDK's PropertyName.
func (r *definitionContext) isSpxPropertyNameType(typ gotypes.Type) bool {
	return r.spxTypeName(typ) == "PropertyName"
}

// spxFunctionDocumentation resolves the implementation documentation for a
// public Sprite method. Absent implementation entries leave the declaration's
// documentation in effect.
func (r *definitionContext) spxFunctionDocumentation(fun *gotypes.Func, doc *pkgdoc.PkgDoc) (string, bool) {
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

// spxDisplayTypeName maps the project's spx implementation type to its public
// name. Other symbols retain their declared type names.
func (r *definitionContext) spxDisplayTypeName(obj gotypes.Object, typeName string) string {
	if typeName == "SpriteImpl" && r.isSpxSymbol(obj) {
		return "Sprite"
	}
	return typeName
}
