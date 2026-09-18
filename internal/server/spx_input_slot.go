package server

import (
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// inferInputType attempts to infer the input type from typ
// using project sprite type metadata.
func (result *spxAnalysis) inferInputType(typ gotypes.Type) SpxInputType {
	if isSpxSpriteInstanceType(result, typ) {
		return SpxInputTypeSpriteInstance
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
func (r *spxSymbols) inferSpxInputTypeFromType(typ gotypes.Type) SpxInputType {
	if _, ok := typ.(*gotypes.Basic); ok {
		return inferBasicInputType(typ)
	}

	if r.spxResourceNameType(typ) != "" {
		return SpxInputTypeResourceName
	}

	switch r.spxTypeName(typ) {
	case "Direction":
		return SpxInputTypeDirection
	case "layerAction":
		return SpxInputTypeLayerAction
	case "dirAction":
		return SpxInputTypeDirAction
	case "EffectKind":
		return SpxInputTypeEffectKind
	case "Key":
		return SpxInputTypeKey
	case "Edge":
		return SpxInputTypeSpecialObj
	case "RotationStyle":
		return SpxInputTypeRotationStyle
	case "PropertyName":
		return SpxInputTypePropertyName
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
