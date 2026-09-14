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
		PredefinedNames: collectPredefinedNames(ctx, call, declaredType),
		Range:           ctx.rangeForNode(call),
	}
}

// createSpxResourceInputSlot constructs a slot from a resolved literal reference.
// References match AST node identity so equal strings elsewhere in the source
// cannot supply the resource or its context.
func createSpxResourceInputSlot(ctx *inputSlotContext, lit *ast.BasicLit, declaredType gotypes.Type) *XGoInputSlot {
	for _, ref := range ctx.spxResult.spxResourceRefs {
		if ref.Node != lit {
			continue
		}
		return &XGoInputSlot{
			Kind:   XGoInputSlotKindValue,
			Accept: XGoInputSlotAccept{Type: XGoInputTypeSpxResourceName, ResourceContext: ToPtr(ref.ID.ContextURI())},
			Input: XGoInput{
				Kind:  XGoInputKindInPlace,
				Type:  XGoInputTypeSpxResourceName,
				Value: ref.ID.URI(),
			},
			PredefinedNames: collectPredefinedNames(ctx, lit, declaredType),
			Range:           ctx.rangeForPosEnd(lit.Pos(), basicLitEnd(ctx.proj.Fset, ctx.astFile, lit)),
		}
	}
	return nil
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
