//go:build js || wasm

// A WASM package for (import ...) via
//
//	GOOS=wasip1 GOARCH=wasm go build -buildmode=c-shared -o wasmhello.wasm
package main

import (
	"deedles.dev/writ"
	"deedles.dev/writ/syntax"
)

func main() {}

func init() {
	writ.ExportGuestPackage(writ.Package{
		Exports: map[string]writ.Value{
			"greet": writ.TypedFn(greet, writ.FnType(
				writ.PosFn(writ.StringType()),
				writ.PosFn(writ.StringType(), writ.StringType()),
			)),
			"mk": writ.TypedFn(mkBox, writ.FnType(writ.PosFn(writ.Opaque("wasmhello", "box")))),
			"inc": writ.TypedFn(incBox, writ.FnType(writ.PosFn(
				writ.Opaque("wasmhello", "box"), writ.Opaque("wasmhello", "box"),
			))),
			"get": writ.TypedFn(getBox, writ.FnType(writ.PosFn(
				writ.IntType(), writ.Opaque("wasmhello", "box"),
			))),
			"echo":    writ.TypedFn(echoVal, writ.FnType(writ.PosFn(writ.Any(), writ.Any()))),
			"unless":  writ.Mac(unless),
			"version": writ.Typed(writ.Int64(1), writ.IntType()),
		},
	})
}

type box struct{ n int64 }

func mkBox(args []writ.Value) (writ.Value, error) {
	return writ.Native(&box{}), nil
}

func echoVal(args []writ.Value) (writ.Value, error) {
	if len(args) < 1 {
		return writ.Nil, nil
	}
	return args[0], nil
}

func incBox(args []writ.Value) (writ.Value, error) {
	if len(args) < 1 {
		return writ.Nil, syntax.ErrorMsg("want box")
	}
	b, ok := args[0].As[*box]()
	if !ok || b == nil {
		return writ.Nil, syntax.ErrorMsg("want box")
	}
	b.n++
	return args[0], nil
}

func getBox(args []writ.Value) (writ.Value, error) {
	if len(args) < 1 {
		return writ.Nil, syntax.ErrorMsg("want box")
	}
	b, ok := args[0].As[*box]()
	if !ok || b == nil {
		return writ.Nil, syntax.ErrorMsg("want box")
	}
	return writ.Int64(b.n), nil
}

func greet(args []writ.Value) (writ.Value, error) {
	name := "world"
	if len(args) > 0 && args[0].Kind() == writ.KindString {
		name = args[0].Text()
	}
	return writ.String("hello, " + name), nil
}

func unless(args []syntax.Form) (syntax.Form, error) {
	if len(args) < 2 {
		return syntax.Form{}, syntax.ErrorMsg("unless needs a test and a body")
	}
	form := syntax.CallList(
		syntax.Symbol("if"),
		syntax.CallList(syntax.Symbol("not"), args[0]),
		args[1],
	)
	return syntax.CallList(form), nil
}
