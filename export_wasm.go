//go:build js || wasm

package writ

import (
	"deedles.dev/writ/runtime"
	"deedles.dev/writ/types"
)

// ExportGuestPackage registers a WASM guest package with optional export types.
// tys may be nil.
func ExportGuestPackage(p runtime.Package, tys map[string]types.Type) {
	runtime.ExportGuestPackage(WithPackageTypes(p, tys))
}
