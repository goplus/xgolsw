//go:build js && wasm

package main

import (
	"encoding/json"
	"fmt"
	"runtime"
	"syscall/js"
	"testing"
	"time"
	"weak"

	"github.com/goplus/xgolsw/internal/server"
	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/xgo"
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
		{"NullFrameworkConfig", map[string]any{"classFrameworkConfig": nil}, "classFrameworkConfig must be a string"},
		{"NumberFrameworkConfig", map[string]any{"classFrameworkConfig": 1}, "classFrameworkConfig must be a string"},
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
		assert.Empty(t, options.ClassFrameworkConfig)
		assert.Nil(t, options.PkgData)
	})
	t.Run("ExplicitEmpty", func(t *testing.T) {
		options, err := parseServerOptions(js.ValueOf(map[string]any{
			"classFrameworkConfig": "", "pkgDataZip": js.Global().Get("Uint8Array").New(0),
		}))
		require.NoError(t, err)
		assert.Empty(t, options.ClassFrameworkConfig)
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
		{"InvalidFrameworkConfig", []js.Value{provider.Value, reply.Value, js.ValueOf(map[string]any{"classFrameworkConfig": "project"})}, "invalid class framework configuration"},
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
		value, ok := NewXGoLanguageServer(js.Undefined(), args).(js.Value)
		require.True(t, ok)
		value.Call("dispose")
	}
}

