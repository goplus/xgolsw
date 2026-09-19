package server

import (
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
)

// createResourceInputSlot constructs a slot from a resolved literal reference.
// References match AST node identity so equal strings elsewhere in the source
// cannot supply the resource or its context.
func (r *resourceAnalysis) createResourceInputSlot(ctx *inputSlotContext, lit *ast.BasicLit, declaredType gotypes.Type, inputType XGoInputType) *XGoInputSlot {
	for _, ref := range r.resourceRefs {
		if resourceSourceNode(ctx.proj, ref.Node) != lit {
			continue
		}
		return &XGoInputSlot{
			Kind:   XGoInputSlotKindValue,
			Accept: XGoInputSlotAccept{Type: inputType, ResourceContext: ToPtr(ref.ID.ContextURI())},
			Input: XGoInput{
				Kind:  XGoInputKindInPlace,
				Type:  inputType,
				Value: ref.ID.URI(),
			},
			PredefinedNames: collectPredefinedNames(ctx, lit, declaredType),
			Range:           ctx.rangeForPosEnd(lit.Pos(), basicLitEnd(ctx.proj.Fset, ctx.astFile, lit)),
		}
	}
	return nil
}
