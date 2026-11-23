/*
 * Copyright (c) 2025 The XGo Authors (xgo.dev). All rights reserved.
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

package xgoutil

import (
	goast "go/ast"
	"go/constant"
	gotypes "go/types"
	"iter"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo/types"
)

const (
	XGotPrefix = "XGot_" // XGo template method.
	XGooPrefix = "XGoo_" // XGo overload function/method.
	XGoxPrefix = "XGox_" // XGo type as parameters function/method.
)

// FunctionAliasName returns the preferred ASCII alias of a function, keeping
// the declared spelling when its lowercase form is a keyword.
func FunctionAliasName(name string) string {
	alias := ToLowerCamelCase(name)
	if alias != name && token.IsKeyword(alias) {
		return name
	}
	return alias
}

// MemberAliasName returns the preferred source alias of a method. Protocol
// aliases omit the XGo prefix before the underscore.
func MemberAliasName(name string) string {
	if strings.HasPrefix(name, "XGo_") {
		return strings.TrimPrefix(name, "XGo")
	}
	return FunctionAliasName(name)
}

// MemberNameForAlias returns the name looked up for an ASCII or protocol
// method alias.
func MemberNameForAlias(name string) string {
	if strings.HasPrefix(name, "_") {
		return "XGo" + name
	}
	return UpperFirstASCII(name)
}

// IsXGotMethodName reports whether the given name is an XGo template method name.
func IsXGotMethodName(name string) bool {
	_, _, ok := SplitXGotMethodName(name, false)
	return ok
}

// SplitXGotMethodName decodes the receiver and method from an XGot_ or Gopt_
// name, including the double-underscore form. If trimXGox is true, it strips
// the method's XGox_ prefix.
func SplitXGotMethodName(name string, trimXGox bool) (recvTypeName string, methodName string, ok bool) {
	recvTypeName, methodName, ok = splitXGoMemberName(name, XGotPrefix, "Gopt_")
	if !ok {
		return "", "", false
	}
	if trimXGox {
		if funcName, ok := splitXGoxMethodName(methodName); ok {
			methodName = funcName
		}
	}
	return
}

// TemplateMethodName decodes a package-level template implementation whose
// receiver names a defined type in a marked package. The trimXGox option
// follows [SplitXGotMethodName].
func TemplateMethodName(fun *gotypes.Func, trimXGox bool) (string, string, bool) {
	owner, method, ok := SplitXGotMethodName(fun.Name(), trimXGox)
	if ok && hasEncodedMemberOwner(fun, owner) {
		return owner, method, true
	}
	return "", "", false
}

// hasEncodedMemberOwner reports whether a package-level function's encoded
// owner names a defined type in the marked package.
func hasEncodedMemberOwner(fun *gotypes.Func, owner string) bool {
	if fun.Signature().Recv() != nil || !IsMarkedAsXGoPackage(fun.Pkg()) {
		return false
	}
	obj, ok := fun.Pkg().Scope().Lookup(owner).(*gotypes.TypeName)
	if !ok {
		return false
	}
	_, ok = obj.Type().(*gotypes.Named)
	return ok
}

// ReconstructedMethodImplementation returns the function wrapped by a static
// or template method. The second result reports template receiver binding.
func ReconstructedMethodImplementation(fun *gotypes.Func) (implementation *gotypes.Func, template bool) {
	if fun == nil {
		return nil, false
	}
	ext, _ := gogen.CheckFuncEx(fun.Signature())
	switch member := ext.(type) {
	case *gogen.TyTemplateRecvMethod:
		implementation, _ = member.Obj().(*gotypes.Func)
		return implementation, true
	case *gogen.TyStaticMember:
		implementation, _ = member.Obj().(*gotypes.Func)
	}
	return implementation, false
}

// ReferencesEncodedFuncName reports whether the source names the encoded Go
// function directly instead of using its reconstructed method or callable.
func ReferencesEncodedFuncName(fun *gotypes.Func, name string) bool {
	encodedName := fun.Name()
	if name != encodedName && name != ToLowerCamelCase(encodedName) {
		// The compiler records the selected implementation at an overload call.
		match := xgoOverloadFuncNameRE.FindStringSubmatch(encodedName)
		if match == nil {
			return false
		}
		group := match[1]
		if name != group && name != ToLowerCamelCase(group) {
			return false
		}
	}
	if _, _, ok := TemplateMethodName(fun, false); ok {
		return true
	}
	if owner, _, ok := SplitXGosMemberName(encodedName); ok && hasEncodedMemberOwner(fun, owner) {
		return true
	}
	_, ok := SplitXGoxFuncName(encodedName)
	return ok && IsMarkedAsXGoPackage(fun.Pkg()) && fun.Signature().Recv() == nil
}

// SplitXGosMemberName splits a static member into its owning type and name.
func SplitXGosMemberName(name string) (recvTypeName, memberName string, ok bool) {
	return splitXGoMemberName(name, "XGos_", "Gops_")
}

// XGosMemberName returns the Go implementation name emitted for a static member.
func XGosMemberName(owner, member string) string {
	separator := "_"
	if strings.ContainsRune(owner, '_') || strings.ContainsRune(member, '_') {
		separator = "__"
	}
	return "XGos" + separator + owner + separator + member
}

// splitXGoMemberName decodes the shared receiver and member name encoding.
func splitXGoMemberName(name, current, historical string) (string, string, bool) {
	rest, ok := strings.CutPrefix(name, current)
	if !ok {
		rest, ok = strings.CutPrefix(name, historical)
		if !ok {
			return "", "", false
		}
	}
	separator := "_"
	if unescaped, ok := strings.CutPrefix(rest, "_"); ok {
		rest, separator = unescaped, "__"
	}
	recv, member, found := strings.Cut(rest, separator)
	if !found || recv == "" || member == "" {
		return "", "", false
	}
	return recv, member, true
}

// splitXGoxMethodName recognizes the type-as-parameter spelling used when
// gogen adds an alias to a reconstructed method.
func splitXGoxMethodName(name string) (string, bool) {
	method, ok := strings.CutPrefix(name, XGoxPrefix)
	return method, ok && method != ""
}

// SplitXGoxFuncName splits an XGo type as parameters function name into the
// function name.
func SplitXGoxFuncName(name string) (funcName string, ok bool) {
	if funcName, ok = strings.CutPrefix(name, XGoxPrefix); !ok {
		funcName, ok = strings.CutPrefix(name, "Gopx_")
	}
	if !ok || funcName == "" {
		return "", false
	}
	return funcName, true
}

// ParseXGoFuncName parses the XGo overloaded function name.
func ParseXGoFuncName(name string) (parsedName string, overloadID *string) {
	parsedName = name
	if matches := xgoOverloadFuncNameRE.FindStringSubmatch(parsedName); len(matches) == 3 {
		parsedName = matches[1]
		overloadID = &matches[2]
	}
	parsedName = FunctionAliasName(parsedName)
	return
}

// xgoOverloadFuncNameRE is the regular expression of the XGo overloaded
// function name.
var xgoOverloadFuncNameRE = regexp.MustCompile(`^(.+)__([0-9a-z])$`)

// IsXGoOverloadedFuncName reports whether the given function name is an XGo
// overloaded function name.
func IsXGoOverloadedFuncName(name string) bool {
	return xgoOverloadFuncNameRE.MatchString(name)
}

// OverloadGroupMarker returns the Go string constant that declares a
// reconstructed overload group, following wrappers to the group they expose.
func OverloadGroupMarker(fun *gotypes.Func) *gotypes.Const {
	for {
		ext, _ := gogen.CheckFuncEx(fun.Signature())
		switch ext := ext.(type) {
		case *gogen.TyOverloadFunc:
			return packageOverloadMarker(fun, ext.Funcs)
		case *gogen.TyOverloadMethod:
			if named := overloadReceiverType(fun); named != nil {
				return methodOverloadMarker(fun, named.Obj().Name(), ext.Methods)
			}
		case interface{ Obj() gotypes.Object }:
			if target, ok := ext.Obj().(*gotypes.Func); ok {
				fun = target
				continue
			}
		}
		return nil
	}
}

// packageOverloadMarker considers only spellings that gogen decodes as the
// given package-level name. A prefix introduced by reconstruction can appear
// in the scope after it decoded the plain marker.
func packageOverloadMarker(group *gotypes.Func, implementations []gotypes.Object) *gotypes.Const {
	scope, name := group.Pkg().Scope(), group.Name()
	var plain, escaped string
	owner, _, hasOwner := strings.Cut(name, "_")
	var packageLevel bool
	switch prefix := scope.Lookup(owner).(type) {
	case nil:
		packageLevel = true
	case *gotypes.Func:
		_, packageLevel = gogen.CheckFuncEx(prefix.Signature())
	case *gotypes.TypeName:
		_, packageLevel = gogen.CheckOverloadNamed(prefix.Type())
	}
	if !hasOwner || owner != "" && packageLevel {
		plain = XGooPrefix + name
	}
	if strings.Index(name, "__") <= 0 {
		escaped = XGooPrefix + "_" + name
	}
	return firstOverloadMarker(group, plain, escaped, implementations)
}

// methodOverloadMarker considers both separators accepted by gogen.
func methodOverloadMarker(group *gotypes.Func, owner string, implementations []gotypes.Object) *gotypes.Const {
	method := group.Name()
	var plain, escaped string
	if !strings.ContainsRune(owner, '_') {
		plain = XGooPrefix + owner + "_" + method
	}
	encoded := owner + "__" + method
	if decoded, _, _ := strings.Cut(encoded, "__"); decoded == owner {
		escaped = XGooPrefix + "_" + encoded
	}
	return firstOverloadMarker(group, plain, escaped, implementations)
}

// firstOverloadMarker finds the first encoding that supplied the reconstructed
// group. A marker can only reference overloads reconstructed earlier.
func firstOverloadMarker(group *gotypes.Func, plain, escaped string, implementations []gotypes.Object) *gotypes.Const {
	if len(implementations) == 0 || implementations[0] == nil {
		return nil
	}
	scope := group.Pkg().Scope()
	first := implementations[0]
	var implementation, availableAfter string
	missingMethod := false
	if method, ok := first.(*gotypes.Func); ok {
		switch {
		case method == nil:
			// Gogen retains an unresolved method as a typed nil candidate.
			missingMethod = true
		case method.Signature().Recv() != nil:
			implementation = "." + method.Name()
		default:
			implementation = method.Name()
		}
		if method != nil && method.Pkg() == group.Pkg() {
			if marker := OverloadGroupMarker(method); marker != nil {
				availableAfter = marker.Name()
			}
		}
	} else {
		implementation = first.Name()
	}
	indexed := group.Name() + "__"
	if group.Signature().Recv() != nil {
		indexed = "." + indexed
	}
	if escaped < plain {
		plain, escaped = escaped, plain
	}
	for _, name := range [...]string{plain, escaped} {
		if name <= availableAfter {
			continue
		}
		marker, ok := scope.Lookup(name).(*gotypes.Const)
		if !ok || marker.Val().Kind() != constant.String {
			continue
		}
		for i, entry := range strings.Split(constant.StringVal(marker.Val()), ",") {
			if entry == "" {
				entry = indexed + strconv.FormatInt(int64(i), 36)
			}
			if entry == implementation || missingMethod && strings.HasPrefix(entry, ".") {
				return marker
			}
		}
	}
	return nil
}

// overloadReceiverType returns the named type declaring an overload method.
func overloadReceiverType(fun *gotypes.Func) *gotypes.Named {
	typ := gotypes.Unalias(DerefType(gotypes.Unalias(fun.Signature().Recv().Type())))
	named, _ := typ.(*gotypes.Named)
	return named
}

// OverloadSourceLookup restores overload groups with a member cache for one
// checked package.
type OverloadSourceLookup struct {
	pkg     *gotypes.Package
	members map[overloadMemberKey]*gotypes.Func
}

// overloadMemberKey distinguishes pointer and value lookup, while sharing
// entries for pointers to the same receiver constructed at different uses.
type overloadMemberKey struct {
	receiver gotypes.Type
	pointer  bool
	name     string
}

// NewOverloadSourceLookup creates a lookup for a single type-check result.
func NewOverloadSourceLookup(pkg *gotypes.Package) *OverloadSourceLookup {
	return &OverloadSourceLookup{pkg: pkg}
}

// Source returns the written group only if it contains the recorded candidate.
func (r *OverloadSourceLookup) Source(recorded *gotypes.Func, name string, receiver gotypes.Type) *gotypes.Func {
	if receiver == nil && recorded.Name() == name || recorded.Pkg() == nil {
		return nil
	}
	ext, _ := gogen.CheckFuncEx(recorded.Signature())
	switch ext.(type) {
	case *gogen.TyOverloadFunc, *gogen.TyOverloadMethod, *gogen.TyTypeAsParams:
	default:
		return nil
	}
	if recv := recorded.Signature().Recv(); receiver == nil && recv != nil {
		receiver = recv.Type()
	}
	if receiver != nil {
		group := r.member(receiver, name)
		if group != nil && overloadSourceContains(group, recorded) {
			return group
		}
		return nil
	}
	scope := recorded.Pkg().Scope()
	for _, spelling := range [...]string{name, UpperFirstASCII(name)} {
		group, ok := scope.Lookup(spelling).(*gotypes.Func)
		if ok && (group.Exported() || group.Pkg() == r.pkg) && overloadSourceContains(group, recorded) {
			return group
		}
	}
	return nil
}

// member reuses selection results without caching candidate membership.
func (r *OverloadSourceLookup) member(receiver gotypes.Type, name string) *gotypes.Func {
	key := overloadMemberKey{receiver: receiver, name: name}
	if ptr, ok := receiver.(*gotypes.Pointer); ok {
		key.receiver = ptr.Elem()
		key.pointer = true
	}
	if group, ok := r.members[key]; ok {
		return group
	}
	group, _ := lookupXGoMember(receiver, name, r.pkg, false).(*gotypes.Func)
	if r.members == nil {
		r.members = make(map[overloadMemberKey]*gotypes.Func)
	}
	r.members[key] = group
	return group
}

// overloadSourceContains checks the candidate list of the source group.
func overloadSourceContains(group, recorded *gotypes.Func) bool {
	if member, template := ReconstructedMethodImplementation(group); member != nil && !template {
		group = member
	}
	_, candidates := gogen.CheckSigFuncExObjects(group.Signature())
	return slices.Contains(candidates, gotypes.Object(recorded))
}

// lookupXGoMember resolves a value or type selector without calling it.
func lookupXGoMember(receiver gotypes.Type, name string, currentPkg *gotypes.Package, typeSelector bool) gotypes.Object {
	// Go and gogen agree on exact, direct members of a named struct value.
	// Promotion, pointers, and reconstructed static members use gogen.
	if named, ok := gotypes.Unalias(receiver).(*gotypes.Named); !typeSelector && ok && token.IsExported(name) {
		if _, structValue := named.Underlying().(*gotypes.Struct); structValue {
			obj, index, _ := gotypes.LookupFieldOrMethod(named, false, currentPkg, name)
			fun, _ := obj.(*gotypes.Func)
			if obj != nil && len(index) == 1 && (fun == nil || types.StaticMemberObject(fun) == nil) {
				return obj
			}
		}
	}
	recorder := new(memberLookupRecorder)
	pkg := gogen.NewPackage("", "", &gogen.Config{
		Types:      currentPkg,
		Recorder:   recorder,
		NewBuiltin: func(*gogen.Package, *gogen.Config) *gotypes.Package { return nil },
	})
	cb := pkg.CB()
	if typeSelector {
		// Member lookup needs a type operand but does not emit source code.
		cb.InternalStack().Push(&gogen.Element{Val: goast.NewIdent("receiver"), Type: gogen.NewTypeType(receiver)})
	} else {
		cb.Val(gotypes.NewVar(token.NoPos, nil, "receiver", receiver))
	}
	if _, err := cb.Member(name, 0, gogen.MemberFlagMethodAlias); err != nil {
		return nil
	}
	return recorder.object
}

// memberLookupRecorder records the object selected by XGo member lookup.
type memberLookupRecorder struct {
	object gotypes.Object
}

// Member records the selected object, if any.
func (r *memberLookupRecorder) Member(_ goast.Node, obj gotypes.Object) {
	r.object = obj
}

// Call is unused during member lookup.
func (*memberLookupRecorder) Call(goast.Node, gotypes.Object) {}

// OverloadGroupForImplementation returns the reconstructed group reached by
// removing the index suffix of a package function or method.
func OverloadGroupForImplementation(fun *gotypes.Func) *gotypes.Func {
	match := xgoOverloadFuncNameRE.FindStringSubmatch(fun.Name())
	if match == nil || fun.Pkg() == nil {
		return nil
	}
	name := match[1]
	if fun.Signature().Recv() != nil {
		if named := overloadReceiverType(fun); named != nil {
			for method := range named.Origin().Methods() {
				if method.Name() == name {
					ext, _ := gogen.CheckFuncEx(method.Signature())
					if _, ok := ext.(*gogen.TyOverloadMethod); ok {
						return method
					}
					break
				}
			}
		}
		return nil
	}
	group, _ := fun.Pkg().Scope().Lookup(name).(*gotypes.Func)
	if group != nil {
		ext, _ := gogen.CheckFuncEx(group.Signature())
		if _, ok := ext.(*gogen.TyOverloadFunc); ok {
			return group
		}
	}
	return nil
}

// IsXGoOverloadableFunc reports whether the given function is an XGo overloadable
// function with a signature like `func(__xgo_overload_args__ interface{_()})`.
func IsXGoOverloadableFunc(fun *gotypes.Func) bool {
	typ, _ := gogen.CheckSigFuncExObjects(fun.Type().(*gotypes.Signature))
	return typ != nil
}

// IsUnexpandableXGoOverloadableFunc reports whether the given function is a
// Unexpandable-XGo-Overloadable-Func, which is a function that:
//  1. is overloadable: has a signature like `func(__xgo_overload_args__ interface{_()})`
//  2. but not expandable: can not be expanded into overloads
//
// A typical example is method `GetWidget` on spx `Game`.
func IsUnexpandableXGoOverloadableFunc(fun *gotypes.Func) bool {
	sig := fun.Type().(*gotypes.Signature)
	if _, ok := gogen.CheckSigFuncEx(sig); ok { // is `func(__xgo_overload_args__ interface{_()})`
		if t, _ := gogen.CheckSigFuncExObjects(sig); t == nil { // not expandable
			return true
		}
	}
	return false
}

// IsOverloadPlaceholder reports whether fun is a gogen filler for an unused
// overload index.
func IsOverloadPlaceholder(fun *gotypes.Func) bool {
	return fun != nil && fun.Name() == "_" && !fun.Pos().IsValid()
}

// ExpandXGoOverloadableFunc returns the implementation functions of a
// reconstructed callable in resolution order. It follows nested overload
// groups and skips unresolved candidates and unused indices. It returns nil
// if the function does not support overload expansion.
func ExpandXGoOverloadableFunc(fun *gotypes.Func) []*gotypes.Func {
	candidates := ExpandXGoOverloads(fun)
	if candidates == nil {
		return nil
	}
	overloads := make([]*gotypes.Func, 0, len(candidates))
	for _, candidate := range candidates {
		if implementation, ok := candidate.(*gotypes.Func); ok {
			overloads = append(overloads, implementation)
		}
	}
	return overloads
}

// ExpandXGoOverloads returns callable implementations in resolution order,
// including function-valued variables listed by a package overload marker.
// It returns nil for a callable that does not support overload expansion.
func ExpandXGoOverloads(fun *gotypes.Func) []gotypes.Object {
	candidates := OverloadImplementations(fun)
	if candidates == nil {
		return nil
	}
	overloads := make([]gotypes.Object, 0)
	for implementation := range candidates {
		overloads = append(overloads, implementation)
	}
	return overloads
}

// OverloadImplementations yields each implementation with the callable that
// binds its parameters. It returns nil if fun does not expand into candidates.
func OverloadImplementations(fun *gotypes.Func) iter.Seq2[gotypes.Object, *gotypes.Func] {
	typ, objs := gogen.CheckSigFuncExObjects(fun.Signature())
	if typ == nil {
		return nil
	}
	return func(yield func(gotypes.Object, *gotypes.Func) bool) {
		yieldOverloadImplementations(objs, fun, yield)
	}
}

// yieldOverloadImplementations visits candidates in the compiler's order.
func yieldOverloadImplementations(objs []gotypes.Object, binding *gotypes.Func, yield func(gotypes.Object, *gotypes.Func) bool) bool {
	for _, obj := range objs {
		if variable, ok := obj.(*gotypes.Var); ok {
			if _, callable := variable.Type().Underlying().(*gotypes.Signature); callable && !yield(variable, binding) {
				return false
			}
			continue
		}
		overload, ok := obj.(*gotypes.Func)
		if !ok || overload == nil || IsOverloadPlaceholder(overload) {
			continue
		}
		ext, candidates := gogen.CheckSigFuncExObjects(overload.Signature())
		switch ext.(type) {
		case *gogen.TyOverloadFunc, *gogen.TyOverloadMethod:
			if !yieldOverloadImplementations(candidates, binding, yield) {
				return false
			}
		case *gogen.TyTypeAsParams:
			if !yieldOverloadImplementations(candidates, overload, yield) {
				return false
			}
		default:
			if !yield(overload, binding) {
				return false
			}
		}
	}
	return true
}