func TestXGoLanguageServerInstanceConfiguration(t *testing.T) {
	archive := testframework.NewPkgDataZip(t)
	data := js.Global().Get("Uint8Array").New(len(archive))
	js.CopyBytesToJS(data, archive)
	plain, plainReplies := newTestServer(t, "main.xgo", `var Count = 1
Count = 2
`, map[string]any{
		"classFrameworkConfig": "", "pkgDataZip": data,
	})
	framework, frameworkReplies := newTestServer(t, "main.actor", `onStart => {
    measure 1
}
`, map[string]any{
		"classFrameworkConfig": `project main.actor App example.com/framework
class -embed *.actor Item
`, "pkgDataZip": data,
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
	t.Cleanup(func() { value.Call("dispose") })
	return value, responses
}

func callTestServer(t *testing.T, handle js.Value, responses <-chan js.Value, method string, params any) js.Value {
	t.Helper()
	result := handle.Call("handleMessage", js.ValueOf(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params}))
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

func TestXGoLanguageServerReadFiles(t *testing.T) {
	t.Run("UnchangedContent", func(t *testing.T) {
		content := js.Global().Get("Uint8Array").New(3)
		js.CopyBytesToJS(content, []byte("old"))
		file := js.ValueOf(map[string]any{"modTime": 100})
		var reads int
		getter := js.FuncOf(func(js.Value, []js.Value) any {
			reads++
			return content
		})
		t.Cleanup(getter.Release)
		js.Global().Get("Object").Call("defineProperty", file, "content", map[string]any{"get": getter})
		provider := js.FuncOf(func(js.Value, []js.Value) any {
			return map[string]any{"main.xgo": file}
		})
		t.Cleanup(provider.Release)
		s := new(XGoLanguageServer)
		first := s.readFiles(provider.Value)
		js.CopyBytesToJS(content, []byte("new"))
		second := s.readFiles(provider.Value)
		assert.Same(t, first["main.xgo"], second["main.xgo"])
		assert.Equal(t, []byte("old"), first["main.xgo"].Content)
		assert.Equal(t, 1, reads)
		file.Set("modTime", 99)
		third := s.readFiles(provider.Value)
		assert.NotSame(t, first["main.xgo"], third["main.xgo"])
		assert.Equal(t, []byte("new"), third["main.xgo"].Content)
		assert.Equal(t, []byte("old"), first["main.xgo"].Content)
		assert.Equal(t, 2, reads)
	})

	t.Run("DeletedAndRecreated", func(t *testing.T) {
		content := js.Global().Get("Uint8Array").New(1)
		content.SetIndex(0, 1)
		files := js.ValueOf(map[string]any{"main.xgo": map[string]any{"content": content, "modTime": 0}})
		provider := js.FuncOf(func(js.Value, []js.Value) any { return files })
		t.Cleanup(provider.Release)
		s := new(XGoLanguageServer)
		first := s.readFiles(provider.Value)
		files.Delete("main.xgo")
		assert.Empty(t, s.readFiles(provider.Value))
		assert.Contains(t, first, "main.xgo")
		content.SetIndex(0, 2)
		files.Set("main.xgo", map[string]any{"content": content, "modTime": 0})
		recreated := s.readFiles(provider.Value)
		assert.NotSame(t, first["main.xgo"], recreated["main.xgo"])
		assert.Equal(t, []byte{2}, recreated["main.xgo"].Content)
	})

	t.Run("FailedConversion", func(t *testing.T) {
		content := js.Global().Get("Uint8Array").New(1)
		content.SetIndex(0, 1)
		original := js.ValueOf(map[string]any{"main.xgo": map[string]any{"content": content, "modTime": 0}})
		provided := original
		provider := js.FuncOf(func(js.Value, []js.Value) any { return provided })
		t.Cleanup(provider.Release)
		s := new(XGoLanguageServer)
		first := s.readFiles(provider.Value)
		provided = js.Global().Get("Object").New()
		provided.Set("main.xgo", map[string]any{"content": content, "modTime": 1})
		provided.Set("broken.xgo", map[string]any{"content": true, "modTime": 0})
		assert.Panics(t, func() { s.readFiles(provider.Value) })
		provided = original
		current := s.readFiles(provider.Value)
		assert.Same(t, first["main.xgo"], current["main.xgo"])
		assert.NotContains(t, current, "broken.xgo")
	})

	for _, tt := range []struct {
		name  string
		value js.Value
	}{
		{"Empty", js.Global().Get("Object").New()},
		{"Undefined", js.Undefined()},
		{"MissingEntry", js.ValueOf(map[string]any{"main.xgo": js.Undefined()})},
	} {
		t.Run(tt.name, func(t *testing.T) {
			content := js.Global().Get("Uint8Array").New(1)
			content.SetIndex(0, 1)
			original := js.ValueOf(map[string]any{"main.xgo": map[string]any{"content": content, "modTime": 0}})
			provided := original
			provider := js.FuncOf(func(js.Value, []js.Value) any { return provided })
			t.Cleanup(provider.Release)
			s := new(XGoLanguageServer)
			first := s.readFiles(provider.Value)
			provided = tt.value
			assert.Empty(t, s.readFiles(provider.Value))
			content.SetIndex(0, 2)
			provided = original
			current := s.readFiles(provider.Value)
			assert.NotSame(t, first["main.xgo"], current["main.xgo"])
			assert.Equal(t, []byte{2}, current["main.xgo"].Content)
		})
	}

	for _, tt := range []struct {
		name      string
		inContent bool
		deleted   bool
	}{
		{name: "ReentrantProviderUpdate"},
		{name: "ReentrantProviderDeletion", deleted: true},
		{name: "ReentrantContentUpdate", inContent: true},
		{name: "ReentrantContentDeletion", inContent: true, deleted: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := new(XGoLanguageServer)
			oldContent := js.Global().Get("Uint8Array").New(1)
			oldContent.SetIndex(0, 1)
			newContent := js.Global().Get("Uint8Array").New(1)
			newContent.SetIndex(0, 2)
			staleFile := js.ValueOf(map[string]any{"content": oldContent, "modTime": 1})
			stale := js.ValueOf(map[string]any{"main.xgo": staleFile})
			latest := js.Global().Get("Object").New()
			if !tt.deleted {
				latest.Set("main.xgo", map[string]any{"content": newContent, "modTime": 2})
			}
			var provider js.Func
			var nested map[string]*xgo.File
			var entered bool
			provider = js.FuncOf(func(js.Value, []js.Value) any {
				if entered {
					return latest
				}
				entered = true
				if !tt.inContent {
					nested = s.readFiles(provider.Value)
				}
				return stale
			})
			t.Cleanup(provider.Release)
			if tt.inContent {
				getter := js.FuncOf(func(js.Value, []js.Value) any {
					nested = s.readFiles(provider.Value)
					return oldContent
				})
				t.Cleanup(getter.Release)
				js.Global().Get("Object").Call("defineProperty", staleFile, "content", map[string]any{"get": getter})
			}
			outer := s.readFiles(provider.Value)
			assert.Equal(t, []byte{1}, outer["main.xgo"].Content)
			if tt.deleted {
				assert.Empty(t, nested)
				latest.Set("main.xgo", map[string]any{"content": newContent, "modTime": 1})
				current := s.readFiles(provider.Value)
				assert.Equal(t, []byte{2}, current["main.xgo"].Content)
			} else {
				current := s.readFiles(provider.Value)
				assert.Same(t, nested["main.xgo"], current["main.xgo"])
				assert.Equal(t, []byte{2}, current["main.xgo"].Content)
			}
		})
	}

	t.Run("IndependentInstances", func(t *testing.T) {
		content := js.Global().Get("Uint8Array").New(1)
		provider := js.FuncOf(func(js.Value, []js.Value) any {
			return map[string]any{"main.xgo": map[string]any{"content": content, "modTime": 0}}
		})
		t.Cleanup(provider.Release)
		first := new(XGoLanguageServer).readFiles(provider.Value)
		second := new(XGoLanguageServer).readFiles(provider.Value)
		assert.NotSame(t, first["main.xgo"], second["main.xgo"])
	})

	t.Run("DisposeDuringConversion", func(t *testing.T) {
		s := &XGoLanguageServer{server: server.New(xgo.NewProject(nil, nil, 0), nil, nil, nil, nil, nil)}
		value := s.jsValue()
		t.Cleanup(func() { value.Call("dispose") })
		content := js.Global().Get("Uint8Array").New(1)
		file := js.ValueOf(map[string]any{"content": content, "modTime": 0})
		provider := js.FuncOf(func(js.Value, []js.Value) any { return map[string]any{"main.xgo": file} })
		t.Cleanup(provider.Release)
		callback := s.wrapCallback(provider.Value)
		oldPointer := func() weak.Pointer[xgo.File] {
			files := s.readFiles(callback)
			return weak.Make(files["main.xgo"])
		}()
		file.Set("modTime", 1)
		getter := js.FuncOf(func(js.Value, []js.Value) any {
			value.Call("dispose")
			return content
		})
		t.Cleanup(getter.Release)
		js.Global().Get("Object").Call("defineProperty", file, "content", map[string]any{"get": getter})
		pointer := func() weak.Pointer[xgo.File] {
			files := s.readFiles(callback)
			return weak.Make(files["main.xgo"])
		}()
		assert.Nil(t, s.readFiles(callback))
		require.Eventually(t, func() bool {
			runtime.GC()
			return oldPointer.Value() == nil && pointer.Value() == nil
		}, 5*time.Second, time.Millisecond)
		runtime.KeepAlive(s)
	})
}

func TestXGoLanguageServerLineEndings(t *testing.T) {
	for _, tt := range []struct{ name, first, second, last string }{
		{"LF", "\n", "\n", "\n"},
		{"CRLF", "\r\n", "\r\n", "\r\n"},
		{"CR", "\r", "\r", "\r"},
		{"Mixed", "\r\n", "\r", "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			archive := testframework.NewPkgDataZip(t)
			data := js.Global().Get("Uint8Array").New(len(archive))
			js.CopyBytesToJS(data, archive)
			source := "var First = 1;" + tt.first + "var Count = 2;" + tt.second + "echo Count;" + tt.last
			handle, replies := newTestServer(t, "main.xgo", source, map[string]any{"classFrameworkConfig": "", "pkgDataZip": data})
			callTestServer(t, handle, replies, "initialize", map[string]any{"capabilities": map[string]any{}})
			handle.Call("handleMessage", js.ValueOf(map[string]any{
				"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": map[string]any{
					"textDocument": map[string]any{"uri": "file:///main.xgo", "version": 1, "languageId": "xgo", "text": source},
				},
			}))
			handle.Call("handleMessage", js.ValueOf(map[string]any{
				"jsonrpc": "2.0", "method": "textDocument/didChange", "params": map[string]any{
					"textDocument": map[string]any{"uri": "file:///main.xgo", "version": 2},
					"contentChanges": []any{
						map[string]any{"range": map[string]any{"start": map[string]any{"line": 1, "character": 4}, "end": map[string]any{"line": 1, "character": 9}}, "text": "Amount"},
						map[string]any{"range": map[string]any{"start": map[string]any{"line": 2, "character": 5}, "end": map[string]any{"line": 2, "character": 10}}, "text": "Amount"},
					},
				},
			}))
			params := map[string]any{
				"textDocument": map[string]any{"uri": "file:///main.xgo"},
				"position":     map[string]any{"line": 2, "character": 6},
			}
			hover := callTestServer(t, handle, replies, "textDocument/hover", params)
			assert.Contains(t, js.Global().Get("JSON").Call("stringify", hover).String(), "var Amount int")
			definition := callTestServer(t, handle, replies, "textDocument/definition", params)
			require.False(t, definition.IsNull())
			assert.Equal(t, 1, definition.Get("range").Get("start").Get("line").Int())
			assert.Equal(t, 4, definition.Get("range").Get("start").Get("character").Int())
		})
	}
}

