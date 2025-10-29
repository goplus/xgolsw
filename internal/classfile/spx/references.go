package spx

import (
	"fmt"
	"go/types"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/xgo"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// ResourceRef captures a single reference to an spx resource in source.
type ResourceRef struct {
	ID   ResourceID
	Kind ResourceRefKind
	Node ast.Node
}

// ResourceRefKind describes how an spx resource reference is expressed.
type ResourceRefKind string

const (
	ResourceRefKindStringLiteral        ResourceRefKind = "stringLiteral"
	ResourceRefKindAutoBindingReference ResourceRefKind = "autoBindingReference"
	ResourceRefKindConstantReference    ResourceRefKind = "constantReference"
)

// resourceRefCollector accumulates spx resource references and diagnostics for a project.
type resourceRefCollector struct {
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

// resourceRefKey uniquely identifies an spx resource reference entry.
type resourceRefKey struct {
	uri  ResourceURI
	kind ResourceRefKind
	pos  token.Pos
	end  token.Pos
}

// collectResourceRefs walks the project to find spx resource references and diagnostics.
func collectResourceRefs(proj *xgo.Project, set *ResourceSet, translate func(string) string) ([]*ResourceRef, []typesutil.Error) {
	typeInfo, _ := proj.TypeInfo()
	if typeInfo == nil {
		return nil, nil
	}

	rrc := &resourceRefCollector{
		proj:               proj,
		typeInfo:           typeInfo,
		translate:          translate,
		resourceSet:        set,
		spriteTypes:        make(map[types.Type]struct{}),
		spriteAutoBindings: make(map[types.Object]struct{}),
		seenRefs:           make(map[resourceRefKey]struct{}),
	}
	rrc.collectSpriteTypes()
	rrc.inspectSpriteAutoBindings()
	rrc.inspectDefinitions()
	rrc.inspectExpressions()
	return rrc.refs, rrc.diagnostics
}

// collectSpriteTypes collects spx sprite types declared in project files.
func (rrc *resourceRefCollector) collectSpriteTypes() {
	pkg := rrc.typeInfo.Pkg
	if pkg == nil {
		return
	}
	for pathName := range rrc.proj.Files() {
		if !strings.HasSuffix(pathName, ".spx") || path.Base(pathName) == "main.spx" {
			continue
		}
		spriteName := strings.TrimSuffix(path.Base(pathName), ".spx")
		if obj := pkg.Scope().Lookup(spriteName); obj != nil {
			typ := xgoutil.DerefType(obj.Type())
			if named, ok := typ.(*types.Named); ok {
				rrc.spriteTypes[named] = struct{}{}
			}
		}
	}
}

// hasSpriteType reports whether typ is a known sprite type in the project.
func (rrc *resourceRefCollector) hasSpriteType(typ types.Type) bool {
	_, ok := rrc.spriteTypes[typ]
	return ok
}

// inspectSpriteAutoBindings records spx sprite auto binding fields on the Game struct.
func (rrc *resourceRefCollector) inspectSpriteAutoBindings() {
	pkg := rrc.typeInfo.Pkg
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

	xgoutil.WalkStruct(gameType, func(member types.Object, _ *types.Named) bool {
		field, ok := member.(*types.Var)
		if !ok {
			return true
		}
		fieldType, ok := xgoutil.DerefType(field.Type()).(*types.Named)
		if !ok {
			return true
		}
		if fieldType == SpriteType() || rrc.hasSpriteType(fieldType) {
			rrc.spriteAutoBindings[member] = struct{}{}
		}
		return true
	})

	for ident, obj := range rrc.typeInfo.Uses {
		if ident == nil || !ident.Pos().IsValid() || ident.Implicit() {
			continue
		}
		if rrc.hasSpriteAutoBinding(obj) {
			rrc.addResourceRef(ResourceRef{
				ID:   SpriteResourceID{SpriteName: obj.Name()},
				Kind: ResourceRefKindAutoBindingReference,
				Node: ident,
			})
		}
	}
}

// hasSpriteAutoBinding reports whether obj is registered as a sprite auto binding.
func (rrc *resourceRefCollector) hasSpriteAutoBinding(obj types.Object) bool {
	if rrc.spriteAutoBindings == nil {
		return false
	}
	_, ok := rrc.spriteAutoBindings[obj]
	return ok
}

// inspectDefinitions examines declarations for constant or variable references.
func (rrc *resourceRefCollector) inspectDefinitions() {
	for ident, obj := range rrc.typeInfo.Defs {
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
			rrc.inspectResourceRefForType(expr, xgoutil.DerefType(obj.Type()), nil)
		}
	}
}

// inspectExpressions evaluates expressions for spx resource reference usage.
func (rrc *resourceRefCollector) inspectExpressions() {
	for expr, tv := range rrc.typeInfo.Types {
		if expr == nil || !expr.Pos().IsValid() || tv.IsType() || tv.Type == nil {
			continue
		}
		switch node := expr.(type) {
		case *ast.BasicLit:
			if node.Kind == token.STRING {
				if returnType := rrc.resolveReturnType(node); returnType != nil {
					getSpriteContext := sync.OnceValue(func() *SpriteResource {
						if rrc.resourceSet == nil {
							return nil
						}
						fileBase := path.Base(xgoutil.NodeFilename(rrc.proj.Fset, node))
						if fileBase == "main.spx" {
							return nil
						}
						spriteName := strings.TrimSuffix(fileBase, ".spx")
						return rrc.resourceSet.Sprite(spriteName)
					})
					rrc.inspectResourceRefForType(node, returnType, getSpriteContext)
				} else {
					rrc.inspectResourceRefForType(node, xgoutil.DerefType(tv.Type), nil)
				}
			}
		case *ast.Ident:
			typ := xgoutil.DerefType(tv.Type)
			switch typ {
			case BackdropNameType(),
				SpriteNameType(),
				SoundNameType(),
				WidgetNameType():
				rrc.inspectResourceRefForType(rrc.resolveAssignedExpr(node), typ, nil)
			}
		case *ast.CallExpr:
			fun := xgoutil.FuncFromCallExpr(rrc.typeInfo, node)
			if fun == nil || !HasResourceNameTypeParams(fun) {
				continue
			}
			getSpriteContext := sync.OnceValue(func() *SpriteResource {
				if rrc.resourceSet == nil {
					return nil
				}
				return rrc.resolveSpriteContext(node)
			})
			xgoutil.WalkCallExprArgs(rrc.typeInfo, node, func(fun *types.Func, params *types.Tuple, paramIndex int, arg ast.Expr, _ int) bool {
				param := params.At(paramIndex)
				paramType := xgoutil.DerefType(param.Type())
				if sliceType, ok := paramType.(*types.Slice); ok {
					paramType = sliceType.Elem()
				}
				if sliceLit, ok := arg.(*ast.SliceLit); ok {
					for _, elt := range sliceLit.Elts {
						rrc.inspectResourceRefForType(elt, paramType, getSpriteContext)
					}
				} else {
					rrc.inspectResourceRefForType(arg, paramType, getSpriteContext)
				}
				return true
			})
		}
	}
}

// inspectResourceRefForType records spx resource references for the given
// expression and type.
func (rrc *resourceRefCollector) inspectResourceRefForType(expr ast.Expr, typ types.Type, getSpriteContext func() *SpriteResource) {
	if expr == nil {
		return
	}
	exprTV, ok := rrc.typeInfo.Types[expr]
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
	case BackdropNameType():
		id := BackdropResourceID{BackdropName: name}
		rrc.handleResource(expr, id, kind, "backdrop", "", "")
	case SpriteNameType():
		id := SpriteResourceID{SpriteName: name}
		rrc.handleResource(expr, id, kind, "sprite", "", "")
	case SpriteCostumeNameType():
		if getSpriteContext == nil {
			break
		}
		if sprite := getSpriteContext(); sprite != nil {
			id := SpriteCostumeResourceID{SpriteName: sprite.Name, CostumeName: name}
			rrc.handleResource(expr, id, kind, "costume", "sprite costume", sprite.Name)
		}
	case SpriteAnimationNameType():
		if getSpriteContext == nil {
			break
		}
		if sprite := getSpriteContext(); sprite != nil {
			id := SpriteAnimationResourceID{SpriteName: sprite.Name, AnimationName: name}
			rrc.handleResource(expr, id, kind, "animation", "sprite animation", sprite.Name)
		}
	case SoundNameType():
		id := SoundResourceID{SoundName: name}
		rrc.handleResource(expr, id, kind, "sound", "", "")
	case WidgetNameType():
		id := WidgetResourceID{WidgetName: name}
		rrc.handleResource(expr, id, kind, "widget", "", "")
	}
}

