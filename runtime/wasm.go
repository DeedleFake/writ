//go:build !js && !wasm

package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sync"

	"deedles.dev/writ/syntax"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

// LoadWasm instantiates a WASI reactor and reads its writ package table.
func LoadWasm(path string) (Package, error) { return loadWasm(path) }

func loadWasm(path string) (Package, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Package{}, err
	}
	return loadWasmBytes(data)
}

func loadWasmBytes(data []byte) (Package, error) {
	if len(data) < 4 || string(data[:4]) != "\x00asm" {
		return Package{}, errMsg("not a wasm module")
	}
	ctx := context.Background()
	r := wazero.NewRuntime(ctx)
	ok := false
	defer func() {
		if !ok {
			_ = r.Close(ctx)
		}
	}()
	if _, err := wasi_snapshot_preview1.Instantiate(ctx, r); err != nil {
		return Package{}, err
	}
	w := &wasmInst{ctx: ctx, r: r, handles: NewHandleTable()}
	_, err := r.NewHostModuleBuilder("writ").
		NewFunctionBuilder().
		WithFunc(w.hostApply).
		Export("writ_host_apply").
		Instantiate(ctx)
	if err != nil {
		return Package{}, err
	}
	cfg := wazero.NewModuleConfig().
		WithStartFunctions().
		WithStdin(bytes.NewReader(nil)).
		WithStdout(io.Discard).
		WithStderr(io.Discard)
	mod, err := r.InstantiateWithConfig(ctx, data, cfg)
	if err != nil {
		return Package{}, err
	}
	w.mod = mod
	if init := mod.ExportedFunction("_initialize"); init != nil {
		if _, err := init.Call(ctx); err != nil {
			return Package{}, fmt.Errorf("wasm _initialize: %w", err)
		}
	}
	if err := w.checkABI(); err != nil {
		return Package{}, err
	}
	pkg, err := w.readPackage()
	if err != nil {
		return Package{}, err
	}
	ok = true
	return pkg, nil
}

type wasmInst struct {
	mu           sync.Mutex
	ctx          context.Context
	r            wazero.Runtime
	mod          api.Module
	handles      *HandleTable
	guestProxies map[uint64]Value
}

// guestRef is a host-side proxy for a Native that lives in a WASM guest.
type guestRef struct {
	w  *wasmInst
	id uint64
}

func (w *wasmInst) foreignGuestHandle(id uint64) Value {
	if !isGuestHandleID(id) {
		return Value{}
	}
	if w.guestProxies == nil {
		w.guestProxies = map[uint64]Value{}
	}
	if v, ok := w.guestProxies[id]; ok {
		return v
	}
	v := Native(&guestRef{w: w, id: id})
	w.guestProxies[id] = v
	return v
}

// HandleID implements HandlePeer for encoding args into this module.
func (w *wasmInst) HandleID(v Value) (uint64, bool) {
	if v.k != KindNative {
		return 0, false
	}
	gr, ok := v.p.(*guestRef)
	if !ok || gr == nil {
		return 0, false
	}
	if gr.w == w {
		return gr.id, true
	}
	return w.handles.Put(v), true
}

func (w *wasmInst) checkABI() error {
	fn := w.mod.ExportedFunction("writ_abi")
	if fn == nil {
		return errMsg("missing writ_abi")
	}
	res, err := fn.Call(w.ctx)
	if err != nil {
		return err
	}
	if len(res) == 0 || api.DecodeI32(res[0]) != 1 {
		return errMsg("unsupported writ abi")
	}
	if w.mod.Memory() == nil {
		return errMsg("missing memory")
	}
	if w.mod.ExportedFunction("writ_alloc") == nil {
		return errMsg("missing writ_alloc")
	}
	if w.mod.ExportedFunction("writ_package") == nil {
		return errMsg("missing writ_package")
	}
	if w.mod.ExportedFunction("writ_call") == nil {
		return errMsg("missing writ_call")
	}
	return nil
}

