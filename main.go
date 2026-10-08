//go:build js && wasm

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"syscall/js"
	"time"

	"github.com/goplus/xgolsw/internal/config"
	"github.com/goplus/xgolsw/internal/pkgdata"
	"github.com/goplus/xgolsw/internal/server"
	"github.com/goplus/xgolsw/jsonrpc2"
	"github.com/goplus/xgolsw/xgo"
)

// XGoLanguageServer implements a lightweight XGo language server that runs in
// the browser using WebAssembly.
type XGoLanguageServer struct {
	messageReplier js.Value
	server         *server.Server
}

// NewXGoLanguageServer creates a new instance of [XGoLanguageServer].
func NewXGoLanguageServer(this js.Value, args []js.Value) any {
	if len(args) < 2 || len(args) > 3 {
		return errors.New("NewXGoLanguageServer: expected 2 or 3 arguments")
	}
	if args[0].Type() != js.TypeFunction {
		return errors.New("NewXGoLanguageServer: filesProvider argument must be a function")
	}
	if args[1].Type() != js.TypeFunction {
		return errors.New("NewXGoLanguageServer: messageReplier argument must be a function")
	}
	var options config.Options
	if len(args) == 3 && !args[2].IsUndefined() {
		var err error
		options, err = parseServerOptions(args[2])
		if err != nil {
			return fmt.Errorf("NewXGoLanguageServer: %w", err)
		}
	}
	filesProvider := args[0]
	s := &XGoLanguageServer{
		messageReplier: args[1],
	}

	fileMapGetter := func() map[string]*xgo.File {
		return ConvertJSFilesToMap(filesProvider.Invoke())
	}
	project, data, err := config.NewProject(fileMapGetter(), options)
	if err != nil {
		return fmt.Errorf("NewXGoLanguageServer: %w", err)
	}
	s.server = server.New(project, s, fileMapGetter, &JSScheduler{}, data.ListPkgs, data.GetPkgDoc, options.ResourceConfig)
	return js.ValueOf(map[string]any{
		"handleMessage": JSFuncOfWithError(s.HandleMessage),
	})
}

// parseServerOptions validates the optional JavaScript instance configuration.
func parseServerOptions(value js.Value) (config.Options, error) {
	var options config.Options
	if value.Type() != js.TypeObject || value.IsNull() || js.Global().Get("Array").Call("isArray", value).Bool() {
		return options, errors.New("options must be an object")
	}
	if classes := value.Get("classfileConfig"); !classes.IsUndefined() {
		if classes.Type() != js.TypeString {
			return options, errors.New("classfileConfig must be a string")
		}
		options.ClassfileConfig = classes.String()
	}
	if data := value.Get("pkgDataZip"); !data.IsUndefined() {
		if data.Type() != js.TypeObject || data.IsNull() || !data.InstanceOf(js.Global().Get("Uint8Array")) {
			return options, errors.New("pkgDataZip must be a Uint8Array")
		}
		var err error
		options.PkgData, err = pkgdata.NewWithEmbedded(JSUint8ArrayToBytes(data))
		if err != nil {
			return options, fmt.Errorf("invalid package data: %w", err)
		}
	}
	if resources := value.Get("resourceConfig"); !resources.IsUndefined() {
		var err error
		options.ResourceConfig, err = parseResourceConfigOption(resources)
		if err != nil {
			return options, err
		}
	}
	return options, nil
}

// parseResourceConfigOption validates JSON configuration and reports JavaScript
// serialization errors, including cyclic objects, through the constructor.
func parseResourceConfigOption(value js.Value) (resources *config.ResourceConfig, err error) {
	if value.Type() != js.TypeObject || value.IsNull() || js.Global().Get("Array").Call("isArray", value).Bool() {
		return nil, errors.New("resourceConfig must be an object")
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			if jsErr, ok := recovered.(js.Error); ok {
				err = fmt.Errorf("invalid resource configuration: %w", jsErr)
			} else {
				panic(recovered)
			}
		}
	}()
	encoded := js.Global().Get("JSON").Call("stringify", value).String()
	return config.ParseResourceConfig([]byte(encoded))
}

// HandleMessage handles incoming LSP messages from the client.
func (s *XGoLanguageServer) HandleMessage(this js.Value, args []js.Value) any {
	if len(args) != 1 {
		return errors.New("XGoLanguageServer.HandleMessage: expected 1 argument")
	}
	if args[0].Type() != js.TypeObject {
		return errors.New("XGoLanguageServer.HandleMessage: message argument must be an object")
	}
	rawMessage := js.Global().Get("JSON").Call("stringify", args[0]).String()
	message, err := jsonrpc2.DecodeMessage([]byte(rawMessage))
	if err != nil {
		return fmt.Errorf("XGoLanguageServer.HandleMessage: %w", err)
	}
	if err := s.server.HandleMessage(message); err != nil {
		return fmt.Errorf("XGoLanguageServer.HandleMessage: %w", err)
	}
	return nil
}

// ReplyMessage sends a message back to the client via s.messageReplier.
func (s *XGoLanguageServer) ReplyMessage(m jsonrpc2.Message) (err error) {
	rawMessage, err := json.Marshal(m)
	if err != nil {
		return err
	}

	// Catch potential panics during JavaScript execution.
	defer func() {
		if r := recover(); r != nil {
			if jsErr, ok := r.(js.Error); ok {
				err = fmt.Errorf("client error: %w", jsErr)
			} else {
				err = fmt.Errorf("client panic: %v", r)
			}
		}
	}()

	message := js.Global().Get("JSON").Call("parse", string(rawMessage))
	s.messageReplier.Invoke(message)
	return nil
}

// JSScheduler implements [server.Scheduler]
type JSScheduler struct{}

// Sched yields the processor in browsers to allow JavaScript event loop to run.
// We use `setTimeout` to ensure microtask queue is processed, which is
// necessary for the browser to handle incoming messages and other events.
func (s *JSScheduler) Sched() {
	done := make(chan bool, 1)
	callback := js.FuncOf(func(this js.Value, p []js.Value) any {
		done <- true
		return nil
	})
	defer callback.Release()
	js.Global().Get("setTimeout").Invoke(callback, js.ValueOf(0))
	<-done
}

// JSFuncOfWithError returns a function to be used by JavaScript that can return
// an error.
func JSFuncOfWithError(fn func(this js.Value, args []js.Value) any) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		result := fn(this, args)
		if err, ok := result.(error); ok {
			return js.Global().Get("Error").New(err.Error())
		}
		return result
	})
}

// JSUint8ArrayToBytes converts a JavaScript Uint8Array to a []byte.
func JSUint8ArrayToBytes(uint8Array js.Value) []byte {
	b := make([]byte, uint8Array.Length())
	js.CopyBytesToGo(b, uint8Array)
	return b
}

// ConvertJSFilesToMap converts a JavaScript object of files to a map.
func ConvertJSFilesToMap(files js.Value) map[string]*xgo.File {
	if files.Type() != js.TypeObject {
		return nil
	}
	keys := js.Global().Get("Object").Call("keys", files)
	result := make(map[string]*xgo.File, keys.Length())
	for i := range keys.Length() {
		key := keys.Index(i).String()
		value := files.Get(key)
		if value.InstanceOf(js.Global().Get("Object")) {
			result[key] = &xgo.File{
				Content: JSUint8ArrayToBytes(value.Get("content")),
				ModTime: time.UnixMilli(int64(value.Get("modTime").Int())),
			}
		}
	}
	return result
}

func main() {
	js.Global().Set("NewXGoLanguageServer", JSFuncOfWithError(NewXGoLanguageServer))
	select {}
}
