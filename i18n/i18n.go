package i18n

import (
	"regexp"
	"strings"
)

// Language represents the target language for translation
type Language string

const (
	LanguageEN Language = "en"
	LanguageCN Language = "cn"
)

// ErrorPattern represents a compiled regex pattern with its Chinese translation template
type ErrorPattern struct {
	Pattern     *regexp.Regexp
	Translation string
}

// Translator handles error message translation
type Translator struct {
	patterns []ErrorPattern
}

// NewTranslator creates a new translator with pre-compiled regex patterns
func NewTranslator() *Translator {
	patterns := []ErrorPattern{
		// 1. 类型不匹配 (Type Mismatch) - 带上下文
		{
			Pattern:     regexp.MustCompile(`^cannot use (.+?) \(type (.+?)\) as type (.+?) in (.+?)$`),
			Translation: "无法将 $1 (类型 $2) 用作类型 $3 在 $4 中",
		},
		// 1. 类型不匹配 (Type Mismatch) - 不带上下文
		{
			Pattern:     regexp.MustCompile(`^cannot use (.+?) \(type (.+?)\) as type (.+?)$`),
			Translation: "无法将 $1 (类型 $2) 用作类型 $3",
		},

		// 2. 类型转换错误 (Type Conversion Errors)
		{
			Pattern:     regexp.MustCompile(`^cannot convert (.+?) to type (.+?)$`),
			Translation: "无法将 $1 转换为类型 $2",
		},

		// 3. 泛型类型错误 (Generic Type Errors)
		{
			Pattern:     regexp.MustCompile(`^cannot use generic type (.+?) without instantiation$`),
			Translation: "无法使用未实例化的泛型类型 $1",
		},
		{
			Pattern:     regexp.MustCompile(`^got (\d+) type parameter(?:s)?, but receiver base type declares (\d+)$`),
			Translation: "获得 $1 个类型参数，但接收器基类型声明了 $2 个",
		},
		{
			Pattern:     regexp.MustCompile(`^got (\d+) arguments but (\d+) type parameters$`),
			Translation: "获得 $1 个参数但需要 $2 个类型参数",
		},

		// 4. 未定义标识符 (Undefined Identifiers)
		{
			Pattern:     regexp.MustCompile(`^undefined: (.+?)$`),
			Translation: "未定义: $1",
		},

		// 5. 重复声明 (Redeclaration Errors)
		{
			Pattern:     regexp.MustCompile(`^(.+?) redeclared in this block$`),
			Translation: "$1 在此代码块中重复声明",
		},
		{
			Pattern:     regexp.MustCompile(`^(.+?) redeclared\n\t(.+?) other declaration of (.+?)$`),
			Translation: "$1 重复声明\n\t$2 $3 的其他声明",
		},

		// 6. 赋值错误 (Assignment Errors)
		{
			Pattern:     regexp.MustCompile(`^assignment mismatch: (\d+) variables? but (.+?) returns? (\d+) values?$`),
			Translation: "赋值不匹配: $1 个变量但 $2 返回 $3 个值",
		},
		{
			Pattern:     regexp.MustCompile(`^assignment mismatch: (\d+) variables? but (\d+) values?$`),
			Translation: "赋值不匹配: $1 个变量但有 $2 个值",
		},
		{
			Pattern:     regexp.MustCompile(`^cannot use _ as value$`),
			Translation: "无法将 _ 用作值",
		},
		{
			Pattern:     regexp.MustCompile(`^(.+?) is not a variable$`),
			Translation: "$1 不是一个变量",
		},
		{
			Pattern:     regexp.MustCompile(`^no new variables on left side of :=$`),
			Translation: ":= 左侧没有新变量",
		},

		// 7. 常量错误 (Constant Errors)
		{
			Pattern:     regexp.MustCompile(`^missing value in const declaration$`),
			Translation: "const 声明中缺少值",
		},
		{
			Pattern:     regexp.MustCompile(`^non-constant (.+?)$`),
			Translation: "非常量 $1",
		},

		// 8. 函数调用错误 (Function Call Errors)
		{
			Pattern:     regexp.MustCompile(`^not enough arguments in call to (.+?)\n\thave \(([^)]*)\)\n\twant \(([^)]*)\)$`),
			Translation: "调用 $1 的参数不足\n\t现有 ($2)\n\t需要 ($3)",
		},
		{
			Pattern:     regexp.MustCompile(`^too (?:few|many) arguments to return\n\thave \(([^)]*)\)\n\twant \(([^)]*)\)$`),
			Translation: "返回参数数量错误\n\t现有 ($1)\n\t需要 ($2)",
		},

		// 9. 方法接收器错误 (Method Receiver Errors)
		{
			Pattern:     regexp.MustCompile(`^invalid receiver type (.+?) \((.+?) is (?:not a defined type|a pointer type|an interface type)\)$`),
			Translation: "无效的接收器类型 $1 ($2)",
		},

		// 10. Lambda 表达式错误 (Lambda Expression Errors)
		{
			Pattern:     regexp.MustCompile(`^too (?:few|many) arguments in lambda expression\n\thave \((.+?)\)\n\twant \((.+?)\)$`),
			Translation: "lambda 表达式参数数量错误\n\t现有 ($1)\n\t需要 ($2)",
		},
		{
			Pattern:     regexp.MustCompile(`^cannot use lambda literal as type (.+?) in (.+?)$`),
			Translation: "无法将 lambda 字面量用作类型 $1 在 $2 中",
		},
		{
			Pattern:     regexp.MustCompile(`^lambda unsupport multiple assignment$`),
			Translation: "lambda 不支持多重赋值",
		},

		// 11. 特殊函数错误 (Special Function Errors)
		{
			Pattern:     regexp.MustCompile(`^func init must have no arguments and no return values$`),
			Translation: "func init 必须没有参数和返回值",
		},
		{
			Pattern:     regexp.MustCompile(`^use of builtin (.+?) not in function call$`),
			Translation: "内建函数 $1 的使用不在函数调用中",
		},

		// 12. Switch 语句错误 (Switch Statement Errors)
		{
			Pattern:     regexp.MustCompile(`^duplicate case (.+?) in (?:type )?switch$`),
			Translation: "switch 中重复的 case $1",
		},
		{
			Pattern:     regexp.MustCompile(`^multiple defaults in (?:type )?switch$`),
			Translation: "switch 中有多个 default",
		},
		{
			Pattern:     regexp.MustCompile(`^multiple nil cases in type switch$`),
			Translation: "类型 switch 中有多个 nil case",
		},

		// 13. 分支语句错误 (Branch Statement Errors)
		{
			Pattern:     regexp.MustCompile(`^fallthrough statement out of place$`),
			Translation: "fallthrough 语句位置错误",
		},
		{
			Pattern:     regexp.MustCompile(`^label (.+?) is not defined$`),
			Translation: "标签 $1 未定义",
		},
		{
			Pattern:     regexp.MustCompile(`^label (.+?) already defined$`),
			Translation: "标签 $1 已经定义",
		},

		// 14. 循环错误 (Loop Errors)
		{
			Pattern:     regexp.MustCompile(`^cannot assign type (.+?) to (.+?) \(type (.+?)\) in range$`),
			Translation: "无法在 range 中将类型 $1 赋值给 $2 (类型 $3)",
		},

		// 15. 数组错误 (Array Errors)
		{
			Pattern:     regexp.MustCompile(`^array index (\d+) out of bounds \[0:(\d+)\]$`),
			Translation: "数组索引 $1 超出范围 [0:$2]",
		},
		{
			Pattern:     regexp.MustCompile(`^cannot use (.+?) as index which must be non-negative integer constant$`),
			Translation: "无法将 $1 用作索引，索引必须是非负整数常量",
		},

		// 16. 切片错误 (Slice Errors)
		{
			Pattern:     regexp.MustCompile(`^cannot slice (.+?) \(type (.+?)\)$`),
			Translation: "无法切片 $1 (类型 $2)",
		},
		{
			Pattern:     regexp.MustCompile(`^invalid operation (.+?) \(3-index slice of (.+?)\)$`),
			Translation: "无效操作 $1 ($2 的 3-索引切片)",
		},

		// 17. 映射错误 (Map Errors)
		{
			Pattern:     regexp.MustCompile(`^missing key in map literal$`),
			Translation: "映射字面量中缺少键",
		},
		{
			Pattern:     regexp.MustCompile(`^invalid map literal$`),
			Translation: "无效的映射字面量",
		},
		{
			Pattern:     regexp.MustCompile(`^invalid composite literal type (.+?)$`),
			Translation: "无效的复合字面量类型 $1",
		},

		// 18. 结构体错误 (Struct Errors)
		{
			Pattern:     regexp.MustCompile(`^too (?:many|few) values in (.+?)\{.*?\}$`),
			Translation: "$1{...} 中值的数量错误",
		},
		{
			Pattern:     regexp.MustCompile(`^(.+?) undefined \(type (.+?) has no field or method (.+?)\)$`),
			Translation: "$1 未定义 (类型 $2 没有字段或方法 $3)",
		},

		// 19. 指针操作错误 (Pointer Operation Errors)
		{
			Pattern:     regexp.MustCompile(`^invalid indirect of (.+?) \(type (.+?)\)$`),
			Translation: "无效的间接引用 $1 (类型 $2)",
		},
		{
			Pattern:     regexp.MustCompile(`^cannot assign to (.+?) \((.+?) are immutable\)$`),
			Translation: "无法赋值给 $1 ($2 是不可变的)",
		},

		// 20. 包导入错误 (Package Import Errors)
		{
			Pattern:     regexp.MustCompile(`^package (.+?) is not in std$`),
			Translation: "包 $1 不在标准库中",
		},
		{
			Pattern:     regexp.MustCompile(`^no required module provides package (.+?)$`),
			Translation: "没有所需的模块提供包 $1",
		},
		{
			Pattern:     regexp.MustCompile(`^cannot refer to unexported name (.+?)$`),
			Translation: "无法引用未导出的名称 $1",
		},
		{
			Pattern:     regexp.MustCompile(`^(.+?) is not a type$`),
			Translation: "$1 不是一个类型",
		},

		// 21. XGo 特有错误 (XGo-Specific Errors)
		{
			Pattern:     regexp.MustCompile(`^operator \$(.+?) undefined$`),
			Translation: "操作符 $$$1 未定义",
		},
		{
			Pattern:     regexp.MustCompile(`^invalid (?:func|recv|method|overload) (.+?)$`),
			Translation: "无效的函数/接收器/方法/重载 $1",
		},
		{
			Pattern:     regexp.MustCompile(`^unknown func (.+?)$`),
			Translation: "未知函数 $1",
		},
		{
			Pattern:     regexp.MustCompile(`^can't send multiple values to a channel$`),
			Translation: "无法向通道发送多个值",
		},

		// 22. 编译错误 (Compilation Errors)
		{
			Pattern:     regexp.MustCompile(`^compile \x60(.+?)\x60: (.+?)$`),
			Translation: "编译 \x60$1\x60: $2",
		},
		{
			Pattern:     regexp.MustCompile(`^compileExpr failed: (.+?)$`),
			Translation: "compileExpr 失败: $1",
		},

		// 23. 类型推导错误 (Type Inference Errors)
		{
			Pattern:     regexp.MustCompile(`^expected '(.+?)', found '(.+?)'$`),
			Translation: "期望 '$1'，但发现 '$2'",
		},
		{
			Pattern:     regexp.MustCompile(`^expected (.+?), found (.+?)$`),
			Translation: "期望 $1，但发现 $2",
		},

		// 24. 无效操作 (Invalid Operations)
		{
			Pattern:     regexp.MustCompile(`^invalid operation: (.+?) \(type (.+?) does not support (.+?)\)$`),
			Translation: "无效操作: $1 (类型 $2 不支持 $3)",
		},
		{
			Pattern:     regexp.MustCompile(`^invalid operation (.+?)$`),
			Translation: "无效操作 $1",
		},
	}

	return &Translator{patterns: patterns}
}

