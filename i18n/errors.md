# XGo Error Message Classification Summary

This document organizes all diagnostic error messages in the XGo Language Server, categorized by type to improve error message readability and accuracy, helping developers identify issues and enabling AI code fixes.

## 1. Type System Errors

### 1.1 Type Mismatch

**Error Pattern**: `cannot use X (type Y) as type Z`

```go
// Basic type mismatches
"cannot use \"Hi\" (type untyped string) as type int in assignment"
"cannot use []string{} (type []string) as type string in assignment" 
"cannot use [2]string{} (type [2]string) as type string in assignment"
"cannot use map[int]string{} (type map[int]string) as type string in assignment"
"cannot use func(){} (type func()) as type string in assignment"

// Function parameter type mismatches
"cannot use \"Hi\" (type untyped string) as type error in return argument"
"cannot use x (type int) as type string in return argument"
"cannot use byte value as type error in return argument"

// Type mismatches in composite types
"cannot use 3.14 (type untyped float) as type int in slice literal"
"cannot use x (type int) as type string in value of field y"
"cannot use 1 (type untyped int) as type string in map key"
"cannot use \"Go\" + \"+\" (type untyped string) as type int in map value"
```

### 1.2 Type Conversion Errors

**Error Pattern**: `cannot convert X to type Y`

```go
// Numeric range overflow
"cannot convert 1<<127 (untyped int constant 170141183460469231731687303715884105728) to type Int128"
"cannot convert -1 (untyped int constant -1) to type Uint128"

// Invalid conversions
"cannot convert 1<<128 (untyped int constant 340282366920938463463374607431768211456) to type Uint128"
```

### 1.3 Generic Type Errors

**Error Pattern**: `cannot use generic type X` | `got N type parameters`

```go
"cannot use generic type %v without instantiation"
"got 1 type parameter, but receiver base type declares %v"
"got %v arguments but %v type parameters"
```

## 2. Variable & Constant Errors

### 2.1 Undefined Identifiers

**Error Pattern**: `undefined: X`

```go
"undefined: foo"
"undefined: os.UndefinedObject"
"undefined: abc"
"undefined: println1"
```

### 2.2 Redeclaration Errors

**Error Pattern**: `X redeclared in this block`

```go
"a redeclared in this block\n\tprevious declaration at bar.xgo:2:5"
"Point redeclared in this block\n\tprevious declaration at bar.xgo:5:6"
"Id redeclared\n\tbar.xgo:5:2 other declaration of Id"
"%s redeclared in this block\n\tprevious declaration at %v"
```

### 2.3 Assignment Errors

**Error Pattern**: `assignment mismatch` | `cannot use X as value` | `no new variables`

```go
// Assignment count mismatch
"assignment mismatch: 1 variables but bar returns 2 values"
"assignment mismatch: 1 variables but 2 values"
"assignment mismatch: 2 variables but 1 values"

// Special value usage errors
"cannot use _ as value"
"println is not a variable"

// New variable definition errors
"no new variables on left side of :="
```

### 2.4 Constant Errors

**Error Pattern**: `missing value` | `non-constant X`

```go
"missing value in const declaration"
"non-constant array bound n"
```

## 3. Function & Method Errors

### 3.1 Function Call Errors

**Error Pattern**: `not enough arguments in call to X` | `too few/many arguments to return`

```go
// Parameter count errors
"not enough arguments in call to Ls\n\thave ()\n\twant (int)"
"not enough arguments in call to f.Ls\n\thave ()\n\twant (int)"
"not enough arguments in call to set\n\thave (untyped string)\n\twant (name string, v int)"

// Return value errors
"too few arguments to return\n\thave (untyped int)\n\twant (int, error)"
"too many arguments to return\n\thave (untyped int, untyped int, untyped string)\n\twant (int, error)"
"not enough arguments to return\n\thave ()\n\twant (byte)"
```

### 3.2 Method Receiver Errors

**Error Pattern**: `invalid receiver type X`

```go
"invalid receiver type %v (%v is not a defined type)"
"invalid receiver type a (a is a pointer type)"
"invalid receiver type error (error is an interface type)"
"invalid receiver type []byte ([]byte is not a defined type)"
```

### 3.3 Lambda Expression Errors

**Error Pattern**: `arguments in lambda expression` | `cannot use lambda literal` | `lambda unsupport`

```go
// Lambda parameter errors
"too few arguments in lambda expression\n\thave ()\n\twant (int, int)"
"too many arguments in lambda expression\n\thave (x, y, z)\n\twant (int, int)"

// Lambda type usage errors
"cannot use lambda literal as type int in field value to Plot"
"cannot use lambda literal as type func() in argument to foo"
"cannot use lambda literal as type func() int in assignment to foo"
"lambda unsupport multiple assignment"
```

### 3.4 Special Function Errors

**Error Pattern**: `func init must` | `use of builtin X not in function call`

```go
"func init must have no arguments and no return values"
"use of builtin %s not in function call"
```

## 4. Control Flow Errors

### 4.1 Switch Statement Errors

**Error Pattern**: `duplicate case X` | `multiple defaults`

```go
// Duplicate cases
"duplicate case %s in switch\n\tprevious case at %v"
"duplicate case %s (value %#v) in switch\n\tprevious case at %v"
"duplicate case int in type switch\n\tprevious case at %v"

// Multiple defaults
"multiple defaults in switch (first at %v)"
"multiple defaults in type switch (first at %v)"
"multiple nil cases in type switch (first at %v)"
```

