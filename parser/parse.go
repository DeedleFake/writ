package parser

import (
	"errors"
	"io"
	"strconv"
	"strings"

	"deedles.dev/writ/runtime"
	"deedles.dev/writ/scanner"
	"deedles.dev/writ/syntax"
)

type parser struct {
	sc   *scanner.Scanner
	cur  scanner.Token
	last scanner.Token
	err  error
	open []bool
}

func (p *parser) tok() scanner.Token {
	return p.cur
}

func (p *parser) pos() int {
	return p.tok().Start
}

func (p *parser) endPos() int {
	return p.last.End
}

func (p *parser) peek() {
	if p.err != nil {
		return
	}
	t, err := p.sc.Peek()
	if err != nil {
		p.err = err
		return
	}
	p.cur = t
}

func (p *parser) advance() {
	if p.err != nil {
		return
	}
	t, err := p.sc.Next()
	if err != nil {
		p.err = err
		return
	}
	p.last = t
	p.noteBroke(t.Text)
	p.peek()
}

func (p *parser) noteBroke(text string) {
	if len(p.open) == 0 || !strings.Contains(text, "\n") {
		return
	}
	for i := range p.open {
		p.open[i] = true
	}
}

func withLex(v syntax.Form, t scanner.Token) syntax.Form {
	return v.WithSpan(t.Start, t.End).WithLexeme(t.Text)
}

// Parse reads r into top-level forms. Comments are included.
func Parse(r io.Reader) ([]syntax.Form, error) {
	return ParseScanner(scanner.New(r))
}

// ParseScanner reads top-level forms from sc. Highlight is turned off.
func ParseScanner(sc *scanner.Scanner) ([]syntax.Form, error) {
	sc.SetHighlight(false)
	p := &parser{sc: sc}
	p.peek()
	if p.err != nil {
		return nil, p.err
	}
	var forms []syntax.Form
	nl := p.skipCount()
	if p.err != nil {
		return nil, p.err
	}
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
		if p.err != nil {
			return nil, p.err
		}
	}
	return forms, nil
}

// Incomplete reports whether a [Parse] error means the input needs more
// data (unclosed list, map, string, tick symbol, or quote).
func Incomplete(err error) bool {
	var e *runtime.Error
	return errors.As(err, &e) && e.IsIncomplete()
}

func (p *parser) skipCount() int {
	nl := 0
	for p.err == nil && p.tok().Kind == scanner.TokWS {
		nl += countNL(p.tok().Text)
		p.advance()
	}
	return nl
}

func countNL(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			n++
		case '\r':
			n++
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
		}
	}
	return n
}

func (p *parser) skip() {
	for p.err == nil && p.tok().Kind == scanner.TokWS {
		p.advance()
	}
}

func (p *parser) skipH() {
	if p.err != nil {
		return
	}
	t := p.tok()
	if t.Kind != scanner.TokWS {
		return
	}
	if strings.ContainsAny(t.Text, "\n\r") {
		return
	}
	p.advance()
}

func (p *parser) attachTrail(v syntax.Form) syntax.Form {
	if v.Kind() == syntax.KindComment {
		return v
	}
	p.skipH()
	if p.err != nil {
		return v
	}
	t := p.tok()
	if t.Kind != scanner.TokComment {
		return v
	}
	p.advance()
	return v.WithComment(t.Text)
}

func (p *parser) trail(v syntax.Form) (syntax.Form, error) {
	v = p.attachTrail(v)
	if p.err != nil {
		return syntax.Form{}, p.err
	}
	return v, nil
}

func (p *parser) read() (syntax.Form, error) {
	p.skip()
	if p.err != nil {
		return syntax.Form{}, p.err
	}
	t := p.tok()
	if t.Kind == scanner.TokEOF {
		start := max(p.endPos()-1, 0)
		return syntax.Form{}, runtime.ErrorIncomplete(start, p.pos(), "unexpected end of script")
	}
	switch t.Kind {
	case scanner.TokComment:
		p.advance()
		if p.err != nil {
			return syntax.Form{}, p.err
		}
		return syntax.Comment(t.Text).WithSpan(t.Start, t.End), nil
	case scanner.TokLParen:
		return p.readDelimited(scanner.TokRParen, ')')
	case scanner.TokLBracket:
		return p.readDelimited(scanner.TokRBracket, ']')
	case scanner.TokRParen:
		p.advance()
		return syntax.Form{}, runtime.ErrorAt(t.Start, t.End, "unexpected )")
	case scanner.TokRBracket:
		p.advance()
		return syntax.Form{}, runtime.ErrorAt(t.Start, t.End, "unexpected ]")
	case scanner.TokQuote, scanner.TokUnquote, scanner.TokSplice:
		p.advance()
		if p.err != nil {
			return syntax.Form{}, p.err
		}
		inner, err := p.read()
		if err != nil {
			return syntax.Form{}, err
		}
		var node syntax.Form
		switch t.Kind {
		case scanner.TokQuote:
			node = syntax.Quote(inner)
		case scanner.TokUnquote:
			node = syntax.Unquote(inner)
		default:
			node = syntax.Splice(inner)
		}
		end := p.endPos()
		if sp, ok := inner.Span(); ok {
			end = sp.End
		}
		return p.trail(node.WithSpan(t.Start, end))
	case scanner.TokTick:
		p.advance()
		if p.err != nil {
			return syntax.Form{}, p.err
		}
		name, err := unquoteTick(t)
		if err != nil {
			return syntax.Form{}, err
		}
		return p.trail(withLex(syntax.Symbol(name), t))
	case scanner.TokString:
		p.advance()
		if p.err != nil {
			return syntax.Form{}, p.err
		}
		s, err := unquoteString(t)
		if err != nil {
			return syntax.Form{}, err
		}
		return p.trail(syntax.String(s).WithSpan(t.Start, t.End))
	case scanner.TokNumber:
		p.advance()
		if p.err != nil {
			return syntax.Form{}, p.err
		}
		word := t.Text
		if scanner.IsIntLit(word) {
			n, ok := syntax.ParseInt(word)
			if !ok {
				return syntax.Form{}, runtime.ErrorAt(t.Start, t.End, "invalid number")
			}
			return p.trail(withLex(n, t))
		}
		f, err := strconv.ParseFloat(word, 64)
		if err != nil {
			return syntax.Form{}, runtime.ErrorAt(t.Start, t.End, "invalid number")
		}
		return p.trail(withLex(syntax.Float(f), t))
	case scanner.TokSymbol:
		p.advance()
		if p.err != nil {
			return syntax.Form{}, p.err
		}
		word := t.Text
		if word == "" {
			return syntax.Form{}, runtime.ErrorAt(t.Start, t.End, "empty token")
		}
		atom, err := dottedForm(word, t.Start, t.End)
		if err != nil {
			return syntax.Form{}, err
		}
		if atom.Kind() == syntax.KindSymbol {
			atom = withLex(atom, t)
		}
		return p.trail(atom)
	default:
		p.advance()
		return syntax.Form{}, runtime.ErrorAt(t.Start, t.End, "empty token")
	}
}

