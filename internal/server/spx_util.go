package server

import (
	gotypes "go/types"
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
func IsInSpxPkg(obj gotypes.Object) bool {
	if obj == nil {
		return false
	}
	pkg := obj.Pkg()
	return pkg != nil && pkg.Path() == SpxPkgPath && pkg == GetSpxPkg()
}
