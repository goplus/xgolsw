package loopclosure

import (
	"go/types"
	"testing"

	"github.com/goplus/xgo/ast"
	"github.com/goplus/xgo/parser"
	"github.com/goplus/xgo/token"
	"github.com/goplus/xgo/x/typesutil"
	"github.com/goplus/xgolsw/internal/analysis/ast/inspector"
	"github.com/goplus/xgolsw/internal/analysis/passes/inspect"
	"github.com/goplus/xgolsw/internal/analysis/protocol"
	xgotypes "github.com/goplus/xgolsw/xgo/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoopclosure(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		wantDiag bool
	}{
		{
			name: "range value captured in goroutine",
			src: `
var s = []int{1, 2, 3}
for _, v := range s {
	go func() {
		println(v)
	}()
}
`,
			wantDiag: true,
		},
		{
			name: "for loop variable captured in goroutine",
			src: `
for i := 0; i < 3; i++ {
	go func() {
		println(i)
	}()
}
`,
			wantDiag: true,
		},
		{
			name: "range value captured in defer",
			src: `
var s = []int{1, 2, 3}
for _, v := range s {
	defer func() {
		println(v)
	}()
}
`,
			wantDiag: true,
		},
		{
			name: "loop variable not captured",
			src: `
var s = []int{1, 2, 3}
for _, v := range s {
	x := v
	go func() {
		println(x)
	}()
}
`,
			wantDiag: false,
		},
		{
			name: "goroutine with no loop variable use",
			src: `
var s = []int{1, 2, 3}
for range s {
	go func() {
		println("hello")
	}()
}
`,
			wantDiag: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "test.xgo", tt.src, parser.ParseComments)
			require.NoError(t, err)

			info := &xgotypes.Info{
				Info: typesutil.Info{
					Types: make(map[ast.Expr]types.TypeAndValue),
					Defs:  make(map[*ast.Ident]types.Object),
					Uses:  make(map[*ast.Ident]types.Object),
				},
			}

			checker := typesutil.NewChecker(
				&types.Config{Error: func(err error) {}},
				&typesutil.Config{
					Fset:  fset,
					Types: types.NewPackage("test", "test"),
				},
				nil,
				&info.Info,
			)

			if err := checker.Files(nil, []*ast.File{f}); err != nil {
				t.Log("type checking error:", err)
			}

			var diagnostics []protocol.Diagnostic
			pass := &protocol.Pass{
				Fset:      fset,
				Files:     []*ast.File{f},
				TypesInfo: info,
				Report: func(d protocol.Diagnostic) {
					diagnostics = append(diagnostics, d)
				},
				ResultOf: map[*protocol.Analyzer]any{
					inspect.Analyzer: inspector.New([]*ast.File{f}),
				},
			}

			_, err = Analyzer.Run(pass)
			require.NoError(t, err)

			for _, d := range diagnostics {
				t.Logf("got diagnostic: %v", d)
			}
			assert.Equal(t, tt.wantDiag, len(diagnostics) > 0)
		})
	}
}

func runLoopclosure(t *testing.T, src string) []protocol.Diagnostic {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.xgo", src, parser.ParseComments)
	require.NoError(t, err)

	info := &xgotypes.Info{
		Info: typesutil.Info{
			Types: make(map[ast.Expr]types.TypeAndValue),
			Defs:  make(map[*ast.Ident]types.Object),
			Uses:  make(map[*ast.Ident]types.Object),
		},
	}
	checker := typesutil.NewChecker(
		&types.Config{Error: func(error) {}},
		&typesutil.Config{Fset: fset, Types: types.NewPackage("test", "test")},
		nil, &info.Info,
	)
	if err := checker.Files(nil, []*ast.File{f}); err != nil {
		t.Log("type checking error:", err)
	}

	var diagnostics []protocol.Diagnostic
	pass := &protocol.Pass{
		Fset:      fset,
		Files:     []*ast.File{f},
		TypesInfo: info,
		Report:    func(d protocol.Diagnostic) { diagnostics = append(diagnostics, d) },
		ResultOf:  map[*protocol.Analyzer]any{inspect.Analyzer: inspector.New([]*ast.File{f})},
	}
	_, err = Analyzer.Run(pass)
	require.NoError(t, err)
	return diagnostics
}

func TestLoopclosureForEachLastStmt(t *testing.T) {
	// goroutine inside last if body (covers forEachLastStmt if branch)
	t.Run("captured inside if body", func(t *testing.T) {
		diags := runLoopclosure(t, `
var s = []int{1, 2}
for _, v := range s {
	if true {
		go func() { println(v) }()
	}
}
`)
		assert.NotEmpty(t, diags)
	})

	// goroutine inside if-else (covers else BlockStmt branch)
	t.Run("captured inside if-else", func(t *testing.T) {
		diags := runLoopclosure(t, `
var s = []int{1, 2}
for _, v := range s {
	if v > 0 {
		println("pos")
	} else {
		go func() { println(v) }()
	}
}
`)
		assert.NotEmpty(t, diags)
	})

	// goroutine inside switch case (covers forEachLastStmt switch branch)
	t.Run("captured inside switch case", func(t *testing.T) {
		diags := runLoopclosure(t, `
var s = []int{1, 2}
for _, v := range s {
	switch v {
	case 1:
		go func() { println(v) }()
	}
}
`)
		assert.NotEmpty(t, diags)
	})

	// goroutine inside nested range (covers forEachLastStmt range branch)
	t.Run("captured inside nested range", func(t *testing.T) {
		diags := runLoopclosure(t, `
var s = []int{1, 2}
for _, v := range s {
	for range s {
		go func() { println(v) }()
	}
}
`)
		assert.NotEmpty(t, diags)
	})

	// goroutine inside nested for (covers forEachLastStmt for branch)
	t.Run("captured inside nested for", func(t *testing.T) {
		diags := runLoopclosure(t, `
var s = []int{1, 2}
for _, v := range s {
	for i := 0; i < 1; i++ {
		go func() { println(v) }()
	}
}
`)
		assert.NotEmpty(t, diags)
	})

	// for post is IncDecStmt (covers addVar via IncDecStmt)
	t.Run("for incr variable captured", func(t *testing.T) {
		diags := runLoopclosure(t, `
for i := 0; i < 3; i++ {
	go func() { println(i) }()
}
`)
		assert.NotEmpty(t, diags)
	})
}
