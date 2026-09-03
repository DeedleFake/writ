package runtime

import (
	"strings"
	"testing"
	"unsafe"
)

func TestSymbolIntern(t *testing.T) {
	a := Symbol("hits")
	b := Symbol("hits")
	if a.h != b.h {
		t.Fatal("Symbol does not intern")
	}
	if !a.Equal(b) {
		t.Fatal("interned symbols should compare equal")
	}
	if a.h != internSym("hits").h {
		t.Fatal("internSym and Symbol disagree")
	}
}

func TestTrueFalseNilHandles(t *testing.T) {
	if !Symbol("true").IsTrue() || Symbol("true").h != True.h {
		t.Fatal("true")
	}
	if !Symbol("false").IsFalse() || Symbol("false").h != False.h {
		t.Fatal("false")
	}
	if !Symbol("nil").IsNil() || Symbol("nil").h != Nil.h {
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
	if a.h != b.h || !a.Equal(b) {
		t.Fatal("span must not affect interned identity")
	}
}

func TestValueSize(t *testing.T) {
	n := unsafe.Sizeof(Value{})
	if n > 64 {
		t.Fatalf("sizeof(Value) = %d, want <= 64 (was 168)", n)
	}
}

func TestSmallIntUnboxed(t *testing.T) {
	v := Int64(42)
	if v.p != nil || v.src != nil {
		t.Fatalf("small int boxed: p=%#v src=%#v", v.p, v.src)
	}
	if v.n != 42 || v.k != KindInt {
		t.Fatalf("small int: k=%v n=%d", v.k, v.n)
	}
}

type nativeEqBox struct{ n int }

func TestNativeEqualPrintAs(t *testing.T) {
	a := Native(&nativeEqBox{n: 1})
	b := Native(&nativeEqBox{n: 1})
	p, ok := a.Native()
	if !ok {
		t.Fatal("Native()")
	}
	if p.(*nativeEqBox) == b.p.(*nativeEqBox) {
		t.Fatal("distinct pointers")
	}
	if a.Equal(b) {
		t.Fatal("distinct pointers should not compare equal")
	}
	same := &nativeEqBox{n: 2}
	if !Native(same).Equal(Native(same)) {
		t.Fatal("same pointer should compare equal")
	}
	if !Native(3).Equal(Native(3)) {
		t.Fatal("comparable natives")
	}
	if Native(3).Equal(Int64(3)) {
		t.Fatal("native vs writ")
	}
	fn := Value{k: KindFn, p: &fnVal{}}
	if fn.Equal(fn) {
		t.Fatal("functions never equal")
	}
	if Native(func() {}).Equal(Native(func() {})) {
		t.Fatal("incomparable natives")
	}
	type ifaceBox struct{ V any }
	panicEq := Native(ifaceBox{[]int{1}})
	if panicEq.Equal(Native(ifaceBox{[]int{1}})) {
		t.Fatal("incomparable interface payload")
	}
	if panicEq.Equal(panicEq) {
		t.Fatal("incomparable native should not equal itself")
	}
	if !Native(ifaceBox{1}).Equal(Native(ifaceBox{1})) {
		t.Fatal("comparable interface payload")
	}
	s := Print(a)
	if !strings.Contains(s, "#<native") {
		t.Fatalf("print: %q", s)
	}
	var got *nativeEqBox
	if !a.As(&got) || got != p.(*nativeEqBox) {
		t.Fatal("As identity")
	}
	var wrong int
	if a.As(&wrong) {
		t.Fatal("As wrong type")
	}
	if a.As(nil) {
		t.Fatal("As nil dst")
	}
	if a.As(0) {
		t.Fatal("As non-pointer dst")
	}
	if _, ok := Int64(1).Native(); ok {
		t.Fatal("Native() on non-native")
	}
	if Int64(1).As(&got) {
		t.Fatal("As on non-native")
	}

	got2, ok := a.AsType[*nativeEqBox]()
	if !ok || got2 != p.(*nativeEqBox) {
		t.Fatal("AsType identity")
	}
	if _, ok := a.AsType[int](); ok {
		t.Fatal("AsType wrong type")
	}
	if _, ok := Int64(1).AsType[*nativeEqBox](); ok {
		t.Fatal("AsType on non-native")
	}
	nilNat := Native(nil)
	if got, ok := nilNat.AsType[*nativeEqBox](); !ok || got != nil {
		t.Fatal("AsType nil pointer")
	}
	if _, ok := nilNat.AsType[int](); ok {
		t.Fatal("AsType nil into non-nillable")
	}
}
