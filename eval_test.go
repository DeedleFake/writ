package writ

import (
	"math/big"
	"strings"
	"testing"
	"time"
)

func evals(t *testing.T, src string) Value {
	t.Helper()
	rt := New()
	v, err := rt.Eval(src)
	if err != nil {
		t.Fatalf("eval %s: %v", src, err)
	}
	return v
}

func evalErr(t *testing.T, src string) error {
	t.Helper()
	_, err := New().Eval(src)
	if err == nil {
		t.Fatalf("expected error for %s", src)
	}
	return err
}

func TestEvalArithIntFloat(t *testing.T) {
	if v := evals(t, "(+ 1 2)"); v.Kind() != KindInt || v.BigInt().Int64() != 3 {
		t.Fatalf("+ int: %v", v)
	}
	if v := evals(t, "(+ 1 2.0)"); v.Kind() != KindFloat || v.Float64() != 3 {
		t.Fatalf("+ mix: %v", v)
	}
	if v := evals(t, "(*)"); !v.Equal(Int64(1)) {
		t.Fatalf("* empty: %v", v)
	}
	if v := evals(t, "(+)"); !v.Equal(Int64(0)) {
		t.Fatalf("+ empty: %v", v)
	}
	if v := evals(t, "(/ 6 2)"); v.Kind() != KindInt || v.BigInt().Int64() != 3 {
		t.Fatalf("/ even: %v", v)
	}
	if v := evals(t, "(/ 5 2)"); v.Kind() != KindFloat || v.Float64() != 2.5 {
		t.Fatalf("/ uneven: %v", v)
	}
	if err := evalErr(t, "(/ 1 0)"); !strings.Contains(err.Error(), "zero") {
		t.Fatalf("/0: %v", err)
	}
	if err := evalErr(t, "(/ 1.0 0)"); !strings.Contains(err.Error(), "zero") {
		t.Fatalf("/ float 0: %v", err)
	}
	if err := evalErr(t, "(/ 1 0.0)"); !strings.Contains(err.Error(), "zero") {
		t.Fatalf("/ 0.0: %v", err)
	}
	if err := evalErr(t, "(mod 1 0)"); !strings.Contains(err.Error(), "zero") {
		t.Fatalf("mod 0: %v", err)
	}
	if v := evals(t, "(mod 7 3)"); !v.Equal(Int64(1)) {
		t.Fatalf("mod: %v", v)
	}
	if v := evals(t, "(* 10 20 30)"); !v.Equal(Int64(6000)) {
		t.Fatalf("*: %v", v)
	}
	bigv := evals(t, "(* 1000000000000 1000000000000)")
	want := new(big.Int)
	want.SetString("1000000000000000000000000", 10)
	if bigv.BigInt().Cmp(want) != 0 {
		t.Fatalf("big *: %s", bigv)
	}
}

func TestEvalCompareAndPred(t *testing.T) {
	if !evals(t, "(= 1 1.0)").IsTrue() {
		t.Fatal("1 = 1.0")
	}
	if !evals(t, "(< 1 2.5)").IsTrue() {
		t.Fatal("< mix")
	}
	if !evals(t, "(int? 1)").IsTrue() || evals(t, "(int? 1.0)").IsTrue() {
		t.Fatal("int?")
	}
	if !evals(t, "(float? 1.0)").IsTrue() || evals(t, "(float? 1)").IsTrue() {
		t.Fatal("float?")
	}
	if !evals(t, "(num? 1)").IsTrue() || !evals(t, "(num? 1.5)").IsTrue() {
		t.Fatal("num?")
	}
	if !evals(t, "(symbol? true)").IsTrue() || !evals(t, "(symbol? nil)").IsTrue() {
		t.Fatal("symbol? true/nil")
	}
	if evals(t, "(bool? nil)").IsTrue() {
		t.Fatal("nil is not bool")
	}
	if !evals(t, "(nil? nil)").IsTrue() {
		t.Fatal("nil?")
	}
}

func TestEvalListsMaps(t *testing.T) {
	v := evals(t, "(first [1 2 3])")
	if !v.Equal(Int64(1)) {
		t.Fatalf("first: %v", v)
	}
	v = evals(t, `(get [a: 1 b: "x"] "a")`)
	if !v.Equal(Int64(1)) {
		t.Fatalf("get: %v", v)
	}
	v = evals(t, `(get [a: [b: 2]] ["a" "b"])`)
	if !v.Equal(Int64(2)) {
		t.Fatalf("get path: %v", v)
	}
	v = evals(t, `(set [:] "k" 1)`)
	got, _ := v.MapGet("k")
	if !got.Equal(Int64(1)) {
		t.Fatalf("set: %v", v)
	}
	v = evals(t, `(merge [a: 1] [b: 2 a: 3])`)
	a, _ := v.MapGet("a")
	if !a.Equal(Int64(3)) {
		t.Fatalf("merge: %v", v)
	}
	v = evals(t, `(from-pairs [["x" 1] ["y" 2]])`)
	if n := len(v.Pairs()); n != 2 {
		t.Fatalf("from-pairs: %v", v)
	}
}

