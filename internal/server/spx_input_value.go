package server

import (
	"go/constant"
	gotypes "go/types"
	"strconv"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
)

// createSpxColorInputSlot constructs a slot for a recognized HSB or HSBA call.
// Every argument must be a numeric literal so replacing the call preserves all
// of its inputs. Calls with extra arguments, kwargs, or ellipsis remain ordinary
// expressions with separately editable arguments.
func createSpxColorInputSlot(ctx *inputSlotContext, call *ast.CallExpr, declaredType gotypes.Type, constructor XGoInputTypeSpxColorConstructor) *XGoInputSlot {
	argCount := 3
	if constructor == XGoInputTypeSpxColorConstructorHSBA {
		argCount = 4
	}
	if len(call.Args) != argCount || len(call.Kwargs) != 0 || call.Ellipsis.IsValid() {
		return nil
	}
	args := make([]float64, argCount)
	for i, expr := range call.Args {
		lit, ok := expr.(*ast.BasicLit)
		if !ok {
			return nil
		}
		switch lit.Kind {
		case token.INT:
			value, err := strconv.ParseInt(lit.Value, 0, 64)
			if err != nil {
				return nil
			}
			args[i] = float64(value)
		case token.FLOAT:
			value, err := strconv.ParseFloat(lit.Value, 64)
			if err != nil {
				return nil
			}
			args[i] = value
		default:
			return nil
		}
	}
	return &XGoInputSlot{
		Kind:   XGoInputSlotKindValue,
		Accept: XGoInputSlotAccept{Type: XGoInputTypeSpxColor},
		Input: XGoInput{
			Kind:  XGoInputKindInPlace,
			Type:  XGoInputTypeSpxColor,
			Value: XGoInputSpxColorValue{Constructor: constructor, Args: args},
		},
		PredefinedNames: collectPredefinedNames(ctx, XGoInputSlotKindValue, call, declaredType),
		Range:           ctx.rangeForNode(call),
	}
}

// spxEnumInput constructs an in-place input for a recognized spx enum constant.
// Direction values are numbers. Other enum values retain the constant's name.
func spxEnumInput(cnst *gotypes.Const, inputType XGoInputType) XGoInput {
	input := XGoInput{Kind: XGoInputKindInPlace, Type: inputType, Value: cnst.Name()}
	if inputType == XGoInputTypeSpxDirection {
		input.Value, _ = constant.Float64Val(cnst.Val())
	}
	return input
}

// adaptInputSlot specializes inputs recognized by the registered SDK.
func (r *spxAnalysis) adaptInputSlot(ctx *inputSlotContext, expr ast.Expr, declaredType gotypes.Type, slot *XGoInputSlot) *XGoInputSlot {
	switch expr := expr.(type) {
	case *ast.CallExpr:
		return r.createValueInputSlotFromColorFuncCall(ctx, expr, declaredType)
	case *ast.BasicLit:
		if _, resource := r.resourceLiterals[expr]; resource || slot.Accept.Type == SpxInputTypeResourceName {
			return r.createResourceInputSlot(ctx, expr, declaredType, XGoInputTypeSpxResourceName)
		}
	case *ast.Ident:
		input, accept := slot.Input, slot.Accept
		switch input.Type {
		case SpxInputTypeDirection,
			SpxInputTypeEffectKind,
			SpxInputTypeLayerAction,
			SpxInputTypeDirAction,
			SpxInputTypeKey,
			SpxInputTypeSpecialObj,
			SpxInputTypeRotationStyle:
			if cnst, ok := ctx.typeInfo.ObjectOf(expr).(*gotypes.Const); ok && r.isSpxSymbol(cnst) {
				input = spxEnumInput(cnst, input.Type)
			}
		}
		switch accept.Type {
		case SpxInputTypeResourceName:
			id, _ := r.resolveResourceID(declaredType, "", func() *SpxSpriteResource {
				return inferSpxSpriteResourceEnclosingNode(ctx.proj, r, expr)
			})
			if id == nil {
				return nil
			}
			accept.ResourceContext = ToPtr(id.ContextURI())
		case SpxInputTypeSpriteInstance:
			accept.ResourceContext = ToPtr(SpxSpriteResourceContextURI)
			if spxSpriteResource := spxSpriteResourceForObject(r, ctx.typeInfo.ObjectOf(expr)); spxSpriteResource != nil {
				input.Kind = XGoInputKindInPlace
				input.Value = spxSpriteResource.ID.URI()
				input.Name = ""
			}
		}
		slot.Input, slot.Accept = input, accept
	}
	return slot
}
