package server

import (
	goast "go/ast"
	"go/constant"
	gotoken "go/token"
	gotypes "go/types"
	"iter"
	"runtime"
	"strings"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
)

// autoProperty is the value of a field or implicit method call, including the
// selected overload. A nil type denotes a failed lookup or a method value.
// A void call has an empty tuple type.
type autoProperty struct {
	typ            gotypes.Type
	object         gotypes.Object
	implementation gotypes.Object
	exists         bool

	// selectedMember and selectedType describe the selector before an implicit call.
	selectedMember gotypes.Object
	selectedType   gotypes.Type
}

// autoPropertyResolver evaluates members and implicit package calls against
// existing project types. Its builder and result cache belong to one request.
type autoPropertyResolver struct {
	proj           *xgo.Project
	receiver       gotypes.Type
	cb             *gogen.CodeBuilder
	member         gotypes.Object
	implementation gotypes.Object
	results        map[autoPropertyKey]autoProperty
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

// resolveTypeMember resolves a method expression or static member on a type.
func (r *autoPropertyResolver) resolveTypeMember(name string) autoProperty {
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
	return r.evaluate(autoPropertyKey{name: xgoutil.FunctionAliasName(obj.Name()), callable: obj})
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
			NewBuiltin:      func(*gogen.Package, *gogen.Config) *gotypes.Package { return info.Builtins },
			CanImplicitCast: canAutoPropertyReceiverCast,
			NoSkipConstant:  true,
		})
		r.cb = pkg.CB()
		r.results = make(map[autoPropertyKey]autoProperty)
	}
	return r.cb
}

// operatorValue applies the compiler's lookup to one unary or binary operation.
func (r *autoPropertyResolver) operatorValue(op token.Token, operands ...gotypes.TypeAndValue) (value gotypes.TypeAndValue) {
	known := true
	for _, operand := range operands {
		if operand.Type == gotypes.Typ[gotypes.Invalid] {
			return operand
		}
		if operand.Type == nil || operand.IsType() || operand.Type.Underlying() == nil {
			known = false
		}
	}
	if !known {
		return value
	}
	cb := r.codeBuilder()
	if cb.Pkg().Builtin().Types == nil {
		return value
	}
	// A rejected XGo operation must not fall back to Go's operator rules.
	value.Type = gotypes.Typ[gotypes.Invalid]
	stack := cb.InternalStack()
	depth := stack.Len()
	defer func() {
		stack.SetLen(depth)
		repanicUnexpectedGogenError(recover(), true)
	}()
	if (op == token.SHL || op == token.SHR) && operands[0].Value != nil && operands[1].Value != nil &&
		!canEvaluateConstantShift(cb.Pkg(), op, operands) {
		return value
	}
	for _, operand := range operands {
		cb.Val(&gogen.Element{Val: goast.NewIdent("_"), Type: operand.Type, CVal: operand.Value})
	}
	if len(operands) == 1 {
		cb.UnaryOp(gotoken.Token(op))
	} else {
		cb.BinaryOp(gotoken.Token(op))
	}
	result := cb.Get(-1)
	return gotypes.TypeAndValue{Type: result.Type, Value: result.CVal}
}

// canEvaluateConstantShift limits builtin folding while allowing shift methods
// to receive ordinary arguments without a builtin shift count restriction.
func canEvaluateConstantShift(pkg *gogen.Package, op token.Token, operands []gotypes.TypeAndValue) bool {
	// Gogen folds builtin shifts before checking the count. Use go/types'
	// bound to avoid unbounded work on unchecked source.
	const shiftBound = 1023 - 1 + 52
	if count, ok := constant.Uint64Val(constant.ToInt(operands[1].Value)); ok && count <= shiftBound {
		return true
	}
	name := "XGo_Lsh"
	if op == token.SHR {
		name = "XGo_Rsh"
	}
	// BinaryOp tries methods on named operands before the builtin, even
	// when a method's signature ultimately rejects the operands.
	var receiver *gotypes.Named
	for _, operand := range operands {
		named, _ := xgoutil.DerefType(operand.Type).(*gotypes.Named)
		if named == nil || named == receiver {
			continue
		}
		receiver = named
		if _, err := pkg.MethodToFunc(operand.Type, name); err == nil {
			return true
		}
	}
	return false
}

