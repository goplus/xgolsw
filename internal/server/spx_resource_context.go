package server

import (
	gotypes "go/types"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// spxSpriteResourceForObject returns the spx sprite resource for obj if it is an
// auto-bound sprite. It returns nil if obj is nil, obj has no auto-binding, or
// the corresponding sprite resource is not found in the resource set.
func spxSpriteResourceForObject(result *compileResult, obj gotypes.Object) *SpxSpriteResource {
	if obj == nil || !result.hasSpxSpriteResourceAutoBinding(obj) {
		return nil
	}
	return result.spxResourceSet.Sprite(obj.Name())
}

// spxSpriteTypeForFile resolves a work class through its registered compiler
// naming rules. The runtime identifies sprite resources by generated type name.
func spxSpriteTypeForFile(proj *xgo.Project, filename string) *gotypes.Named {
	if spxClassForFile(proj, filename) == nil {
		return nil
	}
	file, _ := proj.ASTFile(filename)
	if file == nil || file.IsProj {
		return nil
	}
	return classTypeForFile(proj, file)
}

// spxSpriteResourceForFile returns the resource of the generated sprite class.
func spxSpriteResourceForFile(result *compileResult, filename string) *SpxSpriteResource {
	named := spxSpriteTypeForFile(result.proj, filename)
	if named == nil {
		return nil
	}
	return result.spxResourceSet.Sprite(named.Obj().Name())
}

// spxSpriteResourceForCall resolves an explicit receiver through auto-binding
// object identity, or an implicit receiver through the call's physical file.
func spxSpriteResourceForCall(result *compileResult, call *ast.CallExpr) *SpxSpriteResource {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return spxSpriteResourceForFile(result, result.proj.Fset.PositionFor(call.Pos(), false).Filename)
	case *ast.SelectorExpr:
		ident, ok := fun.X.(*ast.Ident)
		if !ok {
			return nil
		}
		typeInfo, _ := result.proj.TypeInfo()
		if typeInfo == nil {
			return nil
		}
		return spxSpriteResourceForObject(result, typeInfo.ObjectOf(ident))
	default:
		return nil
	}
}

// inferSpxSpriteResourceEnclosingNode infers the enclosing [SpxSpriteResource]
// for the given node. It returns nil if no [SpxSpriteResource] can be inferred.
func inferSpxSpriteResourceEnclosingNode(result *compileResult, node ast.Node) *SpxSpriteResource {
	astFile := sourceASTFile(result.proj, node.Pos())
	if astFile == nil {
		return nil
	}
	for pathNode := range xgoutil.PathEnclosingIntervalNodes(astFile, node.Pos(), node.End(), false) {
		if call := callExprFromNode(pathNode); call != nil {
			return spxSpriteResourceForCall(result, call)
		}
	}
	return nil
}
