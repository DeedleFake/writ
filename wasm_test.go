package writ

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"deedles.dev/writ/runtime"
	"deedles.dev/writ/syntax"
)

func buildWasmHello(t *testing.T) string {
	t.Helper()
	wasm := filepath.Join(t.TempDir(), "wasmhello.wasm")
	cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", wasm, "./example/wasmhello")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("wasip1 c-shared build failed (common in tests): %s", out)
	}
	return wasm
}

func TestWasmImportGreet(t *testing.T) {
	wasm := buildWasmHello(t)
	rt := New(WithAllowAbsoluteImports())
	src := `(let [m: (import "` + filepath.ToSlash(wasm) + `")]
  (m.greet "ada"))`
	v, err := rt.Eval(rd(src))
	if err != nil {
		t.Fatal(err)
	}
	if v.Text() != "hello, ada" {
		t.Fatalf("greet: %v", v)
	}
	v, err = rt.Eval(rd(`(map-get (import "` + filepath.ToSlash(wasm) + `") 'version)`))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(1)) {
		t.Fatalf("version: %v", v)
	}
}

func TestWasmImportMacro(t *testing.T) {
	wasm := buildWasmHello(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "use.writ")
	body := "(import m: \"" + filepath.ToSlash(wasm) + "\")\n(m.unless false 42)\n"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New(WithAllowAbsoluteImports())
	v, err := rt.EvalFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(42)) {
		t.Fatalf("unless: %v", v)
	}
}

func TestWasmCheckInstantiates(t *testing.T) {
	wasm := buildWasmHello(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "use.writ")
	body := "(import m: \"" + filepath.ToSlash(wasm) + "\")\n(m.unless false 1)\n"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New(WithAllowAbsoluteImports())
	res := rt.CheckFile(src)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("check wasm: %v", res.Diagnostics)
	}
}

func TestWasmLoadsByDefault(t *testing.T) {
	wasm := buildWasmHello(t)
	rt := New(WithAllowAbsoluteImports())
	v, err := rt.Eval(rd(`(map-get (import "` + filepath.ToSlash(wasm) + `") 'version)`))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(1)) {
		t.Fatalf("version: %v", v)
	}
}

func TestWasmMissingAndGarbage(t *testing.T) {
	_, err := New().Eval(rd(`(import "definitely-missing.wasm")`))
	if err == nil {
		t.Fatal("expected missing wasm error")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "junk.wasm")
	if err := os.WriteFile(p, []byte("not a wasm"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New(WithAllowAbsoluteImports())
	_, err = rt.Eval(rd(`(import "` + filepath.ToSlash(p) + `")`))
	if err == nil {
		t.Fatal("expected garbage wasm error")
	}
	if !strings.Contains(err.Error(), "wasm") && !strings.Contains(err.Error(), "module") {
		t.Log(err)
	}
}

func TestWasmCheckLoadError(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "junk.wasm")
	if err := os.WriteFile(p, []byte("not a wasm"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "use.writ")
	body := "(import m: \"" + filepath.ToSlash(p) + "\")\n1\n"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New(WithAllowAbsoluteImports())
	res := rt.CheckFile(src)
	if len(res.Diagnostics) == 0 {
		t.Fatal("expected check diagnostic for garbage wasm")
	}
}

func TestRegisterPackageMacro(t *testing.T) {
	rt := New()
	rt.RegisterPackage("mac", runtime.Package{
		Macros: map[string]runtime.Macro{
			"unless": func(args []syntax.Form) (syntax.Form, error) {
				if len(args) < 2 {
					return syntax.Form{}, runtime.ErrorMsg("unless needs 2 args")
				}
				form := syntax.CallList(
					syntax.Symbol("if"),
					syntax.CallList(syntax.Symbol("not"), args[0]),
					args[1],
				)
				return syntax.CallList(form), nil
			},
		},
	})
	v, err := rt.Eval(rd(`
(import m: "mac")
(m.unless false 9)
`))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(9)) {
		t.Fatalf("native macro: %v", v)
	}
}

func TestWasmOpaqueBox(t *testing.T) {
	wasm := buildWasmHello(t)
	rt := New(WithAllowAbsoluteImports())
	src := `(let [m: (import "` + filepath.ToSlash(wasm) + `")]
  (let [c: (m.mk)]
    (m.inc c)
    (m.inc c)
    (m.get c)))`
	v, err := rt.Eval(rd(src))
	if err != nil {
		t.Fatal(err)
	}
	if !v.Equal(runtime.Int64(2)) {
		t.Fatalf("box: %v", v)
	}
}
