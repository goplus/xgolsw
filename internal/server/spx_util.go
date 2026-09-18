package server

import (
	gotypes "go/types"
	"regexp"

	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// spxEventHandlerFuncNameRE is the regular expression of the spx event handler
// function name.
var spxEventHandlerFuncNameRE = regexp.MustCompile(`^on[A-Z]\w*$`)

// isEventHandler reports whether fun is an SDK event registration function.
func (r *spxSymbols) isEventHandler(fun *gotypes.Func) bool {
	if !r.isSpxSymbol(fun) {
		return false
	}
	name, _ := xgoutil.ParseXGoFuncName(fun.Name())
	return spxEventHandlerFuncNameRE.MatchString(name)
}
