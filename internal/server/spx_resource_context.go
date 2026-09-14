package server

import (
	gotypes "go/types"
	"path"
	"strings"

	"github.com/goplus/xgo/ast"
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

// spxSpriteResourceForFile returns the sprite resource represented by filename.
// The project entry file does not represent a sprite.
func spxSpriteResourceForFile(result *compileResult, filename string) *SpxSpriteResource {
	if filename == "" || path.Base(filename) == path.Base(result.mainSpxFile) {
		return nil
	}
	return result.spxResourceSet.Sprite(strings.TrimSuffix(path.Base(filename), ".spx"))
}

// spxSpriteResourceForCall resolves an explicit receiver through auto-binding
// object identity, or an implicit receiver through the call's physical file.
func spxSpriteResourceForCall(result *compileResult, call *ast.CallExpr) *SpxSpriteResource {
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		return spxSpriteResourceForFile(result, result.proj.Fset.File(call.Pos()).Name())
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
