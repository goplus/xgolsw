//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
	"testing"

	"github.com/goplus/xgolsw/internal/testframework"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestXGoLanguageServerNotRecommended(t *testing.T) {
	const source = `//xgols:notrecommended
var hiddenValue int
var visibleValue int
//xgols:notrecommended
func hidden(value int) int { return value }
func visible(value int) int { return value }
func Use(value int) {}
func run() {

use 7
println hidden(visibleValue)
}
`
	archive := testframework.NewPkgDataZip(t)
	data := js.Global().Get("Uint8Array").New(len(archive))
	js.CopyBytesToJS(data, archive)
	handle, replies := newTestServer(t, "main.xgo", source, map[string]any{
		"classFrameworkConfig": "", "pkgDataZip": data,
	})
	callTestServer(t, handle, replies, "initialize", map[string]any{"capabilities": map[string]any{}})
	document := map[string]any{"uri": "file:///main.xgo"}
	for version, annotated := range []bool{true, false, true} {
		updated := source
		if !annotated {
			updated = strings.ReplaceAll(updated, "//xgols:notrecommended", "// Ordinary documentation.")
		}
		method := "textDocument/didChange"
		params := map[string]any{
			"textDocument":   map[string]any{"uri": "file:///main.xgo", "version": version + 1},
			"contentChanges": []any{map[string]any{"text": updated}},
		}
		if version == 0 {
			method = "textDocument/didOpen"
			params = map[string]any{"textDocument": map[string]any{
				"uri": "file:///main.xgo", "version": 1, "languageId": "xgo", "text": updated,
			}}
		}
		result := handle.Call("handleMessage", js.ValueOf(map[string]any{
			"jsonrpc": "2.0", "method": method, "params": params,
		}))
		require.True(t, result.IsNull() || result.IsUndefined())
		items := callTestServer(t, handle, replies, "textDocument/completion", map[string]any{
			"textDocument": document, "position": map[string]any{"line": 8, "character": 0},
		})
		require.True(t, js.Global().Get("Array").Call("isArray", items).Bool())
		var labels []string
		for i := range items.Length() {
			labels = append(labels, items.Index(i).Get("label").String())
		}
		assert.Contains(t, labels, "visible")
		assert.Contains(t, labels, "visibleValue")
		slots := callTestServer(t, handle, replies, "workspace/executeCommand", map[string]any{
			"command": "xgo.getInputSlots", "arguments": []any{map[string]any{"textDocument": document}},
		})
		require.True(t, js.Global().Get("Array").Call("isArray", slots).Bool())
		var names []string
		for i := range slots.Length() {
			slot := slots.Index(i)
			if slot.Get("range").Get("start").Get("line").Int() != 9 {
				continue
			}
			predefined := slot.Get("predefinedNames")
			for j := range predefined.Length() {
				names = append(names, predefined.Index(j).String())
			}
		}
		assert.Contains(t, names, "visibleValue")
		if annotated {
			assert.NotContains(t, labels, "hidden")
			assert.NotContains(t, labels, "hiddenValue")
			assert.NotContains(t, names, "hiddenValue")
		} else {
			assert.Contains(t, labels, "hidden")
			assert.Contains(t, labels, "hiddenValue")
			assert.Contains(t, names, "hiddenValue")
		}
		hover := callTestServer(t, handle, replies, "textDocument/hover", map[string]any{
			"textDocument": document, "position": map[string]any{"line": 10, "character": 8},
		})
		assert.Contains(t, js.Global().Get("JSON").Call("stringify", hover).String(), "func hidden(value int) int")
	}
}
