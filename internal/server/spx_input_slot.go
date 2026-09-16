package server

import (
	"fmt"
	gotypes "go/types"
	"path"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// compileForSpxInputSlots prepares resource data only for an spx classfile.
func (s *Server) compileForSpxInputSlots(proj *xgo.Project, filename string) (*compileResult, error) {
	if path.Ext(filename) != ".spx" {
		return nil, nil
	}
	class, ok := proj.Module().LookupClass(".spx")
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
	switch fun {
	case GetSpxHSBFunc():
		return createSpxColorInputSlot(ctx, callExpr, declaredType, XGoInputTypeSpxColorConstructorHSB)
	case GetSpxHSBAFunc():
		return createSpxColorInputSlot(ctx, callExpr, declaredType, XGoInputTypeSpxColorConstructorHSBA)
	}
	return nil
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