func (w *wasmInst) readPackage() (Package, error) {
	fn := w.mod.ExportedFunction("writ_package")
	res, err := fn.Call(w.ctx)
	if err != nil {
		return Package{}, err
	}
	if len(res) == 0 {
		return Package{}, errMsg("writ_package returned nothing")
	}
	ptr := api.DecodeI32(res[0])
	r := w.memReader(ptr)
	tag, ok := w.mod.Memory().ReadByte(uint32(ptr))
	if !ok {
		return Package{}, errMsg("invalid writ_package pointer")
	}
	if tag == tagError {
		msg, err := DecodeABIError(r)
		if err != nil {
			return Package{}, err
		}
		return Package{}, errMsg(msg)
	}
	pkg, err := DecodePackageTableForeign(r, w.handles, w.foreignGuestHandle)
	if err != nil {
		return Package{}, err
	}
	if pkg.Funcs == nil {
		pkg.Funcs = map[string]Func{}
	}
	if pkg.Macros == nil {
		pkg.Macros = map[string]Macro{}
	}
	if pkg.Vals == nil {
		pkg.Vals = map[string]Value{}
	}
	for name := range pkg.Funcs {
		n := name
		pkg.Funcs[n] = func(args []Value) (Value, error) {
			return w.call(callKindFunc, n, args)
		}
	}
	for name := range pkg.Macros {
		n := name
		pkg.Macros[n] = func(args []syntax.Form) (syntax.Form, error) {
			return w.callMacro(n, args)
		}
	}
	return pkg, nil
}

func (w *wasmInst) call(kind int32, name string, args []Value) (Value, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	blob, err := EncodePeer(CallList(args...), w.handles, w)
	if err != nil {
		return Value{}, err
	}
	nameBytes := []byte(name)
	namePtr, err := w.allocWrite(nameBytes)
	if err != nil {
		return Value{}, err
	}
	argsPtr, err := w.allocWrite(blob)
	if err != nil {
		return Value{}, err
	}
	fn := w.mod.ExportedFunction("writ_call")
	res, err := fn.Call(w.ctx,
		api.EncodeI32(kind),
		api.EncodeI32(namePtr),
		api.EncodeI32(int32(len(nameBytes))),
		api.EncodeI32(argsPtr),
		api.EncodeI32(int32(len(blob))),
	)
	if err != nil {
		return Value{}, err
	}
	if len(res) == 0 {
		return Value{}, errMsg("writ_call returned nothing")
	}
	return w.decodeResult(api.DecodeI32(res[0]))
}

func (w *wasmInst) callMacro(name string, args []syntax.Form) (syntax.Form, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	blob, err := EncodeForm(syntax.CallList(args...))
	if err != nil {
		return syntax.Form{}, err
	}
	nameBytes := []byte(name)
	namePtr, err := w.allocWrite(nameBytes)
	if err != nil {
		return syntax.Form{}, err
	}
	argsPtr, err := w.allocWrite(blob)
	if err != nil {
		return syntax.Form{}, err
	}
	fn := w.mod.ExportedFunction("writ_call")
	res, err := fn.Call(w.ctx,
		api.EncodeI32(callKindMacro),
		api.EncodeI32(namePtr),
		api.EncodeI32(int32(len(nameBytes))),
		api.EncodeI32(argsPtr),
		api.EncodeI32(int32(len(blob))),
	)
	if err != nil {
		return syntax.Form{}, err
	}
	if len(res) == 0 {
		return syntax.Form{}, errMsg("writ_call returned nothing")
	}
	return w.decodeFormResult(api.DecodeI32(res[0]))
}

func (w *wasmInst) decodeFormResult(ptr int32) (syntax.Form, error) {
	mem := w.mod.Memory()
	if mem == nil {
		return syntax.Form{}, errMsg("missing memory")
	}
	tag, ok := mem.ReadByte(uint32(ptr))
	if !ok {
		return syntax.Form{}, errMsg("invalid result pointer")
	}
	r := w.memReader(ptr)
	if tag == tagError {
		msg, err := DecodeABIError(r)
		if err != nil {
			return syntax.Form{}, err
		}
		return syntax.Form{}, errMsg(msg)
	}
	return DecodeFormReader(r)
}

