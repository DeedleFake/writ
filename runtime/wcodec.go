package runtime

import (
	"encoding/binary"
	"io"
	"math"
	"math/big"
	"sort"
	"sync"

	"deedles.dev/writ/syntax"
)

const (
	tagInt     byte = 1
	tagFloat   byte = 2
	tagString  byte = 3
	tagSymbol  byte = 4
	tagList    byte = 5
	tagMap     byte = 6
	tagQuote   byte = 7
	tagUnquote byte = 8
	tagSplice  byte = 9
	tagComment byte = 10
	tagHandle  byte = 11
	tagError   byte = 0xff

	intCompactI64 byte = 0
	intCompactBig byte = 1
)

const (
	pkgKindVal   byte = 0
	pkgKindFunc  byte = 1
	pkgKindMacro byte = 2
)

const (
	callKindFunc  int32 = 0
	callKindMacro int32 = 1
)

// guestHandleBit marks handle ids allocated by a WASM guest.
// Host ids keep the bit clear so the two tables share one tagHandle
// wire format without colliding.
const guestHandleBit uint64 = 1 << 63

func isGuestHandleID(id uint64) bool { return id&guestHandleBit != 0 }

// wireHandle is a peer-owned opaque id carried as KindNative.
// Guest ids have guestHandleBit set; host ids do not.
type wireHandle struct{ id uint64 }

// newWireHandle boxes a peer handle id as a Native value.
func newWireHandle(id uint64) Value {
	return Native(&wireHandle{id: id})
}

func wireHandleID(v Value) (uint64, bool) {
	if v.k != KindNative {
		return 0, false
	}
	wh, ok := v.p.(*wireHandle)
	if !ok || wh == nil {
		return 0, false
	}
	return wh.id, true
}

// HandleTable interns Values for WASM host/guest exchange.
// Host tables allocate ids from 1 upward. Guest tables set guestHandleBit.
type HandleTable struct {
	mu    sync.Mutex
	next  uint64
	guest bool
	vals  map[uint64]Value
}

// NewHandleTable returns an empty host-side table.
func NewHandleTable() *HandleTable {
	return &HandleTable{next: 1, vals: map[uint64]Value{}}
}

// NewGuestHandleTable returns a table whose Put ids have guestHandleBit set.
func NewGuestHandleTable() *HandleTable {
	return &HandleTable{next: 1, guest: true, vals: map[uint64]Value{}}
}

// Put stores v and returns its handle id.
func (t *HandleTable) Put(v Value) uint64 {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.vals == nil {
		t.vals = map[uint64]Value{}
	}
	if t.next == 0 {
		t.next = 1
	}
	id := t.next
	t.next++
	if t.guest {
		id |= guestHandleBit
	}
	t.vals[id] = v
	return id
}

// Get looks up a handle id.
func (t *HandleTable) Get(id uint64) (Value, bool) {
	if t == nil {
		return Value{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	v, ok := t.vals[id]
	return v, ok
}

// Drop removes a handle id.
func (t *HandleTable) Drop(id uint64) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.vals, id)
}

// HandlePeer maps opaque Values to wire handle ids for one destination.
// Used by the WASM host so a guest-owned ref encodes as that guest's local
// id, while a foreign guest ref is imported into the destination's table.
type HandlePeer interface {
	HandleID(v Value) (uint64, bool)
}

// Encode writes v in the tagged binary format.
func Encode(v Value, ht *HandleTable) ([]byte, error) {
	return EncodePeer(v, ht, nil)
}

// EncodePeer is Encode with a destination-specific opaque id mapper.
func EncodePeer(v Value, ht *HandleTable, peer HandlePeer) ([]byte, error) {
	e := enc{peer: peer}
	if err := e.value(v, ht); err != nil {
		return nil, err
	}
	return e.buf, nil
}

// Decode reads one value from b.
func Decode(b []byte, ht *HandleTable) (Value, error) {
	return DecodeForeign(b, ht, nil)
}

// DecodeForeign reads one value from b. Missing handle ids are resolved
// with foreign when non-nil (peer-owned opaques).
func DecodeForeign(b []byte, ht *HandleTable, foreign func(uint64) Value) (Value, error) {
	return DecodeReaderForeign(newByteReader(b), ht, foreign)
}

// DecodeReader reads one value from r.
func DecodeReader(r io.Reader, ht *HandleTable) (Value, error) {
	return DecodeReaderForeign(r, ht, nil)
}

// DecodeReaderForeign is DecodeReader with a foreign-handle resolver.
func DecodeReaderForeign(r io.Reader, ht *HandleTable, foreign func(uint64) Value) (Value, error) {
	d := dec{r: r, foreign: foreign}
	return d.value(ht)
}

type enc struct {
	buf  []byte
	peer HandlePeer
}

func (e *enc) u8(v byte) { e.buf = append(e.buf, v) }

func (e *enc) i8(v int8) { e.buf = append(e.buf, byte(v)) }

func (e *enc) u32(v uint32) {
	e.buf = binary.LittleEndian.AppendUint32(e.buf, v)
}

