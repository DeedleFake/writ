package syntax

import (
	"strings"
	"testing"
	"unsafe"
)

func TestSymbolIntern(t *testing.T) {
	a := Symbol("hits")
	b := Symbol("hits")
	if a.h != b.h || !a.Equal(b) {
		t.Fatal("Symbol does not intern")
	}
}

func TestTrueFalseNil(t *testing.T) {
	if !Symbol("true").IsTrue() || Symbol("true").h != True.h {
		t.Fatal("true")
	}
	if !Symbol("false").IsFalse() || Symbol("false").h != False.h {
		t.Fatal("false")
	}
	if !Symbol("nil").IsNil() || Symbol("nil").h != Nil.h {
		t.Fatal("nil")
	}
}

func TestFormSize(t *testing.T) {
	n := unsafe.Sizeof(Form{})
	if n > 64 {
		t.Fatalf("sizeof(Form) = %d, want <= 64", n)
	}
}

func TestQuoteInner(t *testing.T) {
	q := Quote(Int64(1))
	if !q.Inner().Equal(Int64(1)) {
		t.Fatal("Inner on quote")
	}
	if !q.SetInner(Int64(2)).Inner().Equal(Int64(2)) {
		t.Fatal("SetInner on quote")
	}
	if q.Kind() != KindQuote {
		t.Fatal("kind")
	}
	u := Unquote(Symbol("x"))
	if u.Kind() != KindUnquote || u.Inner().Name() != "x" {
		t.Fatal("unquote")
	}
	s := Splice(List(Int64(1)))
	if s.Kind() != KindSplice || !s.Inner().IsVec() {
		t.Fatal("splice")
	}
}

func TestCommentAndPrint(t *testing.T) {
	c := Comment("; hi")
	if c.Kind() != KindComment || c.CommentText() != "; hi" {
		t.Fatal("comment")
	}
	if Print(Quote(Symbol("x"))) != "'x" {
		t.Fatalf("print quote: %q", Print(Quote(Symbol("x"))))
	}
	if !strings.HasPrefix(Print(Unquote(Symbol("x"))), ",") {
		t.Fatal("print unquote")
	}
}
