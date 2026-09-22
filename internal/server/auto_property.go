package server

import (
	goast "go/ast"
	gotypes "go/types"
	"iter"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// autoProperty is the value of a field or implicit method call, including the
// selected overload. A nil type denotes a failed lookup or a method value.
// A void call has an empty tuple type.
type autoProperty struct {
	typ      gotypes.Type
	object   gotypes.Object
	function *gotypes.Func
	exists   bool
}

// autoPropertyResolver evaluates members and implicit package calls against
// existing project types. Its builder and result cache belong to one request.
type autoPropertyResolver struct {
	proj     *xgo.Project
	receiver gotypes.Type
	cb       *gogen.CodeBuilder
	member   gotypes.Object
	function *gotypes.Func
	results  map[autoPropertyKey]autoProperty
}

// autoPropertyKey distinguishes receiver members from package functions.
type autoPropertyKey struct {
	name     string
	callable gotypes.Object
}

// isAliasCallable reports whether XGo can expose an alias for this declaration.
// Function-valued variables participate only at package scope, not as fields
// or local variables.
func isAliasCallable(obj gotypes.Object) bool {
	switch obj := obj.(type) {
	case *gotypes.Func:
		return true
	case *gotypes.Var:
		return obj.Pkg() != nil && obj.Parent() == obj.Pkg().Scope() && signatureType(obj.Type()) != nil
	}
	return false
}

// resolve evaluates a receiver member by its source name.
func (r *autoPropertyResolver) resolve(name string) autoProperty {
	return r.evaluate(autoPropertyKey{name: name})
}

// resolvePackageObject evaluates a package function or function-valued variable alias.
func (r *autoPropertyResolver) resolvePackageObject(obj gotypes.Object) autoProperty {
	return r.evaluate(autoPropertyKey{name: functionAliasName(obj.Name()), callable: obj})
}

// evaluate uses the compiler's overload selection and generic inference without
// parsing files, checking the project again, or declaring objects in its scopes.
func (r *autoPropertyResolver) evaluate(key autoPropertyKey) (result autoProperty) {
	if result, ok := r.results[key]; ok {
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
		r.results = make(map[autoPropertyKey]autoProperty)
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
		// Keep failed alias calls distinguishable from exact method values
		// so completion can exclude unresolved auto-property candidates.
		_, method := r.member.(*gotypes.Func)
		result.exists = r.member != nil && (method || key.callable != nil) && r.member.Name() != key.name
		result.function = r.function
		result.object = r.member
		r.results[key] = result
	}()
	r.member = nil
	r.function = nil
	// Incomplete overload declarations can contain nil candidates. Resolve the
	// member without calling it before gogen inspects those candidate signatures.
	if key.callable != nil {
		r.member = key.callable
		r.function, _ = key.callable.(*gotypes.Func)
	} else {
		r.cb.Val(gotypes.NewVar(token.NoPos, nil, "this", r.receiver))
		kind, err := r.cb.Member(key.name, 0, gogen.MemberFlagMethodAlias)
		if err != nil {
			return result
		}
		if kind == gogen.MemberField {
			result.typ = r.cb.Get(-1).Type
			return result
		}
	}
	if r.function != nil {
		_, candidates := gogen.CheckSigFuncExObjects(r.function.Signature())
		for _, candidate := range candidates {
			if fun, ok := candidate.(*gotypes.Func); !ok || fun == nil {
				return result
			}
		}
	}
	r.cb.InternalStack().SetLen(0)
	if key.callable != nil {
		if !gogen.HasAutoProperty(key.callable.Type()) {
			r.member = nil
			return result
		}
		r.cb.Val(key.callable).Call(0)
	} else {
		r.member = nil
		r.function = nil
		r.cb.Val(gotypes.NewVar(token.NoPos, nil, "this", r.receiver))
		kind, err := r.cb.Member(key.name, 0, gogen.MemberFlagAutoProperty)
		if err != nil || (kind != gogen.MemberAutoProperty && kind != gogen.MemberField) {
			return result
		}
	}
	result.typ = r.cb.Get(-1).Type
	if result.typ == nil {
		result.typ = gotypes.NewTuple()
	}
	return result
}

// Member records the field or method before a possible implicit call.
func (r *autoPropertyResolver) Member(_ goast.Node, obj gotypes.Object) {
	r.member = obj
	r.function, _ = obj.(*gotypes.Func)
}

// Call records the implementation selected by overload resolution.
func (r *autoPropertyResolver) Call(_ goast.Node, obj gotypes.Object) {
	r.function, _ = obj.(*gotypes.Func)
}

// candidates resolves each possible field or method alias independently of
// its declared spelling. Failed alias calls remain visible for scope shadowing.
func (r *autoPropertyResolver) candidates() iter.Seq2[string, autoProperty] {
	return func(yield func(string, autoProperty) bool) {
		seen := make(map[string]bool)
		for obj := range propertyCandidates(r.receiver) {
			name := obj.Name()
			if xgoutil.IsXGoInternalName(name) {
				continue
			}
			switch obj := obj.(type) {
			case *gotypes.Var:
				if obj.Embedded() {
					continue
				}
			case *gotypes.Func:
				if xgoutil.IsMarkedAsXGoPackage(obj.Pkg()) && xgoutil.IsXGoOverloadedFuncName(name) {
					continue
				}
				name = functionAliasName(name)
				if name == obj.Name() {
					continue
				}
			default:
				continue
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			if !yield(name, r.resolve(name)) {
				return
			}
		}
	}
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
