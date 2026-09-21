package server

import (
	goast "go/ast"
	goparser "go/parser"
	gotypes "go/types"
	"strings"
	"testing"

	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		{"GenericSliceIndex", "var list api.List[func() string]\nvar value string = list[candidate|Length]()", "candidateLength", "candidateText"},
		{"GenericMapIndex", "var dict api.Dict[string, func() string]\nvar value string = dict[candidate|Text]()", "candidateText", "candidateLength"},
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
	base := s.getProj().Importer
	s.getProj().Importer = testImporterFunc(func(path string) (*gotypes.Package, error) {
		if path == pkg.Path() {
			return pkg, nil
		}
		return base.Import(path)
	})
	return s
}
