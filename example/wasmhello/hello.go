//go:build js || wasm

// A WASM package for (import ...) via
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o wasmhello.wasm
package main

import "deedles.dev/writ/runtime"

func main() {}

func init() {
	runtime.ExportGuestPackage(runtime.Package{
		Funcs: map[string]runtime.Func{
			"greet": greet,
		},
		Macros: map[string]runtime.Func{
			"unless": unless,
		},
		Vals: map[string]runtime.Value{
			"version": runtime.Int64(1),
		},
	})
}

func greet(args []runtime.Value) (runtime.Value, error) {
	name := "world"
	if len(args) > 0 && args[0].Kind() == runtime.KindString {
		name = args[0].Text()
	}
	return runtime.String("hello, " + name), nil
}

func unless(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 2 {
		return runtime.Value{}, runtime.ErrorMsg("unless needs a test and a body")
	}
	form := runtime.CallList(
		runtime.Symbol("if"),
		runtime.CallList(runtime.Symbol("not"), args[0]),
		args[1],
	)
	return runtime.CallList(form), nil
}
