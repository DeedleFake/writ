package json_test

import (
	"math"
	"math/big"
	"strings"
	"testing"

	"deedles.dev/writ"
	"deedles.dev/writ/runtime"
	stdjson "deedles.dev/writ/stdlib/json"
)

func TestParseScalars(t *testing.T) {
	cases := []struct {
		in   string
		want runtime.Value
	}{
		{`null`, runtime.Nil},
		{`true`, runtime.True},
		{`false`, runtime.False},
		{`"hi"`, runtime.String("hi")},
		{`0`, runtime.Int64(0)},
		{`-42`, runtime.Int64(-42)},
		{`1.5`, runtime.Float(1.5)},
		{`1e2`, runtime.Float(100)},
		{`1.0`, runtime.Float(1.0)},
	}
	for _, tc := range cases {
		got, err := callParse(tc.in)
		if err != nil {
			t.Errorf("parse %q: %v", tc.in, err)
			continue
		}
		if !got.Equal(tc.want) {
			t.Errorf("parse %q: got %v want %v", tc.in, got, tc.want)
		}
		if tc.want.IsInt() && !got.IsInt() {
			t.Errorf("parse %q: want int kind, got %v", tc.in, got.Kind())
		}
		if tc.want.IsFloat() && !got.IsFloat() {
			t.Errorf("parse %q: want float kind, got %v", tc.in, got.Kind())
		}
	}
}

func TestParseBigInt(t *testing.T) {
	s := "999999999999999999999999999999"
	got, err := callParse(s)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsInt() {
		t.Fatalf("kind %v", got.Kind())
	}
	want, ok := new(big.Int).SetString(s, 10)
	if !ok {
		t.Fatal("big.Int")
	}
	if got.BigInt().Cmp(want) != 0 {
		t.Fatalf("got %v want %v", got.BigInt(), want)
	}
}

func TestParseContainers(t *testing.T) {
	got, err := callParse(`[1, "a", null]`)
	if err != nil {
		t.Fatal(err)
	}
	if !got.IsVec() {
		t.Fatalf("array should be vector, got IsVec=%v", got.IsVec())
	}
	want := runtime.List(runtime.Int64(1), runtime.String("a"), runtime.Nil)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}

	got, err = callParse(`{"x": 1, "y": true}`)
	if err != nil {
		t.Fatal(err)
	}
	want = runtime.MapFrom(
		runtime.MapPair{Key: runtime.Symbol("x"), Value: runtime.Int64(1)},
		runtime.MapPair{Key: runtime.Symbol("y"), Value: runtime.True},
	)
	if !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}

	got, err = callParse(`{}`)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(runtime.EmptyMap()) {
		t.Fatalf("empty object: %v", got)
	}

	got, err = callParse(`[]`)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(runtime.List()) || !got.IsVec() {
		t.Fatalf("empty array: %v IsVec=%v", got, got.IsVec())
	}
}

func TestParseDuplicateKeys(t *testing.T) {
	got, err := callParse(`{"a": 1, "a": 2}`)
	if err != nil {
		t.Fatal(err)
	}
	want := runtime.MapFrom(
		runtime.MapPair{Key: runtime.Symbol("a"), Value: runtime.Int64(2)},
	)
	if !got.Equal(want) {
		t.Fatalf("dupes last-wins: got %v want %v", got, want)
	}
}

func TestParseTrailingData(t *testing.T) {
	_, err := callParse(`1 2`)
	if err == nil {
		t.Fatal("expected trailing data error")
	}
	if !strings.Contains(err.Error(), "json:") {
		t.Fatalf("error should have json: prefix: %v", err)
	}
}

func TestParseRejects(t *testing.T) {
	for _, in := range []string{``, `{`, `undefined`, `'x'`} {
		_, err := callParse(in)
		if err == nil {
			t.Errorf("parse %q: want error", in)
		}
	}
}

func TestStringifyScalars(t *testing.T) {
	cases := []struct {
		in   runtime.Value
		want string
	}{
		{runtime.Nil, `null`},
		{runtime.True, `true`},
		{runtime.False, `false`},
		{runtime.String("hi"), `"hi"`},
		{runtime.Int64(42), `42`},
		{runtime.Int64(-7), `-7`},
		{runtime.Float(1.5), `1.5`},
	}
	for _, tc := range cases {
		got, err := callStringify(tc.in)
		if err != nil {
			t.Errorf("stringify %v: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("stringify %v: got %q want %q", tc.in, got, tc.want)
		}
	}
}

func TestStringifyContainers(t *testing.T) {
	got, err := callStringify(runtime.List(runtime.Int64(1), runtime.Nil))
	if err != nil {
		t.Fatal(err)
	}
	if got != `[1,null]` {
		t.Fatalf("vector: %q", got)
	}

	// Call-shaped lists also encode as arrays.
	got, err = callStringify(runtime.CallList(runtime.Int64(1), runtime.Int64(2)))
	if err != nil {
		t.Fatal(err)
	}
	if got != `[1,2]` {
		t.Fatalf("call list: %q", got)
	}

	got, err = callStringify(runtime.MapFrom(
		runtime.MapPair{Key: runtime.Symbol("a"), Value: runtime.Int64(1)},
	))
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"a":1}` {
		t.Fatalf("map: %q", got)
	}

	got, err = callStringify(runtime.EmptyMap())
	if err != nil {
		t.Fatal(err)
	}
	if got != `{}` {
		t.Fatalf("empty map: %q", got)
	}
}

func TestStringifyRejects(t *testing.T) {
	rejects := []runtime.Value{
		runtime.Symbol("foo"),
		runtime.Float(math.NaN()),
		runtime.Float(math.Inf(1)),
		runtime.Float(math.Inf(-1)),
		runtime.Native(0),
	}
	for _, v := range rejects {
		_, err := callStringify(v)
		if err == nil {
			t.Errorf("stringify %v (%v): want error", v, v.Kind())
		} else if !strings.Contains(err.Error(), "json:") {
			t.Errorf("stringify error missing json: prefix: %v", err)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	in := `{"n":1,"s":"x","a":[true,null,1.5]}`
	v, err := callParse(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := callStringify(v)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := callParse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(v2) {
		t.Fatalf("round-trip: %v vs %v (json %q)", v, v2, out)
	}
}

func TestRegisterSmoke(t *testing.T) {
	rt := writ.New()
	_, err := rt.Eval(strings.NewReader(`(import "json")`))
	if err == nil {
		t.Fatal("New without Register: import should fail")
	}

	rt2 := writ.New()
	stdjson.Register(rt2)
	v, err := rt2.Eval(strings.NewReader(`
(let [j: (import "json")]
  ((map-get j 'stringify) ((map-get j 'parse) "{\"a\": 1}")))
`))
	if err != nil {
		t.Fatal(err)
	}
	if v.Kind() != runtime.KindString || v.Text() != `{"a":1}` {
		t.Fatalf("registered round-trip: %v", v)
	}
}

func callParse(s string) (runtime.Value, error) {
	pkg := stdjson.Package()
	return pkg.Funcs["parse"]([]runtime.Value{runtime.String(s)})
}

func callStringify(v runtime.Value) (string, error) {
	pkg := stdjson.Package()
	out, err := pkg.Funcs["stringify"]([]runtime.Value{v})
	if err != nil {
		return "", err
	}
	return out.Text(), nil
}
