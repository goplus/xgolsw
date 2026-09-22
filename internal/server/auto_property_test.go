package server

import (
	gotypes "go/types"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerClassfileAutoProperties(t *testing.T) {
	for _, tt := range []struct {
		name, methods string
		want          bool
	}{
		{
			name: "ReceiverRejectsString",
			methods: `func XGot_App_Label__0(a interface{ Missing() }) string { return "" }
func XGot_App_Label__1(a any) int { return 0 }
`,
		},
		{
			name: "ReceiverAllowsString",
			methods: `func XGot_App_Label__0(a interface{ Missing() }) int { return 0 }
func XGot_App_Label__1(a any) string { return "" }
`,
			want: true,
		},
		{
			name:    "InvalidReceiver",
			methods: `func XGot_App_Label(a interface{ Missing() }) string { return "" }`,
		},
		{
			name: "UninferableOverload",
			methods: `func XGot_App_Label__0[T any](a *App) string { return "" }
func XGot_App_Label__1(a any) int { return 0 }
`,
		},
		{
			name:    "UninferableProperty",
			methods: `func XGot_App_Label[T any](a *App) string { return "" }`,
		},
		{
			name: "InferredResult",
			methods: `func (a *App) Value() string { return "" }
func XGot_App_Label[T any](a interface{ Value() T }) T { return a.Value() }
`,
			want: true,
		},
		{
			name: "InferredConstraint",
			methods: `func (a *App) Value() string { return "" }
func XGot_App_Label__0[T ~int](a interface{ Value() T }) int { return 0 }
func XGot_App_Label__1[T ~string](a interface{ Value() T }) T { return a.Value() }
`,
			want: true,
		},
		{
			name:    "EmbeddedPointerConversion",
			methods: `func XGot_App_Label(a *App) string { return "" }`,
			want:    true,
		},
		{
			name: "OptionalFirst",
			methods: `func (a *App) Label__0(__xgo_optional_value int) int { return 0 }
func (a *App) Label__1() string { return "" }
`,
		},
		{
			name: "VariadicFirst",
			methods: `func (a *App) Label__0(values ...int) int { return 0 }
func (a *App) Label__1() string { return "" }
`,
		},
		{
			name: "TupleFirst",
			methods: `func (a *App) Label__0() (string, int) { return "", 0 }
func (a *App) Label__1() string { return "" }
`,
		},
		{
			name: "VoidFirst",
			methods: `func (a *App) Label__0() {}
func (a *App) Label__1() string { return "" }
`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "func run() {\nuse |\"text\"\n}\n")
			s := newImportTestServer(t, map[string][]byte{
				"main_fixture.gox": []byte(source),
				"globals.xgo":      []byte("func Use(value string) {}\n"),
			})
			setImportTestFrameworkMethods(t, s, tt.methods)
			_, err := s.requestProject().TypeInfo()
			require.NoError(t, err)
			slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}}})
			require.NoError(t, err)
			require.Len(t, slots, 1)
			assert.Equal(t, tt.want, slices.Contains(slots[0].PredefinedNames, "label"), "input slot candidate")
			var version int
			for _, selector := range []bool{false, true} {
				name := "Bare"
				placeholder := `"text"`
				prefix := ""
				if selector {
					name = "Selector"
					placeholder = "this.Label"
					prefix = "this."
					source, pos = typeDisplayTestSource(t, "func run() {\nuse this.|Label\n}\n")
					version++
					s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(source), Version: version}})
				}
				t.Run(name, func(t *testing.T) {
					item := completionItemByLabel(completionItemsAt(t, s, "main_fixture.gox", pos), "label")
					assert.Equal(t, tt.want, item != nil, "completion candidate")
					insertion := "label"
					if item != nil {
						assert.Equal(t, insertion, item.InsertText)
						insertion = item.InsertText
					}
					version++
					s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte(strings.Replace(source, placeholder, prefix+insertion, 1)), Version: version}})
					_, err := s.requestProject().TypeInfo()
					if tt.want {
						require.NoError(t, err)
					} else {
						require.Error(t, err)
					}
				})
			}
		})
	}
}

