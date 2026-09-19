package server

import (
	gotypes "go/types"
	"iter"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/cl"
	"github.com/goplus/xgolsw/internal/analysis/ast/astutil"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// resolvedNamedType resolves aliases and pointer indirections until it reaches
// a named type. It returns nil if typ does not resolve to a named type.
func resolvedNamedType(typ gotypes.Type) *gotypes.Named {
	seen := make(map[gotypes.Type]struct{})
	for typ != nil {
		if _, ok := seen[typ]; ok {
			return nil
		}
		seen[typ] = struct{}{}

		typ = gotypes.Unalias(typ)
		switch t := typ.(type) {
		case *gotypes.Named:
			return t
		case *gotypes.Pointer:
			typ = t.Elem()
		default:
			return nil
		}
	}
	return nil
}

// memberForIdent resolves the selected member without discarding its declaration
// or selector package.
func (r *definitionContext) memberForIdent(ident *ast.Ident, obj gotypes.Object) *xgoutil.StructMember {
	switch obj := obj.(type) {
	case *gotypes.Var:
		if !obj.IsField() {
			return nil
		}
	case *gotypes.Func:
		if obj.Signature().Recv() == nil {
			return nil
		}
	default:
		return nil
	}
	proj := r.proj
	info, _ := proj.TypeInfo()
	astPkg, _ := proj.ASTPackage()
	file := xgoutil.NodeASTFile(proj.Fset, astPkg, ident)
	if file == nil {
		return nil
	}
	var fieldKey bool
enclosing:
	for node := range xgoutil.PathEnclosingIntervalNodes(file, ident.Pos(), ident.End(), false) {
		switch node := node.(type) {
		case *ast.SelectorExpr:
			if node.Sel == ident {
				return r.memberForObject(info.TypeOf(node.X), obj)
			}
		case *ast.KeyValueExpr:
			fieldKey = node.Key == ident
		case *ast.CompositeLit:
			if typ := info.TypeOf(node); fieldKey && typ != nil {
				if _, ok := typ.Underlying().(*gotypes.Struct); ok {
					return r.memberForObject(typ, obj)
				}
			}
			break enclosing
		}
	}
	if class := classTypeForFile(proj, file); class != nil {
		return r.memberForObject(class, obj)
	}
	return nil
}

// memberForObject locates obj in the receiver's accessible members, including
// instantiated fields and methods selected from an overload declaration.
func (r *definitionContext) memberForObject(receiver gotypes.Type, obj gotypes.Object) *xgoutil.StructMember {
	receiver = gotypes.Unalias(xgoutil.DerefType(gotypes.Unalias(receiver)))
	if receiver == nil {
		return nil
	}
	if selector, ok := r.memberSelectorsFor(receiver)[memberOrigin(obj)]; ok {
		return &xgoutil.StructMember{Member: obj, Selector: selector}
	}
	return nil
}

// memberSelectorsFor indexes accessible member origins in lookup order so a
// batch of selections through the same receiver needs only one traversal.
func (r *definitionContext) memberSelectorsFor(receiver gotypes.Type) map[gotypes.Object]*gotypes.Named {
	if selectors, ok := r.memberSelectors[receiver]; ok {
		return selectors
	}
	selectors := make(map[gotypes.Object]*gotypes.Named)
	add := func(obj gotypes.Object, selector *gotypes.Named) {
		origin := memberOrigin(obj)
		if _, ok := selectors[origin]; !ok {
			selectors[origin] = selector
		}
	}
	members := xgoutil.StructMembers(receiver, r.isClassBaseType)
	_, isInterface := receiver.Underlying().(*gotypes.Interface)
	if isInterface {
		members = importedInterfaceMembers(receiver)
	}
	for member := range members {
		add(member.Member, member.Selector)
		if method, ok := member.Member.(*gotypes.Func); ok && !isInterface {
			for _, overload := range xgoutil.ExpandXGoOverloadableFunc(method) {
				add(overload, member.Selector)
			}
		}
	}
	if r.memberSelectors == nil {
		r.memberSelectors = make(map[gotypes.Type]map[gotypes.Object]*gotypes.Named)
	}
	r.memberSelectors[receiver] = selectors
	return selectors
}

// importedInterfaceMembers yields methods with their public imported selectors.
// Locally declared interfaces retain their source declarations, while imported
// interfaces can expose methods declared in another package.
func importedInterfaceMembers(receiver gotypes.Type) iter.Seq[xgoutil.StructMember] {
	return func(yield func(xgoutil.StructMember) bool) {
		seen := make(map[gotypes.Type]struct{})
		var walk func(gotypes.Type) bool
		walk = func(typ gotypes.Type) bool {
			typ = gotypes.Unalias(typ)
			if _, ok := seen[typ]; ok {
				return true
			}
			seen[typ] = struct{}{}
			iface, ok := typ.Underlying().(*gotypes.Interface)
			if !ok {
				return true
			}
			if named, ok := typ.(*gotypes.Named); ok && named.Obj().Exported() && !xgoutil.IsInMainPkg(named.Obj()) {
				for method := range iface.Methods() {
					if !yield(xgoutil.StructMember{Member: method, Selector: named}) {
						return false
					}
				}
				return true
			}
			for embedded := range iface.EmbeddedTypes() {
				if !walk(embedded) {
					return false
				}
			}
			return true
		}
		walk(receiver)
	}
}

// memberOrigin identifies a field or method before generic instantiation.
func memberOrigin(obj gotypes.Object) gotypes.Object {
	switch obj := obj.(type) {
	case *gotypes.Var:
		return obj.Origin()
	case *gotypes.Func:
		return obj.Origin()
	default:
		return obj
	}
}

// memberTypeName returns the declaring type name of a field or method.
func memberTypeName(proj *xgo.Project, obj gotypes.Object) string {
	switch obj := obj.(type) {
	case *gotypes.Var:
		if !obj.IsField() {
			return ""
		}
		return findFieldOwnerType(proj, obj)
	case *gotypes.Func:
		recv := obj.Signature().Recv()
		if recv == nil {
			return ""
		}
		if _, ok := recv.Type().(*gotypes.Interface); ok {
			name, _ := interfaceMethodDeclaration(proj, obj)
			return name
		}
		return extractTypeName(xgoutil.DerefType(recv.Type()))
	}
	return ""
}

// extractTypeName extracts a clean type name from a types.Type.
func extractTypeName(typ gotypes.Type) string {
	if named, ok := typ.(*gotypes.Named); ok {
		return named.Obj().Name()
	}
	return ""
}

// findFieldOwnerType returns the declaring type name of field. Project fields
// use their source declaration because defined types can share field objects.
func findFieldOwnerType(proj *xgo.Project, field *gotypes.Var) string {
	field = field.Origin()
	if typeInfo, _ := proj.TypeInfo(); typeInfo != nil && field.Pkg() == typeInfo.Pkg {
		astFile := sourceASTFile(proj, field.Pos())
		if astFile == nil {
			return ""
		}
		var structType *ast.StructType
		for node := range xgoutil.PathEnclosingIntervalNodes(astFile, field.Pos(), field.Pos(), false) {
			switch node := node.(type) {
			case *ast.StructType:
				if structType != nil {
					return ""
				}
				structType = node
			case *ast.TypeSpec:
				if node.Type == structType {
					return node.Name.Name
				}
				return ""
			case *ast.GenDecl:
				if structType == nil && node == astFile.ClassFields {
					name, _ := cl.GetFileClassType(astFile, proj.Fset.PositionFor(astFile.Pos(), false).Filename, proj.Module().LookupClass)
					return name
				}
				return ""
			}
		}
		return ""
	}

	// Imported fields have no project AST. Only a unique containing type can
	// identify their owner without a receiver expression.
	pkg := field.Pkg()
	if pkg == nil {
		return ""
	}
	var owner *gotypes.Named
	for _, name := range pkg.Scope().Names() {
		obj, ok := pkg.Scope().Lookup(name).(*gotypes.TypeName)
		if !ok {
			continue
		}
		named, ok := obj.Type().(*gotypes.Named)
		if !ok {
			continue
		}
		structType, ok := named.Underlying().(*gotypes.Struct)
		if !ok {
			continue
		}
		for member := range structType.Fields() {
			if member != field {
				continue
			}
			if owner != nil {
				return ""
			}
			owner = named
		}
	}
	if owner == nil {
		return ""
	}
	return extractTypeName(owner)
}

// interfaceMethodDeclaration resolves the owner name and source field of an
// XGo method whose receiver is recorded as an unnamed interface. Anonymous
// interfaces have no owner name but still have a source field.
func interfaceMethodDeclaration(proj *xgo.Project, method *gotypes.Func) (string, *ast.Field) {
	recv := method.Signature().Recv()
	if recv == nil {
		return "", nil
	}
	if _, ok := recv.Type().(*gotypes.Interface); !ok {
		return "", nil
	}
	file, pos := objectSource(proj, method)
	if file == nil {
		return "", nil
	}
	var field *ast.Field
	var iface *ast.InterfaceType
	for node := range xgoutil.PathEnclosingIntervalNodes(file, pos, pos, false) {
		switch node := node.(type) {
		case *ast.Field:
			if field == nil {
				field = node
			}
		case *ast.InterfaceType:
			if iface != nil {
				return "", field
			}
			iface = node
		case *ast.TypeSpec:
			if node.Type == iface {
				return node.Name.Name, field
			}
			return "", field
		}
	}
	return "", field
}

// propertyTargetForCall resolves an explicit receiver from type information or
// the implicit receiver from its classfile. An unresolved explicit receiver does
// not fall back to the classfile's type.
func propertyTargetForCall(proj *xgo.Project, file *ast.File, call *ast.CallExpr) *gotypes.Named {
	if sel, ok := astutil.Unparen(call.Fun).(*ast.SelectorExpr); ok {
		info, _ := proj.TypeInfo()
		if info == nil {
			return nil
		}
		return resolvedNamedType(info.TypeOf(sel.X))
	}
	return classTypeForFile(proj, file)
}
