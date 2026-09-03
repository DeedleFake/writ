package runtime

var guestPkg Package

// ExportGuestPackage sets the package a WASI reactor exposes through the writ ABI.
func ExportGuestPackage(p Package) {
	guestPkg = p
}

func dispatchGuestCall(kind int32, name string, args []Value) (Value, error) {
	switch kind {
	case callKindFunc:
		f, ok := guestPkg.Funcs[name]
		if !ok || f == nil {
			return Value{}, errf("unknown func %s", name)
		}
		return f(args)
	case callKindMacro:
		f, ok := guestPkg.Macros[name]
		if !ok || f == nil {
			return Value{}, errf("unknown macro %s", name)
		}
		return f(args)
	default:
		return Value{}, errf("unknown call kind %d", kind)
	}
}