func TestEvalNthSymbols(t *testing.T) {
	v := evals(t, `(nth ["a" "b" "c"] 2)`)
	if v.Text() != "c" {
		t.Fatalf("nth: %v", v)
	}
}

func TestEvalFnShortLong(t *testing.T) {
	v := evals(t, "((fn (+ #1 1)) 4)")
	if !v.Equal(Int64(5)) {
		t.Fatalf("short fn: %v", v)
	}
	v = evals(t, "((fn + #1 1) 4)")
	if !v.Equal(Int64(5)) {
		t.Fatalf("short fn call wrap: %v", v)
	}
	v = evals(t, "((fn (nil) 0 fn (n) (+ n 1)) 9)")
	if !v.Equal(Int64(10)) {
		t.Fatalf("long fn: %v", v)
	}
	v = evals(t, "((fn (nil) 0 fn (n) (+ n 1)) nil)")
	if !v.Equal(Int64(0)) {
		t.Fatalf("clause lit: %v", v)
	}
}

func TestEvalDefClauses(t *testing.T) {
	src := `
(def (f 0) 1)
(def (f n) (* n (f (- n 1))))
(f 5)
`
	v := evals(t, src)
	if !v.Equal(Int64(120)) {
		t.Fatalf("fact: %v", v)
	}
}

func TestEvalLetIfAndOr(t *testing.T) {
	v := evals(t, `(let [x: 2 y: 3] (+ x y))`)
	if !v.Equal(Int64(5)) {
		t.Fatalf("let: %v", v)
	}
	v = evals(t, `(if nil 1 2)`)
	if !v.IsNil() {
		t.Fatalf("if nil: %v", v)
	}
	v = evals(t, `(if nil 1 else 2)`)
	if !v.Equal(Int64(2)) {
		t.Fatalf("if else: %v", v)
	}
	v = evals(t, `(if not false 9)`)
	if !v.Equal(Int64(9)) {
		t.Fatalf("if not: %v", v)
	}
	v = evals(t, `(and 1 false 3)`)
	if !v.IsFalse() {
		t.Fatalf("and: %v", v)
	}
	v = evals(t, `(or nil false 7)`)
	if !v.Equal(Int64(7)) {
		t.Fatalf("or: %v", v)
	}
}

func TestEvalPipe(t *testing.T) {
	v := evals(t, `
(pipe [1 2 3]
  (map (fn * #1 2))
  (reduce 0 (fn + #1 #2)))
`)
	if !v.Equal(Int64(12)) {
		t.Fatalf("pipe: %v", v)
	}
}

func TestEvalQuoteEval(t *testing.T) {
	v := evals(t, "(eval '(+ 1 2))")
	if !v.Equal(Int64(3)) {
		t.Fatalf("eval quote: %v", v)
	}
	v = evals(t, "(eval [1 2])")
	if v.Kind() != KindList || !v.IsVec() {
		t.Fatalf("eval vec: %v", v)
	}
	v = evals(t, `'(a ,(+ 1 2))`)
	if v.Kind() != KindList || len(v.Items()) != 2 || !v.Items()[1].Equal(Int64(3)) {
		t.Fatalf("unquote: %v", v)
	}
	v = evals(t, `(+ 1 @[2 3])`)
	if !v.Equal(Int64(6)) {
		t.Fatalf("splice: %v", v)
	}
	evalErr(t, ",x")
}

func TestEvalMacros(t *testing.T) {
	v := evals(t, `
(defm (unless test @body)
  (cons 'if (cons 'not (cons test body))))
(unless false 42)
`)
	if !v.Equal(Int64(42)) {
		t.Fatalf("unless: %v", v)
	}
}

func TestEvalMacroHygiene(t *testing.T) {
	v := evals(t, `
(defm (with-x body)
  '(let [x: 1] ,body))
(let [x: 9]
  (with-x x))
`)
	if !v.Equal(Int64(9)) {
		t.Fatalf("hygiene want 9 (call-site x), got %v", v)
	}
}

func TestEvalLetBang(t *testing.T) {
	v := evals(t, `
(defm (bind-x body)
  '(let! [x: 1] ,body))
(bind-x x)
`)
	if !v.Equal(Int64(1)) {
		t.Fatalf("let!: %v", v)
	}
}

func TestEvalProps(t *testing.T) {
	rt := New()
	if _, err := rt.Eval(`(set-prop "hits" 0)`); err != nil {
		t.Fatal(err)
	}
	v, err := rt.Eval(`(+ (get-prop "hits") 2)`)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(Int64(2)) {
		t.Fatalf("get-prop: %v", v)
	}
	if _, err := rt.Eval(`(update-prop "hits" (fn + #1 3))`); err != nil {
		t.Fatal(err)
	}
	if !rt.GetProp("hits").Equal(Int64(3)) {
		t.Fatalf("update-prop store: %v", rt.GetProp("hits"))
	}
	if _, err := rt.Eval(`(set-prop ["a" "b"] 1)`); err != nil {
		t.Fatal(err)
	}
	if !rt.GetProp("a", "b").Equal(Int64(1)) {
		t.Fatalf("nested: %v", rt.GetProp("a", "b"))
	}
}