// handleResource handles an spx resource reference and related diagnostics,
// optionally scoped to spriteName.
func (rrc *resourceRefCollector) handleResource(expr ast.Expr, id ResourceID, kind ResourceRefKind, resourceType, emptyLabel, spriteName string) {
	label := emptyLabel
	if label == "" {
		label = resourceType
	}
	if id.Name() == "" {
		rrc.addDiagnostic(expr, fmt.Sprintf("%s resource name cannot be empty", label))
		return
	}
	rrc.addResourceRef(ResourceRef{ID: id, Kind: kind, Node: expr})
	if !rrc.isResourceExists(id) {
		var msg string
		if spriteName != "" {
			msg = fmt.Sprintf("%s resource %q not found in sprite %q", resourceType, id.Name(), spriteName)
		} else {
			msg = fmt.Sprintf("%s resource %q not found", resourceType, id.Name())
		}
		rrc.addDiagnostic(expr, msg)
	}
}

// isResourceExists reports whether an spx resource ID resolves to an existing asset.
func (rrc *resourceRefCollector) isResourceExists(id ResourceID) bool {
	if rrc.resourceSet == nil {
		return false
	}
	switch resource := id.(type) {
	case BackdropResourceID:
		return rrc.resourceSet.Backdrop(resource.BackdropName) != nil
	case SpriteResourceID:
		return rrc.resourceSet.Sprite(resource.SpriteName) != nil
	case SpriteCostumeResourceID:
		sprite := rrc.resourceSet.Sprite(resource.SpriteName)
		return sprite != nil && sprite.Costume(resource.CostumeName) != nil
	case SpriteAnimationResourceID:
		sprite := rrc.resourceSet.Sprite(resource.SpriteName)
		return sprite != nil && sprite.Animation(resource.AnimationName) != nil
	case SoundResourceID:
		return rrc.resourceSet.Sound(resource.SoundName) != nil
	case WidgetResourceID:
		return rrc.resourceSet.Widget(resource.WidgetName) != nil
	}
	return true
}

