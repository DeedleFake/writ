// A native package for (import ...) via go build -buildmode=plugin.
//
//	go build -buildmode=plugin -o nativehello.so
package main

import "deedles.dev/writ"

func main() {}

// WritPackage is the plugin entry point.
func WritPackage() writ.Package {
	return writ.Package{
		Funcs: map[string]writ.Func{
			"greet": func(args []writ.Value) (writ.Value, error) {
				name := "world"
				if len(args) > 0 && args[0].Kind() == writ.KindString {
					name = args[0].Text()
				}
				return writ.String("hello, " + name), nil
			},
		},
		Vals: map[string]writ.Value{
			"version": writ.Int64(1),
		},
	}
}
