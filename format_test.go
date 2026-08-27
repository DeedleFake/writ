package writ

import "testing"

func stripComments(v Value) Value {
	switch v.k {
	case KindComment:
		return Nil
	case KindQuote, KindUnquote, KindSplice:
		return v.setInner(stripComments(v.innerVal()))
	case KindList:
		var xs []Value
		for _, x := range v.xs {
			if x.k == KindComment {
				continue
			}
			xs = append(xs, stripComments(x))
		}
		v.xs = xs
		v.cmt = ""
		return v
	case KindMap:
		if v.mp == nil {
			return v
		}
		m := newMap()
		for i, k := range v.mp.keys {
			m.put(k, stripComments(v.mp.vals[i]))
		}
		v.mp = m
		v.cmt = ""
		return v
	default:
		v.cmt = ""
		return v
	}
}

func formEq(a, b Value) bool {
	a, b = stripComments(a), stripComments(b)
	if a.k != b.k || a.vec != b.vec {
		return false
	}
	switch a.k {
	case KindList:
		if len(a.xs) != len(b.xs) {
			return false
		}
		for i := range a.xs {
			if !formEq(a.xs[i], b.xs[i]) {
				return false
			}
		}
		return true
	case KindMap:
		if a.mp.len() != b.mp.len() {
			return false
		}
		for i, k := range a.mp.keys {
			ov, ok := b.mp.get(k)
			if !ok || !formEq(a.mp.vals[i], ov) {
				return false
			}
		}
		return true
	case KindQuote, KindUnquote, KindSplice:
		return formEq(a.innerVal(), b.innerVal())
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
		"(pipe xs\n  (map f)\n  (reduce 0 g))\n",
		"(let [x: 1]\n  x)\n",
		"(on ev (k:)\n  k)\n",
		"1.0\n",
		"-3.5\n",
		"\"a\\nb\"\n",
		"`foo bar`\n",
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
