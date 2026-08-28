package parser

import (
	"errors"
	"strconv"
	"strings"

	"deedles.dev/writ/runtime"
	"deedles.dev/writ/scanner"
)

type parser struct {
	src  string
	toks []scanner.Token
	i    int
}

func (p *parser) tok() scanner.Token {
	if p.i >= len(p.toks) {
		n := len(p.src)
		return scanner.Token{Kind: scanner.TokEOF, Start: n, End: n}
	}
	return p.toks[p.i]
}

func (p *parser) pos() int {
	return p.tok().Start
}

func (p *parser) endPos() int {
	if p.i <= 0 {
		return 0
	}
	return p.toks[p.i-1].End
}

func (p *parser) advance() { p.i++ }

// Parse reads src into top-level forms. Comments are included.
func Parse(src string) ([]runtime.Value, error) {
	p := parser{src: src, toks: scanner.Scan(src)}
	var forms []runtime.Value
	nl := p.skipCount()
	for p.tok().Kind != scanner.TokEOF {
		form, err := p.read()
		if err != nil {
			return nil, err
		}
		if nl >= 2 {
			form = form.WithBlank()
		}
		forms = append(forms, form)
		nl = p.skipCount()
	}
	return forms, nil
}

// Incomplete reports whether a [Parse] error means src needs more input
// (unclosed list, map, string, tick symbol, or quote).
func Incomplete(err error) bool {
	var e *runtime.Error
	return errors.As(err, &e) && e.IsIncomplete()
}

func (p *parser) skipCount() int {
	nl := 0
	for p.tok().Kind == scanner.TokWS {
		nl += countNL(p.tok().Text)
		p.advance()
	}
	return nl
}

func countNL(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			n++
		} else if s[i] == '\r' {
			n++
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
		}
	}
	return n
}

func (p *parser) skip() {
	for p.tok().Kind == scanner.TokWS {
		p.advance()
	}
}

func (p *parser) skipH() {
	t := p.tok()
	if t.Kind != scanner.TokWS {
		return
	}
	if strings.ContainsAny(t.Text, "\n\r") {
		return
	}
	p.advance()
}

func (p *parser) attachTrail(v runtime.Value) runtime.Value {
	if v.Kind() == runtime.KindComment {
		return v
	}
	p.skipH()
	t := p.tok()
	if t.Kind != scanner.TokComment {
		return v
	}
	p.advance()
	return v.WithComment(t.Text)
}

func (p *parser) read() (runtime.Value, error) {
	p.skip()
	t := p.tok()
	if t.Kind == scanner.TokEOF {
		start := max(p.endPos()-1, 0)
		return runtime.Value{}, runtime.ErrorIncomplete(start, p.pos(), "unexpected end of script")
	}
	switch t.Kind {
	case scanner.TokComment:
		p.advance()
		return runtime.Comment(t.Text).WithSpan(t.Start, t.End), nil
	case scanner.TokLParen:
		return p.readDelimited(scanner.TokRParen, ')')
	case scanner.TokLBracket:
		return p.readDelimited(scanner.TokRBracket, ']')
	case scanner.TokRParen:
		p.advance()
		return runtime.Value{}, runtime.ErrorAt(t.Start, t.End, "unexpected )")
	case scanner.TokRBracket:
		p.advance()
		return runtime.Value{}, runtime.ErrorAt(t.Start, t.End, "unexpected ]")
	case scanner.TokQuote, scanner.TokUnquote, scanner.TokSplice:
		p.advance()
		inner, err := p.read()
		if err != nil {
			return runtime.Value{}, err
		}
		var node runtime.Value
		switch t.Kind {
		case scanner.TokQuote:
			node = runtime.Quote(inner)
		case scanner.TokUnquote:
			node = runtime.Unquote(inner)
		default:
			node = runtime.Splice(inner)
		}
		end := p.endPos()
		if sp, ok := inner.Span(); ok {
			end = sp.End
		}
		return p.attachTrail(node.WithSpan(t.Start, end)), nil
	case scanner.TokTick:
		p.advance()
		name, err := unquoteTick(t)
		if err != nil {
			return runtime.Value{}, err
		}
		return p.attachTrail(runtime.Symbol(name).WithSpan(t.Start, t.End)), nil
	case scanner.TokString:
		p.advance()
		s, err := unquoteString(t)
		if err != nil {
			return runtime.Value{}, err
		}
		return p.attachTrail(runtime.String(s).WithSpan(t.Start, t.End)), nil
	case scanner.TokNumber:
		p.advance()
		word := t.Text
		if scanner.IsIntLit(word) {
			n, ok := runtime.ParseInt(word)
			if !ok {
				return runtime.Value{}, runtime.ErrorAt(t.Start, t.End, "invalid number")
			}
			return p.attachTrail(n.WithSpan(t.Start, t.End)), nil
		}
		f, err := strconv.ParseFloat(word, 64)
		if err != nil {
			return runtime.Value{}, runtime.ErrorAt(t.Start, t.End, "invalid number")
		}
		return p.attachTrail(runtime.Float(f).WithSpan(t.Start, t.End)), nil
	case scanner.TokSymbol:
		p.advance()
		word := t.Text
		if word == "" {
			return runtime.Value{}, runtime.ErrorAt(t.Start, t.End, "empty token")
		}
		atom, err := dottedForm(word, t.Start, t.End)
		if err != nil {
			return runtime.Value{}, err
		}
		return p.attachTrail(atom), nil
	default:
		p.advance()
		return runtime.Value{}, runtime.ErrorAt(t.Start, t.End, "empty token")
	}
}

