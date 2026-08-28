package writ

import (
	"bytes"
	"deedles.dev/writ/runtime"
	"deedles.dev/writ/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImportWritFile(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.writ")
	if err := os.WriteFile(lib, []byte("(def (inc n) (+ n 1))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	use := filepath.Join(dir, "use.writ")
	if err := os.WriteFile(use, []byte(`
(let [m: (import "lib.writ")]
  ((map-get m 'inc) 41))
`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New()
	v, err := rt.EvalFile(use)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(42)) {
		t.Fatalf("import: %v", v)
	}
}

func TestImportCycle(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.writ")
	b := filepath.Join(dir, "b.writ")
	if err := os.WriteFile(a, []byte(`(import "b.writ")`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`(import "a.writ")`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New()
	_, err := rt.EvalFile(a)
	if err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("expected cycle, got %v", err)
	}
	res := New().CheckFile(a)
	found := false
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, "cycle") {
			found = true
		}
	}
	if !found {
		t.Fatalf("CheckFile cycle: %v", res.Diagnostics)
	}
}

func TestRegisterPackage(t *testing.T) {
	rt := New()
	rt.RegisterPackage("mathx", runtime.Package{
		Funcs: map[string]runtime.Func{
			"double": func(args []runtime.Value) (runtime.Value, error) {
				if len(args) != 1 || !args[0].IsInt() {
					return runtime.Nil, runtime.ErrorMsg("double needs an int")
				}
				return runtime.Int64(args[0].BigInt().Int64() * 2), nil
			},
		},
		Vals: map[string]runtime.Value{
			"pi": runtime.Float(3.25),
		},
	})
	v, err := rt.Eval(`
(let [m: (import "mathx")]
  (+ ((map-get m 'double) 3) 0))
`)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(6)) {
		t.Fatalf("native: %v", v)
	}
	res := rt.Check(`
(let [m: (import "mathx")]
  ((map-get m 'double) 3))
`)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("check native: %v", res.Diagnostics)
	}
}

func TestRegisterPackageSimple(t *testing.T) {
	rt := New()
	rt.RegisterPackage("hello", runtime.Package{
		Funcs: map[string]runtime.Func{
			"greet": func(args []runtime.Value) (runtime.Value, error) {
				return runtime.String("hello"), nil
			},
		},
		Vals: map[string]runtime.Value{"n": runtime.Int64(1)},
	})
	v, err := rt.Eval(`(map-get (import "hello") 'n)`)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(1)) {
		t.Fatalf("%v", v)
	}
}

func TestHostBuiltinAndEvent(t *testing.T) {
	rt := New()
	var seen []string
	if err := rt.RegisterBuiltin("log", func(args []runtime.Value) (runtime.Value, error) {
		if len(args) > 0 {
			seen = append(seen, runtime.Print(args[0]))
		}
		return runtime.Nil, nil
	}, types.PosRestArrow(types.NilType())); err != nil {
		t.Fatal(err)
	}
	rt.RegisterEvent("ping", types.PayloadKey{Name: "who", Type: types.StringType()})
	if _, err := rt.Eval(`
(on ping (who)
  (log who))
`); err != nil {
		t.Fatal(err)
	}
	if err := rt.Fire("ping", map[string]runtime.Value{"who": runtime.String("ada")}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0] != "ada" {
		t.Fatalf("fire: %v", seen)
	}
}

func TestAfterNoSleep(t *testing.T) {
	var jobs []func()
	var delays []time.Duration
	rt := New(WithScheduler(func(d time.Duration, fn func()) {
		delays = append(delays, d)
		jobs = append(jobs, fn)
	}))
	if _, err := rt.Eval(`(after 0.5 (prop-set 'x 1))`); err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs %d", len(jobs))
	}
	if delays[0] != 500*time.Millisecond {
		t.Fatalf("delay %v", delays[0])
	}
	jobs[0]()
	if !rt.GetProp("x").Equal(runtime.Int64(1)) {
		t.Fatal(rt.GetProp("x"))
	}
}

func TestEvalLimit(t *testing.T) {
	rt := New(WithEvalLimit(5))
	if _, err := rt.Eval("(+ 1 2 3 4 5 6 7 8)"); err == nil {
		t.Fatal("expected budget error")
	}
}

func TestTestdataImport(t *testing.T) {
	rt := New()
	v, err := rt.EvalFile(filepath.Join("testdata", "use.writ"))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(44)) {
		t.Fatalf("testdata import: %v", v)
	}
}

