package repl

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
)

func runRepl(t *testing.T, in string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errb bytes.Buffer
	err = (REPL{In: strings.NewReader(in), Out: &out, Err: &errb}).Run(context.Background())
	return out.String(), errb.String(), err
}

func stripPrompts(s string) string {
	s = strings.ReplaceAll(s, prompt, "")
	s = strings.ReplaceAll(s, contPrompt, "")
	return s
}

func TestRunAdd(t *testing.T) {
	out, errb, err := runRepl(t, "(+ 1 2)\n")
	if err != nil {
		t.Fatal(err)
	}
	if errb != "" {
		t.Fatalf("stderr %q", errb)
	}
	if strings.TrimSpace(stripPrompts(out)) != "3" {
		t.Fatalf("out %q", out)
	}
}

func TestRunDefThenCall(t *testing.T) {
	out, errb, err := runRepl(t, "(def (inc n) (+ n 1))\n(inc 4)\n")
	if err != nil {
		t.Fatal(err)
	}
	if errb != "" {
		t.Fatalf("stderr %q", errb)
	}
	if !strings.Contains(stripPrompts(out), "5") {
		t.Fatalf("out %q", out)
	}
}

func TestRunMultiline(t *testing.T) {
	out, errb, err := runRepl(t, "(\n+ 1 2)\n")
	if err != nil {
		t.Fatal(err)
	}
	if errb != "" {
		t.Fatalf("stderr %q", errb)
	}
	if strings.TrimSpace(stripPrompts(out)) != "3" {
		t.Fatalf("out %q", out)
	}
}

func TestRunBadParseContinues(t *testing.T) {
	out, errb, err := runRepl(t, ")\n(+ 1 2)\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errb, "unexpected") {
		t.Fatalf("stderr %q", errb)
	}
	if !strings.Contains(stripPrompts(out), "3") {
		t.Fatalf("out %q", out)
	}
}

func TestRunEvalErrorContinues(t *testing.T) {
	out, errb, err := runRepl(t, "(+ 1 \"x\")\n(+ 1 2)\n")
	if err != nil {
		t.Fatal(err)
	}
	if errb == "" {
		t.Fatal("expected eval error")
	}
	if !strings.Contains(stripPrompts(out), "3") {
		t.Fatalf("out %q stderr %q", out, errb)
	}
}

func TestRunEOF(t *testing.T) {
	_, errb, err := runRepl(t, "")
	if err != nil {
		t.Fatal(err)
	}
	if errb != "" {
		t.Fatalf("stderr %q", errb)
	}
}

func TestRunEOFIncomplete(t *testing.T) {
	_, errb, err := runRepl(t, "(")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errb, "incomplete") {
		t.Fatalf("stderr %q", errb)
	}
}

func TestRunCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := (REPL{
		In:  strings.NewReader("(+ 1 2)\n"),
		Out: io.Discard,
		Err: io.Discard,
	}).Run(ctx)
	if err != context.Canceled {
		t.Fatalf("got %v", err)
	}
}