func (e *enc) u64(v uint64) {
	e.buf = binary.LittleEndian.AppendUint64(e.buf, v)
}

func (e *enc) i64(v int64) {
	e.u64(uint64(v))
}

func (e *enc) blob(b []byte) {
	e.u32(uint32(len(b)))
	e.buf = append(e.buf, b...)
}

func (e *enc) str(s string) { e.blob([]byte(s)) }

func (e *enc) value(v Value, ht *HandleTable) error {
	switch v.k {
	case KindInt:
		e.u8(tagInt)
		if b := v.bigInt(); b != nil && !b.IsInt64() {
			e.u8(intCompactBig)
			abs := b.Bytes()
			e.blob(abs)
			sign := int8(1)
			if b.Sign() < 0 {
				sign = -1
			}
			e.i8(sign)
			return nil
		}
		e.u8(intCompactI64)
		if b := v.bigInt(); b != nil {
			e.i64(b.Int64())
			return nil
		}
		e.i64(v.n)
		return nil
	case KindFloat:
		e.u8(tagFloat)
		e.u64(math.Float64bits(v.floatVal()))
		return nil
	case KindString:
		e.u8(tagString)
		e.str(v.s)
		return nil
	case KindSymbol:
		e.u8(tagSymbol)
		e.str(v.Name())
		return nil
	case KindList:
		e.u8(tagList)
		if v.IsVec() {
			e.u8(1)
		} else {
			e.u8(0)
		}
		xs := v.Items()
		e.u32(uint32(len(xs)))
		for _, x := range xs {
			if err := e.value(x, ht); err != nil {
				return err
			}
		}
		return nil
	case KindMap:
		e.u8(tagMap)
		pairs := v.Pairs()
		e.u32(uint32(len(pairs)))
		for _, p := range pairs {
			if err := e.value(p.Key, ht); err != nil {
				return err
			}
			if err := e.value(p.Value, ht); err != nil {
				return err
			}
		}
		return nil
	case KindSyntax:
		f, ok := v.Form()
		if !ok {
			return errMsg("invalid syntax value")
		}
		return e.form(f)
	case KindFn, KindMacro, KindNative:
		if e.peer != nil {
			if id, ok := e.peer.HandleID(v); ok {
				e.u8(tagHandle)
				e.u64(id)
				return nil
			}
		}
		if id, ok := wireHandleID(v); ok {
			e.u8(tagHandle)
			e.u64(id)
			return nil
		}
		if ht == nil {
			return errMsg("handle table required")
		}
		e.u8(tagHandle)
		e.u64(ht.Put(v))
		return nil
	default:
		return errf("cannot encode %s", v.k)
	}
}

type dec struct {
	r       io.Reader
	foreign func(uint64) Value
}

func (d *dec) u8() (byte, error) {
	var b [1]byte
	if _, err := io.ReadFull(d.r, b[:]); err != nil {
		return 0, err
	}
	return b[0], nil
}

func (d *dec) i8() (int8, error) {
	v, err := d.u8()
	return int8(v), err
}

