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

	// selectedMember is the object selected before an implicit call is attempted.
	selectedMember gotypes.Object
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

// autoPropertyKey distinguishes instance members, type selectors, and package functions.
type autoPropertyKey struct {
	name         string
	callable     gotypes.Object
	typeSelector bool
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

// resolveTypeMethod resolves a method expression without invoking it.
func (r *autoPropertyResolver) resolveTypeMethod(name string) autoProperty {
	return r.evaluate(autoPropertyKey{name: name, typeSelector: true})
}

// inferFunctionValue uses written type arguments and the expected parameter
// types to instantiate a generic selector, then checks the resulting signature.
func (r *autoPropertyResolver) inferFunctionValue(name string, sig *gotypes.Signature, typeArgs, expected []gotypes.Type) gotypes.Type {
	fn := &gogen.Element{Val: goast.NewIdent(name), Type: sig}
	for _, want := range expected {
		callback := signatureType(want)
		if callback == nil || callback.Params().Len() != sig.Params().Len() || callback.Variadic() != sig.Variadic() {
			continue
		}
		args := make([]*gogen.Element, 0, callback.Params().Len())
		for param := range callback.Params().Variables() {
			args = append(args, &gogen.Element{Val: goast.NewIdent(param.Name()), Type: param.Type()})
		}
		var flags gogen.InstrFlags
		if sig.Variadic() {
			flags = gogen.InstrFlagEllipsis
		}
		typ, err := gogen.InferFunc(r.codeBuilder().Pkg(), fn, sig, typeArgs, args, flags)
		if err == nil && gotypes.AssignableTo(typ, want) {
			return typ
		}
	}
	return nil
}

// resolvePackageObject evaluates a package function or function-valued variable alias.
func (r *autoPropertyResolver) resolvePackageObject(obj gotypes.Object) autoProperty {
	return r.evaluate(autoPropertyKey{name: functionAliasName(obj.Name()), callable: obj})
}

// codeBuilder returns the compiler builder for this request's lookups and inference.
func (r *autoPropertyResolver) codeBuilder() *gogen.CodeBuilder {
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
	return r.cb
}

// evaluate uses the compiler's overload selection and generic inference without
// parsing files, checking the project again, or declaring objects in its scopes.
func (r *autoPropertyResolver) evaluate(key autoPropertyKey) (result autoProperty) {
	if result, ok := r.results[key]; ok {
		return result
	}
	r.codeBuilder()
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
	if key.typeSelector {
		if _, pointer := gotypes.Unalias(r.receiver).(*gotypes.Pointer); pointer {
			if !r.canResolvePointerMethod(key.name) {
				return result
			}
			r.cb.InternalStack().SetLen(0)
			r.member = nil
			r.function = nil
		}
		r.cb.Typ(r.receiver)
		kind, err := r.cb.Member(key.name, 0, gogen.MemberFlagMethodAlias)
		if err == nil && kind == gogen.MemberMethod {
			result.typ = r.cb.Get(-1).Type
		}
		return result
	}
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
		result.selectedMember = r.member
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
	if xgoutil.IsOverloadPlaceholder(r.function) {
		return result
	}
	result.typ = r.cb.Get(-1).Type
	if result.typ == nil {
		result.typ = gotypes.NewTuple()
	}
	return result
}

// canResolvePointerMethod checks the member before gogen constructs a pointer
// method expression. gogen can panic for an ordinary value-receiver method.
func (r *autoPropertyResolver) canResolvePointerMethod(name string) bool {
	r.cb.Val(gotypes.NewVar(token.NoPos, nil, "this", r.receiver))
	kind, err := r.cb.Member(name, 0, gogen.MemberFlagMethodAlias)
	if err != nil || kind != gogen.MemberMethod || r.function == nil {
		return false
	}
	sig := r.function.Signature()
	if _, extended := gogen.CheckFuncEx(sig); extended {
		return true
	}
	if recv := sig.Recv(); recv != nil {
		return xgoutil.MethodExpressionReceiver(r.receiver, recv.Type()) != nil
	}
	return false
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
		for obj := range memberCandidates(r.receiver) {
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