func (p *parser) readDelimited(close scanner.TokenKind, closeCh byte) (syntax.Form, error) {
	open := p.tok()
	p.advance()
	if p.err != nil {
		return syntax.Form{}, p.err
	}
	idx := len(p.open)
	p.open = append(p.open, false)
	defer func() { p.open = p.open[:idx] }()
	var xs []syntax.Form
	for {
		p.skip()
		if p.err != nil {
			return syntax.Form{}, p.err
		}
		t := p.tok()
		if t.Kind == scanner.TokEOF {
			return syntax.Form{}, runtime.ErrorIncomplete(open.Start, p.pos(), "missing "+string(closeCh))
		}
		if t.Kind == close {
			p.advance()
			if p.err != nil {
				return syntax.Form{}, p.err
			}
			break
		}
		if t.Kind == scanner.TokRParen || t.Kind == scanner.TokRBracket {
			return syntax.Form{}, runtime.ErrorAt(t.Start, t.End, "unexpected "+t.Text)
		}
		x, err := p.read()
		if err != nil {
			return syntax.Form{}, err
		}
		xs = append(xs, x)
	}
	var inner syntax.Form
	if closeCh == ']' {
		var err error
		inner, err = finishBracket(xs)
		if err != nil {
			return syntax.Form{}, err
		}
	} else {
		inner = syntax.CallList(xs...)
	}
	end := p.endPos()
	form, err := p.trail(inner.WithSpan(open.Start, end))
	if err != nil {
		return syntax.Form{}, err
	}
	if p.open[idx] {
		form = form.WithBroke()
	}
	return form, nil
}

func finishBracket(xs []syntax.Form) (syntax.Form, error) {
	items := syntax.FilterComments(xs)
	if len(items) == 1 && syntax.IsName(items[0], ":") {
		return syntax.EmptyMap(), nil
	}
	var pairs []syntax.MapPair
	keySpans := map[string]syntax.Span{}
	i := 0
	keys := 0
	for i < len(items) {
		a := items[i]
		if a.IsKey() {
			keys++
			if i+1 >= len(items) || items[i+1].IsKey() {
				return syntax.Form{}, runtime.ErrorMsg("map key needs a value")
			}
			name := a.KeyName()
			pairs = append(pairs, syntax.MapPair{Key: syntax.Symbol(name), Value: items[i+1]})
			if sp, ok := a.Span(); ok {
				keySpans[name] = sp
			}
			i += 2
		} else {
			i++
		}
	}
	if keys == 0 {
		return syntax.List(xs...), nil
	}
	if keys*2 != len(items) {
		return syntax.Form{}, runtime.ErrorMsg("a map cannot mix keys and other items")
	}
	m := syntax.MapFrom(pairs...)
	if len(keySpans) > 0 {
		m = m.WithKeySpans(keySpans)
	}
	return m, nil
}

func dottedForm(word string, start, end int) (syntax.Form, error) {
	if word == "." || !strings.Contains(word, ".") {
		return syntax.Symbol(word).WithSpan(start, end), nil
	}
	parts := strings.Split(word, ".")
	if parts[0] == "" {
		return syntax.Form{}, runtime.ErrorAt(start, end, "dotted name cannot start with .")
	}
	if parts[len(parts)-1] == "" {
		return syntax.Form{}, runtime.ErrorAt(start, end, "dotted name cannot end with .")
	}
	xs := []syntax.Form{syntax.Symbol(".")}
	off := start
	for i, part := range parts {
		if part == "" {
			return syntax.Form{}, runtime.ErrorAt(start, end, "dotted name cannot contain empty field")
		}
		if i > 0 && scanner.IsNumLit(part) {
			return syntax.Form{}, runtime.ErrorAt(off, off+len(part), "dotted name field must be a name")
		}
		xs = append(xs, syntax.Symbol(part).WithSpan(off, off+len(part)))
		off += len(part) + 1
	}
	return syntax.CallList(xs...).WithSpan(start, end), nil
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
