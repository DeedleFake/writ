package writ

import (
	"encoding/json"
	"strings"
)

const maxInline = 72

type printer struct {
	src string
}

// Format pretty-prints src. A trailing newline is added when src is not empty.
func Format(src string) (string, error) {
	forms, err := Parse(src)
	if err != nil {
		return "", err
	}
	if len(forms) == 0 {
		return "", nil
	}
	p := &printer{src: src}
	text := p.formatVal(forms[0], 0)
	for i := 1; i < len(forms); i++ {
		prev := forms[i-1]
		form := forms[i]
		sep := "\n"
		if prev.k == KindComment {
			sep = "\n"
		} else if form.blank {
			sep = "\n\n"
		}
		text += sep + p.formatVal(form, 0)
	}
	return text + "\n", nil
}

func (p *printer) originalAtom(v Value) string {
	if p.src == "" || !v.hasSpan {
		return ""
	}
	if v.span.Start < 0 || v.span.End > len(p.src) || v.span.End <= v.span.Start {
		return ""
	}
	return p.src[v.span.Start:v.span.End]
}

func fnIsMulti(v Value) bool {
	if v.k != KindList || len(v.xs) == 0 || v.xs[0].k != KindSymbol || v.xs[0].s != "fn" {
		return false
	}
	parsed, err := parseFn(v.xs[1:])
	return err == nil && parsed.kind == "long" && len(parsed.clauses) > 1
}

