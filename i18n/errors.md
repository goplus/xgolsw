# XGo 错误信息分类汇总

本文档整理了 XGo Language Server 中的所有诊断错误信息，按类型分类，以提高错误信息的可读性和准确性，便于开发者发现问题和 AI 修复代码。

## 1. 类型系统错误 (Type System Errors)

### 1.1 类型不匹配 (Type Mismatch)

**错误代码模式**：`cannot use X (type Y) as type Z`

```go
// 基本类型不匹配
"cannot use \"Hi\" (type untyped string) as type int in assignment"
"cannot use []string{} (type []string) as type string in assignment" 
"cannot use [2]string{} (type [2]string) as type string in assignment"
"cannot use map[int]string{} (type map[int]string) as type string in assignment"
"cannot use func(){} (type func()) as type string in assignment"

// 函数参数类型不匹配
"cannot use \"Hi\" (type untyped string) as type error in return argument"
"cannot use x (type int) as type string in return argument"
"cannot use byte value as type error in return argument"

// 复合类型中的类型不匹配
"cannot use 3.14 (type untyped float) as type int in slice literal"
"cannot use x (type int) as type string in value of field y"
"cannot use 1 (type untyped int) as type string in map key"
"cannot use \"Go\" + \"+\" (type untyped string) as type int in map value"
```

### 1.2 类型转换错误 (Type Conversion Errors)

**错误代码模式**：`cannot convert X to type Y`

```go
// 数值范围溢出
"cannot convert 1<<127 (untyped int constant 170141183460469231731687303715884105728) to type Int128"
"cannot convert -1 (untyped int constant -1) to type Uint128"

// 无效转换
"cannot convert 1<<128 (untyped int constant 340282366920938463463374607431768211456) to type Uint128"
```

### 1.3 泛型类型错误 (Generic Type Errors)

**错误代码模式**：`cannot use generic type X` | `got N type parameters`

```go
"cannot use generic type %v without instantiation"
"got 1 type parameter, but receiver base type declares %v"
"got %v arguments but %v type parameters"
```

## 2. 变量和常量错误 (Variable & Constant Errors)

### 2.1 未定义标识符 (Undefined Identifiers)

**错误代码模式**：`undefined: X`

```go
"undefined: foo"
"undefined: os.UndefinedObject"
"undefined: abc"
"undefined: println1"
```

### 2.2 重复声明 (Redeclaration Errors)

**错误代码模式**：`X redeclared in this block`

```go
"a redeclared in this block\n\tprevious declaration at bar.xgo:2:5"
"Point redeclared in this block\n\tprevious declaration at bar.xgo:5:6"
"Id redeclared\n\tbar.xgo:5:2 other declaration of Id"
"%s redeclared in this block\n\tprevious declaration at %v"
```

### 2.3 赋值错误 (Assignment Errors)

**错误代码模式**：`assignment mismatch` | `cannot use X as value` | `no new variables`

```go
// 赋值数量不匹配
"assignment mismatch: 1 variables but bar returns 2 values"
"assignment mismatch: 1 variables but 2 values"
"assignment mismatch: 2 variables but 1 values"

// 特殊值使用错误
"cannot use _ as value"
"println is not a variable"

// 新变量定义错误
"no new variables on left side of :="
```

### 2.4 常量错误 (Constant Errors)

**错误代码模式**：`missing value` | `non-constant X`

```go
"missing value in const declaration"
"non-constant array bound n"
```

## 3. 函数和方法错误 (Function & Method Errors)

### 3.1 函数调用错误 (Function Call Errors)

**错误代码模式**：`not enough arguments in call to X` | `too few/many arguments to return`

```go
// 参数数量错误
"not enough arguments in call to Ls\n\thave ()\n\twant (int)"
"not enough arguments in call to f.Ls\n\thave ()\n\twant (int)"
"not enough arguments in call to set\n\thave (untyped string)\n\twant (name string, v int)"

// 返回值错误
"too few arguments to return\n\thave (untyped int)\n\twant (int, error)"
"too many arguments to return\n\thave (untyped int, untyped int, untyped string)\n\twant (int, error)"
"not enough arguments to return\n\thave ()\n\twant (byte)"
```

