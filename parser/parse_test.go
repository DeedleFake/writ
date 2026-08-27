package parser

import (
	"strings"
	"testing"

	"deedles.dev/writ/runtime"
)

func parse1(t *testing.T, src string) runtime.Value {
	t.Helper()
	forms, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	if len(forms) != 1 {
		t.Fatalf("parse %q: got %d forms", src, len(forms))
	}
	return forms[0]
}

func TestParseNumbers(t *testing.T) {
	n := parse1(t, "1")
	if n.Kind() != runtime.KindInt || n.BigInt().Int64() != 1 {
		t.Fatalf("1: %+v", n)
	}
	f := parse1(t, "1.0")
	if f.Kind() != runtime.KindFloat || f.Float64() != 1.0 {
		t.Fatalf("1.0: %+v", f)
	}
	neg := parse1(t, "-3.5")
	if neg.Kind() != runtime.KindFloat || neg.Float64() != -3.5 {
		t.Fatalf("-3.5: %+v", neg)
	}
	big := parse1(t, "999999999999999999999")
	if big.Kind() != runtime.KindInt || big.BigInt().String() != "999999999999999999999" {
		t.Fatalf("big int: %s", big)
	}
}

func TestParseListAndMap(t *testing.T) {
	l := parse1(t, "[a b c]")
	if !l.IsVec() || len(l.Items()) != 3 {
		t.Fatalf("list: %+v", l)
	}
	m := parse1(t, "[k: 1]")
	if m.Kind() != runtime.KindMap {
		t.Fatalf("map kind %v", m.Kind())
	}
	v, ok := m.MapGet("k")
	if !ok || v.Kind() != runtime.KindInt {
		t.Fatalf("map k: %v %v", ok, v)
	}
	empty := parse1(t, "[:]")
	if empty.Kind() != runtime.KindMap || len(empty.Pairs()) != 0 {
		t.Fatalf("empty map: %+v", empty)
	}
}

func TestParseMapMixError(t *testing.T) {
	_, err := Parse("[a k: 1]")
	if err == nil || !strings.Contains(err.Error(), "mix") {
		t.Fatalf("expected mix error, got %v", err)
	}
}

func TestParseQuoteUnquoteSplice(t *testing.T) {
	q := parse1(t, "'x")
	if q.Kind() != runtime.KindQuote || q.Inner().Name() != "x" {
		t.Fatalf("quote: %+v", q)
	}
	u := parse1(t, ",x")
	if u.Kind() != runtime.KindUnquote {
		t.Fatalf("unquote: %+v", u)
	}
	s := parse1(t, "@xs")
	if s.Kind() != runtime.KindSplice {
		t.Fatalf("splice: %+v", s)
	}
	call := parse1(t, "(+ 1 @[2 3])")
	if call.Kind() != runtime.KindList || call.IsVec() {
		t.Fatalf("call: %+v", call)
	}
}

func TestParseKeywordsAndComments(t *testing.T) {
	forms, err := Parse("; head\n(f k: 1) ; trail\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 2 || forms[0].Kind() != runtime.KindComment {
		t.Fatalf("forms: %+v", forms)
	}
	if forms[1].TrailingComment() == "" {
		t.Fatalf("missing trailing comment")
	}
	kw := parse1(t, "(f a: 1 b: 2)")
	xs := kw.Items()
	if len(xs) != 5 || !xs[1].IsKey() {
		t.Fatalf("keywords: %+v", xs)
	}
}

func TestParseTickSymbol(t *testing.T) {
	s := parse1(t, "`foo bar`")
	if s.Kind() != runtime.KindSymbol || s.Name() != "foo bar" {
		t.Fatalf("tick: %+v", s)
	}
}

func TestParseStrings(t *testing.T) {
	v := parse1(t, `"hi"`)
	if v.Kind() != runtime.KindString || v.Text() != "hi" {
		t.Fatalf("hi: %v", v)
	}
	v = parse1(t, `"a\nb"`)
	if v.Kind() != runtime.KindString || v.Text() != "a\nb" {
		t.Fatalf("escape: %q", v.Text())
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		src, sub string
	}{
		{"(", "missing"},
		{")", "unexpected"},
		{"[", "missing"},
		{"]", "unexpected"},
		{"\"abc", "unterminated"},
		{"`abc", "unterminated"},
		{"[k:]", "map key needs a value"},
	}
	for _, c := range cases {
		_, err := Parse(c.src)
		if err == nil || !strings.Contains(err.Error(), c.sub) {
			t.Errorf("%q: got %v want %s", c.src, err, c.sub)
		}
	}
}

func TestIncomplete(t *testing.T) {
	if Incomplete(nil) {
		t.Fatal("nil")
	}
	if !Incomplete(runtime.ErrorIncomplete(0, 1, "xyz")) {
		t.Fatal("flag")
	}
	if Incomplete(runtime.ErrorAt(0, 1, "missing )")) {
		t.Fatal("message text is not enough")
	}
	for _, src := range []string{"(", "[", `"abc`, "`abc", "'", ",", "@", "(+ 1", "[k:", `'(`} {
		_, err := Parse(src)
		if !Incomplete(err) {
			t.Errorf("%q: want incomplete, got %v", src, err)
		}
	}
	for _, src := range []string{")", "]", "(+ 1 2)", "[k:]", "[a k: 1]", "", "; comment"} {
		_, err := Parse(src)
		if Incomplete(err) {
			t.Errorf("%q: unexpected incomplete %v", src, err)
		}
	}
}

func TestParseTrueFalseNil(t *testing.T) {
	for _, name := range []string{"true", "false", "nil"} {
		v := parse1(t, name)
		if v.Kind() != runtime.KindSymbol || v.Name() != name {
			t.Fatalf("%s: %+v", name, v)
		}
	}
}
