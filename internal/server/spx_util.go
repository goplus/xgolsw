package server

import (
	"go/types"
	"regexp"
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
