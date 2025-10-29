package spx

import (
	"fmt"
	"go/types"
	"path"
	"strings"
	"sync"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/xgo"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"slices"
)

// ResourceRefKind describes how a resource reference is expressed.
type ResourceRefKind string

const (
	ResourceRefKindStringLiteral        ResourceRefKind = "stringLiteral"
	ResourceRefKindAutoBinding          ResourceRefKind = "autoBinding"
	ResourceRefKindAutoBindingReference ResourceRefKind = "autoBindingReference"
	ResourceRefKindConstantReference    ResourceRefKind = "constantReference"
)

// ResourceRef captures a single reference to a resource in source.
type ResourceRef struct {
	ID   ResourceID
	Kind ResourceRefKind
	Node ast.Node
}

type resourceRefKey struct {
	uri  ResourceURI
	kind ResourceRefKind
	pos  token.Pos
	end  token.Pos
}

type resourceCollector struct {
	proj        *xgo.Project
	typeInfo    *xgotypes.Info
	translate   func(string) string
	resourceSet *ResourceSet

	spriteTypes        map[types.Type]struct{}
	spriteAutoBindings map[types.Object]struct{}
	refs               []*ResourceRef
	seenRefs           map[resourceRefKey]struct{}
	diagnostics        []typesutil.Error
}

func collectResourceRefs(proj *xgo.Project, set *ResourceSet, translate func(string) string) ([]*ResourceRef, []typesutil.Error) {
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}

	rc := &resourceCollector{
		proj:        proj,
		typeInfo:    typeInfo,
		translate:   translate,
		resourceSet: set,
		spriteTypes: make(map[types.Type]struct{}),
	}
	rc.collectSpriteTypes()
	rc.inspectAutoBindings()
	rc.inspectDefinitions()
	rc.inspectExpressions()
	return rc.refs, rc.diagnostics
}

func (rc *resourceCollector) collectSpriteTypes() {
	pkg := rc.typeInfo.Pkg
	if pkg == nil {
		return
	}
	for pathName := range rc.proj.Files() {
		if !strings.HasSuffix(pathName, ".spx") {
			continue
		}
		if path.Base(pathName) == "main.spx" {
			continue
		}
		spriteName := strings.TrimSuffix(path.Base(pathName), ".spx")
		if obj := pkg.Scope().Lookup(spriteName); obj != nil {
			if named, ok := xgoutil.DerefType(obj.Type()).(*types.Named); ok {
				rc.spriteTypes[named] = struct{}{}
			}
		}
	}
}

func (rc *resourceCollector) inspectAutoBindings() {
	pkg := rc.typeInfo.Pkg
	if pkg == nil {
		return
	}
	gameObj := pkg.Scope().Lookup("Game")
	if gameObj == nil {
		return
	}
	gameType, ok := gameObj.Type().(*types.Named)
	if !ok || !xgoutil.IsNamedStructType(gameType) {
		return
	}
	if rc.spriteAutoBindings == nil {
		rc.spriteAutoBindings = make(map[types.Object]struct{})
	}
	xgoutil.WalkStruct(gameType, func(member types.Object, _ *types.Named) bool {
		field, ok := member.(*types.Var)
		if !ok {
			return true
		}
		fieldType, ok := xgoutil.DerefType(field.Type()).(*types.Named)
		if !ok {
			return true
		}
		if fieldType == GetSpxSpriteType() || rc.hasSpriteType(fieldType) {
			rc.spriteAutoBindings[member] = struct{}{}
			if def := rc.typeInfo.ObjToDef[member]; def != nil {
				rc.addResourceRef(ResourceRef{
					ID:   SpriteResourceID{SpriteName: member.Name()},
					Kind: ResourceRefKindAutoBinding,
					Node: def,
				})
			}
		}
		return true
	})

	for ident, obj := range rc.typeInfo.Uses {
		if ident == nil || !ident.Pos().IsValid() || ident.Implicit() {
			continue
		}
		if rc.hasSpriteAutoBinding(obj) {
			rc.addResourceRef(ResourceRef{
				ID:   SpriteResourceID{SpriteName: obj.Name()},
				Kind: ResourceRefKindAutoBindingReference,
				Node: ident,
			})
		}
	}
}

