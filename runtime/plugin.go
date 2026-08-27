//go:build !js && !wasm

package runtime

import (
	"fmt"
	"plugin"
)

func pluginsSupported() bool { return true }

// PluginsSupported reports whether this platform can load native plugins.
func PluginsSupported() bool { return pluginsSupported() }

// LoadPlugin opens a Go plugin and calls WritPackage.
func LoadPlugin(path string) (Package, error) { return loadPlugin(path) }

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
