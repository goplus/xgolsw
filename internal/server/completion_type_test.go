package server

import (
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletionContextInvalidCompositeElement(t *testing.T) {
	for _, tt := range []struct {
		name, source string
		typ          *gotypes.Basic
	}{
		{"ComplexInIntSlice", "1i", gotypes.Typ[gotypes.UntypedComplex]},
		{"TextInIntSlice", `"text"`, gotypes.Typ[gotypes.UntypedString]},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte("const entry = " + tt.source + "\n")})
			t.Cleanup(s.Close)
			proj := s.requestProject()
			info, err := proj.TypeInfo()
			require.NoError(t, err)
			file, err := proj.ASTFile("main.xgo")
			require.NoError(t, err)
			require.Len(t, file.Decls, 1)
			decl, ok := file.Decls[0].(*ast.GenDecl)
			require.True(t, ok)
			require.Len(t, decl.Specs, 1)
			spec, ok := decl.Specs[0].(*ast.ValueSpec)
			require.True(t, ok)
			require.Len(t, spec.Values, 1)
			element := spec.Values[0]
			require.NotNil(t, info.Types[element].Value)
			require.Same(t, tt.typ, info.TypeOf(element))
			ctx := completionContext{typeInfo: info}
			resolver := &autoPropertyResolver{proj: proj}
			want := gotypes.NewSlice(gotypes.Typ[gotypes.Int])

			literal := ctx.writtenLiteralValue(resolver, &ast.SliceLit{Elts: []ast.Expr{element}}, want)
			assert.Same(t, gotypes.Typ[gotypes.Invalid], literal.Type)
			composite := ctx.writtenCompositeValue(resolver, &ast.CompositeLit{Elts: []ast.Expr{element}}, want)
			assert.Same(t, gotypes.Typ[gotypes.Invalid], composite.Type)
			assert.Zero(t, resolver.codeBuilder().InternalStack().Len())
		})
	}
}

func TestServerTextDocumentCompletionIncompleteTypeDeclaration(t *testing.T) {
	for _, tt := range []struct{ name, source string }{
		{"OpeningBracket", "type Box["},
		{"ArrayLengthIdentifier", "type Box[T"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, kind := range []struct {
				name, filename string
				newServer      testServerFactory
			}{
				{"Plain", "main.xgo", newTestServer},
				{"ProjectClass", "main_fixture.gox", newFrameworkTestServer},
				{"WorkClass", "Worker_fixture.gox", newFrameworkTestServer},
			} {
				t.Run(kind.name, func(t *testing.T) {
					files := map[string][]byte{
						kind.filename: []byte(tt.source),
					}
					if kind.filename == "Worker_fixture.gox" {
						files["main_fixture.gox"] = nil
					}
					s := kind.newServer(t, files)
					items := completionItemsAt(t, s, kind.filename, Position{Character: uint32(len(tt.source))})
					labels := completionItemLabels(items)
					assert.Contains(t, labels, "int")
					assert.NotContains(t, labels, "Box")
				})
			}
		})
	}
}

func TestServerTextDocumentCompletionLineEndings(t *testing.T) {
	for _, ending := range []struct{ name, value string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
		t.Run(ending.name, func(t *testing.T) {
			t.Run("OverloadDeclaration", func(t *testing.T) {
				source, pos := typeDisplayTestSource(t, "func impl(value int) {};"+ending.value+"func R|ead = (\nimpl\n)\n")
				s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				t.Cleanup(s.Close)
				assert.Empty(t, completionItemsAt(t, s, "main.xgo", pos))
			})

			t.Run("ImportGroup", func(t *testing.T) {
				source, pos := typeDisplayTestSource(t, "import ("+ending.value+"\"example.com/s|upport/v2\""+ending.value+")\n")
				s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
				t.Cleanup(s.Close)
				ctx := newCompletionTestContext(t, s, "main.xgo", pos)
				assert.True(t, ctx.isInImportStringLit())
			})
		})
	}
}

