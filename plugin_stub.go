//go:build js || wasm

package writ

import "fmt"

func pluginsSupported() bool { return false }

func loadPlugin(path string) (Package, error) {
	return Package{}, fmt.Errorf("native plugins are not supported on this platform")
}
