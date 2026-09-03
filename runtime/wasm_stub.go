//go:build js || wasm

package runtime

import "fmt"

// LoadWasm instantiates a WASI reactor and reads its writ package table.
func LoadWasm(path string) (Package, error) { return loadWasm(path) }

func loadWasm(path string) (Package, error) {
	return Package{}, fmt.Errorf("wasm packages are not supported on this platform")
}
