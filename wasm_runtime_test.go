//go:build !js && !wasm

package writ

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"deedles.dev/writ/syntax"
)

func TestLoadWasmMissingAndGarbage(t *testing.T) {
	if _, err := LoadWasm(filepath.Join(t.TempDir(), "nope.wasm")); err == nil {
		t.Fatal("missing")
	}
	p := filepath.Join(t.TempDir(), "junk.wasm")
	if err := os.WriteFile(p, []byte("not wasm"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadWasm(p); err == nil {
		t.Fatal("garbage")
	}
}

func TestLoadWasmHello(t *testing.T) {
	wasm := filepath.Join(t.TempDir(), "wasmhello.wasm")
	cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", wasm, "../example/wasmhello")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("wasip1 c-shared build failed (common in tests): %s", out)
	}
	pkg, err := LoadWasm(wasm)
	if err != nil {
		t.Fatal(err)
	}
	v, ok := pkg.Exports["version"]
	if !ok || !v.Equal(Int64(1)) {
		t.Fatalf("version %v %v", v, ok)
	}
	greetV, ok := pkg.Exports["greet"]
	if !ok || greetV.fnData() == nil || greetV.fnData().native == nil {
		t.Fatal("greet")
	}
	out, err := greetV.fnData().native([]Value{String("ada")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text() != "hello, ada" {
		t.Fatalf("greet: %v", out)
	}
	unlessV, ok := pkg.Exports["unless"]
	if !ok || unlessV.fnData() == nil || unlessV.fnData().macro == nil {
		t.Fatal("unless")
	}
	frags, err := unlessV.fnData().macro([]syntax.Form{syntax.False, syntax.Int64(7)})
	if err != nil {
		t.Fatal(err)
	}
	if frags.Kind() != syntax.KindList || len(frags.Items()) != 1 {
		t.Fatalf("unless frags: %v", frags)
	}
}

func TestLoadWasmCrossPackageOpaque(t *testing.T) {
	wasm := filepath.Join(t.TempDir(), "a.wasm")
	cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", wasm, "../example/wasmhello")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("wasip1 c-shared build failed (common in tests): %s", out)
	}
	pkgA, err := LoadWasm(wasm)
	if err != nil {
		t.Fatal(err)
	}
	pkgB, err := LoadWasm(wasm)
	if err != nil {
		t.Fatal(err)
	}
	mk := pkgA.Exports["mk"].fnData().native
	echo := pkgB.Exports["echo"].fnData().native
	inc := pkgA.Exports["inc"].fnData().native
	get := pkgA.Exports["get"].fnData().native
	c, err := mk(nil)
	if err != nil {
		t.Fatal(err)
	}
	passed, err := echo([]Value{c})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := passed.As[*struct{}](); ok {
		t.Fatal("stranger must not unwrap")
	}
	if _, err := inc([]Value{passed}); err != nil {
		t.Fatal(err)
	}
	n, err := get([]Value{passed})
	if err != nil {
		t.Fatal(err)
	}
	if !n.Equal(Int64(1)) {
		t.Fatalf("after echo+inc: %v", n)
	}
}
