package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func buildCLI(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "writ")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build cli: %v\n%s", err, out)
	}
	return bin
}

func TestCLIRunFmtCheck(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.writ")
	if err := os.WriteFile(lib, []byte("(def (n) 3)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "hello.writ")
	if err := os.WriteFile(src, []byte("(def (main) (print ((get (import \"lib.writ\") \"n\"))))\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := exec.Command(bin, "run", src)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "3") {
		t.Fatalf("run out %q", out)
	}

	fmtCmd := exec.Command(bin, "fmt", src)
	out, err = fmtCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fmt: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "def") {
		t.Fatalf("fmt %q", out)
	}

	check := exec.Command(bin, "check", src)
	if err := check.Run(); err != nil {
		t.Fatalf("check: %v", err)
	}

	bad := filepath.Join(dir, "bad.writ")
	if err := os.WriteFile(bad, []byte("(+ 1 \"x\")\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	checkBad := exec.Command(bin, "check", bad)
	out, err = checkBad.CombinedOutput()
	if err == nil {
		t.Fatal("check should fail")
	}
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() == 0 {
		t.Fatalf("exit: %v", err)
	}
	if !strings.Contains(string(out), ":") {
		t.Fatalf("want line:col in %q", out)
	}
}

func TestFmtWriteAndSymlink(t *testing.T) {
	bin := buildCLI(t)
	dir := t.TempDir()
	f := filepath.Join(dir, "a.writ")
	src := "(+ 1   2)\n"
	if err := os.WriteFile(f, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(bin, "fmt", f).CombinedOutput()
	if err != nil {
		t.Fatalf("fmt stdout: %v\n%s", err, out)
	}
	want := string(out)
	if err := exec.Command(bin, "fmt", "-w", f).Run(); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("fmt -w file %q want %q", got, want)
	}
	link := filepath.Join(dir, "link.writ")
	if err := os.Symlink(f, link); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	out, err = exec.Command(bin, "fmt", "-w", link).CombinedOutput()
	if err == nil {
		t.Fatal("fmt -w symlink should fail")
	}
	if !strings.Contains(string(out), "symlink") {
		t.Fatalf("want symlink in %q", out)
	}
	after, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("symlink write changed target")
	}
}

func TestOffsetLineCol(t *testing.T) {
	src := "a\nbc"
	line, col := offsetLineCol(src, 3) // 'c' is index 3
	if line != 2 || col != 2 {
		t.Fatalf("got %d:%d", line, col)
	}
}
