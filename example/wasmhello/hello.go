//go:build js || wasm

// A WASM package for (import ...) via
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o wasmhello.wasm
package main

import (
	"deedles.dev/writ"
	"deedles.dev/writ/runtime"
	"deedles.dev/writ/syntax"
	"deedles.dev/writ/types"
)

func main() {}

func init() {
	writ.ExportGuestPackage(runtime.Package{
		Funcs: map[string]runtime.Func{
			"greet": greet,
			"mk":    mkBox,
			"inc":   incBox,
			"get":   getBox,
			"echo":  echoVal,
		},
		Macros: map[string]runtime.Macro{
			"unless": unless,
		},
		Vals: map[string]runtime.Value{
			"version": runtime.Int64(1),
		},
	}, map[string]types.Type{
		"greet": types.FnType(
			types.PosFn(types.StringType()),
			types.PosFn(types.StringType(), types.StringType()),
		),
		"mk":      types.FnType(types.PosFn(types.Opaque("wasmhello", "box"))),
		"inc":     types.FnType(types.PosFn(types.Opaque("wasmhello", "box"), types.Opaque("wasmhello", "box"))),
		"get":     types.FnType(types.PosFn(types.IntType(), types.Opaque("wasmhello", "box"))),
		"echo":    types.FnType(types.PosFn(types.Any(), types.Any())),
		"unless":  types.MacroType(),
		"version": types.IntType(),
	})
}

type box struct{ n int64 }

func mkBox(args []runtime.Value) (runtime.Value, error) {
	return runtime.Native(&box{}), nil
}

func echoVal(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Nil, nil
	}
	return args[0], nil
}

func incBox(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Nil, runtime.ErrorMsg("want box")
	}
	b, ok := args[0].As[*box]()
	if !ok || b == nil {
		return runtime.Nil, runtime.ErrorMsg("want box")
	}
	b.n++
	return args[0], nil
}

func getBox(args []runtime.Value) (runtime.Value, error) {
	if len(args) < 1 {
		return runtime.Nil, runtime.ErrorMsg("want box")
	}
	b, ok := args[0].As[*box]()
	if !ok || b == nil {
		return runtime.Nil, runtime.ErrorMsg("want box")
	}
	return runtime.Int64(b.n), nil
}

func greet(args []runtime.Value) (runtime.Value, error) {
	name := "world"
	if len(args) > 0 && args[0].Kind() == runtime.KindString {
		name = args[0].Text()
	}
	return runtime.String("hello, " + name), nil
}

func unless(args []syntax.Form) (syntax.Form, error) {
	if len(args) < 2 {
		return syntax.Form{}, runtime.ErrorMsg("unless needs a test and a body")
	}
	form := syntax.CallList(
		syntax.Symbol("if"),
		syntax.CallList(syntax.Symbol("not"), args[0]),
		args[1],
	)
	return syntax.CallList(form), nil
}
