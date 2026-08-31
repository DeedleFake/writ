package writ

import (
	"deedles.dev/writ/runtime"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func rd(s string) *strings.Reader { return strings.NewReader(s) }

func evals(t *testing.T, src string) runtime.Value {
	t.Helper()
	rt := New()
	v, err := rt.Eval(rd(src))
	if err != nil {
		t.Fatalf("eval %s: %v", src, err)
	}
	return v
}

func evalErr(t *testing.T, src string) error {
	t.Helper()
	_, err := New().Eval(rd(src))
	if err == nil {
		t.Fatalf("expected error for %s", src)
	}
	return err
}

func TestEvalIOError(t *testing.T) {
	boom := errors.New("boom")
	_, err := New().Eval(errReader{err: boom})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

func TestEvalEmpty(t *testing.T) {
	for _, src := range []string{"", "  \n  "} {
		v, err := New().Eval(rd(src))
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if !v.IsNil() {
			t.Fatalf("%q: got %v want nil", src, v)
		}
	}
}

func TestEvalArithIntFloat(t *testing.T) {
	if v := evals(t, "(+ 1 2)"); v.Kind() != runtime.KindInt || v.BigInt().Int64() != 3 {
		t.Fatalf("+ int: %v", v)
	}
	if v := evals(t, "(+ 1 2.0)"); v.Kind() != runtime.KindFloat || v.Float64() != 3 {
		t.Fatalf("+ mix: %v", v)
	}
	if v := evals(t, "(*)"); !v.Equal(runtime.Int64(1)) {
		t.Fatalf("* empty: %v", v)
	}
	if v := evals(t, "(+)"); !v.Equal(runtime.Int64(0)) {
		t.Fatalf("+ empty: %v", v)
	}
	if v := evals(t, "(/ 6 2)"); v.Kind() != runtime.KindInt || v.BigInt().Int64() != 3 {
		t.Fatalf("/ even: %v", v)
	}
	if v := evals(t, "(/ 5 2)"); v.Kind() != runtime.KindFloat || v.Float64() != 2.5 {
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
	if v := evals(t, "(mod 7 3)"); !v.Equal(runtime.Int64(1)) {
		t.Fatalf("mod: %v", v)
	}
	if v := evals(t, "(* 10 20 30)"); !v.Equal(runtime.Int64(6000)) {
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
	v := evals(t, "(head [1 2 3])")
	if !v.Equal(runtime.Int64(1)) {
		t.Fatalf("head: %v", v)
	}
	v = evals(t, `(map-get [a: 1 b: "x"] 'a)`)
	if !v.Equal(runtime.Int64(1)) {
		t.Fatalf("map-get: %v", v)
	}
	v = evals(t, `(map-get [a: [b: 2]] ['a 'b])`)
	if !v.Equal(runtime.Int64(2)) {
		t.Fatalf("map-get path: %v", v)
	}
	v = evals(t, `(map-set [:] 'k 1)`)
	got, _ := v.MapGet("k")
	if !got.Equal(runtime.Int64(1)) {
		t.Fatalf("map-set: %v", v)
	}
	v = evals(t, `(map-merge [a: 1] [b: 2 a: 3])`)
	a, _ := v.MapGet("a")
	if !a.Equal(runtime.Int64(3)) {
		t.Fatalf("map-merge: %v", v)
	}
	v = evals(t, `(list-to-map [['x 1] ['y 2]])`)
	if n := len(v.Pairs()); n != 2 {
		t.Fatalf("list-to-map: %v", v)
	}
	v = evals(t, `(map-keys [a: 1])`)
	if len(v.Items()) != 1 || !v.Items()[0].Equal(runtime.Symbol("a")) {
		t.Fatalf("map-keys: %v", v)
	}
	err := evalErr(t, `(map-get [a: 1] "a")`)
	if !strings.Contains(err.Error(), "symbol") {
		t.Fatalf("string key: %v", err)
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
	if !v.Equal(runtime.Int64(5)) {
		t.Fatalf("short fn: %v", v)
	}
	v = evals(t, "((fn + #1 1) 4)")
	if !v.Equal(runtime.Int64(5)) {
		t.Fatalf("short fn call wrap: %v", v)
	}
	v = evals(t, "((fn (nil) 0 fn (n) (+ n 1)) 9)")
	if !v.Equal(runtime.Int64(10)) {
		t.Fatalf("long fn: %v", v)
	}
	v = evals(t, "((fn (nil) 0 fn (n) (+ n 1)) nil)")
	if !v.Equal(runtime.Int64(0)) {
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
	if !v.Equal(runtime.Int64(120)) {
		t.Fatalf("fact: %v", v)
	}
}

func TestEvalLetIfAndOr(t *testing.T) {
	v := evals(t, `(let [x: 2 y: 3] (+ x y))`)
	if !v.Equal(runtime.Int64(5)) {
		t.Fatalf("let: %v", v)
	}
	v = evals(t, `(if nil 1 2)`)
	if !v.IsNil() {
		t.Fatalf("if nil: %v", v)
	}
	v = evals(t, `(if nil 1 else 2)`)
	if !v.Equal(runtime.Int64(2)) {
		t.Fatalf("if else: %v", v)
	}
	v = evals(t, `(if not false 9)`)
	if !v.Equal(runtime.Int64(9)) {
		t.Fatalf("if not: %v", v)
	}
	v = evals(t, `(and 1 false 3)`)
	if !v.IsFalse() {
		t.Fatalf("and: %v", v)
	}
	v = evals(t, `(or nil false 7)`)
	if !v.Equal(runtime.Int64(7)) {
		t.Fatalf("or: %v", v)
	}
}

func TestEvalPipe(t *testing.T) {
	v := evals(t, `
(pipe [1 2 3]
  (list-map (fn * #1 2))
  (list-reduce 0 (fn + #1 #2)))
`)
	if !v.Equal(runtime.Int64(12)) {
		t.Fatalf("pipe: %v", v)
	}
}

func TestEvalInternedSymbols(t *testing.T) {
	v := evals(t, "(= 'hits 'hits)")
	if !v.IsTrue() {
		t.Fatalf("quoted symbols: %v", v)
	}
	v = evals(t, `(= (symbol "hits") 'hits)`)
	if !v.IsTrue() {
		t.Fatalf("symbol constructor: %v", v)
	}
}

func TestEvalQuoteEval(t *testing.T) {
	v := evals(t, "(eval '(+ 1 2))")
	if !v.Equal(runtime.Int64(3)) {
		t.Fatalf("eval quote: %v", v)
	}
	v = evals(t, "(eval [1 2])")
	if v.Kind() != runtime.KindList || !v.IsVec() {
		t.Fatalf("eval vec: %v", v)
	}
	v = evals(t, `'(a ,(+ 1 2))`)
	if v.Kind() != runtime.KindList || len(v.Items()) != 2 || !v.Items()[1].Equal(runtime.Int64(3)) {
		t.Fatalf("unquote: %v", v)
	}
	v = evals(t, `(+ 1 @[2 3])`)
	if !v.Equal(runtime.Int64(6)) {
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
	if !v.Equal(runtime.Int64(42)) {
		t.Fatalf("unless: %v", v)
	}
}

func TestEvalImportedMacros(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "macs.writ"), []byte(`
(def (one) 1)
(defm (unless test @body)
  (cons 'if (cons 'not (cons test body))))
(defm (announce @rest)
  '(prop-set 'hit true)
  @rest)
(defm (from-here x)
  (cons '+ (cons (one) (cons x '()))))
`), 0o644); err != nil {
		t.Fatal(err)
	}
	use := filepath.Join(dir, "use.writ")
	if err := os.WriteFile(use, []byte("(import m: \"macs.writ\")\n(m.unless false 42)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New()
	v, err := rt.EvalFile(use)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(42)) {
		t.Fatalf("keyed unless: %v", v)
	}

	rt = New(WithSearchPath(dir))
	v, err = rt.Eval(rd(`
(let [m: (import "macs.writ")]
  (m.unless false 41))
`))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(41)) {
		t.Fatalf("let unless: %v", v)
	}
	v, err = rt.Eval(rd(`
(let [m: (import "macs.writ")]
  (m.unless true (no-such)))
`))
	if err != nil {
		t.Fatal(err)
	}
	if !v.IsNil() {
		t.Fatalf("skip unused: %v", v)
	}
	v, err = rt.Eval(rd(`
(let [m: (import "macs.writ")]
  (m.from-here 2))
`))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(3)) {
		t.Fatalf("def env: %v", v)
	}

	use2 := filepath.Join(dir, "use2.writ")
	if err := os.WriteFile(use2, []byte(`
(import m: "macs.writ")
(def (f)
  (m.announce (prop-set 'n 7))
  (prop-get 'n))
(f)
`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt = New()
	v, err = rt.EvalFile(use2)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(7)) {
		t.Fatalf("announce: %v", v)
	}
	if !rt.GetProp("hit").IsTrue() {
		t.Fatal("announce hit")
	}

	rt = New(WithSearchPath(dir))
	_, err = rt.Eval(rd(`
(let [m: (import "macs.writ")]
  (list-map [false] (map-get m 'unless)))
`))
	if err == nil || !strings.Contains(err.Error(), "macro") {
		t.Fatalf("macro as fn: %v", err)
	}
}

func TestEvalMacroFragments(t *testing.T) {
	rt := New()
	src := `
(defm (example @rest)
  '(prop-set 'hit true)
  @rest)
(def (f)
  (example (prop-set 'n 7))
  (prop-get 'n))
(f)
`
	v, err := rt.Eval(rd(src))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(7)) {
		t.Fatalf("body splice: %v", v)
	}
	if !rt.GetProp("hit").IsTrue() {
		t.Fatalf("first fragment: %v", rt.GetProp("hit"))
	}

	v = evals(t, `
(defm (example @rest)
  '(prop-set 'a 1)
  @rest)
(example (prop-set 'b 2))
(prop-get 'b)
`)
	if !v.Equal(runtime.Int64(2)) {
		t.Fatalf("top-level splice: %v", v)
	}

	v = evals(t, `
(defm (pair)
  '(def (a) 1)
  '(def (b) 2))
(pair)
(+ (a) (b))
`)
	if !v.Equal(runtime.Int64(3)) {
		t.Fatalf("top-level defs: %v", v)
	}

	v = evals(t, `
(defm (seq @rest) @rest)
(+ (seq 1 2) 3)
`)
	if !v.Equal(runtime.Int64(5)) {
		t.Fatalf("expression position: %v", v)
	}

	v = evals(t, `
(defm (example @rest)
  '(prop-set 'a 1)
  @rest)
(if true
  (example (prop-set 'b 2))
  (prop-get 'b)
  else 0)
`)
	if !v.Equal(runtime.Int64(2)) {
		t.Fatalf("if body splice: %v", v)
	}

	v = evals(t, `
(defm (example @rest)
  '(prop-set 'a 1)
  @rest)
(let [x: 3]
  (example (prop-set 'b x))
  (prop-get 'b))
`)
	if !v.Equal(runtime.Int64(3)) {
		t.Fatalf("let body splice: %v", v)
	}

	v = evals(t, `
(defm (example @rest)
  1
  @rest)
(def (f)
  (example)
  2)
(f)
`)
	if !v.Equal(runtime.Int64(2)) {
		t.Fatalf("empty rest: %v", v)
	}

	rt = New()
	if _, err := rt.Eval(rd(`
(defm (h)
  '(on ping () (prop-set 'n 1)))
(h)
`)); err != nil {
		t.Fatal(err)
	}
	if err := rt.Fire("ping", nil); err != nil {
		t.Fatal(err)
	}
	if !rt.GetProp("n").Equal(runtime.Int64(1)) {
		t.Fatalf("on fragment: %v", rt.GetProp("n"))
	}
}

func TestEvalMacroHygiene(t *testing.T) {
	v := evals(t, `
(defm (with-x body)
  '(let [x: 1] ,body))
(let [x: 9]
  (with-x x))
`)
	if !v.Equal(runtime.Int64(9)) {
		t.Fatalf("hygiene want 9 (call-site x), got %v", v)
	}
}

func TestEvalLetBang(t *testing.T) {
	v := evals(t, `
(defm (bind-x body)
  '(let! [x: 1] ,body))
(bind-x x)
`)
	if !v.Equal(runtime.Int64(1)) {
		t.Fatalf("let!: %v", v)
	}
}

func TestEvalPersistsDefsAndMacros(t *testing.T) {
	rt := New()
	if _, err := rt.Eval(rd("(def (inc n) (+ n 1))")); err != nil {
		t.Fatal(err)
	}
	v, err := rt.Eval(rd("(inc 4)"))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(5)) {
		t.Fatalf("inc: %v", v)
	}
	if _, err := rt.Eval(rd(`(defm (unless test @body) (cons 'if (cons 'not (cons test body))))`)); err != nil {
		t.Fatal(err)
	}
	v, err = rt.Eval(rd("(unless false 42)"))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(42)) {
		t.Fatalf("unless: %v", v)
	}
	if _, err := rt.Eval(rd(`(defm (def-answer) '(def (answer) 42))`)); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(rd("(def-answer)")); err != nil {
		t.Fatal(err)
	}
	v, err = rt.Eval(rd("(answer)"))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(42)) {
		t.Fatalf("answer: %v", v)
	}
}

func TestEvalFnMacroClashAcrossCalls(t *testing.T) {
	rt := New()
	if _, err := rt.Eval(rd("(def (f n) n)")); err != nil {
		t.Fatal(err)
	}
	_, err := rt.Eval(rd("(defm (f n) n)"))
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("def then defm: %v", err)
	}
	rt2 := New()
	if _, err := rt2.Eval(rd("(defm (g n) n)")); err != nil {
		t.Fatal(err)
	}
	_, err = rt2.Eval(rd("(def (g n) n)"))
	if err == nil || !strings.Contains(err.Error(), "both") {
		t.Fatalf("defm then def: %v", err)
	}
}