func TestXGoLanguageServerProviderUpdates(t *testing.T) {
	content := js.Global().Get("Uint8Array").New(len("var Count = 1\n"))
	js.CopyBytesToJS(content, []byte("var Count = 1\n"))
	file := js.ValueOf(map[string]any{"content": content, "modTime": 0})
	provider := js.FuncOf(func(js.Value, []js.Value) any { return map[string]any{"main.xgo": file} })
	t.Cleanup(provider.Release)
	responses := make(chan js.Value, 16)
	reply := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if !args[0].Get("id").IsUndefined() {
			responses <- args[0]
		}
		return nil
	})
	t.Cleanup(reply.Release)
	archive := testframework.NewPkgDataZip(t)
	data := js.Global().Get("Uint8Array").New(len(archive))
	js.CopyBytesToJS(data, archive)
	value, ok := NewXGoLanguageServer(js.Undefined(), []js.Value{provider.Value, reply.Value, js.ValueOf(map[string]any{"pkgDataZip": data})}).(js.Value)
	require.True(t, ok)
	t.Cleanup(func() { value.Call("dispose") })
	callTestServer(t, value, responses, "initialize", map[string]any{"capabilities": map[string]any{}})
	hover := func(want string) {
		t.Helper()
		response := callTestServer(t, value, responses, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": "file:///main.xgo"},
			"position":     map[string]any{"line": 0, "character": 5},
		})
		assert.Contains(t, js.Global().Get("JSON").Call("stringify", response).String(), want)
	}
	notify := func(method string, params any) {
		t.Helper()
		result := value.Call("handleMessage", js.ValueOf(map[string]any{"jsonrpc": "2.0", "method": method, "params": params}))
		require.True(t, result.IsNull() || result.IsUndefined())
	}
	hover("var Count int")
	hover("var Count int")
	source := "var Count = \"disk\"\n"
	content = js.Global().Get("Uint8Array").New(len(source))
	js.CopyBytesToJS(content, []byte(source))
	file.Set("content", content)
	file.Set("modTime", 1)
	hover("var Count string")
	for _, tt := range []struct {
		source, want string
		modTime      int
	}{
		{"var Count = \"disk\"\n", "var Count string", 1},
		{"var Count = 2.5\n", "var Count float64", 0},
		{"var Count = 2\n", "var Count int", 2},
	} {
		notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
			"uri": "file:///main.xgo", "languageId": "xgo", "version": 7, "text": "var Count = true\n",
		}})
		hover("var Count bool")
		content = js.Global().Get("Uint8Array").New(len(tt.source))
		js.CopyBytesToJS(content, []byte(tt.source))
		file.Set("content", content)
		file.Set("modTime", tt.modTime)
		hover("var Count bool")
		notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": "file:///main.xgo"}})
		hover(tt.want)
	}
}

