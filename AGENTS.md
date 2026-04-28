# Project overview

This project is the language server implementation for [XBuilder](https://github.com/goplus/builder), providing LSP
features (completion, diagnostics, hover, etc.) for XGo source files.

# Code conventions

## Go version and language features

Follow the Go version declared in `go.mod`. Prefer recent Go features already used by this repository when they improve
readability, especially iterator-style APIs in code that already uses Go iterators.

## Iterator conventions

For traversal helpers, prefer `iter.Seq` or `iter.Seq2` with `for range` over walker-style callbacks.

## Doc comments

Top-level production Go functions, types, and nontrivial variables should have doc comments, including unexported
declarations. Test-only declarations are exempt. Do not add boilerplate comments for self-explanatory const values or
enum entries.

## Defensive checks

Do not add defensive nil checks, fallbacks, or retries unless they protect a real external boundary, incomplete AST or
type information, or a concrete known failure mode. Inside resolved helper paths, prefer relying on established local
invariants.

## Import alias conventions

When a `github.com/goplus/xgo/*` package corresponds to a standard library `go/*` package, prefer the XGo package and
keep it as the simple package name.

- Use `ast` for `github.com/goplus/xgo/ast`
- Use `doc` for `github.com/goplus/xgo/doc`
- Use `format` for `github.com/goplus/xgo/format`
- Use `parser` for `github.com/goplus/xgo/parser`
- Use `printer` for `github.com/goplus/xgo/printer`
- Use `scanner` for `github.com/goplus/xgo/scanner`
- Use `token` for `github.com/goplus/xgo/token`

Use `types` for the project-local XGo type wrapper `github.com/goplus/xgolsw/xgo/types`. It is not a
`github.com/goplus/xgo/*` package, but it represents XGo type information and follows the same simple-name preference.

Only import the standard library counterpart when it is genuinely required. In that case, use a `go` prefix alias such
as `goast`, `godoc`, `goformat`, `goparser`, `goprinter`, `goscanner`, `gotoken`, or `gotypes`, even when the XGo
counterpart is not imported in the same file.

For `go/doc`, use `godoc` when standard library APIs such as `New` or `Synopsis` are required, because
`github.com/goplus/xgo/doc` does not provide those APIs.

For `go/token`, prefer `github.com/goplus/xgo/token` for source positions and file sets such as `Pos`, `NoPos`,
`FileSet`, and `NewFileSet`, because those are aliases of the standard library types. Use `gotoken` only when the
standard library `go/token.Token` type is genuinely required, such as when switching on token values from a `go/ast`
node. `github.com/goplus/xgo/token.Token` is a distinct XGo token type.

Do not use aliases such as `xgoast`, `xgotoken`, or `xgotypes`. Apply the same convention in both production code and
unit tests.

# Generated data

After changing dependencies in `go.mod`, regenerate `internal/pkgdata/pkgdata.zip` by running:

```sh
go generate ./internal/pkgdata
```

# Testing conventions

## XGo fixtures

Test snippets for XGo behavior should match the existing XGo and classfile fixture style used in this repository. Do not
write XGo-specific fixtures as pure Go. Do not overfit fixtures to the full XGo spec when nearby tests intentionally use
a narrower syntax subset.

## assert vs require

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

Never use `t.Fatal`, `t.Fatalf`, `t.Error`, or `t.Errorf` directly. Always use `require` or `assert` instead.

```go
// Good
require.NoError(t, err)

// Bad
if err != nil {
    t.Fatal(err)
}
```

## Naming conventions

Use `want` instead of `expected` in variable names and messages:

```go
// Good
wantDiag := true
assert.Equal(t, wantDiag, len(diagnostics) > 0)

// Bad
expectedDiag := true
assert.Equal(t, expectedDiag, len(diagnostics) > 0)
```

## Semantic assertions

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

## Helper functions

Use `t.Helper()` as the first line in test helper functions so that failure messages show the correct caller location:

```go
func runAnalyzer(t *testing.T, src string) []protocol.Diagnostic {
    t.Helper()
    // setup code...
}
```

## Resource cleanup

Use `t.Cleanup()` instead of `defer` for resource cleanup. `t.Cleanup()` ensures cleanup runs even if the test panics,
and cleanup functions registered in subtests run after the subtest completes.

```go
// Good
t.Cleanup(server.Close)

// Bad
defer server.Close()
```

## Type assertions

Always check the `ok` value when using type assertions and use `require.True(t, ok)` before using the value:

```go
// Good
bytes, ok := v.([]byte)
require.True(t, ok)
assert.Equal(t, want, string(bytes))

// Bad
assert.Equal(t, want, string(v.([]byte)))
```

## Table-driven tests

When a test case table is used by a single loop, write the anonymous slice inline in the `range` clause instead of
assigning it to a separate local variable.

```go
// Good
for _, tt := range []struct {
    name string
    want bool
}{
    {name: "Enabled", want: true},
    {name: "Disabled"},
} {
    t.Run(tt.name, func(t *testing.T) {
        assert.Equal(t, tt.want, got)
    })
}

// Bad
tests := []struct {
    name string
    want bool
}{
    {name: "Enabled", want: true},
    {name: "Disabled"},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        assert.Equal(t, tt.want, got)
    })
}
```

## Subtest independence

Each subtest should be independent and not share mutable state with other subtests. Use `t.Cleanup()` for cleanup and
create fresh resources in each subtest.