func TestEvalKeywordFn(t *testing.T) {
	v := evals(t, `
(def (greet name: n)
  (str "hi " n))
(greet name: "ada")
`)
	if v.Text() != "hi ada" {
		t.Fatalf("kw: %v", v)
	}
	v = evals(t, `
(def (greet name: n)
  name)
(greet name: "ada")
`)
	if v.Text() != "ada" {
		t.Fatalf("kw key name: %v", v)
	}
}

func TestEvalKeywordClauses(t *testing.T) {
	v := evals(t, `
(def (f a:) 1)
(def (f a: b:) 2)
(f a: 9)
`)
	if !v.Equal(Int64(1)) {
		t.Fatalf("kw one key: %v", v)
	}
	v = evals(t, `
(def (f a:) 1)
(def (f a: b:) 2)
(f a: 9 b: 8)
`)
	if !v.Equal(Int64(2)) {
		t.Fatalf("kw two keys: %v", v)
	}
}

func TestNestedDefRejected(t *testing.T) {
	err := evalErr(t, `(let [x: 1] (def (f) x) (f))`)
	if !strings.Contains(err.Error(), "top") {
		t.Fatalf("nested def: %v", err)
	}
}

func TestMacroMapKeysNotRenamed(t *testing.T) {
	v := evals(t, `
(defm (m)
  '(let [name: "ada"] [name: name]))
(m)
`)
	got, ok := v.MapGet("name")
	if !ok || got.Text() != "ada" {
		t.Fatalf("map key renamed: %v", v)
	}
}

func TestElseIf(t *testing.T) {
	v := evals(t, `(if false 1 else if true 2 else 3)`)
	if !v.Equal(Int64(2)) {
		t.Fatalf("else if taken: %v", v)
	}
	v = evals(t, `(if false 1 else if false 2 else 3)`)
	if !v.Equal(Int64(3)) {
		t.Fatalf("else if skipped: %v", v)
	}
	v = evals(t, `(if true 1 else if true 2 else 3)`)
	if !v.Equal(Int64(1)) {
		t.Fatalf("first branch: %v", v)
	}
	v = evals(t, `(if false 1 else if not false 4 else 5)`)
	if !v.Equal(Int64(4)) {
		t.Fatalf("else if not: %v", v)
	}
}

func TestEvalKeywordExtraArgs(t *testing.T) {
	err := evalErr(t, `
(def (f a:) 1)
(f a: 1 b: 2)
`)
	if !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("extra kw: %v", err)
	}
}

func TestEvalUnreachableClause(t *testing.T) {
	err := evalErr(t, `
(def (f n) 1)
(def (f n) 2)
`)
	if !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("got %v", err)
	}
}

func TestAfterScheduler(t *testing.T) {
	var pending []func()
	var delays []time.Duration
	rt := New(WithScheduler(func(d time.Duration, fn func()) {
		delays = append(delays, d)
		pending = append(pending, fn)
	}))
	if _, err := rt.Eval(`(after 1 (set-prop "done" true))`); err != nil {
		t.Fatal(err)
	}
	if len(delays) != 1 || delays[0] != time.Second {
		t.Fatalf("delay: %v", delays)
	}
	if !rt.GetProp("done").IsNil() {
		t.Fatal("after should not have run yet")
	}
	for _, fn := range pending {
		fn()
	}
	if !rt.GetProp("done").IsTrue() {
		t.Fatalf("after ran: %v", rt.GetProp("done"))
	}
	if err := evalErr(t, `(after "no" 1)`); !strings.Contains(err.Error(), "number") {
		t.Fatalf("after non-number: %v", err)
	}
}

func TestMapFilterReduce(t *testing.T) {
	v := evals(t, `(map [1 2 3] (fn * #1 10))`)
	if len(v.Items()) != 3 || !v.Items()[1].Equal(Int64(20)) {
		t.Fatalf("map: %v", v)
	}
	v = evals(t, `(filter [1 2 3 4] (fn = (mod #1 2) 0))`)
	if len(v.Items()) != 2 {
		t.Fatalf("filter: %v", v)
	}
	v = evals(t, `(reduce [1 2 3] 0 (fn + #1 #2))`)
	if !v.Equal(Int64(6)) {
		t.Fatalf("reduce: %v", v)
	}
	v = evals(t, `(map [a: 1] (fn first #1))`)
	if len(v.Items()) != 1 || v.Items()[0].Text() != "a" {
		t.Fatalf("map map: %v", v)
	}
}
