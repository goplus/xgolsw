/*
 * Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package xgo

import (
	"fmt"
	"go/types"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/goplus/mod/modfile"
	xgoast "github.com/goplus/xgo/ast"
	xgotoken "github.com/goplus/xgo/token"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// ClassfileResourceReferenceStatus is the resolution status of one classfile
// resource reference.
type ClassfileResourceReferenceStatus string

const (
	// ClassfileResourceReferenceResolved means the reference resolves to one
	// resource instance in the active resource set.
	ClassfileResourceReferenceResolved ClassfileResourceReferenceStatus = "resolved"

	// ClassfileResourceReferenceNotFound means the reference has a statically
	// known identity that is absent from the active resource set.
	ClassfileResourceReferenceNotFound ClassfileResourceReferenceStatus = "notFound"

	// ClassfileResourceReferenceEmptyName means the reference name is statically
	// known and empty.
	ClassfileResourceReferenceEmptyName ClassfileResourceReferenceStatus = "emptyName"

	// ClassfileResourceReferenceScopeUnknown means a scoped reference has no
	// statically known complete scope.
	ClassfileResourceReferenceScopeUnknown ClassfileResourceReferenceStatus = "scopeUnknown"

	// ClassfileResourceReferenceDynamic means the expression is resource-typed
	// but its value cannot be statically evaluated.
	ClassfileResourceReferenceDynamic ClassfileResourceReferenceStatus = "dynamic"
)

// ClassfileResourceReferenceSource is the syntactic source of one classfile
// resource reference.
type ClassfileResourceReferenceSource string

const (
	// ClassfileResourceReferenceStringLiteral means the reference comes from a
	// string literal.
	ClassfileResourceReferenceStringLiteral ClassfileResourceReferenceSource = "stringLiteral"

	// ClassfileResourceReferenceConstant means the reference comes from a
	// statically evaluable constant identifier.
	ClassfileResourceReferenceConstant ClassfileResourceReferenceSource = "constant"

	// ClassfileResourceReferenceHandleExpression means the reference comes from
	// a resource handle expression.
	ClassfileResourceReferenceHandleExpression ClassfileResourceReferenceSource = "handleExpression"
)

// ClassfileResourceReference is one source-level classfile resource reference.
type ClassfileResourceReference struct {
	Kind     *ClassfileResourceKind
	Name     string
	Parent   *ClassfileResource
	Resource *ClassfileResource
	Node     xgoast.Expr
	Status   ClassfileResourceReferenceStatus
	Source   ClassfileResourceReferenceSource
}

// ClassfileResourceInfo contains source-level resource references for one
// classfile framework registration.
type ClassfileResourceInfo struct {
	Set *ClassfileResourceSet

	proj       *Project
	references []*ClassfileResourceReference
	byNode     map[xgoast.Expr][]*ClassfileResourceReference
	byResource map[*ClassfileResource][]*ClassfileResourceReference
}

// References reports all source-level classfile resource references.
func (i *ClassfileResourceInfo) References() []*ClassfileResourceReference {
	if i == nil {
		return nil
	}
	return slices.Clone(i.references)
}

// ReferencesForNode reports all resource references recorded at node.
func (i *ClassfileResourceInfo) ReferencesForNode(node xgoast.Expr) []*ClassfileResourceReference {
	if i == nil || node == nil {
		return nil
	}
	return slices.Clone(i.byNode[node])
}

// ReferencesTo reports all resolved references to resource.
func (i *ClassfileResourceInfo) ReferencesTo(resource *ClassfileResource) []*ClassfileResourceReference {
	if i == nil || resource == nil {
		return nil
	}
	return slices.Clone(i.byResource[resource])
}

// ReferenceAtPosition reports the smallest resource reference at position.
func (i *ClassfileResourceInfo) ReferenceAtPosition(pos xgotoken.Position) *ClassfileResourceReference {
	if i == nil || i.proj == nil {
		return nil
	}
	var (
		best     *ClassfileResourceReference
		bestSpan int
	)
	for _, ref := range i.references {
		nodePos := i.proj.Fset.Position(ref.Node.Pos())
		nodeEnd := i.proj.Fset.Position(ref.Node.End())
		if nodePos.Filename != pos.Filename ||
			pos.Line != nodePos.Line ||
			pos.Column < nodePos.Column ||
			pos.Column > nodeEnd.Column {
			continue
		}
		nodeSpan := nodeEnd.Column - nodePos.Column
		if best == nil || nodeSpan < bestSpan {
			best = ref
			bestSpan = nodeSpan
		}
	}
	return best
}

// ClassfileResourceInfos contains cached resource reference info for all
// classfile framework registrations in one project.
type ClassfileResourceInfos struct {
	byExt map[string]*ClassfileResourceInfo
}

// InfoForExt reports the resource reference info for ext.
func (i *ClassfileResourceInfos) InfoForExt(ext string) (*ClassfileResourceInfo, bool) {
	if i == nil {
		return nil, false
	}
	ret, ok := i.byExt[ext]
	return ret, ok
}

// Exts reports all classfile extensions with resource reference info.
func (i *ClassfileResourceInfos) Exts() []string {
	if i == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(i.byExt))
}

// classfileResourceInfosCacheKind is the cache kind for project resource info.
type classfileResourceInfosCacheKind struct{}

// classfileResourceInfosCache stores project resource reference info.
type classfileResourceInfosCache struct {
	infos *ClassfileResourceInfos
}

// buildClassfileResourceInfosCache builds all classfile resource reference info
// for proj.
func buildClassfileResourceInfosCache(proj *Project) (any, error) {
	infos := &ClassfileResourceInfos{byExt: make(map[string]*ClassfileResourceInfo)}
	resources, err := proj.ClassfileResources()
	if err != nil {
		return nil, err
	}

	seen := make(map[*ClassfileResourceSet]*ClassfileResourceInfo)
	for _, ext := range resources.Exts() {
		set, ok := resources.SetForExt(ext)
		if !ok {
			continue
		}
		info := seen[set]
		if info == nil {
			info, err = buildClassfileResourceInfo(proj, set)
			if err != nil {
				return nil, err
			}
			seen[set] = info
		}
		infos.byExt[ext] = info
	}
	return &classfileResourceInfosCache{infos: infos}, nil
}

// ClassfileResourceInfos retrieves the cached resource reference info from the
// project.
func (p *Project) ClassfileResourceInfos() (*ClassfileResourceInfos, error) {
	cacheIface, err := p.Cache(classfileResourceInfosCacheKind{})
	if err != nil {
		return nil, err
	}
	cache := cacheIface.(*classfileResourceInfosCache)
	return cache.infos, nil
}

// ClassfileResourceInfo retrieves the cached resource reference info for ext.
func (p *Project) ClassfileResourceInfo(ext string) (*ClassfileResourceInfo, error) {
	infos, err := p.ClassfileResourceInfos()
	if err != nil {
		return nil, err
	}
	ret, ok := infos.InfoForExt(ext)
	if !ok {
		return nil, fs.ErrNotExist
	}
	return ret, nil
}

// buildClassfileResourceInfo builds source-level resource references for set.
func buildClassfileResourceInfo(proj *Project, set *ClassfileResourceSet) (*ClassfileResourceInfo, error) {
	typeInfo, typeErr := proj.TypeInfo()
	if typeInfo == nil {
		return nil, fmt.Errorf("failed to retrieve type info: %w", typeErr)
	}
	astPkg, astErr := proj.ASTPackage()
	if astPkg == nil {
		return nil, fmt.Errorf("failed to retrieve AST package: %w", astErr)
	}

	builder := classfileResourceInfoBuilder{
		proj:     proj,
		astPkg:   astPkg,
		typeInfo: typeInfo,
		info: &ClassfileResourceInfo{
			Set:        set,
			proj:       proj,
			byNode:     make(map[xgoast.Expr][]*ClassfileResourceReference),
			byResource: make(map[*ClassfileResource][]*ClassfileResourceReference),
		},
		seen: make(map[classfileResourceReferenceKey]struct{}),
	}
	builder.collectWorkHandleResources()
	builder.inspectDefinitions()
	builder.inspectHandleReferences()
	builder.inspectExpressions()
	return builder.info, nil
}

// classfileResourceInfoBuilder builds resource reference info.
type classfileResourceInfoBuilder struct {
	proj     *Project
	astPkg   *xgoast.Package
	typeInfo *xgotypes.Info
	info     *ClassfileResourceInfo
	seen     map[classfileResourceReferenceKey]struct{}

	handleResourcesByType map[*types.TypeName]*ClassfileResource
}

// classfileResourceReferenceKey identifies one recorded resource reference.
type classfileResourceReferenceKey struct {
	kind   *ClassfileResourceKind
	parent *ClassfileResource
	node   xgoast.Expr
	name   string
	status ClassfileResourceReferenceStatus
	source ClassfileResourceReferenceSource
}

// collectWorkHandleResources collects generated work handle types by resource
// identity.
func (b *classfileResourceInfoBuilder) collectWorkHandleResources() {
	workKinds := make(map[string]*ClassfileResourceKind)
	for _, work := range b.info.Set.Project.Works {
		obj, ok := b.info.Set.Schema.Package.Scope().Lookup(work.Class).(*types.TypeName)
		if !ok {
			continue
		}
		kind, ok := b.info.Set.Schema.HandleKindOfType(obj.Type())
		if !ok || kind.ParentName() != "" {
			continue
		}
		workKinds[work.Ext] = kind
	}
	if len(workKinds) == 0 {
		return
	}

	for filename := range b.proj.Files() {
		base := path.Base(filename)
		ext := modfile.ClassExt(base)
		if ext == "" || b.info.Set.Project.IsProj(ext, base) {
			continue
		}
		kind := workKinds[ext]
		if kind == nil {
			continue
		}
		name := strings.TrimSuffix(base, ext)
		resource, ok := b.info.Set.Resource(kind.Name, nil, name)
		if !ok {
			continue
		}
		obj, ok := b.typeInfo.Pkg.Scope().Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		if b.handleResourcesByType == nil {
			b.handleResourcesByType = make(map[*types.TypeName]*ClassfileResource)
		}
		b.handleResourcesByType[obj] = resource
	}
}

// inspectDefinitions inspects resource-typed constant and variable definitions.
func (b *classfileResourceInfoBuilder) inspectDefinitions() {
	for ident, obj := range b.typeInfo.Defs {
		if ident == nil || !b.isValidSourceNode(ident) || ident.Implicit() || obj == nil {
			continue
		}
		switch obj.(type) {
		case *types.Const, *types.Var:
			if ident.Obj == nil {
				continue
			}
			valueSpec, ok := ident.Obj.Decl.(*xgoast.ValueSpec)
			if !ok {
				continue
			}
			idx := slices.Index(valueSpec.Names, ident)
			if idx < 0 || idx >= len(valueSpec.Values) {
				continue
			}
			b.inspectExpr(valueSpec.Values[idx], xgoutil.DerefType(obj.Type()), nil)
		}
	}
}

// inspectHandleReferences inspects resource handle uses.
func (b *classfileResourceInfoBuilder) inspectHandleReferences() {
	for ident, obj := range b.typeInfo.Uses {
		if ident == nil || !b.isValidSourceNode(ident) || ident.Implicit() || obj == nil {
			continue
		}
		resource := b.handleResourceFromObject(obj)
		if resource == nil {
			continue
		}
		if ident.Name != resource.Name && obj.Name() != resource.Name {
			continue
		}
		b.addReference(&ClassfileResourceReference{
			Kind:     resource.Kind,
			Name:     resource.Name,
			Resource: resource,
			Node:     ident,
			Status:   ClassfileResourceReferenceResolved,
			Source:   ClassfileResourceReferenceHandleExpression,
		})
	}
}

// inspectExpressions inspects type-checked expressions for resource references.
func (b *classfileResourceInfoBuilder) inspectExpressions() {
	for expr, tv := range b.typeInfo.Types {
		if expr == nil || !b.isValidSourceNode(expr) || tv.IsType() || tv.Type == nil {
			continue
		}
		switch expr := expr.(type) {
		case *xgoast.BasicLit:
			if expr.Kind != xgotoken.STRING {
				continue
			}
			if returnType := b.returnResourceType(expr); returnType != nil {
				b.inspectExpr(expr, returnType, nil)
				continue
			}
			b.inspectExpr(expr, xgoutil.DerefType(tv.Type), nil)
		case *xgoast.Ident:
			if resource := b.handleReferenceResourceFromExpr(expr, tv.Type); resource != nil {
				b.addReference(&ClassfileResourceReference{
					Kind:     resource.Kind,
					Name:     resource.Name,
					Resource: resource,
					Node:     expr,
					Status:   ClassfileResourceReferenceResolved,
					Source:   ClassfileResourceReferenceHandleExpression,
				})
			}
			typ := xgoutil.DerefType(tv.Type)
			if b.kindOfType(typ) != nil {
				b.inspectExpr(expr, typ, nil)
			}
			if assigned := b.assignedExprForIdent(expr); assigned != expr {
				b.inspectExpr(assigned, typ, nil)
			}
		case *xgoast.CallExpr:
			b.inspectCallExpr(expr)
		case *xgoast.SelectorExpr:
			if resource := b.handleReferenceResourceFromExpr(expr, tv.Type); resource != nil {
				b.addReference(&ClassfileResourceReference{
					Kind:     resource.Kind,
					Name:     resource.Name,
					Resource: resource,
					Node:     expr,
					Status:   ClassfileResourceReferenceResolved,
					Source:   ClassfileResourceReferenceHandleExpression,
				})
			}
		}
	}
}

// isValidSourceNode reports whether node has a concrete source position.
func (b *classfileResourceInfoBuilder) isValidSourceNode(node xgoast.Node) bool {
	if node == nil || !node.Pos().IsValid() {
		return false
	}
	return b.proj.Fset.Position(node.Pos()).Line > 0
}

// inspectCallExpr inspects resource-typed call arguments.
func (b *classfileResourceInfoBuilder) inspectCallExpr(call *xgoast.CallExpr) {
	callInfo := b.collectCallInfo(call)
	if callInfo == nil {
		return
	}
	for _, arg := range callInfo.args {
		typ := xgoutil.DerefType(arg.param.Type())
		if slice, ok := typ.(*types.Slice); ok {
			typ = slice.Elem()
		}
		if b.kindOfType(typ) == nil {
			continue
		}

		parent := b.explicitParentForCallArg(callInfo, arg.paramIndex, make(map[int]struct{}))
		if sliceLit, ok := arg.expr.(*xgoast.SliceLit); ok {
			for _, elt := range sliceLit.Elts {
				b.inspectExpr(elt, typ, parent)
			}
			continue
		}
		b.inspectExpr(arg.expr, typ, parent)
	}
}

// inspectExpr records one resource reference for expr if typ is resource-typed.
func (b *classfileResourceInfoBuilder) inspectExpr(expr xgoast.Expr, typ types.Type, parent *ClassfileResource) *ClassfileResourceReference {
	kind := b.kindOfType(typ)
	if kind == nil {
		return nil
	}

	ref := &ClassfileResourceReference{
		Kind:   kind,
		Node:   expr,
		Status: ClassfileResourceReferenceDynamic,
	}
	name, ok := xgoutil.StringLitOrConstValue(expr, b.exprTypeAndValue(expr))
	if !ok {
		return b.addReference(ref)
	}
	ref.Source = classfileResourceReferenceSource(expr)
	ref.Name = name
	if name == "" {
		ref.Status = ClassfileResourceReferenceEmptyName
		return b.addReference(ref)
	}

	parentName := kind.ParentName()
	if parentName != "" {
		if parent == nil || parent.Kind.Name != parentName {
			parent = b.workParentResourceForNode(parentName, expr)
		}
		if parent == nil {
			ref.Status = ClassfileResourceReferenceScopeUnknown
			return b.addReference(ref)
		}
		ref.Parent = parent
	}

	resource, ok := b.info.Set.Resource(kind.Name, parent, name)
	if !ok {
		ref.Status = ClassfileResourceReferenceNotFound
		return b.addReference(ref)
	}
	ref.Resource = resource
	ref.Status = ClassfileResourceReferenceResolved
	return b.addReference(ref)
}

// addReference records ref unless an equivalent reference already exists.
func (b *classfileResourceInfoBuilder) addReference(ref *ClassfileResourceReference) *ClassfileResourceReference {
	key := classfileResourceReferenceKey{
		kind:   ref.Kind,
		parent: ref.Parent,
		node:   ref.Node,
		name:   ref.Name,
		status: ref.Status,
		source: ref.Source,
	}
	if _, ok := b.seen[key]; ok {
		return ref
	}
	b.seen[key] = struct{}{}
	b.info.references = append(b.info.references, ref)
	b.info.byNode[ref.Node] = append(b.info.byNode[ref.Node], ref)
	if ref.Resource != nil {
		b.info.byResource[ref.Resource] = append(b.info.byResource[ref.Resource], ref)
	}
	return ref
}

// kindOfType reports the resource kind represented by typ.
func (b *classfileResourceInfoBuilder) kindOfType(typ types.Type) *ClassfileResourceKind {
	kind, ok := b.info.Set.Schema.CanonicalKindOfType(xgoutil.DerefType(typ))
	if !ok {
		return nil
	}
	return kind
}

// exprTypeAndValue reports the type-and-value info for expr.
func (b *classfileResourceInfoBuilder) exprTypeAndValue(expr xgoast.Expr) types.TypeAndValue {
	return b.typeInfo.Types[expr]
}

// returnResourceType reports the resource type required by expr's return slot.
func (b *classfileResourceInfoBuilder) returnResourceType(expr xgoast.Expr) types.Type {
	astFile := xgoutil.NodeASTFile(b.proj.Fset, b.astPkg, expr)
	if astFile == nil {
		return nil
	}

	nodePath, _ := xgoutil.PathEnclosingInterval(astFile, expr.Pos(), expr.End())
	stmt := xgoutil.EnclosingReturnStmt(nodePath)
	if stmt == nil {
		return nil
	}

	idx := xgoutil.ReturnValueIndex(stmt, expr)
	if idx < 0 {
		return nil
	}

	sig := xgoutil.EnclosingFuncSignature(b.typeInfo, nodePath)
	if sig == nil || idx >= sig.Results().Len() {
		return nil
	}

	typ := xgoutil.DerefType(sig.Results().At(idx).Type())
	if b.kindOfType(typ) == nil {
		return nil
	}
	return typ
}

// assignedExprForIdent reports the right-hand expression assigned to ident.
func (b *classfileResourceInfoBuilder) assignedExprForIdent(ident *xgoast.Ident) xgoast.Expr {
	astFile := xgoutil.NodeASTFile(b.proj.Fset, b.astPkg, ident)
	if astFile == nil {
		return ident
	}

	var ret xgoast.Expr = ident
	xgoutil.WalkPathEnclosingInterval(astFile, ident.Pos(), ident.End(), false, func(node xgoast.Node) bool {
		assignStmt, ok := node.(*xgoast.AssignStmt)
		if !ok {
			return true
		}
		idx := slices.IndexFunc(assignStmt.Lhs, func(lhs xgoast.Expr) bool {
			return lhs == ident
		})
		if idx < 0 || idx >= len(assignStmt.Rhs) {
			return true
		}
		ret = assignStmt.Rhs[idx]
		return false
	})
	return ret
}

// workParentResourceForNode reports the implied parent resource for node.
func (b *classfileResourceInfoBuilder) workParentResourceForNode(kindName string, node xgoast.Node) *ClassfileResource {
	file := b.proj.Fset.Position(node.Pos()).Filename
	base := path.Base(file)
	ext := modfile.ClassExt(base)
	if ext == "" || b.info.Set.Project.IsProj(ext, base) {
		return nil
	}
	name := strings.TrimSuffix(base, ext)
	if name == "" {
		return nil
	}
	for _, work := range b.info.Set.Project.Works {
		if work.Ext != ext {
			continue
		}
		obj, ok := b.info.Set.Schema.Package.Scope().Lookup(work.Class).(*types.TypeName)
		if !ok {
			continue
		}
		workKind, ok := b.info.Set.Schema.HandleKindOfType(obj.Type())
		if !ok || workKind.Name != kindName {
			continue
		}
		resource, _ := b.info.Set.Resource(kindName, nil, name)
		return resource
	}
	return nil
}

// classfileResourceReferenceSource reports the syntactic source for expr.
func classfileResourceReferenceSource(expr xgoast.Expr) ClassfileResourceReferenceSource {
	switch expr.(type) {
	case *xgoast.Ident:
		return ClassfileResourceReferenceConstant
	case *xgoast.BasicLit:
		return ClassfileResourceReferenceStringLiteral
	default:
		return ""
	}
}

// classfileCallInfo contains resource-relevant call argument info.
type classfileCallInfo struct {
	call    *xgoast.CallExpr
	fun     *types.Func
	params  *types.Tuple
	args    []classfileCallArg
	byParam map[int][]classfileCallArg
}

// classfileCallArg is one actual argument mapped to one formal parameter.
type classfileCallArg struct {
	param      *types.Var
	paramIndex int
	expr       xgoast.Expr
}

// collectCallInfo collects normalized call argument mapping from xgoutil.
func (b *classfileResourceInfoBuilder) collectCallInfo(call *xgoast.CallExpr) *classfileCallInfo {
	var ret *classfileCallInfo
	xgoutil.WalkCallExprArgs(b.typeInfo, call, func(fun *types.Func, params *types.Tuple, paramIndex int, arg xgoast.Expr, argIndex int) bool {
		if ret == nil {
			ret = &classfileCallInfo{
				call:    call,
				fun:     fun,
				params:  params,
				byParam: make(map[int][]classfileCallArg),
			}
		}
		callArg := classfileCallArg{
			param:      params.At(paramIndex),
			paramIndex: paramIndex,
			expr:       arg,
		}
		ret.args = append(ret.args, callArg)
		ret.byParam[paramIndex] = append(ret.byParam[paramIndex], callArg)
		return true
	})
	return ret
}

// explicitParentForCallArg reports the explicit parent resource contributed by
// API-position scope bindings.
func (b *classfileResourceInfoBuilder) explicitParentForCallArg(callInfo *classfileCallInfo, targetParam int, resolving map[int]struct{}) *ClassfileResource {
	if callInfo == nil || callInfo.fun == nil {
		return nil
	}
	targetArg := callInfo.firstArgForParam(targetParam)
	if targetArg == nil {
		return nil
	}
	targetKind := b.kindOfType(targetArg.param.Type())
	if targetKind == nil || targetKind.ParentName() == "" {
		return nil
	}
	for _, binding := range b.info.Set.Schema.APIScopeBindings(callInfo.fun) {
		if binding.TargetParam != targetParam {
			continue
		}
		if binding.SourceReceiver {
			return b.resourceFromCallReceiver(callInfo, targetKind.ParentName())
		}
		return b.resourceFromCallParam(callInfo, binding.SourceParam, targetKind.ParentName(), resolving)
	}
	return nil
}

// firstArgForParam reports the first argument mapped to paramIndex.
func (c *classfileCallInfo) firstArgForParam(paramIndex int) *classfileCallArg {
	if c == nil {
		return nil
	}
	args := c.byParam[paramIndex]
	if len(args) == 0 {
		return nil
	}
	return &args[0]
}

// resourceFromCallReceiver resolves the resource identity of a call receiver.
func (b *classfileResourceInfoBuilder) resourceFromCallReceiver(callInfo *classfileCallInfo, kindName string) *ClassfileResource {
	switch fun := callInfo.call.Fun.(type) {
	case *xgoast.Ident:
		return b.workParentResourceForNode(kindName, callInfo.call)
	case *xgoast.SelectorExpr:
		return b.resourceFromExpr(fun.X, b.typeInfo.TypeOf(fun.X), nil)
	default:
		return nil
	}
}

// resourceFromCallParam resolves the resource identity of a source parameter.
func (b *classfileResourceInfoBuilder) resourceFromCallParam(callInfo *classfileCallInfo, sourceParam int, kindName string, resolving map[int]struct{}) *ClassfileResource {
	if _, ok := resolving[sourceParam]; ok {
		return nil
	}
	resolving[sourceParam] = struct{}{}
	defer delete(resolving, sourceParam)

	sourceArg := callInfo.firstArgForParam(sourceParam)
	if sourceArg == nil {
		return nil
	}
	parent := b.explicitParentForCallArg(callInfo, sourceParam, resolving)
	resource := b.resourceFromExpr(sourceArg.expr, sourceArg.param.Type(), parent)
	if resource == nil || resource.Kind.Name != kindName {
		return nil
	}
	return resource
}

// resourceFromExpr resolves expr to one exact resource identity if possible.
func (b *classfileResourceInfoBuilder) resourceFromExpr(expr xgoast.Expr, typ types.Type, parent *ClassfileResource) *ClassfileResource {
	kind := b.kindOfType(typ)
	if kind != nil {
		name, ok := xgoutil.StringLitOrConstValue(expr, b.exprTypeAndValue(expr))
		if !ok || name == "" {
			return nil
		}
		if parentName := kind.ParentName(); parentName != "" {
			if parent == nil || parent.Kind.Name != parentName {
				parent = b.workParentResourceForNode(parentName, expr)
			}
			if parent == nil {
				return nil
			}
		}
		resource, _ := b.info.Set.Resource(kind.Name, parent, name)
		return resource
	}
	return b.handleResourceFromExpr(expr, typ)
}

// handleReferenceResourceFromExpr resolves expr to a resource handle reference
// if the source name also denotes the resolved resource identity.
func (b *classfileResourceInfoBuilder) handleReferenceResourceFromExpr(expr xgoast.Expr, typ types.Type) *ClassfileResource {
	resource := b.handleResourceFromExpr(expr, typ)
	if resource == nil {
		return nil
	}
	if b.handleResourceNameFromExpr(expr) != resource.Name {
		return nil
	}
	return resource
}

// handleResourceFromExpr resolves expr to a top-level resource handle identity
// if possible.
func (b *classfileResourceInfoBuilder) handleResourceFromExpr(expr xgoast.Expr, typ types.Type) *ClassfileResource {
	if resource := b.handleResourceFromType(typ); resource != nil {
		return resource
	}
	kind, ok := b.info.Set.Schema.HandleKindOfType(xgoutil.DerefType(typ))
	if !ok || kind.ParentName() != "" {
		return nil
	}
	name := b.handleResourceNameFromExpr(expr)
	if name == "" {
		return nil
	}
	resource, _ := b.info.Set.Resource(kind.Name, nil, name)
	return resource
}

// handleResourceFromObject resolves obj to a top-level resource handle identity
// if possible.
func (b *classfileResourceInfoBuilder) handleResourceFromObject(obj types.Object) *ClassfileResource {
	if resource := b.handleResourceFromType(obj.Type()); resource != nil {
		return resource
	}
	kind, ok := b.info.Set.Schema.HandleKindOfType(xgoutil.DerefType(obj.Type()))
	if !ok || kind.ParentName() != "" {
		return nil
	}
	resource, _ := b.info.Set.Resource(kind.Name, nil, obj.Name())
	return resource
}

// handleResourceFromType resolves a generated work handle type to a resource
// identity if possible.
func (b *classfileResourceInfoBuilder) handleResourceFromType(typ types.Type) *ClassfileResource {
	named, ok := xgoutil.DerefType(typ).(*types.Named)
	if !ok {
		return nil
	}
	return b.handleResourcesByType[named.Obj()]
}

// handleResourceNameFromExpr reports the resource name implied by a handle
// expression.
func (b *classfileResourceInfoBuilder) handleResourceNameFromExpr(expr xgoast.Expr) string {
	switch expr := expr.(type) {
	case *xgoast.Ident:
		if expr.Implicit() {
			return ""
		}
		if obj := b.typeInfo.ObjectOf(expr); obj != nil {
			return obj.Name()
		}
		return expr.Name
	case *xgoast.SelectorExpr:
		if expr.Sel == nil {
			return ""
		}
		if obj := b.typeInfo.ObjectOf(expr.Sel); obj != nil {
			return obj.Name()
		}
		return expr.Sel.Name
	default:
		return ""
	}
}