// Translate translates an error message to the specified language
func (t *Translator) Translate(msg string, lang Language) string {
	// If language is English, return the original message
	if lang == LanguageEN {
		return msg
	}

	// If language is Chinese, try to match patterns and translate
	if lang == LanguageCN {
		return t.translateToChinese(msg)
	}

	// For unsupported languages, return the original message
	return msg
}

// translateToChinese attempts to translate an English error message to Chinese
func (t *Translator) translateToChinese(msg string) string {
	// Clean the message (remove extra whitespaces, normalize newlines)
	cleanMsg := strings.TrimSpace(msg)

	// Try to match each pattern
	for _, pattern := range t.patterns {
		if pattern.Pattern.MatchString(cleanMsg) {
			// Extract matches and replace with Chinese translation
			return pattern.Pattern.ReplaceAllString(cleanMsg, pattern.Translation)
		}
	}

	// If no pattern matches, return the original message
	return msg
}

// GetSupportedLanguages returns a list of supported languages
func (t *Translator) GetSupportedLanguages() []Language {
	return []Language{LanguageEN, LanguageCN}
}

// Global translator instance
var defaultTranslator = NewTranslator()

// Translate is a convenience function that uses the default translator
func Translate(msg string, lang Language) string {
	return defaultTranslator.Translate(msg, lang)
}