func TestXGoLanguageServerCompletionIncompleteTypeDeclaration(t *testing.T) {
	for _, tt := range []struct{ name, source string }{
		{"OpeningBracket", "type Box["},
		{"ArrayLengthIdentifier", "type Box[T"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			archive := testframework.NewPkgDataZip(t)
			data := js.Global().Get("Uint8Array").New(len(archive))
			js.CopyBytesToJS(data, archive)
			handle, replies := newTestServer(t, "main.xgo", tt.source, map[string]any{"pkgDataZip": data})
			callTestServer(t, handle, replies, "initialize", map[string]any{"capabilities": map[string]any{}})
			callTestServer(t, handle, replies, "textDocument/completion", map[string]any{
				"textDocument": map[string]any{"uri": "file:///main.xgo"},
				"position":     map[string]any{"line": 0, "character": len(tt.source)},
			})

			result := handle.Call("handleMessage", js.ValueOf(map[string]any{
				"jsonrpc": "2.0",
				"method":  "textDocument/didOpen",
				"params": map[string]any{
					"textDocument": map[string]any{
						"uri":        "file:///main.xgo",
						"languageId": "xgo",
						"version":    1,
						"text": `type Box[2]int
var box Box
echo box
`,
					},
				},
			}))
			require.True(t, result.IsNull() || result.IsUndefined())
			hover := callTestServer(t, handle, replies, "textDocument/hover", map[string]any{
				"textDocument": map[string]any{"uri": "file:///main.xgo"},
				"position":     map[string]any{"line": 2, "character": 5},
			})
			assert.Contains(t, js.Global().Get("JSON").Call("stringify", hover).String(), "var box Box")
		})
	}
}

