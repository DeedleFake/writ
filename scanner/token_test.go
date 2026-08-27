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
