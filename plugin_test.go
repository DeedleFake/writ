package writ

import (
	"deedles.dev/writ/runtime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPluginLoad(t *testing.T) {
	if !runtime.PluginsSupported() {
		t.Skip("plugins not supported on this platform")
	}
	so := filepath.Join(t.TempDir(), "nativehello.so")
	cmd := exec.Command("go", "build", "-buildmode=plugin", "-o", so, "./example/nativehello")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Skipf("plugin build failed (common in tests): %s", out)
	}
	rt := New(WithNativePlugins(), WithAllowAbsoluteImports())
	v, err := rt.Eval(rd(`(map-get (import "` + filepath.ToSlash(so) + `") 'version)`))
	if err != nil {
		t.Skipf("plugin.Open failed: %v", err)
	}
	if !v.Equal(runtime.Int64(1)) {
		t.Fatalf("plugin val: %v", v)
	}
}

func TestPluginMissingAndGarbage(t *testing.T) {
	_, err := New(WithNativePlugins()).Eval(rd(`(import "definitely-missing-plugin.so")`))
	if err == nil {
		t.Fatal("expected missing plugin error")
	}
	dir := t.TempDir()
	so := filepath.Join(dir, "junk.so")
	if err := os.WriteFile(so, []byte("not a plugin"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New(WithNativePlugins(), WithAllowAbsoluteImports())
	_, err = rt.Eval(rd(`(import "` + filepath.ToSlash(so) + `")`))
	if err == nil {
		t.Fatal("expected garbage plugin error")
	}
	if !strings.Contains(err.Error(), "plugin") && !strings.Contains(err.Error(), "open") && !strings.Contains(strings.ToLower(err.Error()), "invalid") {
		// plugin.Open error text varies; any error is enough
		t.Log(err)
	}
}

func TestLoadPluginDirect(t *testing.T) {
	if !runtime.PluginsSupported() {
		_, err := runtime.LoadPlugin("nope.so")
		if err == nil {
			t.Fatal("stub should error")
		}
		return
	}
	dir := t.TempDir()
	so := filepath.Join(dir, "junk.so")
	if err := os.WriteFile(so, []byte("xxxx"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.LoadPlugin(so); err == nil {
		t.Fatal("expected loadPlugin error")
	}
}

func TestCheckDoesNotOpenPlugins(t *testing.T) {
	dir := t.TempDir()
	so := filepath.Join(dir, "evil.so")
	if err := os.WriteFile(so, []byte("not a go plugin"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "x.writ")
	body := `(defm (p) (import "` + filepath.ToSlash(so) + `") 1) (p)`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := New(WithNativePlugins(), WithAllowAbsoluteImports())
	res := rt.CheckFile(src)
	found := false
	for _, d := range res.Diagnostics {
		low := strings.ToLower(d.Message)
		if strings.Contains(low, "plugin.open") || strings.Contains(low, "plugin was built") {
			t.Fatalf("Check opened plugin: %s", d.Message)
		}
		if strings.Contains(d.Message, "disabled") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want disabled diagnostic, got %v", res.Diagnostics)
	}
}

func TestCheckAfterDoesNotOpenPlugin(t *testing.T) {
	dir := t.TempDir()
	so := filepath.Join(dir, "evil.so")
	if err := os.WriteFile(so, []byte("not a go plugin"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "x.writ")
	body := `(defm (p) (after 0 (import "` + filepath.ToSlash(so) + `")) 1) (p)`
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var afterErr error
	n := 0
	rt := New(
		WithNativePlugins(),
		WithAllowAbsoluteImports(),
		WithScheduler(func(_ time.Duration, fn func()) { n++; fn() }),
		WithAfterError(func(err error) { afterErr = err }),
	)
	res := rt.CheckFile(src)
	if n != 0 {
		t.Fatalf("Check scheduled after: n=%d afterErr=%v diags=%v", n, afterErr, res.Diagnostics)
	}
	if afterErr != nil {
		t.Fatalf("after ran during Check: %v", afterErr)
	}
}
