//go:build js && wasm

package main

import (
	"encoding/json"
	"syscall/js"
	"testing"
	"time"

	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseServerOptions(t *testing.T) {
	for _, tt := range []struct {
		name    string
		value   any
		message string
	}{
		{"Null", nil, "options must be an object"},
		{"Array", []any{}, "options must be an object"},
		{"String", "", "options must be an object"},
		{"NullClasses", map[string]any{"classfileConfig": nil}, "classfileConfig must be a string"},
		{"NumberClasses", map[string]any{"classfileConfig": 1}, "classfileConfig must be a string"},
		{"NullArchive", map[string]any{"pkgDataZip": nil}, "pkgDataZip must be a Uint8Array"},
		{"ArrayArchive", map[string]any{"pkgDataZip": []any{}}, "pkgDataZip must be a Uint8Array"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseServerOptions(js.ValueOf(tt.value))
			assert.EqualError(t, err, tt.message)
		})
	}
	t.Run("Omitted", func(t *testing.T) {
		options, err := parseServerOptions(js.ValueOf(map[string]any{}))
		require.NoError(t, err)
		assert.Empty(t, options.ClassfileConfig)
		assert.Nil(t, options.PkgData)
	})
	t.Run("ExplicitEmpty", func(t *testing.T) {
		options, err := parseServerOptions(js.ValueOf(map[string]any{
			"classfileConfig": "", "pkgDataZip": js.Global().Get("Uint8Array").New(0),
		}))
		require.NoError(t, err)
		assert.Empty(t, options.ClassfileConfig)
		require.NotNil(t, options.PkgData)
		packages, err := options.PkgData.ListPkgs()
		require.NoError(t, err)
		assert.NotContains(t, packages, testframework.PkgPath)
	})
	t.Run("CopiesBytes", func(t *testing.T) {
		archive := testframework.NewPkgDataZip(t)
		data := js.Global().Get("Uint8Array").New(len(archive))
		js.CopyBytesToJS(data, archive)
		options, err := parseServerOptions(js.ValueOf(map[string]any{"pkgDataZip": data}))
		require.NoError(t, err)
		data.SetIndex(0, 3)
		doc, err := options.PkgData.GetPkgDoc(testframework.PkgPath)
		require.NoError(t, err)
		assert.Equal(t, testframework.NewPkgDoc(t), doc)
	})
}

func TestNewXGoLanguageServer(t *testing.T) {
	provider := js.FuncOf(func(js.Value, []js.Value) any { return map[string]any{} })
	reply := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	t.Cleanup(provider.Release)
	t.Cleanup(reply.Release)
	for _, tt := range []struct {
		name    string
		args    []js.Value
		message string
	}{
		{"MissingArguments", nil, "expected 2 or 3 arguments"},
		{"ExtraArguments", []js.Value{provider.Value, reply.Value, js.Undefined(), js.Undefined()}, "expected 2 or 3 arguments"},
		{"InvalidProvider", []js.Value{js.Null(), reply.Value}, "filesProvider argument must be a function"},
		{"InvalidReply", []js.Value{provider.Value, js.Null()}, "messageReplier argument must be a function"},
		{"InvalidOptions", []js.Value{provider.Value, reply.Value, js.Null()}, "options must be an object"},
		{"InvalidConfig", []js.Value{provider.Value, reply.Value, js.ValueOf(map[string]any{"classfileConfig": "project"})}, "invalid classfile configuration"},
		{"InvalidArchive", []js.Value{provider.Value, reply.Value, js.ValueOf(map[string]any{"pkgDataZip": js.Global().Get("Uint8Array").New(1)})}, "invalid package data"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err, ok := NewXGoLanguageServer(js.Undefined(), tt.args).(error)
			require.True(t, ok)
			assert.ErrorContains(t, err, tt.message)
		})
	}
	for _, args := range [][]js.Value{
		{provider.Value, reply.Value},
		{provider.Value, reply.Value, js.Undefined()},
	} {
		_, ok := NewXGoLanguageServer(js.Undefined(), args).(js.Value)
		assert.True(t, ok)
	}
}