func (w *wasmInst) allocWrite(b []byte) (int32, error) {
	n := int32(len(b))
	if n == 0 {
		return 0, nil
	}
	alloc := w.mod.ExportedFunction("writ_alloc")
	res, err := alloc.Call(w.ctx, api.EncodeI32(n))
	if err != nil {
		return 0, err
	}
	if len(res) == 0 {
		return 0, errMsg("writ_alloc returned nothing")
	}
	ptr := api.DecodeI32(res[0])
	if !w.mod.Memory().Write(uint32(ptr), b) {
		return 0, errMsg("write guest memory")
	}
	return ptr, nil
}

func (w *wasmInst) decodeResult(ptr int32) (Value, error) {
	mem := w.mod.Memory()
	if mem == nil {
		return Value{}, errMsg("missing memory")
	}
	tag, ok := mem.ReadByte(uint32(ptr))
	if !ok {
		return Value{}, errMsg("invalid result pointer")
	}
	r := w.memReader(ptr)
	if tag == tagError {
		msg, err := DecodeABIError(r)
		if err != nil {
			return Value{}, err
		}
		return Value{}, errMsg(msg)
	}
	return DecodeReaderForeign(r, w.handles, w.foreignGuestHandle)
}

func (w *wasmInst) memReader(ptr int32) io.Reader {
	return &wasmMemReader{mem: w.mod.Memory(), off: uint32(ptr)}
}

func (w *wasmInst) hostApply(ctx context.Context, mod api.Module, handle int64, argsPtr, argsLen int32) int32 {
	write := func(blob []byte) int32 {
		ptr, err := w.allocWriteOn(ctx, mod, blob)
		if err != nil {
			return 0
		}
		return ptr
	}
	v, ok := w.handles.Get(uint64(handle))
	if !ok {
		return write(EncodeABIError("missing handle"))
	}
	if v.k != KindFn {
		return write(EncodeABIError("handle is not a function"))
	}
	raw, err := readMem(mod.Memory(), uint32(argsPtr), uint32(argsLen))
	if err != nil {
		return write(EncodeABIError(err.Error()))
	}
	argsVal, err := DecodeForeign(raw, w.handles, w.foreignGuestHandle)
	if err != nil {
		return write(EncodeABIError(err.Error()))
	}
	if argsVal.k != KindList {
		return write(EncodeABIError("args must be a list"))
	}
	result, err := applyFn(v, callParts{pos: argsVal.Items(), keys: map[string]Value{}}, makeEnv(nil), newCtx(nil, nil, nil))
	if err != nil {
		return write(EncodeABIError(err.Error()))
	}
	blob, err := EncodePeer(result, w.handles, w)
	if err != nil {
		return write(EncodeABIError(err.Error()))
	}
	return write(blob)
}

func (w *wasmInst) allocWriteOn(ctx context.Context, mod api.Module, b []byte) (int32, error) {
	n := int32(len(b))
	if n == 0 {
		return 0, nil
	}
	alloc := mod.ExportedFunction("writ_alloc")
	if alloc == nil {
		return 0, errMsg("missing writ_alloc")
	}
	res, err := alloc.Call(ctx, api.EncodeI32(n))
	if err != nil {
		return 0, err
	}
	if len(res) == 0 {
		return 0, errMsg("writ_alloc returned nothing")
	}
	ptr := api.DecodeI32(res[0])
	if !mod.Memory().Write(uint32(ptr), b) {
		return 0, errMsg("write guest memory")
	}
	return ptr, nil
}

type wasmMemReader struct {
	mem api.Memory
	off uint32
}

func (r *wasmMemReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.mem == nil {
		return 0, io.EOF
	}
	size := r.mem.Size()
	if r.off >= size {
		return 0, io.EOF
	}
	n := uint32(len(p))
	if r.off+n > size {
		n = size - r.off
	}
	buf, ok := r.mem.Read(r.off, n)
	if !ok {
		return 0, io.ErrUnexpectedEOF
	}
	copied := copy(p, buf)
	r.off += uint32(copied)
	if copied < len(p) {
		return copied, io.EOF
	}
	return copied, nil
}

func readMem(mem api.Memory, off, n uint32) ([]byte, error) {
	if n == 0 {
		return nil, nil
	}
	if mem == nil {
		return nil, errMsg("missing memory")
	}
	buf, ok := mem.Read(off, n)
	if !ok {
		return nil, errMsg("read guest memory")
	}
	out := make([]byte, len(buf))
	copy(out, buf)
	return out, nil
}
