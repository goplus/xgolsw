package server

import (
	"go/types"

	"github.com/goplus/gogen"
)

// optionalParamVarKind is the custom kind used by gogen for optional parameters.
const optionalParamVarKind = types.VarKind(gogen.ParamOptionalVar)

// varKind returns the semantic kind of v.
func varKind(v *types.Var) types.VarKind {
	if v == nil {
		return 0
	}
	return v.Kind()
}

// isParameterLikeVarKind reports whether kind should be treated like a parameter.
func isParameterLikeVarKind(kind types.VarKind) bool {
	switch kind {
	case types.ParamVar, types.RecvVar, optionalParamVarKind:
		return true
	default:
		return false
	}
}

// varOverviewPrefix returns the overview prefix used for variable definitions.
func varOverviewPrefix(v *types.Var, forceVar bool) string {
	if forceVar {
		return "var"
	}

	switch varKind(v) {
	case types.FieldVar:
		return "field"
	case types.RecvVar:
		return "recv"
	case types.ParamVar, optionalParamVarKind:
		return "param"
	case types.ResultVar:
		return "result"
	default:
		return "var"
	}
}
