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

package types

import (
	gotypes "go/types"
	"sync"

	"github.com/goplus/gogen"
	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
)

// Info is an enhanced version of [typesutil.Info] for XGo projects. It embeds
// [typesutil.Info] and adds additional functionality and context.
type Info struct {
	typesutil.Info

	// Pkg is the package associated with this type information.
	Pkg *gotypes.Package

	// Builtins holds the compiler's builtin declarations, including operators.
	// The package is shared by snapshots and must be treated as read-only.
	Builtins *gotypes.Package

	// FailedImportPaths maps failed import positions to their canonical package paths.
	FailedImportPaths map[token.Pos]string

	// ClassTypes maps source classfiles to their compiler-generated types.
	ClassTypes map[*ast.File]*gotypes.Named

	// ImplicitReceivers records the receiver type of unqualified reconstructed methods in classfiles.
	ImplicitReceivers map[*ast.Ident]gotypes.Type

	// SelectedOverloads retains call selections missing from Uses, including
	// selections reported as types for parenthesized callees.
	SelectedOverloads map[*ast.Ident]OverloadSelection

	// KeywordArgs records the compiler's lowering, keyed by the last keyword.
	// Copies of a source call share their keyword nodes.
	KeywordArgs map[*ast.KwargExpr]KeywordLowering

	// ObjToDef is a reverse mapping for O(1) object-to-identifier lookup.
	// For identifiers that do not denote objects, the object is nil and
	// they are excluded from this mapping.
	ObjToDef map[gotypes.Object]*ast.Ident

	// FuncDecorators distinguishes decorator calls from ordinary calls, which
	// have different argument expansion rules.
	FuncDecorators map[*ast.CallExpr]bool

	// ImplicitCallTypes supplies result types for source expressions that call
	// a function through an alias. Types retains the compiler's original
	// records, including the callable's operand mode.
	ImplicitCallTypes map[ast.Expr]gotypes.Type

	// callInference retains the instantiation context for this snapshot.
	callInference func() *gogen.Package
}

// NewInfo prepares the recorder maps and inference context for a package check.
func NewInfo(pkg *gotypes.Package) *Info {
	return &Info{
		Info: typesutil.Info{
			Types:      make(map[ast.Expr]gotypes.TypeAndValue),
			Defs:       make(map[*ast.Ident]gotypes.Object),
			Uses:       make(map[*ast.Ident]gotypes.Object),
			Overloads:  make(map[*ast.Ident]gotypes.Object),
			Selections: make(map[*ast.SelectorExpr]*gotypes.Selection),
			Implicits:  make(map[ast.Node]gotypes.Object),
			Scopes:     make(map[ast.Node]*gotypes.Scope),
		},
		Pkg:            pkg,
		FuncDecorators: make(map[*ast.CallExpr]bool),
		callInference:  sync.OnceValue(newCallInferencePackage),
	}
}

// KeywordLowering describes checked parts of a keyword list.
type KeywordLowering struct {
	// Arg is the synthesized value argument. It is nil for an unfinished call.
	Arg ast.Expr

	// Factory is the initial call in an interface builder chain.
	Factory *ast.CallExpr

	// BuilderSteps maps each keyword's source position to its checked call.
	BuilderSteps map[token.Pos]*ast.CallExpr
}

// OverloadSelection describes the concrete target and any distinct
// type-as-parameters wrapper selected for an overload call.
type OverloadSelection struct {
	Implementation gotypes.Object
	Binding        *gotypes.Func
}

// InferCallSignature infers type arguments for a call against a checked
// snapshot. Each check has its own operands and Go checker. Only gogen's
// configuration and the concurrency-safe Go instantiation context are shared.
func (i *Info) InferCallSignature(fn *gogen.Element, sig *gotypes.Signature, args []*gogen.Element) (gotypes.Type, error) {
	var pkg *gogen.Package
	if i != nil && i.callInference != nil {
		pkg = i.callInference()
	} else {
		pkg = newCallInferencePackage()
	}
	return gogen.InferFunc(pkg, fn, sig, nil, args, 0)
}

// newCallInferencePackage creates a compiler context without loading builtins.
func newCallInferencePackage() *gogen.Package {
	return gogen.NewPackage("", "", &gogen.Config{
		NewBuiltin: func(*gogen.Package, *gogen.Config) *gotypes.Package { return nil },
	})
}

// TypeOf returns the source expression's type, including an implicit call's
// result or an inline overload's signature. It returns nil when the type is unknown.
func (i *Info) TypeOf(expr ast.Expr) gotypes.Type {
	if expr == nil {
		return nil
	}
	if typ := i.ImplicitCallTypes[expr]; typ != nil {
		return typ
	}
	typ := i.Info.TypeOf(expr)
	if typ == nil {
		if literal, ok := expr.(*ast.FuncLit); ok {
			if impl := i.Implicits[literal]; impl != nil {
				return impl.Type()
			}
		}
	}
	if _, signature := typ.(*gotypes.Signature); !signature && gogen.IsFunc(typ) {
		// Gogen records a pending inference type for partially instantiated
		// functions. That type cannot be used with go/types operations.
		return nil
	}
	return typ
}

// RefIdentsFor returns all identifiers where the given object is referenced,
// including uses through different generic instantiations and type switch cases.
func (i *Info) RefIdentsFor(obj gotypes.Object) []*ast.Ident {
	if obj == nil {
		return nil
	}
	obj = i.ObjectDeclaration(obj)
	def := i.ObjToDef[obj]
	var idents []*ast.Ident
	for ident, used := range i.Uses {
		if overload := i.Overloads[ident]; overload != nil {
			used = overload
		}
		// Range expressions reuse the declaration identifier in generated uses.
		if ident != def && i.ObjectDeclaration(used) == obj {
			idents = append(idents, ident)
		}
	}
	return idents
}

// SourceObjectOf returns the object named by ident before overload selection.
func (i *Info) SourceObjectOf(ident *ast.Ident) gotypes.Object {
	if overload := i.Overloads[ident]; overload != nil {
		return overload
	}
	return i.ObjectOf(ident)
}

// ObjectDeclaration returns the object at the shared declaration of obj,
// including static members, generic members, and type switch variables.
// Objects without a recorded declaration, including nil, retain their origin.
func (i *Info) ObjectDeclaration(obj gotypes.Object) gotypes.Object {
	if obj == nil {
		return nil
	}
	if fun, ok := obj.(*gotypes.Func); ok {
		if member := StaticMemberObject(fun); member != nil {
			obj = member
		}
	}
	obj = ObjectOrigin(obj)
	if ident := i.ObjToDef[obj]; ident != nil {
		if declaration := i.Defs[ident]; declaration != nil {
			return declaration
		}
	}
	return obj
}

// ObjectOrigin returns the declaration of a field or method before generic
// instantiation. Other objects, including nil, are returned unchanged.
func ObjectOrigin(obj gotypes.Object) gotypes.Object {
	switch obj := obj.(type) {
	case *gotypes.Var:
		return obj.Origin()
	case *gotypes.Func:
		return obj.Origin()
	default:
		return obj
	}
}

// StaticMemberObject returns the Go declaration implemented by a reconstructed
// static member, or nil for ordinary functions and other extensions.
func StaticMemberObject(fun *gotypes.Func) gotypes.Object {
	ext, _ := gogen.CheckFuncEx(fun.Signature())
	if member, ok := ext.(*gogen.TyStaticMember); ok {
		return member.Obj()
	}
	return nil
}
