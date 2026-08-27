//go:build !js && !wasm

package repl

import (
	"strings"
	"testing"
)

func TestCompleteWord(t *testing.T) {
	head, comps, tail := completeWord("(de", 3)
	if head != "(" || tail != "" {
		t.Fatalf("head %q tail %q", head, tail)
	}
	joined := strings.Join(comps, ",")
	if !strings.Contains(joined, "def") || !strings.Contains(joined, "defm") {
		t.Fatalf("completions %q", joined)
	}
}
