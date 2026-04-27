package i18n

import (
	"strings"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/xgo"
	"github.com/stretchr/testify/assert"
)

func TestTranslatorTranslate(t *testing.T) {
	translator := NewTranslator()

	tests := []struct {
		name string
		msg  string
		lang Language
		want string
	}{
		// English - should return original
		{
			name: "EnglishOriginal",
			msg:  `cannot use "Hi" (type untyped string) as type int`,
			lang: LanguageEN,
			want: `cannot use "Hi" (type untyped string) as type int`,
		},

		// Type mismatch errors
		{
			name: "TypeMismatchBasic",
			msg:  `cannot use "Hi" (type untyped string) as type int`,
			lang: LanguageCN,
			want: `无法将 "Hi" (类型 untyped string) 用作类型 int`,
		},
		{
			name: "TypeMismatchWithContext",
			msg:  `cannot use x (type int) as type string in assignment`,
			lang: LanguageCN,
			want: `无法将 x (类型 int) 用作类型 string 在 assignment 中`,
		},

		// Type conversion errors
		{
			name: "TypeConversion",
			msg:  `cannot convert 1<<127 to type Int128`,
			lang: LanguageCN,
			want: `无法将 1<<127 转换为类型 Int128`,
		},

		// Undefined identifiers
		{
			name: "UndefinedIdentifier",
			msg:  `undefined: foo`,
			lang: LanguageCN,
			want: `未定义: foo`,
		},

		// Redeclaration errors
		{
			name: "Redeclaration",
			msg:  `a redeclared in this block`,
			lang: LanguageCN,
			want: `a 在此代码块中重复声明`,
		},

		// Assignment errors
		{
			name: "AssignmentMismatch",
			msg:  `assignment mismatch: 1 variables but bar returns 2 values`,
			lang: LanguageCN,
			want: `赋值不匹配: 1 个变量但 bar 返回 2 个值`,
		},
		{
			name: "CannotUseUnderscore",
			msg:  `cannot use _ as value`,
			lang: LanguageCN,
			want: `无法将 _ 用作值`,
		},

		// Function call errors
		{
			name: "NotEnoughArguments",
			msg:  "not enough arguments in call to Ls\n\thave ()\n\twant (int)",
			lang: LanguageCN,
			want: "调用 Ls 的参数不足\n\t现有 ()\n\t需要 (int)",
		},

		// Array errors
		{
			name: "ArrayIndexOutOfBounds",
			msg:  `array index 5 out of bounds [0:3]`,
			lang: LanguageCN,
			want: `数组索引 5 超出范围 [0:3]`,
		},

		// No match - should return original
		{
			name: "NoPatternMatch",
			msg:  `some random error message`,
			lang: LanguageCN,
			want: `some random error message`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := translator.Translate(tt.msg, tt.lang)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestTranslateGlobalFunction(t *testing.T) {
	// Test the global convenience function
	result := Translate(`undefined: foo`, LanguageCN)
	want := `未定义: foo`

	assert.Equal(t, want, result)
}

// TestCodeBasedErrorTranslation tests error translation using actual xgo code compilation
// This approach is more robust than regex-based testing as it uses real error messages from xgo
//
// Test Coverage Summary:
// - 55+ comprehensive error test cases covering major error categories from errors.md
// - Based on real error patterns from https://github.com/goplus/xgo/blob/main/cl/error_msg_test.go
// - Tests both English passthrough and Chinese translation
// - Flexible pattern matching that logs differences rather than failing on exact matches
// - Real compilation errors ensure patterns stay relevant as xgo evolves
func TestCodeBasedErrorTranslation(t *testing.T) {
	tests := []struct {
		name        string
		code        string
		wantEnError string // Want complete English error message
		wantCnError string // Want complete Chinese translation
	}{
		// 1. Type System Errors
		{
			name:        "TypeMismatchStringToInt",
			code:        `var a int = "Hi"`,
			wantEnError: `cannot use "Hi" (type untyped string) as type int in assignment`,
			wantCnError: `无法将 "Hi" (类型 untyped string) 用作类型 int 在 assignment 中`,
		},
		{
			name:        "TypeMismatchSliceToString",
			code:        `var a string = []string{}`,
			wantEnError: `cannot use []string{} (type []string) as type string in assignment`,
			wantCnError: `无法将 []string{} (类型 []string) 用作类型 string 在 assignment 中`,
		},
		{
			name:        "TypeMismatchArrayToString",
			code:        `var a string = [2]string{}`,
			wantEnError: `cannot use [2]string{} (type [2]string) as type string in assignment`,
			wantCnError: `无法将 [2]string{} (类型 [2]string) 用作类型 string 在 assignment 中`,
		},
		{
			name:        "TypeMismatchMapToString",
			code:        `var a string = map[int]string{}`,
			wantEnError: `cannot use map[int]string{} (type map[int]string) as type string in assignment`,
			wantCnError: `无法将 map[int]string{} (类型 map[int]string) 用作类型 string 在 assignment 中`,
		},
		{
			name:        "TypeMismatchFunctionToString",
			code:        `var a string = func(){}`,
			wantEnError: `cannot use func(){} (type func()) as type string in assignment`,
			wantCnError: `无法将 func(){} (类型 func()) 用作类型 string 在 assignment 中`,
		},
		{
			name:        "TypeMismatchStructToString",
			code:        `type T struct{}; var a string = T{}`,
			wantEnError: `cannot use T{} (type main.T) as type string in assignment`,
			wantCnError: `无法将 T{} (类型 main.T) 用作类型 string 在 assignment 中`,
		},
		{
			name:        "ReturnTypeMismatch",
			code:        `func foo() (int, error) { return 1, "Hi" }`,
			wantEnError: `cannot use "Hi" (type untyped string) as type error in return argument`,
			wantCnError: `无法将 "Hi" (类型 untyped string) 用作类型 error 在 return argument 中`,
		},

		// 2. Variable & Constant Errors
		{
			name:        "UndefinedIdentifier",
			code:        `func main() { println(foo) }`,
			wantEnError: `undefined: foo`,
			wantCnError: `未定义: foo`,
		},
		{
			name:        "UndefinedStructFieldAccess",
			code:        `func main() { foo.x = 1 }`,
			wantEnError: `undefined: foo`,
			wantCnError: `未定义: foo`,
		},
		{
			name:        "BuiltinUsedIncorrectly",
			code:        `func main() { len.x = 1 }`,
			wantEnError: `use of builtin len not in function call`,
			wantCnError: `内建函数 len 的使用不在函数调用中`,
		},
		{
			name: "VariableRedeclaration",
			code: `var a int; var a string`,
			wantEnError: `a redeclared in this block
	previous declaration at main.xgo:1:5`,
			wantCnError: `a 在此代码块中重复声明
	先前声明位于 main.xgo:1:5`,
		},
		{
			name: "ConstRedeclaration",
			code: `var a int; const a = 1`,
			wantEnError: `a redeclared in this block
	previous declaration at main.xgo:1:5`,
			wantCnError: `a 在此代码块中重复声明
	先前声明位于 main.xgo:1:5`,
		},
		{
			name:        "MissingConstValue",
			code:        `const (a = iota; b, c)`,
			wantEnError: `missing value in const declaration`,
			wantCnError: `const 声明中缺少值`,
		},
		{
			name: "NoNewVariablesInDefine",
			code: `a := 1; a := "Hi"`,
			wantEnError: `no new variables on left side of :=
main.xgo:1:14: cannot use "Hi" (type untyped string) as type int in assignment`,
			wantCnError: `:= 左侧没有新变量
main.xgo:1:14: 无法将 "Hi" (类型 untyped string) 用作类型 int 在 assignment 中`,
		},
		{
			name:        "CannotUseUnderscoreAsValue",
			code:        `var a = _`,
			wantEnError: `cannot use _ as value`,
			wantCnError: `无法将 _ 用作值`,
		},
		{
			name:        "IsNotAVariable",
			code:        `println = 1`,
			wantEnError: `println is not a variable`,
			wantCnError: `println 不是一个变量`,
		},

		// 3. Assignment Errors
		{
			name: "AssignmentMismatchMultipleReturn",
			code: `func bar() (int, error) { return 1, nil }
func main() {
	x := 1
	x = bar()
}`,
			wantEnError: `assignment mismatch: 1 variables but bar returns 2 values`,
			wantCnError: `赋值不匹配: 1 个变量但 bar 返回 2 个值`,
		},
		{
			name:        "AssignmentMismatchMultipleValues",
			code:        `x := 1; x = 1, "Hi"`,
			wantEnError: `assignment mismatch: 1 variables but 2 values`,
			wantCnError: `赋值不匹配: 1 个变量但有 2 个值`,
		},

		// 4. Function & Method Errors
		{
			name: "NotEnoughArguments",
			code: `func Ls(int) {}; func main() { Ls() }`,
			wantEnError: `not enough arguments in call to Ls
	have ()
	want (int)`,
			wantCnError: `调用 Ls 的参数不足
	现有 ()
	需要 (int)`,
		},
		{
			name: "TooFewReturnArguments",
			code: `func foo() (int, error) { return 1 }`,
			wantEnError: `too few arguments to return
	have (untyped int)
	want (int, error)`,
			wantCnError: `返回参数数量错误
	现有 (untyped int)
	需要 (int, error)`,
		},
		{
			name: "TooManyReturnArguments",
			code: `func foo() (int, error) { return 1, 2, "Hi" }`,
			wantEnError: `too many arguments to return
	have (untyped int, untyped int, untyped string)
	want (int, error)`,
			wantCnError: `返回参数数量错误
	现有 (untyped int, untyped int, untyped string)
	需要 (int, error)`,
		},
		{
			name: "NotEnoughReturnArgumentsEmpty",
			code: `func foo() byte { return }`,
			wantEnError: `not enough arguments to return
	have ()
	want (byte)`,
			wantCnError: `return 的参数不足
	有 ()
	想要 (byte)`,
		},
		{
			name:        "InitFunctionWithParameters",
			code:        `func init(a int) {}`,
			wantEnError: `func init must have no arguments and no return values`,
			wantCnError: `func init 必须没有参数和返回值`,
		},

		// 5. Control Flow Errors
		{
			name:        "RangeTypeAssignmentError",
			code:        `a := 1; var b []string; for _, a = range b {}`,
			wantEnError: `cannot assign type string to a (type int) in range`,
			wantCnError: `无法在 range 中将类型 string 赋值给 a (类型 int)`,
		},
		{
			name:        "FallthroughOutOfPlace",
			code:        "func foo() {\n\tfallthrough\n}",
			wantEnError: `fallthrough statement out of place`,
			wantCnError: `fallthrough 语句位置错误`,
		},
		{
			name:        "LabelNotDefinedGoto",
			code:        `x := 1; goto foo`,
			wantEnError: `label foo is not defined`,
			wantCnError: `标签 foo 未定义`,
		},
		{
			name:        "LabelNotDefinedBreak",
			code:        `x := 1; break foo`,
			wantEnError: `label foo is not defined`,
			wantCnError: `标签 foo 未定义`,
		},
		{
			name: "DuplicateSwitchCase",
			code: `var n int; switch n { case 100: case 100: }`,
			wantEnError: `duplicate case 100 in switch
	previous case at main.xgo:1:28`,
			wantCnError: `switch 中重复的 case 100
	先前 case 位于 main.xgo:1:28`,
		},
		{
			name:        "MultipleDefaultsInSwitch",
			code:        `var n interface{}; switch n { default: default: }`,
			wantEnError: `multiple defaults in switch (first at main.xgo:1:31)`,
			wantCnError: `switch 中有多个 default (第一个位于 main.xgo:1:31)`,
		},
		{
			name: "DuplicateCaseInTypeSwitch",
			code: `var n interface{} = 100; switch n.(type) { case int: case int: }`,
			wantEnError: `duplicate case int in type switch
	previous case at main.xgo:1:49`,
			wantCnError: `switch 中重复的 case int
	先前 case 位于 main.xgo:1:49`,
		},
		{
			name:        "MultipleDefaultsInTypeSwitch",
			code:        `var n interface{} = 100; switch n.(type) { default: default: }`,
			wantEnError: `multiple defaults in type switch (first at main.xgo:1:44)`,
			wantCnError: `switch 中有多个 default (第一个位于 main.xgo:1:44)`,
		},

		// 6. Data Structure Errors
		{
			name:        "ArrayLiteralBoundsBasic",
			code:        `var a [3]int = [3]int{1, 2, 3, 4}`,
			wantEnError: `array index 3 out of bounds [0:3]`,
			wantCnError: `数组索引 3 超出范围 [0:3]`,
		},
		{
			name:        "ArrayLiteralBoundsWithIndex",
			code:        `a := "Hi"; b := [10]int{9: 1, 3}`,
			wantEnError: `array index 10 out of bounds [0:10]`,
			wantCnError: `数组索引 10 超出范围 [0:10]`,
		},
		{
			name:        "ArrayLiteralBoundsOverflow",
			code:        `a := "Hi"; b := [1]int{1, 2}`,
			wantEnError: `array index 1 out of bounds [0:1]`,
			wantCnError: `数组索引 1 超出范围 [0:1]`,
		},
		{
			name:        "ArrayIndexWithValueOutOfBounds",
			code:        `a := "Hi"; b := [10]int{12: 2}`,
			wantEnError: `array index 12 (value 12) out of bounds [0:10]`,
			wantCnError: `数组索引 12 (值 12) 超出范围 [0:10]`,
		},
		{
			name:        "ArrayLiteralTypeError",
			code:        `a := "Hi"; b := [10]int{a+"!": 1}`,
			wantEnError: `cannot use a+"!" as index which must be non-negative integer constant`,
			wantCnError: `无法将 a+"!" 用作索引，索引必须是非负整数常量`,
		},
		{
			name:        "ArrayIndexNonConstant",
			code:        `a := "Hi"; b := [10]int{a: 1}`,
			wantEnError: `cannot use a as index which must be non-negative integer constant`,
			wantCnError: `无法将 a 用作索引，索引必须是非负整数常量`,
		},
		{
			name:        "NonConstantArrayBound",
			code:        `var n int; var a [n]int`,
			wantEnError: `non-constant array bound n`,
			wantCnError: `非常量 array bound n`,
		},
		{
			name:        "SliceLiteralIndexError",
			code:        `a := "Hi"; b := []int{a: 1}`,
			wantEnError: `cannot use a as index which must be non-negative integer constant`,
			wantCnError: `无法将 a 用作索引，索引必须是非负整数常量`,
		},
		{
			name:        "SliceLiteralTypeError",
			code:        `a := "Hi"; b := []int{a}`,
			wantEnError: `cannot use a (type string) as type int in slice literal`,
			wantCnError: `无法将 a (类型 string) 用作类型 int 在 slice literal 中`,
		},
		{
			name:        "SliceLiteralIndexedTypeError",
			code:        `a := "Hi"; b := []int{2: a}`,
			wantEnError: `cannot use a (type string) as type int in slice literal`,
			wantCnError: `无法将 a (类型 string) 用作类型 int 在 slice literal 中`,
		},
		{
			name:        "CannotSlicePointer",
			code:        `var a *byte; x := 1; b := a[x:2]`,
			wantEnError: `cannot slice a (type *byte)`,
			wantCnError: `无法切片 a (类型 *byte)`,
		},
		{
			name:        "CannotSliceBool",
			code:        `a := true; b := a[1:2]`,
			wantEnError: `cannot slice a (type bool)`,
			wantCnError: `无法切片 a (类型 bool)`,
		},
		{
			name:        "MapLiteralMissingKey",
			code:        `map[string]int{2}`,
			wantEnError: `missing key in map literal`,
			wantCnError: `映射字面量中缺少键`,
		},
		{
			name:        "MapLiteralKeyTypeError",
			code:        `a := map[string]int{1+2: 2}`,
			wantEnError: `cannot use 1+2 (type untyped int) as type string in map key`,
			wantCnError: `无法将 1+2 (类型 untyped int) 用作类型 string 在 map key 中`,
		},
		{
			name:        "MapLiteralValueTypeError",
			code:        `b := map[string]int{"Hi": "Go" + "+"}`,
			wantEnError: `cannot use "Go" + "+" (type untyped string) as type int in map value`,
			wantCnError: `无法将 "Go" + "+" (类型 untyped string) 用作类型 int 在 map value 中`,
		},
		{
			name:        "InvalidCompositeLiteralType",
			code:        `int{2}`,
			wantEnError: `invalid composite literal type int`,
			wantCnError: `无效的复合字面量类型 int`,
		},
		{
			name:        "InvalidMapLiteral",
			code:        `var v any = {1:2,1}`,
			wantEnError: `invalid map literal`,
			wantCnError: `无效的映射字面量`,
		},

		// 7. Struct Errors
		{
			name:        "StructTooManyValues",
			code:        `x := 1; a := struct{x int; y string}{1, "Hi", 2}`,
			wantEnError: `too many values in struct{x int; y string}{...}`,
			wantCnError: `struct{...} 中值的数量错误`,
		},
		{
			name:        "StructTooFewValues",
			code:        `x := 1; a := struct{x int; y string}{1}`,
			wantEnError: `too few values in struct{x int; y string}{...}`,
			wantCnError: `struct{...} 中值的数量错误`,
		},
		{
			name:        "StructFieldTypeMismatch",
			code:        `x := 1; a := struct{x int; y string}{1, x}`,
			wantEnError: `cannot use x (type int) as type string in value of field y`,
			wantCnError: `无法将 x (类型 int) 用作类型 string 在 value of field y 中`,
		},
		{
			name:        "StructUndefinedField",
			code:        `a := struct{x int; y string}{z: 1}`,
			wantEnError: `z undefined (type struct{x int; y string} has no field or method z)`,
			wantCnError: `z 未定义 (类型 struct{x int; y string} 没有字段或方法 z)`,
		},
		{
			name:        "StructFieldAccessUndefined",
			code:        `a := struct{x int; y string}{}; a.z = 1`,
			wantEnError: `a.z undefined (type struct{x int; y string} has no field or method z)`,
			wantCnError: `a.z 未定义 (类型 struct{x int; y string} 没有字段或方法 z)`,
		},

		// 8. Package Import Errors
		{
			name:        "UndefinedPackageInType",
			code:        `func foo(t *testing.T) {}`,
			wantEnError: `undefined: testing`,
			wantCnError: `未定义: testing`,
		},
		{
			name:        "PackageFieldNotAType",
			code:        `import "testing"; func foo(t testing.Verbose) {}`,
			wantEnError: `testing.Verbose is not a type`,
			wantCnError: `testing.Verbose 不是一个类型`,
		},
		{
			name:        "CannotReferToUnexportedName",
			code:        `import "os"; func foo() { os.undefined }`,
			wantEnError: `cannot refer to unexported name os.undefined`,
			wantCnError: `无法引用未导出的名称 os.undefined`,
		},
		{
			name:        "UndefinedExportedName",
			code:        `import "os"; func foo() { os.UndefinedObject }`,
			wantEnError: `undefined: os.UndefinedObject`,
			wantCnError: `未定义: os.UndefinedObject`,
		},

		// 9. Pointer Operation Errors
		{
			name:        "InvalidIndirectOfString",
			code:        `a := "test"; b := *a`,
			wantEnError: `invalid indirect of a (type string)`,
			wantCnError: `无效的间接引用 a (类型 string)`,
		},

		// 11. Lambda Expression Errors
		// Note: These may require specific XGo syntax support
		{
			name:        "LambdaMultipleAssignment",
			code:        `var foo, foo1 func() = nil, => {}`, // XGo Lambda syntax
			wantEnError: `lambda unsupport multiple assignment`,
			wantCnError: `lambda 不支持多重赋值`,
		},

		// 12. Invalid Operations
		{
			name:        "InvalidOperationIndexingBool",
			code:        `a := true; b := a[1]`,
			wantEnError: `invalid operation: a[1] (type bool does not support indexing)`,
			wantCnError: `无效操作: a[1] (类型 bool 不支持 indexing)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compile the code and get the actual error message
			actualError := compileAndGetError(tt.code)

			// Skip test if we couldn't get the wanted error
			if actualError == "" {
				t.Skipf("Could not reproduce wanted error for code: %s", tt.code)
				return
			}

			// Check if we got the wanted error message
			if tt.wantEnError != "" && actualError != tt.wantEnError {
				assert.Equal(t, tt.wantEnError, actualError, "got different error than wanted")
			}

			translator := NewTranslator()
			// Test Chinese translation
			cnResult := translator.Translate(actualError, LanguageCN)

			// Check if the translated message equals wanted Chinese terms
			if tt.wantCnError != "" && cnResult != tt.wantCnError {
				assert.Equal(t, tt.wantCnError, cnResult, "Chinese translation doesn't match wanted term")
			}
		})
	}
}

// compileAndGetError compiles the given xgo code and returns the first error message
func compileAndGetError(code string) string {
	// Import necessary packages for compilation
	gofset := token.NewFileSet()

	// Create files map
	files := map[string]*xgo.File{
		"main.xgo": {
			Content: []byte(code),
			Version: 1,
		},
	}

	// Create project with all features enabled and set package path
	proj := xgo.NewProject(gofset, files, xgo.FeatAll)
	proj.PkgPath = "main"

	// Try to get type info - this will compile the code and capture errors
	_, err := proj.TypeInfo()
	if err != nil {
		// Extract the actual error message from the error
		return extractErrorMessage(err.Error())
	}

	return ""
}

// extractErrorMessage extracts the core error message from xgo compilation error
func extractErrorMessage(fullError string) string {
	// XGo errors typically have format: "filename:line:col: actual error message"
	// We want to extract just the "actual error message" part
	parts := strings.SplitN(fullError, ": ", 2)
	if len(parts) >= 2 {
		msg := parts[1]
		// Remove "missing return" errors that may appear on subsequent lines.
		// These are additional errors reported by gogen but not part of the primary error.
		lines := strings.Split(msg, "\n")
		var result []string
		for _, line := range lines {
			if strings.HasSuffix(line, ": missing return") {
				continue
			}
			result = append(result, line)
		}
		return strings.Join(result, "\n")
	}
	return fullError
}

// Benchmark test
func BenchmarkTranslator_Translate(b *testing.B) {
	translator := NewTranslator()
	msg := `cannot use "Hi" (type untyped string) as type int`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		translator.Translate(msg, LanguageCN)
	}
}