### 3.2 方法接收器错误 (Method Receiver Errors)

**错误代码模式**：`invalid receiver type X`

```go
"invalid receiver type %v (%v is not a defined type)"
"invalid receiver type a (a is a pointer type)"
"invalid receiver type error (error is an interface type)"
"invalid receiver type []byte ([]byte is not a defined type)"
```

### 3.3 Lambda 表达式错误 (Lambda Expression Errors)

**错误代码模式**：`arguments in lambda expression` | `cannot use lambda literal` | `lambda unsupport`

```go
// Lambda 参数错误
"too few arguments in lambda expression\n\thave ()\n\twant (int, int)"
"too many arguments in lambda expression\n\thave (x, y, z)\n\twant (int, int)"

// Lambda 类型使用错误
"cannot use lambda literal as type int in field value to Plot"
"cannot use lambda literal as type func() in argument to foo"
"cannot use lambda literal as type func() int in assignment to foo"
"lambda unsupport multiple assignment"
```

### 3.4 特殊函数错误 (Special Function Errors)

**错误代码模式**：`func init must` | `use of builtin X not in function call`

```go
"func init must have no arguments and no return values"
"use of builtin %s not in function call"
```

## 4. 控制流错误 (Control Flow Errors)

### 4.1 Switch 语句错误 (Switch Statement Errors)

**错误代码模式**：`duplicate case X` | `multiple defaults`

```go
// 重复 case
"duplicate case %s in switch\n\tprevious case at %v"
"duplicate case %s (value %#v) in switch\n\tprevious case at %v"
"duplicate case int in type switch\n\tprevious case at %v"

// 多个 default
"multiple defaults in switch (first at %v)"
"multiple defaults in type switch (first at %v)"
"multiple nil cases in type switch (first at %v)"
```

### 4.2 分支语句错误 (Branch Statement Errors)

**错误代码模式**：`fallthrough statement out of place` | `label X is not defined` | `label X already defined`

```go
"fallthrough statement out of place"
"label %v is not defined"
"label %v already defined at %v\n%v: label %v defined and not used"
```

### 4.3 循环错误 (Loop Errors)

**错误代码模式**：`cannot assign type X to Y in range`

```go
"cannot assign type string to a (type int) in range"
```

## 5. 数据结构错误 (Data Structure Errors)

### 5.1 数组错误 (Array Errors)

**错误代码模式**：`array index X out of bounds` | `cannot use X as index`

```go
// 数组索引错误
"array index %d out of bounds [0:%d]"
"array index %d (value %d) out of bounds [0:%d]"
"cannot use a as index which must be non-negative integer constant"

// 数组字面量错误
"cannot use a+\"!\" (type string) as type int in array literal"
"cannot use a (type string) as type int in array literal"
```

### 5.2 切片错误 (Slice Errors)

**错误代码模式**：`cannot slice X` | `invalid operation X (3-index slice)`

```go
"cannot slice a (type *byte)"
"cannot slice a (type bool)"
"invalid operation a[1:2:5] (3-index slice of string)"
"cannot use a (type string) as type int in slice literal"
```

### 5.3 映射错误 (Map Errors)

**错误代码模式**：`missing key in map literal` | `invalid map literal` | `invalid composite literal type`

```go
"missing key in map literal"
"invalid map literal"
"invalid composite literal type %v"
```

### 5.4 结构体错误 (Struct Errors)

**错误代码模式**：`too many/few values in struct` | `X undefined (type Y has no field or method X)`

```go
"too many values in struct{x int; y string}{...}"
"too few values in struct{x int; y string}{...}"
"z undefined (type struct{x int; y string} has no field or method z)"
```

## 6. 指针和内存操作错误 (Pointer & Memory Errors)

### 6.1 指针操作错误 (Pointer Operation Errors)