func (rc *resourceCollector) inspectDefinitions() {
	for ident, obj := range rc.typeInfo.Defs {
		if ident == nil || !ident.Pos().IsValid() || ident.Implicit() || obj == nil {
			continue
		}
		switch obj.(type) {
		case *types.Const, *types.Var:
			if ident.Obj == nil {
				break
			}
			valueSpec, ok := ident.Obj.Decl.(*ast.ValueSpec)
			if !ok {
				break
			}
			idx := slices.Index(valueSpec.Names, ident)
			if idx < 0 || idx >= len(valueSpec.Values) {
				break
			}
			expr := valueSpec.Values[idx]
			rc.inspectResourceRefForType(expr, xgoutil.DerefType(obj.Type()), nil)
		}
	}
}

func (rc *resourceCollector) inspectExpressions() {
	for expr, tv := range rc.typeInfo.Types {
		if expr == nil || !expr.Pos().IsValid() || tv.IsType() || tv.Type == nil {
			continue
		}
		switch node := expr.(type) {
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				if returnType := rc.resolveReturnType(node); returnType != nil {
					getContext := sync.OnceValue(func() *SpriteResource {
						if rc.resourceSet == nil {
							return nil
						}
						fileBase := path.Base(xgoutil.NodeFilename(rc.proj.Fset, node))
						if fileBase == "main.spx" {
							return nil
						}
						spriteName := strings.TrimSuffix(fileBase, ".spx")
						return rc.resourceSet.Sprite(spriteName)
					})
					rc.inspectResourceRefForType(node, returnType, getContext)
				} else {
					rc.inspectResourceRefForType(node, xgoutil.DerefType(tv.Type), nil)
				}
			}
		case *ast.Ident:
			typ := xgoutil.DerefType(tv.Type)
			switch typ {
			case GetSpxBackdropNameType(),
				GetSpxSpriteNameType(),
				GetSpxSoundNameType(),
				GetSpxWidgetNameType():
				rc.inspectResourceRefForType(rc.resolveAssignedExpr(node), typ, nil)
			}
		case *ast.CallExpr:
			fun := xgoutil.FuncFromCallExpr(rc.typeInfo, node)
			if fun == nil || !HasSpxResourceNameTypeParams(fun) {
				continue
			}
			getContext := sync.OnceValue(func() *SpriteResource {
				if rc.resourceSet == nil {
					return nil
				}
				return rc.resolveSpriteContext(node)
			})
			xgoutil.WalkCallExprArgs(rc.typeInfo, node, func(fun *types.Func, params *types.Tuple, paramIndex int, arg ast.Expr, _ int) bool {
				param := params.At(paramIndex)
				paramType := xgoutil.DerefType(param.Type())
				if sliceType, ok := paramType.(*types.Slice); ok {
					paramType = sliceType.Elem()
				}
				if sliceLit, ok := arg.(*ast.SliceLit); ok {
					for _, elt := range sliceLit.Elts {
						rc.inspectResourceRefForType(elt, paramType, getContext)
					}
				} else {
					rc.inspectResourceRefForType(arg, paramType, getContext)
				}
				return true
			})
		}
	}
}