func TestXGoLanguageServerSymbolRequests(t *testing.T) {
	const source = `type Reader interface { Read() int }
type Item struct{}
func (Item) Read() int { return 1 }
func use(reader Reader) { println reader.Read(), Item{}.Read() }
`
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
			"textDocument": map[string]any{"uri": uri, "version": 2},
			"contentChanges": []any{map[string]any{"text": `type Reader interface { Read() int }
func use(reader Reader) { println reader.Read() }
`}},
		}},
	} {
		result := handle.Call("handleMessage", js.ValueOf(notification))
		require.True(t, result.IsNull() || result.IsUndefined())
	}
	check(0)
}

func TestXGoLanguageServerInterfaceImplementation(t *testing.T) {
	archive := testframework.NewPkgDataZip(t)
	data := js.Global().Get("Uint8Array").New(len(archive))
	js.CopyBytesToJS(data, archive)
	handle, responses := newTestServer(t, "main.xgo", `type Reader interface { Read() int }
type Item struct{}
func (Item) Read() int { return 1 }
`, map[string]any{"pkgDataZip": data})
	callTestServer(t, handle, responses, "initialize", map[string]any{"capabilities": map[string]any{}})
	result := callTestServer(t, handle, responses, "textDocument/implementation", map[string]any{
		"textDocument": map[string]any{"uri": "file:///main.xgo"},
		"position":     map[string]any{"line": 0, "character": 5},
	})
	require.True(t, result.InstanceOf(js.Global().Get("Array")))
	require.Equal(t, 1, result.Length())
	location := result.Index(0)
	assert.Equal(t, "file:///main.xgo", location.Get("uri").String())
	for _, boundary := range []string{"start", "end"} {
		position := location.Get("range").Get(boundary)
		assert.Equal(t, 1, position.Get("line").Int())
		assert.Equal(t, 5, position.Get("character").Int())
	}
}

