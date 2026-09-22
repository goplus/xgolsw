package server

import (
	"cmp"
	gotypes "go/types"
	"iter"
	"maps"
	"slices"
	"sync"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
)

// methodReceiver holds a project's candidate receiver and its pointer method set.
type methodReceiver struct {
	typ     gotypes.Type
	pointer *gotypes.Pointer
	methods *gotypes.MethodSet
}

// methodInfo shares method sets and computes implementation relationships on
// demand. Its maps are immutable after construction and its lazy results use
// sync.OnceValue so concurrent requests share the same analysis. Callers must
// not modify its cached maps or slices.
type methodInfo struct {
	receivers       []methodReceiver
	implementations map[*gotypes.Interface]func() map[string][]*gotypes.Func
	relations       map[string]func() map[*gotypes.Func][]*gotypes.Func
}

// methodInfoCacheKind identifies method analysis for one project revision.
type methodInfoCacheKind struct{}

// buildMethodInfoCache collects method candidates from stable project types.
func buildMethodInfoCache(proj *xgo.Project) (any, error) {
	proj.TypeInfo()
	proj = proj.Snapshot()
	info, _ := proj.TypeInfo()
	result := &methodInfo{
		implementations: make(map[*gotypes.Interface]func() map[string][]*gotypes.Func),
		relations:       make(map[string]func() map[*gotypes.Func][]*gotypes.Func),
	}
	if info == nil {
		return result, nil
	}
	for receiver := range projectReceiverTypes(info) {
		pointer := gotypes.NewPointer(receiver)
		result.receivers = append(result.receivers, methodReceiver{receiver, pointer, gotypes.NewMethodSet(pointer)})
	}
	interfacesByMethod := make(map[string][]*gotypes.Interface)
	for iface := range projectInterfaces(info) {
		result.implementations[iface] = sync.OnceValue(func() map[string][]*gotypes.Func {
			return result.findImplementations(iface)
		})
		for method := range iface.Methods() {
			interfacesByMethod[method.Id()] = append(interfacesByMethod[method.Id()], iface)
		}
	}
	for id, interfaces := range interfacesByMethod {
		result.relations[id] = sync.OnceValue(func() map[*gotypes.Func][]*gotypes.Func {
			return result.methodRelations(id, interfaces)
		})
	}
	return result, nil
}

// methodInfoForProject returns the method index for the project's current state.
func methodInfoForProject(proj *xgo.Project) (*methodInfo, error) {
	data, err := proj.Cache(methodInfoCacheKind{})
	if err != nil {
		return nil, err
	}
	return data.(*methodInfo), nil
}

// findImplementations resolves all methods of an interface in one receiver pass,
// preserving instantiated methods for their source definition positions.
func (info *methodInfo) findImplementations(iface *gotypes.Interface) map[string][]*gotypes.Func {
	result := make(map[string][]*gotypes.Func)
	seen := make(map[*gotypes.Func]bool)
	for _, receiver := range info.receivers {
		if !gotypes.Implements(receiver.typ, iface) && !gotypes.Implements(receiver.pointer, iface) {
			continue
		}
		for target := range iface.Methods() {
			selection := receiver.methods.Lookup(target.Pkg(), target.Name())
			if selection == nil {
				// Implements tolerates invalid types to suppress follow-on
				// errors. Pointers to interfaces also have no methods.
				continue
			}
			method := selection.Obj().(*gotypes.Func)
			if !seen[method] {
				seen[method] = true
				result[target.Id()] = append(result[target.Id()], method)
			}
		}
	}
	return result
}

// relatedMethods returns connected interface and implementation declarations,
// normalizing generic instances to their shared declaration.
func (info *methodInfo) relatedMethods(target *gotypes.Func) []*gotypes.Func {
	target = target.Origin()
	if relations := info.relations[target.Id()]; relations != nil {
		if methods := relations()[target]; len(methods) != 0 {
			return methods
		}
	}
	return []*gotypes.Func{target}
}