func TestXGoLanguageServerInstanceConfiguration(t *testing.T) {
	archive := testframework.NewPkgDataZip(t)
	data := js.Global().Get("Uint8Array").New(len(archive))
	js.CopyBytesToJS(data, archive)
	plain, plainReplies := newTestServer(t, "main.xgo", "var Count = 1\nCount = 2\n", map[string]any{
		"classfileConfig": "", "pkgDataZip": data,
	})
	framework, frameworkReplies := newTestServer(t, "main.actor", "onStart => {\n    measure 1\n}\n", map[string]any{
		"classfileConfig": "project main.actor App example.com/framework\nclass -embed *.actor Item\n", "pkgDataZip": data,
	})
	// Mutate caller-owned data before lazy imports exercise instance ownership.
	data.Call("fill", 0)
	for _, tt := range []struct {
		handle   js.Value
		replies  <-chan js.Value
		filename string
		line     int
		want     string
	}{
		{plain, plainReplies, "main.xgo", 1, "var Count int"},
		{framework, frameworkReplies, "main.actor", 0, "OnStart accepts a project callback."},
	} {
		callTestServer(t, tt.handle, tt.replies, "initialize", map[string]any{"capabilities": map[string]any{}})
		hover := callTestServer(t, tt.handle, tt.replies, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": "file:///" + tt.filename},
			"position":     map[string]any{"line": tt.line, "character": 1},
		})
		assert.Contains(t, js.Global().Get("JSON").Call("stringify", hover).String(), tt.want)
	}
}

func newTestServer(t *testing.T, filename, source string, options map[string]any) (js.Value, <-chan js.Value) {
	t.Helper()
	content := js.Global().Get("Uint8Array").New(len(source))
	js.CopyBytesToJS(content, []byte(source))
	provider := js.FuncOf(func(js.Value, []js.Value) any {
		return map[string]any{filename: map[string]any{"content": content, "modTime": 0}}
	})
	responses := make(chan js.Value, 16)
	reply := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if !args[0].Get("id").IsUndefined() {
			responses <- args[0]
		}
		return nil
	})
	t.Cleanup(provider.Release)
	t.Cleanup(reply.Release)
	value, ok := NewXGoLanguageServer(js.Undefined(), []js.Value{provider.Value, reply.Value, js.ValueOf(options)}).(js.Value)
	require.True(t, ok)
	return value.Get("handleMessage"), responses
}

func callTestServer(t *testing.T, handle js.Value, responses <-chan js.Value, method string, params any) js.Value {
	t.Helper()
	result := handle.Invoke(js.ValueOf(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}))
	require.True(t, result.IsNull() || result.IsUndefined())
	select {
	case response := <-responses:
		encoded := js.Global().Get("JSON").Call("stringify", response).String()
		require.True(t, json.Valid([]byte(encoded)))
		require.True(t, response.Get("error").IsUndefined(), "%s", encoded)
		return response.Get("result")
	case <-time.After(10 * time.Second):
		require.FailNow(t, "language server did not reply", "%s", method)
		return js.Undefined()
	}
}

func TestXGoLanguageServerSymbolRequests(t *testing.T) {
	const source = "type Reader interface { Read() int }\ntype Item struct{}\nfunc (Item) Read() int { return 1 }\nfunc use(reader Reader) { println reader.Read(), Item{}.Read() }\n"
	archive := testframework.NewPkgDataZip(t)
	data := js.Global().Get("Uint8Array").New(len(archive))
	js.CopyBytesToJS(data, archive)
	handle, replies := newTestServer(t, "main.xgo", source, map[string]any{"pkgDataZip": data})
	callTestServer(t, handle, replies, "initialize", map[string]any{"capabilities": map[string]any{}})
	const uri = "file:///main.xgo"
	position := map[string]any{"line": 0, "character": 24}
	document := map[string]any{"uri": uri}
	check := func(implementations int) {
		t.Helper()
		for range 2 {
			refs := callTestServer(t, handle, replies, "textDocument/references", map[string]any{
				"textDocument": document, "position": position, "context": map[string]any{"includeDeclaration": false},
			})
			assert.Equal(t, implementations+1, refs.Length())
			edit := callTestServer(t, handle, replies, "textDocument/rename", map[string]any{
				"textDocument": document, "position": position, "newName": "Fetch",
			})
			assert.Equal(t, (implementations+1)*2, edit.Get("changes").Get(uri).Length())
			impl := callTestServer(t, handle, replies, "textDocument/implementation", map[string]any{
				"textDocument": document, "position": position,
			})
			assert.Equal(t, implementations, impl.Length())
			highlights := callTestServer(t, handle, replies, "textDocument/documentHighlight", map[string]any{
				"textDocument": document, "position": position,
			})
			assert.Equal(t, 2, highlights.Length())
		}
	}
	check(1)
	for _, notification := range []map[string]any{
		{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
			"textDocument": map[string]any{"uri": uri, "languageId": "xgo", "version": 1, "text": source},
		}},
		{"jsonrpc": "2.0", "method": "textDocument/didChange", "params": map[string]any{
			"textDocument":   map[string]any{"uri": uri, "version": 2},
			"contentChanges": []any{map[string]any{"text": "type Reader interface { Read() int }\nfunc use(reader Reader) { println reader.Read() }\n"}},
		}},
	} {
		result := handle.Invoke(js.ValueOf(notification))
		require.True(t, result.IsNull() || result.IsUndefined())
	}
	check(0)
}
