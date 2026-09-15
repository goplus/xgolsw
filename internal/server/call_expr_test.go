package server

import (
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallArgValueTypes(t *testing.T) {
	type valueType struct {
		expr string
		typ  string
	}
	for _, tt := range []struct {
		name    string
		source  string
		want    []valueType
		wantErr bool
	}{
		{
			name: "Positional",
			source: `func use(name Name, count int) {}
use (("first")), 2
`,
			want: []valueType{{`"first"`, "main.Name"}, {"2", "int"}},
		},
		{
			name: "Slice",
			source: `func use(names []Name) {}
use ["first", ("second")]
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			name: "ParenthesizedSlice",
			source: `func use(names []Name) {}
use ((["first", "second"]))
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			// Matrix literals retain argument context even though the compiler
			// does not type-check them yet.
			name: "Matrix",
			source: `func use(names []Name) {}
use [
    "first"
    "second"
]
`,
			want:    []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
			wantErr: true,
		},
		{
			name: "ParenthesizedMatrix",
			source: `func use(names []Name) {}
use (([
    "first"
    "second"
]))
`,
			want:    []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
			wantErr: true,
		},
		{
			name: "Variadic",
			source: `func use(prefix int, names ...Name) {}
use 1, "first", ("second")
`,
			want: []valueType{{"1", "int"}, {`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			name: "VariadicSlice",
			source: `func use(names ...Name) {}
use ["first", "second"]...
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			name: "ParenthesizedVariadicSlice",
			source: `func use(names ...Name) {}
use ((["first", "second"]))...
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			name: "SliceAlias",
			source: `type Names = []Name
func use(names Names) {}
use ["first"]
`,
			want: []valueType{{`"first"`, "main.Name"}},
		},
		{
			name: "DefinedSlice",
			source: `type Names []Name
func use(names Names) {}
use (["first"])
`,
			want: []valueType{{`"first"`, "main.Name"}},
		},
		{
			name: "SliceVariable",
			source: `func use(names []Name) {}
var names []Name
use (names)
`,
			want: []valueType{{"names", "[]main.Name"}},
		},
		{
			name: "StructKwargs",
			source: `type Options struct {
    Name Name
    Names []Name
    Count int
}
func use(opts Options?) {}
use name = ("first"), names = (["second", "third"]), count = 4
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}, {`"third"`, "main.Name"}, {"4", "int"}},
		},
		{
			name: "AliasKwarg",
			source: `type Alias = Name
type Options struct { Name Alias }
func use(opts Options?) {}
use name = "first"
`,
			want: []valueType{{`"first"`, "main.Alias"}},
		},
		{
			name: "MapKwargs",
			source: `func use(opts map[string]Name?) {}
use first = "first", second = ("second")
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			name: "InterfaceKwargs",
			source: `type Options interface { Name(name Name) Options }
type Client struct{}
func (c Client) Options() Options { return nil }
func (c Client) use(opts Options?) {}
var client Client
client.use name = ("first")
`,
			want: []valueType{{`"first"`, "main.Name"}},
		},
		{
			name: "OverloadKwargs",
			source: `type Options struct { Names []Name }
type Worker struct{}
func (w *Worker) useNames(opts Options?) {}
func (Worker).use = (
    (Worker).useNames
)
var worker Worker
worker.use names = (["first", "second"])
`,
			want: []valueType{{`"first"`, "main.Name"}, {`"second"`, "main.Name"}},
		},
		{
			name: "UnresolvedOverload",
			source: `type Options struct { Names []Name }
type Worker struct{}
func (w *Worker) useInt(prefix int, opts Options?) {}
func (w *Worker) useString(prefix string, opts Options?) {}
func (Worker).use = (
    (Worker).useInt
    (Worker).useString
)
var worker Worker
worker.use missing, names = (["first"])
`,
			want:    []valueType{{"missing", "int"}, {`"first"`, "main.Name"}, {"missing", "string"}, {`"first"`, "main.Name"}},
			wantErr: true,
		},
		{
			name: "UnknownKwarg",
			source: `type Options struct { Name Name }
func use(opts Options?) {}
use unknown = "ignored", name = "first"
`,
			want:    []valueType{{`"first"`, "main.Name"}},
			wantErr: true,
		},
		{
			name: "NonSliceCollection",
			source: `func use(name Name) {}
use ["ignored"]
`,
			wantErr: true,
		},
		{
			name: "DefinedElementType",
			source: `type Label string
func use(names []Label) {}
use ["first", "second"]
`,
			want: []valueType{{`"first"`, "main.Label"}, {`"second"`, "main.Label"}},
		},
		{
			name: "EmptyCollection",
			source: `func use(names []Name) {}
use []
`,
		},
		{
			name:   "NoArguments",
			source: "func use() {}\nuse()\n",
		},
		{
			name:    "UnresolvedCall",
			source:  `use "ignored"`,
			wantErr: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := "type Name = string\n" + tt.source
			s := newTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
			proj := s.getProj()
			info, err := proj.TypeInfo()
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.NotNil(t, info)
			file, err := proj.ASTFile("main.xgo")
			require.NoError(t, err)
			var call *ast.CallExpr
			ast.Inspect(file, func(node ast.Node) bool {
				if expr, ok := node.(*ast.CallExpr); ok {
					call = expr
					return false
				}
				return true
			})
			require.NotNil(t, call)
			var got []valueType
			for expr, typ := range callArgValueTypes(proj, info, call) {
				file := proj.Fset.File(expr.Pos())
				got = append(got, valueType{source[file.Offset(expr.Pos()):file.Offset(expr.End())], typ.String()})
			}
			assert.Equal(t, tt.want, got)
			count := 0
			for range callArgValueTypes(proj, info, call) {
				count++
				break
			}
			assert.Equal(t, min(1, len(tt.want)), count)
		})
	}
}
