package scanner

import (
	"errors"
	"io"
	"strings"
	"testing"
)

func collect(t *testing.T, src string, highlight bool) []Token {
	t.Helper()
	s := New(strings.NewReader(src))
	s.SetHighlight(highlight)
	var out []Token
	for {
		tok, err := s.Next()
		if err != nil {
			t.Fatalf("next: %v", err)
		}
		if tok.Kind == TokEOF {
			return out
		}
		out = append(out, tok)
	}
}

func TestHighlightKinds(t *testing.T) {
	toks := collect(t, "(def (f x) (+ x 1)) ; c\n", true)
	kinds := map[TokenKind]int{}
	for _, tok := range toks {
		kinds[tok.Kind]++
	}
	if kinds[TokKeyword] == 0 || kinds[TokBuiltin] == 0 || kinds[TokComment] == 0 {
		t.Fatalf("kinds: %v", kinds)
	}
	if kinds[TokLParen] != 0 || kinds[TokRParen] != 0 {
		t.Fatalf("highlight should remap parens: %v", kinds)
	}
}

func TestScanStructuralKinds(t *testing.T) {
	toks := collect(t, "(def [x] 'y ,z @xs `tick` + #1 k:)", false)
	kinds := map[TokenKind]int{}
	for _, tok := range toks {
		kinds[tok.Kind]++
	}
	if kinds[TokLParen] != 1 || kinds[TokRParen] != 1 || kinds[TokLBracket] != 1 || kinds[TokRBracket] != 1 {
		t.Fatalf("delims: %v", kinds)
	}
	if kinds[TokQuote] != 1 || kinds[TokUnquote] != 1 || kinds[TokSplice] != 1 || kinds[TokTick] != 1 {
		t.Fatalf("quotes: %v", kinds)
	}
	if kinds[TokParen] != 0 || kinds[TokKeyword] != 0 || kinds[TokBuiltin] != 0 {
		t.Fatalf("parser kinds: %v", kinds)
	}
}

func TestHighlightRemapAndSpans(t *testing.T) {
	src := "(def (f x) (+ x 1)) ; c\n#1 k: `tick` 'x"
	raw := collect(t, src, false)
	hi := collect(t, src, true)
	if len(raw) != len(hi) {
		t.Fatalf("count raw %d hi %d", len(raw), len(hi))
	}
	for i := range raw {
		if raw[i].Text != hi[i].Text || raw[i].Start != hi[i].Start || raw[i].End != hi[i].End {
			t.Fatalf("span/text %d: %+v vs %+v", i, raw[i], hi[i])
		}
	}
	found := map[string]TokenKind{}
	for _, tok := range hi {
		found[tok.Text] = tok.Kind
	}
	if found["("] != TokParen || found[")"] != TokParen {
		t.Fatalf("paren kinds %v", found)
	}
	if found["'"] != TokKeyword || found["def"] != TokKeyword || found["+"] != TokBuiltin {
		t.Fatalf("kw/builtin %v", found)
	}
	if found["#1"] != TokKeyword || found["k:"] != TokKeyword {
		t.Fatalf("slot/key %v", found)
	}
	if found["`tick`"] != TokSymbol {
		t.Fatalf("tick %v", found)
	}
}

func TestPeekDoesNotConsume(t *testing.T) {
	s := New(strings.NewReader("(+ 1)"))
	a, err := s.Peek()
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Peek()
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("peek changed: %+v %+v", a, b)
	}
	c, err := s.Next()
	if err != nil {
		t.Fatal(err)
	}
	if c != a {
		t.Fatalf("next != peek: %+v %+v", c, a)
	}
	d, err := s.Peek()
	if err != nil {
		t.Fatal(err)
	}
	if d.Kind == a.Kind && d.Start == a.Start {
		t.Fatalf("peek after next still first token: %+v", d)
	}
}

