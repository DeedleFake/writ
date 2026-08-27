//go:build js || wasm

package runtime

import "fmt"

func pluginsSupported() bool { return false }

// PluginsSupported reports whether this platform can load native plugins.
func PluginsSupported() bool { return pluginsSupported() }

// LoadPlugin opens a Go plugin and calls WritPackage.
func LoadPlugin(path string) (Package, error) { return loadPlugin(path) }

func loadPlugin(path string) (Package, error) {
	return Package{}, fmt.Errorf("native plugins are not supported on this platform")
}