// addResourceRef appends an spx resource reference while de-duplicating entries.
func (rrc *resourceRefCollector) addResourceRef(ref ResourceRef) {
	var pos, end token.Pos
	if ref.Node != nil {
		pos = ref.Node.Pos()
		end = ref.Node.End()
	}
	key := resourceRefKey{
		uri:  ref.ID.URI(),
		kind: ref.Kind,
		pos:  pos,
		end:  end,
	}
	if _, ok := rrc.seenRefs[key]; ok {
		return
	}
	rrc.seenRefs[key] = struct{}{}
	rrc.refs = append(rrc.refs, &ref)
}

// addDiagnostic records a translated diagnostic associated with node.
func (rrc *resourceRefCollector) addDiagnostic(node ast.Node, message string) {
	if node == nil {
		rrc.diagnostics = append(rrc.diagnostics, typesutil.Error{Fset: rrc.proj.Fset, Msg: rrc.translate(message)})
		return
	}
	rrc.diagnostics = append(rrc.diagnostics, typesutil.Error{
		Fset: rrc.proj.Fset,
		Pos:  node.Pos(),
		End:  node.End(),
		Msg:  rrc.translate(message),
	})
}

// resolveAssignedExpr resolves an identifier to its assigned expression by
// looking for assignment statements in the AST.
func (rrc *resourceRefCollector) resolveAssignedExpr(ident *ast.Ident) ast.Expr {
	astPkg, _ := rrc.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(rrc.proj.Fset, astPkg, ident)
	if astFile == nil {
		return ident
	}

	resolvedExpr := ast.Expr(ident)
	xgoutil.WalkPathEnclosingInterval(astFile, ident.Pos(), ident.End(), false, func(node ast.Node) bool {
		assign, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}

		idx := slices.IndexFunc(assign.Lhs, func(lhs ast.Expr) bool {
			return lhs == ident
		})
		if idx < 0 || idx >= len(assign.Rhs) {
			return true
		}
		resolvedExpr = assign.Rhs[idx]
		return false
	})
	return resolvedExpr
}

// resolveReturnType resolves the function return type for an expression when it
// appears in a return statement.
func (rrc *resourceRefCollector) resolveReturnType(expr ast.Expr) types.Type {
	astPkg, _ := rrc.proj.ASTPackage()
	astFile := xgoutil.NodeASTFile(rrc.proj.Fset, astPkg, expr)
	if astFile == nil {
		return nil
	}

	path, _ := xgoutil.PathEnclosingInterval(astFile, expr.Pos(), expr.End())
	stmt := xgoutil.EnclosingReturnStmt(path)
	if stmt == nil {
		return nil
	}

	idx := xgoutil.ReturnValueIndex(stmt, expr)
	if idx < 0 {
		return nil
	}

	sig := xgoutil.EnclosingFuncSignature(rrc.typeInfo, path)
	if sig == nil || idx >= sig.Results().Len() {
		return nil
	}

	typ := xgoutil.DerefType(sig.Results().At(idx).Type())
	if IsResourceNameType(typ) {
		return typ
	}
	return nil
}

// resolveSpriteContext resolves the spx sprite context from a call expression.
func (rrc *resourceRefCollector) resolveSpriteContext(callExpr *ast.CallExpr) *SpriteResource {
	if rrc.resourceSet == nil {
		return nil
	}

	funcType := rrc.typeInfo.TypeOf(callExpr.Fun)
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
	case SpriteType(), SpriteImplType():
	default:
		return nil
	}

	switch fun := callExpr.Fun.(type) {
	case *ast.Ident:
		fileBase := path.Base(xgoutil.NodeFilename(rrc.proj.Fset, callExpr))
		if fileBase == "main.spx" {
			return nil
		}
		spriteName := strings.TrimSuffix(fileBase, ".spx")
		return rrc.resourceSet.Sprite(spriteName)
	case *ast.SelectorExpr:
		ident, ok := fun.X.(*ast.Ident)
		if !ok {
			return nil
		}
		obj := rrc.typeInfo.ObjectOf(ident)
		if obj == nil || !rrc.hasSpriteAutoBinding(obj) {
			return nil
		}
		return rrc.resourceSet.Sprite(obj.Name())
	}
	return nil
}
