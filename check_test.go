package writ

import (
	"deedles.dev/writ/runtime"
	"deedles.dev/writ/types"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckIntFloat(t *testing.T) {
	res := Check(rd("(+ 1 2)"))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("diags: %v", res.Diagnostics)
	}
	res = Check(rd(`(+ 1 "x")`))
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected error for + string")
	}
	res = Check(rd("(+ 1 2.5)"))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("mix number diags: %v", res.Diagnostics)
	}
}

func TestCheckDynamicVsStatic(t *testing.T) {
	ok := `
(prop-set 'hits 0)
(+ (prop-get 'hits) 1)
`
	res := Check(rd(ok))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("prop-set two-pass: %v", res.Diagnostics)
	}
	foundDyn := false
	for _, h := range res.Hints {
		if strings.Contains(h.Text, "dynamic") && strings.Contains(h.Text, "int") {
			foundDyn = true
		}
	}
	if !foundDyn {
		t.Fatalf("expected dynamic int hint, got %#v", res.Hints)
	}
	bad := `(+ (prop-get 'hits) 1)`
	res = Check(rd(bad))
	if len(res.Diagnostics) == 0 {
		t.Fatal("prop-get with no writes should be nil, not a number")
	}
}

func TestCheckClauseArrows(t *testing.T) {
	src := `
(def (f 0) 1)
(def (f n) (+ n 1))
(f 2)
`
	res := Check(rd(src))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("clauses: %v", res.Diagnostics)
	}
	res = Check(rd(`
(def (f 0) 1)
(def (f n) (+ n 1))
(f "no")
`))
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected no matching clause")
	}
}

func TestCheckIfNarrow(t *testing.T) {
	src := `
(def (f x)
  (if (int? x)
    (+ x 1)
    0))
`
	res := Check(rd(src))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("narrow: %v", res.Diagnostics)
	}
}

func TestCheckUnknownName(t *testing.T) {
	res := Check(rd("(+ nope 1)"))
	if len(res.Diagnostics) == 0 {
		t.Fatal("unknown name")
	}
}

func TestCheckDoesNotSeeEvalBindings(t *testing.T) {
	rt := New()
	if _, err := rt.Eval(rd("(def (inc n) (+ n 1))")); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(rd(`(defm (unless test @body) (cons 'if (cons 'not (cons test body))))`)); err != nil {
		t.Fatal(err)
	}
	res := rt.Check(rd("(inc 4)"))
	if len(res.Diagnostics) == 0 {
		t.Fatal("Check should not see Eval defs")
	}
	res = rt.Check(rd("(unless false 1)"))
	if len(res.Diagnostics) == 0 {
		t.Fatal("Check should not see Eval macros")
	}
}

func TestCheckHints(t *testing.T) {
	res := Check(rd("(+ 1 2)"))
	if len(res.Hints) == 0 {
		t.Fatal("expected hints")
	}
}

func TestCheckHostEvent(t *testing.T) {
	rt := New()
	rt.RegisterEvent("tick", types.PayloadKey{Name: "n", Type: types.IntType()})
	res := rt.Check(rd(`
(on tick (n)
  (+ n 1))
`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("on tick: %v", res.Diagnostics)
	}
	res = rt.Check(rd(`(on boom () 1)`))
	if len(res.Diagnostics) == 0 {
		t.Fatal("unknown event")
	}
	found := false
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, "unknown") && strings.Contains(d.Message, "boom") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want unknown boom, got %v", res.Diagnostics)
	}
}

func TestCheckNoEventsAnyName(t *testing.T) {
	res := Check(rd(`(on whatever () 1)`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("any event: %v", res.Diagnostics)
	}
}

func TestCheckDefNotTop(t *testing.T) {
	res := Check(rd(`(let [x: 1] (def (f) x))`))
	found := false
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, "top") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected top-level def error: %v", res.Diagnostics)
	}
}

