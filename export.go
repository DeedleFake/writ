package writ

// WithPackageTypes returns a copy of p with Types set from tys.
// Use with RegisterPackage for in-process typed exports, or pass the
// result to ExportGuestPackage from a WASM guest:
//
//	ExportGuestPackage(WithPackageTypes(p, tys))
//
// tys may be nil. Encode happens in EncodePackageTable, not here.
func WithPackageTypes(p Package, tys map[string]Type) Package {
	if tys == nil {
		p.Types = nil
		return p
	}
	cp := make(map[string]Type, len(tys))
	for k, v := range tys {
		cp[k] = v
	}
	p.Types = cp
	return p
}
