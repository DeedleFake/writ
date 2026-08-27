//go:build !js && !wasm

package repl

import (
	"strings"
	"testing"
)

func TestWordCompleter(t *testing.T) {
	c := wordCompleter{words: []string{"+", "def", "defm"}}
	got, n := c.Do([]rune("(de"), 3)
	if n != 2 {
		t.Fatalf("offset %d", n)
	}
	var s []string
	for _, g := range got {
		s = append(s, string(g))
	}
	joined := strings.Join(s, ",")
	if !strings.Contains(joined, "f") || !strings.Contains(joined, "fm") {
		t.Fatalf("completions %q", joined)
	}
}
