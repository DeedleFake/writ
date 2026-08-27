package writ

import (
	"strconv"
	"strings"
)

type parser struct {
	s string
	i int
}

// Parse reads src into top-level forms. Comments are included.
func Parse(src string) ([]Value, error) {
	p := parser{s: src}
	var forms []Value
	nl := p.skipCount()
	for p.i < len(p.s) {
		form, err := p.read()
		if err != nil {
			return nil, err
		}
		if nl >= 2 {
			form.blank = true
		}
		forms = append(forms, form)
		nl = p.skipCount()
	}
	return forms, nil
}

func (p *parser) skipCount() int {
	nl := 0
	for p.i < len(p.s) && isWS(p.s[p.i]) {
		c := p.s[p.i]
		if c == '\n' {
			nl++
		} else if c == '\r' {
			nl++
			if p.i+1 < len(p.s) && p.s[p.i+1] == '\n' {
				p.i++
			}
		}
		p.i++
	}
	return nl
}

func (p *parser) skip() {
	for p.i < len(p.s) && isWS(p.s[p.i]) {
		p.i++
	}
}

func (p *parser) skipH() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
		p.i++
	}
}

func (p *parser) readComment() Value {
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != '\n' {
		p.i++
	}
	return Value{k: KindComment, s: p.s[start:p.i]}.withSpan(start, p.i)
}

func (p *parser) attachTrail(v Value) Value {
	if v.k == KindComment {
		return v
	}
	p.skipH()
	if p.i >= len(p.s) || p.s[p.i] != ';' {
		return v
	}
	start := p.i
	for p.i < len(p.s) && p.s[p.i] != '\n' {
		p.i++
	}
	return v.withCmt(p.s[start:p.i])
}

func (p *parser) readDelimited(close byte) (Value, error) {
	open := p.i
	p.i++
	var xs []Value
	for {
		p.skip()
		if p.i >= len(p.s) {
			return Value{}, errAt(open, p.i, "missing "+string(close))
		}
		ch := p.s[p.i]
		if ch == close {
			p.i++
			break
		}
		if ch == ')' || ch == ']' {
			return Value{}, errAt(p.i, p.i+1, "unexpected "+string(ch))
		}
		x, err := p.read()
		if err != nil {
			return Value{}, err
		}
		xs = append(xs, x)
	}
	var inner Value
	if close == ']' {
		var err error
		inner, err = finishBracket(xs)
		if err != nil {
			return Value{}, err
		}
	} else {
		inner = CallList(xs...)
		inner.xs = xs
	}
	form := p.attachTrail(inner.withSpan(open, p.i))
	if strings.Contains(p.s[open:p.i], "\n") {
		form.broke = true
	}
	return form, nil
}

func finishBracket(xs []Value) (Value, error) {
	items := filterComments(xs)
	if len(items) == 1 && isSymName(items[0], ":") {
		return EmptyMap(), nil
	}
	var pairs []MapPair
	keySpans := map[string]Span{}
	i := 0
	keys := 0
	for i < len(items) {
		a := items[i]
		if a.isKeySym() {
			keys++
			if i+1 >= len(items) || items[i+1].isKeySym() {
				return Value{}, errMsg("map key needs a value")
			}
			name := a.keyName()
			pairs = append(pairs, MapPair{Key: name, Value: items[i+1]})
			if a.hasSpan {
				keySpans[name] = a.span
			}
			i += 2
		} else {
			i++
		}
	}
	if keys == 0 {
		return Value{k: KindList, xs: xs, vec: true}, nil
	}
	if keys*2 != len(items) {
		return Value{}, errMsg("a map cannot mix keys and other items")
	}
	m := MapFrom(pairs...)
	if len(keySpans) > 0 {
		m.keySpans = keySpans
	}
	return m, nil
}

func (p *parser) read() (Value, error) {
	p.skip()
	if p.i >= len(p.s) {
		start := p.i - 1
		if start < 0 {
			start = 0
		}
		return Value{}, errAt(start, p.i, "unexpected end of script")
	}
	start := p.i
	c := p.s[p.i]
	if c == ';' {
		return p.readComment(), nil
	}
	if c == '(' {
		return p.readDelimited(')')
	}
	if c == '[' {
		return p.readDelimited(']')
	}
	if c == ')' {
		return Value{}, errAt(p.i, p.i+1, "unexpected )")
	}
	if c == ']' {
		return Value{}, errAt(p.i, p.i+1, "unexpected ]")
	}
	if c == '\'' || c == ',' || c == '@' {
		p.i++
		inner, err := p.read()
		if err != nil {
			return Value{}, err
		}
		var node Value
		switch c {
		case '\'':
			node = Quote(inner)
		case ',':
			node = Unquote(inner)
		default:
			node = Splice(inner)
		}
		end := p.i
		if inner.hasSpan {
			end = inner.span.End
		}
		return p.attachTrail(node.withSpan(start, end)), nil
	}
	if c == '`' {
		p.i++
		var out strings.Builder
		for p.i < len(p.s) {
			ch := p.s[p.i]
			if ch == '`' {
				p.i++
				if out.Len() == 0 {
					return Value{}, errAt(start, p.i, "empty symbol")
				}
				return p.attachTrail(Value{k: KindSymbol, s: out.String()}.withSpan(start, p.i)), nil
			}
			if ch == '\\' && p.i+1 < len(p.s) {
				out.WriteByte(p.s[p.i+1])
				p.i += 2
				continue
			}
			out.WriteByte(ch)
			p.i++
		}
		return Value{}, errAt(start, p.i, "unterminated symbol")
	}
	if c == '"' {
		p.i++
		var out strings.Builder
		for p.i < len(p.s) {
			ch := p.s[p.i]
			if ch == '"' {
				p.i++
				return p.attachTrail(String(out.String()).withSpan(start, p.i)), nil
			}
			if ch == '\\' && p.i+1 < len(p.s) {
				n := p.s[p.i+1]
				switch n {
				case 'n':
					out.WriteByte('\n')
				case 't':
					out.WriteByte('\t')
				default:
					out.WriteByte(n)
				}
				p.i += 2
				continue
			}
			out.WriteByte(ch)
			p.i++
		}
		return Value{}, errAt(start, p.i, "unterminated string")
	}
	j := p.i + 1
	for j < len(p.s) && !isAtomStop(p.s[j]) {
		j++
	}
	word := p.s[p.i:j]
	p.i = j
	if word == "true" || word == "false" || word == "nil" {
		return p.attachTrail(Value{k: KindSymbol, s: word}.withSpan(start, j)), nil
	}
	if isNumLit(word) {
		if isIntLit(word) {
			n, ok := intFromString(word)
			if !ok {
				return Value{}, errAt(start, j, "invalid number")
			}
			return p.attachTrail(n.withSpan(start, j)), nil
		}
		f, err := strconv.ParseFloat(word, 64)
		if err != nil {
			return Value{}, errAt(start, j, "invalid number")
		}
		return p.attachTrail(Float(f).withSpan(start, j)), nil
	}
	if word == "" {
		return Value{}, errAt(start, j, "empty token")
	}
	return p.attachTrail(Value{k: KindSymbol, s: word}.withSpan(start, j)), nil
}
