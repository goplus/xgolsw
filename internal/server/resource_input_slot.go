package server

import (
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
)

// createResourceInputSlot constructs a slot from a literal's resolved context.
// AST node identity keeps equal strings elsewhere from supplying its context.
func (r *resourceAnalysis) createResourceInputSlot(ctx *inputSlotContext, lit *ast.BasicLit, declaredType gotypes.Type) *XGoInputSlot {
	resolved := r.expressions[lit]
	id := resolved.id
	if id == nil || !resolved.value.Static {
		return nil
	}
	input := XGoInput{Kind: XGoInputKindInPlace, Type: XGoInputTypeResourceName}
	if validResourceName(id.Name()) {
		input.Value = id.URI()
	} else {
		// Preserve editable placeholders without inventing a URI for an invalid name.
		input.Type = XGoInputTypeString
		input.Value = id.Name()
	}
	return &XGoInputSlot{
		Kind:            XGoInputSlotKindValue,
		Accept:          XGoInputSlotAccept{Type: XGoInputTypeResourceName, ResourceContext: ToPtr(id.ContextURI())},
		Input:           input,
		PredefinedNames: collectPredefinedNames(ctx, XGoInputSlotKindValue, lit, declaredType),
		Range:           ctx.rangeForPosEnd(lit.Pos(), basicLitEnd(ctx.proj.Fset, ctx.astFile, lit)),
	}
}
