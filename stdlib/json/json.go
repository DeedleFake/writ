package json

import (
	"deedles.dev/writ"
	"deedles.dev/writ/runtime"
)

// ImportPath is the (import) name used by [Register].
const ImportPath = "stdlib/json"

// Package returns the json stdlib package (parse, stringify).
func Package() runtime.Package {
	return runtime.Package{
		Funcs: map[string]runtime.Func{
			"parse":     parse,
			"stringify": stringify,
		},
	}
}

// Register installs [Package] under [ImportPath].
// writ.New does not call Register.
func Register(rt *writ.Runtime) {
	rt.RegisterPackage(ImportPath, Package())
}

func parse(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, runtime.Errorf("json: parse needs 1 argument")
	}
	if args[0].Kind() != runtime.KindString {
		return runtime.Value{}, runtime.Errorf("json: parse needs a string")
	}
	return decode(args[0].Text())
}

func stringify(args []runtime.Value) (runtime.Value, error) {
	if len(args) != 1 {
		return runtime.Value{}, runtime.Errorf("json: stringify needs 1 argument")
	}
	b, err := encode(args[0])
	if err != nil {
		return runtime.Value{}, err
	}
	return runtime.String(string(b)), nil
}