// methodRelations groups declarations connected by interface implementation.
// Union roots avoid retaining the dense graph of individual relationships.
func (info *methodInfo) methodRelations(id string, interfaces []*gotypes.Interface) map[*gotypes.Func][]*gotypes.Func {
	roots := make(map[*gotypes.Func]*gotypes.Func)
	root := func(method *gotypes.Func) *gotypes.Func {
		method = method.Origin()
		if roots[method] == nil {
			roots[method] = method
		}
		for roots[method] != method {
			roots[method] = roots[roots[method]]
			method = roots[method]
		}
		return method
	}
	connect := func(first, second *gotypes.Func) {
		roots[root(first)] = root(second)
	}
	methods := make([]*gotypes.Func, len(interfaces))
	for i, iface := range interfaces {
		for method := range iface.Methods() {
			if method.Id() != id {
				continue
			}
			methods[i] = method
			root(method)
			for _, implementation := range info.implementations[iface]()[id] {
				connect(method, implementation)
			}
			break
		}
	}
	// Interface contracts can be connected without a concrete implementation.
	for i, iface := range interfaces {
		for j, other := range interfaces[:i] {
			if root(methods[i]) == root(methods[j]) {
				continue
			}
			if gotypes.Implements(iface, other) || gotypes.Implements(other, iface) {
				connect(methods[i], methods[j])
			}
		}
	}
	groups := make(map[*gotypes.Func][]*gotypes.Func)
	for method := range roots {
		leader := root(method)
		groups[leader] = append(groups[leader], method)
	}
	result := make(map[*gotypes.Func][]*gotypes.Func, len(roots))
	for _, group := range groups {
		// Stable order keeps rename validation and response construction
		// independent of map iteration order.
		slices.SortFunc(group, func(a, b *gotypes.Func) int {
			return cmp.Compare(a.Pos(), b.Pos())
		})
		for _, method := range group {
			result[method] = group
		}
	}
	return result
}

// projectReceiverTypes returns method set candidates from project type information,
// including anonymous structs, generated classes, local types, and imported types.
func projectReceiverTypes(info *types.Info) iter.Seq[gotypes.Type] {
	candidates := make(map[gotypes.Type]struct{})
	addType := func(typ gotypes.Type) {
		for {
			typ = gotypes.Unalias(typ)
			pointer, ok := typ.(*gotypes.Pointer)
			if !ok {
				break
			}
			typ = pointer.Elem()
		}
		switch typ.(type) {
		case *gotypes.Named, *gotypes.Struct:
			if typ.Underlying() != nil {
				candidates[typ] = struct{}{}
			}
		}
	}
	addScope := func(scope *gotypes.Scope) {
		for _, name := range scope.Names() {
			addType(scope.Lookup(name).Type())
		}
	}
	addScope(info.Pkg.Scope())
	for _, scope := range info.Scopes {
		addScope(scope)
	}
	for _, value := range info.Types {
		addType(value.Type)
	}
	return maps.Keys(candidates)
}

// projectInterfaces yields interfaces used in project types, including contracts
// nested in imported signatures, fields, containers, and generic constraints.
func projectInterfaces(info *types.Info) iter.Seq[*gotypes.Interface] {
	return func(yield func(*gotypes.Interface) bool) {
		var pending []gotypes.Type
		for _, value := range info.Types {
			pending = append(pending, value.Type)
		}
		for _, objects := range []map[*ast.Ident]gotypes.Object{info.Defs, info.Uses, info.Overloads} {
			for _, obj := range objects {
				if obj != nil {
					pending = append(pending, obj.Type())
				}
			}
		}
		addTypeParams := func(params *gotypes.TypeParamList) {
			for i := 0; i < params.Len(); i++ {
				pending = append(pending, params.At(i).Constraint())
			}
		}
		seen := make(map[gotypes.Type]bool)
		for len(pending) != 0 {
			typ := gotypes.Unalias(pending[len(pending)-1])
			pending = pending[:len(pending)-1]
			if typ == nil || seen[typ] {
				continue
			}
			seen[typ] = true
			switch typ := typ.(type) {
			case *gotypes.Named:
				pending = append(pending, typ.Underlying())
				addTypeParams(typ.TypeParams())
				for i := 0; i < typ.TypeArgs().Len(); i++ {
					pending = append(pending, typ.TypeArgs().At(i))
				}
			case *gotypes.TypeParam:
				pending = append(pending, typ.Constraint())
			case *gotypes.Interface:
				if !yield(typ) {
					return
				}
				for method := range typ.Methods() {
					pending = append(pending, method.Type())
				}
				for embedded := range typ.EmbeddedTypes() {
					pending = append(pending, embedded)
				}
			case *gotypes.Signature:
				// A failed overload call may record only its wrapper. Visit
				// candidate signatures too, including nested overloads.
				_, candidates := gogen.CheckSigFuncExObjects(typ)
				for _, candidate := range candidates {
					if fn, ok := candidate.(*gotypes.Func); ok && fn != nil {
						pending = append(pending, fn.Type())
					}
				}
				pending = append(pending, typ.Params(), typ.Results())
				addTypeParams(typ.TypeParams())
				if recv := typ.Recv(); recv != nil {
					pending = append(pending, recv.Type())
				}
			case *gotypes.Tuple:
				for i := 0; i < typ.Len(); i++ {
					pending = append(pending, typ.At(i).Type())
				}
			case *gotypes.Struct:
				for field := range typ.Fields() {
					pending = append(pending, field.Type())
				}
			case *gotypes.Map:
				pending = append(pending, typ.Key(), typ.Elem())
			case interface{ Elem() gotypes.Type }:
				pending = append(pending, typ.Elem())
			}
		}
	}
}
