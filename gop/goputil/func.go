/*
 * Copyright (c) 2025 The GoPlus Authors (goplus.org). All rights reserved.
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

package goputil

import (
	"go/types"
	"regexp"
	"strings"

	"github.com/goplus/gogen"
)

const (
	GoptPrefix = "Gopt_" // Go+ template method.
	GopoPrefix = "Gopo_" // Go+ overload function/method.
	GopxPrefix = "Gopx_" // Go+ type as parameters function/method.
)

// IsGoptMethodName reports whether the given name is a Go+ template method name.
func IsGoptMethodName(name string) bool {
	_, _, ok := SplitGoptMethodName(name, false)
	return ok
}

// SplitGoptMethodName splits a Go+ template method name into receiver type
// name and method name.
func SplitGoptMethodName(name string, trimGopx bool) (recvTypeName string, methodName string, ok bool) {
	if !strings.HasPrefix(name, GoptPrefix) {
		return "", "", false
	}
	recvTypeName, methodName, ok = strings.Cut(name[len(GoptPrefix):], "_")
	if !ok {
		return "", "", false
	}
	if trimGopx {
		if funcName, ok := SplitGopxFuncName(methodName); ok {
			methodName = funcName
		}
	}
	return
}

// SplitGopxFuncName splits a Go+ type as parameters function name into the
// function name.
func SplitGopxFuncName(name string) (funcName string, ok bool) {
	if !strings.HasPrefix(name, GopxPrefix) {
		return "", false
	}
	funcName = strings.TrimPrefix(name, GopxPrefix)
	ok = true
	return
}

// ParseGopFuncName parses the Go+ overloaded function name.
func ParseGopFuncName(name string) (parsedName string, overloadID *string) {
	parsedName = name
	if matches := gopOverloadFuncNameRE.FindStringSubmatch(parsedName); len(matches) == 3 {
		parsedName = matches[1]
		overloadID = &matches[2]
	}
	parsedName = ToLowerCamelCase(parsedName)
	return
}

// gopOverloadFuncNameRE is the regular expression of the Go+ overloaded
// function name.
var gopOverloadFuncNameRE = regexp.MustCompile(`^(.+)__([0-9a-z])$`)

// IsGopOverloadedFuncName reports whether the given function name is a Go+
// overloaded function name.
func IsGopOverloadedFuncName(name string) bool {
	return gopOverloadFuncNameRE.MatchString(name)
}

// IsGopOverloadableFunc reports whether the given function is a Go+ overloadable
// function with a signature like `func(__gop_overload_args__ interface{_()})`.
func IsGopOverloadableFunc(fun *types.Func) bool {
	typ, _ := gogen.CheckSigFuncExObjects(fun.Type().(*types.Signature))
	return typ != nil
}

// IsUnexpandableGopOverloadableFunc reports whether the given function is a
// Unexpandable-Gop-Overloadable-Func, which is a function that:
//  1. is overloadable: has a signature like `func(__gop_overload_args__ interface{_()})`
//  2. but not expandable: can not be expanded into overloads
//
// A typical example is method `GetWidget` on spx `Game`.
func IsUnexpandableGopOverloadableFunc(fun *types.Func) bool {
	sig := fun.Type().(*types.Signature)
	if _, ok := gogen.CheckSigFuncEx(sig); ok { // is `func(__gop_overload_args__ interface{_()})`
		if t, _ := gogen.CheckSigFuncExObjects(sig); t == nil { // not expandable
			return true
		}
	}
	return false
}

// ExpandGopOverloadableFunc expands the given Go+ function with a signature
// like `func(__gop_overload_args__ interface{_()})` to all its overloads. It
// returns nil if the function is not qualified for overload expansion.
func ExpandGopOverloadableFunc(fun *types.Func) []*types.Func {
	typ, objs := gogen.CheckSigFuncExObjects(fun.Type().(*types.Signature))
	if typ == nil {
		return nil
	}
	overloads := make([]*types.Func, 0, len(objs))
	for _, obj := range objs {
		overloads = append(overloads, obj.(*types.Func))
	}
	return overloads
}