func hasBreakComment(v Value) bool {
	switch v.k {
	case KindComment:
		return true
	case KindQuote, KindUnquote, KindSplice:
		return v.cmt != "" || hasBreakComment(v.innerVal())
	case KindMap:
		if v.mp == nil {
			return false
		}
		for _, x := range v.mp.vals {
			if x.cmt != "" || hasBreakComment(x) {
				return true
			}
		}
		return false
	case KindList:
		for i, x := range v.xs {
			if x.k == KindComment {
				return true
			}
			if x.cmt != "" && i < len(v.xs)-1 {
				return true
			}
			if hasBreakComment(x) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func suffixCmt(v Value, text string) string {
	if v.cmt != "" {
		return text + " " + v.cmt
	}
	return text
}

func prefixLines(prefix, text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) <= 1 {
		return prefix + text
	}
	pad := strings.Repeat(" ", len(prefix))
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(lines[0])
	for _, l := range lines[1:] {
		b.WriteByte('\n')
		b.WriteString(pad)
		b.WriteString(l)
	}
	return b.String()
}

func pushPrefixed(lines *[]string, prefix, text string) {
	*lines = append(*lines, strings.Split(prefixLines(prefix, text), "\n")...)
}

func (p *printer) formatVal(v Value, indent int) string {
	if v.k == KindComment {
		return v.s
	}
	if v.k == KindQuote || v.k == KindUnquote || v.k == KindSplice {
		mark := "'"
		if v.k == KindUnquote {
			mark = ","
		} else if v.k == KindSplice {
			mark = "@"
		}
		return suffixCmt(v, prefixLines(mark, p.formatVal(v.innerVal(), indent)))
	}
	if v.k == KindMap {
		return p.formatMap(v, indent)
	}
	if v.k != KindList {
		return p.formatAtom(v)
	}
	empty := "()"
	if v.vec {
		empty = "[]"
	}
	if len(v.xs) == 0 {
		return suffixCmt(v, empty)
	}
	if v.vec {
		if hasBreakComment(v) || v.broke {
			return suffixCmt(v, p.formatVecBlock(v, indent))
		}
		inline := p.formatInline(v)
		col := indent * 2
		if len(inline)+col <= maxInline {
			return inline
		}
		return suffixCmt(v, p.formatVecBlock(v, indent))
	}
	headName := ""
	if v.xs[0].k == KindSymbol {
		headName = v.xs[0].s
	}
	_, special := bodySpecials[headName]
	if hasBreakComment(v) || (special && v.broke) || fnIsMulti(v) {
		return suffixCmt(v, p.formatBlock(v, indent))
	}
	inline := p.formatInline(v)
	col := indent * 2
	if len(inline)+col <= maxInline {
		return inline
	}
	return suffixCmt(v, p.formatBlock(v, indent))
}

func closeOn(lines []string, close string) string {
	if len(lines) == 0 {
		return close
	}
	last := lines[len(lines)-1]
	cmt := strings.Index(last, " ;")
	if cmt >= 0 {
		lines[len(lines)-1] = last[:cmt] + close + last[cmt:]
	} else {
		lines[len(lines)-1] = last + close
	}
	return strings.Join(lines, "\n")
}

func (p *printer) formatVecBlock(v Value, indent int) string {
	if len(v.xs) == 0 {
		return "[]"
	}
	var lines []string
	pushPrefixed(&lines, "[", p.formatVal(v.xs[0], indent+1))
	for _, item := range v.xs[1:] {
		pushPrefixed(&lines, "  ", p.formatVal(item, indent+1))
	}
	return closeOn(lines, "]")
}

func (p *printer) formatMap(v Value, indent int) string {
	if v.mp.len() == 0 {
		return suffixCmt(v, "[:]")
	}
	pairs := v.mp.pairs()
	parts := make([]string, len(pairs))
	for i, pair := range pairs {
		parts[i] = pair.Key + ": " + p.formatInline(pair.Value)
	}
	inline := "[" + strings.Join(parts, " ") + "]"
	col := indent * 2
	if !hasBreakComment(v) && !v.broke && len(inline)+col <= maxInline {
		return suffixCmt(v, inline)
	}
	pairText := func(k string, val Value) string {
		return prefixLines(k+": ", p.formatVal(val, indent+1))
	}
	var lines []string
	pushPrefixed(&lines, "[", pairText(pairs[0].Key, pairs[0].Value))
	for _, pair := range pairs[1:] {
		pushPrefixed(&lines, " ", pairText(pair.Key, pair.Value))
	}
	return suffixCmt(v, closeOn(lines, "]"))
}

func (p *printer) formatBlock(v Value, indent int) string {
	body := "  "
	head := v.xs[0]
	headName := ""
	if head.k == KindSymbol {
		headName = head.s
	} else {
		headName = p.formatVal(head, indent)
	}

	if headName == "on" {
		ev := ""
		if len(v.xs) > 1 {
			ev = p.formatVal(v.xs[1], indent)
		}
		params := "()"
		if len(v.xs) > 2 {
			params = p.formatInline(v.xs[2])
		}
		rest := v.xs[3:]
		if len(rest) == 0 {
			return "(on " + ev + " " + params + ")"
		}
		lines := []string{"(on " + ev + " " + params}
		for _, item := range rest {
			pushPrefixed(&lines, body, p.formatVal(item, indent+1))
		}
		return closeOn(lines, ")")
	}

	if headName == "after" {
		i := 1
		header := "(" + headName
		for i < len(v.xs) && v.xs[i].k != KindList {
			header += " " + p.formatAtom(v.xs[i])
			i++
		}
		if i >= len(v.xs) {
			return header + ")"
		}
		lines := []string{header}
		for ; i < len(v.xs); i++ {
			pushPrefixed(&lines, body, p.formatVal(v.xs[i], indent+1))
		}
		return closeOn(lines, ")")
	}

	if headName == "if" {
		clauses, err := parseIfArgs(v.xs[1:])
		if err != nil {
			cond := "nil"
			if len(v.xs) > 1 {
				cond = p.formatVal(v.xs[1], indent+1)
			}
			var lines []string
			pushPrefixed(&lines, "(if ", cond)
			for _, item := range v.xs[2:] {
				pushPrefixed(&lines, body, p.formatVal(item, indent+1))
			}
			return closeOn(lines, ")")
		}
		var lines []string
		for i, c := range clauses {
			if i == 0 {
				t := "nil"
				if c.test != nil {
					t = p.formatVal(*c.test, indent+1)
				}
				not := ""
				if c.not {
					not = "not "
				}
				pushPrefixed(&lines, "(if "+not, t)
			} else if c.test != nil {
				not := ""
				if c.not {
					not = "not "
				}
				pushPrefixed(&lines, "else if "+not, p.formatVal(*c.test, indent+1))
			} else {
				lines = append(lines, "else")
			}
			for _, item := range c.body {
				pushPrefixed(&lines, body, p.formatVal(item, indent+1))
			}
		}
		if len(lines) == 0 {
			return "(if)"
		}
		return closeOn(lines, ")")
	}

	if headName == "def" || headName == "defm" {
		headStr := "()"
		if len(v.xs) > 1 {
			headStr = p.formatInline(v.xs[1])
		}
		rest := v.xs[2:]
		if len(rest) == 0 {
			return "(" + headName + " " + headStr + ")"
		}
		lines := []string{"(" + headName + " " + headStr}
		for _, item := range rest {
			pushPrefixed(&lines, body, p.formatVal(item, indent+1))
		}
		return closeOn(lines, ")")
	}

	if headName == "fn" {
		rest := v.xs[1:]
		parsed, err := parseFn(rest)
		if err == nil && parsed.kind == "long" {
			var lines []string
			for i, c := range parsed.clauses {
				sig := "()"
				if c.ParamsForm != nil {
					sig = p.formatInline(*c.ParamsForm)
				}
				if i == 0 {
					lines = append(lines, "(fn "+sig)
				} else {
					lines = append(lines, " fn "+sig)
				}
				for _, item := range c.Body {
					pushPrefixed(&lines, body, p.formatVal(item, indent+1))
				}
			}
			if len(lines) == 0 {
				return "(fn)"
			}
			return closeOn(lines, ")")
		}
		sig := ""
		if len(v.xs) > 1 {
			sig = " " + p.formatInline(v.xs[1])
		}
		after := v.xs[2:]
		if len(after) == 0 {
			return "(" + headName + sig + ")"
		}
		lines := []string{"(" + headName + sig}
		for _, item := range after {
			pushPrefixed(&lines, body, p.formatVal(item, indent+1))
		}
		return closeOn(lines, ")")
	}

	if headName == "let" || headName == "let!" {
		bindStr := "[:]"
		if len(v.xs) > 1 {
			bindStr = p.formatVal(v.xs[1], indent+1)
		}
		var lines []string
		pushPrefixed(&lines, "("+headName+" ", bindStr)
		for _, item := range v.xs[2:] {
			pushPrefixed(&lines, body, p.formatVal(item, indent+1))
		}
		return closeOn(lines, ")")
	}

	if headName == "pipe" {
		rest := v.xs[1:]
		if len(rest) == 0 {
			return "(pipe)"
		}
		first := p.formatVal(rest[0], indent+1)
		if len(rest) == 1 {
			return prefixLines("(pipe ", first) + ")"
		}
		var lines []string
		pushPrefixed(&lines, "(pipe ", first)
		for _, item := range rest[1:] {
			pushPrefixed(&lines, body, p.formatVal(item, indent+1))
		}
		return closeOn(lines, ")")
	}

	args := v.xs[1:]
	if len(args) == 0 {
		return "(" + headName + ")"
	}
	var chunks [][]Value
	for i := 0; i < len(args); {
		a := args[i]
		if a.isKeySym() && i+1 < len(args) {
			chunks = append(chunks, []Value{a, args[i+1]})
			i += 2
		} else {
			chunks = append(chunks, []Value{a})
			i++
		}
	}
	fmtChunk := func(c []Value) string {
		if len(c) == 2 && c[0].isKeySym() {
			return prefixLines(c[0].s+" ", p.formatVal(c[1], indent+1))
		}
		if len(c) == 1 {
			return p.formatVal(c[0], indent+1)
		}
		parts := make([]string, len(c))
		for i, x := range c {
			parts[i] = p.formatVal(x, indent+1)
		}
		return strings.Join(parts, " ")
	}
	argPad := strings.Repeat(" ", len(headName)+2)
	var lines []string
	pushPrefixed(&lines, "("+headName+" ", fmtChunk(chunks[0]))
	for _, c := range chunks[1:] {
		pushPrefixed(&lines, argPad, fmtChunk(c))
	}
	return closeOn(lines, ")")
}

func (p *printer) formatInline(v Value) string {
	if v.k == KindQuote || v.k == KindUnquote || v.k == KindSplice {
		mark := "'"
		if v.k == KindUnquote {
			mark = ","
		} else if v.k == KindSplice {
			mark = "@"
		}
		return mark + p.formatInline(v.innerVal())
	}
	if v.k != KindList {
		return p.formatAtom(v)
	}
	open, close := "(", ")"
	if v.vec {
		open, close = "[", "]"
	}
	if len(v.xs) == 0 {
		return suffixCmt(v, open+close)
	}
	last := v.xs[len(v.xs)-1]
	parts := make([]string, len(v.xs))
	for i, x := range v.xs {
		if i == len(v.xs)-1 {
			parts[i] = p.formatCore(x)
		} else {
			parts[i] = p.formatInline(x)
		}
	}
	text := open + strings.Join(parts, " ") + close
	if last.cmt != "" {
		text += " " + last.cmt
	}
	return suffixCmt(v, text)
}

func (p *printer) formatCore(v Value) string {
	if v.k == KindComment {
		return v.s
	}
	if v.k == KindQuote || v.k == KindUnquote || v.k == KindSplice {
		mark := "'"
		if v.k == KindUnquote {
			mark = ","
		} else if v.k == KindSplice {
			mark = "@"
		}
		return mark + p.formatCore(v.innerVal())
	}
	if v.k == KindList {
		return p.formatInline(v)
	}
	if v.k == KindMap {
		return p.formatMap(v, 0)
	}
	return p.formatAtomCore(v)
}

func (p *printer) formatAtomCore(v Value) string {
	switch v.k {
	case KindInt:
		if s := p.originalAtom(v); s != "" {
			return s
		}
		return formatInt(v)
	case KindFloat:
		if s := p.originalAtom(v); s != "" {
			return s
		}
		return formatFloat(v.f)
	case KindString:
		b, _ := json.Marshal(v.s)
		return string(b)
	case KindSymbol:
		if s := p.originalAtom(v); s != "" {
			return s
		}
		if reservedLit(v) {
			return v.s
		}
		return formatSymName(v.s)
	case KindFn:
		return "#<fn>"
	case KindComment:
		return v.s
	case KindList:
		return p.formatInline(v)
	case KindMap:
		return p.formatMap(v, 0)
	case KindQuote:
		return "'" + p.formatAtomCore(v.innerVal())
	case KindUnquote:
		return "," + p.formatAtomCore(v.innerVal())
	case KindSplice:
		return "@" + p.formatAtomCore(v.innerVal())
	default:
		return printVal(v)
	}
}

func (p *printer) formatAtom(v Value) string {
	return suffixCmt(v, p.formatAtomCore(v))
}
