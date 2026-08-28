package parser

import (
	"testing"

	"deedles.dev/writ/runtime"
)

func stripComments(v runtime.Value) runtime.Value {
	switch v.Kind() {
	case runtime.KindComment:
		return runtime.Nil
	case runtime.KindQuote:
		return runtime.Quote(stripComments(v.Inner()))
	case runtime.KindUnquote:
		return runtime.Unquote(stripComments(v.Inner()))
	case runtime.KindSplice:
		return runtime.Splice(stripComments(v.Inner()))
	case runtime.KindList:
		var xs []runtime.Value
		for _, x := range v.Items() {
			if x.Kind() == runtime.KindComment {
				continue
			}
			xs = append(xs, stripComments(x))
		}
		if v.IsVec() {
			return runtime.List(xs...)
		}
		return runtime.CallList(xs...)
	case runtime.KindMap:
		var pairs []runtime.MapPair
		for _, p := range v.Pairs() {
			pairs = append(pairs, runtime.MapPair{Key: p.Key, Value: stripComments(p.Value)})
		}
		return runtime.MapFrom(pairs...)
	default:
		return v.WithComment("")
	}
}

func formEq(a, b runtime.Value) bool {
	a, b = stripComments(a), stripComments(b)
	if a.Kind() != b.Kind() || a.IsVec() != b.IsVec() {
		return false
	}
	switch a.Kind() {
	case runtime.KindList:
		as, bs := a.Items(), b.Items()
		if len(as) != len(bs) {
			return false
		}
		for i := range as {
			if !formEq(as[i], bs[i]) {
				return false
			}
		}
		return true
	case runtime.KindMap:
		ap, bp := a.Pairs(), b.Pairs()
		if len(ap) != len(bp) {
			return false
		}
		for i, pair := range ap {
			ov, ok := b.MapGet(pair.Key.Name())
			if !ok || !formEq(pair.Value, ov) {
				return false
			}
			_ = bp[i]
		}
		return true
	case runtime.KindQuote, runtime.KindUnquote, runtime.KindSplice:
		return formEq(a.Inner(), b.Inner())
	default:
		return a.Equal(b)
	}
}

func TestFormatRoundTrip(t *testing.T) {
	srcs := []string{
		"(+ 1 2)\n",
		"(def (f n)\n  (+ n 1))\n",
		"[a b c]\n",
		"[k: 1 m: 2]\n",
		"[:]\n",
		"'(+ 1 2)\n",
		"(if x\n  1\nelse\n  2)\n",
		"(fn (n)\n  (+ n 1))\n",
		"(pipe xs\n  (list-map f)\n  (list-reduce 0 g))\n",
		"(let [x: 1]\n  x)\n",
		"(on ev (k:)\n  k)\n",
		"1.0\n",
		"-3.5\n",
		"\"a\\nb\"\n",
		"`foo bar`\n",
		"`io.write`\n",
		"io.write\n",
		"a.b.c\n",
		"(if x\n  1\nelse if y\n  2\nelse\n  3)\n",
	}
	for _, src := range srcs {
		out, err := Format(src)
		if err != nil {
			t.Fatalf("format %q: %v", src, err)
		}
		a, err := Parse(src)
		if err != nil {
			t.Fatal(err)
		}
		b, err := Parse(out)
		if err != nil {
			t.Fatalf("reparse %q -> %q: %v", src, out, err)
		}
		if len(a) != len(b) {
			t.Fatalf("len %q vs %q", src, out)
		}
		for i := range a {
			if !formEq(a[i], b[i]) {
				t.Fatalf("round-trip\n src: %q\n out: %q", src, out)
			}
		}
	}
}

func TestFormatEmpty(t *testing.T) {
	out, err := Format("")
	if err != nil || out != "" {
		t.Fatalf("%q %v", out, err)
	}
}

func TestFormatParseError(t *testing.T) {
	if _, err := Format("("); err == nil {
		t.Fatal("expected error")
	}
}

func TestFormatDottedAccess(t *testing.T) {
	out, err := Format("(. io write)\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "io.write\n" {
		t.Fatalf("got %q", out)
	}
	out, err = Format("(. (. io fs) write)\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "io.fs.write\n" {
		t.Fatalf("nested got %q", out)
	}
	out, err = Format("(. (f) write)\n")
	if err != nil {
		t.Fatal(err)
	}
	if out != "(. (f) write)\n" {
		t.Fatalf("computed got %q", out)
	}
}