func TestImportedHandlersUseLibEnv(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.writ")
	if err := os.WriteFile(lib, []byte(`
(def (handle n) (prop-set 'x n))
(on tick (n) (handle n))
`), 0o644); err != nil {
		t.Fatal(err)
	}
	use := filepath.Join(dir, "use.writ")
	if err := os.WriteFile(use, []byte(`(import "lib.writ")`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New()
	if _, err := rt.EvalFile(use); err != nil {
		t.Fatal(err)
	}
	if err := rt.Fire("tick", map[string]runtime.Value{"n": runtime.Int64(7)}); err != nil {
		t.Fatal(err)
	}
	if !rt.GetProp("x").Equal(runtime.Int64(7)) {
		t.Fatalf("imported on: %v %v", rt.GetProp("x"), "want 7")
	}
}

func TestEvalFileOnAccumulates(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "on.writ")
	if err := os.WriteFile(src, []byte("(on ping () (prop-update 'n (fn + #1 1)))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New()
	if err := rt.SetProp(runtime.Int64(0), "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.EvalFile(src); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.EvalFile(src); err != nil {
		t.Fatal(err)
	}
	if err := rt.Fire("ping", nil); err != nil {
		t.Fatal(err)
	}
	if !rt.GetProp("n").Equal(runtime.Int64(2)) {
		t.Fatalf("EvalFile accumulate: %v", rt.GetProp("n"))
	}
	rt.Reset()
	if err := rt.SetProp(runtime.Int64(0), "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.EvalFile(src); err != nil {
		t.Fatal(err)
	}
	if err := rt.Fire("ping", nil); err != nil {
		t.Fatal(err)
	}
	if !rt.GetProp("n").Equal(runtime.Int64(1)) {
		t.Fatalf("EvalFile after Reset: %v", rt.GetProp("n"))
	}
}

func TestFirePositionalWithoutRegisterEvent(t *testing.T) {
	rt := New()
	if _, err := rt.Eval(`(on ping (who) (prop-set 'w who))`); err != nil {
		t.Fatal(err)
	}
	if err := rt.Fire("ping", map[string]runtime.Value{"who": runtime.String("ada")}); err != nil {
		t.Fatal(err)
	}
	if rt.GetProp("w").Text() != "ada" {
		t.Fatalf("got %v", rt.GetProp("w"))
	}
}

func TestFireKeywordAndMissingKey(t *testing.T) {
	rt := New()
	if _, err := rt.Eval(`
(on ping (who:)
  (prop-set 'w who))
`); err != nil {
		t.Fatal(err)
	}
	if err := rt.Fire("ping", map[string]runtime.Value{"who": runtime.String("ada")}); err != nil {
		t.Fatal(err)
	}
	if rt.GetProp("w").Text() != "ada" {
		t.Fatalf("kw fire: %v", rt.GetProp("w"))
	}
	rt2 := New()
	rt2.RegisterEvent("ping", types.PayloadKey{Name: "who", Type: types.StringType()})
	if _, err := rt2.Eval(`
(on ping (who)
  (prop-set 'w who))
`); err != nil {
		t.Fatal(err)
	}
	if err := rt2.Fire("ping", map[string]runtime.Value{}); err != nil {
		t.Fatal(err)
	}
	if !rt2.GetProp("w").IsNil() {
		t.Fatalf("missing key: %v", rt2.GetProp("w"))
	}
}

func TestFireKeywordIgnoresExtraPayload(t *testing.T) {
	rt := New()
	if _, err := rt.Eval(`
(on ping (who:)
  (prop-set 'w who))
`); err != nil {
		t.Fatal(err)
	}
	if err := rt.Fire("ping", map[string]runtime.Value{"who": runtime.String("ada"), "n": runtime.Int64(1)}); err != nil {
		t.Fatal(err)
	}
	if rt.GetProp("w").Text() != "ada" {
		t.Fatalf("extra payload keys skipped handler: %v", rt.GetProp("w"))
	}
	rt.RegisterEvent("pong", types.PayloadKey{Name: "who", Type: types.StringType()}, types.PayloadKey{Name: "n", Type: types.IntType()})
	if _, err := rt.Eval(`
(on pong (who:)
  (prop-set 'p who))
`); err != nil {
		t.Fatal(err)
	}
	if err := rt.Fire("pong", map[string]runtime.Value{"who": runtime.String("ada"), "n": runtime.Int64(1)}); err != nil {
		t.Fatal(err)
	}
	if rt.GetProp("p").Text() != "ada" {
		t.Fatalf("event spec extras: %v", rt.GetProp("p"))
	}
}

func TestMainLookupApplyImport(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lib.writ"), []byte("(def (inc n) (+ n 1))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.writ")
	if err := os.WriteFile(main, []byte(`
(def (main)
  (let [m: (import "lib.writ")]
    ((map-get m 'inc) 41)))
`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New()
	if _, err := rt.EvalFile(main); err != nil {
		t.Fatal(err)
	}
	fn, ok := rt.Lookup("main")
	if !ok {
		t.Fatal("lookup main")
	}
	v, err := rt.Apply(fn, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(42)) {
		t.Fatalf("apply main: %v", v)
	}
}

func TestRegisterPrintStdout(t *testing.T) {
	var buf bytes.Buffer
	rt := New(WithStdout(&buf))
	rt.RegisterPrint()
	if _, err := rt.Eval(`(print "x")`); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "x\n" {
		t.Fatalf("print: %q", buf.String())
	}
}

func TestWithSearchPath(t *testing.T) {
	dir := t.TempDir()
	libdir := filepath.Join(dir, "libs")
	if err := os.Mkdir(libdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libdir, "lib.writ"), []byte("(def (n) 3)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "use.writ")
	if err := os.WriteFile(src, []byte(`((map-get (import "lib.writ") 'n))`), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New(WithSearchPath(libdir))
	v, err := rt.EvalFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(3)) {
		t.Fatalf("search path: %v", v)
	}
}

func TestAfterErrorHook(t *testing.T) {
	var jobs []func()
	var got error
	rt := New(
		WithScheduler(func(_ time.Duration, fn func()) { jobs = append(jobs, fn) }),
		WithAfterError(func(err error) { got = err }),
	)
	if _, err := rt.Eval(`(after 0 (+ 1 "x"))`); err != nil {
		t.Fatal(err)
	}
	jobs[0]()
	if got == nil {
		t.Fatal("expected after error")
	}
}

func TestAfterErrorHookCanGetProp(t *testing.T) {
	var jobs []func()
	rt := New(WithScheduler(func(_ time.Duration, fn func()) { jobs = append(jobs, fn) }))
	rt.SetAfterError(func(err error) {
		_ = rt.GetProp("x")
	})
	if _, err := rt.Eval(`(after 0 (+ 1 "x"))`); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		jobs[0]()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("after error hook deadlocked on GetProp")
	}
}

func TestHostBuiltinCanGetProp(t *testing.T) {
	rt := New()
	if err := rt.RegisterBuiltin("gp", func(args []runtime.Value) (runtime.Value, error) {
		return rt.GetProp("x"), nil
	}, types.PosRestArrow(types.Any())); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`(prop-set 'x 3)`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		v, err := rt.Eval(`(gp)`)
		if err != nil {
			done <- err
			return
		}
		if !v.Equal(runtime.Int64(3)) {
			done <- runtime.ErrorMsg("want 3")
			return
		}
		done <- nil
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("host builtin deadlocked on GetProp")
	}
}

func TestImportRejectsNonWrit(t *testing.T) {
	dir := t.TempDir()
	body := "(def (n) 1)\n"
	txt := filepath.Join(dir, "ok.txt")
	if err := os.WriteFile(txt, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "use.writ")
	if err := os.WriteFile(src, []byte(`(import "ok.txt")`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := New().EvalFile(src)
	if err == nil {
		t.Fatal("expected reject non-writ")
	}
	msg := err.Error()
	if !strings.Contains(msg, "cannot find import") && !strings.Contains(msg, ".writ") && !strings.Contains(msg, "ok.txt") {
		t.Fatalf("want suffix/not-found error, got %v", err)
	}
	writPath := filepath.Join(dir, "ok.writ")
	if err := os.WriteFile(writPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	use2 := filepath.Join(dir, "use2.writ")
	if err := os.WriteFile(use2, []byte(`((map-get (import "ok.writ") 'n))`), 0o644); err != nil {
		t.Fatal(err)
	}
	v, err := New().EvalFile(use2)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(1)) {
		t.Fatalf("ok.writ: %v", v)
	}
}

func TestImportDeniesAbsoluteAndDotDot(t *testing.T) {
	root := t.TempDir()
	secretDir := filepath.Join(root, "secret")
	aDir := filepath.Join(root, "a")
	if err := os.Mkdir(secretDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(aDir, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(secretDir, "secret.writ")
	if err := os.WriteFile(secret, []byte("(def (n) 9)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, err := filepath.Abs(secret)
	if err != nil {
		t.Fatal(err)
	}
	_, err = New().Eval(`(import "` + filepath.ToSlash(abs) + `")`)
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("absolute: %v", err)
	}
	use := filepath.Join(aDir, "use.writ")
	if err := os.WriteFile(use, []byte(`(import "../secret/secret.writ")`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New().EvalFile(use); err == nil {
		t.Fatal("expected .. escape to fail")
	}
	v, err := New(WithAllowAbsoluteImports()).EvalFile(use)
	if err != nil {
		t.Fatal(err)
	}
	_ = v
	v, err = New(WithAllowAbsoluteImports()).Eval(`((map-get (import "` + filepath.ToSlash(abs) + `") 'n))`)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(9)) {
		t.Fatalf("allow abs: %v", v)
	}
}

func TestSearchPathSkipsCwd(t *testing.T) {
	cwd := t.TempDir()
	libdir := t.TempDir()
	t.Chdir(cwd)
	if err := os.WriteFile(filepath.Join(cwd, "lib.writ"), []byte("(def (n) 1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(libdir, "other.writ"), []byte("(def (n) 2)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New(WithSearchPath(libdir))
	if _, err := rt.Eval(`(import "lib.writ")`); err == nil {
		t.Fatal("cwd lib.writ should not be imported when search path is set")
	}
	v, err := rt.Eval(`((map-get (import "other.writ") 'n))`)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(2)) {
		t.Fatalf("search: %v", v)
	}
}

func TestPluginsDisabledByDefault(t *testing.T) {
	_, err := New().Eval(`(import "missing.so")`)
	if err == nil || !strings.Contains(err.Error(), "plugin") {
		t.Fatalf("got %v", err)
	}
}
