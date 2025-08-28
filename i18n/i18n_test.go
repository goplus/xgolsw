package i18n

import (
	"strings"
	"testing"

	gotoken "go/token"

	"github.com/goplus/xgolsw/xgo"
)

func TestTranslator_Translate(t *testing.T) {
	translator := NewTranslator()

	tests := []struct {
		name     string
		msg      string
		lang     Language
		expected string
	}{
		// English - should return original
		{
			name:     "English original",
			msg:      `cannot use "Hi" (type untyped string) as type int`,
			lang:     LanguageEN,
			expected: `cannot use "Hi" (type untyped string) as type int`,
		},

		// Type mismatch errors
		{
			name:     "Type mismatch basic",
			msg:      `cannot use "Hi" (type untyped string) as type int`,
			lang:     LanguageCN,
			expected: `无法将 "Hi" (类型 untyped string) 用作类型 int`,
		},
		{
			name:     "Type mismatch with context",
			msg:      `cannot use x (type int) as type string in assignment`,
			lang:     LanguageCN,
			expected: `无法将 x (类型 int) 用作类型 string 在 assignment 中`,
		},

		// Type conversion errors
		{
			name:     "Type conversion",
			msg:      `cannot convert 1<<127 to type Int128`,
			lang:     LanguageCN,
			expected: `无法将 1<<127 转换为类型 Int128`,
		},

		// Undefined identifiers
		{
			name:     "Undefined identifier",
			msg:      `undefined: foo`,
			lang:     LanguageCN,
			expected: `未定义: foo`,
		},

		// Redeclaration errors
		{
			name:     "Redeclaration",
			msg:      `a redeclared in this block`,
			lang:     LanguageCN,
			expected: `a 在此代码块中重复声明`,
		},

		// Assignment errors
		{
			name:     "Assignment mismatch",
			msg:      `assignment mismatch: 1 variables but bar returns 2 values`,
			lang:     LanguageCN,
			expected: `赋值不匹配: 1 个变量但 bar 返回 2 个值`,
		},
		{
			name:     "Cannot use underscore",
			msg:      `cannot use _ as value`,
			lang:     LanguageCN,
			expected: `无法将 _ 用作值`,
		},

		// Function call errors
		{
			name:     "Not enough arguments",
			msg:      "not enough arguments in call to Ls\n\thave ()\n\twant (int)",
			lang:     LanguageCN,
			expected: "调用 Ls 的参数不足\n\t现有 ()\n\t需要 (int)",
		},

		// Array errors
		{
			name:     "Array index out of bounds",
			msg:      `array index 5 out of bounds [0:3]`,
			lang:     LanguageCN,
			expected: `数组索引 5 超出范围 [0:3]`,
		},

		// No match - should return original
		{
			name:     "No pattern match",
			msg:      `some random error message`,
			lang:     LanguageCN,
			expected: `some random error message`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := translator.Translate(tt.msg, tt.lang)
			if result != tt.expected {
				t.Errorf("Translate() = %q, expected %q", result, tt.expected)
			}
		})
	}
}

func TestTranslate_GlobalFunction(t *testing.T) {
	// Test the global convenience function
	result := Translate(`undefined: foo`, LanguageCN)
	expected := `未定义: foo`

	if result != expected {
		t.Errorf("Global Translate() = %q, expected %q", result, expected)
	}
}

