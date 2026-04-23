package server

import (
	"fmt"
	"go/types"
	"sync"

	"github.com/goplus/mod/xgomod"
	xgoast "github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/internal"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// getSpxClassfileResourceSchema loads and resolves the standardized spx
// classfile resource schema once.
var getSpxClassfileResourceSchema = sync.OnceValues(func() (*xgo.ClassfileResourceSchema, error) {
	project, ok := xgomod.Default.LookupClass(".spx")
	if !ok {
		return nil, fmt.Errorf("failed to resolve spx classfile project")
	}
	schema, err := xgo.LoadClassfileResourceSchema(project, internal.Importer)
	if err != nil {
		return nil, fmt.Errorf("failed to load spx resource schema: %w", err)
	}
	return schema, nil
})

// inspectForSpxResourceRefsFromClassfileInfo adapts standardized classfile
// resource references to the server's existing spx resource model.
func (s *Server) inspectForSpxResourceRefsFromClassfileInfo(result *compileResult, ext string) {
	info, err := result.proj.ClassfileResourceInfo(ext)
	if err != nil {
		return
	}
	for _, ref := range info.References() {
		s.inspectSpxResourceRefFromClassfileResourceRef(result, ref)
	}
}

// inspectSpxResourceRefFromClassfileResourceRef records one standardized
// resource reference as an spx resource reference when possible.
func (s *Server) inspectSpxResourceRefFromClassfileResourceRef(result *compileResult, ref *xgo.ClassfileResourceReference) {
	if ref == nil || ref.Kind == nil || ref.Node == nil {
		return
	}
	switch ref.Status {
	case xgo.ClassfileResourceReferenceResolved:
		id := spxResourceIDFromClassfileResourceReference(ref)
		kind, ok := spxResourceRefKindFromClassfileResourceReference(ref)
		if id == nil || !ok {
			return
		}
		result.addSpxResourceRef(SpxResourceRef{ID: id, Kind: kind, Node: ref.Node})
	case xgo.ClassfileResourceReferenceNotFound:
		id := spxResourceIDFromClassfileResourceReference(ref)
		kind, ok := spxResourceRefKindFromClassfileResourceReference(ref)
		if id != nil && ok {
			result.addSpxResourceRef(SpxResourceRef{ID: id, Kind: kind, Node: ref.Node})
		}
		resourceType, context := spxResourceMissingDiagnosticContextFromClassfileResourceReference(ref)
		if resourceType != "" {
			s.addSpxResourceNotFoundDiagnostic(result, ref.Node, resourceType, ref.Name, context)
		}
	case xgo.ClassfileResourceReferenceEmptyName:
		resourceType := spxResourceEmptyNameDiagnosticTypeFromClassfileResourceReference(ref)
		if resourceType != "" {
			s.addEmptySpxResourceNameDiagnostic(result, ref.Node, resourceType)
		}
	}
}

// spxResourceIDFromClassfileResourceReference converts a standardized resource
// reference identity to an spx resource ID.
func spxResourceIDFromClassfileResourceReference(ref *xgo.ClassfileResourceReference) SpxResourceID {
	switch ref.Kind.Name {
	case "backdrop":
		return SpxBackdropResourceID{BackdropName: ref.Name}
	case "sound":
		return SpxSoundResourceID{SoundName: ref.Name}
	case "sprite":
		return SpxSpriteResourceID{SpriteName: ref.Name}
	case "widget":
		return SpxWidgetResourceID{WidgetName: ref.Name}
	case "sprite.costume":
		if ref.Parent == nil {
			return nil
		}
		return SpxSpriteCostumeResourceID{SpriteName: ref.Parent.Name, CostumeName: ref.Name}
	case "sprite.animation":
		if ref.Parent == nil {
			return nil
		}
		return SpxSpriteAnimationResourceID{SpriteName: ref.Parent.Name, AnimationName: ref.Name}
	default:
		return nil
	}
}

// spxResourceRefKindFromClassfileResourceReference converts a standardized
// resource reference source to an spx reference kind.
func spxResourceRefKindFromClassfileResourceReference(ref *xgo.ClassfileResourceReference) (SpxResourceRefKind, bool) {
	switch ref.Source {
	case xgo.ClassfileResourceReferenceStringLiteral:
		return SpxResourceRefKindStringLiteral, true
	case xgo.ClassfileResourceReferenceConstant:
		return SpxResourceRefKindConstantReference, true
	case xgo.ClassfileResourceReferenceHandleExpression:
		return SpxResourceRefKindAutoBindingReference, true
	default:
		return "", false
	}
}

// spxResourceMissingDiagnosticContextFromClassfileResourceReference reports the
// diagnostic resource type and optional sprite context for a missing reference.
func spxResourceMissingDiagnosticContextFromClassfileResourceReference(ref *xgo.ClassfileResourceReference) (string, string) {
	switch ref.Kind.Name {
	case "backdrop":
		return "backdrop", ""
	case "sound":
		return "sound", ""
	case "sprite":
		return "sprite", ""
	case "widget":
		return "widget", ""
	case "sprite.costume":
		if ref.Parent == nil {
			return "", ""
		}
		return "costume", ref.Parent.Name
	case "sprite.animation":
		if ref.Parent == nil {
			return "", ""
		}
		return "animation", ref.Parent.Name
	default:
		return "", ""
	}
}

// spxResourceEmptyNameDiagnosticTypeFromClassfileResourceReference reports the
// diagnostic resource type for an empty resource name.
func spxResourceEmptyNameDiagnosticTypeFromClassfileResourceReference(ref *xgo.ClassfileResourceReference) string {
	switch ref.Kind.Name {
	case "backdrop":
		return "backdrop"
	case "sound":
		return "sound"
	case "sprite":
		return "sprite"
	case "widget":
		return "widget"
	case "sprite.costume":
		return "sprite costume"
	case "sprite.animation":
		return "sprite animation"
	default:
		return ""
	}
}

// resolveSpxSpriteResourceForNode resolves the sprite resource context for one node.
func resolveSpxSpriteResourceForNode(result *compileResult, node xgoast.Node) *SpxSpriteResource {
	sprite := resolveSpxSpriteResourceFromEnclosingCall(result, node)
	if sprite != nil {
		return sprite
	}
	return inferSpxSpriteResourceEnclosingNode(result, node)
}

// resolveSpxSpriteResourceFromCallArg resolves the sprite resource context for
// one call argument mapped to targetParam on fun.
func resolveSpxSpriteResourceFromCallArg(result *compileResult, callExpr *xgoast.CallExpr, fun *types.Func, targetParam int) *SpxSpriteResource {
	schema, err := getSpxClassfileResourceSchema()
	if err != nil {
		return inferSpxSpriteResourceEnclosingNode(result, callExpr)
	}
	for _, binding := range schema.APIScopeBindings(fun) {
		if binding.TargetParam != targetParam {
			continue
		}
		if sprite := resolveSpxSpriteResourceFromBinding(result, callExpr, binding); sprite != nil {
			return sprite
		}
		break
	}
	return inferSpxSpriteResourceEnclosingNode(result, callExpr)
}

// resolveSpxSpriteResourceFromEnclosingCall resolves the sprite resource context
// for one node by checking API-position scope bindings on its enclosing call.
func resolveSpxSpriteResourceFromEnclosingCall(result *compileResult, node xgoast.Node) *SpxSpriteResource {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	astPkg, _ := result.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(result.proj.Fset, astPkg, node)
	if astFile == nil {
		return nil
	}

	var sprite *SpxSpriteResource
	xgoutil.WalkPathEnclosingInterval(astFile, node.Pos(), node.End(), false, func(pathNode xgoast.Node) bool {
		callExpr, ok := pathNode.(*xgoast.CallExpr)
		if !ok {
			return true
		}
		xgoutil.WalkCallExprArgs(typeInfo, callExpr, func(fun *types.Func, params *types.Tuple, paramIndex int, arg xgoast.Expr, argIndex int) bool {
			if node.Pos() < arg.Pos() || node.End() > arg.End() {
				return true
			}
			sprite = resolveSpxSpriteResourceFromCallArg(result, callExpr, fun, paramIndex)
			return false
		})
		return false
	})
	return sprite
}

// resolveSpxSpriteResourceFromBinding resolves the sprite resource context for
// one binding source.
func resolveSpxSpriteResourceFromBinding(result *compileResult, callExpr *xgoast.CallExpr, binding xgo.ClassfileResourceAPIScopeBinding) *SpxSpriteResource {
	if binding.SourceReceiver {
		return resolveSpxSpriteResourceFromReceiver(result, callExpr)
	}
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	var sprite *SpxSpriteResource
	xgoutil.WalkCallExprArgs(typeInfo, callExpr, func(fun *types.Func, params *types.Tuple, paramIndex int, arg xgoast.Expr, argIndex int) bool {
		if paramIndex != binding.SourceParam {
			return true
		}
		sprite = resolveSpxSpriteResourceFromExpr(result, arg)
		return false
	})
	return sprite
}

// resolveSpxSpriteResourceFromReceiver resolves the sprite resource context from
// one call receiver.
func resolveSpxSpriteResourceFromReceiver(result *compileResult, callExpr *xgoast.CallExpr) *SpxSpriteResource {
	switch fun := callExpr.Fun.(type) {
	case *xgoast.Ident:
		return inferSpxSpriteResourceEnclosingNode(result, callExpr)
	case *xgoast.SelectorExpr:
		return resolveSpxSpriteResourceFromExpr(result, fun.X)
	default:
		return nil
	}
}

// resolveSpxSpriteResourceFromExpr resolves the sprite resource context from
// one expression.
func resolveSpxSpriteResourceFromExpr(result *compileResult, expr xgoast.Expr) *SpxSpriteResource {
	typeInfo, _ := result.proj.TypeInfo()
	if typeInfo == nil {
		return nil
	}
	typ := xgoutil.DerefType(typeInfo.TypeOf(expr))
	switch canonicalSpxResourceNameType(typ) {
	case GetSpxSpriteNameType():
		tv := typeInfo.Types[expr]
		spriteName, ok := xgoutil.StringLitOrConstValue(expr, tv)
		if !ok || spriteName == "" {
			return nil
		}
		return result.spxResourceSet.Sprite(spriteName)
	}

	switch expr := expr.(type) {
	case *xgoast.Ident:
		obj := typeInfo.ObjectOf(expr)
		if obj == nil {
			return nil
		}
		if _, ok := result.spxSpriteResourceAutoBindings[obj]; !ok {
			return nil
		}
		return result.spxResourceSet.Sprite(obj.Name())
	}
	return nil
}