func (p *parser) readDelimited(close scanner.TokenKind, closeCh byte) (runtime.Value, error) {
	open := p.tok()
	p.advance()
	var xs []runtime.Value
	for {
		p.skip()
		t := p.tok()
		if t.Kind == scanner.TokEOF {
			return runtime.Value{}, runtime.ErrorIncomplete(open.Start, p.pos(), "missing "+string(closeCh))
		}
		if t.Kind == close {
			p.advance()
			break
		}
		if t.Kind == scanner.TokRParen || t.Kind == scanner.TokRBracket {
			return runtime.Value{}, runtime.ErrorAt(t.Start, t.End, "unexpected "+t.Text)
		}
		x, err := p.read()
		if err != nil {
			return runtime.Value{}, err
		}
		xs = append(xs, x)
	}
	var inner runtime.Value
	if closeCh == ']' {
		var err error
		inner, err = finishBracket(xs)
		if err != nil {
			return runtime.Value{}, err
		}
	} else {
		inner = runtime.CallList(xs...)
	}
	end := p.endPos()
	form := p.attachTrail(inner.WithSpan(open.Start, end))
	if strings.Contains(p.src[open.Start:end], "\n") {
		form = form.WithBroke()
	}
	return form, nil
}

func finishBracket(xs []runtime.Value) (runtime.Value, error) {
	items := runtime.FilterComments(xs)
	if len(items) == 1 && runtime.IsName(items[0], ":") {
		return runtime.EmptyMap(), nil
	}
	var pairs []runtime.MapPair
	keySpans := map[string]runtime.Span{}
	i := 0
	keys := 0
	for i < len(items) {
		a := items[i]
		if a.IsKey() {
			keys++
			if i+1 >= len(items) || items[i+1].IsKey() {
				return runtime.Value{}, runtime.ErrorMsg("map key needs a value")
			}
			name := a.KeyName()
			pairs = append(pairs, runtime.MapPair{Key: runtime.Symbol(name), Value: items[i+1]})
			if sp, ok := a.Span(); ok {
				keySpans[name] = sp
			}
			i += 2
		} else {
			i++
		}
	}
	if keys == 0 {
		return runtime.List(xs...), nil
	}
	if keys*2 != len(items) {
		return runtime.Value{}, runtime.ErrorMsg("a map cannot mix keys and other items")
	}
	m := runtime.MapFrom(pairs...)
	if len(keySpans) > 0 {
		m = m.WithKeySpans(keySpans)
	}
	return m, nil
}

func dottedForm(word string, start, end int) (runtime.Value, error) {
	if word == "." || !strings.Contains(word, ".") {
		return runtime.Symbol(word).WithSpan(start, end), nil
	}
	parts := strings.Split(word, ".")
	if parts[0] == "" {
		return runtime.Value{}, runtime.ErrorAt(start, end, "dotted name cannot start with .")
	}
	if parts[len(parts)-1] == "" {
		return runtime.Value{}, runtime.ErrorAt(start, end, "dotted name cannot end with .")
	}
	xs := []runtime.Value{runtime.Symbol(".")}
	off := start
	for i, part := range parts {
		if part == "" {
			return runtime.Value{}, runtime.ErrorAt(start, end, "dotted name cannot contain empty field")
		}
		if i > 0 && scanner.IsNumLit(part) {
			return runtime.Value{}, runtime.ErrorAt(off, off+len(part), "dotted name field must be a name")
		}
		xs = append(xs, runtime.Symbol(part).WithSpan(off, off+len(part)))
		off += len(part) + 1
	}
	return runtime.CallList(xs...).WithSpan(start, end), nil
}

func unquoteTick(t scanner.Token) (string, error) {
	s := t.Text
	if len(s) < 2 || s[0] != '`' || s[len(s)-1] != '`' || !closedQuoted(s, '`') {
		return "", runtime.ErrorIncomplete(t.Start, t.End, "unterminated symbol")
	}
	inner := s[1 : len(s)-1]
	var out strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			out.WriteByte(inner[i+1])
			i++
			continue
		}
		out.WriteByte(inner[i])
	}
	if out.Len() == 0 {
		return "", runtime.ErrorAt(t.Start, t.End, "empty symbol")
	}
	return out.String(), nil
}

func unquoteString(t scanner.Token) (string, error) {
	s := t.Text
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' || !closedQuoted(s, '"') {
		return "", runtime.ErrorIncomplete(t.Start, t.End, "unterminated string")
	}
	inner := s[1 : len(s)-1]
	var out strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			n := inner[i+1]
			switch n {
			case 'n':
				out.WriteByte('\n')
			case 't':
				out.WriteByte('\t')
			default:
				out.WriteByte(n)
			}
			i++
			continue
		}
		out.WriteByte(inner[i])
	}
	return out.String(), nil
}

func closedQuoted(s string, quote byte) bool {
	if len(s) < 2 || s[0] != quote {
		return false
	}
	i := 1
	for i < len(s) {
		ch := s[i]
		if ch == '\\' && i+1 < len(s) {
			i += 2
			continue
		}
		i++
		if ch == quote {
			return i == len(s)
		}
	}
	return false
}