func TestCheckHostAlias(t *testing.T) {
	rt := New()
	rt.RegisterAlias("color", "red", "blue")
	ct, ok := rt.AliasType("color")
	if !ok {
		t.Fatal("alias")
	}
	if err := rt.RegisterBuiltin("paint", func(args []runtime.Value) (runtime.Value, error) {
		return runtime.Nil, nil
	}, types.PosArrow(types.NilType(), ct)); err != nil {
		t.Fatal(err)
	}
	res := rt.Check(rd(`(paint "red")`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("paint red: %v", res.Diagnostics)
	}
	res = rt.Check(rd(`(paint "green")`))
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected color error")
	}
}

func TestCheckParseAndFile(t *testing.T) {
	res := Check(rd("("))
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected parse diagnostic")
	}
	res = New().CheckFile(filepath.Join("testdata", "use.writ"))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("use.writ: %v", res.Diagnostics)
	}
	res = New().CheckFile(filepath.Join("testdata", "no-such.writ"))
	if len(res.Diagnostics) == 0 {
		t.Fatal("missing file")
	}
}

func TestCheckEmptyAndIOError(t *testing.T) {
	res := Check(rd(""))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("empty: %v", res.Diagnostics)
	}
	res = Check(rd("  \n  "))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("ws: %v", res.Diagnostics)
	}
	res = Check(errReader{err: errors.New("boom")})
	if len(res.Diagnostics) == 0 || !strings.Contains(res.Diagnostics[0].Message, "boom") {
		t.Fatalf("io: %v", res.Diagnostics)
	}
}

func TestCheckArity(t *testing.T) {
	if len(Check(rd("(-)")).Diagnostics) == 0 {
		t.Fatal("(-)")
	}
	if len(Check(rd("(mod 1)")).Diagnostics) == 0 {
		t.Fatal("(mod 1)")
	}
}

func TestCheckMapSymbolKeys(t *testing.T) {
	res := Check(rd(`(map-get [a: 1] 'a)`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("symbol key: %v", res.Diagnostics)
	}
	res = Check(rd(`(map-get [a: 1] "a")`))
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected error for string map key")
	}
}

func TestCheckKeywordParamName(t *testing.T) {
	res := Check(rd(`
(def (greet name: n) name)
(greet name: "ada")
`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("name: n / name: %v", res.Diagnostics)
	}
}

func TestCheckImportedArrows(t *testing.T) {
	res := New().CheckFile(filepath.Join("testdata", "use.writ"))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("%v", res.Diagnostics)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.writ"), []byte("(def (add a b) (+ a b))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	use := filepath.Join(dir, "bad.writ")
	if err := os.WriteFile(use, []byte(`((map-get (import "lib.writ") 'add) 1 "x")`), 0o644); err != nil {
		t.Fatal(err)
	}
	res = New().CheckFile(use)
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected imported add type error")
	}
}

func TestCheckKeywordExtraArgs(t *testing.T) {
	res := Check(rd(`
(def (f a:) 1)
(f a: 1 b: 2)
`))
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected extra keyword diagnostic")
	}
}

