package runtime

import (
	"bytes"
	"math"
	"math/big"
	"strings"
	"testing"
)

func roundTrip(t *testing.T, v Value, ht *HandleTable) Value {
	t.Helper()
	b, err := Encode(v, ht)
	if err != nil {
		t.Fatalf("encode %s: %v", v, err)
	}
	got, err := Decode(b, ht)
	if err != nil {
		t.Fatalf("decode %s: %v", v, err)
	}
	return got
}

func TestWcodecLiterals(t *testing.T) {
	cases := []Value{
		Int64(0),
		Int64(1),
		Int64(-1),
		Int64(math.MinInt64),
		Int64(math.MaxInt64),
		Int(mustBig("18446744073709551616")),
		Int(mustBig("-18446744073709551616")),
		Int(mustBig("1000000000000000000000000")),
		Float(0),
		Float(math.Copysign(0, -1)),
		Float(1.5),
		Float(-2.25),
		Float(math.Inf(1)),
		Float(math.Inf(-1)),
		String(""),
		String("hello"),
		String("héllo\x00world"),
		Symbol("x"),
		True,
		False,
		Nil,
		List(),
		List(Int64(1), String("a"), Symbol("b")),
		CallList(Symbol("if"), False, Int64(1)),
		EmptyMap(),
		MapFrom(MapPair{Key: Symbol("a"), Value: Int64(1)}, MapPair{Key: Symbol("b"), Value: String("x")}),
		Quote(Symbol("x")),
		Unquote(CallList(Symbol("f"), Int64(1))),
		Splice(List(Int64(1), Int64(2))),
		Comment("; hi"),
	}
	for _, c := range cases {
		got := roundTrip(t, c, nil)
		if c.k == KindFloat && math.IsInf(c.floatVal(), 0) {
			if got.k != KindFloat || got.floatVal() != c.floatVal() {
				t.Fatalf("inf: got %v want %v", got, c)
			}
			continue
		}
		if !got.Equal(c) {
			t.Fatalf("round-trip %s: got %s kind=%s", c, got, got.k)
		}
		if c.k == KindList && got.IsVec() != c.IsVec() {
			t.Fatalf("vecflag %s: got vec=%v", c, got.IsVec())
		}
	}
}

func TestWcodecNaN(t *testing.T) {
	v := Float(math.NaN())
	got := roundTrip(t, v, nil)
	if got.k != KindFloat || !math.IsNaN(got.floatVal()) {
		t.Fatalf("nan: %v", got)
	}
}

func TestWcodecHandle(t *testing.T) {
	ht := NewHandleTable()
	fn := Value{k: KindFn, p: &fnVal{name: "greet", native: func(args []Value) (Value, error) { return String("ok"), nil }}}
	mac := Value{k: KindMacro, p: &fnVal{name: "unless", native: func(args []Value) (Value, error) { return Nil, nil }}}
	nat := Native(3)
	for i, v := range []Value{fn, mac, nat} {
		got := roundTrip(t, v, ht)
		if got.k != v.k {
			t.Fatalf("%d kind: got %s want %s", i, got.k, v.k)
		}
		if v.k == KindNative {
			if !got.Equal(v) {
				t.Fatalf("native: %v", got)
			}
			continue
		}
		if got.fnData() != v.fnData() {
			t.Fatalf("%d handle identity", i)
		}
	}
}

func TestWcodecHandleMissingAndNilTable(t *testing.T) {
	fn := Value{k: KindFn, p: &fnVal{name: "f"}}
	if _, err := Encode(fn, nil); err == nil {
		t.Fatal("encode fn without table")
	}
	ht := NewHandleTable()
	b, err := Encode(fn, ht)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(b, NewHandleTable()); err == nil {
		t.Fatal("missing handle")
	}
	if _, err := Decode(b, nil); err == nil {
		t.Fatal("nil table missing handle")
	}
}

func TestWcodecUnknownTag(t *testing.T) {
	if _, err := Decode([]byte{99}, nil); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("unknown tag: %v", err)
	}
}

func TestWcodecDrop(t *testing.T) {
	ht := NewHandleTable()
	id := ht.Put(Int64(1))
	if id != 1 {
		t.Fatalf("first id %d", id)
	}
	v, ok := ht.Get(id)
	if !ok || !v.Equal(Int64(1)) {
		t.Fatalf("get %v %v", v, ok)
	}
	ht.Drop(id)
	if _, ok := ht.Get(id); ok {
		t.Fatal("dropped")
	}
	ht.Drop(id)
}

func TestWcodecPackageTable(t *testing.T) {
	p := Package{
		Funcs: map[string]Func{"greet": func(args []Value) (Value, error) { return Nil, nil }},
		Vals:  map[string]Value{"version": Int64(1)},
		Macros: map[string]Func{
			"unless": func(args []Value) (Value, error) { return Nil, nil },
		},
	}
	b, err := EncodePackageTable(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodePackageTable(bytes.NewReader(b), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Funcs["greet"]; !ok {
		t.Fatalf("funcs %v", got.Funcs)
	}
	if _, ok := got.Macros["unless"]; !ok {
		t.Fatalf("macros %v", got.Macros)
	}
	v, ok := got.Vals["version"]
	if !ok || !v.Equal(Int64(1)) {
		t.Fatalf("vals %v", got.Vals)
	}
}

func TestWcodecABIError(t *testing.T) {
	b := EncodeABIError("boom")
	if b[0] != tagError {
		t.Fatalf("tag %d", b[0])
	}
	msg, err := DecodeABIError(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if msg != "boom" {
		t.Fatalf("msg %q", msg)
	}
}

func mustBig(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		panic(s)
	}
	return n
}
