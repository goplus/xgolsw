/*
 * Copyright (c) 2026 The XGo Authors (xgo.dev). All rights reserved.
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
	gotoken "go/token"
	gotypes "go/types"
	"iter"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/goplus/gogen"
	"github.com/goplus/xgolsw/xgo/types"
)

// CallValueTypeMatches reports whether a known operand can be passed to a parameter.
func CallValueTypeMatches(info *types.Info, value gotypes.TypeAndValue, want gotypes.Type) bool {
	return callValueTypeMatches(info, value, want, nil)
}

// callValueTypeMatches checks assignability for a known operand. The active
// initializers prevent cycles when checking incomplete declarations.
func callValueTypeMatches(info *types.Info, value gotypes.TypeAndValue, want gotypes.Type, initializers []gotypes.Object) bool {
	actual := value.Type
	if !IsValidType(actual) || !IsValidType(want) || actual.Underlying() == nil || want.Underlying() == nil {
		return false
	}
	if _, ok := want.(*gotypes.TypeParam); ok {
		return IsTypesCompatible(actual, want)
	}
	// Gogen treats an integral untyped float as an integer before looking
	// up an implicit initializer or inferring its type arguments.
	if actual == gotypes.Typ[gotypes.UntypedFloat] && value.Value != nil && constant.ToInt(value.Value).Kind() == constant.Int {
		actual = gotypes.Typ[gotypes.UntypedInt]
		value.Type = actual
	}
	if !gotypes.AssignableTo(actual, want) {
		if CanCastEmbeddedPointer(actual, want) {
			return true
		}
		init := TypeInitializer(want)
		if init == nil || slices.Contains(initializers, init) || !initializerConstantFits(init, value.Value) {
			return false
		}
		return callInitializerMatches(info, init, value, append(initializers, init))
	}

	from, ok := actual.(*gotypes.Basic)
	if value.Value == nil || !ok || from.Info()&gotypes.IsUntyped == 0 {
		return true
	}
	to, ok := want.Underlying().(*gotypes.Basic)
	if !ok || to.Info()&gotypes.IsNumeric == 0 {
		return true
	}
	return callConstantFits(value.Value, from, to)
}

// callInitializerMatches checks the initializer against a single value argument.
func callInitializerMatches(info *types.Info, init gotypes.Object, value gotypes.TypeAndValue, initializers []gotypes.Object) bool {
	for sig := range callInitializerSignatures(init) {
		if singleValueCallSignature(info, sig, value, initializers) != nil {
			return true
		}
	}
	return false
}

// callInitializerSignatures yields the initializer signatures callable without
// explicit type arguments in compiler order.
func callInitializerSignatures(init gotypes.Object) iter.Seq[*gotypes.Signature] {
	return func(yield func(*gotypes.Signature) bool) {
		if fun, ok := init.(*gotypes.Func); ok {
			if implementations := OverloadImplementations(fun); implementations != nil {
				for implementation, binding := range implementations {
					sig := implementation.Type().Underlying().(*gotypes.Signature)
					ext, _ := gogen.CheckFuncEx(binding.Signature())
					if _, typeAsParams := ext.(*gogen.TyTypeAsParams); typeAsParams && sig.TypeParams().Len() != 0 {
						continue
					}
					if !yield(sig) {
						return
					}
				}
				return
			}
		}
		yield(init.Type().Underlying().(*gotypes.Signature))
	}
}

// singleValueCallSignature binds a callable that accepts the given value as
// its only argument. The result includes any inferred type arguments.
func singleValueCallSignature(info *types.Info, sig *gotypes.Signature, value gotypes.TypeAndValue, initializers []gotypes.Object) *gotypes.Signature {
	if sig.Params().Len() == 0 || RequiredParamCount(info, sig, sig.Params(), 0) > 1 {
		return nil
	}
	sig = inferValueCallSignature(info, sig, value)
	if sig == nil || !callValueTypeMatches(info, value, firstCallValueParamType(sig), initializers) {
		return nil
	}
	return sig
}

// inferValueCallSignature binds a signature using the types of its supplied
// operands. An operand with an unknown type contributes no inference evidence.
func inferValueCallSignature(info *types.Info, sig *gotypes.Signature, values ...gotypes.TypeAndValue) *gotypes.Signature {
	if sig.TypeParams().Len() == 0 {
		return sig
	}
	ident := goast.NewIdent("_")
	fn := &gogen.Element{Val: ident, Type: sig}
	args := make([]*gogen.Element, len(values))
	for i, value := range values {
		typ := value.Type
		if !IsValidType(typ) {
			typ = gotypes.Typ[gotypes.UntypedNil]
		}
		args[i] = &gogen.Element{Val: ident, Type: typ, CVal: value.Value}
	}
	instance, err := info.InferCallSignature(fn, sig, args)
	if err != nil {
		return nil
	}
	return instance.(*gotypes.Signature)
}

// firstCallValueParamType returns the type that accepts the first value passed
// to a signature, including the element type of a single variadic parameter.
func firstCallValueParamType(sig *gotypes.Signature) gotypes.Type {
	typ := sig.Params().At(0).Type()
	if sig.Variadic() && sig.Params().Len() == 1 {
		return variadicValueType(typ)
	}
	return typ
}

// initializerConstantFits checks the optional integer bounds on a T_Init conversion.
func initializerConstantFits(init gotypes.Object, value constant.Value) bool {
	if value == nil || value.Kind() != constant.Int && value.Kind() != constant.Float {
		return true
	}
	value = constant.ToInt(value)
	if value.Kind() != constant.Int {
		return true
	}
	name := strings.TrimSuffix(init.Name(), "_Init")
	scope := init.Pkg().Scope()
	for _, bound := range [...]struct {
		suffix string
		op     gotoken.Token
	}{{"_Min", gotoken.LSS}, {"_Max", gotoken.GTR}} {
		if c, ok := scope.Lookup(name + bound.suffix).(*gotypes.Const); ok && c.Val() != nil {
			if limit := constant.ToInt(c.Val()); limit.Kind() == constant.Int && constant.Compare(value, bound.op, limit) {
				return false
			}
		}
	}
	return true
}

// constantCheckPackage supplies gogen's range check for two basic types.
// AssignableConv does not load types or mutate the package on this path.
var constantCheckPackage = sync.OnceValue(func() *gogen.Package {
	return gogen.NewPackage("", "", &gogen.Config{
		NewBuiltin: func(*gogen.Package, *gogen.Config) *gotypes.Package { return nil },
	})
})

// callConstantFits checks the value of an untyped constant against a numeric parameter.
func callConstantFits(value constant.Value, from, to *gotypes.Basic) bool {
	if to.Info()&gotypes.IsInteger != 0 {
		// Gogen's integer range check requires an ordered constant.
		if from.Info()&gotypes.IsComplex != 0 {
			return false
		}
		return gogen.AssignableConv(constantCheckPackage(), from, to, &gogen.Element{Type: from, CVal: value})
	}
	if to.Info()&gotypes.IsFloat != 0 && from.Info()&gotypes.IsComplex != 0 {
		return false
	}
	// Floating-point overflow is rejected when Go checks the generated call.
	switch to.Kind() {
	case gotypes.Float32, gotypes.Complex64:
		realPart, _ := constant.Float32Val(constant.Real(value))
		imagPart, _ := constant.Float32Val(constant.Imag(value))
		return !math.IsInf(float64(realPart), 0) && !math.IsInf(float64(imagPart), 0)
	case gotypes.Float64, gotypes.Complex128:
		realPart, _ := constant.Float64Val(constant.Real(value))
		imagPart, _ := constant.Float64Val(constant.Imag(value))
		return !math.IsInf(realPart, 0) && !math.IsInf(imagPart, 0)
	default:
		return true
	}
}
