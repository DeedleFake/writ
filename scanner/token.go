package scanner

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// TokenKind classifies a source token.
type TokenKind int

const (
	TokWS TokenKind = iota
	TokComment
	TokParen
	TokString
	TokNumber
	TokKeyword
	TokBuiltin
	TokSymbol
	TokLParen
	TokRParen
	TokLBracket
	TokRBracket
	TokQuote
	TokUnquote
	TokSplice
	TokTick
	TokEOF
)

func (k TokenKind) String() string {
	switch k {
	case TokWS:
		return "ws"
	case TokComment:
		return "comment"
	case TokParen:
		return "paren"
	case TokString:
		return "string"
	case TokNumber:
		return "number"
	case TokKeyword:
		return "keyword"
	case TokBuiltin:
		return "builtin"
	case TokSymbol:
		return "symbol"
	case TokLParen:
		return "("
	case TokRParen:
		return ")"
	case TokLBracket:
		return "["
	case TokRBracket:
		return "]"
	case TokQuote:
		return "'"
	case TokUnquote:
		return ","
	case TokSplice:
		return "@"
	case TokTick:
		return "tick"
	case TokEOF:
		return "eof"
	default:
		return "invalid"
	}
}

// Token is a source token. Text is the exact source bytes.
type Token struct {
	Kind  TokenKind
	Text  string
	Start int
	End   int
}

func isWS(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func isAtomStop(c byte) bool {
	return isWS(c) || c == '(' || c == ')' || c == ';' || c == '[' || c == ']' || c == '\'' || c == ',' || c == '@' || c == '`'
}

// IsNumLit reports whether word is an integer or decimal float literal.
func IsNumLit(word string) bool {
	if word == "" {
		return false
	}
	i := 0
	if word[0] == '+' || word[0] == '-' {
		if len(word) == 1 {
			return false
		}
		i = 1
	}
	digits := 0
	for i < len(word) && word[i] >= '0' && word[i] <= '9' {
		digits++
		i++
	}
	if digits == 0 {
		return false
	}
	if i == len(word) {
		return true
	}
	if word[i] != '.' {
		return false
	}
	i++
	frac := 0
	for i < len(word) && word[i] >= '0' && word[i] <= '9' {
		frac++
		i++
	}
	return frac > 0 && i == len(word)
}

// IsIntLit reports whether word is a signed integer literal.
func IsIntLit(word string) bool {
	if word == "" {
		return false
	}
	i := 0
	if word[0] == '+' || word[0] == '-' {
		if len(word) == 1 {
			return false
		}
		i = 1
	}
	if i >= len(word) {
		return false
	}
	for ; i < len(word); i++ {
		if word[i] < '0' || word[i] > '9' {
			return false
		}
	}
	return true
}

// IsSlot reports whether word is a short-fn slot (#1, #2, …).
func IsSlot(word string) bool {
	if len(word) < 2 || word[0] != '#' {
		return false
	}
	if word[1] < '1' || word[1] > '9' {
		return false
	}
	for i := 2; i < len(word); i++ {
		if word[i] < '0' || word[i] > '9' {
			return false
		}
	}
	return true
}

// Scanner reads tokens from a source stream.
type Scanner struct {
	br        *bufio.Reader
	off       int
	highlight bool
	hasPeek   bool
	peekTok   Token
	peekErr   error
	done      bool
	ioErr     error
}

// New returns a scanner over r. Highlighting is off (parser kinds).
func New(r io.Reader) *Scanner {
	return &Scanner{br: bufio.NewReader(r)}
}

// SetHighlight remaps token kinds for editors. Spans, Text, and token
// count are unchanged.
func (s *Scanner) SetHighlight(on bool) {
	s.highlight = on
}

// Next returns the next token. At end of input it returns TokEOF.
// Other errors are from the reader. After TokEOF, later Next calls
// may return TokEOF again.
func (s *Scanner) Next() (Token, error) {
	t, err := s.nextRaw()
	if err != nil {
		return Token{}, err
	}
	return s.out(t), nil
}

// Peek returns the next token without consuming it.
func (s *Scanner) Peek() (Token, error) {
	if !s.hasPeek {
		s.peekTok, s.peekErr = s.readToken()
		s.hasPeek = true
	}
	if s.peekErr != nil {
		return Token{}, s.peekErr
	}
	return s.out(s.peekTok), nil
}

func (s *Scanner) nextRaw() (Token, error) {
	if s.hasPeek {
		s.hasPeek = false
		t, err := s.peekTok, s.peekErr
		s.peekTok = Token{}
		s.peekErr = nil
		return t, err
	}
	return s.readToken()
}

func (s *Scanner) out(t Token) Token {
	if s.highlight {
		return highlightKind(t)
	}
	return t
}

func highlightKind(t Token) Token {
	switch t.Kind {
	case TokLParen, TokRParen, TokLBracket, TokRBracket:
		t.Kind = TokParen
	case TokQuote, TokUnquote, TokSplice:
		t.Kind = TokKeyword
	case TokTick:
		t.Kind = TokSymbol
	case TokSymbol:
		word := t.Text
		switch {
		case IsSlot(word):
			t.Kind = TokKeyword
		case len(word) > 1 && word[len(word)-1] == ':':
			t.Kind = TokKeyword
		case IsKeyword(word):
			t.Kind = TokKeyword
		case IsBuiltin(word):
			t.Kind = TokBuiltin
		default:
			t.Kind = TokSymbol
		}
	}
	return t
}

func (s *Scanner) readByte() (byte, error) {
	if s.ioErr != nil {
		return 0, s.ioErr
	}
	b, err := s.br.ReadByte()
	if err != nil {
		if !errors.Is(err, io.EOF) {
			s.ioErr = err
		}
		return 0, err
	}
	s.off++
	return b, nil
}

func (s *Scanner) peekByte() (byte, error) {
	if s.ioErr != nil {
		return 0, s.ioErr
	}
	buf, err := s.br.Peek(1)
	if err != nil {
		if !errors.Is(err, io.EOF) {
			s.ioErr = err
		}
		return 0, err
	}
	return buf[0], nil
}

func (s *Scanner) eofTok() Token {
	return Token{Kind: TokEOF, Start: s.off, End: s.off}
}

func (s *Scanner) tok(kind TokenKind, start int, text string) Token {
	return Token{Kind: kind, Text: text, Start: start, End: s.off}
}

func (s *Scanner) readToken() (Token, error) {
	if s.ioErr != nil {
		return Token{}, s.ioErr
	}
	if s.done {
		return s.eofTok(), nil
	}
	start := s.off
	c, err := s.readByte()
	if errors.Is(err, io.EOF) {
		s.done = true
		return s.eofTok(), nil
	}
	if err != nil {
		return Token{}, err
	}
	if isWS(c) {
		return s.scanRun(TokWS, start, c, isWS)
	}
	if c == ';' {
		return s.scanComment(start, c)
	}
	switch c {
	case '(':
		return s.tok(TokLParen, start, "("), nil
	case ')':
		return s.tok(TokRParen, start, ")"), nil
	case '[':
		return s.tok(TokLBracket, start, "["), nil
	case ']':
		return s.tok(TokRBracket, start, "]"), nil
	case '\'':
		return s.tok(TokQuote, start, "'"), nil
	case ',':
		return s.tok(TokUnquote, start, ","), nil
	case '@':
		return s.tok(TokSplice, start, "@"), nil
	case '`':
		text, err := s.scanQuoted(c, '`')
		if err != nil {
			return Token{}, err
		}
		return s.tok(TokTick, start, text), nil
	case '"':
		text, err := s.scanQuoted(c, '"')
		if err != nil {
			return Token{}, err
		}
		return s.tok(TokString, start, text), nil
	}
	text, err := s.scanAtom(c)
	if err != nil {
		return Token{}, err
	}
	kind := TokSymbol
	if IsNumLit(text) {
		kind = TokNumber
	}
	return s.tok(kind, start, text), nil
}

func (s *Scanner) scanRun(kind TokenKind, start int, first byte, keep func(byte) bool) (Token, error) {
	var b strings.Builder
	b.WriteByte(first)
	for {
		p, err := s.peekByte()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Token{}, err
		}
		if !keep(p) {
			break
		}
		c, err := s.readByte()
		if err != nil {
			return Token{}, err
		}
		b.WriteByte(c)
	}
	return s.tok(kind, start, b.String()), nil
}