func TestServerCompletionAutoPropertyFunctionValues(t *testing.T) {
	for _, tt := range []struct {
		name, methods, expected, want string
	}{
		{
			name:     "PropertyResult",
			methods:  `func (a *App) Label() func() string { return nil }`,
			expected: "func() string",
			want:     "label",
		},
		{
			name:     "MethodValue",
			methods:  `func (a *App) Label() func() string { return nil }`,
			expected: "func() func() string",
			want:     "Label",
		},
		{
			name: "InferredPropertyResult",
			methods: `func (a *App) Value() func() string { return nil }
func XGot_App_Label[T any](a interface{ Value() T }) T { return a.Value() }
`,
			expected: "func() string",
			want:     "label",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, ttSource := range []struct{ name, source, prefix string }{
				{"Bare", "use |nil", ""},
				{"Selector", "use this.|Label", "this."},
			} {
				t.Run(ttSource.name, func(t *testing.T) {
					source, pos := typeDisplayTestSource(t, "func run() {\n"+ttSource.source+"\n}\n")
					s := newImportTestServer(t, map[string][]byte{
						"main_fixture.gox": []byte(source),
						"globals.xgo":      []byte("func Use(value " + tt.expected + ") {}\n"),
					})
					setImportTestFrameworkMethods(t, s, tt.methods)
					items := completionItemsAt(t, s, "main_fixture.gox", pos)
					item := completionItemByLabel(items, tt.want)
					require.NotNil(t, item)
					assert.Equal(t, tt.want, item.InsertText)
					if tt.want == "Label" {
						assert.NotContains(t, completionItemLabels(items), "label")
					}
					s.ModifyFiles([]FileChange{{Path: "main_fixture.gox", Content: []byte("func run() { use " + ttSource.prefix + item.InsertText + " }\n"), Version: 1}})
					_, err := s.requestProject().TypeInfo()
					require.NoError(t, err)
				})
			}
		})
	}
}

func TestServerAutoPropertyAnalysisIsolation(t *testing.T) {
	source, pos := typeDisplayTestSource(t, "func run() {\nuse |\"text\"\n}\n")
	s := newImportTestServer(t, map[string][]byte{
		"main_fixture.gox": []byte(source),
		"globals.xgo":      []byte("func Use(value string) {}\n"),
	})
	setImportTestFrameworkMethods(t, s, `func XGot_App_Bad[T any](a *App) string { return "" }
func (a *App) Value() string { return "" }
func XGot_App_Label[T any](a interface{ Value() T }) T { return a.Value() }
`)
	proj := s.requestProject()
	info, err := proj.TypeInfo()
	require.NoError(t, err)
	astPkg, err := proj.ASTPackage()
	require.NoError(t, err)
	type scopeState struct {
		objects  []gotypes.Object
		children int
	}
	scopes := make(map[*gotypes.Scope]scopeState)
	var record func(*gotypes.Scope)
	record = func(scope *gotypes.Scope) {
		state := scopeState{children: scope.NumChildren()}
		for _, name := range scope.Names() {
			state.objects = append(state.objects, scope.Lookup(name))
		}
		scopes[scope] = state
		for i := range scope.NumChildren() {
			record(scope.Child(i))
		}
	}
	record(info.Pkg.Scope())
	for _, pkg := range info.Pkg.Imports() {
		record(pkg.Scope())
	}
	t.Run("ConcurrentRequests", func(t *testing.T) {
		for i := range 8 {
			t.Run("Request"+strconv.Itoa(i), func(t *testing.T) {
				t.Parallel()
				for range 3 {
					labels := completionItemLabels(completionItemsAt(t, s, "main_fixture.gox", pos))
					assert.Contains(t, labels, "label")
					assert.NotContains(t, labels, "bad")
					slots, err := s.xgoGetInputSlots([]XGoGetInputSlotsParams{{TextDocument: TextDocumentIdentifier{URI: "file:///main_fixture.gox"}}})
					require.NoError(t, err)
					require.Len(t, slots, 1)
					assert.Contains(t, slots[0].PredefinedNames, "label")
					assert.NotContains(t, slots[0].PredefinedNames, "bad")
				}
			})
		}
	})
	for scope, state := range scopes {
		assert.Equal(t, state.children, scope.NumChildren())
		assert.Len(t, scope.Names(), len(state.objects))
		for _, obj := range state.objects {
			assert.Same(t, obj, scope.Lookup(obj.Name()))
		}
	}
	currentInfo, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	assert.Same(t, info, currentInfo)
	currentAST, err := s.requestProject().ASTPackage()
	require.NoError(t, err)
	assert.Same(t, astPkg, currentAST)
}

func TestServerCompletionAutoPropertyVoid(t *testing.T) {
	source, pos := typeDisplayTestSource(t, "func run() {\nthis.|label\n}\n")
	s := newImportTestServer(t, map[string][]byte{"main_fixture.gox": []byte(source)})
	setImportTestFrameworkMethods(t, s, "func (a *App) Label() {}\n")
	_, err := s.requestProject().TypeInfo()
	require.NoError(t, err)
	item := completionItemByLabel(completionItemsAt(t, s, "main_fixture.gox", pos), "label")
	require.NotNil(t, item)
	assert.Equal(t, "label", item.InsertText)
}
