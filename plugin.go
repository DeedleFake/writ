//go:build !js && !wasm

package writ

import (
	"fmt"
	"plugin"
)

func pluginsSupported() bool { return true }

func loadPlugin(path string) (Package, error) {
	p, err := plugin.Open(path)
	if err != nil {
		return Package{}, err
	}
	sym, err := p.Lookup("WritPackage")
	if err != nil {
		return Package{}, err
	}
	switch f := sym.(type) {
	case func() Package:
		return f(), nil
	case *func() Package:
		return (*f)(), nil
	default:
		return Package{}, fmt.Errorf("WritPackage has unexpected type %T", sym)
	}
}
