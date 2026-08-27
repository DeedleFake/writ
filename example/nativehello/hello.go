// A native package for (import ...) via go build -buildmode=plugin.
//
//	go build -buildmode=plugin -o nativehello.so
package main

import "deedles.dev/writ/runtime"

func main() {}

// WritPackage is the plugin entry point.
func WritPackage() runtime.Package {
	return runtime.Package{
		Funcs: map[string]runtime.Func{
			"greet": func(args []runtime.Value) (runtime.Value, error) {
				name := "world"
				if len(args) > 0 && args[0].Kind() == runtime.KindString {
					name = args[0].Text()
				}
				return runtime.String("hello, " + name), nil
			},
		},
		Vals: map[string]runtime.Value{
			"version": runtime.Int64(1),
		},
	}
}