func TestServerTextDocumentCompletionTypeArguments(t *testing.T) {
	for _, tt := range []struct {
		name, source, want string
		incomplete         bool
	}{
		{"Call", "var value string = api.Use[Candidate|](Candidate{})", "Candidate", false},
		{"Multiple", "var value string = api.Pair[int, Candidate|](1, Candidate{})", "Candidate", false},
		{"First", "var value string = api.Pair[Candidate|, int](Candidate{}, 1)", "Candidate", false},
		{"Return", "func run() string { return api.Use[Candidate|](Candidate{}) }", "Candidate", false},
		{"Incomplete", "var value string = api.Use[Cand|](Candidate{})", "Candidate", true},
		{"Empty", "var value string = api.Use[|](Candidate{})", "Candidate", true},
		{"Composite", "var value any = api.Box[Candidate|]{}", "Candidate", false},
		{"Declaration", "var value api.Box[Candidate|]", "Candidate", false},
		{"Conversion", "var value any = api.Box[Candidate|](api.Box[Candidate]{})", "Candidate", false},
		{"Alias", "var value api.Alias[Candidate|]", "Candidate", false},
		{"Qualified", "var value string = api.Use[api.Candidate|](api.Candidate{})", "Candidate", false},
		{"QualifiedIncomplete", "var value string = api.Use[api.Cand|](api.Candidate{})", "Candidate", true},
		{"Slice", "var value string = api.Use[[]Candidate|]([]Candidate{})", "Candidate", false},
		{"Pointer", "var value string = api.Use[*Candidate|](&Candidate{})", "Candidate", false},
		{"Array", "var value string = api.Use[[2]Candidate|]([2]Candidate{})", "Candidate", false},
		{"Map", "var value string = api.Use[map[string]Candidate|](map[string]Candidate{})", "Candidate", false},
		{"Function", "var value string = api.Use[func() Candidate|](nil)", "Candidate", false},
		{"Nested", "var value string = api.Use[api.Box[Candidate|]](api.Box[Candidate]{})", "Candidate", false},
		{"Package", "var value string = api.Use[Candidate|](Candidate{})", "api", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, kind := range []struct {
				name, filename string
				newServer      testServerFactory
			}{
				{"Plain", "main.xgo", newTestServer},
				{"ProjectClass", "main_fixture.gox", newFrameworkTestServer},
				{"WorkClass", "Worker_fixture.gox", newFrameworkTestServer},
			} {
				t.Run(kind.name, func(t *testing.T) {
					body := tt.source
					if !strings.HasPrefix(body, "func ") {
						body = "func run() {\n" + body + "\necho value\n}\n"
					}
					source, pos := typeDisplayTestSource(t, "import api \"example.com/api\"\n"+body+"\n")
					s := newGenericCompletionTestServer(t, kind.newServer, kind.filename, source)
					_, err := s.requestProject().TypeInfo()
					if tt.incomplete {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
					}
					items := completionItemsAt(t, s, kind.filename, pos)
					labels := completionItemLabels(items)
					assert.Contains(t, labels, tt.want)
					for _, name := range []string{"ordinaryValue", "ordinaryFunc", "OrdinaryValue", "Value", "println"} {
						assert.NotContains(t, labels, name)
					}
					for _, item := range items {
						assert.Contains(t, []CompletionItemKind{ClassCompletion, EnumCompletion, InterfaceCompletion, StructCompletion, ModuleCompletion}, item.Kind)
					}
				})
			}
		})
	}
}

