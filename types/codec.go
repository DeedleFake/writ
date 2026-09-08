package types

import (
	"encoding/binary"
	"fmt"
	"math"
)

// Wire format for package export types (WASM package table /
// Package.TypeBlobs). Versioned; unknown versions fail closed.
//
// Version 2: Writ-shaped types — no exact strings / unknown_string,
// opaque package+name (not reflect native IDs), macro atom, no rest
// flag on function clauses.

const typeCodecVersion byte = 2

const (
	wireNone byte = iota + 1
	wireAny
	wireNil
	wireBool
	wireInt
	wireFloat
	wireStr
	wireSym
	wireUSym
	wireEmptyList
	wireEmptyMap
	wireList
	wireTuple
	wireMap
	wireFn
	wireOr
	wireDyn
	wireOpaque
	wireMacro
)

const (
	clauseFlagKey byte = 1
)

// Encode serializes t for package export tables.
func Encode(t Type) ([]byte, error) {
	var e typeEnc
	e.u8(typeCodecVersion)
	if err := e.typ(t); err != nil {
		return nil, err
	}
	return e.buf, nil
}

// EncodePackageTypes encodes each type for Package.TypeBlobs.
func EncodePackageTypes(m map[string]Type) (map[string][]byte, error) {
	if len(m) == 0 {
		return nil, nil
	}
	out := make(map[string][]byte, len(m))
	for name, t := range m {
		b, err := Encode(t)
		if err != nil {
			return nil, err
		}
		if len(b) > 0 {
			out[name] = b
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// MustEncode is Encode panicking on error.
func MustEncode(t Type) []byte {
	b, err := Encode(t)
	if err != nil {
		panic(err)
	}
	return b
}

// Decode reads a type produced by [Encode].
func Decode(b []byte) (Type, error) {
	d := typeDec{b: b}
	ver, err := d.u8()
	if err != nil {
		return Type{}, err
	}
	if ver != typeCodecVersion {
		return Type{}, fmt.Errorf("unsupported type codec version %d", ver)
	}
	return d.typ()
}

type typeEnc struct {
	buf []byte
}

func (e *typeEnc) u8(v byte) { e.buf = append(e.buf, v) }

func (e *typeEnc) u32(v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	e.buf = append(e.buf, b[:]...)
}

func (e *typeEnc) str(s string) {
	if len(s) > math.MaxUint32 {
		s = s[:math.MaxUint32]
	}
	e.u32(uint32(len(s)))
	e.buf = append(e.buf, s...)
}

func (e *typeEnc) typ(t Type) error {
	switch t.k {
	case tyNone:
		e.u8(wireNone)
	case tyAny:
		e.u8(wireAny)
	case tyNil:
		e.u8(wireNil)
	case tyBool:
		e.u8(wireBool)
		if t.has {
			e.u8(1)
			if t.b {
				e.u8(1)
			} else {
				e.u8(0)
			}
		} else {
			e.u8(0)
		}
	case tyInt:
		e.u8(wireInt)
	case tyFloat:
		e.u8(wireFloat)
	case tyStr:
		e.u8(wireStr)
	case tySym:
		e.u8(wireSym)
		if t.has {
			e.u8(1)
			e.str(t.s)
		} else {
			e.u8(0)
		}
	case tyUSym:
		e.u8(wireUSym)
	case tyEmptyList:
		e.u8(wireEmptyList)
	case tyEmptyMap:
		e.u8(wireEmptyMap)
	case tyList:
		e.u8(wireList)
		if t.inner == nil {
			return fmt.Errorf("list type missing element")
		}
		return e.typ(*t.inner)
	case tyTuple:
		e.u8(wireTuple)
		e.u32(uint32(len(t.items)))
		for _, x := range t.items {
			if err := e.typ(x); err != nil {
				return err
			}
		}
	case tyMap:
		e.u8(wireMap)
		e.u32(uint32(len(t.fields)))
		for _, f := range t.fields {
			e.str(f.name)
			if err := e.typ(f.t); err != nil {
				return err
			}
		}
		if t.rest != nil {
			e.u8(1)
			if err := e.typ(*t.rest); err != nil {
				return err
			}
		} else {
			e.u8(0)
		}
	case tyFn:
		e.u8(wireFn)
		e.u32(uint32(len(t.clauses)))
		for _, cl := range t.clauses {
			if err := e.clause(cl); err != nil {
				return err
			}
		}
	case tyOr:
		e.u8(wireOr)
		e.u32(uint32(len(t.items)))
		for _, x := range t.items {
			if err := e.typ(x); err != nil {
				return err
			}
		}
	case tyDyn:
		e.u8(wireDyn)
		if t.inner == nil {
			return fmt.Errorf("dynamic type missing inner")
		}
		return e.typ(*t.inner)
	case tyOpaque:
		e.u8(wireOpaque)
		e.str(t.pkg)
		e.str(t.name)
	case tyMacro:
		e.u8(wireMacro)
	default:
		return fmt.Errorf("cannot encode type kind %d", t.k)
	}
	return nil
}

func (e *typeEnc) clause(cl FnClause) error {
	var flags byte
	if cl.Key {
		flags |= clauseFlagKey
	}
	e.u8(flags)
	if cl.Key {
		e.u32(uint32(len(cl.Keys)))
		for _, k := range cl.Keys {
			e.str(k.Name)
			if err := e.typ(k.Type); err != nil {
				return err
			}
		}
	} else {
		e.u32(uint32(len(cl.Args)))
		for _, a := range cl.Args {
			if err := e.typ(a); err != nil {
				return err
			}
		}
	}
	return e.typ(cl.Result)
}

type typeDec struct {
	b []byte
	i int
}

func (d *typeDec) u8() (byte, error) {
	if d.i >= len(d.b) {
		return 0, fmt.Errorf("type codec eof")
	}
	v := d.b[d.i]
	d.i++
	return v, nil
}

func (d *typeDec) fill(n int) ([]byte, error) {
	if n < 0 || d.i+n > len(d.b) {
		return nil, fmt.Errorf("type codec eof")
	}
	s := d.b[d.i : d.i+n]
	d.i += n
	return s, nil
}

func (d *typeDec) u32() (uint32, error) {
	s, err := d.fill(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(s), nil
}

func (d *typeDec) str() (string, error) {
	n, err := d.u32()
	if err != nil {
		return "", err
	}
	s, err := d.fill(int(n))
	if err != nil {
		return "", err
	}
	return string(s), nil
}

func (d *typeDec) typ() (Type, error) {
	kind, err := d.u8()
	if err != nil {
		return Type{}, err
	}
	switch kind {
	case wireNone:
		return None(), nil
	case wireAny:
		return Any(), nil
	case wireNil:
		return NilType(), nil
	case wireBool:
		has, err := d.u8()
		if err != nil {
			return Type{}, err
		}
		if has == 0 {
			return BoolType(), nil
		}
		v, err := d.u8()
		if err != nil {
			return Type{}, err
		}
		if v != 0 {
			return TrueType(), nil
		}
		return FalseType(), nil
	case wireInt:
		return IntType(), nil
	case wireFloat:
		return FloatType(), nil
	case wireStr:
		return StringType(), nil
	case wireSym:
		has, err := d.u8()
		if err != nil {
			return Type{}, err
		}
		if has == 0 {
			return SymbolType(), nil
		}
		s, err := d.str()
		if err != nil {
			return Type{}, err
		}
		return ExactSymbol(s), nil
	case wireUSym:
		return UnknownSymbol(), nil
	case wireEmptyList:
		return EmptyList(), nil
	case wireEmptyMap:
		return EmptyMapType(), nil
	case wireList:
		inner, err := d.typ()
		if err != nil {
			return Type{}, err
		}
		return ListOf(inner), nil
	case wireTuple:
		n, err := d.u32()
		if err != nil {
			return Type{}, err
		}
		items := make([]Type, n)
		for i := range items {
			items[i], err = d.typ()
			if err != nil {
				return Type{}, err
			}
		}
		return Tuple(items...), nil
	case wireMap:
		n, err := d.u32()
		if err != nil {
			return Type{}, err
		}
		keys := make([]FnKey, n)
		for i := range keys {
			name, err := d.str()
			if err != nil {
				return Type{}, err
			}
			t, err := d.typ()
			if err != nil {
				return Type{}, err
			}
			keys[i] = FnKey{Name: name, Type: t}
		}
		hasRest, err := d.u8()
		if err != nil {
			return Type{}, err
		}
		var rest *Type
		if hasRest != 0 {
			rt, err := d.typ()
			if err != nil {
				return Type{}, err
			}
			rest = &rt
		}
		return MapType(keys, rest), nil
	case wireFn:
		n, err := d.u32()
		if err != nil {
			return Type{}, err
		}
		clauses := make([]FnClause, n)
		for i := range clauses {
			clauses[i], err = d.clause()
			if err != nil {
				return Type{}, err
			}
		}
		return FnType(clauses...), nil
	case wireOr:
		n, err := d.u32()
		if err != nil {
			return Type{}, err
		}
		items := make([]Type, n)
		for i := range items {
			items[i], err = d.typ()
			if err != nil {
				return Type{}, err
			}
		}
		return Union(items...), nil
	case wireDyn:
		inner, err := d.typ()
		if err != nil {
			return Type{}, err
		}
		return Dynamic(inner), nil
	case wireOpaque:
		pkg, err := d.str()
		if err != nil {
			return Type{}, err
		}
		name, err := d.str()
		if err != nil {
			return Type{}, err
		}
		if pkg == "" && name == "" {
			return OpaqueType(), nil
		}
		if name == "" {
			return Opaque(pkg), nil
		}
		return Opaque(pkg, name), nil
	case wireMacro:
		return MacroType(), nil
	default:
		return Type{}, fmt.Errorf("unknown type wire kind %d", kind)
	}
}

func (d *typeDec) clause() (FnClause, error) {
	flags, err := d.u8()
	if err != nil {
		return FnClause{}, err
	}
	cl := FnClause{
		Key: flags&clauseFlagKey != 0,
	}
	if cl.Key {
		n, err := d.u32()
		if err != nil {
			return FnClause{}, err
		}
		cl.Keys = make([]FnKey, n)
		for i := range cl.Keys {
			name, err := d.str()
			if err != nil {
				return FnClause{}, err
			}
			t, err := d.typ()
			if err != nil {
				return FnClause{}, err
			}
			cl.Keys[i] = FnKey{Name: name, Type: t}
		}
	} else {
		n, err := d.u32()
		if err != nil {
			return FnClause{}, err
		}
		cl.Args = make([]Type, n)
		for i := range cl.Args {
			cl.Args[i], err = d.typ()
			if err != nil {
				return FnClause{}, err
			}
		}
	}
	cl.Result, err = d.typ()
	if err != nil {
		return FnClause{}, err
	}
	return cl, nil
}