func TestPeekHighlight(t *testing.T) {
	s := New(strings.NewReader("(def + #1 k: `tick`)"))
	s.SetHighlight(true)
	want := []struct {
		kind TokenKind
		text string
	}{
		{TokParen, "("},
		{TokKeyword, "def"},
		{TokBuiltin, "+"},
		{TokKeyword, "#1"},
		{TokKeyword, "k:"},
		{TokSymbol, "`tick`"},
		{TokParen, ")"},
	}
	for _, w := range want {
		a, err := s.Peek()
		if err != nil {
			t.Fatal(err)
		}
		b, err := s.Peek()
		if err != nil {
			t.Fatal(err)
		}
		c, err := s.Next()
		if err != nil {
			t.Fatal(err)
		}
		if a != b || a != c {
			t.Fatalf("%q: peek %+v peek %+v next %+v", w.text, a, b, c)
		}
		if c.Kind != w.kind || c.Text != w.text {
			t.Fatalf("got %+v want %+v %q", c, w.kind, w.text)
		}
		if tok, err := s.Peek(); err != nil {
			t.Fatal(err)
		} else if tok.Kind == TokWS {
			if _, err := s.Next(); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestPeekEOF(t *testing.T) {
	s := New(strings.NewReader(""))
	a, err := s.Peek()
	if err != nil || a.Kind != TokEOF || a.Start != 0 || a.End != 0 {
		t.Fatalf("peek: %+v %v", a, err)
	}
	b, err := s.Peek()
	if err != nil || b != a {
		t.Fatalf("peek again: %+v %v", b, err)
	}
	c, err := s.Next()
	if err != nil || c != a {
		t.Fatalf("next: %+v %v", c, err)
	}
	d, err := s.Next()
	if err != nil || d.Kind != TokEOF || d.Start != 0 || d.End != 0 {
		t.Fatalf("next again: %+v %v", d, err)
	}
}

func TestHighlightAfterPeek(t *testing.T) {
	s := New(strings.NewReader("("))
	raw, err := s.Peek()
	if err != nil || raw.Kind != TokLParen {
		t.Fatalf("peek: %+v %v", raw, err)
	}
	s.SetHighlight(true)
	hi, err := s.Peek()
	if err != nil || hi.Kind != TokParen || hi.Start != raw.Start || hi.End != raw.End || hi.Text != raw.Text {
		t.Fatalf("peek hi: %+v %v", hi, err)
	}
	next, err := s.Next()
	if err != nil || next != hi {
		t.Fatalf("next: %+v %v", next, err)
	}
}

func TestEOF(t *testing.T) {
	s := New(strings.NewReader(""))
	tok, err := s.Next()
	if err != nil || tok.Kind != TokEOF || tok.Start != 0 || tok.End != 0 {
		t.Fatalf("empty: %+v %v", tok, err)
	}
	tok2, err := s.Next()
	if err != nil || tok2.Kind != TokEOF {
		t.Fatalf("again: %+v %v", tok2, err)
	}

	src := "(+ 1)"
	s = New(strings.NewReader(src))
	for {
		tok, err = s.Next()
		if err != nil {
			t.Fatal(err)
		}
		if tok.Kind == TokEOF {
			break
		}
	}
	if tok.Start != len(src) || tok.End != len(src) {
		t.Fatalf("eof pos %+v want %d", tok, len(src))
	}
	tok, err = s.Next()
	if err != nil || tok.Kind != TokEOF || tok.Start != len(src) {
		t.Fatalf("eof again %+v %v", tok, err)
	}
}

func TestUnterminatedStillTokens(t *testing.T) {
	for _, src := range []string{`"abc`, "`abc", `"abc\`, "`ab\\"} {
		toks := collect(t, src, false)
		if len(toks) != 1 {
			t.Fatalf("%q: %v", src, toks)
		}
		if toks[0].Kind != TokString && toks[0].Kind != TokTick {
			t.Fatalf("%q kind %v", src, toks[0].Kind)
		}
		if toks[0].Text != src {
			t.Fatalf("%q text %q", src, toks[0].Text)
		}
	}
}

type failReader struct{ err error }

func (f failReader) Read([]byte) (int, error) { return 0, f.err }

func TestScanIOError(t *testing.T) {
	boom := errors.New("boom")
	s := New(failReader{err: boom})
	_, err := s.Peek()
	if !errors.Is(err, boom) {
		t.Fatalf("peek: %v", err)
	}
	_, err = s.Next()
	if !errors.Is(err, boom) {
		t.Fatalf("next: %v", err)
	}
}

func TestScanPartialThenIOError(t *testing.T) {
	boom := errors.New("boom")
	r := io.MultiReader(strings.NewReader("("), failReader{err: boom})
	s := New(r)
	tok, err := s.Next()
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if tok.Kind != TokLParen {
		t.Fatalf("first tok %+v", tok)
	}
	_, err = s.Next()
	if !errors.Is(err, boom) {
		t.Fatalf("second: %v", err)
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
	if !IsKeyword(".") {
		t.Fatal(". should be a keyword")
	}
}
