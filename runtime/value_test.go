package runtime

import "testing"

func TestSymbolIntern(t *testing.T) {
	a := Symbol("hits")
	b := Symbol("hits")
	if a.sym != b.sym {
		t.Fatal("Symbol does not intern")
	}
	if !a.Equal(b) {
		t.Fatal("interned symbols should compare equal")
	}
	if a.sym != internSym("hits").sym {
		t.Fatal("internSym and Symbol disagree")
	}
}

func TestTrueFalseNilHandles(t *testing.T) {
	if !Symbol("true").IsTrue() || Symbol("true").sym != True.sym {
		t.Fatal("true")
	}
	if !Symbol("false").IsFalse() || Symbol("false").sym != False.sym {
		t.Fatal("false")
	}
	if !Symbol("nil").IsNil() || Symbol("nil").sym != Nil.sym {
		t.Fatal("nil")
	}
	if True.Equal(False) || True.Equal(Nil) {
		t.Fatal("distinct interned literals")
	}
}

func TestWithSpanDoesNotChangeIdentity(t *testing.T) {
	a := Symbol("hits").WithSpan(0, 4)
	b := Symbol("hits").WithSpan(10, 14)
	sa, okA := a.Span()
	sb, okB := b.Span()
	if !okA || !okB || sa == sb {
		t.Fatalf("spans %+v %+v", sa, sb)
	}
	if a.sym != b.sym || !a.Equal(b) {
		t.Fatal("span must not affect interned identity")
	}
}
