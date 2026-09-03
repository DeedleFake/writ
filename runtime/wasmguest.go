//go:build js || wasm

package runtime

import (
	"unsafe"

	"deedles.dev/writ/syntax"
)

var (
	guestPkg     Package
	guestAllocs  [][]byte
	guestRet     []byte
	guestHandles = NewGuestHandleTable()
)

// ExportGuestPackage sets the package a WASI reactor exposes through the writ ABI.
func ExportGuestPackage(p Package) {
	guestPkg = p
}

func guestForeignHandle(id uint64) Value {
	if isGuestHandleID(id) {
		return Value{}
	}
	return newWireHandle(id)
}

func dispatchGuestFunc(name string, args []Value) (Value, error) {
	f, ok := guestPkg.Funcs[name]
	if !ok || f == nil {
		return Value{}, errf("unknown func %s", name)
	}
	return f(args)
}

func dispatchGuestMacro(name string, args []syntax.Form) (syntax.Form, error) {
	f, ok := guestPkg.Macros[name]
	if !ok || f == nil {
		return syntax.Form{}, errf("unknown macro %s", name)
	}
	return f(args)
}

//go:wasmexport writ_abi
func guestWritABI() int32 { return 1 }

//go:wasmexport writ_alloc
func guestWritAlloc(n int32) int32 {
	if n < 0 {
		n = 0
	}
	b := make([]byte, n)
	guestAllocs = append(guestAllocs, b)
	if n == 0 {
		return 0
	}
	return int32(uintptr(unsafe.Pointer(&b[0])))
}

//go:wasmexport writ_package
func guestWritPackage() int32 {
	blob, err := EncodePackageTable(guestPkg, guestHandles)
	if err != nil {
		return retainGuest(EncodeABIError(err.Error()))
	}
	return retainGuest(blob)
}

//go:wasmexport writ_call
func guestWritCall(kind, namePtr, nameLen, argsPtr, argsLen int32) int32 {
	name := string(guestRead(namePtr, nameLen))
	argsBlob := guestRead(argsPtr, argsLen)
	guestAllocs = nil
	if kind == callKindMacro {
		argsForm, err := DecodeForm(argsBlob)
		if err != nil {
			return retainGuest(EncodeABIError(err.Error()))
		}
		if argsForm.Kind() != syntax.KindList {
			return retainGuest(EncodeABIError("args must be a list"))
		}
		result, err := dispatchGuestMacro(name, argsForm.Items())
		if err != nil {
			return retainGuest(EncodeABIError(err.Error()))
		}
		blob, err := EncodeForm(result)
		if err != nil {
			return retainGuest(EncodeABIError(err.Error()))
		}
		return retainGuest(blob)
	}
	argsVal, err := DecodeForeign(argsBlob, guestHandles, guestForeignHandle)
	if err != nil {
		return retainGuest(EncodeABIError(err.Error()))
	}
	if argsVal.k != KindList {
		return retainGuest(EncodeABIError("args must be a list"))
	}
	result, err := dispatchGuestFunc(name, argsVal.Items())
	if err != nil {
		return retainGuest(EncodeABIError(err.Error()))
	}
	blob, err := Encode(result, guestHandles)
	if err != nil {
		return retainGuest(EncodeABIError(err.Error()))
	}
	return retainGuest(blob)
}

func retainGuest(b []byte) int32 {
	guestRet = b
	if len(b) == 0 {
		return 0
	}
	return int32(uintptr(unsafe.Pointer(&b[0])))
}

func guestRead(ptr, length int32) []byte {
	if length <= 0 || ptr == 0 {
		return nil
	}
	s := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(uint32(ptr)))), int(length))
	out := make([]byte, len(s))
	copy(out, s)
	return out
}
