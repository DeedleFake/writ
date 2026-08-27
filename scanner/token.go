package scanner

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

// Token is a source token. Text is the exact source slice.
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

func scanQuoted(src string, i int, quote byte) (end int) {
	j := i + 1
	n := len(src)
	for j < n {
		ch := src[j]
		if ch == '\\' && j+1 < n {
			j += 2
			continue
		}
		j++
		if ch == quote {
			break
		}
	}
	return j
}

// Scan splits src into tokens with positions. It does not fail on
// unterminated strings or ticks; the parser reports those.
func Scan(src string) []Token {
	out := make([]Token, 0, 16)
	i := 0
	n := len(src)
	push := func(kind TokenKind, start, end int) {
		out = append(out, Token{Kind: kind, Text: src[start:end], Start: start, End: end})
	}
	for i < n {
		c := src[i]
		if isWS(c) {
			j := i + 1
			for j < n && isWS(src[j]) {
				j++
			}
			push(TokWS, i, j)
			i = j
			continue
		}
		if c == ';' {
			j := i + 1
			for j < n && src[j] != '\n' {
				j++
			}
			push(TokComment, i, j)
			i = j
			continue
		}
		if c == '(' {
			push(TokLParen, i, i+1)
			i++
			continue
		}
		if c == ')' {
			push(TokRParen, i, i+1)
			i++
			continue
		}
		if c == '[' {
			push(TokLBracket, i, i+1)
			i++
			continue
		}
		if c == ']' {
			push(TokRBracket, i, i+1)
			i++
			continue
		}
		if c == '\'' {
			push(TokQuote, i, i+1)
			i++
			continue
		}
		if c == ',' {
			push(TokUnquote, i, i+1)
			i++
			continue
		}
		if c == '@' {
			push(TokSplice, i, i+1)
			i++
			continue
		}
		if c == '`' {
			j := scanQuoted(src, i, '`')
			push(TokTick, i, j)
			i = j
			continue
		}
		if c == '"' {
			j := scanQuoted(src, i, '"')
			push(TokString, i, j)
			i = j
			continue
		}
		j := i + 1
		for j < n && !isAtomStop(src[j]) {
			j++
		}
		word := src[i:j]
		if IsNumLit(word) {
			push(TokNumber, i, j)
		} else {
			push(TokSymbol, i, j)
		}
		i = j
	}
	return out
}

// Tokenize splits src into highlighting tokens.
func Tokenize(src string) []Token {
	raw := Scan(src)
	out := make([]Token, 0, len(raw))
	for _, t := range raw {
		kind := t.Kind
		switch t.Kind {
		case TokLParen, TokRParen, TokLBracket, TokRBracket:
			kind = TokParen
		case TokQuote, TokUnquote, TokSplice:
			kind = TokKeyword
		case TokTick:
			kind = TokSymbol
		case TokSymbol:
			word := t.Text
			switch {
			case IsSlot(word):
				kind = TokKeyword
			case len(word) > 1 && word[len(word)-1] == ':':
				kind = TokKeyword
			case IsKeyword(word):
				kind = TokKeyword
			case IsBuiltin(word):
				kind = TokBuiltin
			default:
				kind = TokSymbol
			}
		}
		out = append(out, Token{Kind: kind, Text: t.Text, Start: t.Start, End: t.End})
	}
	return out
}
