package server

import (
	"fmt"
	gotypes "go/types"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgolsw/xgo"
	"github.com/goplus/xgolsw/xgo/types"
	"github.com/goplus/xgolsw/xgo/xgoutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpressionTypeInfo(t *testing.T) {
	const source = `var Label = func() string { return "" }
func Use(value string) {}
func Keep(callback func() string) {}
use (label)
keep Label
_ = label()
_ = Label()
`
	t.Run("PreserveCompilerRecords", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		proj := s.requestProject()
		info, err := proj.TypeInfo()
		require.NoError(t, err)
		recordedTypes := maps.Clone(info.Types)
		recordedUses := maps.Clone(info.Uses)
		recordedDefs := maps.Clone(info.Defs)
		recordedOverloads := maps.Clone(info.Overloads)
		values, err := expressionTypeInfo(proj)
		require.NoError(t, err)
		require.NotEmpty(t, values.ImplicitCallTypes)
		assert.Empty(t, info.ImplicitCallTypes)
		assert.Equal(t, recordedTypes, info.Types)
		assert.Equal(t, recordedTypes, values.Types)
		assert.Equal(t, recordedUses, values.Uses)
		assert.Equal(t, recordedDefs, values.Defs)
		assert.Equal(t, recordedOverloads, values.Overloads)
		for expr, typ := range values.ImplicitCallTypes {
			assert.Same(t, gotypes.Typ[gotypes.String], typ)
			if ident, ok := expr.(*ast.Ident); ok {
				assert.Same(t, info.ObjectOf(ident), values.ObjectOf(ident))
				assert.IsType(t, (*gotypes.Signature)(nil), values.ObjectOf(ident).Type())
			}
		}
		callbacks := 0
		for ident, obj := range values.Uses {
			if obj.Name() == "Label" && values.ImplicitCallTypes[ident] == nil {
				callbacks++
				assert.Same(t, obj.Type(), values.TypeOf(ident))
			}
		}
		assert.Equal(t, 3, callbacks)
		again, err := expressionTypeInfo(s.requestProject())
		require.NoError(t, err)
		assert.Same(t, values, again)
		snapshot, err := expressionTypeInfo(proj.Snapshot())
		require.NoError(t, err)
		assert.Same(t, values, snapshot)
	})

	t.Run("ConcurrentColdCache", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		proj := s.requestProject()
		var results [8]*types.Info
		var errs [8]error
		var wg sync.WaitGroup
		for i := range results {
			wg.Go(func() { results[i], errs[i] = expressionTypeInfo(proj) })
		}
		wg.Wait()
		require.NotNil(t, results[0])
		require.NotEmpty(t, results[0].ImplicitCallTypes)
		for i := range results {
			require.NoError(t, errs[i])
			assert.Same(t, results[0], results[i])
		}
	})

	t.Run("SharedSourceCache", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		var builds atomic.Int32
		s.getProj().RegisterCacheBuilder(expressionTypesCacheKind{}, func(proj *xgo.Project) (any, error) {
			builds.Add(1)
			return buildExpressionTypesCache(proj)
		})
		_, err := sourceInfoForProject(s.requestProject())
		require.NoError(t, err)
		view, err := expressionTypeInfo(s.requestProject())
		require.NoError(t, err)
		require.NotEmpty(t, view.ImplicitCallTypes)
		assert.EqualValues(t, 1, builds.Load())
	})

	t.Run("RetainOldRevision", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source)})
		oldProject := s.requestProject()
		oldInfo, err := expressionTypeInfo(oldProject)
		require.NoError(t, err)
		updated := strings.ReplaceAll(strings.ReplaceAll(source, "string", "int"), `""`, "0")
		s.ModifyFiles([]FileChange{{Path: "main.xgo", Content: []byte(updated), Version: 1}})
		proj := s.requestProject()
		_, err = proj.TypeInfo()
		require.NoError(t, err)
		values, err := expressionTypeInfo(proj)
		require.NoError(t, err)
		assert.NotSame(t, oldInfo, values)
		require.NotEmpty(t, values.ImplicitCallTypes)
		for _, typ := range values.ImplicitCallTypes {
			assert.Same(t, gotypes.Typ[gotypes.Int], typ)
		}
		retained, err := expressionTypeInfo(oldProject)
		require.NoError(t, err)
		assert.Same(t, oldInfo, retained)
		for _, typ := range retained.ImplicitCallTypes {
			assert.Same(t, gotypes.Typ[gotypes.String], typ)
		}
	})

	t.Run("PartialTypes", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{"main.xgo": []byte(source + "_ = missing\n")})
		proj := s.requestProject()
		_, err := proj.TypeInfo()
		require.Error(t, err)
		info, err := expressionTypeInfo(proj)
		require.NoError(t, err)
		require.NotEmpty(t, info.ImplicitCallTypes)
		for _, typ := range info.ImplicitCallTypes {
			assert.Same(t, gotypes.Typ[gotypes.String], typ)
		}
	})

	t.Run("SourceContexts", func(t *testing.T) {
		for _, tt := range []struct{ name, body, want string }{
			{"Chain", "_ = ((record.next)).la|bel", "string"},
			{"Call", "_ = ((record.next).la|bel)()", "func() string"},
			{"MethodValue", "_ = record.La|bel", "func() string"},
			{"MethodExpression", "_ = Record.la|bel", "func() string"},
			{"FunctionResult", "_ = record.call|back", "func() string"},
			{"FunctionResultCall", "_ = record.call|back()", "func() func() string"},
			{"Lambda", "apply => { _ = la|bel }", "string"},
			{"Comprehension", "_ = [la|bel for value <- [1, 2]]", "string"},
			{"Interpolation", `_ = "${la|bel}"`, "string"},
			{"Collection", "take [la|bel]", "string"},
		} {
			t.Run(tt.name, func(t *testing.T) {
				source, pos := typeDisplayTestSource(t, "func run() { "+tt.body+" }\n")
				s := newImportTestServer(t, map[string][]byte{
					"main.xgo": []byte(source),
					"types.xgo": []byte(`type Record struct{}
func (Record) Next() Record { return Record{} }
func (Record) Label() string { return "" }
func (Record) Callback() func() string { return nil }
var record Record
func Label() string { return "" }
func apply(f func()) {}
func take(values []string) {}
`),
				})
				proj := s.requestProject()
				info, err := proj.TypeInfo()
				require.NoError(t, err)
				file, err := proj.ASTFile("main.xgo")
				require.NoError(t, err)
				ident := xgoutil.IdentAtPosition(proj.Fset, info, file, ToPosition(proj, file, pos))
				require.NotNil(t, ident)
				values, err := expressionTypeInfo(proj)
				require.NoError(t, err)
				typ := values.TypeOf(ident)
				require.NotNil(t, typ)
				assert.Equal(t, tt.want, gotypes.TypeString(typ, nil))
			})
		}
	})

	t.Run("ReceiverInstantiations", func(t *testing.T) {
		s := newImportTestServer(t, map[string][]byte{
			"number.xgo": []byte("import f \"example.com/framework\"\nvar number f.Box[int]\nfunc useNumber() { _ = number.value; _ = f.Box[int].value }\n"),
			"text.xgo":   []byte("import f \"example.com/framework\"\nvar text f.Box[string]\nfunc useText() { _ = text.value; _ = (f.Box[string]).value; _ = (*f.Box[string]).pointerValue }\n"),
		})
		setImportTestFrameworkMethods(t, s, "type Box[T any] struct { Item T }\nfunc (b Box[T]) Value() T { return b.Item }\nfunc (b *Box[T]) PointerValue() T { return b.Item }\n")
		proj := s.requestProject()
		_, err := proj.TypeInfo()
		require.NoError(t, err)
		values, err := expressionTypeInfo(proj)
		require.NoError(t, err)
		for _, tt := range []struct {
			filename, want string
			count          int
		}{{"number.xgo", "int", 2}, {"text.xgo", "string", 3}} {
			file, err := proj.ASTFile(tt.filename)
			require.NoError(t, err)
			found := 0
			ast.Inspect(file, func(node ast.Node) bool {
				if selector, ok := node.(*ast.SelectorExpr); ok && (selector.Sel.Name == "value" || selector.Sel.Name == "pointerValue") {
					found++
					typ := values.TypeOf(selector)
					if _, variable := selector.X.(*ast.Ident); variable {
						assert.Equal(t, tt.want, gotypes.TypeString(typ, nil))
					} else {
						sig, ok := typ.(*gotypes.Signature)
						require.True(t, ok)
						assert.Equal(t, tt.want, gotypes.TypeString(sig.Results().At(0).Type(), nil))
						assert.Same(t, sig, values.Info.TypeOf(selector))
					}
				}
				return true
			})
			assert.Equal(t, tt.count, found, tt.filename)
		}
	})
}