func TestServerTextDocumentCompletionTypeArgumentValues(t *testing.T) {
	for _, tt := range []struct{ name, source, want, absent string }{
		{"ArrayLength", "var value string = api.Use[[candidate|Length]Candidate]([2]Candidate{})", "candidateLength", "Candidate"},
		{"ArrayLengthCall", "var value string = api.Use[[len(candidate|Text)]Candidate]([2]Candidate{})", "candidateText", ""},
		{"Index", "var value string = callbacks[candidate|Length]()", "candidateLength", "candidateText"},
		{"MapIndex", "var value string = mapping[candidate|Text]()", "candidateText", "candidateLength"},
		{"GenericSliceIndex", `var list api.List[func() string]
var value string = list[candidate|Length]()`, "candidateLength", "candidateText"},
		{"GenericMapIndex", `var dict api.Dict[string, func() string]
var value string = dict[candidate|Text]()`, "candidateText", "candidateLength"},
		{"CallArgument", "var value string = api.Use[int](candidate|Length)", "candidateLength", "candidateText"},
		{"CompositeField", "var value any = api.Box[int]{Val|}", "Value", "Candidate"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, "import api \"example.com/api\"\n"+tt.source+"\n")
			s := newGenericCompletionTestServer(t, newTestServer, "main.xgo", source)
			_, err := s.requestProject().TypeInfo()
			if tt.name == "CompositeField" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Contains(t, labels, tt.want)
			if tt.absent != "" {
				assert.NotContains(t, labels, tt.absent)
			}
		})
	}
}

func TestServerTextDocumentCompletionXGoxValueArguments(t *testing.T) {
	const declarations = `import dep "example.com/dependency"
var candidateNumber int
var candidateText string
var record dep.Record
`
	for _, tt := range []struct {
		name, call string
		incomplete bool
	}{
		{name: "Resolved", call: "dep.convertValue(int, candidate|Number)"},
		{name: "Overloaded", call: "(dep.convertPairOrType)(int, candidate|Number)"},
		{name: "BoundTemplate", call: "record.adapt(int, candidate|Number)"},
		{name: "InferredTail", call: `dep.keepFirst(int, candidate|Number, "text")`},
		{name: "EmptyArgument", call: "dep.convertValue(int, |)", incomplete: true},
		{name: "EmptyOverloadArgument", call: "(dep.convertPairOrType)(int, |)", incomplete: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, declarations+"_ = "+tt.call+"\n")
			s := newNotRecommendedImportServer(t, source)
			t.Cleanup(s.Close)
			if tt.incomplete {
				_, err := s.getProj().TypeInfo()
				require.Error(t, err)
			} else {
				requireNoDiagnostics(t, s)
			}

			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			assert.Contains(t, labels, "candidateNumber")
			assert.NotContains(t, labels, "candidateText")
		})
	}
}

func TestServerTextDocumentCompletionXGoxInferenceContext(t *testing.T) {
	const declarations = `import dep "example.com/dependency"
var candidateNumber int
var candidateText string
`
	for _, tt := range []struct {
		name, call, absent string
		want               []string
	}{
		{name: "Resolved", call: "dep.keepFirst(int, 1, candidate|Text)", want: []string{"candidateNumber", "candidateText"}},
		{name: "ResolvedPackageMember", call: "dep.keepFirst(int, 1, dep.Vis|ibleValue)", want: []string{"VisibleNumber", "VisibleValue"}},
		{name: "OverloadPackageMember", call: "(dep.keepFirstOrPair)(int, 1, dep.Vis|ibleValue)", want: []string{"VisibleNumber", "VisibleValue"}},
		{name: "SiblingArgument", call: "dep.keepSame(int, candidateNumber, candidate|Number)",
			want: []string{"candidateNumber"}, absent: "candidateText"},
		{name: "KeywordValue", call: "dep.keepWithOptions(int, candidateNumber, label = candidate|Number)",
			want: []string{"candidateNumber"}, absent: "candidateText"},
		{name: "KeywordSibling", call: "dep.keepWithOptions(int, candidate|Number, label = candidateNumber)",
			want: []string{"candidateNumber"}, absent: "candidateText"},
		{name: "OverloadSiblingArgument", call: "(dep.keepSameOrPair)(int, candidateNumber, candidate|Number)",
			want: []string{"candidateNumber"}, absent: "candidateText"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, declarations+"_ = "+tt.call+"\n")
			s := newNotRecommendedImportServer(t, source)
			t.Cleanup(s.Close)
			requireNoDiagnostics(t, s)

			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			for _, label := range tt.want {
				assert.Contains(t, labels, label)
			}
			if tt.absent != "" {
				assert.NotContains(t, labels, tt.absent)
			}
		})
	}
}