func TestEvalResetAndOnAccumulate(t *testing.T) {
	rt := New()
	if _, err := rt.Eval(rd("(def (inc n) (+ n 1))")); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(rd(`(defm (unless test @body) (cons 'if (cons 'not (cons test body))))`)); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(rd(`(prop-set 'n 0)`)); err != nil {
		t.Fatal(err)
	}
	on := `(on ping () (prop-update 'n (fn + #1 1)))`
	if _, err := rt.Eval(rd(on)); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(rd(on)); err != nil {
		t.Fatal(err)
	}
	if err := rt.Fire("ping", nil); err != nil {
		t.Fatal(err)
	}
	if !rt.GetProp("n").Equal(runtime.Int64(2)) {
		t.Fatalf("accumulate: %v", rt.GetProp("n"))
	}
	rt.Reset()
	if _, ok := rt.Lookup("inc"); ok {
		t.Fatal("lookup after reset")
	}
	if _, err := rt.Eval(rd("(inc 1)")); err == nil {
		t.Fatal("inc after reset")
	}
	if _, err := rt.Eval(rd("(unless false 1)")); err == nil {
		t.Fatal("unless after reset")
	}
	if !rt.GetProp("n").IsNil() {
		t.Fatal("prop after reset")
	}
	if err := rt.Fire("ping", nil); err != nil {
		t.Fatal(err)
	}
	if !rt.GetProp("n").IsNil() {
		t.Fatal("on after reset")
	}
	if _, err := rt.Eval(rd(`(prop-set 'n 0)`)); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(rd(on)); err != nil {
		t.Fatal(err)
	}
	if err := rt.Fire("ping", nil); err != nil {
		t.Fatal(err)
	}
	if !rt.GetProp("n").Equal(runtime.Int64(1)) {
		t.Fatalf("on after reset eval: %v", rt.GetProp("n"))
	}
}