// conversionValue applies XGo's cast protocols to positional operands. A call
// that keeps the synthetic callee is a native Go cast and needs Go checking.
func (r *autoPropertyResolver) conversionValue(typ gotypes.Type, operands []*gogen.Element, ellipsis bool) (value gotypes.TypeAndValue, native bool) {
	cb := r.codeBuilder()
	stack := cb.InternalStack()
	depth := stack.Len()
	value.Type = gotypes.Typ[gotypes.Invalid]
	defer func() {
		stack.SetLen(depth)
		repanicUnexpectedGogenError(recover(), false)
	}()
	callee := goast.NewIdent("_")
	cb.Val(&gogen.Element{Val: callee, Type: gogen.NewTypeType(typ)})
	for _, operand := range operands {
		cb.Val(operand)
	}
	var flags gogen.InstrFlags
	if ellipsis {
		flags = gogen.InstrFlagEllipsis
	}
	if err := cb.CallWithEx(len(operands), 1, flags); err != nil {
		return value, false
	}
	result := cb.Get(-1)
	if call, ok := result.Val.(*goast.CallExpr); ok && goast.Unparen(call.Fun) == callee {
		return gotypes.TypeAndValue{}, true
	}
	return gotypes.TypeAndValue{Type: result.Type, Value: result.CVal}, false
}

// evaluate uses the compiler's overload selection and generic inference without
// parsing files, checking the project again, or declaring objects in its scopes.
func (r *autoPropertyResolver) evaluate(key autoPropertyKey) (result autoProperty) {
	if result, ok := r.results[key]; ok {
		return result
	}
	cb := r.codeBuilder()
	stack := cb.InternalStack()
	depth := stack.Len()
	defer func() {
		stack.SetLen(depth)
		repanicUnexpectedGogenError(recover(), false)
		// Keep failed alias calls distinguishable from exact method values
		// so completion can exclude unresolved auto-property candidates.
		_, method := r.member.(*gotypes.Func)
		result.exists = r.member != nil && (method || key.callable != nil) && r.member.Name() != key.name
		result.implementation = r.implementation
		result.object = r.member
		r.results[key] = result
	}()
	r.member = nil
	r.implementation = nil
	if key.typeSelector {
		if ptr, ok := gotypes.Unalias(r.receiver).(*gotypes.Pointer); ok {
			if !r.canResolvePointerMember(ptr, key.name) {
				return result
			}
			stack.SetLen(depth)
			r.member = nil
			r.implementation = nil
		}
		r.cb.Typ(r.receiver)
		if _, err := r.cb.Member(key.name, 0, gogen.MemberFlagMethodAlias); err == nil {
			result.typ = r.cb.Get(-1).Type
		}
		return result
	}
	// Incomplete overload declarations can contain nil candidates. Resolve the
	// member without calling it before gogen inspects those candidate signatures.
	if key.callable != nil {
		r.member = key.callable
		r.implementation = key.callable
	} else {
		r.cb.Val(gotypes.NewVar(token.NoPos, nil, "this", r.receiver))
		kind, err := r.cb.Member(key.name, 0, gogen.MemberFlagMethodAlias)
		if err != nil {
			return result
		}
		result.selectedMember = r.member
		result.selectedType = r.cb.Get(-1).Type
		if kind == gogen.MemberField {
			result.typ = result.selectedType
			return result
		}
	}
	if fun, ok := r.implementation.(*gotypes.Func); ok && !completeAutoPropertyCandidates(fun) {
		return result
	}
	stack.SetLen(depth)
	if key.callable != nil {
		if !gogen.HasAutoProperty(key.callable.Type()) {
			r.member = nil
			return result
		}
		r.cb.Val(key.callable).Call(0)
	} else {
		r.member = nil
		r.implementation = nil
		r.cb.Val(gotypes.NewVar(token.NoPos, nil, "this", r.receiver))
		kind, err := r.cb.Member(key.name, 0, gogen.MemberFlagAutoProperty)
		if err != nil || (kind != gogen.MemberAutoProperty && kind != gogen.MemberField) {
			return result
		}
	}
	if fun, ok := r.implementation.(*gotypes.Func); ok && xgoutil.IsOverloadPlaceholder(fun) {
		return result
	}
	result.typ = r.cb.Get(-1).Type
	if result.typ == nil {
		result.typ = gotypes.NewTuple()
	}
	return result
}

