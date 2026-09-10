package server

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerTextDocumentCompletionContext(t *testing.T) {
	t.Run("CrossFileSourceKinds", func(t *testing.T) {
		for _, tt := range []struct {
			name         string
			filename     string
			needsProject bool
		}{
			{name: "XGo", filename: "main.xgo"},
			{name: "LegacyXGo", filename: "main.gop"},
			{name: "StandaloneClass", filename: "Record.gox"},
			{name: "ProjectClass", filename: "main_fixture.gox"},
			{name: "WorkClass", filename: "Worker_fixture.gox", needsProject: true},
		} {
			t.Run(tt.name, func(t *testing.T) {
				files := map[string][]byte{
					"values.xgo": []byte(`// candidateInt returns an integer.
func candidateInt() int { return 1 }
func candidateBool() bool { return true }
func candidatePair() (int, bool) { return 1, true }
func candidateVoid() {}
`),
					tt.filename: []byte(`func result() int {
	return candidateInt()
}
`),
				}
				if tt.needsProject {
					files["main_fixture.gox"] = nil
				}
				s := newTestServer(t, files)
				_, err := s.workspaceRootFS.TypeInfo()
				require.NoError(t, err)

				items := completionItemsAt(t, s, tt.filename, Position{Line: 1, Character: 12})
				labels := completionItemLabels(items)
				assert.Contains(t, labels, "candidateInt")
				for _, label := range []string{"candidateBool", "candidatePair", "candidateVoid"} {
					assert.NotContains(t, labels, label)
				}
				item := completionItemByLabel(items, "candidateInt")
				require.NotNil(t, item)
				assert.Equal(t, FunctionCompletion, item.Kind)
				require.NotNil(t, item.Documentation)
				doc := requireValueAs[MarkupContent](t, item.Documentation.Value)
				assert.Contains(t, doc.Value, "candidateInt returns an integer.")
			})
		}
	})

	t.Run("ClassfileContexts", func(t *testing.T) {
		for _, class := range []struct {
			name     string
			filename string
			callback string
		}{
			{name: "Project", filename: "main_fixture.gox", callback: `onEvent "tick", value => {`},
			{name: "Work", filename: "Worker_fixture.gox", callback: "onValue value => {"},
		} {
			t.Run(class.name, func(t *testing.T) {
				for _, tt := range []struct {
					name   string
					body   string
					want   []string
					absent []string
				}{
					{
						name: "FieldAssignment",
						body: `func run() {
	count = candidateInt()
}
`,
						want:   []string{"candidateInt"},
						absent: []string{"candidateBool", "candidatePair", "candidateTriple", "candidateVoid"},
					},
					{
						name: "MultipleFieldAssignment",
						body: `func run() {
	count, flag = candidatePair()
}
`,
						want:   []string{"candidateInt", "candidatePair"},
						absent: []string{"candidateBool", "candidateTriple", "candidateVoid"},
					},
					{
						name: "InferredCallbackParameter",
						body: class.callback + `
	value = candidateInt()
}
`,
						want:   []string{"candidateInt"},
						absent: []string{"candidateBool", "candidatePair", "candidateTriple", "candidateVoid"},
					},
					{
						name:   "UTF16Position",
						body:   class.callback + "\r\n\t/* \U0001f600 */ value = candidateInt()\r\n}\r\n",
						want:   []string{"candidateInt"},
						absent: []string{"candidateBool", "candidatePair", "candidateTriple", "candidateVoid"},
					},
					{
						name: "CallbackNestedReturn",
						body: class.callback + `
	fn := func() int {
		return candidateInt()
	}
	echo fn(), value
}
`,
						want:   []string{"candidateInt"},
						absent: []string{"candidateBool", "candidatePair", "candidateTriple", "candidateVoid"},
					},
				} {
					t.Run(tt.name, func(t *testing.T) {
						prefix := `var (
	count int
	flag bool
)
func candidateInt() int { return 1 }
func candidateBool() bool { return true }
func candidatePair() (int, bool) { return 1, true }
func candidateTriple() (int, bool, int) { return 1, true, 2 }
func candidateVoid() {}
`
						files := map[string][]byte{"main_fixture.gox": nil}
						files[class.filename] = []byte(prefix + tt.body)
						s := newTestServer(t, files)
						_, err := s.workspaceRootFS.TypeInfo()
						require.NoError(t, err)

						beforeCursor := prefix + tt.body[:strings.Index(tt.body, "candidate")+len("cand")]
						items := completionItemsAt(t, s, class.filename, Position{
							Line:      uint32(strings.Count(beforeCursor, "\n")),
							Character: uint32(UTF16Len(beforeCursor[strings.LastIndex(beforeCursor, "\n")+1:])),
						})
						labels := completionItemLabels(items)
						for _, label := range tt.want {
							assert.Contains(t, labels, label)
						}
						for _, label := range tt.absent {
							assert.NotContains(t, labels, label)
						}
					})
				}
			})
		}
	})

	t.Run("XGoStyleMapLit", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func printMap(m map[string]int) {
	echo m
}

func run() {
	var foo int
	printMap {
		"foo": f
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 8, Character: 10}) // After "f"
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "foo")
	})

	t.Run("XGoStyleMapLitWithMultipleValues", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func printMap(m map[string]string) {
	echo m
}

func run() {
	var bar, baz string
	printMap {
		"first": bar,
		"second": b
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 13}) // After "b" in second value
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "bar")
		assert.Contains(t, labels, "baz")
	})

	t.Run("XGoStyleNestedMapLit", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func processData(data map[string]map[string]int) {
	echo data
}

func run() {
	var count int
	processData {
		"nested": {
			"value": c
		}
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 13}) // After "c" in nested map
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "count")
	})

	t.Run("XGoMapLiteralWithoutType", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func printData(data any) {
	echo data
}

func run() {
	var myVar string
	printData {
		"name": m
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 8, Character: 11}) // After "m" in map value
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "myVar")
	})

	t.Run("RegularStructLitNotAffected", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Config struct {
	Name  string
	Value int
}

func setup(cfg Config) {
	echo cfg
}

func run() {
	var myName string
	setup Config{
		Name: m
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 13, Character: 9}) // After "m" in struct field value
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "myName")
	})

	t.Run("TypedMapLiteral", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func run() {
	var value int
	var data map[string]int
	data = map[string]int{
		"key": value
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 5, Character: 14}) // After "value" in map value
		assert.NotEmpty(t, items)
	})

	t.Run("TypedMapLiteralAsArgument", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func processMap(m map[string]int) {
	echo m
}

func run() {
	var num int
	processMap map[string]int{
		"count": n
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 8, Character: 12}) // After "n" in map value
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "num")
	})

	t.Run("StructLitFieldValue", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type MyStruct struct {
	Field1 string
	Field2 int
}

func run() {
	var s MyStruct
	s = MyStruct{
		F
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 3}) // After "F" in struct literal
		require.NotEmpty(t, items)
		hasField1 := containsCompletionItemLabel(items, "Field1")
		hasField2 := containsCompletionItemLabel(items, "Field2")
		assert.True(t, hasField1 || hasField2, "Should suggest at least one struct field")
	})

	t.Run("SimpleReturn", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func getName() string {
	var str string = "myName"
	return s
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 3, Character: 9}) // After "s" in return
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "str")
		assert.Contains(t, labels, "string")
	})

	t.Run("SimpleAssign", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func run() {
	var str string = "myName"
	str = s
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 3, Character: 8}) // After "s" in assignment
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "str")
		assert.Contains(t, labels, "string")
	})

	t.Run("SimpleCallArg", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func run() {
	var str string = "myName"
	println(s)
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 3, Character: 10}) // After "s" in call argument
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "str")
		assert.Contains(t, labels, "string")
	})

	t.Run("TypedMapLitInReturn", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func countFunc() int { return 42 }
func countFuncNoReturnValue() {}
func countFuncMultiReturnValues() (int, int) { return 0, 1 }

func getData() map[string]int {
	var count int = 42
	return map[string]int{
		"total": c
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 8, Character: 12}) // After "c" in map value
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "count")
		assert.Contains(t, labels, "countFunc")
		assert.NotContains(t, labels, "countFuncNoReturnValue")
		assert.NotContains(t, labels, "countFuncMultiReturnValues")
	})

	t.Run("XGoStyleMapLitInReturn", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func appNameFunc() string { return "myApp" }
func appNameFuncNoReturnValue() {}
func appNameFuncMultiReturnValues() (string, string) { return "app1", "app2" }

func getConfig() map[string]string {
	var appName = "myApp"
	return {
		"name": a
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 8, Character: 11}) // After "a" in map value
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "appName")
		assert.Contains(t, labels, "appNameFunc")
		assert.NotContains(t, labels, "appNameFuncNoReturnValue")
		assert.NotContains(t, labels, "appNameFuncMultiReturnValues")
	})

	t.Run("XGoStyleNestedMapLitInReturn", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func getNestedData() map[string]map[string]int {
	var total int = 100
	return {
		"stats": {
			"count": t
		}
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 5, Character: 13}) // After "t" in nested map value
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "total")
	})

	t.Run("MapLitInMultiReturn", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func getResult() (map[string]int, error) {
	var result int = 42
	return {
		"value": r
	}, nil
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 4, Character: 12}) // After "r" in map value
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "result")
	})

	t.Run("TypedStructLitInReturn", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Person struct {
	Name string
	Age  int
}

func getPerson() Person {
	var myName = "Alice"
	var myAge = 25
	return Person{
		Name: m
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 10, Character: 9}) // After "m" in struct field value
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "myName")
		assert.Contains(t, labels, "myAge")
	})

	t.Run("PointerStructLitInReturn", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type Config struct {
	Host string
	Port int
}

func getConfig() *Config {
	var defaultHost = "localhost"
	var defaultPort = 8080
	return &Config{
		Host: d
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 10, Character: 9}) // After "d" in struct field value
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "defaultHost")
		assert.Contains(t, labels, "defaultPort")
	})

	t.Run("FuncLiteral", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func run() {
	var myCallback = func(x int) int {
		var result = x * 2
		return r
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 4, Character: 10}) // After "r" in return statement
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "result")
	})

	t.Run("FuncLiteralAsArgument", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func process(fn func(int) int) {
	echo fn(10)
}

func run() {
	var multiplier = 3
	process func(x int) int {
		var product = x * multiplier
		return p
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 10}) // After "p" in return statement
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "product")
		assert.NotContains(t, labels, "process")
	})

	t.Run("SliceLiteral", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func run() {
	var first = 10
	var second = 20
	var nums = []int{
		f
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 5, Character: 3}) // After "f" in slice literal
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "first")
	})

	t.Run("SliceLiteralInReturn", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func getNumbers() []int {
	var num1 = 100
	var num2 = 200
	return []int{
		n
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 5, Character: 3}) // After "n" in slice literal
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "num1")
		assert.Contains(t, labels, "num2")
	})

	t.Run("XGoStyleSliceLiteral", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func printSlice(s []string) {
	echo s
}

func run() {
	var item1 = "hello"
	var item2 = "world"
	printSlice [
		i
	]
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 3}) // After "i" in slice literal
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "item1")
		assert.Contains(t, labels, "item2")
	})

	t.Run("XGoStyleMatrixLiteral", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func printMatrix(m [][]string) {
	echo m
}

func run() {
	var item1 = "hello"
	var item2 = "world"
	printMatrix [
		item1, i
		item2, ""
	]
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 10}) // After "i" in matrix literal
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "item1")
		assert.Contains(t, labels, "item2")
	})

	t.Run("XGoStyleSliceLiteralInReturn", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func getItems() []string {
	var item1 = "hello"
	var item2 = "world"
	return [
		i
	]
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 5, Character: 3}) // After "i" in slice literal
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "item1")
		assert.Contains(t, labels, "item2")
	})

	t.Run("NestedSliceLiteral", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func processMatrix(m [][]int) {
	echo m
}

func run() {
	var value = 42
	processMatrix [][]int{
		[]int{v},
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 8, Character: 9}) // After "v" in nested slice
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "value")
	})

	t.Run("ArrayLiteral", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func run() {
	var element1 = "a"
	var element2 = "b"
	var arr = [3]string{
		e
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 5, Character: 3}) // After "e" in array literal
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "element1")
		assert.Contains(t, labels, "element2")
	})

	t.Run("FuncLiteralInReturn", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func getHandler() func(int) int {
	var factor = 5
	return func(x int) int {
		var result = x * factor
		return r
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 5, Character: 10}) // After "r" in inner return
		assert.NotEmpty(t, items)
		assert.Contains(t, completionItemLabels(items), "result")
	})

	t.Run("VarDeclWithValue", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func run() {
	var str string = "test"
	var x string = s
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 3, Character: 17}) // After "s" in var declaration with value
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "str")
		assert.Contains(t, labels, "string")
	})

	t.Run("ConstDeclWithValue", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func run() {
	var str string = "test"
	const x = s
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 3, Character: 12}) // After "s" in const declaration
		assert.NotEmpty(t, items)

		labels := completionItemLabels(items)
		assert.Contains(t, labels, "str")
		assert.Contains(t, labels, "string")
	})

	t.Run("ShortVarDecl", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func run() {
	var str string = "test"
	x := s
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 3, Character: 7}) // After "s" in short var decl
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "str")
		assert.Contains(t, labels, "string")
	})

	t.Run("MultipleReceiverAssignment", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func getTwoValues() (string, int) { return "hello", 42 }
func getSingleValue() string { return "world" }
func getThreeValues() (string, int, bool) { return "test", 123, true }

func run() {
	var x string
	var y int
	x, y = g
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 8, Character: 9}) // After "g" in assignment
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "getTwoValues", "Should suggest function returning (string, int)")
		assert.Contains(t, labels, "getSingleValue", "Should suggest single value functions for flexible use")
		assert.NotContains(t, labels, "getThreeValues", "Should not suggest function returning three values")
	})

	t.Run("MultipleReceiverShortVarDecl", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func getTwoInts() (int, int) { return 1, 2 }
func getTwoStrings() (string, string) { return "a", "b" }
func getSingleInt() int { return 42 }

func run() {
	x, y := g
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 6, Character: 10}) // After "g" in short var decl
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "getTwoInts", "Should suggest functions returning two values (int, int)")
		assert.Contains(t, labels, "getTwoStrings", "Should suggest functions returning two values (string, string)")
		assert.Contains(t, labels, "getSingleInt", "Should suggest single value functions for flexible use")
	})

	t.Run("MultipleExpressionAssignment", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func getInt() int { return 1 }
func getString() string { return "hello" }
func getTwoInts() (int, int) { return 1, 2 }

func run() {
	var x int
	var y int
	x, y = getInt(), g
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 8, Character: 19}) // After "g" in second expression
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "getInt", "Should suggest function returning int for second position")
		assert.NotContains(t, labels, "getString", "Should not suggest function returning string")
		assert.NotContains(t, labels, "getTwoInts", "Should not suggest function returning multiple values")
	})

	t.Run("MultipleReceiverWithError", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
import "errors"

func getTwoValuesWithError() (string, error) { return "hello", nil }
func getSingleValueWithError() error { return nil }
func getThreeValues() (string, int, error) { return "test", 123, nil }

func run() {
	var s string
	var err error
	s, err = g
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 10, Character: 11}) // After "g" in assignment
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "getTwoValuesWithError", "Should suggest function returning (string, error)")
		assert.NotContains(t, labels, "getSingleValueWithError", "Should not suggest single value function with incompatible type")
		assert.NotContains(t, labels, "getThreeValues", "Should not suggest function returning three values")
	})

	t.Run("NestedMultipleReceiverAssignment", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func getTwoBools() (bool, bool) { return true, false }
func getSingleBool() bool { return true }

func run() {
	if x, y := g; x && y {
		// Inside if statement with short var decl
	}
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 5, Character: 13}) // After "g" in if statement init
		assert.NotEmpty(t, items)
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "getTwoBools", "Should suggest function returning two bools")
		assert.Contains(t, labels, "getSingleBool", "Should suggest single value functions for flexible use")
	})

	t.Run("TypeConversion", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
type UserID int
type OrderID int

func getUserID() UserID { return 123 }
func getOrderID() OrderID { return 456 }
func getInt() int { return 789 }

func run() {
	var id int = g
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 9, Character: 15}) // After "g"
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "getInt", "Should show exact type match")
		assert.Contains(t, labels, "getUserID", "Should show convertible type UserID")
		assert.Contains(t, labels, "getOrderID", "Should show convertible type OrderID")
	})

	t.Run("TypeConversionExclusion", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func getPort() int { return 8080 }
func getHost() string { return "localhost" }

func run() {
	var port int = g
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 5, Character: 17}) // After "g" in int assignment
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "getPort", "Should show int function")
		assert.NotContains(t, labels, "getHost", "Should not suggest string to int conversion")
	})

	t.Run("SelfReferenceInValueExpression", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func run() {
	var counter int = 10
	counter = counter + c
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 3, Character: 22}) // After "c"
		assert.Contains(t, completionItemLabels(items), "counter", "Should show counter in value expression")
	})

	t.Run("CombinedSingleReturns", func(t *testing.T) {
		s := newTestServer(t, map[string][]byte{
			"main.xgo": []byte(`
func getX() int { return 1 }
func getY() int { return 2 }
func getPair() (int, int) { return 1, 2 }

func run() {
	var x, y int
	x, y = g
}
`),
		})

		items := completionItemsAt(t, s, "main.xgo", Position{Line: 7, Character: 9}) // After "g"
		labels := completionItemLabels(items)
		assert.Contains(t, labels, "getPair", "Should show function with matching return count")
		assert.Contains(t, labels, "getX", "Should show single return for flexible use")
		assert.Contains(t, labels, "getY", "Should show single return for flexible use")
	})
}
