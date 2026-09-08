package writ

import (
	"deedles.dev/writ/runtime"
	"deedles.dev/writ/types"
)

// WithPackageTypes returns a copy of p with TypeBlobs filled from tys.
// Use with RegisterPackage for in-process typed exports, or pass the
// result to runtime.ExportGuestPackage from a WASM guest.
func WithPackageTypes(p runtime.Package, tys map[string]types.Type) runtime.Package {
	blobs, err := types.EncodePackageTypes(tys)
	if err != nil {
		panic(err)
	}
	p.TypeBlobs = blobs
	return p
}