func (d *dec) fill(n int) ([]byte, error) {
	if n < 0 {
		return nil, errMsg("negative length")
	}
	if n == 0 {
		return nil, nil
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(d.r, b); err != nil {
		return nil, err
	}
	return b, nil
}

func (d *dec) u32() (uint32, error) {
	b, err := d.fill(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(b), nil
}

func (d *dec) u64() (uint64, error) {
	b, err := d.fill(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(b), nil
}

func (d *dec) i64() (int64, error) {
	v, err := d.u64()
	return int64(v), err
}

func (d *dec) blob() ([]byte, error) {
	n, err := d.u32()
	if err != nil {
		return nil, err
	}
	return d.fill(int(n))
}

func (d *dec) str() (string, error) {
	b, err := d.blob()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (d *dec) value(ht *HandleTable) (Value, error) {
	tag, err := d.u8()
	if err != nil {
		return Value{}, err
	}
	switch tag {
	case tagInt:
		mode, err := d.u8()
		if err != nil {
			return Value{}, err
		}
		switch mode {
		case intCompactI64:
			n, err := d.i64()
			if err != nil {
				return Value{}, err
			}
			return Int64(n), nil
		case intCompactBig:
			abs, err := d.blob()
			if err != nil {
				return Value{}, err
			}
			sign, err := d.i8()
			if err != nil {
				return Value{}, err
			}
			if sign != 1 && sign != -1 {
				return Value{}, errMsg("invalid int sign")
			}
			n := new(big.Int).SetBytes(abs)
			if sign < 0 {
				n.Neg(n)
			}
			return Int(n), nil
		default:
			return Value{}, errf("unknown int compact %d", mode)
		}
	case tagFloat:
		bits, err := d.u64()
		if err != nil {
			return Value{}, err
		}
		return Float(math.Float64frombits(bits)), nil
	case tagString:
		s, err := d.str()
		if err != nil {
			return Value{}, err
		}
		return String(s), nil
	case tagSymbol:
		s, err := d.str()
		if err != nil {
			return Value{}, err
		}
		return Symbol(s), nil
	case tagList:
		vec, err := d.u8()
		if err != nil {
			return Value{}, err
		}
		if vec != 0 && vec != 1 {
			return Value{}, errf("invalid list vecflag %d", vec)
		}
		n, err := d.u32()
		if err != nil {
			return Value{}, err
		}
		xs := make([]Value, n)
		for i := range xs {
			xs[i], err = d.value(ht)
			if err != nil {
				return Value{}, err
			}
		}
		return listVal(xs, vec == 1), nil
	case tagMap:
		n, err := d.u32()
		if err != nil {
			return Value{}, err
		}
		pairs := make([]MapPair, n)
		for i := range pairs {
			k, err := d.value(ht)
			if err != nil {
				return Value{}, err
			}
			val, err := d.value(ht)
			if err != nil {
				return Value{}, err
			}
			pairs[i] = MapPair{Key: k, Value: val}
		}
		return MapFrom(pairs...), nil
	case tagQuote:
		inner, err := d.form()
		if err != nil {
			return Value{}, err
		}
		return Syntax(syntax.Quote(inner)), nil
	case tagUnquote:
		inner, err := d.form()
		if err != nil {
			return Value{}, err
		}
		return Syntax(syntax.Unquote(inner)), nil
	case tagSplice:
		inner, err := d.form()
		if err != nil {
			return Value{}, err
		}
		return Syntax(syntax.Splice(inner)), nil
	case tagComment:
		s, err := d.str()
		if err != nil {
			return Value{}, err
		}
		return Syntax(syntax.Comment(s)), nil
	case tagHandle:
		id, err := d.u64()
		if err != nil {
			return Value{}, err
		}
		if v, ok := ht.Get(id); ok {
			return v, nil
		}
		if d.foreign != nil {
			if v := d.foreign(id); v.k != KindInvalid {
				return v, nil
			}
		}
		return Value{}, errf("missing handle %d", id)
	default:
		return Value{}, errf("unknown value tag %d", tag)
	}
}

type byteReader struct {
	b []byte
}

func newByteReader(b []byte) *byteReader { return &byteReader{b: b} }

func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		if len(p) == 0 {
			return 0, nil
		}
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

func EncodeABIError(msg string) []byte {
	var e enc
	e.u8(tagError)
	e.str(msg)
	return e.buf
}

// DecodeABIError reads an ABI error payload starting at tag 0xff.
func DecodeABIError(r io.Reader) (string, error) {
	d := dec{r: r}
	tag, err := d.u8()
	if err != nil {
		return "", err
	}
	if tag != tagError {
		return "", errf("not an error tag: %d", tag)
	}
	return d.str()
}

// EncodePackageTable encodes a WASM package export table.
func EncodePackageTable(p Package, ht *HandleTable) ([]byte, error) {
	type entry struct {
		kind byte
		name string
		val  Value
	}
	var ents []entry
	names := make([]string, 0, len(p.Vals))
	for k := range p.Vals {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, n := range names {
		ents = append(ents, entry{kind: pkgKindVal, name: n, val: p.Vals[n]})
	}
	names = names[:0]
	for k := range p.Funcs {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, n := range names {
		ents = append(ents, entry{kind: pkgKindFunc, name: n})
	}
	names = names[:0]
	for k := range p.Macros {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, n := range names {
		ents = append(ents, entry{kind: pkgKindMacro, name: n})
	}
	var e enc
	e.u32(uint32(len(ents)))
	for _, ent := range ents {
		e.u8(ent.kind)
		e.str(ent.name)
		if ent.kind == pkgKindVal {
			if err := e.value(ent.val, ht); err != nil {
				return nil, err
			}
		}
	}
	return e.buf, nil
}

// DecodePackageTable reads a WASM package export table.
// Func and macro bodies are not included; the host binds them to writ_call.
func DecodePackageTable(r io.Reader, ht *HandleTable) (Package, error) {
	return DecodePackageTableForeign(r, ht, nil)
}

// DecodePackageTableForeign is DecodePackageTable with a foreign-handle resolver.
func DecodePackageTableForeign(r io.Reader, ht *HandleTable, foreign func(uint64) Value) (Package, error) {
	d := dec{r: r, foreign: foreign}
	n, err := d.u32()
	if err != nil {
		return Package{}, err
	}
	p := Package{
		Funcs:  map[string]Func{},
		Vals:   map[string]Value{},
		Macros: map[string]Macro{},
	}
	for range n {
		kind, err := d.u8()
		if err != nil {
			return Package{}, err
		}
		name, err := d.str()
		if err != nil {
			return Package{}, err
		}
		switch kind {
		case pkgKindVal:
			v, err := d.value(ht)
			if err != nil {
				return Package{}, err
			}
			p.Vals[name] = v
		case pkgKindFunc:
			p.Funcs[name] = nil
		case pkgKindMacro:
			p.Macros[name] = nil
		default:
			return Package{}, errf("unknown package entry kind %d", kind)
		}
	}
	return p, nil
}