### 4.2 Branch Statement Errors

**Error Pattern**: `fallthrough statement out of place` | `label X is not defined` | `label X already defined`

```go
"fallthrough statement out of place"
"label %v is not defined"
"label %v already defined at %v\n%v: label %v defined and not used"
```

### 4.3 Loop Errors

**Error Pattern**: `cannot assign type X to Y in range`

```go
"cannot assign type string to a (type int) in range"
```

## 5. Data Structure Errors

### 5.1 Array Errors

**Error Pattern**: `array index X out of bounds` | `cannot use X as index`

```go
// Array index errors
"array index %d out of bounds [0:%d]"
"array index %d (value %d) out of bounds [0:%d]"
"cannot use a as index which must be non-negative integer constant"

// Array literal errors
"cannot use a+\"!\" (type string) as type int in array literal"
"cannot use a (type string) as type int in array literal"
```

### 5.2 Slice Errors

**Error Pattern**: `cannot slice X` | `invalid operation X (3-index slice)`

```go
"cannot slice a (type *byte)"
"cannot slice a (type bool)"
"invalid operation a[1:2:5] (3-index slice of string)"
"cannot use a (type string) as type int in slice literal"
```

### 5.3 Map Errors

**Error Pattern**: `missing key in map literal` | `invalid map literal` | `invalid composite literal type`

```go
"missing key in map literal"
"invalid map literal"
"invalid composite literal type %v"
```

### 5.4 Struct Errors

**Error Pattern**: `too many/few values in struct` | `X undefined (type Y has no field or method X)`

```go
"too many values in struct{x int; y string}{...}"
"too few values in struct{x int; y string}{...}"
"z undefined (type struct{x int; y string} has no field or method z)"
```

## 6. Pointer & Memory Operation Errors

### 6.1 Pointer Operation Errors

**Error Pattern**: `invalid indirect of X` | `cannot assign to X (immutable)`

```go
"invalid indirect of a (type string)"
"cannot assign to a[1] (strings are immutable)"
```

### 6.2 Member Access Errors

**Error Pattern**: `X undefined (type Y has no field or method X)`

```go
"a.x undefined (type string has no field or method x)"
"a.x undefined (type aaa has no field or method x)"
"[].string undefined (type []interface{} has no field or method string)"
```

## 7. Package & Import Errors

### 7.1 Package Import Errors

**Error Pattern**: `package X is not in std` | `no required module provides package X`

```go
"package fmt2 is not in std (%v)"
"no required module provides package github.com/goplus/xgo/fmt2; to add it:\n\tgo get github.com/goplus/xgo/fmt2"
```

### 7.2 Symbol Access Errors

**Error Pattern**: `confliction: X declared both in` | `cannot refer to unexported name` | `X is not a type`

```go
"confliction: NewEncoding declared both in \"encoding/base64\" and \"encoding/base32\""
"cannot refer to unexported name os.undefined"
"cannot refer to unexported name fmt.println"
"undefined: os.UndefinedObject"
"testing.Verbose is not a type"
"%s is not a type"
```

## 8. XGo-Specific Errors

### 8.1 Environment Variable Operation Errors

**Error Pattern**: `operator $X undefined`

```go
"operator $%v undefined"
"operator $name undefined"
"operator $id undefined"
```

### 8.2 String Template Errors

**Error Pattern**: `X.string undefined` | `X.stringY`

```go
"[].string undefined (type []interface{} has no field or method string)"
"%s.string%s"
```

### 8.3 Overload Function Errors

**Error Pattern**: `invalid func/recv/method/overload X` | `unknown func X`

```go
"invalid func (foo).mulInt"
"invalid recv type *foo"
"invalid recv type (foo2)"
"invalid method mulInt"
"invalid recv type (**foo)"
"unknown func (\"ok\")"
"invalid method func(){}"
"invalid overload operator ++"
```

### 8.4 Send Statement Errors

**Error Pattern**: `can't send multiple values to a channel`

```go
"can't send multiple values to a channel"
```

## 9. Compilation & Syntax Errors

### 9.1 Compilation Errors

**Error Pattern**: `compile X: Y` | `compileExpr failed` | `unreachable`

```go
"compile `%v`: %v"
"compile `printf(\"%+v\\n\", int32)`: unreachable"
"compileExpr failed: unknown - %T"
"compileExprLHS failed: unknown - %T"
```

### 9.2 Type Inference Errors

**Error Pattern**: `toType unexpected` | `X is not a type` | `expected X, found Y`

```go
"toType unexpected: %T"
"%s.%s is not a type"
"expected 'IDENT', found '}'"
"expected operand, found '=>'"
"expected statement, found ','"
```

## 10. Runtime & Operation Errors

### 10.1 Invalid Operations

**Error Pattern**: `invalid operation X` | `type Y does not support Z`

```go
"invalid operation: a[1] (type bool does not support indexing)"
"invalid operation a[1:2:5] (3-index slice of string)"
```

### 10.2 Type Checking Errors

**Error Pattern**: `X not type` | `inconsistent matrix column count`

```go
"%v not type"
"inconsistent matrix column count: got %v, want %v"
```

