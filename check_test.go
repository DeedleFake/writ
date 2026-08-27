package writ

import (
	"deedles.dev/writ/runtime"
	"deedles.dev/writ/types"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCheckIntFloat(t *testing.T) {
	res := Check("(+ 1 2)")
	if len(res.Diagnostics) != 0 {
		t.Fatalf("diags: %v", res.Diagnostics)
	}
	res = Check(`(+ 1 "x")`)
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected error for + string")
	}
	res = Check("(+ 1 2.5)")
	if len(res.Diagnostics) != 0 {
		t.Fatalf("mix number diags: %v", res.Diagnostics)
	}
}

func TestCheckDynamicVsStatic(t *testing.T) {
	ok := `
(set-prop "hits" 0)
(+ (get-prop "hits") 1)
`
	res := Check(ok)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("set-prop two-pass: %v", res.Diagnostics)
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
	bad := `(+ (get-prop "hits") 1)`
	res = Check(bad)
	if len(res.Diagnostics) == 0 {
		t.Fatal("get-prop with no writes should be nil, not a number")
	}
}

func TestCheckClauseArrows(t *testing.T) {
	src := `
(def (f 0) 1)
(def (f n) (+ n 1))
(f 2)
`
	res := Check(src)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("clauses: %v", res.Diagnostics)
	}
	res = Check(`
(def (f 0) 1)
(def (f n) (+ n 1))
(f "no")
`)
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
	res := Check(src)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("narrow: %v", res.Diagnostics)
	}
}

func TestCheckUnknownName(t *testing.T) {
	res := Check("(+ nope 1)")
	if len(res.Diagnostics) == 0 {
		t.Fatal("unknown name")
	}
}

func TestCheckHints(t *testing.T) {
	res := Check("(+ 1 2)")
	if len(res.Hints) == 0 {
		t.Fatal("expected hints")
	}
}

func TestCheckHostEvent(t *testing.T) {
	rt := New()
	rt.RegisterEvent("tick", types.PayloadKey{Name: "n", Type: types.IntType()})
	res := rt.Check(`
(on tick (n)
  (+ n 1))
`)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("on tick: %v", res.Diagnostics)
	}
	res = rt.Check(`(on boom () 1)`)
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
	res := Check(`(on whatever () 1)`)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("any event: %v", res.Diagnostics)
	}
}

func TestCheckDefNotTop(t *testing.T) {
	res := Check(`(let [x: 1] (def (f) x))`)
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
	res := rt.Check(`(paint "red")`)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("paint red: %v", res.Diagnostics)
	}
	res = rt.Check(`(paint "green")`)
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected color error")
	}
}

func TestCheckParseAndFile(t *testing.T) {
	res := Check("(")
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

func TestCheckArity(t *testing.T) {
	if len(Check("(-)").Diagnostics) == 0 {
		t.Fatal("(-)")
	}
	if len(Check("(mod 1)").Diagnostics) == 0 {
		t.Fatal("(mod 1)")
	}
}

func TestCheckKeywordParamName(t *testing.T) {
	res := Check(`
(def (greet name: n) name)
(greet name: "ada")
`)
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
	if err := os.WriteFile(use, []byte(`((get (import "lib.writ") "add") 1 "x")`), 0o644); err != nil {
		t.Fatal(err)
	}
	res = New().CheckFile(use)
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected imported add type error")
	}
}

func TestCheckKeywordExtraArgs(t *testing.T) {
	res := Check(`
(def (f a:) 1)
(f a: 1 b: 2)
`)
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
	if err := os.WriteFile(cwdsec, []byte(`(set-prop "leaked" 1)`), 0o644); err != nil {
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
	_ = rt.Check(`
(defm (p)
  (after 0 1)
  1)
(p)
`)
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
	res := Check(`(print "hi")`)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("print: %v", res.Diagnostics)
	}
}

func TestCheckModInt(t *testing.T) {
	res := Check("(mod 3 2)")
	if len(res.Diagnostics) != 0 {
		t.Fatalf("%v", res.Diagnostics)
	}
	res = Check("(mod 3 2.0)")
	if len(res.Diagnostics) == 0 {
		t.Fatal("mod float")
	}
}