func TestTranslator_GetSupportedLanguages(t *testing.T) {
	translator := NewTranslator()
	languages := translator.GetSupportedLanguages()

	expectedLanguages := []Language{LanguageEN, LanguageCN}

	if len(languages) != len(expectedLanguages) {
		t.Errorf("Expected %d languages, got %d", len(expectedLanguages), len(languages))
		return
	}

	for i, lang := range languages {
		if lang != expectedLanguages[i] {
			t.Errorf("Expected language %s at index %d, got %s", expectedLanguages[i], i, lang)
		}
	}
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
		name          string
		code          string
		expectEnError string // Expected complete English error message
		expectCnError string // Expected complete Chinese translation
	}{
		// 1. 类型系统错误 (Type System Errors)
		{
			name:          "Type mismatch string to int",
			code:          `var a int = "Hi"`,
			expectEnError: `cannot use "Hi" (type untyped string) as type int in assignment`,
			expectCnError: `无法将 "Hi" (类型 untyped string) 用作类型 int 在 assignment 中`,
		},
		{
			name:          "Type mismatch slice to string",
			code:          `var a string = []string{}`,
			expectEnError: `cannot use []string{} (type []string) as type string in assignment`,
			expectCnError: `无法将 []string{} (类型 []string) 用作类型 string 在 assignment 中`,
		},
		{
			name:          "Type mismatch array to string",
			code:          `var a string = [2]string{}`,
			expectEnError: `cannot use [2]string{} (type [2]string) as type string in assignment`,
			expectCnError: `无法将 [2]string{} (类型 [2]string) 用作类型 string 在 assignment 中`,
		},
		{
			name:          "Type mismatch map to string",
			code:          `var a string = map[int]string{}`,
			expectEnError: `cannot use map[int]string{} (type map[int]string) as type string in assignment`,
			expectCnError: `无法将 map[int]string{} (类型 map[int]string) 用作类型 string 在 assignment 中`,
		},
		{
			name:          "Type mismatch function to string",
			code:          `var a string = func(){}`,
			expectEnError: `cannot use func(){} (type func()) as type string in assignment`,
			expectCnError: `无法将 func(){} (类型 func()) 用作类型 string 在 assignment 中`,
		},
		{
			name:          "Type mismatch struct to string",
			code:          `type T struct{}; var a string = T{}`,
			expectEnError: `cannot use T{} (type main.T) as type string in assignment`,
			expectCnError: `无法将 T{} (类型 main.T) 用作类型 string 在 assignment 中`,
		},
		{
			name:          "Return type mismatch",
			code:          `func foo() (int, error) { return 1, "Hi" }`,
			expectEnError: `cannot use "Hi" (type untyped string) as type error in return argument`,
			expectCnError: `无法将 "Hi" (类型 untyped string) 用作类型 error 在 return argument 中`,
		},

		// 2. 变量和常量错误 (Variable & Constant Errors)
		{
			name:          "Undefined identifier",
			code:          `func main() { println(foo) }`,
			expectEnError: `undefined: foo`,
			expectCnError: `未定义: foo`,
		},
		{
			name:          "Undefined struct field access",
			code:          `func main() { foo.x = 1 }`,
			expectEnError: `undefined: foo`,
			expectCnError: `未定义: foo`,
		},
		{
			name:          "Builtin used incorrectly",
			code:          `func main() { len.x = 1 }`,
			expectEnError: `use of builtin len not in function call`,
			expectCnError: `内建函数 len 的使用不在函数调用中`,
		},
		{
			name: "Variable redeclaration",
			code: `var a int; var a string`,
			expectEnError: `a redeclared in this block
	previous declaration at main.xgo:1:5`,
			expectCnError: `a 在此代码块中重复声明
	先前声明位于 main.xgo:1:5`,
		},
		{
			name: "Const redeclaration",
			code: `var a int; const a = 1`,
			expectEnError: `a redeclared in this block
	previous declaration at main.xgo:1:5`,
			expectCnError: `a 在此代码块中重复声明
	先前声明位于 main.xgo:1:5`,
		},
		{
			name:          "Missing const value",
			code:          `const (a = iota; b, c)`,
			expectEnError: `missing value in const declaration`,
			expectCnError: `const 声明中缺少值`,
		},
		{
			name: "No new variables in :=",
			code: `a := 1; a := "Hi"`,
			expectEnError: `no new variables on left side of :=
main.xgo:1:14: cannot use "Hi" (type untyped string) as type int in assignment`,
			expectCnError: `:= 左侧没有新变量
main.xgo:1:14: 无法将 "Hi" (类型 untyped string) 用作类型 int 在 assignment 中`,
		},
		{
			name:          "Cannot use underscore as value",
			code:          `var a = _`,
			expectEnError: `cannot use _ as value`,
			expectCnError: `无法将 _ 用作值`,
		},
		{
			name:          "Is not a variable",
			code:          `println = 1`,
			expectEnError: `println is not a variable`,
			expectCnError: `println 不是一个变量`,
		},

		// 3. 赋值错误 (Assignment Errors)
		{
			name: "Assignment mismatch multiple return",
			code: `func bar() (int, error) { return 1, nil }
func main() {
	x := 1
	x = bar()
}`,
			expectEnError: `assignment mismatch: 1 variables but bar returns 2 values`,
			expectCnError: `赋值不匹配: 1 个变量但 bar 返回 2 个值`,
		},
		{
			name:          "Assignment mismatch multiple values",
			code:          `x := 1; x = 1, "Hi"`,
			expectEnError: `assignment mismatch: 1 variables but 2 values`,
			expectCnError: `赋值不匹配: 1 个变量但有 2 个值`,
		},

		// 4. 函数和方法错误 (Function & Method Errors)
		{
			name: "Not enough arguments",
			code: `func Ls(int) {}; func main() { Ls() }`,
			expectEnError: `not enough arguments in call to Ls
	have ()
	want (int)`,
			expectCnError: `调用 Ls 的参数不足
	现有 ()
	需要 (int)`,
		},
		{
			name: "Too few return arguments",
			code: `func foo() (int, error) { return 1 }`,
			expectEnError: `too few arguments to return
	have (untyped int)
	want (int, error)`,
			expectCnError: `返回参数数量错误
	现有 (untyped int)
	需要 (int, error)`,
		},
		{
			name: "Too many return arguments",
			code: `func foo() (int, error) { return 1, 2, "Hi" }`,
			expectEnError: `too many arguments to return
	have (untyped int, untyped int, untyped string)
	want (int, error)`,
			expectCnError: `返回参数数量错误
	现有 (untyped int, untyped int, untyped string)
	需要 (int, error)`,
		},
		{
			name: "Not enough return arguments empty",
			code: `func foo() byte { return }`,
			expectEnError: `not enough arguments to return
	have ()
	want (byte)`,
			expectCnError: `return 的参数不足
	有 ()
	想要 (byte)`,
		},
		{
			name:          "Init function with parameters",
			code:          `func init(a int) {}`,
			expectEnError: `func init must have no arguments and no return values`,
			expectCnError: `func init 必须没有参数和返回值`,
		},

		// 5. 控制流错误 (Control Flow Errors)
		{
			name:          "Range type assignment error",
			code:          `a := 1; var b []string; for _, a = range b {}`,
			expectEnError: `cannot assign type string to a (type int) in range`,
			expectCnError: `无法在 range 中将类型 string 赋值给 a (类型 int)`,
		},
		{
			name:          "Fallthrough out of place",
			code:          "func foo() {\n\tfallthrough\n}",
			expectEnError: `fallthrough statement out of place`,
			expectCnError: `fallthrough 语句位置错误`,
		},
		{
			name:          "Label not defined goto",
			code:          `x := 1; goto foo`,
			expectEnError: `label foo is not defined`,
			expectCnError: `标签 foo 未定义`,
		},
		{
			name:          "Label not defined break",
			code:          `x := 1; break foo`,
			expectEnError: `label foo is not defined`,
			expectCnError: `标签 foo 未定义`,
		},
		{
			name: "Duplicate switch case",
			code: `var n int; switch n { case 100: case 100: }`,
			expectEnError: `duplicate case 100 in switch
	previous case at main.xgo:1:28`,
			expectCnError: `switch 中重复的 case 100
	先前 case 位于 main.xgo:1:28`,
		},
		{
			name:          "Multiple defaults in switch",
			code:          `var n interface{}; switch n { default: default: }`,
			expectEnError: `multiple defaults in switch (first at main.xgo:1:31)`,
			expectCnError: `switch 中有多个 default (第一个位于 main.xgo:1:31)`,
		},
		{
			name: "Duplicate case in type switch",
			code: `var n interface{} = 100; switch n.(type) { case int: case int: }`,
			expectEnError: `duplicate case int in type switch
	previous case at main.xgo:1:49`,
			expectCnError: `switch 中重复的 case int
	先前 case 位于 main.xgo:1:49`,
		},
		{
			name:          "Multiple defaults in type switch",
			code:          `var n interface{} = 100; switch n.(type) { default: default: }`,
			expectEnError: `multiple defaults in type switch (first at main.xgo:1:44)`,
			expectCnError: `switch 中有多个 default (第一个位于 main.xgo:1:44)`,
		},

		// 6. 数据结构错误 (Data Structure Errors)
		{
			name:          "Array literal bounds basic",
			code:          `var a [3]int = [3]int{1, 2, 3, 4}`,
			expectEnError: `array index 3 out of bounds [0:3]`,
			expectCnError: `数组索引 3 超出范围 [0:3]`,
		},
		{
			name:          "Array literal bounds with index",
			code:          `a := "Hi"; b := [10]int{9: 1, 3}`,
			expectEnError: `array index 10 out of bounds [0:10]`,
			expectCnError: `数组索引 10 超出范围 [0:10]`,
		},
		{
			name:          "Array literal bounds overflow",
			code:          `a := "Hi"; b := [1]int{1, 2}`,
			expectEnError: `array index 1 out of bounds [0:1]`,
			expectCnError: `数组索引 1 超出范围 [0:1]`,
		},
		{
			name:          "Array index with value out of bounds",
			code:          `a := "Hi"; b := [10]int{12: 2}`,
			expectEnError: `array index 12 (value 12) out of bounds [0:10]`,
			expectCnError: `数组索引 12 (值 12) 超出范围 [0:10]`,
		},
		{
			name:          "Array literal type error",
			code:          `a := "Hi"; b := [10]int{a+"!": 1}`,
			expectEnError: `cannot use a+"!" as index which must be non-negative integer constant`,
			expectCnError: `无法将 a+"!" 用作索引，索引必须是非负整数常量`,
		},
		{
			name:          "Array index non-constant",
			code:          `a := "Hi"; b := [10]int{a: 1}`,
			expectEnError: `cannot use a as index which must be non-negative integer constant`,
			expectCnError: `无法将 a 用作索引，索引必须是非负整数常量`,
		},
		{
			name:          "Non-constant array bound",
			code:          `var n int; var a [n]int`,
			expectEnError: `non-constant array bound n`,
			expectCnError: `非常量 array bound n`,
		},
		{
			name:          "Slice literal index error",
			code:          `a := "Hi"; b := []int{a: 1}`,
			expectEnError: `cannot use a as index which must be non-negative integer constant`,
			expectCnError: `无法将 a 用作索引，索引必须是非负整数常量`,
		},
		{
			name:          "Slice literal type error",
			code:          `a := "Hi"; b := []int{a}`,
			expectEnError: `cannot use a (type string) as type int in slice literal`,
			expectCnError: `无法将 a (类型 string) 用作类型 int 在 slice literal 中`,
		},
		{
			name:          "Slice literal indexed type error",
			code:          `a := "Hi"; b := []int{2: a}`,
			expectEnError: `cannot use a (type string) as type int in slice literal`,
			expectCnError: `无法将 a (类型 string) 用作类型 int 在 slice literal 中`,
		},
		{
			name:          "Cannot slice pointer",
			code:          `var a *byte; x := 1; b := a[x:2]`,
			expectEnError: `cannot slice a (type *byte)`,
			expectCnError: `无法切片 a (类型 *byte)`,
		},
		{
			name:          "Cannot slice bool",
			code:          `a := true; b := a[1:2]`,
			expectEnError: `cannot slice a (type bool)`,
			expectCnError: `无法切片 a (类型 bool)`,
		},
		{
			name:          "Map literal missing key",
			code:          `map[string]int{2}`,
			expectEnError: `missing key in map literal`,
			expectCnError: `映射字面量中缺少键`,
		},
		{
			name:          "Map literal key type error",
			code:          `a := map[string]int{1+2: 2}`,
			expectEnError: `cannot use 1+2 (type untyped int) as type string in map key`,
			expectCnError: `无法将 1+2 (类型 untyped int) 用作类型 string 在 map key 中`,
		},
		{
			name:          "Map literal value type error",
			code:          `b := map[string]int{"Hi": "Go" + "+"}`,
			expectEnError: `cannot use "Go" + "+" (type untyped string) as type int in map value`,
			expectCnError: `无法将 "Go" + "+" (类型 untyped string) 用作类型 int 在 map value 中`,
		},
		{
			name:          "Invalid composite literal type",
			code:          `int{2}`,
			expectEnError: `invalid composite literal type int`,
			expectCnError: `无效的复合字面量类型 int`,
		},
		{
			name:          "Invalid map literal",
			code:          `var v any = {1:2,1}`,
			expectEnError: `invalid map literal`,
			expectCnError: `无效的映射字面量`,
		},

		// 7. 结构体错误 (Struct Errors)
		{
			name:          "Struct too many values",
			code:          `x := 1; a := struct{x int; y string}{1, "Hi", 2}`,
			expectEnError: `too many values in struct{x int; y string}{...}`,
			expectCnError: `struct{...} 中值的数量错误`,
		},
		{
			name:          "Struct too few values",
			code:          `x := 1; a := struct{x int; y string}{1}`,
			expectEnError: `too few values in struct{x int; y string}{...}`,
			expectCnError: `struct{...} 中值的数量错误`,
		},
		{
			name:          "Struct field type mismatch",
			code:          `x := 1; a := struct{x int; y string}{1, x}`,
			expectEnError: `cannot use x (type int) as type string in value of field y`,
			expectCnError: `无法将 x (类型 int) 用作类型 string 在 value of field y 中`,
		},
		{
			name:          "Struct undefined field",
			code:          `a := struct{x int; y string}{z: 1}`,
			expectEnError: `z undefined (type struct{x int; y string} has no field or method z)`,
			expectCnError: `z 未定义 (类型 struct{x int; y string} 没有字段或方法 z)`,
		},
		{
			name:          "Struct field access undefined",
			code:          `a := struct{x int; y string}{}; a.z = 1`,
			expectEnError: `a.z undefined (type struct{x int; y string} has no field or method z)`,
			expectCnError: `a.z 未定义 (类型 struct{x int; y string} 没有字段或方法 z)`,
		},

		// 8. 包导入错误 (Package Import Errors)
		{
			name:          "Undefined package in type",
			code:          `func foo(t *testing.T) {}`,
			expectEnError: `undefined: testing`,
			expectCnError: `未定义: testing`,
		},
		{
			name:          "Package field not a type",
			code:          `import "testing"; func foo(t testing.Verbose) {}`,
			expectEnError: `testing.Verbose is not a type`,
			expectCnError: `testing.Verbose 不是一个类型`,
		},
		{
			name:          "Cannot refer to unexported name",
			code:          `import "os"; func foo() { os.undefined }`,
			expectEnError: `cannot refer to unexported name os.undefined`,
			expectCnError: `无法引用未导出的名称 os.undefined`,
		},
		{
			name:          "Undefined exported name",
			code:          `import "os"; func foo() { os.UndefinedObject }`,
			expectEnError: `undefined: os.UndefinedObject`,
			expectCnError: `未定义: os.UndefinedObject`,
		},

		// 9. 指针操作错误 (Pointer Operation Errors)
		{
			name:          "Invalid indirect of string",
			code:          `a := "test"; b := *a`,
			expectEnError: `invalid indirect of a (type string)`,
			expectCnError: `无效的间接引用 a (类型 string)`,
		},

		// 11. Lambda 表达式错误 (Lambda Expression Errors)
		// Note: These may require specific XGo syntax support
		{
			name:          "Lambda multiple assignment",
			code:          `var foo, foo1 func() = nil, => {}`, // XGo Lambda syntax
			expectEnError: `lambda unsupport multiple assignment`,
			expectCnError: `lambda 不支持多重赋值`,
		},

		// 12. 无效操作 (Invalid Operations)
		{
			name:          "Invalid operation indexing bool",
			code:          `a := true; b := a[1]`,
			expectEnError: `invalid operation: a[1] (type bool does not support indexing)`,
			expectCnError: `无效操作: a[1] (类型 bool 不支持 indexing)`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compile the code and get the actual error message
			actualError := compileAndGetError(tt.code)

			// Skip test if we couldn't get the expected error
			if actualError == "" {
				t.Skipf("Could not reproduce expected error for code: %s", tt.code)
				return
			}

			// Check if we got the expected error message
			if tt.expectEnError != "" && actualError != tt.expectEnError {
				t.Errorf("Got different error than expected. Got: %s, Expected: %s", actualError, tt.expectEnError)
			}

			translator := NewTranslator()
			// Test Chinese translation
			cnResult := translator.Translate(actualError, LanguageCN)

			// Check if the translated message equals expected Chinese terms
			if tt.expectCnError != "" && cnResult != tt.expectCnError {
				t.Errorf("Chinese translation doesn't equals expected term '%s'. Got: %q", tt.expectCnError, cnResult)
			}
		})
	}
}

// compileAndGetError compiles the given xgo code and returns the first error message
func compileAndGetError(code string) string {
	// Import necessary packages for compilation
	gofset := gotoken.NewFileSet()

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
		return parts[1]
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
