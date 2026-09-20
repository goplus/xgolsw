package server

import (
	gotypes "go/types"
	"iter"
	"slices"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo/types"
)

// relatedMethodDeclarations finds the connected interface and implementation
// methods for target, normalizing generic instantiations to their declarations.
func relatedMethodDeclarations(info *types.Info, target *gotypes.Func) []*gotypes.Func {
	connections := make(map[*gotypes.Func][]*gotypes.Func)
	connect := func(first, second *gotypes.Func) {
		first, second = first.Origin(), second.Origin()
		if first != second {
			connections[first] = append(connections[first], second)
			connections[second] = append(connections[second], first)
		}
	}
	interfaces := slices.Collect(projectInterfaces(info))
	for _, iface := range interfaces {
		for method := range iface.Methods() {
			if method.Id() != target.Id() {
				continue
			}
			for implementation := range implementingMethods(info, iface, method) {
				connect(method, implementation)
			}
			// Interfaces can implement each other without any concrete type
			// appearing in the project, including through embedded declarations.
			for _, other := range interfaces {
				if other == iface || !gotypes.Implements(other, iface) {
					continue
				}
				for candidate := range other.Methods() {
					if candidate.Id() == method.Id() {
						connect(method, candidate)
					}
				}
			}
		}
	}
	methods := []*gotypes.Func{target.Origin()}
	seen := map[*gotypes.Func]bool{target.Origin(): true}
	for i := 0; i < len(methods); i++ {
		for _, method := range connections[methods[i]] {
			if !seen[method] {
				seen[method] = true
				methods = append(methods, method)
			}
		}
	}
	return methods
}

// projectInterfaces yields interfaces used in project types, including contracts
// nested in imported signatures, fields, containers, and generic constraints.
func projectInterfaces(info *types.Info) iter.Seq[*gotypes.Interface] {
	return func(yield func(*gotypes.Interface) bool) {
		var pending []gotypes.Type
		for _, value := range info.Types {
			pending = append(pending, value.Type)
		}
		for _, objects := range []map[*ast.Ident]gotypes.Object{info.Defs, info.Uses} {
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
