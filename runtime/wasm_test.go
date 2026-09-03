//go:build !js && !wasm

package runtime

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
	v, ok := pkg.Vals["version"]
	if !ok || !v.Equal(Int64(1)) {
		t.Fatalf("version %v %v", v, ok)
	}
	greet, ok := pkg.Funcs["greet"]
	if !ok || greet == nil {
		t.Fatal("greet")
	}
	out, err := greet([]Value{String("ada")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Text() != "hello, ada" {
		t.Fatalf("greet: %v", out)
	}
	unless, ok := pkg.Macros["unless"]
	if !ok || unless == nil {
		t.Fatal("unless")
	}
	frags, err := unless([]syntax.Form{syntax.False, syntax.Int64(7)})
	if err != nil {
		t.Fatal(err)
	}
	if frags.Kind() != syntax.KindList || len(frags.Items()) != 1 {
		t.Fatalf("unless frags: %v", frags)
	}
}