func TestServerTextDocumentCompletionOverloadTupleParameterShapes(t *testing.T) {
	const declarations = `type Pair (first int, second int)
func packed(pair Pair) {}
func expanded(first int, second string) {}
func use = (
	packed
	expanded
)
var candidateNumber int
var candidateText string
var candidateFlag bool
var candidate struct {
	Number int
	Text string
	Flag bool
}
`
	for _, tt := range []struct {
		name, expr, absent string
		want               []string
		incomplete         bool
	}{
		{name: "Identifier", expr: "candidateFl|ag", want: []string{"candidateNumber", "candidateText"}, absent: "candidateFlag", incomplete: true},
		{name: "Selector", expr: "candidate.Fl|ag", want: []string{"Number", "Text"}, absent: "Flag", incomplete: true},
		{name: "EmptyElement", expr: "|", want: []string{"candidateNumber", "candidateText"}, absent: "candidateFlag", incomplete: true},
		{name: "SelectedPacked", expr: "candidateNum|ber", want: []string{"candidateNumber"}, absent: "candidateText"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source, pos := typeDisplayTestSource(t, declarations+"use((1, "+tt.expr+"))\n")
			s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			t.Cleanup(s.Close)
			_, err := s.requestProject().TypeInfo()
			if tt.incomplete {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			labels := completionItemLabels(completionItemsAt(t, s, "main.xgo", pos))
			for _, label := range tt.want {
				assert.Contains(t, labels, label)
			}
			assert.NotContains(t, labels, tt.absent)
		})
	}
}

func TestCompletionItemSetSetExpectedTypes(t *testing.T) {
	for _, tt := range []struct {
		name     string
		function bool
	}{
		{name: "Value"},
		{name: "Function", function: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var complete gotypes.Type = gotypes.Typ[gotypes.Int]
			if tt.function {
				complete = gotypes.NewSignatureType(nil, nil, nil, nil, nil, false)
			}
			incomplete := gotypes.NewNamed(gotypes.NewTypeName(0, nil, "Box", nil), nil, nil)
			items := newCompletionItemSet(PlainText)
			items.setExpectedTypes([]gotypes.Type{incomplete})
			assert.False(t, items.matchesExpectedValue(gotypes.TypeAndValue{Type: complete}))
			assert.False(t, items.matchesExpectedValue(gotypes.TypeAndValue{Type: incomplete}))

			items.setExpectedTypes([]gotypes.Type{complete})
			assert.False(t, items.matchesExpectedValue(gotypes.TypeAndValue{Type: incomplete}))
			assert.True(t, items.matchesExpectedValue(gotypes.TypeAndValue{Type: complete}))
		})
	}
}

func newGenericCompletionTestServer(t *testing.T, factory testServerFactory, filename, source string) *Server {
	t.Helper()

	files := map[string][]byte{
		filename: []byte(source),
		"helpers.xgo": []byte(`type Candidate struct{}
var ordinaryValue int
func ordinaryFunc() int { return 0 }
const candidateLength = 2
const candidateText = "ab"
var callbacks []func() string
var mapping map[string]func() string
`),
	}
	if filename == "Worker_fixture.gox" {
		files["main_fixture.gox"] = nil
	}
	s := factory(t, files)
	t.Cleanup(s.Close)
	fset := token.NewFileSet()
	file, err := goparser.ParseFile(fset, "api.go", `package api
func Use[T any](value T) string { return "" }
func Pair[K, V any](key K, value V) string { return "" }
type Box[T any] struct { Value T }
type Alias[T any] = Box[T]
type List[T any] []T
type Dict[K comparable, V any] map[K]V
type Candidate struct{}
var OrdinaryValue int
`, 0)
	require.NoError(t, err)
	pkg, err := new(gotypes.Config).Check("example.com/api", fset, []*goast.File{file}, nil)
	require.NoError(t, err)
	fmtPkg := testframework.NewFmtPackage(t, fset)
	proj := s.getProj()
	base := proj.Importer
	proj.Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
		switch path {
		case pkg.Path():
			return pkg, nil
		case fmtPkg.Path():
			return fmtPkg, nil
		default:
			return base.Import(path)
		}
	})
	return s
}