// repanicUnexpectedGogenError tolerates rejected source expressions while
// leaving implementation errors visible.
func repanicUnexpectedGogenError(recovered any, foldingConstants bool) {
	switch recovered.(type) {
	case nil, *gogen.CodeError, *gogen.MatchError, *gogen.ImportError:
		return
	}
	if !isGogenSourcePanic(recovered, foldingConstants) {
		panic(recovered)
	}
}

// isGogenSourcePanic recognizes untyped diagnostics emitted by the pinned
// gogen version. Use the panic site to distinguish them from the same panic
// types raised by unrelated code.
func isGogenSourcePanic(recovered any, foldingConstants bool) bool {
	var pcs [16]uintptr
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs[:])])
	var panicking, logged bool
	_, runtimeError := recovered.(runtime.Error)
	for {
		frame, more := frames.Next()
		switch {
		case frame.Function == "runtime.gopanic":
			panicking = true
		case strings.HasPrefix(frame.Function, "runtime."):
			// Runtime helper frames vary between Go architectures.
		case panicking:
			switch frame.Function {
			case "log.Panicf":
				logged = true
			case "github.com/goplus/gogen.boundTypeParams":
				// Gogen indexes a missing explicit type argument before
				// it can report a match error or try another candidate.
				return runtimeError
			case "github.com/goplus/gogen.matchRcast":
				// A reverse cast method with parameters is rejected here.
				return logged
			case "github.com/goplus/gogen.doBinaryOp":
				_, ok := recovered.(error)
				return foldingConstants && ok && !runtimeError
			case "go/constant.BinaryOp":
				_, message := recovered.(string)
				return foldingConstants && (message || runtimeError)
			case "go/constant.Compare":
				_, message := recovered.(string)
				if !message || foldingConstants {
					return message
				}
				// Gogen's integer range check cannot order complex constants.
				caller, _ := frames.Next()
				return caller.Function == "github.com/goplus/gogen.outOfRange"
			case "go/constant.UnaryOp", "go/constant.Shift",
				"math/big.(*Int).Quo", "math/big.(*Int).Rem", "math/big.(*Rat).Quo":
				_, message := recovered.(string)
				return foldingConstants && message
			default:
				return false
			}
		}
		if !more {
			return false
		}
	}
}

// completeAutoPropertyCandidates checks nested candidates before gogen
// inspects their signatures during implicit call resolution.
func completeAutoPropertyCandidates(fun *gotypes.Func) bool {
	_, candidates := gogen.CheckSigFuncExObjects(fun.Signature())
	for _, candidate := range candidates {
		switch candidate := candidate.(type) {
		case nil:
			return false
		case *gotypes.Func:
			if candidate == nil || !completeAutoPropertyCandidates(candidate) {
				return false
			}
		}
	}
	return true
}

// canResolvePointerMember checks static values and methods before gogen
// constructs a pointer selector. Gogen can panic for a value-receiver method.
func (r *autoPropertyResolver) canResolvePointerMember(ptr *gotypes.Pointer, name string) bool {
	if named, ok := gotypes.Unalias(ptr.Elem()).(*gotypes.Named); ok {
		for method := range named.Origin().Methods() {
			if method.Name() != name {
				continue
			}
			obj := types.StaticMemberObject(method)
			if _, callable := obj.(*gotypes.Func); obj != nil && !callable &&
				(obj.Exported() || obj.Pkg() == nil || obj.Pkg().Path() == r.cb.Pkg().Path()) {
				return true
			}
			break
		}
	}
	r.cb.Val(gotypes.NewVar(token.NoPos, nil, "this", r.receiver))
	kind, err := r.cb.Member(name, 0, gogen.MemberFlagMethodAlias)
	fun, ok := r.member.(*gotypes.Func)
	if err != nil || kind != gogen.MemberMethod || !ok {
		return false
	}
	sig := fun.Signature()
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
	if _, ok := obj.(*gotypes.Func); ok {
		r.implementation = obj
	}
}

// Call records the implementation selected by overload resolution.
func (r *autoPropertyResolver) Call(_ goast.Node, obj gotypes.Object) {
	fun, _ := obj.(*gotypes.Func)
	if fun != nil {
		ext, _ := gogen.CheckFuncEx(fun.Signature())
		switch ext.(type) {
		case *gogen.TyOverloadFunc, *gogen.TyOverloadMethod:
			// Gogen reports nested groups after reporting the implementation.
			return
		}
	}
	r.implementation = obj
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
				name = xgoutil.MemberAliasName(name)
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
	return value != nil && xgoutil.CanCastEmbeddedPointer(from, to)
}
