package server

import (
	"fmt"
	gotypes "go/types"
	"path"
	"slices"
	"strconv"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// compileForSpxInputSlots prepares resource data only for an spx classfile.
func (s *Server) compileForSpxInputSlots(proj *xgo.Project, filename string) (*compileResult, error) {
	if path.Ext(filename) != ".spx" {
		return nil, nil
	}
	class, ok := proj.Mod.LookupClass(".spx")
	if !ok || !slices.Contains(class.PkgPaths, SpxPkgPath) {
		return nil, nil
	}
	result, err := s.compileAt(proj)
	if err != nil {
		return nil, fmt.Errorf("failed to compile: %w", err)
	}
	return result, nil
}

// inferSpxInputTypeFromTypeInProject attempts to infer the input type from typ
// using project sprite type metadata.
func inferSpxInputTypeFromTypeInProject(result *compileResult, typ gotypes.Type) SpxInputType {
	if isSpxSpriteInstanceType(result, typ) {
		return SpxInputTypeSpriteInstance
	}
	return inferSpxInputTypeFromType(typ)
}

// isSpxSpriteInstanceType reports whether the given type represents an spx
// sprite instance.
func isSpxSpriteInstanceType(result *compileResult, typ gotypes.Type) bool {
	if typ == nil {
		return false
	}
	typ = xgoutil.DerefType(typ)
	if typ == GetSpxSpriteType() {
		return true
	}
	if result != nil && result.hasSpxSpriteType(typ) {
		return true
	}
	return gotypes.AssignableTo(typ, GetSpxSpriteType())
}

// createValueInputSlotFromColorFuncCall creates a value input slot from an spx
// color function call.
func createValueInputSlotFromColorFuncCall(ctx *inputSlotContext, callExpr *ast.CallExpr, declaredType gotypes.Type) *XGoInputSlot {
	if ctx.spxResult == nil || ctx.typeInfo == nil {
		return nil
	}

	fun := xgoutil.FuncFromCallExpr(ctx.typeInfo, callExpr)
	if fun == nil || !IsInSpxPkg(fun) || !isSpxColorFunc(fun) {
		return nil
	}

	constructor := SpxInputTypeSpxColorConstructor(fun.Name())
	maxArgs := 3
	switch constructor {
	case SpxInputTypeSpxColorConstructorHSB:
	case SpxInputTypeSpxColorConstructorHSBA:
		maxArgs = 4
	default:
		return nil // This should never happen, but just in case.
	}

	var args []float64
	for i, argExpr := range callExpr.Args {
		if i >= maxArgs {
			break
		}
		lit, ok := argExpr.(*ast.BasicLit)
		if !ok {
			return nil
		}

		var val float64
		switch lit.Kind {
		case token.FLOAT:
			floatVal, err := strconv.ParseFloat(lit.Value, 64)
			if err != nil {
				return nil
			}
			val = floatVal
		case token.INT:
			intVal, err := strconv.ParseInt(lit.Value, 0, 64)
			if err != nil {
				return nil
			}
			val = float64(intVal)
		default:
			return nil
		}
		args = append(args, val)
	}
	if len(args) < maxArgs {
		return nil
	}

	return &XGoInputSlot{
		Kind:   XGoInputSlotKindValue,
		Accept: XGoInputSlotAccept{Type: SpxInputTypeColor},
		Input: XGoInput{
			Kind: XGoInputKindInPlace,
			Type: SpxInputTypeColor,
			Value: SpxColorInputValue{
				Constructor: constructor,
				Args:        args,
			},
		},
		PredefinedNames: collectPredefinedNames(ctx, callExpr, declaredType),
		Range:           ctx.rangeForNode(callExpr),
	}
}

// isSpxColorFunc checks if the fun is an spx color function.
func isSpxColorFunc(fun *gotypes.Func) bool {
	switch fun {
	case GetSpxHSBFunc(), GetSpxHSBAFunc():
		return true
	}
	return false
}

// inferSpxInputTypeFromType attempts to infer the input type from the given type.
func inferSpxInputTypeFromType(typ gotypes.Type) SpxInputType {
	if _, ok := typ.(*gotypes.Basic); ok {
		return inferBasicInputType(typ)
	}

	if IsSpxResourceNameType(typ) {
		return SpxInputTypeResourceName
	}

	switch typ {
	case GetSpxDirectionType():
		return SpxInputTypeDirection
	case GetSpxLayerActionType():
		return SpxInputTypeLayerAction
	case GetSpxDirActionType():
		return SpxInputTypeDirAction
	case GetSpxEffectKindType():
		return SpxInputTypeEffectKind
	case GetSpxKeyType():
		return SpxInputTypeKey
	case GetSpxSpecialObjType():
		return SpxInputTypeSpecialObj
	case GetSpxRotationStyleType():
		return SpxInputTypeRotationStyle
	case GetSpxPropertyNameType():
		return SpxInputTypePropertyName
	}

	// Fall back to the alias RHS when no direct basic or spx type match is found.
	if alias, ok := typ.(*gotypes.Alias); ok {
		rhs := alias.Rhs()
		if rhs != nil && rhs != typ {
			return inferSpxInputTypeFromType(rhs)
		}
	}
	return XGoInputTypeUnknown
}
