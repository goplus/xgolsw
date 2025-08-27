package i18n

import (
	"testing"
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

		// XGo specific errors
		{
			name:     "Operator undefined",
			msg:      `operator $name undefined`,
			lang:     LanguageCN,
			expected: `操作符 $name 未定义`,
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

// Benchmark test
func BenchmarkTranslator_Translate(b *testing.B) {
	translator := NewTranslator()
	msg := `cannot use "Hi" (type untyped string) as type int`

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		translator.Translate(msg, LanguageCN)
	}
}