func TestXGoLanguageServerDispose(t *testing.T) {
	t.Run("CopiedCallbackRevoked", func(t *testing.T) {
		s := &XGoLanguageServer{server: server.New(xgo.NewProject(nil, nil, 0), nil, nil, nil, nil, nil)}
		var calls int
		callback := js.FuncOf(func(js.Value, []js.Value) any {
			calls++
			return nil
		})
		t.Cleanup(callback.Release)
		copied := s.wrapCallback(callback.Value)
		value := s.jsValue()
		value.Call("dispose")
		assert.Panics(t, func() { copied.Invoke() })
		assert.Zero(t, calls)
	})

	for _, disposed := range []bool{false, true} {
		name := "ActiveProviderError"
		if disposed {
			name = "DisposeDuringProviderError"
		}
		t.Run(name, func(t *testing.T) {
			s := &XGoLanguageServer{server: server.New(xgo.NewProject(nil, nil, 0), nil, nil, nil, nil, nil)}
			value := s.jsValue()
			t.Cleanup(func() { value.Call("dispose") })
			body := `throw new Error("provider failed")`
			if disposed {
				body = `dispose(); ` + body
			}
			callback := js.Global().Get("Function").New("dispose", body).Call("bind", js.Undefined(), value.Get("dispose"))
			provider := s.wrapCallback(callback)
			if disposed {
				assert.Nil(t, s.readFiles(provider))
				assert.Nil(t, s.readFiles(provider))
			} else {
				assert.Panics(t, func() { s.readFiles(provider) })
			}
		})
	}

	t.Run("GoPanicAfterDisposal", func(t *testing.T) {
		s := &XGoLanguageServer{server: server.New(xgo.NewProject(nil, nil, 0), nil, nil, nil, nil, nil)}
		value := s.jsValue()
		callback := js.FuncOf(func(js.Value, []js.Value) any {
			value.Call("dispose")
			return map[string]any{
				"main.xgo": map[string]any{
					"content": true,
					"modTime": 0,
				},
			}
		})
		t.Cleanup(callback.Release)
		t.Cleanup(func() { value.Call("dispose") })
		provider := s.wrapCallback(callback.Value)
		var panicValue any
		func() {
			defer func() { panicValue = recover() }()
			s.readFiles(provider)
		}()
		assert.IsType(t, new(js.ValueError), panicValue)
	})

	t.Run("DisposeDuringReplyError", func(t *testing.T) {
		s := &XGoLanguageServer{server: server.New(xgo.NewProject(nil, nil, 0), nil, nil, nil, nil, nil)}
		value := s.jsValue()
		t.Cleanup(func() { value.Call("dispose") })
		callback := js.Global().Get("Function").New("dispose", `dispose(); throw new Error("reply failed")`)
		s.messageReplier = s.wrapCallback(callback.Call("bind", js.Undefined(), value.Get("dispose")))
		notification, err := jsonrpc2.NewNotification("exit", nil)
		require.NoError(t, err)
		assert.ErrorIs(t, s.ReplyMessage(notification), server.ErrClosed)
	})

	t.Run("RepeatedInstancesAndSavedMethods", func(t *testing.T) {
		for range 10 {
			value, _ := newTestServer(t, "main.xgo", "var Count = 1\n", nil)
			handle, dispose := value.Get("handleMessage"), value.Get("dispose")
			js.Global().Get("Object").Call("freeze", value)
			dispose.Invoke()
			dispose.Invoke()
			value.Call("dispose")
			result := handle.Invoke(js.ValueOf(map[string]any{"jsonrpc": "2.0", "method": "exit"}))
			require.True(t, result.InstanceOf(js.Global().Get("Error")))
			assert.Contains(t, result.Get("message").String(), "language server is closed")
		}
	})

	t.Run("InstanceIsolation", func(t *testing.T) {
		first, _ := newTestServer(t, "main.xgo", "", nil)
		second, replies := newTestServer(t, "main.xgo", "", nil)
		first.Call("dispose")
		callTestServer(t, second, replies, "initialize", map[string]any{"capabilities": map[string]any{}})
	})

	t.Run("ReleaseInstance", func(t *testing.T) {
		create := func() (js.Value, weak.Pointer[XGoLanguageServer]) {
			t.Helper()

			s := &XGoLanguageServer{server: server.New(xgo.NewProject(nil, nil, 0), nil, nil, nil, nil, nil)}
			value := s.jsValue()
			pointer := weak.Make(s)
			value.Call("dispose")
			s.Dispose()
			closedErr, ok := s.HandleMessage(js.Undefined(), nil).(error)
			require.True(t, ok)
			assert.ErrorIs(t, closedErr, server.ErrClosed)
			notification, err := jsonrpc2.NewNotification("exit", nil)
			require.NoError(t, err)
			assert.ErrorIs(t, s.ReplyMessage(notification), server.ErrClosed)
			return value, pointer
		}
		value, pointer := create()
		require.Eventually(t, func() bool {
			runtime.GC()
			return pointer.Value() == nil
		}, 5*time.Second, time.Millisecond)
		runtime.KeepAlive(value)
	})

	t.Run("ReentrantReply", func(t *testing.T) {
		provider := js.FuncOf(func(js.Value, []js.Value) any { return map[string]any{} })
		t.Cleanup(provider.Release)
		var value js.Value
		var messages int
		disposed := make(chan struct{})
		reply := js.FuncOf(func(js.Value, []js.Value) any {
			messages++
			value.Call("dispose")
			close(disposed)
			return nil
		})
		t.Cleanup(reply.Release)
		created, ok := NewXGoLanguageServer(js.Undefined(), []js.Value{provider.Value, reply.Value}).(js.Value)
		require.True(t, ok)
		value = created
		t.Cleanup(func() { value.Call("dispose") })
		value.Call("handleMessage", js.ValueOf(map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{"capabilities": map[string]any{}},
		}))
		select {
		case <-disposed:
		case <-time.After(5 * time.Second):
			require.FailNow(t, "dispose blocked inside the reply callback")
		}
		new(JSScheduler).Sched()
		assert.Equal(t, 1, messages)
	})

	t.Run("DisposeDuringReplyConversion", func(t *testing.T) {
		s := &XGoLanguageServer{server: server.New(xgo.NewProject(nil, nil, 0), nil, nil, nil, nil, nil)}
		value := s.jsValue()
		t.Cleanup(func() { value.Call("dispose") })
		jsonValue := js.Global().Get("JSON")
		originalParse := jsonValue.Get("parse")
		parse := js.FuncOf(func(_ js.Value, args []js.Value) any {
			value.Call("dispose")
			return originalParse.Invoke(args[0])
		})
		t.Cleanup(parse.Release)
		t.Cleanup(func() { jsonValue.Set("parse", originalParse) })
		jsonValue.Set("parse", parse)
		notification, err := jsonrpc2.NewNotification("exit", nil)
		require.NoError(t, err)
		assert.ErrorIs(t, s.ReplyMessage(notification), server.ErrClosed)
	})
}

func BenchmarkXGoLanguageServerReadFiles(b *testing.B) {
	for _, tt := range []struct {
		name  string
		files int
		size  int
	}{
		{"Source4KiB", 1, 4 << 10},
		{"TextProject512KiB", 128, 4 << 10},
		{"Resource16MiB", 1, 16 << 20},
	} {
		b.Run(tt.name, func(b *testing.B) {
			files := make(map[string]any, tt.files)
			for i := range tt.files {
				files[fmt.Sprintf("file%d.gox", i)] = map[string]any{
					"content": js.Global().Get("Uint8Array").New(tt.size), "modTime": 100,
				}
			}
			value := js.ValueOf(files)
			provider := js.FuncOf(func(js.Value, []js.Value) any { return value })
			b.Cleanup(provider.Release)
			s := new(XGoLanguageServer)
			s.readFiles(provider.Value)
			b.ReportAllocs()
			for b.Loop() {
				s.readFiles(provider.Value)
			}
		})
	}
}