func BenchmarkBuildExpressionTypesCache(b *testing.B) {
	for _, tt := range []struct{ name, filename, declarations, expression string }{
		{"Package", "main.xgo", "func Label() int { return 0 }", "label"},
		{"Classfile", "main_fixture.gox", "func Label() int { return 0 }", "label"},
		{"Member", "main.xgo", "type Record struct{}\nfunc (Record) Label() int { return 0 }\nvar record Record", "record.label"},
		{"Chain", "main.xgo", "type Record struct{}\nfunc (Record) Next() Record { return Record{} }\nfunc (Record) Label() int { return 0 }\nvar record Record", "record.next.label"},
	} {
		for _, count := range []int{100, 1000, 3000} {
			b.Run(fmt.Sprintf("%s/Uses%d", tt.name, count), func(b *testing.B) {
				source := tt.declarations + "\nfunc run() {\n" + strings.Repeat("_ = "+tt.expression+"\n", count) + "}\n"
				s := newImportTestServer(b, map[string][]byte{tt.filename: []byte(source)})
				proj := s.requestProject()
				_, err := proj.TypeInfo()
				require.NoError(b, err)
				// Measure building a fresh value-type view of completed compiler data.
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					_, err := buildExpressionTypesCache(proj)
					require.NoError(b, err)
				}
			})
		}
	}
}
