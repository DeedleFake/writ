package types

import (
	"testing"
)

func TestTypeCodecRoundTrip(t *testing.T) {
	cases := []Type{
		Any(),
		NilType(),
		BoolType(),
		TrueType(),
		FalseType(),
		IntType(),
		FloatType(),
		StringType(),
		SymbolType(),
		ExactSymbol("x"),
		UnknownSymbol(),
		EmptyList(),
		EmptyMapType(),
		ListOf(IntType()),
		Tuple(IntType(), StringType()),
		MapType([]FnKey{{Name: "a", Type: IntType()}}, nil),
		MapType(nil, ptrType(StringType())),
		FnType(PosFn(StringType(), IntType())),
		FnType(KeyFn(IntType(), FnKey{Name: "n", Type: IntType()})),
		Union(IntType(), StringType()),
		Dynamic(IntType()),
		OpaqueType(),
		Opaque("wasmhello"),
		Opaque("wasmhello", "box"),
		Native[*struct{ n int }](),
		MacroType(),
	}
	for _, want := range cases {
		b, err := Encode(want)
		if err != nil {
			t.Fatalf("encode %s: %v", PrintType(want), err)
		}
		got, err := Decode(b)
		if err != nil {
			t.Fatalf("decode %s: %v", PrintType(want), err)
		}
		if !sameType(want, got) {
			t.Fatalf("round-trip %s -> %s", PrintType(want), PrintType(got))
		}
	}
}

func ptrType(t Type) *Type { return &t }

func TestTypeCodecBadVersion(t *testing.T) {
	if _, err := Decode([]byte{99}); err == nil {
		t.Fatal("expected version error")
	}
	if _, err := Decode([]byte{1}); err == nil {
		t.Fatal("expected reject of codec v1")
	}
}

func TestPrintTypeWritShape(t *testing.T) {
	cases := []struct {
		t    Type
		want string
	}{
		{IntType(), "int"},
		{StringType(), "string"},
		{UnknownSymbol(), "unknown_symbol"},
		{MacroType(), "macro"},
		{OpaqueType(), "opaque"},
		{Opaque("pkg"), "(opaque from pkg)"},
		{Opaque("pkg", "Box"), "(opaque from pkg named Box)"},
		{ListOf(IntType()), "(list int)"},
		{Tuple(IntType(), StringType()), "[int string]"},
		{EmptyList(), "[]"},
		{EmptyMapType(), "[:]"},
		{MapType([]FnKey{{Name: "a", Type: StringType()}}, ptrType(IntType())), "['a: string symbol: int]"},
		{FnType(PosFn(StringType(), IntType())), "(fn (int) string)"},
		{FnType(PosFn(IntType()), PosFn(StringType(), StringType())), "(fn () int) and (fn (string) string)"},
		{Union(IntType(), StringType()), "int or string"},
		{Dynamic(IntType()), "(dynamic int)"},
		{BoolType(), "bool"},
	}
	for _, c := range cases {
		if got := PrintType(c.t); got != c.want {
			t.Fatalf("PrintType: got %q want %q", got, c.want)
		}
	}
}