**错误代码模式**：`invalid indirect of X` | `cannot assign to X (immutable)`

```go
"invalid indirect of a (type string)"
"cannot assign to a[1] (strings are immutable)"
```

### 6.2 成员访问错误 (Member Access Errors)

**错误代码模式**：`X undefined (type Y has no field or method X)`

```go
"a.x undefined (type string has no field or method x)"
"a.x undefined (type aaa has no field or method x)"
"[].string undefined (type []interface{} has no field or method string)"
```

## 7. 包和导入错误 (Package & Import Errors)

### 7.1 包导入错误 (Package Import Errors)

**错误代码模式**：`package X is not in std` | `no required module provides package X`

```go
"package fmt2 is not in std (%v)"
"no required module provides package github.com/goplus/xgo/fmt2; to add it:\n\tgo get github.com/goplus/xgo/fmt2"
```

### 7.2 符号访问错误 (Symbol Access Errors)

**错误代码模式**：`confliction: X declared both in` | `cannot refer to unexported name` | `X is not a type`

```go
"confliction: NewEncoding declared both in \"encoding/base64\" and \"encoding/base32\""
"cannot refer to unexported name os.undefined"
"cannot refer to unexported name fmt.println"
"undefined: os.UndefinedObject"
"testing.Verbose is not a type"
"%s is not a type"
```

## 8. XGo 特有错误 (XGo-Specific Errors)

### 8.1 环境变量操作错误 (Environment Variable Errors)

**错误代码模式**：`operator $X undefined`

```go
"operator $%v undefined"
"operator $name undefined"
"operator $id undefined"
```

### 8.2 字符串模板错误 (String Template Errors)

**错误代码模式**：`X.string undefined` | `X.stringY`

```go
"[].string undefined (type []interface{} has no field or method string)"
"%s.string%s"
```

### 8.3 重载函数错误 (Overload Function Errors)

**错误代码模式**：`invalid func/recv/method/overload X` | `unknown func X`

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

### 8.4 发送语句错误 (Send Statement Errors)

**错误代码模式**：`can't send multiple values to a channel`

```go
"can't send multiple values to a channel"
```

## 9. 编译和语法错误 (Compilation & Syntax Errors)

### 9.1 编译错误 (Compilation Errors)

**错误代码模式**：`compile X: Y` | `compileExpr failed` | `unreachable`

```go
"compile `%v`: %v"
"compile `printf(\"%+v\\n\", int32)`: unreachable"
"compileExpr failed: unknown - %T"
"compileExprLHS failed: unknown - %T"
```

### 9.2 类型推导错误 (Type Inference Errors)

**错误代码模式**：`toType unexpected` | `X is not a type` | `expected X, found Y`

```go
"toType unexpected: %T"
"%s.%s is not a type"
"expected 'IDENT', found '}'"
"expected operand, found '=>'"
"expected statement, found ','"
```

## 10. 运行时和操作错误 (Runtime & Operation Errors)

### 10.1 无效操作 (Invalid Operations)

**错误代码模式**：`invalid operation X` | `type Y does not support Z`

```go
"invalid operation: a[1] (type bool does not support indexing)"
"invalid operation a[1:2:5] (3-index slice of string)"
```

### 10.2 类型检查错误 (Type Checking Errors)

**错误代码模式**：`X not type` | `inconsistent matrix column count`

```go
"%v not type"
"inconsistent matrix column count: got %v, want %v"
```

## 改进建议

### 1. 错误信息的一致性
- 统一错误消息格式
- 确保位置信息准确
- 提供更具体的修复建议

### 2. 可读性改进
- 使用更友好的语言描述
- 提供示例代码
- 增加错误解释

### 3. AI 友好性
- 结构化错误信息
- 包含错误类别标识
- 提供修复模式

### 4. 开发者体验
- 提供快速修复建议
- 链接到相关文档
- 突出关键信息

这个分类体系涵盖了 XGo 编译器中的主要错误类型，有助于提高错误诊断的准确性和用户体验。