func TestEvalProps(t *testing.T) {
	rt := New()
	if _, err := rt.Eval(rd(`(prop-set 'hits 0)`)); err != nil {
		t.Fatal(err)
	}
	v, err := rt.Eval(rd(`(+ (prop-get 'hits) 2)`))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(2)) {
		t.Fatalf("prop-get: %v", v)
	}
	if _, err := rt.Eval(rd(`(prop-update 'hits (fn + #1 3))`)); err != nil {
		t.Fatal(err)
	}
	if !rt.GetProp("hits").Equal(runtime.Int64(3)) {
		t.Fatalf("prop-update store: %v", rt.GetProp("hits"))
	}
	if _, err := rt.Eval(rd(`(prop-set ['a 'b] 1)`)); err != nil {
		t.Fatal(err)
	}
	if !rt.GetProp("a", "b").Equal(runtime.Int64(1)) {
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
	if !v.Equal(runtime.Int64(1)) {
		t.Fatalf("kw one key: %v", v)
	}
	v = evals(t, `
(def (f a:) 1)
(def (f a: b:) 2)
(f a: 9 b: 8)
`)
	if !v.Equal(runtime.Int64(2)) {
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
	if !v.Equal(runtime.Int64(2)) {
		t.Fatalf("else if taken: %v", v)
	}
	v = evals(t, `(if false 1 else if false 2 else 3)`)
	if !v.Equal(runtime.Int64(3)) {
		t.Fatalf("else if skipped: %v", v)
	}
	v = evals(t, `(if true 1 else if true 2 else 3)`)
	if !v.Equal(runtime.Int64(1)) {
		t.Fatalf("first branch: %v", v)
	}
	v = evals(t, `(if false 1 else if not false 4 else 5)`)
	if !v.Equal(runtime.Int64(4)) {
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

func TestEvalDottedAccess(t *testing.T) {
	v := evals(t, `(let [io: [write: (fn (x) x)]] (io.write 9))`)
	if !v.Equal(runtime.Int64(9)) {
		t.Fatalf("call: %v", v)
	}
	v = evals(t, `(let [io: [write: 4]] io.write)`)
	if !v.Equal(runtime.Int64(4)) {
		t.Fatalf("value: %v", v)
	}
	v = evals(t, `(let [a: [b: [c: 3]]] a.b.c)`)
	if !v.Equal(runtime.Int64(3)) {
		t.Fatalf("nested: %v", v)
	}
	v = evals(t, `(let [f: (fn () [write: 7])] (. (f) write))`)
	if !v.Equal(runtime.Int64(7)) {
		t.Fatalf("computed left: %v", v)
	}
	v = evals(t, `(let [map-get: (fn (m k) 0) m: [a: 1]] m.a)`)
	if !v.Equal(runtime.Int64(1)) {
		t.Fatalf("shadow map-get: %v", v)
	}
	err := evalErr(t, `(let [m: [a: 1]] m.b)`)
	if !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("missing: %v", err)
	}
	err = evalErr(t, `(let [x: 1] x.a)`)
	if !strings.Contains(err.Error(), "map") {
		t.Fatalf("non-map: %v", err)
	}
	err = evalErr(t, `(let [a: [b: 1]] a.b.c)`)
	if !strings.Contains(err.Error(), "map") {
		t.Fatalf("nested non-map: %v", err)
	}
}

func TestEvalKeyedImport(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.writ"), []byte("(def (double n) (* n 2))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	use := filepath.Join(dir, "use.writ")
	if err := os.WriteFile(use, []byte("(import lib: \"lib.writ\")\n(lib.double 21)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := New().EvalFile(use)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(42)) {
		t.Fatalf("keyed import: %v", v)
	}

	wrap := filepath.Join(dir, "wrap.writ")
	if err := os.WriteFile(wrap, []byte("(import lib: \"lib.writ\")\n(def (f) 1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outer := filepath.Join(dir, "outer.writ")
	if err := os.WriteFile(outer, []byte("(import wrap: \"wrap.writ\")\n(wrap.f)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err = New().EvalFile(outer)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(1)) {
		t.Fatalf("wrap f: %v", v)
	}
	miss := filepath.Join(dir, "miss.writ")
	if err := os.WriteFile(miss, []byte("(import wrap: \"wrap.writ\")\nwrap.lib\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = New().EvalFile(miss)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("import not exported: %v", err)
	}

	pos := filepath.Join(dir, "pos.writ")
	if err := os.WriteFile(pos, []byte("(let [m: (import \"lib.writ\")] (m.double 3))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err = New().EvalFile(pos)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(6)) {
		t.Fatalf("positional import: %v", v)
	}

	late := filepath.Join(dir, "late.writ")
	if err := os.WriteFile(late, []byte("(def (f) 1)\n(import lib: \"lib.writ\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = New().EvalFile(late)
	if err == nil || !strings.Contains(err.Error(), "top-level") && !strings.Contains(err.Error(), "before") {
		t.Fatalf("import after def: %v", err)
	}

	rt := New(WithSearchPath(dir))
	if _, err := rt.Eval(rd("(def (f) 1)")); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(rd(`(import lib: "lib.writ")`)); err != nil {
		t.Fatalf("session import after def: %v", err)
	}
	v, err = rt.Eval(rd("(lib.double 4)"))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(8)) {
		t.Fatalf("session lib: %v", v)
	}

	err = evalErr(t, `(let [x: 1] (import lib: "lib.writ"))`)
	if !strings.Contains(err.Error(), "top") {
		t.Fatalf("nested keyed import: %v", err)
	}
	err = evalErr(t, `(import)`)
	if err == nil {
		t.Fatal("empty import")
	}
	err = evalErr(t, `(def (. x) x)`)
	if err == nil || !strings.Contains(err.Error(), "redefine") {
		t.Fatalf("redefine .: %v", err)
	}
	err = evalErr(t, `(import "lib.writ" lib: "lib.writ")`)
	if err == nil {
		t.Fatal("mixed import args")
	}

	clash := filepath.Join(dir, "clash.writ")
	if err := os.WriteFile(clash, []byte("(import lib: \"lib.writ\")\n(def (lib n) n)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = New().EvalFile(clash)
	if err == nil || !strings.Contains(err.Error(), "already bound as an import") {
		t.Fatalf("import then def: %v", err)
	}

	lateMac := filepath.Join(dir, "latemac.writ")
	if err := os.WriteFile(lateMac, []byte(`
(defm (imp)
  '(import lib: "lib.writ"))
(def (f) 1)
(imp)
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = New().EvalFile(lateMac)
	if err == nil || !strings.Contains(err.Error(), "before") && !strings.Contains(err.Error(), "top-level") {
		t.Fatalf("expanded import after def: %v", err)
	}
}

func TestEvalPrivateExport(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.writ"), []byte(`
(def (-helper n) (* n 2))
(def (double n) (-helper n))
(defm (-unless test @body)
  (cons 'if (cons 'not (cons test body))))
(defm (unless test @body)
  (cons 'if (cons 'not (cons test body))))
`), 0o644); err != nil {
		t.Fatal(err)
	}
	use := filepath.Join(dir, "use.writ")
	if err := os.WriteFile(use, []byte("(import lib: \"lib.writ\")\n(lib.double 21)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := New().EvalFile(use)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(42)) {
		t.Fatalf("public uses private: %v", v)
	}
	miss := filepath.Join(dir, "miss.writ")
	if err := os.WriteFile(miss, []byte("(import lib: \"lib.writ\")\nlib.-helper\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = New().EvalFile(miss)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("private fn: %v", err)
	}
	mac := filepath.Join(dir, "mac.writ")
	if err := os.WriteFile(mac, []byte("(import lib: \"lib.writ\")\n(lib.unless false 3)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err = New().EvalFile(mac)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(3)) {
		t.Fatalf("public macro: %v", v)
	}
	privMac := filepath.Join(dir, "privmac.writ")
	if err := os.WriteFile(privMac, []byte("(import lib: \"lib.writ\")\n(lib.-unless false 1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = New().EvalFile(privMac)
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("private macro: %v", err)
	}

	same := filepath.Join(dir, "same.writ")
	if err := os.WriteFile(same, []byte("(def (-h) 9)\n(-h)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err = New().EvalFile(same)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(9)) {
		t.Fatalf("private in file: %v", v)
	}
}

func TestEvalDottedHygiene(t *testing.T) {
	v := evals(t, `
(defm (m)
  '(fn (.) (. [a: 3] a)))
((m) 0)
`)
	if !v.Equal(runtime.Int64(3)) {
		t.Fatalf("fn param named .: %v", v)
	}
	v = evals(t, "(defm (m)\n  '(let [`.:` 1] (let [m: [a: 3]] m.a)))\n(m)\n")
	if !v.Equal(runtime.Int64(3)) {
		t.Fatalf("let bind named .: %v", v)
	}
	v = evals(t, `
(defm (access m write)
  '(. ,m write))
(let [write: 99 m: [write: 8]]
  (access m write))
`)
	if !v.Equal(runtime.Int64(8)) {
		t.Fatalf("key hygiene: %v", v)
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
	if _, err := rt.Eval(rd(`(after 1 (prop-set 'done true))`)); err != nil {
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
	v := evals(t, `(list-map [1 2 3] (fn * #1 10))`)
	if len(v.Items()) != 3 || !v.Items()[1].Equal(runtime.Int64(20)) {
		t.Fatalf("list-map: %v", v)
	}
	v = evals(t, `(list-filter [1 2 3 4] (fn = (mod #1 2) 0))`)
	if len(v.Items()) != 2 {
		t.Fatalf("list-filter: %v", v)
	}
	v = evals(t, `(list-reduce [1 2 3] 0 (fn + #1 #2))`)
	if !v.Equal(runtime.Int64(6)) {
		t.Fatalf("list-reduce: %v", v)
	}
	v = evals(t, `(list-map [a: 1] (fn head #1))`)
	if len(v.Items()) != 1 || v.Items()[0].Name() != "a" {
		t.Fatalf("list-map map: %v", v)
	}
}
