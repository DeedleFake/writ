//go:build js || wasm

package runtime

import "unsafe"

var (
	guestAllocs [][]byte
	guestRet    []byte
)

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
	blob, err := EncodePackageTable(guestPkg, nil)
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
	argsVal, err := Decode(argsBlob, nil)
	if err != nil {
		return retainGuest(EncodeABIError(err.Error()))
	}
	if argsVal.k != KindList {
		return retainGuest(EncodeABIError("args must be a list"))
	}
	result, err := DispatchGuestCall(kind, name, argsVal.Items())
	if err != nil {
		return retainGuest(EncodeABIError(err.Error()))
	}
	blob, err := Encode(result, nil)
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
