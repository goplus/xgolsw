package server

import (
	goast "go/ast"
	gotypes "go/types"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
)

// autoProperty is the result of an implicit method call, including the selected
// overload. A nil type denotes a failed lookup or call. A void call has an empty
// tuple type.
type autoProperty struct {
	typ      gotypes.Type
	function *gotypes.Func
	exists   bool
}

// autoPropertyResolver evaluates member expressions against existing project
// types. Its builder and result cache belong to one request and receiver.
type autoPropertyResolver struct {
	proj     *xgo.Project
	receiver gotypes.Type
	cb       *gogen.CodeBuilder
	member   *gotypes.Func
	function *gotypes.Func
	results  map[string]autoProperty
}

// resolve uses the compiler's overload selection and generic inference without
// parsing files, checking the project again, or declaring objects in its scopes.
func (r *autoPropertyResolver) resolve(name string) (result autoProperty) {
	if result, ok := r.results[name]; ok {
		return result
	}
	if r.cb == nil {
		info, _ := r.proj.TypeInfo()
		pkg := gogen.NewPackage(info.Pkg.Path(), info.Pkg.Name(), &gogen.Config{
			Types:           info.Pkg,
			Fset:            r.proj.Fset,
			Importer:        r.proj,
			Recorder:        r,
			NewBuiltin:      func(*gogen.Package, *gogen.Config) *gotypes.Package { return nil },
			CanImplicitCast: canAutoPropertyReceiverCast,
		})
		r.cb = pkg.CB()
		r.results = make(map[string]autoProperty)
	}
	defer func() {
		r.cb.InternalStack().SetLen(0)
		// Auto-property calls panic on ordinary type errors. Invalid
		// receivers and uninferable parameters simply exclude a candidate.
		switch recovered := recover().(type) {
		case nil, *gogen.CodeError, *gogen.MatchError, *gogen.ImportError:
		default:
			panic(recovered)
		}
		// Member records only accessible methods. An inexact match is
		// recorded only after the compiler accepts it as an auto-property.
		result.exists = r.member != nil && r.member.Name() != name
		result.function = r.function
		r.results[name] = result
	}()
	r.member = nil
	r.function = nil
	r.cb.Val(gotypes.NewVar(token.NoPos, nil, "this", r.receiver))
	kind, err := r.cb.Member(name, 0, gogen.MemberFlagAutoProperty)
	if err != nil || kind != gogen.MemberAutoProperty {
		return result
	}
	result.typ = r.cb.Get(-1).Type
	if result.typ == nil {
		result.typ = gotypes.NewTuple()
	}
	return result
}

// Member records the method before an overload or template call is resolved.
func (r *autoPropertyResolver) Member(_ goast.Node, obj gotypes.Object) {
	r.member, _ = obj.(*gotypes.Func)
	r.function = r.member
}

// Call records the implementation selected by overload resolution.
func (r *autoPropertyResolver) Call(_ goast.Node, obj gotypes.Object) {
	r.function, _ = obj.(*gotypes.Func)
}

// canAutoPropertyReceiverCast follows XGo's implicit conversion from a struct
// pointer to an embedded named type's pointer. Only the type result is used, so
// there is no need to rewrite the temporary expression as an embedded selector.
func canAutoPropertyReceiverCast(_ *gogen.Package, from, to gotypes.Type, value *gogen.Element) bool {
	source, sourceOK := from.(*gotypes.Pointer)
	target, targetOK := to.(*gotypes.Pointer)
	if value == nil || !sourceOK || !targetOK {
		return false
	}
	named, ok := target.Elem().(*gotypes.Named)
	if !ok {
		return false
	}
	seen := make(map[*gotypes.Struct]bool)
	var contains func(gotypes.Type) bool
	contains = func(typ gotypes.Type) bool {
		structure, ok := typ.Underlying().(*gotypes.Struct)
		if !ok || seen[structure] {
			return false
		}
		seen[structure] = true
		for field := range structure.Fields() {
			if !field.Embedded() {
				continue
			}
			typ := field.Type()
			if pointer, ok := typ.(*gotypes.Pointer); ok {
				typ = pointer.Elem()
			}
			if embedded, ok := typ.(*gotypes.Named); ok && (embedded == named || contains(embedded)) {
				return true
			}
		}
		return false
	}
	return contains(source.Elem())
}