func (rc *resourceCollector) inspectResourceRefForType(expr ast.Expr, typ types.Type, getSpriteContext func() *SpriteResource) {
	if expr == nil {
		return
	}
	exprTV, ok := rc.typeInfo.Types[expr]
	if !ok {
		return
	}
	name, ok := xgoutil.StringLitOrConstValue(expr, exprTV)
	if !ok {
		return
	}
	kind := ResourceRefKindStringLiteral
	if _, ok := expr.(*ast.Ident); ok {
		kind = ResourceRefKindConstantReference
	}

	switch typ {
	case GetSpxBackdropNameType():
		rc.handleSimpleResource(expr, BackdropResourceID{BackdropName: name}, kind, "backdrop", "")
	case GetSpxSpriteNameType():
		rc.handleSimpleResource(expr, SpriteResourceID{SpriteName: name}, kind, "sprite", "")
	case GetSpxSoundNameType():
		rc.handleSimpleResource(expr, SoundResourceID{SoundName: name}, kind, "sound", "")
	case GetSpxWidgetNameType():
		rc.handleSimpleResource(expr, WidgetResourceID{WidgetName: name}, kind, "widget", "")
	case GetSpxSpriteCostumeNameType():
		if sprite := rc.lazySprite(getSpriteContext); sprite != nil {
			rc.handleSpriteScopedResource(expr, SpriteCostumeResourceID{SpriteName: sprite.Name, CostumeName: name}, kind, "sprite costume", "costume", sprite.Name)
		}
	case GetSpxSpriteAnimationNameType():
		if sprite := rc.lazySprite(getSpriteContext); sprite != nil {
			rc.handleSpriteScopedResource(expr, SpriteAnimationResourceID{SpriteName: sprite.Name, AnimationName: name}, kind, "sprite animation", "animation", sprite.Name)
		}
	}
}

func (rc *resourceCollector) handleSimpleResource(expr ast.Expr, id ResourceID, kind ResourceRefKind, resourceType string, spriteName string) {
	if id.Name() == "" {
		rc.addDiagnostic(expr, fmt.Sprintf("%s resource name cannot be empty", resourceType))
		return
	}
	rc.addResourceRef(ResourceRef{ID: id, Kind: kind, Node: expr})
	if !rc.resourceExists(id, spriteName) {
		rc.addDiagnostic(expr, rc.missingMessage(resourceType, id.Name(), spriteName))
	}
}

func (rc *resourceCollector) handleSpriteScopedResource(expr ast.Expr, id ResourceID, kind ResourceRefKind, emptyMsg, resourceType, spriteName string) {
	if id.Name() == "" {
		rc.addDiagnostic(expr, fmt.Sprintf("%s resource name cannot be empty", emptyMsg))
		return
	}
	rc.addResourceRef(ResourceRef{ID: id, Kind: kind, Node: expr})
	if !rc.resourceExists(id, spriteName) {
		rc.addDiagnostic(expr, rc.missingMessage(resourceType, id.Name(), spriteName))
	}
}

func (rc *resourceCollector) resourceExists(id ResourceID, spriteName string) bool {
	switch specific := id.(type) {
	case BackdropResourceID:
		return rc.resourceSet != nil && rc.resourceSet.Backdrop(specific.BackdropName) != nil
	case SpriteResourceID:
		return rc.resourceSet != nil && rc.resourceSet.Sprite(specific.SpriteName) != nil
	case SpriteCostumeResourceID:
		if rc.resourceSet == nil {
			return false
		}
		sprite := rc.resourceSet.Sprite(specific.SpriteName)
		return sprite != nil && sprite.Costume(specific.CostumeName) != nil
	case SpriteAnimationResourceID:
		if rc.resourceSet == nil {
			return false
		}
		sprite := rc.resourceSet.Sprite(specific.SpriteName)
		return sprite != nil && sprite.Animation(specific.AnimationName) != nil
	case SoundResourceID:
		return rc.resourceSet != nil && rc.resourceSet.Sound(specific.SoundName) != nil
	case WidgetResourceID:
		return rc.resourceSet != nil && rc.resourceSet.Widget(specific.WidgetName) != nil
	}
	return true
}

func (rc *resourceCollector) lazySprite(get func() *SpriteResource) *SpriteResource {
	if get == nil {
		return nil
	}
	return get()
}

func (rc *resourceCollector) missingMessage(resourceType, name, spriteName string) string {
	if spriteName != "" {
		return fmt.Sprintf("%s resource %q in sprite %q not found", resourceType, name, spriteName)
	}
	return fmt.Sprintf("%s resource %q not found", resourceType, name)
}