func (s *Scanner) scanComment(start int, first byte) (Token, error) {
	var b strings.Builder
	b.WriteByte(first)
	for {
		p, err := s.peekByte()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Token{}, err
		}
		if p == '\n' {
			break
		}
		c, err := s.readByte()
		if err != nil {
			return Token{}, err
		}
		b.WriteByte(c)
	}
	return s.tok(TokComment, start, b.String()), nil
}

func (s *Scanner) scanQuoted(first, quote byte) (string, error) {
	var b strings.Builder
	b.WriteByte(first)
	for {
		c, err := s.readByte()
		if errors.Is(err, io.EOF) {
			return b.String(), nil
		}
		if err != nil {
			return "", err
		}
		b.WriteByte(c)
		if c == '\\' {
			n, err := s.readByte()
			if errors.Is(err, io.EOF) {
				return b.String(), nil
			}
			if err != nil {
				return "", err
			}
			b.WriteByte(n)
			continue
		}
		if c == quote {
			return b.String(), nil
		}
	}
}

func (s *Scanner) scanAtom(first byte) (string, error) {
	var b strings.Builder
	b.WriteByte(first)
	for {
		p, err := s.peekByte()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", err
		}
		if isAtomStop(p) {
			break
		}
		c, err := s.readByte()
		if err != nil {
			return "", err
		}
		b.WriteByte(c)
	}
	return b.String(), nil
}
