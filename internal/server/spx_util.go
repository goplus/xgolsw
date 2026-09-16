package server

import (
	gotypes "go/types"
	"regexp"

	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// spxEventHandlerFuncNameRE is the regular expression of the spx event handler
// function name.
var spxEventHandlerFuncNameRE = regexp.MustCompile(`^on[A-Z]\w*$`)

// isSpxEventHandler reports whether obj is an SDK event registration function.
func (r *definitionContext) isSpxEventHandler(obj gotypes.Object) bool {
	fun, ok := obj.(*gotypes.Func)
	if !ok || fun == nil || !r.isSpxSymbol(fun) {
		return false
	}
	name, _ := xgoutil.ParseXGoFuncName(fun.Name())
	return spxEventHandlerFuncNameRE.MatchString(name)
}