func (rc *resourceCollector) addResourceRef(ref ResourceRef) {
	var pos, end token.Pos
	if ref.Node != nil {
		pos = ref.Node.Pos()
		end = ref.Node.End()
	}
	key := resourceRefKey{uri: ref.ID.URI(), kind: ref.Kind, pos: pos, end: end}
	if rc.seenRefs == nil {
		rc.seenRefs = make(map[resourceRefKey]struct{})
	}
	if _, exists := rc.seenRefs[key]; exists {
		return
	}
	rc.seenRefs[key] = struct{}{}
	rc.refs = append(rc.refs, &ref)
}

func (rc *resourceCollector) addDiagnostic(node ast.Node, message string) {
	if node == nil {
		rc.diagnostics = append(rc.diagnostics, typesutil.Error{Fset: rc.proj.Fset, Msg: rc.translate(message)})
		return
	}
	rc.diagnostics = append(rc.diagnostics, typesutil.Error{
		Fset: rc.proj.Fset,
		Pos:  node.Pos(),
		End:  node.End(),
		Msg:  rc.translate(message),
	})
}

func (rc *resourceCollector) hasSpriteType(typ types.Type) bool {
	_, ok := rc.spriteTypes[typ]
	return ok
}

func (rc *resourceCollector) hasSpriteAutoBinding(obj types.Object) bool {
	if rc.spriteAutoBindings == nil {
		return false
	}
	_, ok := rc.spriteAutoBindings[obj]
	return ok
}

func (rc *resourceCollector) resolveAssignedExpr(ident *ast.Ident) ast.Expr {
	astPkg, _ := rc.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(rc.proj.Fset, astPkg, ident)
	if astFile == nil {
		return ident
	}
	resolved := ast.Expr(ident)
	xgoutil.WalkPathEnclosingInterval(astFile, ident.Pos(), ident.End(), false, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		idx := slices.IndexFunc(assign.Lhs, func(lhs ast.Expr) bool { return lhs == ident })
		if idx < 0 || idx >= len(assign.Rhs) {
			return true
		}
		resolved = assign.Rhs[idx]
		return false
	})
	return resolved
}

func (rc *resourceCollector) resolveReturnType(expr ast.Expr) types.Type {
	astPkg, _ := rc.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(rc.proj.Fset, astPkg, expr)
	if astFile == nil {
		return nil
	}
	pathNodes, _ := xgoutil.PathEnclosingInterval(astFile, expr.Pos(), expr.End())
	stmt := xgoutil.EnclosingReturnStmt(pathNodes)
	if stmt == nil {
		return nil
	}
	idx := xgoutil.ReturnValueIndex(stmt, expr)
	if idx < 0 {
		return nil
	}
	sig := xgoutil.EnclosingFuncSignature(rc.typeInfo, pathNodes)
	if sig == nil || idx >= sig.Results().Len() {
		return nil
	}
	typ := xgoutil.DerefType(sig.Results().At(idx).Type())
	if IsSpxResourceNameType(typ) {
		return typ
	}
	return nil
}

func (rc *resourceCollector) resolveSpriteContext(call *ast.CallExpr) *SpriteResource {
	if rc.resourceSet == nil {
		return nil
	}
	funcType := rc.typeInfo.TypeOf(call.Fun)
	if !xgoutil.IsValidType(funcType) {
		return nil
	}
	sig, ok := funcType.(*types.Signature)
	if !ok {
		return nil
	}
	recv := sig.Recv()
	if recv == nil {
		return nil
	}
	switch xgoutil.DerefType(recv.Type()) {
	case GetSpxSpriteType(), GetSpxSpriteImplType():
	default:
		return nil
	}
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		fileBase := path.Base(xgoutil.NodeFilename(rc.proj.Fset, call))
		if fileBase == "main.spx" {
			return nil
		}
		spriteName := strings.TrimSuffix(fileBase, ".spx")
		return rc.resourceSet.Sprite(spriteName)
	case *ast.SelectorExpr:
		ident, ok := fun.X.(*ast.Ident)
		if !ok {
			return nil
		}
		obj := rc.typeInfo.ObjectOf(ident)
		if obj == nil || !rc.hasSpriteAutoBinding(obj) {
			return nil
		}
		return rc.resourceSet.Sprite(obj.Name())
	}
	return nil
}
