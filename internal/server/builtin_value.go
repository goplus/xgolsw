package server

import (
	gotypes "go/types"
	"iter"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// builtinCallName identifies builtins by their resolved object, so a local
// function named append or delete retains its ordinary signature.
func builtinCallName(info *types.Info, call *ast.CallExpr) string {
	ident := xgoutil.CallExprFunIdent(call)
	if ident == nil {
		return ""
	}
	obj := info.ObjectOf(ident)
	if _, ok := obj.(*gotypes.Builtin); ok || xgoutil.IsInBuiltinPkg(obj) {
		return obj.Name()
	}
	return ""
}

// appendedValueTypes returns the types accepted after a slice destination.
// Appending to a byte slice with ellipsis also accepts a string.
func appendedValueTypes(container gotypes.Type, ellipsis bool) []gotypes.Type {
	if !xgoutil.IsValidType(container) {
		return nil
	}
	slice, ok := container.Underlying().(*gotypes.Slice)
	if !ok {
		return nil
	}
	if !ellipsis {
		return []gotypes.Type{slice.Elem()}
	}
	result := []gotypes.Type{container}
	if isByteSlice(container) {
		result = append(result, gotypes.Typ[gotypes.String])
	}
	return result
}

// builtinArgTypes resolves the concrete argument types of builtins whose
// parameters depend on another argument. It also accepts the next argument
// index while the user is completing an unfinished call.
func builtinArgTypes(info *types.Info, call *ast.CallExpr, index int) []gotypes.Type {
	if index < 0 {
		return nil
	}
	switch builtinCallName(info, call) {
	case "append":
		if index > 0 && len(call.Args) > 0 {
			return appendedValueTypes(info.TypeOf(call.Args[0]), call.Ellipsis.IsValid())
		}
	case "delete":
		if index == 1 && len(call.Args) > 0 {
			if typ := info.TypeOf(call.Args[0]); xgoutil.IsValidType(typ) {
				if m, ok := typ.Underlying().(*gotypes.Map); ok {
					return []gotypes.Type{m.Key()}
				}
			}
		}
	case "copy":
		if index == 1 && len(call.Args) > 0 {
			return appendedValueTypes(info.TypeOf(call.Args[0]), true)
		}
		if index == 0 && len(call.Args) > 1 {
			typ := info.TypeOf(call.Args[1])
			if xgoutil.IsValidType(typ) {
				if _, ok := typ.Underlying().(*gotypes.Slice); ok {
					return []gotypes.Type{typ}
				}
				if basic, ok := typ.Underlying().(*gotypes.Basic); ok && basic.Info()&gotypes.IsString != 0 {
					return []gotypes.Type{gotypes.NewSlice(gotypes.Typ[gotypes.Byte])}
				}
			}
		}
	case "make":
		if index > 0 {
			return []gotypes.Type{gotypes.Typ[gotypes.Int]}
		}
	case "min", "max":
		// The result records the common operand type, including promotion of
		// untyped constants. A typed sibling also works in incomplete calls.
		if typ := info.TypeOf(call); xgoutil.IsValidType(typ) {
			return []gotypes.Type{gotypes.Default(typ)}
		}
		for i, arg := range call.Args {
			if typ := info.TypeOf(arg); i != index && xgoutil.IsValidType(typ) && !isUntypedType(typ) {
				return []gotypes.Type{typ}
			}
		}
	}
	return nil
}

// builtinArgValueTypes yields values with builtin-specific parameter types.
func builtinArgValueTypes(info *types.Info, call *ast.CallExpr) iter.Seq2[ast.Expr, gotypes.Type] {
	return func(yield func(ast.Expr, gotypes.Type) bool) {
		for index, arg := range call.Args {
			typ := expectedValueType(info, arg, builtinArgTypes(info, call, index))
			if typ == nil {
				continue
			}
			for value, typ := range valueElementTypes(info, arg, typ) {
				if !yield(value, typ) {
					return
				}
			}
		}
	}
}

// isByteSlice reports whether typ is a slice of bytes, including named slices.
func isByteSlice(typ gotypes.Type) bool {
	if !xgoutil.IsValidType(typ) {
		return false
	}
	slice, ok := typ.Underlying().(*gotypes.Slice)
	return ok && gotypes.Identical(slice.Elem(), gotypes.Typ[gotypes.Byte])
}
