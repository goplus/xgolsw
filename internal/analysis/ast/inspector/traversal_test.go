package inspector

import (
	"reflect"
	"slices"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectorPreorder(t *testing.T) {
	t.Run("EmptyFiles", func(t *testing.T) {
		var got []ast.Node
		New(nil).Preorder(nil, func(n ast.Node) { got = append(got, n) })
		assert.Empty(t, got)
	})

	t.Run("AllNodes", func(t *testing.T) {
		files := newTraversalFiles(t)
		var want []ast.Node
		for _, file := range files {
			ast.Inspect(file, func(n ast.Node) bool {
				if n != nil {
					want = append(want, n)
				}
				return true
			})
		}
		var got []ast.Node
		New(files).Preorder(nil, func(n ast.Node) { got = append(got, n) })
		assert.Equal(t, want, got)
	})

	t.Run("EachNodeType", func(t *testing.T) {
		files := newTraversalFiles(t)
		inspect := New(files)
		var nodeTypes []reflect.Type
		for _, file := range files {
			ast.Inspect(file, func(n ast.Node) bool {
				if n != nil && !slices.Contains(nodeTypes, reflect.TypeOf(n)) {
					nodeTypes = append(nodeTypes, reflect.TypeOf(n))
				}
				return true
			})
		}
		for _, nodeType := range nodeTypes {
			t.Run(nodeType.Elem().Name(), func(t *testing.T) {
				var want []ast.Node
				for _, file := range files {
					ast.Inspect(file, func(n ast.Node) bool {
						if reflect.TypeOf(n) == nodeType {
							want = append(want, n)
						}
						return true
					})
				}
				var got []ast.Node
				inspect.Preorder([]ast.Node{want[0]}, func(n ast.Node) { got = append(got, n) })
				assert.Equal(t, want, got)
			})
		}
	})
}

func TestInspectorNodes(t *testing.T) {
	for _, tt := range []struct {
		name   string
		filter []ast.Node
		prune  bool
	}{
		{name: "AllNodes"},
		{name: "FilteredNodes", filter: []ast.Node{(*ast.FuncDecl)(nil), (*ast.Ident)(nil)}},
		{name: "PrunedNodes", prune: true},
		{name: "FilteredPrunedNodes", filter: []ast.Node{(*ast.FuncDecl)(nil), (*ast.Ident)(nil)}, prune: true},
		{name: "AbsentType", filter: []ast.Node{(*ast.BadDecl)(nil)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := newTraversalFiles(t)
			want := wantTraversalEvents(files, tt.filter, tt.prune)
			var got []traversalEvent
			New(files).Nodes(tt.filter, func(n ast.Node, push bool) bool {
				got = append(got, traversalEvent{node: n, push: push})
				return push && !(tt.prune && isSkippedFunction(n))
			})
			for i := range want {
				want[i].stack = nil
			}
			assert.Equal(t, want, got)
		})
	}
}

func TestInspectorWithStack(t *testing.T) {
	for _, tt := range []struct {
		name   string
		filter []ast.Node
		prune  bool
	}{
		{name: "AllNodes"},
		{name: "FilteredNodes", filter: []ast.Node{(*ast.FuncDecl)(nil), (*ast.Ident)(nil)}},
		{name: "PrunedNodes", prune: true},
		{name: "FilteredPrunedNodes", filter: []ast.Node{(*ast.FuncDecl)(nil), (*ast.Ident)(nil)}, prune: true},
		{name: "AbsentType", filter: []ast.Node{(*ast.BadDecl)(nil)}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			files := newTraversalFiles(t)
			var got []traversalEvent
			New(files).WithStack(tt.filter, func(n ast.Node, push bool, stack []ast.Node) bool {
				got = append(got, traversalEvent{node: n, push: push, stack: slices.Clone(stack)})
				return push && !(tt.prune && isSkippedFunction(n))
			})
			assert.Equal(t, wantTraversalEvents(files, tt.filter, tt.prune), got)
		})
	}
}

type traversalEvent struct {
	node  ast.Node
	push  bool
	stack []ast.Node
}

func isSkippedFunction(n ast.Node) bool {
	function, ok := n.(*ast.FuncDecl)
	return ok && function.Name.Name == "skip"
}

func wantTraversalEvents(files []*ast.File, filter []ast.Node, prune bool) []traversalEvent {
	matches := func(n ast.Node) bool {
		return len(filter) == 0 || slices.ContainsFunc(filter, func(pattern ast.Node) bool {
			return reflect.TypeOf(n) == reflect.TypeOf(pattern)
		})
	}
	var events []traversalEvent
	for _, file := range files {
		var stack []ast.Node
		ast.Inspect(file, func(n ast.Node) bool {
			if n == nil {
				n = stack[len(stack)-1]
				if matches(n) {
					events = append(events, traversalEvent{node: n, stack: slices.Clone(stack)})
				}
				stack = stack[:len(stack)-1]
				return false
			}
			stack = append(stack, n)
			if matches(n) {
				events = append(events, traversalEvent{node: n, push: true, stack: slices.Clone(stack)})
				if prune && isSkippedFunction(n) {
					stack = stack[:len(stack)-1]
					return false
				}
			}
			return true
		})
	}
	return events
}

func newTraversalFiles(t *testing.T) []*ast.File {
	t.Helper()

	fset := token.NewFileSet()
	var files []*ast.File
	for _, source := range []string{
		`// Package fixture exercises traversal.
package fixture
import "example.com/support"
type Runner interface { Run(int) string }
type Box struct { Value int }
func (box *Box) Run(value int) string {
	values := [...]int{1, 2, 3}
	copy := values[1:2:3]
	for index, value := range copy {
		if value < 0 { continue }
		switch index { case 0, 1: value++; default: value-- }
	}
	return support.Format(box.Value, values...)
}
func skip() { unreachable := 1 }
func run(ch chan int, incoming <-chan int, outgoing chan<- int, value any) {
	var consume func(...int)
	defer consume(1)
	go consume(2)
	pointer := &Box{Value: 3}
	var action Runner = pointer
	_ = action.Run(1)
	_ = value.(string)
	switch typed := value.(type) { case string: _ = typed; default: }
loop:
	for i := 0; i < 3; i++ {
		select {
		case item := <-incoming: outgoing <- item
		case ch <- 1: break loop
		default: goto done
		}
	}
done:
	support.Get[int, string](1, "x")
	_ = (func() {})
	_ = map[string]int{"x": 1}
}
`,
		`type Color const (Red = iota)
@retry
func decorated() { echo 1ms }
echo [1, 2], (1, "x")
echo [
	decorated()
	row...
]
`,
	} {
		file, err := parser.ParseFile(fset, "test.xgo", source, parser.ParseComments)
		require.NoError(t, err)
		files = append(files, file)
	}
	return files
}
