package writ

// TokenKind classifies a source token for highlighting.
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
	default:
		return "invalid"
	}
}

// Token is a highlighting token. Text is the exact source slice.
type Token struct {
	Kind TokenKind
	Text string
}

func isWS(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

func isAtomStop(c byte) bool {
	return isWS(c) || c == '(' || c == ')' || c == ';' || c == '[' || c == ']' || c == '\'' || c == ',' || c == '@' || c == '`'
}

func isNumLit(word string) bool {
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

func isIntLit(word string) bool {
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

func isSlotTok(word string) bool {
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

// Tokenize splits src into highlighting tokens.
func Tokenize(src string) []Token {
	out := make([]Token, 0, 16)
	i := 0
	n := len(src)
	push := func(kind TokenKind, text string) {
		out = append(out, Token{Kind: kind, Text: text})
	}
	for i < n {
		c := src[i]
		if isWS(c) {
			j := i + 1
			for j < n && isWS(src[j]) {
				j++
			}
			push(TokWS, src[i:j])
			i = j
			continue
		}
		if c == ';' {
			j := i + 1
			for j < n && src[j] != '\n' {
				j++
			}
			push(TokComment, src[i:j])
			i = j
			continue
		}
		if c == '(' || c == ')' || c == '[' || c == ']' {
			push(TokParen, src[i:i+1])
			i++
			continue
		}
		if c == '\'' || c == ',' || c == '@' {
			push(TokKeyword, src[i:i+1])
			i++
			continue
		}
		if c == '`' {
			j := i + 1
			for j < n {
				ch := src[j]
				if ch == '\\' && j+1 < n {
					j += 2
					continue
				}
				j++
				if ch == '`' {
					break
				}
			}
			push(TokSymbol, src[i:j])
			i = j
			continue
		}
		if c == '"' {
			j := i + 1
			for j < n {
				ch := src[j]
				if ch == '\\' && j+1 < n {
					j += 2
					continue
				}
				j++
				if ch == '"' {
					break
				}
			}
			push(TokString, src[i:j])
			i = j
			continue
		}
		j := i + 1
		for j < n && !isAtomStop(src[j]) {
			j++
		}
		word := src[i:j]
		switch {
		case isNumLit(word):
			push(TokNumber, word)
		case isSlotTok(word):
			push(TokKeyword, word)
		case len(word) > 1 && word[len(word)-1] == ':':
			push(TokKeyword, word)
		case isKeyword(word):
			push(TokKeyword, word)
		case isBuiltinName(word):
			push(TokBuiltin, word)
		default:
			push(TokSymbol, word)
		}
		i = j
	}
	return out
}
