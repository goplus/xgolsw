package server

import (
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// inferInputType attempts to infer the input type from typ
// using project sprite type metadata.
func (result *spxAnalysis) inferInputType(typ gotypes.Type) XGoInputType {
	if isSpxSpriteInstanceType(result, typ) {
		return XGoInputTypeSpxSpriteInstance
	}
	return result.inferSpxInputTypeFromType(typ)
}

// isSpxSpriteInstanceType reports whether the given type represents an spx
// sprite instance.
func isSpxSpriteInstanceType(result *spxAnalysis, typ gotypes.Type) bool {
	if typ == nil {
		return false
	}
	if result.hasSpxSpriteType(gotypes.Unalias(xgoutil.DerefType(gotypes.Unalias(typ)))) {
		return true
	}
	sdk := result.spxSymbols
	if sdk.pkg == nil {
		return false
	}
	sprite := sdk.pkg.Scope().Lookup("Sprite")
	return sprite != nil && gotypes.AssignableTo(typ, sprite.Type())
}

// createValueInputSlotFromColorFuncCall creates a value input slot from an spx
// color function call.
func (r *spxAnalysis) createValueInputSlotFromColorFuncCall(ctx *inputSlotContext, callExpr *ast.CallExpr, declaredType gotypes.Type) *XGoInputSlot {
	if ctx.typeInfo == nil {
		return nil
	}

	fun := xgoutil.FuncFromCallExpr(ctx.typeInfo, callExpr)
	if fun == nil || !r.isSpxSymbol(fun) || fun.Signature().Recv() != nil {
		return nil
	}
	switch fun.Name() {
	case "HSB":
		return createSpxColorInputSlot(ctx, callExpr, declaredType, XGoInputTypeSpxColorConstructorHSB)
	case "HSBA":
		return createSpxColorInputSlot(ctx, callExpr, declaredType, XGoInputTypeSpxColorConstructorHSBA)
	}
	return nil
}

// inferSpxInputTypeFromType attempts to infer the input type from the given type.
func (r *spxSymbols) inferSpxInputTypeFromType(typ gotypes.Type) XGoInputType {
	if _, ok := typ.(*gotypes.Basic); ok {
		return inferBasicInputType(typ)
	}

	if r.spxResourceNameType(typ) != "" {
		return XGoInputTypeResourceName
	}

	switch r.spxTypeName(typ) {
	case "Direction":
		return XGoInputTypeSpxDirection
	case "layerAction":
		return XGoInputTypeSpxLayerAction
	case "dirAction":
		return XGoInputTypeSpxDirAction
	case "EffectKind":
		return XGoInputTypeSpxEffectKind
	case "Key":
		return XGoInputTypeSpxKey
	case "Edge":
		return XGoInputTypeSpxSpecialObj
	case "RotationStyle":
		return XGoInputTypeSpxRotationStyle
	case "PropertyName":
		return XGoInputTypeSpxPropertyName
	}

	// Fall back to the alias RHS when no direct basic or spx type match is found.
	if alias, ok := typ.(*gotypes.Alias); ok {
		rhs := alias.Rhs()
		if rhs != nil && rhs != typ {
			return r.inferSpxInputTypeFromType(rhs)
		}
	}
	return XGoInputTypeUnknown
}
