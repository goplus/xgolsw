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
	gotypes "go/types"
	"regexp"
	"strings"

	"github.com/goplus/gogen"
)

const (
	XGotPrefix = "XGot_" // XGo template method.
	XGooPrefix = "XGoo_" // XGo overload function/method.
	XGoxPrefix = "XGox_" // XGo type as parameters function/method.
)

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
	parsedName = ToLowerCamelCase(parsedName)
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

// ExpandXGoOverloadableFunc returns the source overloads of a reconstructed
// function. It omits unresolved candidates and unused indices. It returns nil
// if the function does not support overload expansion.
func ExpandXGoOverloadableFunc(fun *gotypes.Func) []*gotypes.Func {
	typ, objs := gogen.CheckSigFuncExObjects(fun.Type().(*gotypes.Signature))
	if typ == nil {
		return nil
	}
	overloads := make([]*gotypes.Func, 0, len(objs))
	for _, obj := range objs {
		if overload, ok := obj.(*gotypes.Func); ok && overload != nil && !IsOverloadPlaceholder(overload) {
			overloads = append(overloads, overload)
		}
	}
	return overloads
}
