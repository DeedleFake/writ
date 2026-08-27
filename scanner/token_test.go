package scanner

import "testing"

func TestTokenizeKinds(t *testing.T) {
	toks := Tokenize("(def (f x) (+ x 1)) ; c\n")
	kinds := map[TokenKind]int{}
	for _, tok := range toks {
		kinds[tok.Kind]++
	}
	if kinds[TokKeyword] == 0 || kinds[TokBuiltin] == 0 || kinds[TokComment] == 0 {
		t.Fatalf("kinds: %v", kinds)
	}
}

func TestWords(t *testing.T) {
	ws := Words()
	if len(ws) < 2 {
		t.Fatalf("words %v", ws)
	}
	hasDef, hasPlus := false, false
	for i, w := range ws {
		if i > 0 && w <= ws[i-1] {
			t.Fatalf("unsorted %v", ws)
		}
		if w == "def" {
			hasDef = true
		}
		if w == "+" {
			hasPlus = true
		}
	}
	if !hasDef || !hasPlus {
		t.Fatalf("words %v", ws)
	}
}