func TestCheckFileImportUsesScriptDir(t *testing.T) {
	root := t.TempDir()
	jail := filepath.Join(root, "jail")
	if err := os.Mkdir(jail, 0o755); err != nil {
		t.Fatal(err)
	}
	cwdsec := filepath.Join(root, "cwdsec.writ")
	if err := os.WriteFile(cwdsec, []byte(`(prop-set 'leaked 1)`), 0o644); err != nil {
		t.Fatal(err)
	}
	evil := filepath.Join(jail, "evil.writ")
	if err := os.WriteFile(evil, []byte(`
(defm (p)
  (import "cwdsec.writ")
  1)
(p)
`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	rt := New()
	res := rt.CheckFile(evil)
	if rt.GetProp("leaked").Equal(runtime.Int64(1)) {
		t.Fatalf("CheckFile imported cwd file: diags=%v", res.Diagnostics)
	}
	if _, err := New().EvalFile(evil); err == nil {
		t.Fatal("EvalFile should refuse cwdsec from jail")
	}
}

func TestCheckDoesNotScheduleAfter(t *testing.T) {
	n := 0
	rt := New(WithScheduler(func(_ time.Duration, fn func()) { n++ }))
	_ = rt.Check(rd(`
(defm (p)
  (after 0 1)
  1)
(p)
`))
	if n != 0 {
		t.Fatalf("Check scheduled after %d times", n)
	}
}

func TestCheckSurfacesImportedErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.writ"), []byte("(def (bad) (+ 1 \"x\"))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	use := filepath.Join(dir, "use.writ")
	if err := os.WriteFile(use, []byte(`(import "lib.writ")`), 0o644); err != nil {
		t.Fatal(err)
	}
	res := New().CheckFile(use)
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected diagnostics from imported lib")
	}
}

func TestCheckPrintAligned(t *testing.T) {
	res := Check(rd(`(print "hi")`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("print: %v", res.Diagnostics)
	}
}

func TestCheckDottedAccess(t *testing.T) {
	res := Check(rd(`(let [io: [write: (fn (x) x)]] (io.write 1))`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("call: %v", res.Diagnostics)
	}
	res = Check(rd(`(let [a: [b: [c: 3]]] a.b.c)`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("nested: %v", res.Diagnostics)
	}
	res = Check(rd(`(let [m: [a: 1]] m.b)`))
	found := false
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, "unknown field") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing field: %v", res.Diagnostics)
	}
	res = Check(rd(`(let [x: 1] x.a)`))
	found = false
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, "map") {
			found = true
		}
	}
	if !found {
		t.Fatalf("non-map: %v", res.Diagnostics)
	}
	res = Check(rd(`(fn (m) (if (map? m) m.x else 0))`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("map? then .: %v", res.Diagnostics)
	}
	res = Check(rd(`(let [m: (if true [a: 1] else [b: 2])] m.a)`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("union m.a: %v", res.Diagnostics)
	}
	res = Check(rd(`(let [m: (if true [a: [c: 1]] else [a: [d: 2]])] m.a.c)`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("union nested: %v", res.Diagnostics)
	}
}

func TestCheckKeyedImport(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.writ"), []byte("(def (add a b) (+ a b))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	use := filepath.Join(dir, "use.writ")
	if err := os.WriteFile(use, []byte("(import lib: \"lib.writ\")\n(lib.add 1 2)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := New().CheckFile(use)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("keyed import: %v", res.Diagnostics)
	}
	bad := filepath.Join(dir, "bad.writ")
	if err := os.WriteFile(bad, []byte("(import lib: \"lib.writ\")\n(lib.add 1 \"x\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = New().CheckFile(bad)
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected imported add type error")
	}
	late := filepath.Join(dir, "late.writ")
	if err := os.WriteFile(late, []byte("(def (f) 1)\n(import lib: \"lib.writ\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = New().CheckFile(late)
	found := false
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, "top-level") || strings.Contains(d.Message, "before") {
			found = true
		}
	}
	if !found {
		t.Fatalf("import after def: %v", res.Diagnostics)
	}

	rt := New()
	rt.RegisterPackage("io", runtime.Package{
		Funcs: map[string]runtime.Func{
			"write": func(args []runtime.Value) (runtime.Value, error) {
				return runtime.Nil, nil
			},
		},
	})
	res = rt.Check(rd(`(import io: "io") (io.write "x")`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("host io.write: %v", res.Diagnostics)
	}

	if err := os.WriteFile(filepath.Join(dir, "priv.writ"), []byte("(def (-h n) n)\n(def (f n) n)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	privUse := filepath.Join(dir, "privuse.writ")
	if err := os.WriteFile(privUse, []byte("(import p: \"priv.writ\")\n(p.f 1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = New().CheckFile(privUse)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("public: %v", res.Diagnostics)
	}
	privBad := filepath.Join(dir, "privbad.writ")
	if err := os.WriteFile(privBad, []byte("(import p: \"priv.writ\")\n(p.-h 1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = New().CheckFile(privBad)
	found = false
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, "unknown field") {
			found = true
		}
	}
	if !found {
		t.Fatalf("private field: %v", res.Diagnostics)
	}
}

func TestCheckImportedMacros(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "macs.writ"), []byte(`
(defm (unless test @body)
  (cons 'if (cons 'not (cons test body))))
`), 0o644); err != nil {
		t.Fatal(err)
	}
	use := filepath.Join(dir, "use.writ")
	if err := os.WriteFile(use, []byte("(import m: \"macs.writ\")\n(m.unless false 1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := New().CheckFile(use)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("imported unless: %v", res.Diagnostics)
	}
}

func TestCheckDefmFragments(t *testing.T) {
	res := Check(rd(`
(defm (example @rest)
  '(prop-set 'hit true)
  @rest)
(def (f)
  (example (prop-set 'n 7))
  (prop-get 'n))
`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("defm fragments: %v", res.Diagnostics)
	}
}

func TestCheckModInt(t *testing.T) {
	res := Check(rd("(mod 3 2)"))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("%v", res.Diagnostics)
	}
	res = Check(rd("(mod 3 2.0)"))
	if len(res.Diagnostics) == 0 {
		t.Fatal("mod float")
	}
}

type nativeHandle struct{ n int }

type nativeOther struct{ n int }

func TestCheckNativeHostArrow(t *testing.T) {
	rt := New()
	if err := rt.RegisterBuiltin("mk-box", func(args []runtime.Value) (runtime.Value, error) {
		return runtime.Native(&nativeHandle{n: 7}), nil
	}, types.PosArrow(types.Native[*nativeHandle]())); err != nil {
		t.Fatal(err)
	}
	if err := rt.RegisterBuiltin("use-box", func(args []runtime.Value) (runtime.Value, error) {
		var h *nativeHandle
		if !args[0].As(&h) || h == nil {
			return runtime.Nil, runtime.ErrorMsg("want handle")
		}
		return runtime.Int64(int64(h.n)), nil
	}, types.PosArrow(types.IntType(), types.Native[*nativeHandle]())); err != nil {
		t.Fatal(err)
	}
	if err := rt.RegisterBuiltin("echo-box", func(args []runtime.Value) (runtime.Value, error) {
		return args[0], nil
	}, types.PosArrow(types.Native[*nativeHandle](), types.Native[*nativeHandle]())); err != nil {
		t.Fatal(err)
	}
	if err := rt.RegisterBuiltin("mk-other", func(args []runtime.Value) (runtime.Value, error) {
		return runtime.Native(&nativeOther{n: 1}), nil
	}, types.PosArrow(types.Native[*nativeOther]())); err != nil {
		t.Fatal(err)
	}

	res := rt.Check(rd("(use-box (mk-box))"))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("accept matching native: %v", res.Diagnostics)
	}
	res = rt.Check(rd("(use-box 1)"))
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected native type error")
	}
	res = rt.Check(rd("(use-box (mk-other))"))
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected Native[A] vs Native[B] error")
	}
	res = rt.Check(rd(`(let [x: (mk-box)] (use-box x))`))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("var from host return: %v", res.Diagnostics)
	}

	v, err := rt.Eval(rd("(use-box (mk-box))"))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(7)) {
		t.Fatalf("round-trip: %v", v)
	}

	echoed, err := rt.Eval(rd("(echo-box (mk-box))"))
	if err != nil {
		t.Fatal(err)
	}
	var h *nativeHandle
	if !echoed.As(&h) || h == nil || h.n != 7 {
		t.Fatalf("echo As: %#v", echoed)
	}
	raw, ok := echoed.Native()
	if !ok || raw.(*nativeHandle) != h {
		t.Fatal("identity not preserved")
	}
}
