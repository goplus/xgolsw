package util

import (
	"go/types"
	"strings"
)

// ToPtr returns a pointer to the value.
func ToPtr[T any](v T) *T {
	return &v
}

// FromPtr returns the value from a pointer. It returns the zero value of type T
// if the pointer is nil.
func FromPtr[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

const (
	GoptPrefix = "Gopt_"      // Go+ template method
	GopoPrefix = "Gopo_"      // Go+ overload function/method
	GopxPrefix = "Gopx_"      // Go+ type as parameters function/method
	GopPackage = "GopPackage" // Indicates a Go+ package
)

// IsGopPackage checks if the given package is a Go+ package.
func IsGopPackage(pkg *types.Package) bool {
	scope := pkg.Scope()
	if scope == nil {
		return false
	}
	obj := scope.Lookup(GopPackage)
	if obj == nil {
		return false
	}
	return obj.Type() == types.Typ[types.UntypedBool]
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
