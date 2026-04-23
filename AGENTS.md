# AGENTS.md

Guidelines for AI coding agents working on this project.

## Project overview

This project is the language server implementation for [XBuilder](https://github.com/goplus/builder), providing LSP
features (completion, diagnostics, hover, etc.) for XGo source files.

## Code conventions

### Import alias conventions

In `xgo` and its subpackages, prefer the XGo package when a corresponding package exists for a standard library `go/*`
import. When these packages are needed under `xgo/`, keep the XGo package as the simple name.

- Use `ast` for `github.com/goplus/xgo/ast`
- Use `format` for `github.com/goplus/xgo/format`
- Use `parser` for `github.com/goplus/xgo/parser`
- Use `printer` for `github.com/goplus/xgo/printer`
- Use `scanner` for `github.com/goplus/xgo/scanner`
- Use `token` for `github.com/goplus/xgo/token`
- Use `types` for `github.com/goplus/xgolsw/xgo/types`

Only import the standard library counterpart when it is genuinely required. In that case, use a `go` prefix alias such
as `goast`, `goformat`, `goparser`, `goprinter`, `goscanner`, `gotoken`, or `gotypes`.

Do not use aliases such as `xgoast`, `xgotoken`, or `xgotypes` in `xgo/`. Apply the same convention in both production
code and unit tests.

## Testing conventions

### assert vs require

Use `require` when subsequent code depends on the assertion passing (e.g., dereferencing a pointer, accessing array
elements, calling methods on the value). Use `assert` for independent checks.

```go
// Good: use require when dereferencing afterward
user, err := GetUser(ctx, id)
require.NoError(t, err)
require.NotNil(t, user)
assert.Equal(t, "Alice", user.Name)  // user is dereferenced

// Good: use require.Len when accessing elements afterward
require.Len(t, items, 2)
assert.Equal(t, "first", items[0].Name)

// Bad: using assert when subsequent code depends on it
user, err := GetUser(ctx, id)
assert.NoError(t, err)  // If this fails, next line will panic
assert.Equal(t, "Alice", user.Name)
```

Never use `t.Fatal`, `t.Fatalf`, `t.Error`, or `t.Errorf` directly — always use `require` or `assert` instead.

```go
// Good
require.NoError(t, err)

// Bad
if err != nil {
    t.Fatal(err)
}
```

### Naming conventions

Use `want` instead of `expected` in variable names and messages:

```go
// Good
wantDiag := true
assert.Equal(t, wantDiag, len(diagnostics) > 0)

// Bad
expectedDiag := true
assert.Equal(t, expectedDiag, len(diagnostics) > 0)
```

### Semantic assertions

Use semantic assertion methods for clarity:

```go
// Good
assert.Empty(t, str)
assert.Len(t, items, 3)
assert.ErrorIs(t, err, ErrNotFound)

// Bad
assert.Equal(t, "", str)
assert.Equal(t, 3, len(items))
assert.True(t, errors.Is(err, ErrNotFound))
```

### Helper functions

Use `t.Helper()` as the first line in test helper functions so that failure messages show the correct caller location:

```go
func runAnalyzer(t *testing.T, src string) []protocol.Diagnostic {
    t.Helper()
    // setup code...
}
```

### Resource cleanup

Use `t.Cleanup()` instead of `defer` for resource cleanup. `t.Cleanup()` ensures cleanup runs even if the test panics,
and cleanup functions registered in subtests run after the subtest completes.

```go
// Good
t.Cleanup(server.Close)

// Bad
defer server.Close()
```

### Type assertions

Always check the `ok` value when using type assertions and use `require.True(t, ok)` before using the value:

```go
// Good
bytes, ok := v.([]byte)
require.True(t, ok)
assert.Equal(t, expected, string(bytes))

// Bad
assert.Equal(t, expected, string(v.([]byte)))
```

### Subtest independence

Each subtest should be independent and not share mutable state with other subtests. Use `t.Cleanup()` for cleanup and
create fresh resources in each subtest.
